# Agent Templates

Pre-built agent templates for common Kubernetes operations. Each template
provides a domain-specific persona, skills, and least-privilege RBAC
permissions — ready to deploy with a single command.

## Available Templates

| Template | Description |
|----------|-------------|
| [helm-ops](helm-ops/) | Helm release inspection, troubleshooting, and chart management |
| [network-troubleshooter](network-troubleshooter/) | Connectivity diagnosis, DNS debugging, NetworkPolicy analysis |
| [security-auditor](security-auditor/) | RBAC review, pod security compliance, secret scanning |
| [observability](observability/) | Log analysis, metrics inspection, alert investigation |
| [gitops](gitops/) | Argo CD and Flux resource inspection, sync status, drift detection |
| [node-management](node-management/) | Node health checks, drain/cordon operations, capacity planning |

## Prerequisites

1. The **kube-copilot-agent** operator must be installed in the cluster.
2. A GitHub token Secret must exist in the target namespace:

```bash
kubectl create namespace kube-copilot-agent

kubectl create secret generic github-token \
  --from-literal=GITHUB_TOKEN=<your-token> \
  -n kube-copilot-agent
```

## Quick Start

Deploy any template with Kustomize:

```bash
# Deploy the security auditor agent
kubectl apply -k config/agent-templates/security-auditor/

# Deploy the network troubleshooter agent
kubectl apply -k config/agent-templates/network-troubleshooter/
```

You can deploy multiple templates side-by-side — each uses unique resource
names to avoid conflicts.

## Customization

### Changing the namespace

Override the namespace with a Kustomize overlay:

```yaml
# my-overlay/kustomization.yaml
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
  - ../config/agent-templates/security-auditor
namespace: my-namespace
```

### Adjusting RBAC permissions

Edit the `spec.rbac` section in the template's `agent.yaml` to grant or
restrict permissions. The operator automatically creates the ServiceAccount,
Role/ClusterRole, and bindings from these rules.

### Adding or replacing skills

Edit the `skills-configmap.yaml` to add new skill entries or modify existing
ones. Each key must include YAML frontmatter with `name` and `description`
fields.

### Changing the persona

Edit the `AGENT.md` content in `agent-md-configmap.yaml` to adjust the
agent's identity, behavior guidelines, or domain expertise.

## Contributing New Templates

Community contributions are welcome! To add a new template:

1. Create a new directory under `config/agent-templates/<your-template>/`.
2. Include these files (see existing templates for examples):
   - `kustomization.yaml` — references the three resources
   - `agent.yaml` — `KubeCopilotAgent` CR with RBAC permissions
   - `agent-md-configmap.yaml` — domain-specific AGENT.md persona
   - `skills-configmap.yaml` — domain-specific skills with bash snippets
3. Use **unique resource names** (prefixed with the template name).
4. Scope RBAC to **least privilege** for the domain.
5. Only reference tools available in the agent image (`kubectl`, `oc`,
   `curl`, `python3`). If a template requires additional CLIs, document this
   clearly.
6. Open a pull request with a description of the use case.
