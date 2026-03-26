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

// KubeCopilotSessionSpec defines the desired state of KubeCopilotSession.
type KubeCopilotSessionSpec struct {
	// TenantID is a unique identifier for the tenant that owns this session.
	// All resources created for this session are labelled with this tenant ID
	// and placed in a dedicated namespace for strict isolation.
	// +required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern="^[a-z0-9]([a-z0-9-]*[a-z0-9])?$"
	TenantID string `json:"tenantID"`

	// AgentRef is the name of a KubeCopilotAgent in the same namespace to use
	// as the template for the session's agent. The agent configuration is
	// cloned into the session namespace.
	// +required
	AgentRef string `json:"agentRef"`

	// IsolationLevel controls the level of network isolation applied to the
	// session namespace. "strict" (the default) installs a NetworkPolicy that
	// denies all ingress from pods outside the session namespace.
	// "none" skips NetworkPolicy creation.
	// +optional
	// +kubebuilder:default="strict"
	// +kubebuilder:validation:Enum=strict;none
	IsolationLevel string `json:"isolationLevel,omitempty"`
}

// KubeCopilotSessionStatus defines the observed state of KubeCopilotSession.
type KubeCopilotSessionStatus struct {
	// Phase describes the current lifecycle phase.
	// Pending → Active → Terminating (on deletion).
	// +optional
	Phase string `json:"phase,omitempty"`

	// Namespace is the isolated namespace created for this session.
	// +optional
	Namespace string `json:"namespace,omitempty"`

	// Conditions represent the current state of the session.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="TenantID",type="string",JSONPath=".spec.tenantID"
// +kubebuilder:printcolumn:name="Namespace",type="string",JSONPath=".status.namespace"

// KubeCopilotSession is the Schema for the kubecopilotsessions API.
// It represents an isolated tenant session: the controller creates a dedicated
// namespace, a deny-all NetworkPolicy, and a tenant-scoped Role/RoleBinding
// so that each session's data is private.
type KubeCopilotSession struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeCopilotSessionSpec   `json:"spec"`
	Status KubeCopilotSessionStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeCopilotSessionList contains a list of KubeCopilotSession
type KubeCopilotSessionList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeCopilotSession `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeCopilotSession{}, &KubeCopilotSessionList{})
}
