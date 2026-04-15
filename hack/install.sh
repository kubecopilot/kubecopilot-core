#!/bin/bash

echo "Starting Kube Copilot Agent installation..."

# Ask for cluster context used to build the OpenShift apps route URL.
echo "Enter cluster name (default: simpsons):"
read -r CLUSTER_NAME

if [[ -z "$CLUSTER_NAME" ]]; then
  CLUSTER_NAME="simpsons"
  echo "[INFO] Cluster name not provided, using default: ${CLUSTER_NAME}"
fi

echo "Enter cluster domain (for example: lab.gfontana.me):"
read -r CLUSTER_DOMAIN

if [[ -z "$CLUSTER_DOMAIN" ]]; then
  CLUSTER_DOMAIN="lab.gfontana.me"
  echo "[INFO] Cluster domain not provided, using default: ${CLUSTER_DOMAIN}"
fi

echo "Enter container registry URL (for example: quay.io/gfontana):"
read -r REGISTRY_URL

if [[ -z "$REGISTRY_URL" ]]; then
  REGISTRY_URL="quay.io/gfontana"
  echo "[INFO] Container registry URL not provided, using default: ${REGISTRY_URL}"
fi

echo "Enter container image version/tag (for example: v1.0):"
read -r IMAGE_VERSION

if [[ -z "$IMAGE_VERSION" ]]; then
  IMAGE_VERSION="v1.0"
  echo "[INFO] Container image version not provided, using default: ${IMAGE_VERSION}"
fi

WEB_UI_ROUTE_URL="https://kube-copilot-ui-kube-copilot-agent.apps.${CLUSTER_NAME}.${CLUSTER_DOMAIN}"

# Image variables consumed by Makefile build/push targets.
IMG="${REGISTRY_URL}/kube-copilot-agent:${IMAGE_VERSION}"
AGENT_IMG="${REGISTRY_URL}/github-copilot-agent:${IMAGE_VERSION}"
UI_IMG="${REGISTRY_URL}/kube-copilot-agent-ui:${IMAGE_VERSION}"
CONSOLE_PLUGIN_IMG="${REGISTRY_URL}/kube-copilot-console-plugin:${IMAGE_VERSION}"

# Move to repo root (script lives in hack/).
cd "$(dirname "$0")/.." || {
	echo "[ERROR] Failed to change directory to repository root."
	exit 1
}


# Ask whether to build/push images
echo "Do you want to build and push operator/agent/UI container images? [Y/n]"
read -r build_images_choice

if [[ -z "$build_images_choice" || "$build_images_choice" =~ ^[Yy]$ ]]; then
  echo "[1/8] Building and pushing operator, agent, and UI container images..."
  make \
    IMG="$IMG" \
    AGENT_IMG="$AGENT_IMG" \
    UI_IMG="$UI_IMG" \
    CONSOLE_PLUGIN_IMG="$CONSOLE_PLUGIN_IMG" \
    container-build container-push container-build-agent container-push-agent container-build-ui container-push-ui
  echo "Container images built and pushed."
else
  echo "[1/8] Skipping container image build and push."
fi

# Deploy the operator
echo "[2/8] Deploying operator..."
helm upgrade --install kube-copilot-agent ./helm/kube-copilot-agent \
  --namespace kube-copilot-agent \
  --set image.repository="${REGISTRY_URL}/kube-copilot-agent" \
  --set image.tag="${IMAGE_VERSION}" \
  --create-namespace=true
echo "Operator deployed."

# Wait for user to create the GitHub token secret
echo "[3/8] Manual step required: GitHub token secret"
echo "Please create the GitHub token secret by running:"
echo "kubectl apply -f config/samples/github-token-secret.yaml"
echo "Press Enter once you have created the secret..."
read
echo "GitHub token secret step completed."

# Wait for user to create the cluster kubeconfig secret
echo "[4/8] Manual step required: cluster kubeconfig secret"
echo "Please create the cluster kubeconfig secret by running:"
echo "kubectl apply -f config/samples/cluster-kubeconfig-secret.yaml"
echo "Press Enter once you have created the secret..."
read
echo "Cluster kubeconfig secret step completed."

### Deploy an agent
echo "[5/8] Deploying sample agent resource..."
helm upgrade --install my-agent ./helm/github-copilot-agent \
  --namespace kube-copilot-agent \
  --set githubToken.existingSecret=github-token \
  --set kubeconfigSecretRef=cluster-kubeconfig \
  --set agent.image="${REGISTRY_URL}/github-copilot-agent:${IMAGE_VERSION}"
echo "Sample agent deployed."

### Deploy the Web UI
echo "[6/8] Deploying Web UI..."
helm upgrade --install kube-copilot-ui ./helm/kube-copilot-ui \
  --namespace kube-copilot-agent \
  --set route.enabled=true \
  --set image.repository="${REGISTRY_URL}/kube-copilot-agent-ui" \
  --set image.tag="${IMAGE_VERSION}"
echo "Web UI deployed."

### Deploy console plugin
echo "[7/8] Deploying console plugin..."
helm upgrade --install kube-copilot-console-plugin ./helm/kube-copilot-console-plugin \
  --namespace kube-copilot-agent \
  --set plugin.image="${REGISTRY_URL}/kube-copilot-console-plugin:${IMAGE_VERSION}" \
  --set webUI.serviceName=kube-copilot-ui \
  --set webUI.servicePort=8000 \
  --set webUI.routeUrl="${WEB_UI_ROUTE_URL}"
echo "Console plugin deployed."

### Test the setup by creating a KubecopilotSend resource
echo "[8/8] Creating a test KubeCopilotSend resource..."
Sleep 20 # wait a bit for everything to be up and running

cat <<EOF | kubectl apply -f -
  apiVersion: kubecopilot.io/v1
  kind: KubeCopilotSend
  metadata:
    name: my-question
    namespace: kube-copilot-agent
  spec:
    agentRef: github-copilot-agent
    message: "What is 2 + 2?"
    sessionID: ""   # leave empty to start a new session
EOF
echo "Test resource created."

### Watch kubecopilotchunks and present a success message once the response is received
echo "Waiting for chunks..."
while true; do
  if kubectl get kubecopilotchunks -n kube-copilot-agent -o name 2>/dev/null | grep -q .; then
    echo "Received chunks!"
    break
  fi
  sleep 2
done

### Watch kubecopilotresponse and present a success message once the response is received
echo "Waiting for response..."
while true; do
  if kubectl get kubecopilotresponse -n kube-copilot-agent -o name 2>/dev/null | grep -q .; then
    echo "Received response!"
    break
  fi
  sleep 2
done

echo "Installation flow completed successfully."