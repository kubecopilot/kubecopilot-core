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
	"context"
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

// KubeCopilotCancelReconciler reconciles a KubeCopilotCancel object
type KubeCopilotCancelReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotcancels,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotcancels/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsends,verbs=get;list;watch

func (r *KubeCopilotCancelReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	cancel := &agentv1.KubeCopilotCancel{}
	if err := r.Get(ctx, req.NamespacedName, cancel); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Idempotent: skip if already terminal
	if cancel.Status.Phase == "Cancelled" || cancel.Status.Phase == "Error" {
		return ctrl.Result{}, nil
	}

	// Look up the KubeCopilotSend to get its queue_id
	send := &agentv1.KubeCopilotSend{}
	if err := r.Get(ctx, types.NamespacedName{Name: cancel.Spec.SendRef, Namespace: cancel.Namespace}, send); err != nil {
		log.Error(err, "failed to get KubeCopilotSend", "sendRef", cancel.Spec.SendRef)
		cancel.Status.Phase = "Error"
		cancel.Status.ErrorMessage = fmt.Sprintf("send not found: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, cancel)
	}

	queueID := send.Status.QueueID
	if queueID == "" {
		// Send not yet queued — requeue briefly
		log.Info("send has no queueID yet, requeueing", "sendRef", cancel.Spec.SendRef)
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	// Find the agent to get its service name
	agent := &agentv1.KubeCopilotAgent{}
	if err := r.Get(ctx, types.NamespacedName{Name: cancel.Spec.AgentRef, Namespace: cancel.Namespace}, agent); err != nil {
		log.Error(err, "failed to get agent", "agentRef", cancel.Spec.AgentRef)
		cancel.Status.Phase = "Error"
		cancel.Status.ErrorMessage = fmt.Sprintf("agent not found: %v", err)
		return ctrl.Result{}, r.Status().Update(ctx, cancel)
	}

	// POST /cancel/{queue_id} to the agent
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:8080/cancel/%s",
		agent.Status.ServiceName, cancel.Namespace, queueID)

	httpClient := &http.Client{Timeout: 10 * time.Second}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		cancel.Status.Phase = "Error"
		cancel.Status.ErrorMessage = err.Error()
		return ctrl.Result{}, r.Status().Update(ctx, cancel)
	}

	resp, err := httpClient.Do(httpReq)
	if err != nil {
		log.Error(err, "failed to POST cancel to agent", "url", url)
		cancel.Status.Phase = "Error"
		cancel.Status.ErrorMessage = err.Error()
		return ctrl.Result{}, r.Status().Update(ctx, cancel)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		cancel.Status.Phase = "Error"
		cancel.Status.ErrorMessage = fmt.Sprintf("agent returned status %d", resp.StatusCode)
		return ctrl.Result{}, r.Status().Update(ctx, cancel)
	}

	log.Info("cancelled agent request", "sendRef", cancel.Spec.SendRef, "queueID", queueID)
	cancel.Status.Phase = "Cancelled"
	return ctrl.Result{}, r.Status().Update(ctx, cancel)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotCancelReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotCancel{}).
		Named("kubecopilotcancel").
		Complete(r)
}
