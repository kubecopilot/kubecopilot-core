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

// Package webhook implements the operator-side HTTP webhook server that receives
// async responses from agent containers and creates KubeCopilotResponse objects.
package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

var log = logf.Log.WithName("webhook-server")

// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotchunks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotchunks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotresponses,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotnotifications,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=kubecopilot.io,resources=kubecopilotnotifications/status,verbs=get;update;patch

// ResponsePayload is the JSON body the agent POSTs when a queued response is ready.
type ResponsePayload struct {
	QueueID   string `json:"queue_id"`
	SessionID string `json:"session_id"`
	Prompt    string `json:"prompt"`
	Response  string `json:"response"`
	SendRef   string `json:"send_ref,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	AgentRef  string `json:"agent_ref,omitempty"`
}

// Server is a lightweight HTTP server that listens for agent webhook calls.
type Server struct {
	k8sClient client.Client
	addr      string
}

// New creates a new webhook Server.
func New(k8sClient client.Client, addr string) *Server {
	return &Server{k8sClient: k8sClient, addr: addr}
}

// Start runs the HTTP server. It blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/response", s.handleResponse)
	mux.HandleFunc("/chunk", s.handleChunk)
	mux.HandleFunc("/notification", s.handleNotification)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:    s.addr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()

	log.Info("Starting webhook server", "addr", s.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("webhook server failed: %w", err)
	}
	return nil
}

func (s *Server) handleResponse(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload ResponsePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Error(err, "failed to decode webhook payload")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	namespace := payload.Namespace
	if namespace == "" {
		namespace = "default"
	}

	// Build a unique name for the KubeCopilotResponse from the queue_id (truncated).
	queueShort := payload.QueueID
	if len(queueShort) > 8 {
		queueShort = queueShort[:8]
	}
	name := fmt.Sprintf("resp-%s", queueShort)

	now := metav1.Now()
	resp := &agentv1.KubeCopilotResponse{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"kubecopilot.io/session-id": payload.SessionID,
				"kubecopilot.io/agent-ref":  payload.AgentRef,
				"kubecopilot.io/send-ref":   payload.SendRef,
			},
		},
		Spec: agentv1.KubeCopilotResponseSpec{
			AgentRef:  payload.AgentRef,
			SessionID: payload.SessionID,
			Prompt:    payload.Prompt,
			Response:  payload.Response,
			SendRef:   payload.SendRef,
		},
	}

	ctx := context.Background()
	if err := s.k8sClient.Create(ctx, resp); err != nil {
		log.Error(err, "failed to create KubeCopilotResponse", "name", name, "namespace", namespace)
		http.Error(w, "failed to create response object", http.StatusInternalServerError)
		return
	}

	// Stamp the createdAt status
	resp.Status.CreatedAt = &now
	_ = s.k8sClient.Status().Update(ctx, resp)

	log.Info("Created KubeCopilotResponse", "name", name, "namespace", namespace, "sendRef", payload.SendRef)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"name": name, "namespace": namespace})
}

// ChunkPayload is the JSON body the agent POSTs for each streaming chunk.
type ChunkPayload struct {
	SendRef   string `json:"send_ref"`
	SessionID string `json:"session_id,omitempty"`
	AgentRef  string `json:"agent_ref,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Sequence  int    `json:"sequence"`
	ChunkType string `json:"chunk_type"`
	Content   string `json:"content"`
}

func (s *Server) handleChunk(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload ChunkPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Error(err, "failed to decode chunk payload")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	namespace := payload.Namespace
	if namespace == "" {
		namespace = "default"
	}

	sendShort := payload.SendRef
	if len(sendShort) > 12 {
		sendShort = sendShort[len(sendShort)-12:]
	}
	name := fmt.Sprintf("chunk-%s-%04d", sendShort, payload.Sequence)

	chunk := &agentv1.KubeCopilotChunk{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"kubecopilot.io/session-id": payload.SessionID,
				"kubecopilot.io/agent-ref":  payload.AgentRef,
				"kubecopilot.io/send-ref":   payload.SendRef,
			},
		},
		Spec: agentv1.KubeCopilotChunkSpec{
			AgentRef:  payload.AgentRef,
			SessionID: payload.SessionID,
			SendRef:   payload.SendRef,
			Sequence:  payload.Sequence,
			ChunkType: payload.ChunkType,
			Content:   payload.Content,
		},
	}

	ctx := context.Background()
	if err := s.k8sClient.Create(ctx, chunk); err != nil {
		log.Error(err, "failed to create KubeCopilotChunk", "name", name)
		http.Error(w, "failed to create chunk", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusCreated)
}

// NotificationPayload is the JSON body the agent POSTs for one-way notifications.
type NotificationPayload struct {
	SessionID        string `json:"session_id"`
	AgentRef         string `json:"agent_ref,omitempty"`
	Namespace        string `json:"namespace,omitempty"`
	Message          string `json:"message"`
	NotificationType string `json:"notification_type,omitempty"`
	Title            string `json:"title,omitempty"`
	TaskRef          string `json:"task_ref,omitempty"`
}

func (s *Server) handleNotification(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var payload NotificationPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		log.Error(err, "Failed to decode notification payload")
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}

	if payload.SessionID == "" || payload.Message == "" {
		http.Error(w, "session_id and message are required", http.StatusBadRequest)
		return
	}

	namespace := payload.Namespace
	if namespace == "" {
		namespace = "default"
	}

	notifType := payload.NotificationType
	if notifType == "" {
		notifType = "info"
	}

	// Validate notification type
	validTypes := map[string]bool{"info": true, "success": true, "warning": true, "error": true}
	if !validTypes[notifType] {
		http.Error(w, "notification_type must be one of: info, success, warning, error", http.StatusBadRequest)
		return
	}

	now := metav1.Now()
	name := fmt.Sprintf("notif-%s-%d", payload.SessionID, now.UnixMilli())
	if len(name) > 63 {
		name = name[:63]
	}

	notif := &agentv1.KubeCopilotNotification{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels: map[string]string{
				"kubecopilot.io/session-id": payload.SessionID,
				"kubecopilot.io/agent-ref":  payload.AgentRef,
			},
		},
		Spec: agentv1.KubeCopilotNotificationSpec{
			AgentRef:         payload.AgentRef,
			SessionID:        payload.SessionID,
			Message:          payload.Message,
			NotificationType: notifType,
			Title:            payload.Title,
			TaskRef:          payload.TaskRef,
		},
	}

	ctx := context.Background()
	if err := s.k8sClient.Create(ctx, notif); err != nil {
		log.Error(err, "Failed to create KubeCopilotNotification", "name", name, "namespace", namespace)
		http.Error(w, "failed to create notification object", http.StatusInternalServerError)
		return
	}

	// Stamp the createdAt status
	notif.Status.CreatedAt = &now
	_ = s.k8sClient.Status().Update(ctx, notif)

	log.Info("Created KubeCopilotNotification", "name", name, "namespace", namespace, "sessionID", payload.SessionID)
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"name": name, "namespace": namespace})
}
