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

// ModelEntry describes a single LLM model available from a provider.
type ModelEntry struct {
	// Name is the model identifier (e.g. "gpt-4o", "claude-sonnet-4").
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`

	// MaxTokens is the maximum context window size for this model.
	// +optional
	// +kubebuilder:validation:Minimum=1
	MaxTokens *int32 `json:"maxTokens,omitempty"`

	// CostPerInputToken is the cost in USD per input token expressed as a
	// decimal string (e.g. "0.000001") for cost-based routing decisions.
	// +optional
	CostPerInputToken string `json:"costPerInputToken,omitempty"`

	// CostPerOutputToken is the cost in USD per output token expressed as a
	// decimal string (e.g. "0.000002") for cost-based routing decisions.
	// +optional
	CostPerOutputToken string `json:"costPerOutputToken,omitempty"`
}

// RateLimitSpec defines rate limiting parameters for a provider.
type RateLimitSpec struct {
	// RequestsPerMinute is the maximum number of API requests per minute.
	// +optional
	// +kubebuilder:validation:Minimum=1
	RequestsPerMinute *int32 `json:"requestsPerMinute,omitempty"`

	// TokensPerMinute is the maximum number of tokens per minute.
	// +optional
	// +kubebuilder:validation:Minimum=1
	TokensPerMinute *int32 `json:"tokensPerMinute,omitempty"`
}

// KubeCopilotModelConfigSpec defines the desired state of KubeCopilotModelConfig.
type KubeCopilotModelConfigSpec struct {
	// Provider is the LLM provider type.
	// +required
	// +kubebuilder:validation:Enum=openai;azure-openai;anthropic;google-vertex;ollama;custom
	Provider string `json:"provider"`

	// Endpoint is the base URL for the provider API.
	// Leave empty to use the provider's default public endpoint.
	// +optional
	Endpoint string `json:"endpoint,omitempty"`

	// SecretRef references a Kubernetes Secret that contains the API key
	// under the key "api-key". The Secret must be in the same namespace.
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`

	// Models is the list of LLM models available through this provider.
	// +optional
	Models []ModelEntry `json:"models,omitempty"`

	// RateLimits configures request and token rate limits for this provider.
	// +optional
	RateLimits *RateLimitSpec `json:"rateLimits,omitempty"`

	// Fallback optionally references another KubeCopilotModelConfig to try
	// when this provider fails or is rate-limited.
	// +optional
	Fallback *LocalObjectReference `json:"fallback,omitempty"`
}

// LocalObjectReference contains enough information to let you locate the
// referenced object inside the same namespace.
type LocalObjectReference struct {
	// Name of the referent.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// KubeCopilotModelConfigStatus defines the observed state of KubeCopilotModelConfig.
type KubeCopilotModelConfigStatus struct {
	// Phase describes the current lifecycle phase: Available, Unavailable, Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// LastValidated is the timestamp of the last successful provider connectivity check.
	// +optional
	LastValidated *metav1.Time `json:"lastValidated,omitempty"`

	// Conditions represent the current state of the model config.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint"

// KubeCopilotModelConfig is the Schema for the kubecopilotmodelconfigs API.
// It represents a centralized LLM provider configuration including credentials,
// available models, rate limits, and fallback chains.
type KubeCopilotModelConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeCopilotModelConfigSpec   `json:"spec"`
	Status KubeCopilotModelConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeCopilotModelConfigList contains a list of KubeCopilotModelConfig.
type KubeCopilotModelConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeCopilotModelConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeCopilotModelConfig{}, &KubeCopilotModelConfigList{})
}
