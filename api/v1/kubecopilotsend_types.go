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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ProviderConfig specifies a BYOK (Bring Your Own Key) model provider.
type ProviderConfig struct {
	// Type is the provider type (e.g. "openai", "azure").
	// +optional
	Type string `json:"type,omitempty"`

	// BaseURL is the base URL of the model provider's API.
	// +optional
	BaseURL string `json:"baseURL,omitempty"`

	// SecretRef references a Secret containing the API key (key: "api-key").
	// +optional
	SecretRef string `json:"secretRef,omitempty"`
}

// CustomAgent defines a sub-agent that the copilot can delegate to.
type CustomAgent struct {
	// Name is the unique identifier of the custom agent.
	// +required
	Name string `json:"name"`

	// DisplayName is the human-readable name shown in UI.
	// +optional
	DisplayName string `json:"displayName,omitempty"`

	// Description explains what this agent does.
	// +optional
	Description string `json:"description,omitempty"`

	// Prompt is the system prompt for this sub-agent.
	// +required
	Prompt string `json:"prompt"`

	// Tools is the list of tool names this agent can use.
	// +optional
	Tools []string `json:"tools,omitempty"`

	// Infer controls whether the agent infers tool use. Defaults to true.
	// +optional
	Infer *bool `json:"infer,omitempty"`
}

// SessionConfig holds per-session configuration that overrides agent defaults.
type SessionConfig struct {
	// Model overrides the default model (e.g. "gpt-4o", "claude-sonnet-4").
	// +optional
	Model string `json:"model,omitempty"`

	// SystemMessage overrides the default system prompt.
	// +optional
	SystemMessage string `json:"systemMessage,omitempty"`

	// DisabledSkills is a list of skill names to disable for this session.
	// +optional
	DisabledSkills []string `json:"disabledSkills,omitempty"`

	// CustomAgents defines sub-agents available in this session.
	// +optional
	CustomAgents []CustomAgent `json:"customAgents,omitempty"`

	// Provider specifies a BYOK model provider for this session.
	// +optional
	Provider *ProviderConfig `json:"provider,omitempty"`

	// ToolsConfig enables or disables specific tools by name.
	// +optional
	ToolsConfig map[string]bool `json:"toolsConfig,omitempty"`
}

// KubeCopilotSendSpec defines the desired state of KubeCopilotSend
type KubeCopilotSendSpec struct {
	// AgentRef is the name of the KubeCopilotAgent in the same namespace.
	// +required
	AgentRef string `json:"agentRef"`

	// Message is the prompt to send to the copilot agent asynchronously.
	// +required
	Message string `json:"message"`

	// SessionID optionally continues an existing conversation session.
	// +optional
	SessionID string `json:"sessionID,omitempty"`

	// SessionConfig holds optional per-session configuration overrides.
	// +optional
	SessionConfig *SessionConfig `json:"sessionConfig,omitempty"`
}

// KubeCopilotSendStatus defines the observed state of KubeCopilotSend.
type KubeCopilotSendStatus struct {
	// Phase: Pending, Queued, Done, Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// QueueID is the identifier returned by the agent for this queued request.
	// +optional
	QueueID string `json:"queueID,omitempty"`

	// ErrorMessage contains error details when Phase is Error.
	// +optional
	ErrorMessage string `json:"errorMessage,omitempty"`

	// Conditions represent the current state of the send request.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="AgentRef",type="string",JSONPath=".spec.agentRef"
// +kubebuilder:printcolumn:name="QueueID",type="string",JSONPath=".status.queueID"

// KubeCopilotSend is the Schema for the kubecopilotsends API
type KubeCopilotSend struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeCopilotSendSpec   `json:"spec"`
	Status KubeCopilotSendStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeCopilotSendList contains a list of KubeCopilotSend
type KubeCopilotSendList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeCopilotSend `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeCopilotSend{}, &KubeCopilotSendList{})
}
