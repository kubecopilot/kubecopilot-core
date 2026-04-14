# Agent-to-Agent Communication

## Overview

KubeCopilot supports multi-agent collaboration through agent-to-agent (A2A)
communication. A **coordinator** agent can delegate tasks to **member** agents,
enabling specialised problem-solving across different Kubernetes domains such as
networking, security, and observability.

The mechanism builds on two existing primitives:

| Primitive | Purpose |
|---|---|
| `KubeCopilotAgent` | Defines a running agent instance. |
| `KubeCopilotSend` | Sends an asynchronous message to an agent. |

A new CRD, **`KubeCopilotAgentTeam`**, ties these together by declaring which
agents form a team and how delegation is orchestrated.

## How Delegation Works

1. An operator creates a `KubeCopilotAgentTeam` CR that specifies a
   **coordinator** agent and one or more **member** agents.
2. The team controller validates that all referenced `KubeCopilotAgent` CRs
   exist and updates the coordinator's `spec.delegateTo` field with the list of
   member agent names.
3. The agent controller detects the `delegateTo` field and injects the
   `DELEGATE_TO_AGENTS` environment variable (JSON-encoded array) into the
   coordinator's pod.
4. The agent runtime uses this information to expose a `delegate_to_agent` tool
   that creates `KubeCopilotSend` CRs targeting the specified member agents.

```
User ──▶ KubeCopilotSend ──▶ Coordinator Agent
                                   │
                        ┌──────────┼──────────┐
                        ▼          ▼          ▼
                    Member A   Member B   Member C
                   (network)  (security) (storage)
```

## KubeCopilotAgentTeam CRD Reference

### Spec

| Field | Type | Required | Description |
|---|---|---|---|
| `coordinator` | `string` | Yes | Name of the `KubeCopilotAgent` that acts as team coordinator. |
| `members` | `[]TeamMember` | Yes | List of member agents (minimum 1). |
| `strategy` | `string` | No | Delegation strategy: `sequential` (default) or `parallel`. |

### TeamMember

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | `string` | Yes | Name of the `KubeCopilotAgent` CR. |
| `role` | `string` | Yes | Short role identifier (e.g., `network-expert`). |
| `description` | `string` | No | Human-readable description of the member's speciality. |

### Status

| Field | Type | Description |
|---|---|---|
| `phase` | `string` | `Pending`, `Active`, or `Error`. |
| `memberCount` | `int` | Number of validated member agents. |
| `conditions` | `[]Condition` | Standard Kubernetes conditions (type `Ready`). |

## Example Team Configurations

### Operations Team

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgentTeam
metadata:
  name: ops-team
  namespace: kube-copilot-agent
spec:
  coordinator: github-copilot-agent
  members:
    - name: network-troubleshooter-agent
      role: network-expert
      description: Diagnoses connectivity issues, DNS problems, and NetworkPolicy conflicts
    - name: security-auditor-agent
      role: security-auditor
      description: Reviews RBAC configurations, pod security compliance, and secret hygiene
  strategy: sequential
```

### Incident Response Team (Parallel)

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgentTeam
metadata:
  name: incident-response
  namespace: kube-copilot-agent
spec:
  coordinator: triage-agent
  members:
    - name: logs-agent
      role: log-analyst
      description: Searches and correlates application and cluster logs
    - name: metrics-agent
      role: metrics-analyst
      description: Analyses Prometheus metrics and resource utilisation
    - name: events-agent
      role: event-watcher
      description: Monitors Kubernetes events for anomalies
  strategy: parallel
```

## Sequential vs Parallel Strategies

| Strategy | Behaviour | Best For |
|---|---|---|
| `sequential` | The coordinator delegates to members one at a time, waiting for each response before proceeding. | Step-by-step workflows where each step depends on the previous result. |
| `parallel` | The coordinator delegates to all members simultaneously. | Independent investigations that can run concurrently (e.g., incident triage). |

The strategy is advisory — it tells the coordinator's runtime how to schedule
delegations. The underlying mechanism (creating `KubeCopilotSend` CRs) is the
same in both cases.

## Adding Delegation to an Existing Agent

You can also configure delegation directly on a `KubeCopilotAgent` without
creating a `KubeCopilotAgentTeam`:

```yaml
apiVersion: kubecopilot.io/v1
kind: KubeCopilotAgent
metadata:
  name: coordinator-agent
  namespace: kube-copilot-agent
spec:
  githubTokenSecretRef:
    name: gh-token
  delegateTo:
    - network-agent
    - security-agent
```

The team CRD is recommended for production use as it provides validation,
status tracking, and a single source of truth for team composition.
