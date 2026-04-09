← [Back to README](../README.md)

# Architecture

kube-copilot-agent is built around a Kubernetes operator that manages AI agent pods through CRDs. The operator is engine-agnostic — it communicates with any agent server container that implements the required API contract.

```mermaid
flowchart TB
    subgraph ui["🖥️ Web UI"]
        chat["Chat Panel"]
        settings["Settings Dialog<br/><sub>Model · Instructions · Skills · Agents · BYOK</sub>"]
    end

    subgraph backend["⚙️ Web UI Backend"]
        main["main.py"]
        k8s["k8s_client.py"]
    end

    subgraph operator["🎛️ Operator"]
        ctrl["Controller Manager"]
        webhook["Webhook Server"]
    end

    subgraph agent["🤖 Agent Pod"]
        srv["Agent Server<br/><sub>pluggable engine</sub>"]
        engine["AI Backend<br/><sub>e.g. Copilot SDK · Claude Code</sub>"]
        pvc[("PVC<br/><sub>sessions · skills<br/>instructions · agents</sub>")]
    end

    subgraph crds["📋 Kubernetes CRDs"]
        send["KubeCopilotSend"]
        chunk["KubeCopilotChunk"]
        resp["KubeCopilotResponse"]
        cancel["KubeCopilotCancel"]
        notif["KubeCopilotNotification"]
    end

    secrets[("🔐 K8s Secrets<br/><sub>API tokens · BYOK API keys</sub>")]

    chat -- "/chat/stream" --> main
    settings -- "/api/agents/{ref}/..." --> main
    main --> k8s
    k8s -- "creates CR" --> send
    k8s -- "proxy HTTP" --> srv
    k8s -- "CRUD" --> secrets

    ctrl -- "reconciles" --> send
    ctrl -- "reads" --> secrets
    ctrl -- "POST /asyncchat<br/><sub>+ session_config</sub>" --> srv

    srv -- "delegates" --> engine
    srv -- "reads/writes" --> pvc

    srv -- "POST /chunk" --> webhook
    srv -- "POST /response" --> webhook

    webhook -- "creates" --> chunk
    webhook -- "creates" --> resp
    webhook -- "creates" --> notif

    cancel -. "DELETE /cancel" .-> srv

    style ui fill:#1a1a2e,stroke:#00bcd4,color:#e0e0e0
    style backend fill:#16213e,stroke:#00bcd4,color:#e0e0e0
    style operator fill:#0f3460,stroke:#00bcd4,color:#e0e0e0
    style agent fill:#1a1a2e,stroke:#e94560,color:#e0e0e0
    style crds fill:#16213e,stroke:#ffc107,color:#e0e0e0
```

## Request Flow

```mermaid
sequenceDiagram
    actor User
    participant UI as Web UI
    participant BE as Backend
    participant K8s as Kubernetes API
    participant Ctrl as Controller
    participant Agent as Agent Server
    participant Engine as AI Backend

    User->>UI: Send message
    UI->>BE: POST /chat/stream
    BE->>K8s: Create KubeCopilotSend CR
    Ctrl->>K8s: Watch & reconcile Send
    Ctrl->>K8s: Read Secret (if BYOK)
    Ctrl->>Agent: POST /asyncchat + session_config

    Agent->>Engine: send message

    loop Streaming events
        Engine-->>Agent: event (delta, tool call, etc.)
        Agent-->>Ctrl: POST /chunk (webhook)
        Ctrl-->>K8s: Create KubeCopilotChunk
    end

    Engine-->>Agent: done
    Agent-->>Ctrl: POST /response (webhook)
    Ctrl-->>K8s: Create KubeCopilotResponse
    K8s-->>BE: Watch response
    BE-->>UI: SSE stream
    UI-->>User: Display answer
```

## CRDs

| CRD | Purpose |
|---|---|
| `KubeCopilotAgent` | Declares an agent instance (image, credentials, skills, instructions) |
| `KubeCopilotSession` | Creates an isolated tenant session: dedicated namespace, NetworkPolicy, and RBAC for namespace-per-tenant isolation |
| `KubeCopilotSend` | Send a message to an agent; dispatched to the agent server |
| `KubeCopilotResponse` | Final response from the agent (written by operator webhook) |
| `KubeCopilotChunk` | Real-time streaming events (thinking, tool calls, results) |
| `KubeCopilotCancel` | Cancel an in-flight request |
| `KubeCopilotNotification` | One-way notification pushed by the agent to a user session (e.g. background task completion) |
| `KubeCopilotMessage` | Legacy single-turn message CRD |

### KubeCopilotNotification

`KubeCopilotNotification` CRs are created by the operator webhook when the agent server POSTs a notification (e.g. when a background monitoring task completes). The Web UI polls for new notifications via SSE and displays them as inline bubbles and toast popups.

**Spec fields:**

| Field | Type | Required | Description |
|---|---|---|---|
| `agentRef` | `string` | ✅ | Name of the `KubeCopilotAgent` that produced this notification |
| `sessionID` | `string` | ✅ | Conversation session this notification belongs to |
| `message` | `string` | ✅ | Notification body (supports markdown) |
| `notificationType` | `string` | — | Severity: `info` \| `success` \| `warning` \| `error` (default: `info`) |
| `title` | `string` | — | Short summary shown in toast popups |
| `taskRef` | `string` | — | ID of the background task that triggered this notification |

**Example:**

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotNotification
metadata:
  name: notif-abc123
  namespace: kube-copilot-agent
  labels:
    kubecopilot.io/agent-ref: my-agent
    kubecopilot.io/session-id: session-xyz
spec:
  agentRef: my-agent
  sessionID: session-xyz
  message: "Node **worker-3** is now Ready!"
  notificationType: success
  title: "Background task completed"
  taskRef: task-abc123def456
```
