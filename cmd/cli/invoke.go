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
	"sort"
	"time"

	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var (
	invokeAgent     string
	invokeSessionID string
	invokeTimeout   time.Duration
)

var invokeCmd = &cobra.Command{
	Use:   "invoke <message>",
	Short: "Send a message to a KubeCopilotAgent and stream the response",
	Long: `Create a KubeCopilotSend CR with the given message and stream response
chunks (KubeCopilotChunk) to stdout in real-time.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		message := args[0]

		c, err := getClient()
		if err != nil {
			return fmt.Errorf("failed to create client: %w", err)
		}

		// Create the Send CR.
		send := &agentv1.KubeCopilotSend{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: fmt.Sprintf("%s-send-", invokeAgent),
				Namespace:    namespace,
			},
			Spec: agentv1.KubeCopilotSendSpec{
				AgentRef:  invokeAgent,
				Message:   message,
				SessionID: invokeSessionID,
			},
		}
		ctx, cancel := context.WithTimeout(context.Background(), invokeTimeout)
		defer cancel()

		if err := c.Create(ctx, send); err != nil {
			return fmt.Errorf("failed to create send: %w", err)
		}
		fmt.Fprintf(os.Stderr, "Created send/%s — streaming response…\n", send.Name)

		return streamChunks(ctx, c, send.Name)
	},
}

// streamChunks watches KubeCopilotChunk resources and prints them ordered by
// sequence. It stops once the corresponding KubeCopilotSend reaches a terminal
// phase (Done or Error).
func streamChunks(ctx context.Context, c client.Client, sendName string) error {
	printed := 0
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for response")
		case <-ticker.C:
			// Fetch chunks matching this send.
			var chunkList agentv1.KubeCopilotChunkList
			if err := c.List(ctx, &chunkList, client.InNamespace(namespace),
				client.MatchingFields{"spec.sendRef": sendName}); err != nil {
				// Field selector may not be indexed; fall back to filtering.
				if err := c.List(ctx, &chunkList, client.InNamespace(namespace)); err != nil {
					return fmt.Errorf("failed to list chunks: %w", err)
				}
			}

			// Filter and sort chunks for our send.
			var relevant []agentv1.KubeCopilotChunk
			for i := range chunkList.Items {
				if chunkList.Items[i].Spec.SendRef == sendName {
					relevant = append(relevant, chunkList.Items[i])
				}
			}
			sort.Slice(relevant, func(i, j int) bool {
				return relevant[i].Spec.Sequence < relevant[j].Spec.Sequence
			})

			// Print new chunks.
			for _, chunk := range relevant {
				if chunk.Spec.Sequence < printed {
					continue
				}
				printChunk(chunk)
				printed = chunk.Spec.Sequence + 1
			}

			// Check send status.
			var send agentv1.KubeCopilotSend
			if err := c.Get(ctx, client.ObjectKey{Namespace: namespace, Name: sendName}, &send); err == nil {
				switch send.Status.Phase {
				case "Done":
					return nil
				case "Error":
					return fmt.Errorf("agent returned error: %s", send.Status.ErrorMessage)
				}
			}
		}
	}
}

// printChunk outputs a single chunk to stdout.
func printChunk(chunk agentv1.KubeCopilotChunk) {
	switch chunk.Spec.ChunkType {
	case "thinking":
		_, _ = fmt.Fprintf(os.Stderr, "[thinking] %s\n", chunk.Spec.Content)
	case "tool_call":
		_, _ = fmt.Fprintf(os.Stderr, "[tool] %s\n", chunk.Spec.Content)
	case "tool_result":
		_, _ = fmt.Fprintf(os.Stderr, "[result] %s\n", chunk.Spec.Content)
	case "error":
		_, _ = fmt.Fprintf(os.Stderr, "[error] %s\n", chunk.Spec.Content)
	case "info":
		_, _ = fmt.Fprintf(os.Stderr, "[info] %s\n", chunk.Spec.Content)
	default:
		_, _ = fmt.Fprint(os.Stdout, chunk.Spec.Content)
	}
}

func init() {
	invokeCmd.Flags().StringVar(&invokeAgent, "agent", "", "name of the KubeCopilotAgent to invoke (required)")
	invokeCmd.Flags().StringVar(&invokeSessionID, "session", "", "session ID for multi-turn conversation")
	invokeCmd.Flags().DurationVar(&invokeTimeout, "timeout", 5*time.Minute, "maximum time to wait for response")
	_ = invokeCmd.MarkFlagRequired("agent")
	rootCmd.AddCommand(invokeCmd)
}
