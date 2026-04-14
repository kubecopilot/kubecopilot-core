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

package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage KubeCopilotAgent resources",
}

// --- list ---

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List KubeCopilotAgent resources",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		var list agentv1.KubeCopilotAgentList
		if err := c.List(context.Background(), &list, client.InNamespace(namespace)); err != nil {
			return fmt.Errorf("failed to list agents: %w", err)
		}
		w := newStdoutTabWriterHelper()
		w.Println("NAME\tPHASE\tAGENT ID\tAGE")
		for i := range list.Items {
			a := &list.Items[i]
			age := formatAge(a.CreationTimestamp.Time)
			w.Printf("%s\t%s\t%s\t%s\n", a.Name, a.Status.Phase, a.Status.AgentID, age)
		}
		return w.Flush()
	},
}

// --- get ---

var agentGetCmd = &cobra.Command{
	Use:   "get <name>",
	Short: "Get details of a KubeCopilotAgent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		var agent agentv1.KubeCopilotAgent
		key := client.ObjectKey{Namespace: namespace, Name: args[0]}
		if err := c.Get(context.Background(), key, &agent); err != nil {
			return fmt.Errorf("failed to get agent %q: %w", args[0], err)
		}
		w := newStdoutTabWriterHelper()
		w.Printf("Name:\t%s\n", agent.Name)
		w.Printf("Namespace:\t%s\n", agent.Namespace)
		w.Printf("Phase:\t%s\n", agent.Status.Phase)
		w.Printf("Agent ID:\t%s\n", agent.Status.AgentID)
		w.Printf("Token Secret:\t%s\n", agent.Spec.GitHubTokenSecretRef.Name)
		if agent.Spec.Image != "" {
			w.Printf("Image:\t%s\n", agent.Spec.Image)
		}
		w.Printf("Storage Size:\t%s\n", agent.Spec.StorageSize)
		if agent.Spec.SkillsConfigMap != "" {
			w.Printf("Skills ConfigMap:\t%s\n", agent.Spec.SkillsConfigMap)
		}
		if agent.Spec.AgentConfigMap != "" {
			w.Printf("Agent ConfigMap:\t%s\n", agent.Spec.AgentConfigMap)
		}
		if agent.Status.ServiceName != "" {
			w.Printf("Service:\t%s\n", agent.Status.ServiceName)
		}
		if len(agent.Status.Conditions) > 0 {
			w.Println("\nConditions:")
			w.Println("  TYPE\tSTATUS\tREASON\tMESSAGE")
			for _, cond := range agent.Status.Conditions {
				w.Printf("  %s\t%s\t%s\t%s\n",
					cond.Type, cond.Status, cond.Reason, cond.Message)
			}
		}
		return w.Flush()
	},
}

// --- create ---

var (
	agentTokenSecret string
	agentImage       string
	agentStorageSize string
	agentSkillsCM    string
	agentAgentCM     string
)

var agentCreateCmd = &cobra.Command{
	Use:   "create <name>",
	Short: "Create a new KubeCopilotAgent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		agent := &agentv1.KubeCopilotAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      args[0],
				Namespace: namespace,
			},
			Spec: agentv1.KubeCopilotAgentSpec{
				GitHubTokenSecretRef: agentv1.SecretReference{Name: agentTokenSecret},
			},
		}
		if agentImage != "" {
			agent.Spec.Image = agentImage
		}
		if agentStorageSize != "" {
			agent.Spec.StorageSize = agentStorageSize
		}
		if agentSkillsCM != "" {
			agent.Spec.SkillsConfigMap = agentSkillsCM
		}
		if agentAgentCM != "" {
			agent.Spec.AgentConfigMap = agentAgentCM
		}
		if err := c.Create(context.Background(), agent); err != nil {
			return fmt.Errorf("failed to create agent: %w", err)
		}
		_, _ = fmt.Fprintf(os.Stdout, "agent/%s created\n", args[0])
		return nil
	},
}

// --- delete ---

var agentDeleteCmd = &cobra.Command{
	Use:   "delete <name>",
	Short: "Delete a KubeCopilotAgent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		agent := &agentv1.KubeCopilotAgent{
			ObjectMeta: metav1.ObjectMeta{
				Name:      args[0],
				Namespace: namespace,
			},
		}
		if err := c.Delete(context.Background(), agent); err != nil {
			return fmt.Errorf("failed to delete agent %q: %w", args[0], err)
		}
		_, _ = fmt.Fprintf(os.Stdout, "agent/%s deleted\n", args[0])
		return nil
	},
}

func init() {
	agentCreateCmd.Flags().StringVar(&agentTokenSecret, "token-secret", "",
		"name of the Secret containing GITHUB_TOKEN (required)")
	agentCreateCmd.Flags().StringVar(&agentImage, "image", "", "override the default agent container image")
	agentCreateCmd.Flags().StringVar(&agentStorageSize, "storage-size", "", "PVC size for session state (default: 1Gi)")
	agentCreateCmd.Flags().StringVar(&agentSkillsCM, "skills-configmap", "", "name of the skills ConfigMap")
	agentCreateCmd.Flags().StringVar(&agentAgentCM, "agent-configmap", "", "name of the agent ConfigMap")
	_ = agentCreateCmd.MarkFlagRequired("token-secret")

	agentCmd.AddCommand(agentListCmd)
	agentCmd.AddCommand(agentGetCmd)
	agentCmd.AddCommand(agentCreateCmd)
	agentCmd.AddCommand(agentDeleteCmd)
	rootCmd.AddCommand(agentCmd)
}
