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

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

// KubeCopilotSendReconciler reconciles a KubeCopilotSend object
type KubeCopilotSendReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends/finalizers,verbs=update
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotresponses,verbs=get;list;watch;create;update;patch

type asyncChatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
	SendRef   string `json:"send_ref,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	AgentRef  string `json:"agent_ref,omitempty"`
}

type asyncChatResponse struct {
	QueueID string `json:"queue_id"`
	Status  string `json:"status"`
}

const (
	phaseDone    = "Done"
	phaseError   = "Error"
	phaseRunning = "Running"
)

func (r *KubeCopilotSendReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	send := &agentv1.KubeCopilotSend{}
	if err := r.Get(ctx, req.NamespacedName, send); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotent: skip if already terminal
	if send.Status.Phase == phaseDone || send.Status.Phase == phaseError {
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
		log.Info("agent not running, requeueing", "agentPhase", agent.Status.Phase)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/asyncchat", agent.Status.ServiceName, send.Namespace)

	reqBody := asyncChatRequest{
		Message:   send.Spec.Message,
		SessionID: send.Spec.SessionID,
		SendRef:   send.Name,
		Namespace: send.Namespace,
		AgentRef:  send.Spec.AgentRef,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ctrl.Result{}, err
	}

	httpClient := &http.Client{Timeout: 15 * time.Second}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		log.Error(err, "failed to call agent asyncchat")
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
	return ctrl.Result{}, r.Status().Update(ctx, send)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotSendReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotSend{}).
		Named("kubecopilotsend").
		Complete(r)
}
