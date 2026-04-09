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

package webhook

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	agentv1 "github.com/gfontana/kube-copilot-agent/api/v1"
)

func newTestServer() *Server {
	scheme := runtime.NewScheme()
	_ = agentv1.AddToScheme(scheme)
	fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&agentv1.KubeCopilotNotification{}).Build()
	return New(fakeClient, ":0")
}

func TestHandleNotification_ValidPayload(t *testing.T) {
	s := newTestServer()

	payload := NotificationPayload{
		SessionID:        "test-session-123",
		AgentRef:         "test-agent",
		Namespace:        defaultNamespace,
		Message:          "Node worker-3 is now Ready!",
		NotificationType: "success",
		Title:            "Node Ready",
		TaskRef:          "task-abc",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["namespace"] != defaultNamespace {
		t.Errorf("expected namespace 'default', got %q", resp["namespace"])
	}
}

func TestHandleNotification_DefaultNamespace(t *testing.T) {
	s := newTestServer()

	payload := NotificationPayload{
		SessionID: "test-session",
		AgentRef:  "test-agent",
		Message:   "Task completed",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["namespace"] != defaultNamespace {
		t.Errorf("expected default namespace, got %q", resp["namespace"])
	}
}

func TestHandleNotification_DefaultNotificationType(t *testing.T) {
	s := newTestServer()

	// No notification_type provided — should default to "info"
	payload := NotificationPayload{
		SessionID: "test-session",
		AgentRef:  "test-agent",
		Message:   "Something happened",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected status 201, got %d", w.Code)
	}
}

func TestHandleNotification_MissingSessionID(t *testing.T) {
	s := newTestServer()

	payload := NotificationPayload{
		AgentRef: "test-agent",
		Message:  "Missing session",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing session_id, got %d", w.Code)
	}
}

func TestHandleNotification_MissingMessage(t *testing.T) {
	s := newTestServer()

	payload := NotificationPayload{
		SessionID: "test-session",
		AgentRef:  "test-agent",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing message, got %d", w.Code)
	}
}

func TestHandleNotification_MissingAgentRef(t *testing.T) {
	s := newTestServer()

	payload := NotificationPayload{
		SessionID: "test-session",
		Message:   "Missing agent ref",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing agent_ref, got %d", w.Code)
	}
}

func TestHandleNotification_InvalidNotificationType(t *testing.T) {
	s := newTestServer()

	payload := NotificationPayload{
		SessionID:        "test-session",
		AgentRef:         "test-agent",
		Message:          "Test",
		NotificationType: "invalid",
	}

	body, _ := json.Marshal(payload)
	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader(body))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for invalid notification type, got %d", w.Code)
	}
}

func TestHandleNotification_MethodNotAllowed(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodGet, "/notification", nil)
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("expected status 405, got %d", w.Code)
	}
}

func TestHandleNotification_InvalidJSON(t *testing.T) {
	s := newTestServer()

	req := httptest.NewRequest(http.MethodPost, "/notification", bytes.NewReader([]byte("not json")))
	w := httptest.NewRecorder()

	s.handleNotification(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for bad JSON, got %d", w.Code)
	}
}
