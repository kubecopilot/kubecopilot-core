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
	"time"

	"github.com/google/uuid"
	corev1 "k8s.io/api/core/v1"
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
)

// KubeCopilotAgentReconciler reconciles a KubeCopilotAgent object
type KubeCopilotAgentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagents,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagents/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotagents/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch

func (r *KubeCopilotAgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	agent := &agentv1.KubeCopilotAgent{}
	if err := r.Get(ctx, req.NamespacedName, agent); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Assign AgentID if not set
	if agent.Status.AgentID == "" {
		agent.Status.AgentID = uuid.New().String()
		if err := r.Status().Update(ctx, agent); err != nil {
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
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      "agent-config",
			MountPath: "/copilot/copilot-instructions.md",
			SubPath:   "AGENT.md",
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
