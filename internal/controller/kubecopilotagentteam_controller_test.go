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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var _ = Describe("KubeCopilotAgentTeam Controller", func() {

	const namespace = "default"

	// Helper: ensure a KubeCopilotAgent and its token secret exist.
	ensureAgent := func(ctx context.Context, name string) {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{Name: "gh-token-team", Namespace: namespace}
		if err := k8sClient.Get(ctx, secretKey, secret); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "gh-token-team", Namespace: namespace},
				StringData: map[string]string{"GITHUB_TOKEN": "tok"},
			})).To(Succeed())
		}

		agent := &agentv1.KubeCopilotAgent{}
		agentKey := types.NamespacedName{Name: name, Namespace: namespace}
		if err := k8sClient.Get(ctx, agentKey, agent); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotAgent{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
				Spec: agentv1.KubeCopilotAgentSpec{
					GitHubTokenSecretRef: agentv1.SecretReference{Name: "gh-token-team"},
				},
			})).To(Succeed())
		}
	}

	// ──────────────────────────────────────────────────────────────────────
	// Valid team: coordinator + all members exist
	// ──────────────────────────────────────────────────────────────────────
	Context("When all agents exist", func() {
		const teamName = "team-valid"
		ctx := context.Background()

		teamKey := types.NamespacedName{Name: teamName, Namespace: namespace}

		BeforeEach(func() {
			ensureAgent(ctx, "coord-agent")
			ensureAgent(ctx, "member-a")
			ensureAgent(ctx, "member-b")

			team := &agentv1.KubeCopilotAgentTeam{}
			if err := k8sClient.Get(ctx, teamKey, team); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotAgentTeam{
					ObjectMeta: metav1.ObjectMeta{Name: teamName, Namespace: namespace},
					Spec: agentv1.KubeCopilotAgentTeamSpec{
						Coordinator: "coord-agent",
						Members: []agentv1.TeamMember{
							{Name: "member-a", Role: "net-expert"},
							{Name: "member-b", Role: "sec-auditor"},
						},
						Strategy: "sequential",
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			team := &agentv1.KubeCopilotAgentTeam{}
			if err := k8sClient.Get(ctx, teamKey, team); err == nil {
				_ = k8sClient.Delete(ctx, team)
			}
		})

		It("should set Phase to Active and MemberCount to 2", func() {
			reconciler := &KubeCopilotAgentTeamReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: teamKey})
			Expect(err).NotTo(HaveOccurred())

			team := &agentv1.KubeCopilotAgentTeam{}
			Expect(k8sClient.Get(ctx, teamKey, team)).To(Succeed())
			Expect(team.Status.Phase).To(Equal("Active"))
			Expect(team.Status.MemberCount).To(Equal(2))
		})

		It("should update the coordinator's delegateTo", func() {
			reconciler := &KubeCopilotAgentTeamReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: teamKey})
			Expect(err).NotTo(HaveOccurred())

			coordinator := &agentv1.KubeCopilotAgent{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "coord-agent", Namespace: namespace,
			}, coordinator)).To(Succeed())
			Expect(coordinator.Spec.DelegateTo).To(ConsistOf("member-a", "member-b"))
		})
	})

	// ──────────────────────────────────────────────────────────────────────
	// Missing coordinator
	// ──────────────────────────────────────────────────────────────────────
	Context("When the coordinator does not exist", func() {
		const teamName = "team-no-coord"
		ctx := context.Background()

		teamKey := types.NamespacedName{Name: teamName, Namespace: namespace}

		BeforeEach(func() {
			ensureAgent(ctx, "member-c")

			team := &agentv1.KubeCopilotAgentTeam{}
			if err := k8sClient.Get(ctx, teamKey, team); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotAgentTeam{
					ObjectMeta: metav1.ObjectMeta{Name: teamName, Namespace: namespace},
					Spec: agentv1.KubeCopilotAgentTeamSpec{
						Coordinator: "nonexistent-coord",
						Members: []agentv1.TeamMember{
							{Name: "member-c", Role: "storage"},
						},
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			team := &agentv1.KubeCopilotAgentTeam{}
			if err := k8sClient.Get(ctx, teamKey, team); err == nil {
				_ = k8sClient.Delete(ctx, team)
			}
		})

		It("should set Phase to Error", func() {
			reconciler := &KubeCopilotAgentTeamReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: teamKey})
			Expect(err).NotTo(HaveOccurred())

			team := &agentv1.KubeCopilotAgentTeam{}
			Expect(k8sClient.Get(ctx, teamKey, team)).To(Succeed())
			Expect(team.Status.Phase).To(Equal("Error"))
		})
	})

	// ──────────────────────────────────────────────────────────────────────
	// Missing member
	// ──────────────────────────────────────────────────────────────────────
	Context("When a member does not exist", func() {
		const teamName = "team-no-member"
		ctx := context.Background()

		teamKey := types.NamespacedName{Name: teamName, Namespace: namespace}

		BeforeEach(func() {
			ensureAgent(ctx, "coord-agent-2")

			team := &agentv1.KubeCopilotAgentTeam{}
			if err := k8sClient.Get(ctx, teamKey, team); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotAgentTeam{
					ObjectMeta: metav1.ObjectMeta{Name: teamName, Namespace: namespace},
					Spec: agentv1.KubeCopilotAgentTeamSpec{
						Coordinator: "coord-agent-2",
						Members: []agentv1.TeamMember{
							{Name: "ghost-member", Role: "unknown"},
						},
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			team := &agentv1.KubeCopilotAgentTeam{}
			if err := k8sClient.Get(ctx, teamKey, team); err == nil {
				_ = k8sClient.Delete(ctx, team)
			}
		})

		It("should set Phase to Error", func() {
			reconciler := &KubeCopilotAgentTeamReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{NamespacedName: teamKey})
			Expect(err).NotTo(HaveOccurred())

			team := &agentv1.KubeCopilotAgentTeam{}
			Expect(k8sClient.Get(ctx, teamKey, team)).To(Succeed())
			Expect(team.Status.Phase).To(Equal("Error"))
			Expect(team.Status.MemberCount).To(Equal(0))
		})
	})
})
