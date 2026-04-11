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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

const (
	phaseAvailable = "Available"
)

// KubeCopilotToolServerReconciler reconciles a KubeCopilotToolServer object.
type KubeCopilotToolServerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilottoolservers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilottoolservers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilottoolservers/finalizers,verbs=update

func (r *KubeCopilotToolServerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	toolServer := &agentv1.KubeCopilotToolServer{}
	if err := r.Get(ctx, req.NamespacedName, toolServer); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Set phase to Available if not already set.
	if toolServer.Status.Phase != phaseAvailable {
		toolServer.Status.Phase = phaseAvailable
		meta.SetStatusCondition(&toolServer.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "Tool server is available",
			ObservedGeneration: toolServer.GetGeneration(),
		})
		toolServer.Status.LastChecked = metav1.Now()
		if err := r.Status().Update(ctx, toolServer); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Tool server reconciled", "name", toolServer.Name, "url", toolServer.Spec.URL)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotToolServerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotToolServer{}).
		Named("kubecopilottoolserver").
		Complete(r)
}
