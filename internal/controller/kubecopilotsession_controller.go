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

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

const (
	sessionFinalizer = "kubecopilot.io/session-cleanup"
	phaseActive      = "Active"
	phaseTerminating = "Terminating"

	labelTenantID  = "kubecopilot.io/tenant-id"
	labelSessionNS = "kubecopilot.io/session"
)

// KubeCopilotSessionReconciler reconciles a KubeCopilotSession object.
// It creates an isolated namespace per session, installs a deny-all
// NetworkPolicy, and creates tenant-scoped RBAC so that each tenant's
// data stays private.
type KubeCopilotSessionReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsessions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsessions/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotsessions/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=networking.k8s.io,resources=networkpolicies,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;delete

func (r *KubeCopilotSessionReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	session := &agentv1.KubeCopilotSession{}
	if err := r.Get(ctx, req.NamespacedName, session); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Handle deletion: clean up the namespace when the session is removed.
	if !session.DeletionTimestamp.IsZero() {
		return r.handleDeletion(ctx, session)
	}

	// Ensure the finalizer is present.
	if !controllerutil.ContainsFinalizer(session, sessionFinalizer) {
		controllerutil.AddFinalizer(session, sessionFinalizer)
		if err := r.Update(ctx, session); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Validate that the referenced agent exists.
	agent := &agentv1.KubeCopilotAgent{}
	agentKey := types.NamespacedName{Name: session.Spec.AgentRef, Namespace: session.Namespace}
	if err := r.Get(ctx, agentKey, agent); err != nil {
		if errors.IsNotFound(err) {
			log.Info("Referenced agent not found", "agentRef", session.Spec.AgentRef)
			return r.setPhase(ctx, session, phaseError, fmt.Sprintf("agent %q not found", session.Spec.AgentRef))
		}
		return ctrl.Result{}, err
	}

	nsName := sessionNamespaceName(session)

	// 1. Create the session namespace.
	if err := r.ensureNamespace(ctx, session, nsName); err != nil {
		log.Error(err, "Failed to ensure namespace")
		return ctrl.Result{}, err
	}

	// 2. Create a deny-all NetworkPolicy when isolation is strict.
	if session.Spec.IsolationLevel == "" || session.Spec.IsolationLevel == "strict" {
		if err := r.ensureNetworkPolicy(ctx, session, nsName); err != nil {
			log.Error(err, "Failed to ensure NetworkPolicy")
			return ctrl.Result{}, err
		}
	}

	// 3. Create tenant-scoped RBAC.
	if err := r.ensureRBAC(ctx, session, nsName); err != nil {
		log.Error(err, "Failed to ensure RBAC")
		return ctrl.Result{}, err
	}

	// 4. Update status.
	session.Status.Namespace = nsName
	session.Status.Phase = phaseActive
	if err := r.Status().Update(ctx, session); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("Session reconciled", "namespace", nsName, "tenantID", session.Spec.TenantID)
	return ctrl.Result{}, nil
}

// handleDeletion removes the session namespace and the finalizer.
func (r *KubeCopilotSessionReconciler) handleDeletion(ctx context.Context, session *agentv1.KubeCopilotSession) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	if controllerutil.ContainsFinalizer(session, sessionFinalizer) {
		// Update phase to Terminating.
		session.Status.Phase = phaseTerminating
		if err := r.Status().Update(ctx, session); err != nil {
			return ctrl.Result{}, err
		}

		nsName := sessionNamespaceName(session)
		ns := &corev1.Namespace{}
		if err := r.Get(ctx, types.NamespacedName{Name: nsName}, ns); err == nil {
			log.Info("Deleting session namespace", "namespace", nsName)
			if err := r.Delete(ctx, ns); err != nil && !errors.IsNotFound(err) {
				return ctrl.Result{}, err
			}
		}

		controllerutil.RemoveFinalizer(session, sessionFinalizer)
		if err := r.Update(ctx, session); err != nil {
			return ctrl.Result{}, err
		}
	}
	return ctrl.Result{}, nil
}

// ensureNamespace creates the session namespace with tenant labels.
func (r *KubeCopilotSessionReconciler) ensureNamespace(ctx context.Context, session *agentv1.KubeCopilotSession, name string) error {
	ns := &corev1.Namespace{}
	if err := r.Get(ctx, types.NamespacedName{Name: name}, ns); err == nil {
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}

	ns = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				labelTenantID:  session.Spec.TenantID,
				labelSessionNS: session.Name,
			},
		},
	}
	return r.Create(ctx, ns)
}

// ensureNetworkPolicy installs a default-deny ingress policy that blocks all
// traffic from pods outside the session namespace.
func (r *KubeCopilotSessionReconciler) ensureNetworkPolicy(ctx context.Context, session *agentv1.KubeCopilotSession, nsName string) error {
	npName := "tenant-isolation"
	np := &networkingv1.NetworkPolicy{}
	if err := r.Get(ctx, types.NamespacedName{Name: npName, Namespace: nsName}, np); err == nil {
		return nil
	} else if !errors.IsNotFound(err) {
		return err
	}

	np = &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      npName,
			Namespace: nsName,
			Labels: map[string]string{
				labelTenantID: session.Spec.TenantID,
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			// Select all pods in the namespace.
			PodSelector: metav1.LabelSelector{},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeIngress,
			},
			// Allow ingress only from the same namespace.
			Ingress: []networkingv1.NetworkPolicyIngressRule{
				{
					From: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									labelSessionNS: session.Name,
								},
							},
						},
					},
				},
			},
		},
	}
	return r.Create(ctx, np)
}

// ensureRBAC creates a Role and RoleBinding that restrict the tenant to only
// operate on resources within the session namespace.
func (r *KubeCopilotSessionReconciler) ensureRBAC(ctx context.Context, session *agentv1.KubeCopilotSession, nsName string) error {
	roleName := "tenant-session-role"
	role := &rbacv1.Role{}
	if err := r.Get(ctx, types.NamespacedName{Name: roleName, Namespace: nsName}, role); errors.IsNotFound(err) {
		role = &rbacv1.Role{
			ObjectMeta: metav1.ObjectMeta{
				Name:      roleName,
				Namespace: nsName,
				Labels: map[string]string{
					labelTenantID: session.Spec.TenantID,
				},
			},
			Rules: []rbacv1.PolicyRule{
				{
					APIGroups: []string{"kubecopilot.io"},
					Resources: []string{
						"kubecopilotagents",
						"kubecopilotmessages",
						"kubecopilotsends",
						"kubecopilotresponses",
						"kubecopilotchunks",
						"kubecopilotcancels",
					},
					Verbs: []string{"get", "list", "watch", "create", "update", "patch", "delete"},
				},
			},
		}
		if err := r.Create(ctx, role); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	rbName := "tenant-session-binding"
	rb := &rbacv1.RoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: rbName, Namespace: nsName}, rb); errors.IsNotFound(err) {
		rb = &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rbName,
				Namespace: nsName,
				Labels: map[string]string{
					labelTenantID: session.Spec.TenantID,
				},
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "Role",
				Name:     roleName,
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      rbacv1.GroupKind,
					Name:      fmt.Sprintf("kubecopilot:tenant:%s", session.Spec.TenantID),
					Namespace: nsName,
				},
			},
		}
		if err := r.Create(ctx, rb); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}

	return nil
}

// setPhase updates the session phase and error condition.
func (r *KubeCopilotSessionReconciler) setPhase(ctx context.Context, session *agentv1.KubeCopilotSession, phase, message string) (ctrl.Result, error) {
	session.Status.Phase = phase
	if phase == phaseError {
		meta.SetStatusCondition(&session.Status.Conditions, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "Error",
			Message:            message,
			LastTransitionTime: metav1.Now(),
		})
	}
	if err := r.Status().Update(ctx, session); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

// sessionNamespaceName returns the deterministic namespace name for a session.
func sessionNamespaceName(session *agentv1.KubeCopilotSession) string {
	return fmt.Sprintf("kc-session-%s", session.Name)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotSessionReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotSession{}).
		Named("kubecopilotsession").
		Complete(r)
}
