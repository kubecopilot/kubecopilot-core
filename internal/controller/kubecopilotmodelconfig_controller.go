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

// KubeCopilotModelConfigReconciler reconciles a KubeCopilotModelConfig object.
type KubeCopilotModelConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotmodelconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotmodelconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotmodelconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

func (r *KubeCopilotModelConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	modelConfig := &agentv1.KubeCopilotModelConfig{}
	if err := r.Get(ctx, req.NamespacedName, modelConfig); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Mark the model config as Available.
	if modelConfig.Status.Phase != phaseAvailable {
		modelConfig.Status.Phase = phaseAvailable
		now := metav1.Now()
		modelConfig.Status.LastValidated = &now
		meta.SetStatusCondition(&modelConfig.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Reconciled",
			Message:            "Model config is available",
			ObservedGeneration: modelConfig.GetGeneration(),
		})
		if err := r.Status().Update(ctx, modelConfig); err != nil {
			return ctrl.Result{}, err
		}
	}

	log.Info("Model config reconciled", "name", modelConfig.Name, "provider", modelConfig.Spec.Provider)
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotModelConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotModelConfig{}).
		Named("kubecopilotmodelconfig").
		Complete(r)
}
