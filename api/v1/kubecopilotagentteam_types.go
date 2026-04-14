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

// TeamMember defines a member agent in a team.
type TeamMember struct {
	// Name is the name of the KubeCopilotAgent CR.
	// +required
	Name string `json:"name"`

	// Role describes this member's role in the team (e.g., "network-expert", "security-auditor").
	// +required
	Role string `json:"role"`

	// Description explains what this member specializes in.
	// +optional
	Description string `json:"description,omitempty"`
}

// KubeCopilotAgentTeamSpec defines the desired state of KubeCopilotAgentTeam.
type KubeCopilotAgentTeamSpec struct {
	// Coordinator is the name of the KubeCopilotAgent that acts as the team coordinator.
	// It receives user messages and delegates to member agents.
	// +required
	Coordinator string `json:"coordinator"`

	// Members is the list of member agents in the team.
	// +required
	// +kubebuilder:validation:MinItems=1
	Members []TeamMember `json:"members"`

	// Strategy defines how the coordinator delegates to members.
	// "sequential" processes members one at a time, "parallel" delegates to all simultaneously.
	// +optional
	// +kubebuilder:default="sequential"
	// +kubebuilder:validation:Enum=sequential;parallel
	Strategy string `json:"strategy,omitempty"`
}

// KubeCopilotAgentTeamStatus defines the observed state of KubeCopilotAgentTeam.
type KubeCopilotAgentTeamStatus struct {
	// Phase describes the current lifecycle phase: Pending, Active, Error.
	// +optional
	Phase string `json:"phase,omitempty"`

	// MemberCount is the number of active member agents.
	// +optional
	MemberCount int `json:"memberCount,omitempty"`

	// Conditions represent the current state of the team.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Coordinator",type="string",JSONPath=".spec.coordinator"
// +kubebuilder:printcolumn:name="Members",type="integer",JSONPath=".status.memberCount"
// +kubebuilder:printcolumn:name="Strategy",type="string",JSONPath=".spec.strategy"

// KubeCopilotAgentTeam is the Schema for the kubecopilotagentteams API.
type KubeCopilotAgentTeam struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KubeCopilotAgentTeamSpec   `json:"spec"`
	Status KubeCopilotAgentTeamStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// KubeCopilotAgentTeamList contains a list of KubeCopilotAgentTeam.
type KubeCopilotAgentTeamList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []KubeCopilotAgentTeam `json:"items"`
}

func init() {
	SchemeBuilder.Register(&KubeCopilotAgentTeam{}, &KubeCopilotAgentTeamList{})
}
