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
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var _ = Describe("KubeCopilotAgent Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default", // TODO(user):Modify as needed
		}
		kubecopilotagent := &agentv1.KubeCopilotAgent{}

		BeforeEach(func() {
			By("creating the github-token secret required by the agent spec")
			secret := &corev1.Secret{}
			secretName := types.NamespacedName{Name: "github-token-test", Namespace: "default"}
			err := k8sClient.Get(ctx, secretName, secret)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "github-token-test",
						Namespace: "default",
					},
					StringData: map[string]string{
						"GITHUB_TOKEN": "test-token",
					},
				})).To(Succeed())
			}

			By("creating the custom resource for the Kind KubeCopilotAgent")
			err = k8sClient.Get(ctx, typeNamespacedName, kubecopilotagent)
			if err != nil && errors.IsNotFound(err) {
				resource := &agentv1.KubeCopilotAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotAgentSpec{
						GitHubTokenSecretRef: agentv1.SecretReference{
							Name: "github-token-test",
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &agentv1.KubeCopilotAgent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance KubeCopilotAgent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KubeCopilotAgentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
		})
	})

	Context("When reconciling a resource with RBAC configuration", func() {
		const resourceName = "test-rbac-agent"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the github-token secret required by the agent spec")
			secret := &corev1.Secret{}
			secretName := types.NamespacedName{Name: "github-token-rbac", Namespace: "default"}
			err := k8sClient.Get(ctx, secretName, secret)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "github-token-rbac",
						Namespace: "default",
					},
					StringData: map[string]string{
						"GITHUB_TOKEN": "test-token",
					},
				})).To(Succeed())
			}

			By("creating the KubeCopilotAgent with RBAC configuration")
			agent := &agentv1.KubeCopilotAgent{}
			err = k8sClient.Get(ctx, typeNamespacedName, agent)
			if err != nil && errors.IsNotFound(err) {
				resource := &agentv1.KubeCopilotAgent{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotAgentSpec{
						GitHubTokenSecretRef: agentv1.SecretReference{
							Name: "github-token-rbac",
						},
						RBAC: &agentv1.AgentRBAC{
							Rules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"pods", "services"},
									Verbs:     []string{"get", "list", "watch"},
								},
							},
							ClusterRules: []rbacv1.PolicyRule{
								{
									APIGroups: []string{""},
									Resources: []string{"namespaces"},
									Verbs:     []string{"get", "list"},
								},
							},
						},
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &agentv1.KubeCopilotAgent{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Removing the finalizer if present so the agent can be deleted")
			if len(resource.Finalizers) > 0 {
				resource.Finalizers = nil
				Expect(k8sClient.Update(ctx, resource)).To(Succeed())
			}

			By("Cleanup the specific resource instance KubeCopilotAgent")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			By("Cleanup cluster-scoped RBAC resources")
			clusterRoleName := "default-" + resourceName + "-clusterrole"
			cr := &rbacv1.ClusterRole{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterRoleName}, cr); err == nil {
				_ = k8sClient.Delete(ctx, cr)
			}
			crb := &rbacv1.ClusterRoleBinding{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: clusterRoleName + "-binding"}, crb); err == nil {
				_ = k8sClient.Delete(ctx, crb)
			}
		})

		It("should create ServiceAccount, Role, RoleBinding, and kubeconfig Secret", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KubeCopilotAgentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the ServiceAccount was created")
			sa := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-sa",
				Namespace: "default",
			}, sa)).To(Succeed())

			By("Verifying the Role was created with correct rules")
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-role",
				Namespace: "default",
			}, role)).To(Succeed())
			Expect(role.Rules).To(HaveLen(1))
			Expect(role.Rules[0].Resources).To(ContainElements("pods", "services"))
			Expect(role.Rules[0].Verbs).To(ContainElements("get", "list", "watch"))

			By("Verifying the RoleBinding was created")
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-role-binding",
				Namespace: "default",
			}, rb)).To(Succeed())
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Name).To(Equal(resourceName + "-sa"))
			Expect(rb.RoleRef.Name).To(Equal(resourceName + "-role"))

			By("Verifying the ClusterRole was created")
			cr := &rbacv1.ClusterRole{}
			clusterRoleName := "default-" + resourceName + "-clusterrole"
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: clusterRoleName,
			}, cr)).To(Succeed())
			Expect(cr.Rules).To(HaveLen(1))
			Expect(cr.Rules[0].Resources).To(ContainElement("namespaces"))

			By("Verifying the ClusterRoleBinding was created")
			crb := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: clusterRoleName + "-binding",
			}, crb)).To(Succeed())
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Name).To(Equal(resourceName + "-sa"))

			By("Verifying the kubeconfig Secret was created")
			kubeconfigSecret := &corev1.Secret{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-kubeconfig",
				Namespace: "default",
			}, kubeconfigSecret)).To(Succeed())
			Expect(kubeconfigSecret.Data).To(HaveKey("config"))
		})

		It("should use a custom ServiceAccount name when specified", func() {
			By("Updating the agent with a custom SA name")
			agent := &agentv1.KubeCopilotAgent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, agent)).To(Succeed())
			agent.Spec.RBAC.ServiceAccountName = "my-custom-sa"
			Expect(k8sClient.Update(ctx, agent)).To(Succeed())

			By("Reconciling the updated resource")
			controllerReconciler := &KubeCopilotAgentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the custom-named ServiceAccount was created")
			sa := &corev1.ServiceAccount{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      "my-custom-sa",
				Namespace: "default",
			}, sa)).To(Succeed())

			By("Verifying RoleBinding references the custom ServiceAccount")
			rb := &rbacv1.RoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-role-binding",
				Namespace: "default",
			}, rb)).To(Succeed())
			Expect(rb.Subjects).To(HaveLen(1))
			Expect(rb.Subjects[0].Name).To(Equal("my-custom-sa"))

			By("Verifying ClusterRoleBinding references the custom ServiceAccount")
			clusterRoleName := "default-" + resourceName + "-clusterrole"
			crb := &rbacv1.ClusterRoleBinding{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: clusterRoleName + "-binding",
			}, crb)).To(Succeed())
			Expect(crb.Subjects).To(HaveLen(1))
			Expect(crb.Subjects[0].Name).To(Equal("my-custom-sa"))
		})

		It("should update Role rules when spec changes", func() {
			By("Reconciling the original resource")
			controllerReconciler := &KubeCopilotAgentReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Updating the agent with new rules")
			agent := &agentv1.KubeCopilotAgent{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, agent)).To(Succeed())
			agent.Spec.RBAC.Rules = []rbacv1.PolicyRule{
				{
					APIGroups: []string{""},
					Resources: []string{"pods"},
					Verbs:     []string{"get"},
				},
			}
			Expect(k8sClient.Update(ctx, agent)).To(Succeed())

			By("Reconciling the updated resource")
			_, err = controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying the Role was updated")
			role := &rbacv1.Role{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      resourceName + "-role",
				Namespace: "default",
			}, role)).To(Succeed())
			Expect(role.Rules).To(HaveLen(1))
			Expect(role.Rules[0].Resources).To(Equal([]string{"pods"}))
			Expect(role.Rules[0].Verbs).To(Equal([]string{"get"}))
		})
	})
})
