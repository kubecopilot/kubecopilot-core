# Multi-Tenant Session Architecture

This document describes the multi-tenant session model introduced by the
**KubeCopilotSession** custom resource. It enables multiple users (tenants) to
share a single kube-copilot-agent installation while maintaining strict data
privacy and isolation.

## Design Decisions

| Decision | Approach |
|---|---|
| Isolation boundary | **Namespace-per-session** — every `KubeCopilotSession` gets its own Kubernetes namespace. |
| Network isolation | A **deny-all ingress** `NetworkPolicy` is installed in each session namespace (configurable via `isolationLevel`). |
| Access control | A `Role` + `RoleBinding` scoped to the session namespace limits the tenant to kubecopilot.io resources only. |
| Lifecycle | A **finalizer** on the session object ensures the namespace (and all its contents) is deleted when the session is removed. |

## How It Works

```
┌──────────────────────────────────────────────────────────────────────┐
│ Namespace: kube-copilot-agent (operator namespace)                   │
│                                                                      │
│  KubeCopilotSession         KubeCopilotAgent                         │
│  ┌───────────────────┐      ┌────────────────────┐                   │
│  │ name: tenant-alice │──ref─▶│ name: my-agent     │                  │
│  │ tenantID: alice    │      └────────────────────┘                   │
│  │ isolationLevel:    │                                               │
│  │   strict           │                                               │
│  └───────────────────┘                                               │
│            │                                                          │
│            │ creates                                                  │
│            ▼                                                          │
│  ┌─────────────────────────────────────────────────┐                 │
│  │ Namespace: kc-session-tenant-alice               │                 │
│  │   Labels:                                        │                 │
│  │     kubecopilot.io/tenant-id: alice              │                 │
│  │     kubecopilot.io/session: tenant-alice          │                 │
│  │                                                   │                 │
│  │   NetworkPolicy: tenant-isolation (deny-all)      │                 │
│  │   Role: tenant-session-role                       │                 │
│  │   RoleBinding: tenant-session-binding             │                 │
│  └─────────────────────────────────────────────────┘                 │
└──────────────────────────────────────────────────────────────────────┘
```

## Quick Start

### 1. Create an agent (if you haven't already)

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgent
metadata:
  name: my-agent
  namespace: kube-copilot-agent
spec:
  githubTokenSecretRef:
    name: github-token
```

### 2. Create a session for a tenant

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotSession
metadata:
  name: tenant-alice
  namespace: kube-copilot-agent
spec:
  tenantID: alice
  agentRef: my-agent
  isolationLevel: strict   # default; use "none" to skip NetworkPolicy
```

### 3. List sessions

```bash
kubectl get kubecopilotsessions -n kube-copilot-agent
```

```
NAME            PHASE    TENANTID   NAMESPACE
tenant-alice    Active   alice      kc-session-tenant-alice
tenant-bob      Active   bob        kc-session-tenant-bob
```

### 4. Destroy a session

```bash
kubectl delete kubecopilotsession tenant-alice -n kube-copilot-agent
```

The finalizer ensures the session namespace (`kc-session-tenant-alice`) and all
resources inside it are deleted automatically.

## Session Spec Reference

| Field | Type | Required | Default | Description |
|---|---|---|---|---|
| `tenantID` | string | ✅ | — | Unique tenant identifier (1–63 chars, DNS-label format). |
| `agentRef` | string | ✅ | — | Name of the `KubeCopilotAgent` in the same namespace. |
| `isolationLevel` | enum | ❌ | `strict` | `strict` installs a deny-all `NetworkPolicy`; `none` skips it. |

## Session Status

| Field | Description |
|---|---|
| `phase` | `Pending`, `Active`, `Error`, or `Terminating`. |
| `namespace` | The isolated namespace created for the session (e.g. `kc-session-tenant-alice`). |
| `conditions` | Standard Kubernetes conditions. |

## Security Model

1. **Namespace isolation** — each tenant's resources live in a separate
   namespace, preventing cross-tenant `kubectl get` or `kubectl exec`.
2. **NetworkPolicy** — by default, a `deny-all ingress` policy prevents pods
   in one session namespace from receiving traffic originating in another
   session's namespace.
3. **RBAC** — a tenant-scoped `Role` restricts access to `kubecopilot.io/*`
   resources within the session namespace. The `RoleBinding` binds to the
   group `kubecopilot:tenant:<tenantID>`, allowing operators to grant access
   through standard Kubernetes group-based authentication.
4. **Finalizer cleanup** — when a session is deleted the controller removes
   the entire namespace, guaranteeing no orphaned data.

## Developer Guide

### Running tests

```bash
make test
```

The controller tests include:
- Basic reconciliation (namespace, NetworkPolicy, RBAC creation)
- Session isolation (two tenants get distinct namespaces)
- `isolationLevel=none` skips NetworkPolicy
- Error handling (missing agent reference)

### Regenerating manifests

After changing `api/v1/kubecopilotsession_types.go`:

```bash
make manifests generate
```
