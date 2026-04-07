# Contributing to KubeCopilot

Thank you for your interest in contributing! This document explains how to get the project running locally, the conventions we follow, and how to submit your changes.

---

## Table of Contents

- [Code of Conduct](#code-of-conduct)
- [Getting Started](#getting-started)
- [Project Structure](#project-structure)
- [Development Workflow](#development-workflow)
- [Adding a New Agent Backend](#adding-a-new-agent-backend)
- [Changing CRD Types](#changing-crd-types)
- [Commit Messages](#commit-messages)
- [Pull Request Guidelines](#pull-request-guidelines)
- [Reporting Bugs](#reporting-bugs)
- [Requesting Features](#requesting-features)

---

## Code of Conduct

This project follows the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md). By participating, you are expected to uphold this code. Please report unacceptable behaviour to [conduct@kubecopilot.io](mailto:conduct@kubecopilot.io).

---

## Getting Started

### Prerequisites

| Tool | Version |
|---|---|
| Go | v1.24+ |
| kubectl | v1.20+ |
| kubebuilder | v4.x |
| Podman or Docker | any recent |
| A Kubernetes cluster | local (kind/minikube) or remote |
| GitHub Copilot access | for end-to-end testing |

### Fork and clone

```sh
git clone https://github.com/kubecopilot/kubecopilot-core.git
cd kube-copilot-agent
```

### Install dependencies

```sh
go mod download
```

### Install CRDs into your cluster

```sh
make install
```

### Run the operator locally

```sh
make run
```

The operator connects to whichever cluster your current `KUBECONFIG` context points to.

---

## Project Structure

```
api/v1/                          CRD type definitions (edit these to change the API)
internal/controller/             Reconcilers for each CRD
internal/webhook/                HTTP server that receives chunks/responses from agent pods
agent-server-container/          Agent HTTP shim images
  github-copilot/                GitHub Copilot CLI implementation
    server.py                    FastAPI shim
    entrypoint.sh                Auth setup and skill staging
    Containerfile
web-ui/                          Browser-based chat interface
  app/main.py                    FastAPI backend
  app/k8s_client.py              Kubernetes API client
  app/static/index.html          Single-page UI (no build step)
  deploy/base/                   Kustomize manifests
config/
  crd/bases/                     Generated manifests — do not edit manually
  rbac/role.yaml                 Operator ClusterRole — keep in sync with controller code
  samples/                       Example CRs for testing
```

---

## Development Workflow

### 1. Make your changes

- **API types** live in `api/v1/*_types.go`
- **Reconciliation logic** lives in `internal/controller/`
- **Webhook server** (chunk/response receiver) lives in `internal/webhook/server.go`
- **Agent shim** lives in `agent-server-container/github-copilot/server.py`
- **Web UI** lives in `web-ui/app/static/index.html` (single file, no build step)

### 2. Regenerate manifests after API changes

Any change to `api/v1/*_types.go` requires regenerating CRDs and RBAC:

```sh
make manifests   # regenerates config/crd/bases/ and config/rbac/
make generate    # regenerates DeepCopy methods
```

> **Important:** if you add a new verb to a controller (e.g., `create` on a new resource type), you must also add the corresponding rule to `config/rbac/role.yaml`. Forgetting this causes a `forbidden` error at runtime.

### 3. Build and test

```sh
make build
make test
```

### 4. Build container images

```sh
make container-build          # operator
make container-build-agent    # agent shim (github-copilot)
make container-build-ui       # web UI
```

### 5. Deploy and test end-to-end

```sh
make deploy                        # deploy operator
kubectl apply -f config/samples/   # deploy sample agent and supporting resources
```

Watch logs:
```sh
kubectl logs -n kube-copilot-agent -l control-plane=controller-manager -f
kubectl logs -n kube-copilot-agent -l app=github-copilot-agent -f
```

---

## Adding a New Agent Backend

If you want to contribute support for a new AI CLI (e.g., Claude Code, Gemini CLI), add a new subdirectory under `agent-server-container/`:

```
agent-server-container/
  your-agent/
    server.py        ← must implement /health, /asyncchat, /cancel/{queue_id}
    entrypoint.sh    ← auth setup, skill staging
    Containerfile
```

See the **Agent Server Container** section in [README.md](README.md) for the full API contract, required webhook payload shapes, and a complete working skeleton.

Add a `Makefile` target for your image:

```makefile
YOUR_IMG ?= quay.io/yourorg/kube-your-agent-server:v1.0

.PHONY: container-build-your-agent container-push-your-agent
container-build-your-agent:
	$(CONTAINER_TOOL) build -t $(YOUR_IMG) ./agent-server-container/your-agent/

container-push-your-agent:
	$(CONTAINER_TOOL) push $(YOUR_IMG)
```

Add a sample CR under `config/samples/` so reviewers can test your agent end-to-end.

---

## Changing CRD Types

1. Edit the relevant `*_types.go` file in `api/v1/`
2. Run `make manifests generate`
3. If you added a new CRD resource, add it to the `resources` list in `PROJECT`
4. Add a sample CR to `config/samples/`
5. Update `config/rbac/role.yaml` if the controller needs new permissions for the new type
6. Update `README.md` CRDs table if the change is user-visible

---

## Commit Messages

Use [Conventional Commits](https://www.conventionalcommits.org/) format:

```
<type>(<scope>): <short summary>

[optional body]
```

Common types:

| Type | When to use |
|---|---|
| `feat` | New feature or CRD field |
| `fix` | Bug fix |
| `docs` | Documentation only |
| `refactor` | Code change that is not a fix or feature |
| `chore` | Build, CI, dependency updates |
| `test` | Adding or fixing tests |

Examples:
```
feat(controller): add retry logic for agent pod creation
fix(webhook): handle missing session_id in chunk payload
docs: add agent backend contribution guide
chore: rename docker targets to container in Makefile
```

---

## Developer Certificate of Origin (DCO)

By contributing to KubeCopilot, you agree to the [Developer Certificate of Origin (DCO)](https://developercertificate.org/). This is a lightweight way to certify that you wrote or have the right to submit the code you are contributing.

All commits must be signed off using `git commit -s`:

```sh
git commit -s -m "feat(controller): add retry logic"
```

This adds a `Signed-off-by` trailer to your commit message with your name and email.

> **Tip**: Configure git to always sign off: `git config --global format.signOff true`

---

## Pull Request Guidelines

1. **One concern per PR** — keep changes focused; avoid mixing unrelated fixes
2. **Include tests** for new controller logic where possible
3. **Update docs** — if your change affects user-facing behaviour, update `README.md`
4. **Regenerate manifests** — always run `make manifests generate` before opening a PR if you touched `api/v1/`
5. **Verify RBAC** — check that `config/rbac/role.yaml` matches the permissions your controller uses
6. **Add a sample** — if you add a new CRD or agent backend, include a sample CR in `config/samples/`
7. **Test end-to-end** — deploy to a real cluster and verify the happy path before requesting review

### PR title

Use the same Conventional Commits format as commit messages.

---

## Reporting Bugs

Open a GitHub issue and include:

- What you did (steps to reproduce)
- What you expected to happen
- What actually happened (logs, error messages)
- Cluster version and operator version (`kubectl get kubecopilotagents -o yaml`)

---

## Requesting Features

Open a GitHub issue describing:

- The problem you are trying to solve
- Your proposed solution or approach
- Any alternative approaches you considered

For larger changes, open an issue **before** starting work so we can discuss the design first.
