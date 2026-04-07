← [Back to README](../README.md)

# Usage Guide

## Via the Web UI

Open the route URL in a browser, select your agent, and start chatting. The UI supports:

- Multi-turn conversations with session history in the sidebar
- **Running Sessions** panel showing in-progress requests
- **Agent Activity** tab showing real-time chunk streaming
- **Stop** button to cancel an in-flight request

## Via kubectl (CRDs directly)

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
