← [Back to README](../README.md)

# Development Guide

## Run locally

```sh
make install   # install CRDs into current cluster
make run       # run operator locally against current kubeconfig context
```

## Regenerate CRDs and RBAC after changing API types

```sh
make manifests
make generate
```

## Build and test

```sh
make build
make test
```

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
