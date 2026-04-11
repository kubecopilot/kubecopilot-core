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
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var _ = Describe("KubeCopilotToolServer Controller", func() {

	Context("When reconciling a resource", func() {
		const resourceName = "test-toolserver"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KubeCopilotToolServer")
			toolServer := &agentv1.KubeCopilotToolServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, toolServer); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotToolServer{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotToolServerSpec{
						URL:       "http://mcp-k8s-server:8080/sse",
						Transport: "sse",
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &agentv1.KubeCopilotToolServer{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should set Phase to Available on creation", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KubeCopilotToolServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the tool server status is Available")
			toolServer := &agentv1.KubeCopilotToolServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, toolServer)).To(Succeed())
			Expect(toolServer.Status.Phase).To(Equal("Available"))
		})

		It("should set the Ready condition to True", func() {
			controllerReconciler := &KubeCopilotToolServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			toolServer := &agentv1.KubeCopilotToolServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, toolServer)).To(Succeed())
			Expect(toolServer.Status.Conditions).To(HaveLen(1))
			Expect(toolServer.Status.Conditions[0].Type).To(Equal("Ready"))
			Expect(toolServer.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(toolServer.Status.Conditions[0].Reason).To(Equal("Reconciled"))
		})

		It("should set the LastChecked timestamp", func() {
			controllerReconciler := &KubeCopilotToolServerReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			toolServer := &agentv1.KubeCopilotToolServer{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, toolServer)).To(Succeed())
			Expect(toolServer.Status.LastChecked.IsZero()).To(BeFalse())
		})
	})
})
