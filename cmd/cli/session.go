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

	"github.com/spf13/cobra"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var sessionAgentFilter string

var sessionCmd = &cobra.Command{
	Use:   "session",
	Short: "Manage KubeCopilotSession resources",
}

var sessionListCmd = &cobra.Command{
	Use:   "list",
	Short: "List KubeCopilotSession resources",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}
		var list agentv1.KubeCopilotSessionList
		if err := c.List(context.Background(), &list, client.InNamespace(namespace)); err != nil {
			return fmt.Errorf("failed to list sessions: %w", err)
		}
		w := newStdoutTabWriterHelper()
		w.Println("NAME\tAGENT\tTENANT\tPHASE\tNAMESPACE\tAGE")
		for i := range list.Items {
			s := &list.Items[i]
			if sessionAgentFilter != "" && s.Spec.AgentRef != sessionAgentFilter {
				continue
			}
			age := formatAge(s.CreationTimestamp.Time)
			w.Printf("%s\t%s\t%s\t%s\t%s\t%s\n",
				s.Name, s.Spec.AgentRef, s.Spec.TenantID,
				s.Status.Phase, s.Status.Namespace, age)
		}
		return w.Flush()
	},
}

func init() {
	sessionListCmd.Flags().StringVar(&sessionAgentFilter, "agent", "", "filter sessions by agent name")
	sessionCmd.AddCommand(sessionListCmd)
	rootCmd.AddCommand(sessionCmd)
}
