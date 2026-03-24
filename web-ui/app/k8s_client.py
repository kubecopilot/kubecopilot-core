import uuid

import httpx
from kubernetes import client, config as k8s_config
from kubernetes.client.rest import ApiException

GROUP = "kubecopilot.io"
VERSION = "v1"
PLURAL_SENDS = "kubecopilotsends"
PLURAL_RESPONSES = "kubecopilotresponses"
PLURAL_CHUNKS = "kubecopilotchunks"
PLURAL_AGENTS = "kubecopilotagents"
PLURAL_CANCELS = "kubecopilotcancels"


def _load_config():
    try:
        k8s_config.load_incluster_config()
    except Exception:
        k8s_config.load_kube_config()


_load_config()
_api = client.CustomObjectsApi()
_core_api = client.CoreV1Api()


def create_send(message: str, agent_ref: str, session_id: str | None, namespace: str,
                session_config: dict | None = None) -> str:
    """Create a KubeCopilotSend (async, fire-and-forget). Returns the object name."""
    name = f"send-{uuid.uuid4().hex[:12]}"
    spec = {
        "agentRef": agent_ref,
        "message": message,
        "sessionID": session_id or "",
    }
    if session_config:
        spec["sessionConfig"] = session_config
    body = {
        "apiVersion": f"{GROUP}/{VERSION}",
        "kind": "KubeCopilotSend",
        "metadata": {
            "name": name,
            "namespace": namespace,
            "labels": {
                "kubecopilot.io/agent-ref": agent_ref,
            },
        },
        "spec": spec,
    }
    _api.create_namespaced_custom_object(GROUP, VERSION, namespace, PLURAL_SENDS, body)
    return name


def get_response_for_send(send_name: str, namespace: str) -> dict | None:
    """Poll for a KubeCopilotResponse whose sendRef label matches send_name. Returns None if not ready yet."""
    label_selector = f"kubecopilot.io/send-ref={send_name}"
    result = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_RESPONSES,
        label_selector=label_selector,
    )
    items = result.get("items", [])
    return items[0] if items else None


def list_agents(namespace: str) -> list[str]:
    result = _api.list_namespaced_custom_object(GROUP, VERSION, namespace, PLURAL_AGENTS)
    return [item["metadata"]["name"] for item in result.get("items", [])]


def list_responses(namespace: str) -> list[dict]:
    result = _api.list_namespaced_custom_object(GROUP, VERSION, namespace, PLURAL_RESPONSES)
    return result.get("items", [])


def list_sessions(agent_ref: str, namespace: str) -> list[dict]:
    """Return unique sessions derived from past KubeCopilotResponse objects."""
    label_selector = f"kubecopilot.io/agent-ref={agent_ref}"
    result = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_RESPONSES,
        label_selector=label_selector,
    )
    items = result.get("items", [])

    seen: dict[str, dict] = {}
    sorted_items = sorted(
        items,
        key=lambda r: r["metadata"].get("creationTimestamp", ""),
        reverse=True,
    )
    for resp in sorted_items:
        spec = resp.get("spec", {})
        labels = resp["metadata"].get("labels", {})
        sid = spec.get("sessionID") or labels.get("kubecopilot.io/session-id")
        if not sid or sid in seen:
            continue
        seen[sid] = {
            "session_id": sid,
            "first_message": spec.get("prompt", "")[:80],
            "created_at": resp["metadata"].get("creationTimestamp", ""),
        }
    return list(seen.values())


def delete_session(session_id: str, agent_ref: str, namespace: str) -> int:
    """Delete all KubeCopilotResponse, KubeCopilotChunk and KubeCopilotSend objects for a session."""
    deleted = 0
    label_selector = f"kubecopilot.io/agent-ref={agent_ref},kubecopilot.io/session-id={session_id}"

    # Delete KubeCopilotResponse objects (have session-id label)
    for item in _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_RESPONSES, label_selector=label_selector,
    ).get("items", []):
        try:
            _api.delete_namespaced_custom_object(
                GROUP, VERSION, namespace, PLURAL_RESPONSES, item["metadata"]["name"]
            )
            deleted += 1
        except ApiException:
            pass

    # Delete KubeCopilotChunk objects (have session-id label)
    for item in _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_CHUNKS, label_selector=label_selector,
    ).get("items", []):
        try:
            _api.delete_namespaced_custom_object(
                GROUP, VERSION, namespace, PLURAL_CHUNKS, item["metadata"]["name"]
            )
            deleted += 1
        except ApiException:
            pass

    # Delete KubeCopilotSend objects — filter by spec.agentRef + spec.sessionID
    # (label may not exist on older sends, so list all and filter in Python)
    for send in _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_SENDS,
    ).get("items", []):
        spec = send.get("spec", {})
        if spec.get("agentRef") == agent_ref and spec.get("sessionID") == session_id:
            try:
                _api.delete_namespaced_custom_object(
                    GROUP, VERSION, namespace, PLURAL_SENDS, send["metadata"]["name"]
                )
                deleted += 1
            except ApiException:
                pass

    return deleted


def get_session_history(session_id: str, agent_ref: str, namespace: str) -> list[dict]:
    """Return all prompt/response pairs for a session, ordered by creation time."""
    label_selector = f"kubecopilot.io/agent-ref={agent_ref},kubecopilot.io/session-id={session_id}"
    result = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_RESPONSES,
        label_selector=label_selector,
    )
    items = sorted(
        result.get("items", []),
        key=lambda r: r["metadata"].get("creationTimestamp", ""),
    )
    history = []
    for resp in items:
        spec = resp.get("spec", {})
        history.append({"role": "user", "content": spec.get("prompt", "")})
        history.append({"role": "assistant", "content": spec.get("response", "")})
    return history


def list_chunks_for_session(session_id: str, agent_ref: str, namespace: str, since_sequence: int = 0) -> list[dict]:
    """Return KubeCopilotChunk objects for a session, ordered by sequence, optionally filtered."""
    label_selector = f"kubecopilot.io/agent-ref={agent_ref},kubecopilot.io/session-id={session_id}"
    result = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_CHUNKS,
        label_selector=label_selector,
    )
    items = result.get("items", [])
    chunks = [
        {
            "sequence": item["spec"]["sequence"],
            "chunk_type": item["spec"]["chunkType"],
            "content": item["spec"].get("content", ""),
            "send_ref": item["spec"].get("sendRef", ""),
            "session_id": item["spec"].get("sessionID", ""),
            "created_at": item["metadata"].get("creationTimestamp", ""),
        }
        for item in items
        if item["spec"]["sequence"] >= since_sequence
    ]
    chunks.sort(key=lambda c: c["sequence"])
    return chunks


def create_cancel(send_ref: str, agent_ref: str, namespace: str) -> str:
    """Create a KubeCopilotCancel to stop an in-flight send. Returns the object name."""
    name = f"cancel-{uuid.uuid4().hex[:12]}"
    body = {
        "apiVersion": f"{GROUP}/{VERSION}",
        "kind": "KubeCopilotCancel",
        "metadata": {
            "name": name,
            "namespace": namespace,
            "labels": {
                "kubecopilot.io/send-ref": send_ref,
                "kubecopilot.io/agent-ref": agent_ref,
            },
        },
        "spec": {
            "agentRef": agent_ref,
            "sendRef": send_ref,
        },
    }
    _api.create_namespaced_custom_object(GROUP, VERSION, namespace, PLURAL_CANCELS, body)
    return name


def list_chunks_for_send(send_ref: str, agent_ref: str, namespace: str) -> list[dict]:
    """Return KubeCopilotChunk objects for a specific send, ordered by sequence."""
    label_selector = f"kubecopilot.io/agent-ref={agent_ref},kubecopilot.io/send-ref={send_ref}"
    result = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_CHUNKS,
        label_selector=label_selector,
    )
    items = result.get("items", [])
    chunks = [
        {
            "sequence": item["spec"]["sequence"],
            "chunk_type": item["spec"]["chunkType"],
            "content": item["spec"].get("content", ""),
            "send_ref": item["spec"].get("sendRef", ""),
            "session_id": item["spec"].get("sessionID", ""),
            "created_at": item["metadata"].get("creationTimestamp", ""),
        }
        for item in items
    ]
    chunks.sort(key=lambda c: c["sequence"])
    return chunks


def list_running_sessions(agent_ref: str, namespace: str) -> list[dict]:
    """
    Return KubeCopilotSend objects that have no corresponding KubeCopilotResponse yet.
    These represent in-progress requests.
    """
    # Get all sends for this agent
    sends = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_SENDS,
        label_selector=f"kubecopilot.io/agent-ref={agent_ref}",
    ).get("items", [])

    # Get all send-refs that have a response
    responses = _api.list_namespaced_custom_object(
        GROUP, VERSION, namespace, PLURAL_RESPONSES,
        label_selector=f"kubecopilot.io/agent-ref={agent_ref}",
    ).get("items", [])
    responded_refs = {
        r.get("metadata", {}).get("labels", {}).get("kubecopilot.io/send-ref", "")
        for r in responses
    }

    running = []
    for send in sends:
        name = send["metadata"]["name"]
        if name not in responded_refs:
            spec = send.get("spec", {})
            running.append({
                "send_ref": name,
                "message": spec.get("message", ""),
                "session_id": spec.get("sessionID", ""),
                "created_at": send["metadata"].get("creationTimestamp", ""),
            })
    running.sort(key=lambda s: s["created_at"])
    return running


# ── Agent service discovery ──────────────────────────────────────────────────

def get_agent_service_url(agent_ref: str, namespace: str) -> str:
    """Return the in-cluster base URL for the agent HTTP server."""
    agent = _api.get_namespaced_custom_object(GROUP, VERSION, namespace, PLURAL_AGENTS, agent_ref)
    service_name = agent.get("status", {}).get("serviceName", "")
    if not service_name:
        raise ValueError(f"Agent {agent_ref} has no serviceName in status")
    return f"http://{service_name}.{namespace}.svc.cluster.local:8080"


async def proxy_agent_get(agent_ref: str, namespace: str, path: str) -> dict:
    """GET a path from the agent HTTP server."""
    base = get_agent_service_url(agent_ref, namespace)
    async with httpx.AsyncClient(timeout=10.0) as http:
        resp = await http.get(f"{base}{path}")
        resp.raise_for_status()
        return resp.json()


async def proxy_agent_put(agent_ref: str, namespace: str, path: str, body: dict) -> dict:
    """PUT a path on the agent HTTP server."""
    base = get_agent_service_url(agent_ref, namespace)
    async with httpx.AsyncClient(timeout=10.0) as http:
        resp = await http.put(f"{base}{path}", json=body)
        resp.raise_for_status()
        return resp.json()


async def proxy_agent_delete(agent_ref: str, namespace: str, path: str) -> dict:
    """DELETE a path on the agent HTTP server."""
    base = get_agent_service_url(agent_ref, namespace)
    async with httpx.AsyncClient(timeout=10.0) as http:
        resp = await http.delete(f"{base}{path}")
        resp.raise_for_status()
        return resp.json()


# ── Provider Secret management ───────────────────────────────────────────────

PROVIDER_SECRET_LABEL = "kubecopilot.io/type"
PROVIDER_SECRET_LABEL_VALUE = "provider"


def get_provider_secret(agent_ref: str, namespace: str) -> dict | None:
    """Get the provider secret for an agent (label kubecopilot.io/type=provider)."""
    label = f"{PROVIDER_SECRET_LABEL}={PROVIDER_SECRET_LABEL_VALUE},kubecopilot.io/agent-ref={agent_ref}"
    result = _core_api.list_namespaced_secret(namespace, label_selector=label)
    if result.items:
        secret = result.items[0]
        data = {}
        if secret.data:
            import base64
            for k, v in secret.data.items():
                data[k] = base64.b64decode(v).decode("utf-8")
        return {
            "name": secret.metadata.name,
            "data": data,
        }
    return None


def upsert_provider_secret(agent_ref: str, namespace: str, provider_type: str,
                           base_url: str, api_key: str) -> str:
    """Create or update the provider Secret for an agent. Returns the Secret name."""
    secret_name = f"{agent_ref}-provider"
    labels = {
        PROVIDER_SECRET_LABEL: PROVIDER_SECRET_LABEL_VALUE,
        "kubecopilot.io/agent-ref": agent_ref,
    }
    string_data = {
        "type": provider_type,
        "base-url": base_url,
        "api-key": api_key,
    }
    try:
        existing = _core_api.read_namespaced_secret(secret_name, namespace)
        existing.string_data = string_data
        existing.metadata.labels = labels
        _core_api.replace_namespaced_secret(secret_name, namespace, existing)
    except ApiException as e:
        if e.status == 404:
            secret = client.V1Secret(
                metadata=client.V1ObjectMeta(name=secret_name, namespace=namespace, labels=labels),
                string_data=string_data,
                type="Opaque",
            )
            _core_api.create_namespaced_secret(namespace, secret)
        else:
            raise
    return secret_name


def delete_provider_secret(agent_ref: str, namespace: str) -> bool:
    """Delete the provider Secret for an agent. Returns True if deleted."""
    secret_name = f"{agent_ref}-provider"
    try:
        _core_api.delete_namespaced_secret(secret_name, namespace)
        return True
    except ApiException as e:
        if e.status == 404:
            return False
        raise
