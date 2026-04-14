# MCP (Model Context Protocol) Integration

## Overview

KubeCopilot supports the [Model Context Protocol (MCP)](https://modelcontextprotocol.io/)
for extensible agent tooling. MCP servers expose tools that agents can discover
and invoke at runtime, allowing you to extend agent capabilities without
rebuilding the operator.

## KubeCopilotToolServer CRD

The `KubeCopilotToolServer` custom resource defines an MCP server endpoint:

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotToolServer
metadata:
  name: k8s-tools
  namespace: kube-copilot-agent
spec:
  # Required: the MCP server endpoint URL.
  url: "http://mcp-k8s-server:8080/sse"

  # Optional: transport type. Defaults to "sse".
  # Supported values: "sse", "streamable-http".
  transport: sse

  # Optional: static headers sent with every MCP request.
  headers:
    X-Custom-Header: "value"

  # Optional: reference a Secret whose keys are added as HTTP headers.
  secretRef:
    name: mcp-auth-secret
```

### Status

The operator sets the following status fields:

| Field            | Description                                      |
|------------------|--------------------------------------------------|
| `phase`          | `Available`, `Unavailable`, or `Error`           |
| `availableTools` | List of tool names discovered from the server    |
| `lastChecked`    | Timestamp of the last reconciliation             |
| `conditions`     | Standard Kubernetes conditions (e.g., `Ready`)   |

## Connecting Tool Servers to Agents

Add `spec.toolServers` to a `KubeCopilotAgent` to connect it to one or more
MCP servers:

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgent
metadata:
  name: my-agent
  namespace: kube-copilot-agent
spec:
  githubTokenSecretRef:
    name: gh-token
  toolServers:
    - k8s-tools
    - custom-tools
```

The operator looks up each referenced `KubeCopilotToolServer` in the same
namespace and passes their configurations to the agent container via the
`MCP_SERVERS` environment variable as a JSON-encoded array:

```json
[
  {"name":"k8s-tools","url":"http://mcp-k8s-server:8080/sse","transport":"sse"},
  {"name":"custom-tools","url":"http://custom-mcp:9090/sse","transport":"sse"}
]
```

## Example: Full Setup

1. Deploy an MCP server (e.g., a Kubernetes tools server):

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: mcp-k8s-server
  namespace: kube-copilot-agent
spec:
  replicas: 1
  selector:
    matchLabels:
      app: mcp-k8s-server
  template:
    metadata:
      labels:
        app: mcp-k8s-server
    spec:
      containers:
        - name: mcp-server
          image: your-registry/mcp-k8s-server:latest
          ports:
            - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: mcp-k8s-server
  namespace: kube-copilot-agent
spec:
  selector:
    app: mcp-k8s-server
  ports:
    - port: 8080
      targetPort: 8080
```

2. Register the MCP server:

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotToolServer
metadata:
  name: k8s-tools
  namespace: kube-copilot-agent
spec:
  url: "http://mcp-k8s-server:8080/sse"
  transport: sse
```

3. Connect the tool server to an agent:

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgent
metadata:
  name: my-agent
  namespace: kube-copilot-agent
spec:
  githubTokenSecretRef:
    name: gh-token
  toolServers:
    - k8s-tools
```

## Transport Types

| Transport          | Description                                           |
|--------------------|-------------------------------------------------------|
| `sse`              | Server-Sent Events (default). Persistent connection.  |
| `streamable-http`  | HTTP-based streaming. Better for load-balanced setups. |
