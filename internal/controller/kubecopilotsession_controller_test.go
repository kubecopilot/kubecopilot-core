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
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var _ = Describe("KubeCopilotSession Controller", func() {

	// ──────────────────────────────────────────────────────────────────────
	// Helper: create an agent that the session can reference.
	// ──────────────────────────────────────────────────────────────────────
	createAgentDeps := func(ctx context.Context, ns string) {
		secret := &corev1.Secret{}
		secretKey := types.NamespacedName{Name: "gh-token", Namespace: ns}
		if err := k8sClient.Get(ctx, secretKey, secret); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{Name: "gh-token", Namespace: ns},
				StringData: map[string]string{"GITHUB_TOKEN": "tok"},
			})).To(Succeed())
		}
		agent := &agentv1.KubeCopilotAgent{}
		agentKey := types.NamespacedName{Name: "test-agent", Namespace: ns}
		if err := k8sClient.Get(ctx, agentKey, agent); errors.IsNotFound(err) {
			Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotAgent{
				ObjectMeta: metav1.ObjectMeta{Name: "test-agent", Namespace: ns},
				Spec: agentv1.KubeCopilotAgentSpec{
					GitHubTokenSecretRef: agentv1.SecretReference{Name: "gh-token"},
				},
			})).To(Succeed())
		}
	}

	// ──────────────────────────────────────────────────────────────────────
	// Basic reconciliation
	// ──────────────────────────────────────────────────────────────────────
	Context("When reconciling a resource", func() {
		const resourceName = "session-basic"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			createAgentDeps(ctx, "default")

			By("creating the custom resource for the Kind KubeCopilotSession")
			session := &agentv1.KubeCopilotSession{}
			if err := k8sClient.Get(ctx, typeNamespacedName, session); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotSession{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotSessionSpec{
						TenantID: "tenant-a",
						AgentRef: "test-agent",
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &agentv1.KubeCopilotSession{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the session status is Active with expected namespace")
			session := &agentv1.KubeCopilotSession{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, session)).To(Succeed())
			Expect(session.Status.Phase).To(Equal("Active"))
			Expect(session.Status.Namespace).To(Equal("kc-session-session-basic"))
		})

		It("should create a namespace with tenant labels", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			ns := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "kc-session-session-basic"}, ns)).To(Succeed())
			Expect(ns.Labels).To(HaveKeyWithValue("kubecopilot.io/tenant-id", "tenant-a"))
			Expect(ns.Labels).To(HaveKeyWithValue("kubecopilot.io/session", "session-basic"))
		})

		It("should create a NetworkPolicy for strict isolation", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			np := &networkingv1.NetworkPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "tenant-isolation",
				Namespace: "kc-session-session-basic",
			}, np)).To(Succeed())
			Expect(np.Labels).To(HaveKeyWithValue("kubecopilot.io/tenant-id", "tenant-a"))
			Expect(np.Spec.PolicyTypes).To(ContainElement(networkingv1.PolicyTypeIngress))
		})

		It("should create RBAC resources for the tenant", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			nsName := "kc-session-session-basic"

			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "tenant-session-role", Namespace: nsName,
			}, role)).To(Succeed())
			Expect(role.Labels).To(HaveKeyWithValue("kubecopilot.io/tenant-id", "tenant-a"))

			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "tenant-session-binding", Namespace: nsName,
			}, rb)).To(Succeed())
			Expect(rb.Subjects[0].Name).To(Equal("kubecopilot:tenant:tenant-a"))
		})

		It("should add the finalizer", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			session := &agentv1.KubeCopilotSession{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, session)).To(Succeed())
			Expect(session.Finalizers).To(ContainElement("kubecopilot.io/session-cleanup"))
		})
	})

	// ──────────────────────────────────────────────────────────────────────
	// Session isolation: two sessions must get distinct namespaces
	// ──────────────────────────────────────────────────────────────────────
	Context("Session isolation between tenants", func() {
		ctx := context.Background()

		sessionA := types.NamespacedName{Name: "session-iso-a", Namespace: "default"}
		sessionB := types.NamespacedName{Name: "session-iso-b", Namespace: "default"}

		BeforeEach(func() {
			createAgentDeps(ctx, "default")

			for _, s := range []struct {
				nn       types.NamespacedName
				tenantID string
			}{
				{sessionA, "tenant-x"},
				{sessionB, "tenant-y"},
			} {
				session := &agentv1.KubeCopilotSession{}
				if err := k8sClient.Get(ctx, s.nn, session); errors.IsNotFound(err) {
					Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotSession{
						ObjectMeta: metav1.ObjectMeta{Name: s.nn.Name, Namespace: s.nn.Namespace},
						Spec: agentv1.KubeCopilotSessionSpec{
							TenantID: s.tenantID,
							AgentRef: "test-agent",
						},
					})).To(Succeed())
				}
			}
		})

		AfterEach(func() {
			for _, nn := range []types.NamespacedName{sessionA, sessionB} {
				resource := &agentv1.KubeCopilotSession{}
				if err := k8sClient.Get(ctx, nn, resource); err == nil {
					resource.Finalizers = nil
					_ = k8sClient.Update(ctx, resource)
					_ = k8sClient.Delete(ctx, resource)
				}
			}
		})

		It("should create distinct namespaces for different tenants", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: sessionA})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: sessionB})
			Expect(err).NotTo(HaveOccurred())

			// Verify namespaces are different
			sA := &agentv1.KubeCopilotSession{}
			Expect(k8sClient.Get(ctx, sessionA, sA)).To(Succeed())
			sB := &agentv1.KubeCopilotSession{}
			Expect(k8sClient.Get(ctx, sessionB, sB)).To(Succeed())

			Expect(sA.Status.Namespace).NotTo(Equal(sB.Status.Namespace))
			Expect(sA.Status.Namespace).To(Equal("kc-session-session-iso-a"))
			Expect(sB.Status.Namespace).To(Equal("kc-session-session-iso-b"))

			// Verify tenant labels on namespaces don't leak
			nsA := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sA.Status.Namespace}, nsA)).To(Succeed())
			Expect(nsA.Labels[labelTenantID]).To(Equal("tenant-x"))

			nsB := &corev1.Namespace{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: sB.Status.Namespace}, nsB)).To(Succeed())
			Expect(nsB.Labels[labelTenantID]).To(Equal("tenant-y"))
		})

		It("should create independent NetworkPolicies per session namespace", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: sessionA})
			Expect(err).NotTo(HaveOccurred())
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{NamespacedName: sessionB})
			Expect(err).NotTo(HaveOccurred())

			npA := &networkingv1.NetworkPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "tenant-isolation", Namespace: "kc-session-session-iso-a",
			}, npA)).To(Succeed())
			Expect(npA.Labels[labelTenantID]).To(Equal("tenant-x"))

			npB := &networkingv1.NetworkPolicy{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: "tenant-isolation", Namespace: "kc-session-session-iso-b",
			}, npB)).To(Succeed())
			Expect(npB.Labels[labelTenantID]).To(Equal("tenant-y"))
		})
	})

	// ──────────────────────────────────────────────────────────────────────
	// IsolationLevel=none should skip NetworkPolicy
	// ──────────────────────────────────────────────────────────────────────
	Context("When isolationLevel is none", func() {
		const resourceName = "session-no-iso"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			createAgentDeps(ctx, "default")
			session := &agentv1.KubeCopilotSession{}
			if err := k8sClient.Get(ctx, typeNamespacedName, session); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotSession{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: agentv1.KubeCopilotSessionSpec{
						TenantID:       "tenant-none",
						AgentRef:       "test-agent",
						IsolationLevel: "none",
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &agentv1.KubeCopilotSession{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should not create a NetworkPolicy", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			np := &networkingv1.NetworkPolicy{}
			err = k8sClient.Get(ctx, types.NamespacedName{
				Name: "tenant-isolation", Namespace: "kc-session-session-no-iso",
			}, np)
			Expect(errors.IsNotFound(err)).To(BeTrue())
		})
	})

	// ──────────────────────────────────────────────────────────────────────
	// Error: referenced agent does not exist
	// ──────────────────────────────────────────────────────────────────────
	Context("When the referenced agent does not exist", func() {
		const resourceName = "session-no-agent"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			session := &agentv1.KubeCopilotSession{}
			if err := k8sClient.Get(ctx, typeNamespacedName, session); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotSession{
					ObjectMeta: metav1.ObjectMeta{Name: resourceName, Namespace: "default"},
					Spec: agentv1.KubeCopilotSessionSpec{
						TenantID: "tenant-err",
						AgentRef: "does-not-exist",
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &agentv1.KubeCopilotSession{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				resource.Finalizers = nil
				_ = k8sClient.Update(ctx, resource)
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should set phase to Error", func() {
			controllerReconciler := &KubeCopilotSessionReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			session := &agentv1.KubeCopilotSession{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, session)).To(Succeed())
			Expect(session.Status.Phase).To(Equal("Error"))
		})
	})
})
