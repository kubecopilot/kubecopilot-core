# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in KubeCopilot, please report it responsibly.

**Do NOT open a public GitHub issue for security vulnerabilities.**

Instead, please email: **[security@kubecopilot.io](mailto:security@kubecopilot.io)**

Include:
- A description of the vulnerability
- Steps to reproduce
- Potential impact
- Suggested fix (if any)

We will acknowledge your report within **48 hours** and aim to provide a fix or mitigation within **7 days** for critical issues.

## Supported Versions

| Version | Supported |
|---|---|
| latest (main branch) | ✅ |
| Previous releases | Best effort |

## Security Considerations

KubeCopilot deploys AI agents with cluster access. Please review the following before deploying:

- **RBAC**: Always use the least-privilege ServiceAccount for your agents. See [issue #4](https://github.com/kubecopilot/kubecopilot-core/issues/4) for the RBAC feature.
- **Network Policies**: Restrict agent pod network access to only what is needed.
- **Secrets**: API keys are stored in Kubernetes Secrets. Ensure your cluster encrypts etcd at rest.
- **Agent Instructions**: Review `AGENT.md` and skills carefully — they define what agents can do.
- **Guardrails**: A safety policy CRD is planned ([issue #22](https://github.com/kubecopilot/kubecopilot-core/issues/22)) to constrain agent actions.

## Disclosure Policy

We follow [coordinated disclosure](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure). We will credit reporters in the fix announcement unless they prefer to remain anonymous.
