/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var sendTracer = otel.Tracer("kubecopilot/controller/send")

// KubeCopilotSendReconciler reconciles a KubeCopilotSend object
type KubeCopilotSendReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotresponses,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotpolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

type providerConfig struct {
	Type      string `json:"type,omitempty"`
	BaseURL   string `json:"base_url,omitempty"`
	APIKey    string `json:"api_key,omitempty"`
	ModelName string `json:"model_name,omitempty"`
}

type customAgentConfig struct {
	Name        string   `json:"name"`
	DisplayName string   `json:"display_name,omitempty"`
	Description string   `json:"description,omitempty"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
	Infer       *bool    `json:"infer,omitempty"`
}

type sessionConfigPayload struct {
	Model          string              `json:"model,omitempty"`
	SystemMessage  string              `json:"system_message,omitempty"`
	DisabledSkills []string            `json:"disabled_skills,omitempty"`
	CustomAgents   []customAgentConfig `json:"custom_agents,omitempty"`
	Provider       *providerConfig     `json:"provider,omitempty"`
	ToolsConfig    map[string]bool     `json:"tools_config,omitempty"`
}

type asyncChatRequest struct {
	Message       string                `json:"message"`
	SessionID     string                `json:"session_id,omitempty"`
	SendRef       string                `json:"send_ref,omitempty"`
	Namespace     string                `json:"namespace,omitempty"`
	AgentRef      string                `json:"agent_ref,omitempty"`
	SessionConfig *sessionConfigPayload `json:"session_config,omitempty"`
}

type asyncChatResponse struct {
	QueueID string `json:"queue_id"`
	Status  string `json:"status"`
}

func (r *KubeCopilotSendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ctx, span := sendTracer.Start(ctx, "KubeCopilotSend.Reconcile")
	defer span.End()
	span.SetAttributes(
		attribute.String("kubecopilot.send.name", req.Name),
		attribute.String("kubecopilot.send.namespace", req.Namespace),
	)

	log := logf.FromContext(ctx)

	send := &agentv1.KubeCopilotSend{}
	if err := r.Get(ctx, req.NamespacedName, send); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotent: skip if already terminal
	if send.Status.Phase == phaseDone || send.Status.Phase == phaseError || send.Status.Phase == phaseDenied {
		return ctrl.Result{}, nil
	}

	// If previously paused for approval, check if now approved
	if send.Status.Phase == phasePendingApproval && !send.Spec.Approved {
		return ctrl.Result{}, nil
	}

	agent := &agentv1.KubeCopilotAgent{}
	if err := r.Get(ctx, types.NamespacedName{Name: send.Spec.AgentRef, Namespace: send.Namespace}, agent); err != nil {
		log.Error(err, "failed to get agent", "agentRef", send.Spec.AgentRef)
		send.Status.Phase = phaseError
		send.Status.ErrorMessage = fmt.Sprintf("agent not found: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, send)
	}

	if agent.Status.Phase != phaseRunning {
		log.Info("Agent not running, requeueing", "agentPhase", agent.Status.Phase)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	// Evaluate policies before dispatch (pre-dispatch screening).
	// Deny rules always take precedence over require-approval rules.
	evalResult, err := EvaluatePolicies(ctx, r.Client, send.Namespace, send.Spec.AgentRef, send.Spec.Message)
	if err != nil {
		log.Error(err, "Failed to evaluate policies")
		send.Status.Phase = phaseError
		send.Status.ErrorMessage = fmt.Sprintf("policy evaluation failed: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, send)
	}

	switch evalResult.Decision {
	case PolicyDecisionDeny:
		log.Info("Send denied by policy", "policy", evalResult.PolicyName, "rule", evalResult.RuleName)
		send.Status.Phase = phaseDenied
		msg := fmt.Sprintf("denied by policy %q rule %q", evalResult.PolicyName, evalResult.RuleName)
		if evalResult.Message != "" {
			msg += ": " + evalResult.Message
		}
		send.Status.ErrorMessage = msg
		return ctrl.Result{}, r.Status().Update(ctx, send)

	case PolicyDecisionRequireApproval:
		if !send.Spec.Approved {
			log.Info("Send requires approval", "policy", evalResult.PolicyName, "rule", evalResult.RuleName)
			send.Status.Phase = phasePendingApproval
			msg := fmt.Sprintf("requires approval per policy %q rule %q", evalResult.PolicyName, evalResult.RuleName)
			if evalResult.Message != "" {
				msg += ": " + evalResult.Message
			}
			send.Status.ErrorMessage = msg
			return ctrl.Result{}, r.Status().Update(ctx, send)
		}
		log.Info("Send approved, proceeding", "policy", evalResult.PolicyName, "rule", evalResult.RuleName)
	}

	span.SetAttributes(
		attribute.String("kubecopilot.send.agent_ref", send.Spec.AgentRef),
		attribute.String("kubecopilot.send.session_id", send.Spec.SessionID),
	)
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/asyncchat", agent.Status.ServiceName, send.Namespace)

	reqBody := asyncChatRequest{
		Message:   send.Spec.Message,
		SessionID: send.Spec.SessionID,
		SendRef:   send.Name,
		Namespace: send.Namespace,
		AgentRef:  send.Spec.AgentRef,
	}

	// Forward optional session config
	if sc := send.Spec.SessionConfig; sc != nil {
		payload := &sessionConfigPayload{
			Model:          sc.Model,
			SystemMessage:  sc.SystemMessage,
			DisabledSkills: sc.DisabledSkills,
			ToolsConfig:    sc.ToolsConfig,
		}
		for _, ca := range sc.CustomAgents {
			payload.CustomAgents = append(payload.CustomAgents, customAgentConfig{
				Name:        ca.Name,
				DisplayName: ca.DisplayName,
				Description: ca.Description,
				Prompt:      ca.Prompt,
				Tools:       ca.Tools,
				Infer:       ca.Infer,
			})
		}
		// Resolve provider Secret if referenced
		if sc.Provider != nil {
			pc := &providerConfig{
				Type:    sc.Provider.Type,
				BaseURL: sc.Provider.BaseURL,
			}
			if sc.Provider.SecretRef != "" {
				secret := &corev1.Secret{}
				secretKey := types.NamespacedName{Name: sc.Provider.SecretRef, Namespace: send.Namespace}
				if err := r.Get(ctx, secretKey, secret); err != nil {
					log.Error(err, "Failed to get provider secret", "secretRef", sc.Provider.SecretRef)
					send.Status.Phase = phaseError
					send.Status.ErrorMessage = fmt.Sprintf("provider secret not found: %v", err)
					return ctrl.Result{}, r.Status().Update(ctx, send)
				}
				// Secret values override CRD fields (secret is the authoritative source for BYOK)
				if t, ok := secret.Data["type"]; ok {
					pc.Type = string(t)
				}
				if baseURL, ok := secret.Data["base-url"]; ok {
					pc.BaseURL = string(baseURL)
				}
				if apiKey, ok := secret.Data["api-key"]; ok {
					pc.APIKey = string(apiKey)
				}
				if modelName, ok := secret.Data["model-name"]; ok {
					pc.ModelName = string(modelName)
					if payload.Model == "" {
						payload.Model = string(modelName)
					}
				}
			}
			payload.Provider = pc
		}
		reqBody.SessionConfig = payload
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ctrl.Result{}, err
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		log.Error(err, "failed to call agent asyncchat")
		span.RecordError(err)
		span.SetStatus(codes.Error, "asyncchat call failed")
		send.Status.Phase = phaseError
		send.Status.ErrorMessage = err.Error()
		return ctrl.Result{}, r.Status().Update(ctx, send)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		send.Status.Phase = phaseError
		send.Status.ErrorMessage = fmt.Sprintf("agent returned status %d", resp.StatusCode)
		return ctrl.Result{}, r.Status().Update(ctx, send)
	}

	var asyncResp asyncChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&asyncResp); err != nil {
		send.Status.Phase = phaseError
		send.Status.ErrorMessage = fmt.Sprintf("failed to decode response: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, send)
	}

	send.Status.Phase = phaseDone
	send.Status.QueueID = asyncResp.QueueID
	span.SetAttributes(attribute.String("kubecopilot.send.queue_id", asyncResp.QueueID))
	return ctrl.Result{}, r.Status().Update(ctx, send)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotSendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotSend{}).
		Named("kubecopilotsend").
		Complete(r)
}
