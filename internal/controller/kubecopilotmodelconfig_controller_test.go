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

var _ = Describe("KubeCopilotModelConfig Controller", func() {

	Context("When reconciling a resource", func() {
		const resourceName = "test-modelconfig"
		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind KubeCopilotModelConfig")
			modelConfig := &agentv1.KubeCopilotModelConfig{}
			if err := k8sClient.Get(ctx, typeNamespacedName, modelConfig); errors.IsNotFound(err) {
				maxTokens := int32(8192)
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotModelConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotModelConfigSpec{
						Provider: "openai",
						Endpoint: "https://api.openai.com/v1",
						SecretRef: &agentv1.SecretReference{
							Name: "openai-api-key",
						},
						Models: []agentv1.ModelEntry{
							{
								Name:               "gpt-4o",
								MaxTokens:          &maxTokens,
								CostPerInputToken:  "0.000005",
								CostPerOutputToken: "0.000015",
							},
						},
						RateLimits: &agentv1.RateLimitSpec{
							RequestsPerMinute: func() *int32 { v := int32(60); return &v }(),
							TokensPerMinute:   func() *int32 { v := int32(90000); return &v }(),
						},
					},
				})).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &agentv1.KubeCopilotModelConfig{}
			if err := k8sClient.Get(ctx, typeNamespacedName, resource); err == nil {
				_ = k8sClient.Delete(ctx, resource)
			}
		})

		It("should set Phase to Available on creation", func() {
			By("Reconciling the created resource")
			controllerReconciler := &KubeCopilotModelConfigReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking the model config status is Available")
			modelConfig := &agentv1.KubeCopilotModelConfig{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, modelConfig)).To(Succeed())
			Expect(modelConfig.Status.Phase).To(Equal("Available"))
		})

		It("should set the Ready condition to True", func() {
			controllerReconciler := &KubeCopilotModelConfigReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			modelConfig := &agentv1.KubeCopilotModelConfig{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, modelConfig)).To(Succeed())
			Expect(modelConfig.Status.Conditions).To(HaveLen(1))
			Expect(modelConfig.Status.Conditions[0].Type).To(Equal("Ready"))
			Expect(modelConfig.Status.Conditions[0].Status).To(Equal(metav1.ConditionTrue))
			Expect(modelConfig.Status.Conditions[0].Reason).To(Equal("Reconciled"))
		})

		It("should set the LastValidated timestamp", func() {
			controllerReconciler := &KubeCopilotModelConfigReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			modelConfig := &agentv1.KubeCopilotModelConfig{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, modelConfig)).To(Succeed())
			Expect(modelConfig.Status.LastValidated).NotTo(BeNil())
			Expect(modelConfig.Status.LastValidated.IsZero()).To(BeFalse())
		})

		It("should configure a fallback reference", func() {
			By("Creating a fallback model config")
			fallbackName := "test-modelconfig-fallback"
			fallback := &agentv1.KubeCopilotModelConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: fallbackName, Namespace: "default"}, fallback); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotModelConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fallbackName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotModelConfigSpec{
						Provider: "ollama",
						Endpoint: "http://ollama:11434",
					},
				})).To(Succeed())
			}

			By("Creating a primary model config with fallback")
			primaryName := "test-modelconfig-with-fallback"
			primary := &agentv1.KubeCopilotModelConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: primaryName, Namespace: "default"}, primary); errors.IsNotFound(err) {
				Expect(k8sClient.Create(ctx, &agentv1.KubeCopilotModelConfig{
					ObjectMeta: metav1.ObjectMeta{
						Name:      primaryName,
						Namespace: "default",
					},
					Spec: agentv1.KubeCopilotModelConfigSpec{
						Provider: "openai",
						Fallback: &agentv1.LocalObjectReference{
							Name: fallbackName,
						},
					},
				})).To(Succeed())
			}

			By("Verifying the fallback reference is stored correctly")
			created := &agentv1.KubeCopilotModelConfig{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: primaryName, Namespace: "default"}, created)).To(Succeed())
			Expect(created.Spec.Fallback).NotTo(BeNil())
			Expect(created.Spec.Fallback.Name).To(Equal(fallbackName))

			_ = k8sClient.Delete(ctx, created)
			fallbackObj := &agentv1.KubeCopilotModelConfig{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: fallbackName, Namespace: "default"}, fallbackObj); err == nil {
				_ = k8sClient.Delete(ctx, fallbackObj)
			}
		})
	})
})
