#!/usr/bin/env bash

# Uninstalls Kube Copilot Agent components from the kube-copilot-agent namespace.
# This script is intentionally tolerant: missing releases/namespace are treated
# as informational, so it can be safely re-run.

NAMESPACE="kube-copilot-agent"
API_GROUP="kubecopilot.io"

echo "[INFO] Starting Kube Copilot Agent uninstallation..."

# Move to repo root (script lives in hack/).
cd "$(dirname "$0")/.." || {
	echo "[ERROR] Failed to change directory to repository root."
	exit 1
}

# Ensure required CLIs are available before attempting uninstall.
if ! command -v helm >/dev/null 2>&1; then
	echo "[ERROR] helm is not installed or not in PATH."
	exit 1
fi

if ! command -v kubectl >/dev/null 2>&1; then
	echo "[ERROR] kubectl is not installed or not in PATH."
	exit 1
fi

uninstall_release() {
	local release_name="$1"

	if helm status "$release_name" --namespace "$NAMESPACE" >/dev/null 2>&1; then
		echo "[INFO] Uninstalling Helm release: $release_name"
		helm uninstall "$release_name" --namespace "$NAMESPACE"
		echo "[INFO] Successfully uninstalled: $release_name"
	else
		echo "[INFO] Helm release not found (skipping): $release_name"
	fi
}

delete_api_group_objects() {
	local group="$1"
	local has_any_resource=false

	echo "[INFO] Searching API resources in group: $group"

	# Delete namespaced objects from this API group across all namespaces.
	while IFS= read -r resource; do
		[ -z "$resource" ] && continue
		has_any_resource=true
		echo "[INFO] Deleting namespaced objects for resource: $resource"
		kubectl delete "$resource" --all --all-namespaces --ignore-not-found=true >/dev/null 2>&1 || true
	done < <(kubectl api-resources --api-group="$group" --namespaced=true --verbs=list -o name 2>/dev/null)

	# Delete cluster-scoped objects from this API group.
	while IFS= read -r resource; do
		[ -z "$resource" ] && continue
		has_any_resource=true
		echo "[INFO] Deleting cluster-scoped objects for resource: $resource"
		kubectl delete "$resource" --all --ignore-not-found=true >/dev/null 2>&1 || true
	done < <(kubectl api-resources --api-group="$group" --namespaced=false --verbs=list -o name 2>/dev/null)

	if [ "$has_any_resource" = false ]; then
		echo "[INFO] No API resources found for group: $group"
	else
		echo "[INFO] Completed deletion of objects from API group: $group"
	fi
}

# First, clean up all custom resources from kubecopilot.io API group.
delete_api_group_objects "$API_GROUP"

# Uninstall all known releases for this project.
uninstall_release "kube-copilot-console-plugin"
uninstall_release "kube-copilot-ui"
uninstall_release "my-agent"
uninstall_release "kube-copilot-agent"

# Delete namespace if it still exists.
if kubectl get namespace "$NAMESPACE" >/dev/null 2>&1; then
	echo "[INFO] Deleting namespace: $NAMESPACE"
	kubectl delete namespace "$NAMESPACE"
	echo "[INFO] Namespace deleted: $NAMESPACE"
else
	echo "[INFO] Namespace not found (nothing to delete): $NAMESPACE"
fi

echo "[INFO] Uninstallation workflow completed."