# KubeCopilot CLI

`kubecopilot` is a command-line tool for installing, managing, and invoking
KubeCopilot AI agents on Kubernetes and OpenShift clusters.

## Installation

### Build from source

```bash
make build-cli
```

The binary is written to `bin/kubecopilot`.

### As a kubectl plugin

Symlink or rename the binary so that `kubectl` discovers it:

```bash
cp bin/kubecopilot /usr/local/bin/kubectl-kubecopilot
kubectl kubecopilot --help
```

## Global Flags

| Flag           | Default                | Description                     |
| -------------- | ---------------------- | ------------------------------- |
| `--kubeconfig` | `~/.kube/config`       | Path to the kubeconfig file     |
| `--namespace`  | `kube-copilot-agent`   | Target namespace                |
| `--context`    |                        | Kubernetes context to use       |

## Commands

### `install` — Install the operator

Apply the KubeCopilot operator manifests from a GitHub release:

```bash
kubecopilot install                  # latest release
kubecopilot install --version v1.0.0 # specific version
```

### `uninstall` — Remove the operator

```bash
kubecopilot uninstall
```

### `agent` — Manage agents

```bash
# List all agents
kubecopilot agent list

# Get details for a specific agent
kubecopilot agent get my-agent

# Create a new agent
kubecopilot agent create my-agent \
  --token-secret github-token \
  --image ghcr.io/my-org/agent:latest \
  --storage-size 5Gi

# Delete an agent
kubecopilot agent delete my-agent
```

### `invoke` — Send a message and stream the response

```bash
# One-shot message
kubecopilot invoke --agent my-agent "List all pods in the cluster"

# Continue an existing session
kubecopilot invoke --agent my-agent --session abc123 "Now delete the crashlooping pod"

# Custom timeout
kubecopilot invoke --agent my-agent --timeout 10m "Run a full security audit"
```

The command creates a `KubeCopilotSend` CR and streams `KubeCopilotChunk`
resources to stdout in real time, ordered by sequence number.

### `session list` — List sessions

```bash
# All sessions
kubecopilot session list

# Filter by agent
kubecopilot session list --agent my-agent
```

### `dashboard` — Port-forward the Web UI

```bash
kubecopilot dashboard              # forward to localhost:3000
kubecopilot dashboard --port 8080  # forward to localhost:8080
```

## Examples

```bash
# Full workflow
kubecopilot install
kubecopilot agent create demo --token-secret my-gh-token
kubecopilot invoke --agent demo "Hello, what can you do?"
kubecopilot session list --agent demo
kubecopilot dashboard
kubecopilot agent delete demo
kubecopilot uninstall
```
