# Agent Templates

KubeCopilot ships pre-built agent templates for common Kubernetes operations.
Each template provides a domain-specific persona, skills, and least-privilege
RBAC permissions — ready to deploy in seconds.

## Available Templates

| Template | Description | Skills | RBAC Scope |
|----------|-------------|--------|------------|
| **helm-ops** | Helm release inspection, troubleshooting, chart analysis | `helm-inspect`, `helm-troubleshoot` | Read Secrets, workloads (namespace + cluster namespaces) |
| **network-troubleshooter** | Connectivity diagnosis, DNS debugging, NetworkPolicy analysis | `network-diagnosis`, `network-policy` | Read/create Pods, Services, NetworkPolicies (namespace + cluster nodes) |
| **security-auditor** | RBAC review, pod security compliance, secret scanning | `rbac-audit`, `pod-security`, `secret-audit` | Read RBAC, Pods, Secret metadata (namespace + cluster RBAC) |
| **observability** | Log analysis, metrics inspection, alert investigation | `log-analysis`, `metrics-inspect`, `event-analysis` | Read Pods/logs, events, monitoring CRDs (namespace + cluster) |
| **gitops** | Argo CD / Flux status, sync inspection, drift detection | `argocd-inspect`, `flux-inspect` | Read Argo CD / Flux CRDs, workloads (cluster-wide) |
| **node-management** | Node health, drain/cordon operations, capacity planning | `node-health`, `node-ops`, `capacity-planning` | Read/update Nodes, evict Pods (cluster-wide) |

## Prerequisites

1. The **kube-copilot-agent** operator is installed and running.
2. A GitHub token Secret exists in the target namespace:

```bash
kubectl create namespace kube-copilot-agent

kubectl create secret generic github-token \
  --from-literal=GITHUB_TOKEN=<your-token> \
  -n kube-copilot-agent
```

## Deploying a Template

Each template is a self-contained Kustomize overlay. Deploy with:

```bash
kubectl apply -k config/agent-templates/<template-name>/
```

For example:

```bash
# Deploy the security auditor
kubectl apply -k config/agent-templates/security-auditor/

# Deploy the network troubleshooter
kubectl apply -k config/agent-templates/network-troubleshooter/

# Deploy multiple templates side-by-side
kubectl apply -k config/agent-templates/security-auditor/
kubectl apply -k config/agent-templates/observability/
```

Each template uses unique resource names, so multiple agents can run
simultaneously in the same namespace without conflicts.

## Template Structure

Every template follows the same structure:

```
config/agent-templates/<template>/
├── kustomization.yaml          # Kustomize resource list
├── agent.yaml                  # KubeCopilotAgent CR with RBAC
├── agent-md-configmap.yaml     # AGENT.md persona (system prompt)
└── skills-configmap.yaml       # Domain-specific skills
```

### agent.yaml

The `KubeCopilotAgent` custom resource with:
- `spec.rbac.rules` — namespace-scoped permissions (Role + RoleBinding)
- `spec.rbac.clusterRules` — cluster-scoped permissions (ClusterRole +
  ClusterRoleBinding)
- References to the skills and agent-md ConfigMaps

### agent-md-configmap.yaml

Contains the AGENT.md persona that defines the agent's identity, behavior
guidelines, and communication style. This is the system prompt that shapes
how the agent responds.

### skills-configmap.yaml

Contains domain-specific skills as ConfigMap entries. Each skill includes:
- YAML frontmatter with `name` and `description` (tells the agent when to use
  the skill)
- Bash command snippets for common operations in the domain

## Customization Guide

### Adjusting RBAC Permissions

Each template ships with least-privilege RBAC for its domain. To customize:

```yaml
# In agent.yaml, modify spec.rbac:
spec:
  rbac:
    rules:
      # Add namespace-scoped permissions
      - apiGroups: [""]
        resources: ["secrets"]
        verbs: ["get", "list"]
    clusterRules:
      # Add cluster-scoped permissions
      - apiGroups: [""]
        resources: ["nodes"]
        verbs: ["get", "list"]
```

**Important**: `rules` create a Role in the agent's namespace only.
For cross-namespace access, use `clusterRules`.

### Adding Skills

Add a new entry to the skills ConfigMap:

```yaml
# In skills-configmap.yaml, add under data:
data:
  my-custom-skill.md: |
    ---
    name: my-custom-skill
    description: >
      Description of when the agent should use this skill.
    ---

    # My Custom Skill

    ## Commands
    ```bash
    kubectl get pods -A
    ```
```

### Changing the Namespace

Use a Kustomize overlay to deploy to a different namespace:

```yaml
# overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../../config/agent-templates/security-auditor
namespace: my-custom-namespace
```

### Combining Templates

You can merge skills from multiple templates into a single agent by creating
a custom ConfigMap that combines skill entries from multiple templates.

## RBAC Considerations

### Namespace vs Cluster Scope

- **`spec.rbac.rules`** creates a `Role` and `RoleBinding` in the agent's
  namespace. The agent can only access resources in that namespace.
- **`spec.rbac.clusterRules`** creates a `ClusterRole` and `ClusterRoleBinding`.
  The agent can access resources across all namespaces.

### Security Notes

- **Security Auditor**: has `list` (not `get`) access to Secrets — it can see
  secret names and metadata but not values.
- **Node Management**: has `update` and `patch` on Nodes and `create` on
  `pods/eviction` — required for drain/cordon operations. Review these
  permissions if you don't need maintenance capabilities.
- **Network Troubleshooter**: can create and delete Pods for debug testing.
  Pods are always ephemeral (`--rm --restart=Never`).

### Available Tools

All templates use only tools available in the default agent image:
`kubectl`, `oc`, `curl`, and `python3`. Templates that reference external
tools (e.g., `helm`, `argocd`) use kubectl-based alternatives to inspect
the same resources.

## Contributing

See [config/agent-templates/README.md](../config/agent-templates/README.md)
for contribution guidelines.
