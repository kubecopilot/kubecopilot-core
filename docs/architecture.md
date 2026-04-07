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
| `KubeCopilotSend` | Send a message to an agent; dispatched to the agent server |
| `KubeCopilotResponse` | Final response from the agent (written by operator webhook) |
| `KubeCopilotChunk` | Real-time streaming events (thinking, tool calls, results) |
| `KubeCopilotCancel` | Cancel an in-flight request |
| `KubeCopilotMessage` | Legacy single-turn message CRD |
