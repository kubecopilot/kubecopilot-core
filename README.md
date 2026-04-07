# kube-copilot-agent

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.20+-326CE5?logo=kubernetes&logoColor=white)](https://kubernetes.io)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)

**A Kubernetes operator that deploys AI agents inside your cluster, controlled entirely through CRDs.**

kube-copilot-agent provides a pluggable framework for running AI agents as Kubernetes-native workloads. The operator is engine-agnostic — it ships with a [GitHub Copilot SDK](docs/agent-server.md#github-copilot-sdk-default-engine) implementation out of the box and can be extended with other AI backends (e.g., [Claude Code](docs/agent-server.md#creating-a-new-agent-image-eg-claude-code)) by swapping the agent server container image. Users interact with agents by creating Kubernetes resources — no direct pod access required.

> [!WARNING]
> **Disclaimer:** This project is experimental and has not been tested in a production or live environment. It may contain bugs, security vulnerabilities, or incomplete features. Running AI agents with cluster access carries inherent risks — agents may execute unintended commands or access sensitive resources. **Use at your own risk.** Review all manifests, RBAC rules, and agent instructions carefully before deploying in any environment you care about.

---

## Table of Contents

- [Features](#features)
- [Screenshots](#screenshots)
- [Architecture](#architecture) · [full docs →](docs/architecture.md)
- [Quick Start](#quick-start)
- [Installation](#installation) · [full docs →](docs/installation.md)
- [Usage](#usage) · [full docs →](docs/usage.md)
- [Configuration](#configuration) · [full docs →](docs/configuration.md)
- [Agent Server Container](#agent-server-container) · [full docs →](docs/agent-server.md)
- [Development](#development) · [full docs →](docs/development.md)
- [Contributing](#contributing)
- [Uninstall](#uninstall)
- [License](#license)

---

## Features

- **Pluggable agent engines** — swap the AI backend by changing the container image in your `KubeCopilotAgent` CR
- **Multi-turn conversations** with session continuity
- **Real-time streaming** of agent activity via `KubeCopilotChunk` CRDs
- **Custom skills** loaded from a ConfigMap or managed at runtime via the UI
- **Custom instructions** via an `AGENT.md` ConfigMap or editable live
- **Custom agents** — define inline agent personas with specific prompts and tool sets
- **Dynamic model selection** — switch models at runtime without redeploying
- **BYOK (Bring Your Own Key)** — use an external OpenAI-compatible or Azure OpenAI provider, with API keys stored securely in Kubernetes Secrets
- **Cancellation** of in-flight requests
- **Web UI** with a settings panel for chatting with agents, browsing session history, and configuring agent behaviour at runtime
- **OpenShift Console Plugin** — embed the UI directly inside the OpenShift Web Console

See [Agent Server Container](#agent-server-container) for the full pluggable architecture and how to add new engines.

---

## Screenshots

<details>
<summary><strong>Main Chat Interface</strong></summary>

![Main UI](docs/screenshots/main-ui.png)

</details>

<details>
<summary><strong>Settings — Model Selection</strong></summary>

![Model Selection](docs/screenshots/settings-model.png)

</details>

<details>
<summary><strong>Settings — Instructions Editor</strong></summary>

![Instructions Editor](docs/screenshots/settings-instructions.png)

</details>

<details>
<summary><strong>Settings — Skills Management</strong></summary>

![Skills Management](docs/screenshots/settings-skills.png)

</details>

<details>
<summary><strong>Settings — Custom Agents</strong></summary>

![Custom Agents](docs/screenshots/settings-agents.png)

</details>

<details>
<summary><strong>Settings — BYOK Provider Configuration</strong></summary>

![BYOK Configuration](docs/screenshots/settings-byok.png)

</details>

---

## Architecture

The operator reconciles CRDs (`KubeCopilotSend`, `KubeCopilotChunk`, `KubeCopilotResponse`, `KubeCopilotCancel`) and delegates work to a pluggable agent server pod. The Web UI creates CRs and streams results back to the user via SSE.

For detailed architecture diagrams and CRD descriptions, see **[Architecture](docs/architecture.md)**.

---

## Quick Start

Get up and running in four steps. See [Installation](#installation) for full configuration options.

**1. Install the operator**

```sh
helm upgrade --install kube-copilot-agent ./helm/kube-copilot-agent \
  --namespace kube-copilot-agent --create-namespace
```

**2. Deploy an agent**

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  --set githubToken.value=<your-github-pat>
```

**3. Deploy the Web UI**

```sh
helm upgrade --install kube-copilot-ui ./helm/kube-copilot-ui \
  --namespace kube-copilot-agent
```

**4. Access the UI**

```sh
kubectl port-forward svc/kube-copilot-ui 8080:80 -n kube-copilot-agent
# Open: http://localhost:8080
```

> [!TIP]
> On OpenShift, use `--set route.enabled=true` in step 3 to create a Route instead of port-forwarding.

---

## Installation

There are three Helm charts, meant to be installed in order:

| Chart | Purpose |
|---|---|
| `helm/kube-copilot-agent` | The operator (CRDs + controller) |
| `helm/github-copilot-agent` | A GitHub Copilot agent instance |
| `helm/kube-copilot-ui` | The web UI |


For prerequisites, Helm values, OpenShift Console Plugin setup, and step-by-step instructions, see the **[Installation Guide](docs/installation.md)**.

---

## Usage

Chat with agents via the Web UI or create CRDs directly with kubectl. The UI supports multi-turn conversations, session history, real-time agent activity streaming, and request cancellation.

For kubectl examples and CRD manifests, see the **[Usage Guide](docs/usage.md)**.

---

## Configuration

Customize agent behaviour through skills (bash tool snippets), persistent instructions (`AGENT.md`), and a runtime Settings dialog in the Web UI. Features include dynamic model selection, runtime skill/instruction editing, custom agent personas, and BYOK (Bring Your Own Key) for external OpenAI-compatible providers.

For full configuration options, see **[Configuration](docs/configuration.md)**.

---

## Agent Server Container

The `agent-server-container/` directory contains the pluggable server that bridges the operator with an AI backend. The operator is engine-agnostic — any container implementing the required HTTP API contract (`/health`, `/asyncchat`, `/cancel`) works seamlessly with the full UI, streaming, and cancellation features.

The default engine uses the **GitHub Copilot Python SDK** with persistent JSON-RPC connections and typed streaming events.

For the full API contract, webhook payloads, environment variables, and a step-by-step guide to creating a new engine (e.g., Claude Code), see **[Agent Server Container](docs/agent-server.md)**.

---

## Development

```sh
make install   # install CRDs into current cluster
make run       # run operator locally
make manifests # regenerate CRDs/RBAC after changing API types
make generate  # regenerate DeepCopy methods
make build     # build the operator binary
make test      # run unit tests
```

For the full project structure diagram and directory reference, see the **[Development Guide](docs/development.md)**.

---

## Contributing

Contributions are welcome! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines on setting up your development environment, coding conventions, and how to submit pull requests.

---

## Uninstall

**Via Helm** (recommended):

```sh
helm uninstall kube-copilot-console-plugin --namespace kube-copilot-agent  # if installed
helm uninstall kube-copilot-ui      --namespace kube-copilot-agent
helm uninstall my-agent             --namespace kube-copilot-agent
helm uninstall kube-copilot-agent   --namespace kube-copilot-agent
kubectl delete namespace kube-copilot-agent
```

**Via kustomize** (development/CI):

```sh
kubectl delete -k config/samples/
make undeploy
make uninstall
kubectl delete namespace kube-copilot-agent
```

---

## License

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
