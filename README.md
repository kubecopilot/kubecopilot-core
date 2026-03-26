# kube-copilot-agent

[![License](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](https://opensource.org/licenses/Apache-2.0)
[![Kubernetes](https://img.shields.io/badge/Kubernetes-1.20+-326CE5?logo=kubernetes&logoColor=white)](https://kubernetes.io)
[![Go](https://img.shields.io/badge/Go-1.24+-00ADD8?logo=go&logoColor=white)](https://go.dev)

**A Kubernetes operator that deploys AI agents inside your cluster, controlled entirely through CRDs.**

kube-copilot-agent provides a pluggable framework for running AI agents as Kubernetes-native workloads. The operator is engine-agnostic — it ships with a [GitHub Copilot SDK](#github-copilot-sdk-default-engine) implementation out of the box and can be extended with other AI backends (e.g., [Claude Code](#creating-a-new-agent-image-eg-claude-code)) by swapping the agent server container image. Users interact with agents by creating Kubernetes resources — no direct pod access required.

> [!WARNING]
> **Disclaimer:** This project is experimental and has not been tested in a production or live environment. It may contain bugs, security vulnerabilities, or incomplete features. Running AI agents with cluster access carries inherent risks — agents may execute unintended commands or access sensitive resources. **Use at your own risk.** Review all manifests, RBAC rules, and agent instructions carefully before deploying in any environment you care about.

---

## Table of Contents

- [Features](#features)
- [Screenshots](#screenshots)
- [Architecture](#architecture)
- [Quick Start](#quick-start)
- [Installation](#installation)
- [Usage](#usage)
- [Configuration](#configuration)
- [Agent Server Container](#agent-server-container)
- [Development](#development)
- [Project Structure](#project-structure)
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

### Request Flow

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

### CRDs

| CRD | Purpose |
|---|---|
| `KubeCopilotAgent` | Declares an agent instance (image, credentials, skills, instructions) |
| `KubeCopilotSend` | Send a message to an agent; dispatched to the agent server |
| `KubeCopilotResponse` | Final response from the agent (written by operator webhook) |
| `KubeCopilotChunk` | Real-time streaming events (thinking, tool calls, results) |
| `KubeCopilotCancel` | Cancel an in-flight request |
| `KubeCopilotMessage` | Legacy single-turn message CRD |

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

### Prerequisites

- kubectl v1.20+
- Helm v3.10+
- Access to a Kubernetes or OpenShift cluster
- A GitHub account with Copilot access
- A GitHub Personal Access Token (PAT) with `copilot` scope

### Step 1 — Install the Operator

```sh
helm upgrade --install kube-copilot-agent ./helm/kube-copilot-agent \
  --namespace kube-copilot-agent \
  --create-namespace
```

If the namespace already exists:

```sh
helm upgrade --install kube-copilot-agent ./helm/kube-copilot-agent \
  --namespace kube-copilot-agent \
  --set createNamespace=false
```

**Key operator values:**

| Value | Default | Description |
|---|---|---|
| `namespace` | `kube-copilot-agent` | Namespace to deploy into |
| `createNamespace` | `true` | Create the namespace as part of the chart |
| `image.repository` | `quay.io/gfontana/kube-copilot-agent` | Operator image |
| `image.tag` | `v1.0` | Operator image tag |
| `image.pullPolicy` | `Always` | Image pull policy |
| `agentImage.repository` | `quay.io/gfontana/kube-github-copilot-agent-server` | Default agent image |
| `agentImage.tag` | `v1.0` | Default agent image tag |
| `replicaCount` | `1` | Operator replicas |
| `installCRDs` | `true` | Install CRDs with the chart |
| `rbac.create` | `true` | Create RBAC resources |
| `leaderElect` | `true` | Enable leader election |

### Step 2 — Create Credentials

Create a secret with your GitHub PAT:

```sh
kubectl create secret generic github-token \
  --from-literal=GITHUB_TOKEN=<your-pat> \
  -n kube-copilot-agent
```

Optionally, provide a kubeconfig so the agent can inspect your cluster:

```sh
kubectl create secret generic cluster-kubeconfig \
  --from-file=config=<path-to-kubeconfig> \
  -n kube-copilot-agent
```

Alternatively, use [ServiceAccount-based permissions](#serviceaccount-based-permissions) to let the operator manage RBAC for you automatically.

### Step 3 — Deploy the GitHub Copilot Agent

The `github-copilot-agent` chart creates the `KubeCopilotAgent` CR, a GitHub token Secret, and ConfigMaps for skills and `AGENT.md`. Default skills (monitor, deploy, troubleshoot) and a SysAdmin persona are included out of the box.

**Minimal install** (uses built-in skills and AGENT.md):

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  --set githubToken.value=<your-pat>
```

**With an existing token secret:**

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  --set githubToken.existingSecret=github-token
```

**With a kubeconfig secret** (so the agent can talk to the cluster):

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  --set githubToken.existingSecret=github-token \
  --set kubeconfigSecretRef=cluster-kubeconfig
```

**Custom skills and AGENT.md** via a values file:

```yaml
# my-agent-values.yaml
name: my-agent
githubToken:
  existingSecret: github-token

kubeconfigSecretRef: cluster-kubeconfig

createSkillsConfigMap: true
skillsContent:
  my-skill.md: |
    ---
    name: my-skill
    description: Does something useful
    ---
    # My Skill
    ...

createAgentConfigMap: true
agentContent:
  AGENT.md: |
    # My Agent
    You are a helpful Kubernetes assistant.
```

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  -f my-agent-values.yaml
```

**Key agent values:**

| Value | Default | Description |
|---|---|---|
| `name` | `github-copilot-agent` | Name of the `KubeCopilotAgent` CR |
| `namespace` | `kube-copilot-agent` | Target namespace |
| `githubToken.value` | `""` | PAT value (creates a new Secret) |
| `githubToken.existingSecret` | `""` | Reference an existing Secret |
| `githubToken.secretKey` | `GITHUB_TOKEN` | Key inside the secret |
| `image` | `""` | Override the agent container image |
| `storageSize` | `1Gi` | PVC size for session history |
| `kubeconfigSecretRef` | `""` | Existing Secret name with a kubeconfig |
| `rbac` | `{}` | RBAC config (SA, rules, clusterRules) — see [ServiceAccount Permissions](#serviceaccount-based-permissions) |
| `createSkillsConfigMap` | `true` | Create a skills ConfigMap from `skillsContent` |
| `skillsConfigMap` | `""` | Reference an existing skills ConfigMap |
| `createAgentConfigMap` | `true` | Create an AGENT.md ConfigMap from `agentContent` |
| `agentConfigMap` | `""` | Reference an existing AGENT.md ConfigMap |

Wait for the agent to become ready:

```sh
kubectl get kubecopilotagent my-agent -n kube-copilot-agent -w
```

### Step 4 — Deploy the Web UI

```sh
helm upgrade --install kube-copilot-ui ./helm/kube-copilot-ui \
  --namespace kube-copilot-agent
```

**On OpenShift** (creates a Route with TLS):

```sh
helm upgrade --install kube-copilot-ui ./helm/kube-copilot-ui \
  --namespace kube-copilot-agent \
  --set route.enabled=true
```

Then get the URL:

```sh
kubectl get route kube-copilot-ui -n kube-copilot-agent -o jsonpath='{.spec.host}'
```

**On plain Kubernetes** (port-forward):

```sh
kubectl port-forward svc/kube-copilot-ui 8080:80 -n kube-copilot-agent
# Open: http://localhost:8080
```

**Key UI values:**

| Value | Default | Description |
|---|---|---|
| `namespace` | `kube-copilot-agent` | Namespace to deploy into |
| `image.repository` | `quay.io/gfontana/kube-copilot-agent-ui` | UI image |
| `image.tag` | `v1.0` | UI image tag |
| `operatorNamespace` | `kube-copilot-agent` | Namespace the UI watches for agents |
| `commandTimeout` | `300` | Seconds to wait for an agent response |
| `imagePullSecret` | `""` | Pull secret name (for private registries) |
| `rbac.create` | `true` | Create Role/RoleBinding |
| `route.enabled` | `false` | Create an OpenShift Route |
| `route.timeout` | `360s` | HAProxy timeout for SSE streams |

### Step 5 — (Optional) Deploy the OpenShift Console Plugin

If you're running on **OpenShift**, you can embed the KubeCopilot UI directly inside the OpenShift Web Console as a [dynamic plugin](https://github.com/openshift/console/tree/main/frontend/packages/console-dynamic-plugin-sdk).

```mermaid
graph LR
    subgraph ocp["OpenShift Console"]
        nav["🧭 Navigation<br/><sub>Home → KubeCopilot AI</sub>"]
        page["�� Plugin Page<br/><sub>React component</sub>"]
    end

    subgraph plugin["Console Plugin Pod"]
        nginx["nginx<br/><sub>serves JS/CSS assets</sub>"]
    end

    subgraph webui["Web UI Pod"]
        fastapi["FastAPI<br/><sub>chat API + static HTML</sub>"]
    end

    nav --> page
    page --> |"iframe ?embedded=true"| fastapi
    ocp --> |"module federation"| nginx

    style ocp fill:#1a1a2e,stroke:#00bcd4,color:#e0e0e0
    style plugin fill:#16213e,stroke:#e94560,color:#e0e0e0
    style webui fill:#0f3460,stroke:#ffc107,color:#e0e0e0
```

**Build the plugin image:**

```sh
cd openshift-console-plugin
podman build -t quay.io/yourorg/kube-copilot-console-plugin:latest .
podman push quay.io/yourorg/kube-copilot-console-plugin:latest
```

**Install via Helm:**

```sh
helm upgrade --install kube-copilot-console-plugin ./helm/kube-copilot-console-plugin \
  --namespace kube-copilot-agent \
  --set plugin.image=quay.io/yourorg/kube-copilot-console-plugin:latest \
  --set webUI.serviceName=kube-copilot-ui \
  --set webUI.servicePort=8000
```

After installation, refresh the OpenShift Console — a new **"KubeCopilot AI"** nav item appears under **Home** in both the Administrator and Developer perspectives.

**How it works:**

1. A `ConsolePlugin` CR registers the plugin with the OpenShift Console operator
2. A post-install Job patches the Console operator config to enable the plugin
3. The plugin page loads the existing Web UI inside an iframe with `?embedded=true`
4. In embedded mode, the Web UI hides its own header and adjusts dimensions to fit the Console content area
5. Theme sync: the plugin forwards OpenShift Console dark/light mode changes to the iframe via `postMessage`

**Key Helm values:**

| Value | Default | Description |
|---|---|---|
| `plugin.image` | *(required)* | Console plugin container image |
| `plugin.name` | `kube-copilot-console-plugin` | Name of the `ConsolePlugin` CR |
| `plugin.replicas` | `2` | Number of plugin pod replicas |
| `plugin.port` | `9443` | HTTPS port for nginx (auto-TLS via serving cert) |
| `webUI.serviceName` | `kube-copilot-ui` | Name of the KubeCopilot Web UI service |
| `webUI.serviceNamespace` | *(release namespace)* | Namespace of the Web UI service |
| `webUI.servicePort` | `8000` | Port of the Web UI service |

---

## Usage

### Via the Web UI

Open the route URL in a browser, select your agent, and start chatting. The UI supports:

- Multi-turn conversations with session history in the sidebar
- **Running Sessions** panel showing in-progress requests
- **Agent Activity** tab showing real-time chunk streaming
- **Stop** button to cancel an in-flight request

### Via kubectl (CRDs directly)

**Send a message:**

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotSend
metadata:
  name: my-question
  namespace: kube-copilot-agent
spec:
  agentRef: github-copilot-agent
  message: "What is the overall health of the cluster?"
  sessionID: ""   # leave empty to start a new session
```

```sh
kubectl apply -f my-question.yaml
```

**Watch real-time activity:**

```sh
kubectl get kubecopilotchunks -n kube-copilot-agent -w
```

**Read the response:**

```sh
kubectl get kubecopilotresponses -n kube-copilot-agent -o yaml
```

**Resume a session:** set `spec.sessionID` to the session ID returned in a previous `KubeCopilotResponse`.

**Cancel a request:**

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotCancel
metadata:
  name: cancel-my-question
  namespace: kube-copilot-agent
spec:
  sendRef: my-question
  agentRef: github-copilot-agent
```

---

## Configuration

### Custom Skills

Skills are bash snippets the agent can invoke as tools. Define them in a ConfigMap with a `skills.md` key:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: copilot-skills
  namespace: kube-copilot-agent
data:
  skills.md: |
    ## Skill: List unhealthy pods
    Lists all pods that are not Running or Completed.
    ```bash
    kubectl get pods -A | grep -vE "Running|Completed"
    ```
```

> [!NOTE]
> See `config/samples/skills-configmap.yaml` for a full example with Kubernetes operations skills.

### Custom Instructions (AGENT.md)

Shape agent behaviour with persistent instructions:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: copilot-agent-md
  namespace: kube-copilot-agent
data:
  AGENT.md: |
    # Agent Instructions
    - Always confirm the current cluster context before acting.
    - Never modify resources in production namespaces (prefixed with `prod-`).
    - Prefer read-only operations unless explicitly asked to make changes.
```

### Dynamic Configuration (Runtime Settings)

The web UI includes a **Settings dialog** that lets you configure agent behaviour at runtime — no pod restart or Helm upgrade needed.

```mermaid
graph LR
    subgraph settings["⚙️ Settings Dialog"]
        A["🧠 Model<br/><sub>Select LLM model</sub>"]
        B["📝 Instructions<br/><sub>Edit copilot-instructions.md</sub>"]
        C["🛠️ Skills<br/><sub>Add/edit/delete SKILL.md</sub>"]
        D["🤖 Custom Agents<br/><sub>Define agent personas</sub>"]
        E["🔑 BYOK<br/><sub>External provider config</sub>"]
    end

    subgraph storage["Storage"]
        PVC[("PVC<br/><sub>instructions · skills<br/>custom-agents.json</sub>")]
        Secret[("K8s Secret<br/><sub>API key</sub>")]
    end

    A --> |"per-request"| SDK["Agent Server"]
    B --> |"PUT /config/instructions"| PVC
    C --> |"PUT /config/skills/{name}"| PVC
    D --> |"PUT /config/agents"| PVC
    E --> |"upsert Secret"| Secret

    style settings fill:#1a1a2e,stroke:#00bcd4,color:#e0e0e0
    style storage fill:#16213e,stroke:#ffc107,color:#e0e0e0
```

#### Model Selection

Switch between available Copilot models at runtime. The UI queries `/models` (backed by `client.list_models()` from the SDK) and sends the chosen model with each request via the `session_config.model` field.

#### Runtime Instructions

Edit the agent's `copilot-instructions.md` file directly from the UI. Changes are written to the PVC and take effect on the next session — no restart needed.

#### Runtime Skills

Create, edit, or delete skills through the UI. Each skill is stored as a `SKILL.md` file under `$COPILOT_HOME/skills/<name>/` on the PVC.

#### Custom Agents

Define inline agent personas with specific prompts and tool restrictions. Stored as `custom-agents.json` on the PVC and loaded into each SDK session.

#### BYOK (Bring Your Own Key)

Configure an external OpenAI-compatible or Azure OpenAI provider:

- **Provider type** and **base URL** are stored in the `KubeCopilotSend` CR's `sessionConfig.provider` field
- **API keys** are stored securely in a Kubernetes Secret and read by the controller at reconciliation time — never persisted in CRDs

```yaml
# Example: KubeCopilotSend with session config
apiVersion: kubecopilot.io/v1
kind: KubeCopilotSend
metadata:
  name: my-question
  namespace: kube-copilot-agent
spec:
  agentRef: github-copilot-agent
  message: "What is the cluster health?"
  sessionConfig:
    model: "gpt-4o"
    provider:
      type: openai
      baseURL: "https://api.openai.com/v1"
      secretRef: my-provider-secret   # K8s Secret with 'api-key' key
```

### Chunk Types (Real-time Streaming)

`KubeCopilotChunk` resources are created as the agent works:

| `chunkType` | Description |
|---|---|
| `thinking` | Agent's internal reasoning |
| `tool_call` | Agent invoking a skill or tool |
| `tool_result` | Result returned from the tool |
| `response` | Final answer text |
| `info` | Processing status (e.g. "Processing: ...") |
| `error` | Error during processing or cancellation |

### ServiceAccount-Based Permissions

By default, agents interact with the cluster through a manually-created `kubeconfigSecretRef`. The **RBAC configuration** option lets the operator manage all of this automatically — creating a dedicated ServiceAccount, RBAC Role/ClusterRole, bindings, and a kubeconfig Secret so each agent runs with **least-privilege** access.

#### How it works

When you set `spec.rbac` in a `KubeCopilotAgent`, the operator:

1. **Creates a ServiceAccount** (name defaults to `<agent-name>-sa` or uses `rbac.serviceAccountName`)
2. **Creates a Role** (namespace-scoped) with the rules you specify in `rbac.rules`
3. **Creates a RoleBinding** to bind the ServiceAccount to that Role
4. **Creates a ClusterRole** (cluster-scoped) with the rules in `rbac.clusterRules` (optional)
5. **Creates a ClusterRoleBinding** to bind the ServiceAccount to that ClusterRole
6. **Generates a kubeconfig Secret** that uses the ServiceAccount token for authentication
7. **Mounts the kubeconfig** into the agent pod at `/copilot/.kube/config`

All resources are owned by the `KubeCopilotAgent` and garbage-collected when it is deleted.

> [!NOTE]
> `spec.rbac` is mutually exclusive with `spec.kubeconfigSecretRef`. Use one or the other.

#### CR Example

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgent
metadata:
  name: my-agent
  namespace: kube-copilot-agent
spec:
  githubTokenSecretRef:
    name: github-token
  rbac:
    serviceAccountName: my-agent-sa   # optional, defaults to "<name>-sa"
    rules:                             # namespace-scoped
      - apiGroups: [""]
        resources: ["pods", "services", "configmaps", "events"]
        verbs: ["get", "list", "watch"]
      - apiGroups: ["apps"]
        resources: ["deployments", "replicasets"]
        verbs: ["get", "list", "watch"]
    clusterRules:                      # cluster-scoped (optional)
      - apiGroups: [""]
        resources: ["namespaces", "nodes"]
        verbs: ["get", "list"]
```

#### Helm Example

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  --set githubToken.value=<your-pat> \
  --set rbac.serviceAccountName=my-agent-sa \
  --set 'rbac.rules[0].apiGroups[0]=""' \
  --set 'rbac.rules[0].resources[0]=pods' \
  --set 'rbac.rules[0].resources[1]=services' \
  --set 'rbac.rules[0].verbs[0]=get' \
  --set 'rbac.rules[0].verbs[1]=list' \
  --set 'rbac.rules[0].verbs[2]=watch'
```

Or use a values file:

```yaml
# my-agent-rbac-values.yaml
githubToken:
  existingSecret: github-token

rbac:
  serviceAccountName: my-agent-sa
  rules:
    - apiGroups: [""]
      resources: ["pods", "services", "configmaps"]
      verbs: ["get", "list", "watch"]
    - apiGroups: ["apps"]
      resources: ["deployments"]
      verbs: ["get", "list", "watch"]
  clusterRules:
    - apiGroups: [""]
      resources: ["namespaces", "nodes"]
      verbs: ["get", "list"]
```

```sh
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  -f my-agent-rbac-values.yaml
```

#### Verifying Permissions

After deploying, verify the agent's effective permissions:

```sh
# Check what the ServiceAccount can do in the agent namespace
kubectl auth can-i --list \
  --as=system:serviceaccount:kube-copilot-agent:my-agent-sa \
  -n kube-copilot-agent

# Check cluster-scoped permissions
kubectl auth can-i list namespaces \
  --as=system:serviceaccount:kube-copilot-agent:my-agent-sa

# Inspect the generated RBAC resources
kubectl get sa,role,rolebinding -n kube-copilot-agent -l app.kubernetes.io/managed-by=kube-copilot-agent
kubectl get clusterrole,clusterrolebinding | grep my-agent
```

---

## Agent Server Container

The `agent-server-container/` directory contains the pluggable server that bridges the Kubernetes operator with an AI backend running inside the agent pod. Each subdirectory implements the server for a **specific AI engine**. The operator doesn't care which engine is inside — as long as the container implements the [API contract](#api-contract) below, everything works.

```
agent-server-container/
  github-copilot/       ← GitHub Copilot SDK engine (default, shipped)
    server.py           ← FastAPI server (SDK-backed)
    entrypoint.sh       ← Container entrypoint (auth setup, skill staging)
    Containerfile       ← Container image definition
  claude-code/          ← (example) Claude Code engine — add your own!
```

### API Contract

Every agent server image **must** expose at minimum these HTTP endpoints:

| Endpoint | Method | Description |
|---|---|---|
| `/health` | GET | Liveness probe — return `{"status":"ok"}` |
| `/asyncchat` | POST | Enqueue a message (with optional `session_config`); returns `{"queue_id": "..."}` |
| `/cancel/{queue_id}` | DELETE | Cancel/disconnect the in-flight request for a given queue item |

The server **must** POST streaming chunks and final responses back to `$WEBHOOK_URL` (injected by the operator). Optional endpoints for richer functionality:

| Endpoint | Method | Description |
|---|---|---|
| `/chat` | POST | Synchronous chat — blocks until the agent responds |
| `/models` | GET | List available models (enables model picker in the UI) |
| `/config/instructions` | GET/PUT | Manage instructions file on the PVC |
| `/config/skills` | GET | List all skills on the PVC |
| `/config/skills/{name}` | GET/PUT/DELETE | Manage individual skills |
| `/config/agents` | GET/PUT | Manage custom agent definitions on the PVC |

### GitHub Copilot SDK (Default Engine)

The GitHub Copilot implementation uses the **GitHub Copilot Python SDK** (`CopilotClient`) to communicate with the Copilot CLI running in server mode via JSON-RPC. This replaces the previous subprocess-per-request approach with a persistent connection, proper session management, and typed streaming events.

```mermaid
sequenceDiagram
    participant Op as Operator
    participant Shim as Agent Server (SDK)
    participant CLI as Copilot CLI (server mode)

    Note over Shim,CLI: Persistent JSON-RPC connection

    Op->>Shim: POST /asyncchat + session_config
    Shim-->>Op: { queue_id }
    Shim->>CLI: create_session(opts) / resume_session()
    Shim->>CLI: session.send(message)

    loop SDK streaming events
        CLI-->>Shim: assistant.message_delta
        Shim-->>Op: POST /chunk (thinking/tool_call/response)
        CLI-->>Shim: tool.execution_start
        Shim-->>Op: POST /chunk (tool_call)
        CLI-->>Shim: tool.execution_complete
        Shim-->>Op: POST /chunk (tool_result)
    end

    CLI-->>Shim: session.idle
    Shim->>CLI: session.disconnect()
    Shim-->>Op: POST /response (final answer)

    Note over Op,Shim: Cancellation
    Op->>Shim: DELETE /cancel/{queue_id}
    Shim->>CLI: session.disconnect()
```

**Key SDK features used:**

- `CopilotClient(SubprocessConfig)` — singleton managing the CLI in server mode
- `PermissionHandler.approve_all` — auto-approve tool executions
- `asyncio.Semaphore(3)` — bounded concurrency for parallel sessions
- `client.list_models()` — query available models for the settings UI
- `session.on(callback)` — typed event streaming for real-time chunks

### Webhook Payloads

Every agent server must POST these payloads to `$WEBHOOK_URL` (injected by the operator).

**Chunk** (streamed during execution):

```json
{
  "queue_id": "<uuid>",
  "seq": 1,
  "type": "thinking|tool_call|tool_result|response|info|error",
  "content": "...",
  "session_id": "<copilot-session-id>",
  "send_ref": "...",
  "namespace": "...",
  "agent_ref": "..."
}
```

**Final response** (POST to `$WEBHOOK_URL`):

```json
{
  "queue_id": "<uuid>",
  "response": "full answer text",
  "session_id": "<session-id>",
  "send_ref": "...",
  "namespace": "...",
  "agent_ref": "..."
}
```

### Environment Variables

Variables injected by the operator into the agent container:

| Variable | Description |
|---|---|
| `GITHUB_TOKEN` | Auth token from the `githubTokenSecretRef` (can be repurposed for any API key) |
| `WEBHOOK_URL` | URL of the operator's internal webhook (`http://<svc>/response`) |
| `COPILOT_HOME` | Persistent storage root (backed by a PV) |
| `KUBECONFIG` | Path to kubeconfig if a `kubeconfigSecretRef` is set |

**Skills and AGENT.md** are mounted into the container as ConfigMaps:

- Skills ConfigMap → `/copilot-skills-staging/` → `entrypoint.sh` stages them into `$COPILOT_HOME/skills/<name>/SKILL.md`
- AGENT.md ConfigMap → `$COPILOT_HOME/AGENT.md`

### Creating a New Agent Image (e.g., Claude Code)

To add a new AI engine (such as [Claude Code](https://docs.anthropic.com/en/docs/claude-code)), create a new subdirectory under `agent-server-container/` and implement the [API contract](#api-contract):

```mermaid
graph TB
    subgraph repo["agent-server-container/"]
        A["github-copilot/<br/><sub>default engine · Copilot SDK</sub>"]
        B["claude-code/<br/><sub>example new engine</sub>"]
    end

    subgraph files["Required Files"]
        F1["server.py<br/><sub>implement /health, /asyncchat, /cancel</sub>"]
        F2["entrypoint.sh<br/><sub>auth setup, skill staging</sub>"]
        F3["Containerfile<br/><sub>build the image</sub>"]
    end

    B --> F1
    B --> F2
    B --> F3

    style repo fill:#1a1a2e,stroke:#00bcd4,color:#e0e0e0
    style files fill:#16213e,stroke:#e94560,color:#e0e0e0
```

#### 1. Write entrypoint.sh

Set up auth and launch `server.py`:

```bash
#!/bin/bash
set -e

export ANTHROPIC_API_KEY="${ANTHROPIC_API_KEY}"
export AGENT_HOME="${AGENT_HOME:-/agent}"

mkdir -p "${AGENT_HOME}/sessions" "${AGENT_HOME}/.cache"

# Stage skills (same pattern as github-copilot)
if [ -d /copilot-skills-staging ]; then
  for f in /copilot-skills-staging/*.md; do
    [ -f "$f" ] || continue
    skill_name="$(basename "$f" .md)"
    mkdir -p "${AGENT_HOME}/skills/${skill_name}"
    cp "$f" "${AGENT_HOME}/skills/${skill_name}/SKILL.md"
  done
fi

exec /opt/venv/bin/python /server.py
```

#### 2. Write server.py

Implement the three required endpoints:

```python
import asyncio, httpx, json, os, subprocess, uuid
from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI()
WEBHOOK_URL = os.environ.get("WEBHOOK_URL", "")
_active_procs = {}

class AsyncChatRequest(BaseModel):
    message: str
    session_id: str | None = None
    send_ref: str | None = None
    namespace: str | None = None
    agent_ref: str | None = None

@app.get("/health")
async def health():
    return {"status": "ok"}

@app.post("/asyncchat")
async def asyncchat(req: AsyncChatRequest):
    queue_id = str(uuid.uuid4())
    asyncio.create_task(process(queue_id, req))
    return {"queue_id": queue_id, "status": "queued"}

@app.delete("/cancel/{queue_id}")
async def cancel(queue_id: str):
    proc = _active_procs.get(queue_id)
    if proc:
        proc.terminate()
        _active_procs.pop(queue_id, None)
        return {"status": "cancelled", "queue_id": queue_id}
    return {"status": "not_found", "queue_id": queue_id}

async def process(queue_id: str, req: AsyncChatRequest):
    chunk_url = WEBHOOK_URL.replace("/response", "/chunk")
    # Launch Claude Code CLI — adapt flags to the actual binary
    cmd = ["claude", "--no-interactive", "--output-format", "stream-json",
           req.message]
(.. omitted ..)
```

#### 3. Write Containerfile

```dockerfile
FROM python:3.12-slim

RUN pip install --no-cache-dir fastapi uvicorn httpx && \
    # Install the Claude Code CLI (adjust to actual install method)
    pip install claude-code

RUN useradd -m -s /bin/bash agent
WORKDIR /home/agent

COPY entrypoint.sh /entrypoint.sh
COPY server.py /server.py
RUN chmod +x /entrypoint.sh

USER agent
EXPOSE 8080
ENTRYPOINT ["/entrypoint.sh"]
```

#### 4. Add a Makefile target

```makefile
CLAUDE_IMG ?= quay.io/yourorg/kube-claude-code-agent-server:v1.0

.PHONY: container-build-claude container-push-claude
container-build-claude:
$(CONTAINER_TOOL) build -t $(CLAUDE_IMG) ./agent-server-container/claude-code/

container-push-claude:
$(CONTAINER_TOOL) push $(CLAUDE_IMG)
```

#### 5. Create a KubeCopilotAgent CR pointing to the new image

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgent
metadata:
  name: claude-code-agent
  namespace: kube-copilot-agent
spec:
  image: quay.io/yourorg/kube-claude-code-agent-server:v1.0
  githubTokenSecretRef:   # reuse field for ANTHROPIC_API_KEY via a secret
    name: anthropic-token
  skillsConfigMap: claude-skills
  agentConfigMap: claude-agent-md
  storageSize: "1Gi"
```

The operator treats every `KubeCopilotAgent` the same way regardless of which AI engine runs inside — as long as the container implements the [API contract](#api-contract), the full UI, streaming, session history, and cancellation features work automatically.

---

## Development

### Run locally

```sh
make install   # install CRDs into current cluster
make run       # run operator locally against current kubeconfig context
```

### Regenerate CRDs and RBAC after changing API types

```sh
make manifests
make generate
```

### Build and test

```sh
make build
make test
```

---

## Project Structure

```mermaid
graph LR
    subgraph api["api/v1/"]
        types["*_types.go<br/><sub>CRD schemas</sub>"]
    end

    subgraph ctrl["internal/controller/"]
        agent_ctrl["kubecopilotagent_controller.go"]
        send_ctrl["kubecopilotsend_controller.go"]
        cancel_ctrl["kubecopilotcancel_controller.go"]
    end

    subgraph wh["internal/webhook/"]
        server_wh["server.go<br/><sub>receives chunks + responses</sub>"]
    end

    subgraph agentsrv["agent-server-container/"]
        srv["server.py<br/><sub>pluggable engine (default: Copilot SDK)</sub>"]
        ep["entrypoint.sh"]
        cf["Containerfile"]
    end

    subgraph webui["web-ui/"]
        be["app/main.py + k8s_client.py"]
        fe["app/static/index.html"]
    end

    subgraph config["config/"]
        crds["crd/bases/ <sub>generated</sub>"]
        rbac["rbac/ <sub>generated</sub>"]
        mgr["manager/"]
        samples["samples/"]
    end

    subgraph helm_dir["helm/"]
        h1["kube-copilot-agent/"]
        h2["github-copilot-agent/"]
        h3["kube-copilot-ui/"]
        h4["kube-copilot-console-plugin/"]
    end

    subgraph consoleplugin["openshift-console-plugin/"]
        cp_pkg["package.json<br/><sub>plugin metadata</sub>"]
        cp_ext["console-extensions.json<br/><sub>nav + page extensions</sub>"]
        cp_src["src/components/<br/><sub>KubeCopilotPage.tsx</sub>"]
        cp_dock["Containerfile"]
    end

    types --> crds
    types --> ctrl
    ctrl --> agentsrv
    server_wh --> api

    style api fill:#1a1a2e,stroke:#00bcd4,color:#e0e0e0
    style ctrl fill:#16213e,stroke:#00bcd4,color:#e0e0e0
    style wh fill:#0f3460,stroke:#00bcd4,color:#e0e0e0
    style agentsrv fill:#1a1a2e,stroke:#e94560,color:#e0e0e0
    style webui fill:#16213e,stroke:#e94560,color:#e0e0e0
    style config fill:#0f3460,stroke:#ffc107,color:#e0e0e0
    style helm_dir fill:#1a1a2e,stroke:#ffc107,color:#e0e0e0
    style consoleplugin fill:#16213e,stroke:#4caf50,color:#e0e0e0
```

| Directory | Purpose |
|---|---|
| `api/v1/` | CRD type definitions (`*_types.go`) |
| `internal/controller/` | Reconciliation logic (agent, send, cancel controllers) |
| `internal/webhook/` | HTTP server receiving chunks + responses from agent pod |
| `agent-server-container/github-copilot/` | Default engine: SDK-backed FastAPI server wrapping the Copilot CLI |
| `web-ui/` | FastAPI backend + single-page chat UI with settings panel |
| `openshift-console-plugin/` | OpenShift Console dynamic plugin (embeds Web UI in Console) |
| `config/` | Generated CRDs, RBAC, manager manifests, samples |
| `helm/` | Helm charts for operator, agent instance, web UI, and console plugin |

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
