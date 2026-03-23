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

// KubeCopilotMessageReconciler reconciles a KubeCopilotMessage object
type KubeCopilotMessageReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotmessages,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotmessages/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotmessages/finalizers,verbs=update

type chatRequest struct {
	Message   string `json:"message"`
	SessionID string `json:"session_id,omitempty"`
}

type chatResponse struct {
	Response  string `json:"response"`
	SessionID string `json:"session_id"`
}

func (r *KubeCopilotMessageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cmd := &agentv1.KubeCopilotMessage{}
	if err := r.Get(ctx, req.NamespacedName, cmd); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotent: skip if already terminal
	if cmd.Status.Phase == "Done" || cmd.Status.Phase == "Error" {
		return ctrl.Result{}, nil
	}

	agent := &agentv1.KubeCopilotAgent{}
	if err := r.Get(ctx, types.NamespacedName{Name: cmd.Spec.AgentRef, Namespace: cmd.Namespace}, agent); err != nil {
		log.Error(err, "failed to get agent", "agentRef", cmd.Spec.AgentRef)
		cmd.Status.Phase = "Error"
		cmd.Status.ErrorMessage = fmt.Sprintf("agent not found: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, cmd)
	}

	if agent.Status.Phase != "Running" {
		log.Info("agent not running, requeueing", "agentPhase", agent.Status.Phase)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/chat", agent.Status.ServiceName, cmd.Namespace)

	reqBody := chatRequest{
		Message:   cmd.Spec.Message,
		SessionID: cmd.Spec.SessionID,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return ctrl.Result{}, err
	}

	httpClient := &http.Client{Timeout: 120 * time.Second}
	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(bodyBytes))
	if err != nil {
		log.Error(err, "failed to call agent")
		cmd.Status.Phase = "Error"
		cmd.Status.ErrorMessage = err.Error()
		return ctrl.Result{}, r.Status().Update(ctx, cmd)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		cmd.Status.Phase = "Error"
		cmd.Status.ErrorMessage = fmt.Sprintf("agent returned status %d", resp.StatusCode)
		return ctrl.Result{}, r.Status().Update(ctx, cmd)
	}

	var chatResp chatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		cmd.Status.Phase = "Error"
		cmd.Status.ErrorMessage = fmt.Sprintf("failed to decode response: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, cmd)
	}

	cmd.Status.Phase = "Done"
	cmd.Status.Response = chatResp.Response
	cmd.Status.SessionID = chatResp.SessionID
	return ctrl.Result{}, r.Status().Update(ctx, cmd)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotMessageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotMessage{}).
		Named("kubecopilotmessage").
		Complete(r)
}
