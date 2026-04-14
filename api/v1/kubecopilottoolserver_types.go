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

// KubeCopilotToolServerSpec defines the desired state of KubeCopilotToolServer.
type KubeCopilotToolServerSpec struct {
	// URL is the endpoint URL of the MCP server (e.g., "http://mcp-k8s:8080/sse").
	// +required
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`

	// Transport is the MCP transport type: "sse" or "streamable-http". Defaults to "sse".
	// +optional
	// +kubebuilder:default="sse"
	// +kubebuilder:validation:Enum=sse;streamable-http
	Transport string `json:"transport,omitempty"`

	// Headers is optional authentication headers to send with requests.
	// +optional
	Headers map[string]string `json:"headers,omitempty"`

	// SecretRef optionally references a Secret containing authentication credentials.
	// The Secret data keys are added as HTTP headers on MCP requests.
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// KubeCopilotToolServerStatus defines the observed state of KubeCopilotToolServer.
type KubeCopilotToolServerStatus struct {
	// Phase describes the current lifecycle phase: Available, Unavailable, Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// AvailableTools lists the tool names discovered from the MCP server.
	// +optional
	AvailableTools []string `json:"availableTools,omitempty"`

	// LastChecked is the timestamp of the last health check.
	// +optional
	LastChecked metav1.Time `json:"lastChecked,omitempty"`

	// Conditions represent the current state of the tool server.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="URL",type="string",JSONPath=".spec.url"

// KubeCopilotToolServer is the Schema for the kubecopilottoolservers API.
// It represents an MCP (Model Context Protocol) server endpoint that provides
// tools to KubeCopilotAgent instances.
type KubeCopilotToolServer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeCopilotToolServerSpec   `json:"spec"`
	Status KubeCopilotToolServerStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeCopilotToolServerList contains a list of KubeCopilotToolServer.
type KubeCopilotToolServerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeCopilotToolServer `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeCopilotToolServer{}, &KubeCopilotToolServerList{})
}
