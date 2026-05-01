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

package v1

import (
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KubeCopilotAgentSpec defines the desired state of KubeCopilotAgent
// +kubebuilder:validation:XValidation:rule="!(has(self.kubeconfigSecretRef) && has(self.rbac))",message="kubeconfigSecretRef and rbac are mutually exclusive"
type KubeCopilotAgentSpec struct {
	// GitHubTokenSecretRef references a Secret containing key GITHUB_TOKEN.
	// +required
	GitHubTokenSecretRef SecretReference `json:"githubTokenSecretRef"`

	// SkillsConfigMap is the name of a ConfigMap whose data keys are mounted
	// as files under /home/copilot/.copilot/skills/ inside the agent pod.
	// +optional
	SkillsConfigMap string `json:"skillsConfigMap,omitempty"`

	// AgentConfigMap is the name of a ConfigMap that must contain a key
	// "AGENT.md" mounted at /home/copilot/.copilot/AGENT.md.
	// +optional
	AgentConfigMap string `json:"agentConfigMap,omitempty"`

	// StorageSize is the size of the PersistentVolumeClaim for session state.
	// Defaults to "1Gi".
	// +optional
	// +kubebuilder:default="1Gi"
	StorageSize string `json:"storageSize,omitempty"`

	// Image overrides the default agent container image.
	// +optional
	Image string `json:"image,omitempty"`

	// KubeconfigSecretRef optionally references a Secret containing a kubeconfig
	// under key "config". When set, the secret is mounted at /copilot/.kube/config
	// and KUBECONFIG is set accordingly, giving kubectl/oc access to that cluster.
	// This field is mutually exclusive with RBAC — when RBAC is configured the
	// operator generates the kubeconfig automatically.
	// +optional
	KubeconfigSecretRef *SecretReference `json:"kubeconfigSecretRef,omitempty"`

	// RBAC configures a ServiceAccount and role-based permissions for the agent.
	// When set, the operator creates a ServiceAccount, a Role with the specified
	// rules, a RoleBinding, and a kubeconfig Secret so the agent pod runs with
	// least-privilege access scoped to these permissions.
	// This field is mutually exclusive with KubeconfigSecretRef.
	// +optional
	RBAC *AgentRBAC `json:"rbac,omitempty"`

	// ToolServers is a list of KubeCopilotToolServer names that this agent can use.
	// The operator passes these MCP server configurations to the agent container
	// as the MCP_SERVERS environment variable (JSON-encoded).
	// +optional
	ToolServers []string `json:"toolServers,omitempty"`

	// DelegateTo is a list of KubeCopilotAgent names that this agent can delegate
	// tasks to. The operator configures the agent with a delegate_to_agent tool
	// that creates KubeCopilotSend CRs targeting the specified agents.
	// +optional
	DelegateTo []string `json:"delegateTo,omitempty"`

	// ModelConfigRef references a KubeCopilotModelConfig in the same namespace
	// that provides LLM provider configuration (credentials, models, rate limits,
	// fallback chains) for this agent. When set, it supersedes per-request
	// provider config in SessionConfig.
	// +optional
	ModelConfigRef *LocalObjectReference `json:"modelConfigRef,omitempty"`
}

// AgentRBAC defines the ServiceAccount and RBAC rules for an agent.
type AgentRBAC struct {
	// ServiceAccountName is the name of the ServiceAccount to create for this
	// agent. Defaults to "<agent-name>-sa" when omitted.
	// +optional
	ServiceAccountName string `json:"serviceAccountName,omitempty"`

	// Rules define namespace-scoped permissions granted to the agent's
	// ServiceAccount via a Role and RoleBinding in the agent's namespace.
	// +optional
	Rules []rbacv1.PolicyRule `json:"rules,omitempty"`

	// ClusterRules define cluster-scoped permissions granted to the agent's
	// ServiceAccount via a ClusterRole and ClusterRoleBinding.
	// +optional
	ClusterRules []rbacv1.PolicyRule `json:"clusterRules,omitempty"`
}

// SecretReference is a reference to a secret by name.
type SecretReference struct {
	// Name of the secret.
	// +required
	Name string `json:"name"`
}

// KubeCopilotAgentStatus defines the observed state of KubeCopilotAgent.
type KubeCopilotAgentStatus struct {
	// AgentID is a stable UUID assigned on first reconcile.
	// +optional
	AgentID string `json:"agentID,omitempty"`

	// Phase describes the current lifecycle phase: Pending, Running, Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// ServiceName is the ClusterIP service name for this agent.
	// +optional
	ServiceName string `json:"serviceName,omitempty"`

	// Conditions represent the current state of the agent.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="AgentID",type="string",JSONPath=".status.agentID"

// KubeCopilotAgent is the Schema for the kubecopilotagents API
type KubeCopilotAgent struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeCopilotAgentSpec   `json:"spec"`
	Status KubeCopilotAgentStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeCopilotAgentList contains a list of KubeCopilotAgent
type KubeCopilotAgentList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeCopilotAgent `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeCopilotAgent{}, &KubeCopilotAgentList{})
}
