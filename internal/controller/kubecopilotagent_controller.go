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
	"encoding/base64"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

const (
	defaultAgentImage = "quay.io/gfontana/kube-github-copilot-agent-server:v1.0"
	// rbacFinalizerName is set on the KubeCopilotAgent to ensure cluster-scoped
	// RBAC resources (ClusterRole, ClusterRoleBinding) are cleaned up on deletion.
	rbacFinalizerName = "kubecopilot.io/rbac-cleanup"
)

// KubeCopilotAgentReconciler reconciles a KubeCopilotAgent object
type KubeCopilotAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	// APIServerURL is the API server address used when generating kubeconfig
	// secrets for agents. When empty, in-cluster defaults are used.
	APIServerURL string
	// CAData is the PEM-encoded CA certificate used when generating kubeconfig
	// secrets. When empty, in-cluster defaults are used.
	CAData []byte
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagents/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings,verbs=get;list;watch;create;update;patch;delete

func (r *KubeCopilotAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	agent := &agentv1.KubeCopilotAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Reject invalid configuration: both fields are mutually exclusive.
	if agent.Spec.RBAC != nil && agent.Spec.KubeconfigSecretRef != nil {
		log.Error(nil, "spec.rbac and spec.kubeconfigSecretRef are mutually exclusive")
		agent.Status.Phase = phaseError
		_ = r.Status().Update(ctx, agent)
		return ctrl.Result{}, nil
	}

	// Handle deletion: clean up cluster-scoped RBAC resources.
	if !agent.DeletionTimestamp.IsZero() {
		if containsFinalizer(agent, rbacFinalizerName) {
			if err := r.cleanupClusterRBAC(ctx, agent); err != nil {
				log.Error(err, "Failed to clean up cluster-scoped RBAC resources")
				return ctrl.Result{}, err
			}
			removeFinalizer(agent, rbacFinalizerName)
			if err := r.Update(ctx, agent); err != nil {
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, nil
	}

	// Assign AgentID if not set
	if agent.Status.AgentID == "" {
		agent.Status.AgentID = uuid.New().String()
		if err := r.Status().Update(ctx, agent); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Manage RBAC resources when the RBAC spec is set.
	if agent.Spec.RBAC != nil {
		saName := agentServiceAccountName(agent)

		if err := r.ensureServiceAccount(ctx, agent, saName); err != nil {
			log.Error(err, "Failed to ensure ServiceAccount")
			return ctrl.Result{}, err
		}

		roleName := agent.Name + "-role"
		if len(agent.Spec.RBAC.Rules) > 0 {
			if err := r.ensureRole(ctx, agent, roleName); err != nil {
				log.Error(err, "Failed to ensure Role")
				return ctrl.Result{}, err
			}
			if err := r.ensureRoleBinding(ctx, agent, roleName, saName); err != nil {
				log.Error(err, "Failed to ensure RoleBinding")
				return ctrl.Result{}, err
			}
		}

		clusterRoleName := agent.Namespace + "-" + agent.Name + "-clusterrole"
		if len(agent.Spec.RBAC.ClusterRules) > 0 {
			// Ensure the finalizer is present so cluster-scoped resources are
			// cleaned up when the agent is deleted.
			if !containsFinalizer(agent, rbacFinalizerName) {
				addFinalizer(agent, rbacFinalizerName)
				if err := r.Update(ctx, agent); err != nil {
					return ctrl.Result{}, err
				}
			}

			if err := r.ensureClusterRole(ctx, agent, clusterRoleName); err != nil {
				log.Error(err, "Failed to ensure ClusterRole")
				return ctrl.Result{}, err
			}
			if err := r.ensureClusterRoleBinding(ctx, agent, clusterRoleName, saName); err != nil {
				log.Error(err, "Failed to ensure ClusterRoleBinding")
				return ctrl.Result{}, err
			}
		}

		kubeconfigSecretName := agent.Name + "-kubeconfig"
		if err := r.ensureKubeconfigSecret(ctx, agent, kubeconfigSecretName, saName); err != nil {
			log.Error(err, "Failed to ensure kubeconfig Secret")
			return ctrl.Result{}, err
		}
	}

	pvcName := agent.Name + "-session"
	if err := r.ensurePVC(ctx, agent, pvcName); err != nil {
		log.Error(err, "failed to ensure PVC")
		return ctrl.Result{}, err
	}

	podName := agent.Name + "-agent"
	if err := r.ensurePod(ctx, agent, podName, pvcName); err != nil {
		log.Error(err, "failed to ensure Pod")
		return ctrl.Result{}, err
	}

	svcName := agent.Name + "-agent-svc"
	if err := r.ensureService(ctx, agent, svcName, podName); err != nil {
		log.Error(err, "failed to ensure Service")
		return ctrl.Result{}, err
	}

	pod := &corev1.Pod{}
	if err := r.Get(ctx, types.NamespacedName{Name: podName, Namespace: agent.Namespace}, pod); err == nil {
		phase := phasePending
		switch pod.Status.Phase {
		case corev1.PodRunning:
			phase = phaseRunning
		case corev1.PodFailed:
			phase = phaseError
		}
		agent.Status.Phase = phase
		agent.Status.ServiceName = svcName
		if err := r.Status().Update(ctx, agent); err != nil {
			return ctrl.Result{}, err
		}
		// Requeue until the pod is Running so the phase gets updated
		if phase == phasePending {
			return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *KubeCopilotAgentReconciler) ensurePVC(ctx context.Context, agent *agentv1.KubeCopilotAgent, name string) error {
	pvc := &corev1.PersistentVolumeClaim{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: agent.Namespace}, pvc)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	storageSize := agent.Spec.StorageSize
	if storageSize == "" {
		storageSize = "1Gi"
	}

	pvc = &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agent.Namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
			Resources: corev1.VolumeResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceStorage: resource.MustParse(storageSize),
				},
			},
		},
	}
	setOwnerRef(agent, pvc)
	return r.Create(ctx, pvc)
}

func (r *KubeCopilotAgentReconciler) ensurePod(ctx context.Context, agent *agentv1.KubeCopilotAgent, name, pvcName string) error {
	pod := &corev1.Pod{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: agent.Namespace}, pod)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	image := agent.Spec.Image
	if image == "" {
		image = defaultAgentImage
	}

	volumes := []corev1.Volume{
		{
			Name: "session-storage",
			VolumeSource: corev1.VolumeSource{
				PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
					ClaimName: pvcName,
				},
			},
		},
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "session-storage",
			MountPath: "/copilot",
		},
	}

	if agent.Spec.SkillsConfigMap != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "skills-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: agent.Spec.SkillsConfigMap,
					},
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "skills-config",
			MountPath: "/copilot-skills-staging",
		})
	}

	if agent.Spec.AgentConfigMap != "" {
		volumes = append(volumes, corev1.Volume{
			Name: "agent-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: agent.Spec.AgentConfigMap,
					},
				},
			},
		})
		// Mount to a staging path; server.py copies it to the writable PVC at startup
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "agent-config",
			MountPath: "/copilot-agent-staging",
		})
	}

	healthProbe := &corev1.Probe{
		ProbeHandler: corev1.ProbeHandler{
			HTTPGet: &corev1.HTTPGetAction{
				Path: "/health",
				Port: intstr.FromInt32(8080),
			},
		},
	}

	// Optional kubeconfig secret
	envVars := []corev1.EnvVar{
		{
			Name: "GITHUB_TOKEN",
			ValueFrom: &corev1.EnvVarSource{
				SecretKeyRef: &corev1.SecretKeySelector{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: agent.Spec.GitHubTokenSecretRef.Name,
					},
					Key: "GITHUB_TOKEN",
				},
			},
		},
		{
			Name:  "COPILOT_HOME",
			Value: "/copilot",
		},
		{
			Name:  "AGENT_MD",
			Value: "/copilot/copilot-instructions.md",
		},
		{
			Name:  "WEBHOOK_URL",
			Value: fmt.Sprintf("http://kube-copilot-agent-webhook.%s.svc.cluster.local:8090/response", agent.Namespace),
		},
	}

	if agent.Spec.KubeconfigSecretRef != nil {
		volumes = append(volumes, corev1.Volume{
			Name: "kubeconfig",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: agent.Spec.KubeconfigSecretRef.Name,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "kubeconfig",
			MountPath: "/copilot/.kube/config",
			SubPath:   "config",
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "KUBECONFIG",
			Value: "/copilot/.kube/config",
		})
	} else if agent.Spec.RBAC != nil {
		// When RBAC is configured the operator generates the kubeconfig
		// automatically from the ServiceAccount token.
		kubeconfigSecretName := agent.Name + "-kubeconfig"
		volumes = append(volumes, corev1.Volume{
			Name: "kubeconfig",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: kubeconfigSecretName,
				},
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "kubeconfig",
			MountPath: "/copilot/.kube/config",
			SubPath:   "config",
		})
		envVars = append(envVars, corev1.EnvVar{
			Name:  "KUBECONFIG",
			Value: "/copilot/.kube/config",
		})
	}

	pod = &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agent.Namespace,
			Labels:    map[string]string{"app": name},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:            "agent",
					Image:           image,
					ImagePullPolicy: corev1.PullAlways,
					Ports: []corev1.ContainerPort{
						{ContainerPort: 8080},
					},
					Env:            envVars,
					VolumeMounts:   volumeMounts,
					LivenessProbe:  healthProbe,
					ReadinessProbe: healthProbe,
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						Limits: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("1000m"),
							corev1.ResourceMemory: resource.MustParse("2048Mi"),
						},
					},
				},
			},
			Volumes: volumes,
		},
	}

	setOwnerRef(agent, pod)
	return r.Create(ctx, pod)
}

func (r *KubeCopilotAgentReconciler) ensureService(ctx context.Context, agent *agentv1.KubeCopilotAgent, name, podName string) error {
	svc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: agent.Namespace}, svc)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	svc = &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agent.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{"app": podName},
			Ports: []corev1.ServicePort{
				{
					Port:       8080,
					TargetPort: intstr.FromInt32(8080),
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	setOwnerRef(agent, svc)
	return r.Create(ctx, svc)
}

// SetupWithManager sets up the controller with the Manager.
func (r *KubeCopilotAgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&agentv1.KubeCopilotAgent{}).
		Named("kubecopilotagent").
		Complete(r)
}

// ---------------------------------------------------------------------------
// RBAC resource helpers
// ---------------------------------------------------------------------------

// agentServiceAccountName returns the ServiceAccount name for the agent.
func agentServiceAccountName(agent *agentv1.KubeCopilotAgent) string {
	if agent.Spec.RBAC != nil && agent.Spec.RBAC.ServiceAccountName != "" {
		return agent.Spec.RBAC.ServiceAccountName
	}
	return agent.Name + "-sa"
}

func (r *KubeCopilotAgentReconciler) ensureServiceAccount(ctx context.Context, agent *agentv1.KubeCopilotAgent, name string) error {
	sa := &corev1.ServiceAccount{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: agent.Namespace}, sa)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	sa = &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agent.Namespace,
		},
	}
	setOwnerRef(agent, sa)
	return r.Create(ctx, sa)
}

func (r *KubeCopilotAgentReconciler) ensureRole(ctx context.Context, agent *agentv1.KubeCopilotAgent, name string) error {
	role := &rbacv1.Role{}
	key := types.NamespacedName{Name: name, Namespace: agent.Namespace}
	err := r.Get(ctx, key, role)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	desired := &rbacv1.Role{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: agent.Namespace,
		},
		Rules: agent.Spec.RBAC.Rules,
	}

	if errors.IsNotFound(err) {
		setOwnerRef(agent, desired)
		return r.Create(ctx, desired)
	}

	// Update rules if they have changed.
	role.Rules = desired.Rules
	return r.Update(ctx, role)
}

func (r *KubeCopilotAgentReconciler) ensureRoleBinding(ctx context.Context, agent *agentv1.KubeCopilotAgent, roleName, saName string) error {
	rb := &rbacv1.RoleBinding{}
	name := roleName + "-binding"
	key := types.NamespacedName{Name: name, Namespace: agent.Namespace}
	err := r.Get(ctx, key, rb)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	desiredSubjects := []rbacv1.Subject{
		{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      saName,
			Namespace: agent.Namespace,
		},
	}
	desiredRoleRef := rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "Role",
		Name:     roleName,
	}

	if errors.IsNotFound(err) {
		rb = &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: agent.Namespace,
			},
			Subjects: desiredSubjects,
			RoleRef:  desiredRoleRef,
		}
		setOwnerRef(agent, rb)
		return r.Create(ctx, rb)
	}

	// Update subjects if they have changed.
	rb.Subjects = desiredSubjects
	return r.Update(ctx, rb)
}

func (r *KubeCopilotAgentReconciler) ensureClusterRole(ctx context.Context, agent *agentv1.KubeCopilotAgent, name string) error {
	cr := &rbacv1.ClusterRole{}
	err := r.Get(ctx, types.NamespacedName{Name: name}, cr)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	if errors.IsNotFound(err) {
		cr = &rbacv1.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Rules: agent.Spec.RBAC.ClusterRules,
		}
		return r.Create(ctx, cr)
	}

	cr.Rules = agent.Spec.RBAC.ClusterRules
	return r.Update(ctx, cr)
}

func (r *KubeCopilotAgentReconciler) ensureClusterRoleBinding(ctx context.Context, agent *agentv1.KubeCopilotAgent, clusterRoleName, saName string) error {
	crb := &rbacv1.ClusterRoleBinding{}
	name := clusterRoleName + "-binding"
	err := r.Get(ctx, types.NamespacedName{Name: name}, crb)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	desiredSubjects := []rbacv1.Subject{
		{
			Kind:      rbacv1.ServiceAccountKind,
			Name:      saName,
			Namespace: agent.Namespace,
		},
	}
	desiredRoleRef := rbacv1.RoleRef{
		APIGroup: rbacv1.GroupName,
		Kind:     "ClusterRole",
		Name:     clusterRoleName,
	}

	if errors.IsNotFound(err) {
		crb = &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
			Subjects: desiredSubjects,
			RoleRef:  desiredRoleRef,
		}
		return r.Create(ctx, crb)
	}

	// Update subjects if they have changed.
	crb.Subjects = desiredSubjects
	return r.Update(ctx, crb)
}

// ensureKubeconfigSecret creates or updates a Secret containing a kubeconfig
// that uses the ServiceAccount token for authentication. The generated
// kubeconfig points to the in-cluster API server by default.
func (r *KubeCopilotAgentReconciler) ensureKubeconfigSecret(
	ctx context.Context,
	agent *agentv1.KubeCopilotAgent,
	name, saName string,
) error {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: name, Namespace: agent.Namespace}, secret)
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	kubeconfig := r.buildKubeconfig(agent, saName)

	if errors.IsNotFound(err) {
		secret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: agent.Namespace,
			},
			StringData: map[string]string{
				"config": kubeconfig,
			},
		}
		setOwnerRef(agent, secret)
		return r.Create(ctx, secret)
	}

	// Update the kubeconfig if it has changed.
	existing := ""
	if secret.Data != nil {
		existing = string(secret.Data["config"])
	}
	if existing != kubeconfig {
		secret.StringData = map[string]string{
			"config": kubeconfig,
		}
		return r.Update(ctx, secret)
	}
	return nil
}

// buildKubeconfig generates a kubeconfig YAML string that uses the projected
// ServiceAccount token for authentication.
func (r *KubeCopilotAgentReconciler) buildKubeconfig(agent *agentv1.KubeCopilotAgent, saName string) string {
	server := r.APIServerURL
	if server == "" {
		server = "https://kubernetes.default.svc"
	}

	caBlock := ""
	if len(r.CAData) > 0 {
		caBlock = fmt.Sprintf("    certificate-authority-data: %s",
			base64.StdEncoding.EncodeToString(r.CAData))
	} else {
		caBlock = "    certificate-authority: /var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	}

	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    server: %s
%s
  name: default
contexts:
- context:
    cluster: default
    namespace: %s
    user: %s
  name: default
current-context: default
users:
- name: %s
  user:
    tokenFile: /var/run/secrets/kubernetes.io/serviceaccount/token
`, server, caBlock, agent.Namespace, saName, saName)
}

// setOwnerRef sets an owner reference on obj pointing to agent without using
// the REST mapper (avoids "cannot find RESTMapping" errors when CRDs are newly installed).
func setOwnerRef(agent *agentv1.KubeCopilotAgent, obj metav1.Object) {
	isController := true
	obj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: schema.GroupVersion{Group: "kubecopilot.io", Version: "v1"}.String(),
			Kind:       "KubeCopilotAgent",
			Name:       agent.Name,
			UID:        agent.UID,
			Controller: &isController,
		},
	})
}

// ---------------------------------------------------------------------------
// Finalizer helpers
// ---------------------------------------------------------------------------

func containsFinalizer(agent *agentv1.KubeCopilotAgent, finalizer string) bool {
	return slices.Contains(agent.Finalizers, finalizer)
}

func addFinalizer(agent *agentv1.KubeCopilotAgent, finalizer string) {
	agent.Finalizers = append(agent.Finalizers, finalizer)
}

func removeFinalizer(agent *agentv1.KubeCopilotAgent, finalizer string) {
	var result []string
	for _, f := range agent.Finalizers {
		if f != finalizer {
			result = append(result, f)
		}
	}
	agent.Finalizers = result
}

// cleanupClusterRBAC deletes the ClusterRole and ClusterRoleBinding created
// for the agent. These are cluster-scoped so they cannot use ownerReferences
// for garbage collection and must be explicitly removed.
func (r *KubeCopilotAgentReconciler) cleanupClusterRBAC(ctx context.Context, agent *agentv1.KubeCopilotAgent) error {
	clusterRoleName := agent.Namespace + "-" + agent.Name + "-clusterrole"

	crb := &rbacv1.ClusterRoleBinding{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterRoleName + "-binding"}, crb); err == nil {
		if err := r.Delete(ctx, crb); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	cr := &rbacv1.ClusterRole{}
	if err := r.Get(ctx, types.NamespacedName{Name: clusterRoleName}, cr); err == nil {
		if err := r.Delete(ctx, cr); err != nil && !errors.IsNotFound(err) {
			return err
		}
	}

	return nil
}
