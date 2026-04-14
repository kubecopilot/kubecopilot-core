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
	"slices"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

const (
	phaseTeamActive = "Active"
)

// KubeCopilotAgentTeamReconciler reconciles a KubeCopilotAgentTeam object.
type KubeCopilotAgentTeamReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagentteams,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagentteams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagentteams/finalizers,verbs=update

func (r *KubeCopilotAgentTeamReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	team := &agentv1.KubeCopilotAgentTeam{}
	if err := r.Get(ctx, req.NamespacedName, team); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Validate the coordinator agent exists.
	coordinator := &agentv1.KubeCopilotAgent{}
	coordinatorKey := types.NamespacedName{
		Name:      team.Spec.Coordinator,
		Namespace: team.Namespace,
	}
	if err := r.Get(ctx, coordinatorKey, coordinator); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Coordinator agent not found", "coordinator", team.Spec.Coordinator)
			return r.setErrorStatus(ctx, team,
				fmt.Sprintf("coordinator agent %q not found", team.Spec.Coordinator))
		}
		return ctrl.Result{}, err
	}

	// Validate all member agents exist and collect their names.
	var memberNames []string
	for _, member := range team.Spec.Members {
		memberAgent := &agentv1.KubeCopilotAgent{}
		memberKey := types.NamespacedName{
			Name:      member.Name,
			Namespace: team.Namespace,
		}
		if err := r.Get(ctx, memberKey, memberAgent); err != nil {
			if errors.IsNotFound(err) {
				log.Info("Member agent not found", "member", member.Name)
				return r.setErrorStatus(ctx, team,
					fmt.Sprintf("member agent %q not found", member.Name))
			}
			return ctrl.Result{}, err
		}
		memberNames = append(memberNames, member.Name)
	}

	// Update the coordinator's delegateTo with all member agent names.
	if err := r.Get(ctx, coordinatorKey, coordinator); err != nil {
		return ctrl.Result{}, err
	}
	if !slices.Equal(coordinator.Spec.DelegateTo, memberNames) {
		coordinator.Spec.DelegateTo = memberNames
		if err := r.Update(ctx, coordinator); err != nil {
			log.Error(err, "Failed to update coordinator delegateTo")
			return ctrl.Result{}, err
		}
		log.Info("Updated coordinator delegateTo", "coordinator", team.Spec.Coordinator, "members", memberNames)
	}

	// Set status to Active.
	team.Status.Phase = phaseTeamActive
	team.Status.MemberCount = len(team.Spec.Members)
	meta.SetStatusCondition(&team.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionTrue,
		Reason:             "AllAgentsFound",
		Message:            "All coordinator and member agents are available",
		ObservedGeneration: team.Generation,
	})
	if err := r.Status().Update(ctx, team); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// setErrorStatus sets the team Phase to Error and updates conditions.
func (r *KubeCopilotAgentTeamReconciler) setErrorStatus(
	ctx context.Context,
	team *agentv1.KubeCopilotAgentTeam,
	message string,
) (ctrl.Result, error) {
	team.Status.Phase = phaseError
	team.Status.MemberCount = 0
	meta.SetStatusCondition(&team.Status.Conditions, metav1.Condition{
		Type:               "Ready",
		Status:             metav1.ConditionFalse,
		Reason:             "AgentNotFound",
		Message:            message,
		ObservedGeneration: team.Generation,
	})
	if err := r.Status().Update(ctx, team); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotAgentTeamReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotAgentTeam{}).
		Named("kubecopilotagentteam").
		Complete(r)
}
