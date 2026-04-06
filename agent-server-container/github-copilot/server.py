#!/usr/bin/env python3
"""
KubeCopilot Agent Server — backed by the GitHub Copilot Python SDK.

Replaces the previous subprocess-based approach with CopilotClient (JSON-RPC
to the Copilot CLI in server mode), providing proper streaming events, session
management, concurrent request handling, and programmatic configuration.
"""

import asyncio
import json
import os
import re
import shutil
import uuid
from pathlib import Path
from typing import Any

import httpx
from copilot import CopilotClient, PermissionHandler, SubprocessConfig
from fastapi import FastAPI, HTTPException, Request
from pydantic import BaseModel

app = FastAPI(title="KubeCopilot Agent")

# ── Paths ────────────────────────────────────────────────────────────────────
COPILOT_HOME = Path(os.environ.get("COPILOT_HOME", "/copilot"))
SESSIONS_DIR = COPILOT_HOME / "sessions"
SKILLS_DIR = COPILOT_HOME / "skills"
CUSTOM_AGENTS_FILE = COPILOT_HOME / "custom-agents.json"
INSTRUCTIONS_FILE = COPILOT_HOME / "copilot-instructions.md"
# Staging paths — ConfigMaps are read-only; server copies them to the writable PVC at startup
SKILLS_STAGING_DIR = Path("/copilot-skills-staging")
AGENT_STAGING_DIR = Path("/copilot-agent-staging")

SESSIONS_DIR.mkdir(parents=True, exist_ok=True)
SKILLS_DIR.mkdir(parents=True, exist_ok=True)

WEBHOOK_URL = os.environ.get("WEBHOOK_URL", "")
TASKS_FILE = COPILOT_HOME / "tasks.json"

# ── Singleton CopilotClient ──────────────────────────────────────────────────
_client: CopilotClient | None = None
_client_lock = asyncio.Lock()

# Track active SDK sessions for cancellation: queue_id → session
_active_sessions: dict[str, Any] = {}

# Semaphore to limit concurrent SDK sessions (CLI in server mode can handle
# multiple, but we bound it to avoid overwhelming the single CLI process).
_concurrency = asyncio.Semaphore(int(os.environ.get("MAX_CONCURRENT_SESSIONS", "3")))


async def _get_client() -> CopilotClient:
    """Return (and lazily start) the singleton CopilotClient."""
    global _client
    async with _client_lock:
        if _client is None:
            _client = CopilotClient(SubprocessConfig(
                cwd=str(COPILOT_HOME),
                log_level="info",
            ))
            await _client.start()
        return _client


# ── Pydantic models ──────────────────────────────────────────────────────────

class ProviderConfig(BaseModel):
    type: str = "openai"
    base_url: str | None = None
    api_key: str | None = None
    bearer_token: str | None = None
    model_name: str | None = None


class CustomAgentConfig(BaseModel):
    name: str
    display_name: str | None = None
    description: str | None = None
    prompt: str
    tools: list[str] | None = None
    infer: bool = True


class SessionConfig(BaseModel):
    model: str | None = None
    system_message: str | None = None
    disabled_skills: list[str] | None = None
    custom_agents: list[CustomAgentConfig] | None = None
    provider: ProviderConfig | None = None
    tools_config: dict[str, bool] | None = None


class ChatRequest(BaseModel):
    message: str
    session_id: str | None = None
    session_config: SessionConfig | None = None


class AsyncChatRequest(BaseModel):
    message: str
    session_id: str | None = None
    send_ref: str | None = None
    namespace: str | None = None
    agent_ref: str | None = None
    session_config: SessionConfig | None = None


class ChatResponse(BaseModel):
    response: str
    session_id: str


# ── Session history helpers ──────────────────────────────────────────────────

def load_session(session_id: str) -> list[dict]:
    path = SESSIONS_DIR / f"{session_id}.json"
    if path.exists():
        return json.loads(path.read_text())
    return []


def save_session(session_id: str, history: list[dict]) -> None:
    path = SESSIONS_DIR / f"{session_id}.json"
    path.write_text(json.dumps(history, indent=2))


# ── Custom agents from PVC ───────────────────────────────────────────────────

def _load_custom_agents_from_file() -> list[dict]:
    """Load custom agent definitions from /copilot/custom-agents.json."""
    if CUSTOM_AGENTS_FILE.exists():
        try:
            data = json.loads(CUSTOM_AGENTS_FILE.read_text())
            if isinstance(data, list):
                return data
        except (json.JSONDecodeError, OSError):
            pass
    return []


# ── SDK session builder ──────────────────────────────────────────────────────

async def _build_session_opts(cfg: SessionConfig | None) -> dict:
    """Build the options dict for CopilotClient.create_session()."""
    opts: dict[str, Any] = {
        "on_permission_request": PermissionHandler.approve_all,
        "streaming": True,
    }

    # Skills from PVC (always loaded)
    if SKILLS_DIR.exists() and any(SKILLS_DIR.iterdir()):
        opts["skill_directories"] = [str(SKILLS_DIR)]

    # Custom agents from PVC file + optional per-request overrides
    pvc_agents = _load_custom_agents_from_file()
    if pvc_agents:
        opts["custom_agents"] = pvc_agents

    if cfg is None:
        return opts

    if cfg.model:
        opts["model"] = cfg.model

    if cfg.system_message:
        opts["system_message"] = {"content": cfg.system_message}

    if cfg.disabled_skills:
        opts["disabled_skills"] = cfg.disabled_skills

    if cfg.custom_agents:
        # Per-request agents override PVC agents
        opts["custom_agents"] = [a.model_dump(exclude_none=True) for a in cfg.custom_agents]

    if cfg.provider:
        opts["provider"] = cfg.provider.model_dump(exclude_none=True)
        # Provider's model_name takes precedence as the top-level SDK model option
        if cfg.provider.model_name and not opts.get("model"):
            opts["model"] = cfg.provider.model_name

    return opts


# ── Webhook helpers ──────────────────────────────────────────────────────────

SKIP_TOOLS = {"report_intent", "skill"}


async def _post_chunk(
    chunk_url: str, send_ref: str, session_id: str | None,
    agent_ref: str | None, namespace: str | None,
    sequence: int, chunk_type: str, content: str,
):
    """POST a streaming chunk to the operator webhook (fire-and-forget)."""
    try:
        async with httpx.AsyncClient(timeout=5.0) as http:
            await http.post(chunk_url, json={
                "send_ref": send_ref or "",
                "session_id": session_id or "",
                "agent_ref": agent_ref or "",
                "namespace": namespace or "",
                "sequence": sequence,
                "chunk_type": chunk_type,
                "content": content,
            })
    except Exception as e:
        print(f"[chunk] POST failed seq={sequence}: {e}")


# ── Core SDK-based execution ─────────────────────────────────────────────────

async def _run_sdk_streaming(
    message: str,
    session_id: str | None,
    send_ref: str | None,
    namespace: str | None,
    agent_ref: str | None,
    queue_id: str | None,
    session_config: SessionConfig | None = None,
) -> tuple[str, str]:
    """
    Run a copilot interaction via the SDK, streaming chunks to the webhook.
    Returns (response_text, session_id).
    """
    client = await _get_client()
    chunk_url = WEBHOOK_URL.replace("/response", "/chunk") if WEBHOOK_URL else ""
    sequence = 0
    response_text = ""
    resolved_session_id = session_id or ""
    _thinking_buffer = ""  # accumulates reasoning deltas until a sentence boundary

    if chunk_url and send_ref:
        await _post_chunk(
            chunk_url, send_ref, session_id, agent_ref, namespace,
            sequence, "info", f"Processing: {message[:120]}",
        )
        sequence += 1

    opts = await _build_session_opts(session_config)

    # Create or resume session
    if session_id:
        session = await client.resume_session(session_id, **opts)
    else:
        session = await client.create_session(**opts)

    # Capture session ID immediately from the session object
    resolved_session_id = getattr(session, "session_id", None) or session_id or ""
    if queue_id:
        _active_sessions[queue_id] = session

    # Collect response via events
    done = asyncio.Event()
    cancelled = False

    def on_event(event):
        nonlocal sequence, response_text, resolved_session_id, cancelled, _thinking_buffer

        etype = event.type.value if hasattr(event.type, "value") else str(event.type)
        data = event.data if hasattr(event, "data") else None

        # Session ID extraction
        if etype == "session.created" and data and hasattr(data, "session_id"):
            resolved_session_id = data.session_id or resolved_session_id

        # Streaming text deltas
        elif etype == "assistant.message_delta":
            delta = getattr(data, "delta_content", "") or ""
            if delta:
                response_text += delta

        # Reasoning deltas — buffer until a sentence boundary, then post one chunk
        elif etype == "assistant.reasoning_delta":
            delta = getattr(data, "delta_content", "") or ""
            if delta:
                _thinking_buffer += delta
                # Flush on sentence-ending punctuation followed by whitespace or end
                if chunk_url and send_ref and any(
                    c in _thinking_buffer for c in (".", "!", "?", "\n")
                ):
                    # Split on sentence boundary; keep trailing fragment in buffer
                    parts = re.split(r'(?<=[.!?\n])\s*', _thinking_buffer, maxsplit=1)
                    flush_text = parts[0].strip()
                    _thinking_buffer = parts[1].strip() if len(parts) > 1 else ""
                    if flush_text:
                        asyncio.get_event_loop().create_task(_post_chunk(
                            chunk_url, send_ref, resolved_session_id or session_id,
                            agent_ref, namespace, sequence, "thinking",
                            f"🤔 {flush_text[:300]}",
                        ))
                        sequence += 1

        # Final message
        elif etype == "assistant.message":
            content = getattr(data, "content", "") or ""
            tool_requests = getattr(data, "tool_requests", None)
            if content and not tool_requests:
                response_text = content
                if chunk_url and send_ref:
                    preview = content[:200] + ("…" if len(content) > 200 else "")
                    asyncio.get_event_loop().create_task(_post_chunk(
                        chunk_url, send_ref, resolved_session_id or session_id,
                        agent_ref, namespace, sequence, "response",
                        f"💬 {preview}",
                    ))
                    sequence += 1
            elif tool_requests:
                names = ", ".join(
                    getattr(tr, "name", "?") for tr in tool_requests
                    if getattr(tr, "name", "") not in SKIP_TOOLS
                )
                if names and chunk_url and send_ref:
                    asyncio.get_event_loop().create_task(_post_chunk(
                        chunk_url, send_ref, resolved_session_id or session_id,
                        agent_ref, namespace, sequence, "tool_call",
                        f"Invoking: **{names}**",
                    ))
                    sequence += 1

        # Tool execution
        elif etype == "tool.execution_start":
            tool_name = getattr(data, "tool_name", "") or ""
            if tool_name not in SKIP_TOOLS and chunk_url and send_ref:
                args = getattr(data, "arguments", {}) or {}
                desc = args.get("description") or args.get("command") or str(args)[:120]
                asyncio.get_event_loop().create_task(_post_chunk(
                    chunk_url, send_ref, resolved_session_id or session_id,
                    agent_ref, namespace, sequence, "tool_call",
                    f"🔧 **{tool_name}**: {desc[:200]}",
                ))
                sequence += 1

        elif etype == "tool.execution_complete":
            tool_name = getattr(data, "tool_name", "") or ""
            if tool_name not in SKIP_TOOLS and chunk_url and send_ref:
                result = getattr(data, "result", None)
                output = ""
                if result:
                    output = getattr(result, "content", "") or getattr(result, "detailed_content", "") or ""
                if output and output.strip():
                    success = "✅" if getattr(data, "success", True) else "❌"
                    asyncio.get_event_loop().create_task(_post_chunk(
                        chunk_url, send_ref, resolved_session_id or session_id,
                        agent_ref, namespace, sequence, "tool_result",
                        f"{success} **{tool_name}** result:\n```\n{output[:400]}\n```",
                    ))
                    sequence += 1

        # Skill invocation
        elif etype == "skill.invoked":
            name = getattr(data, "name", "?")
            if chunk_url and send_ref:
                asyncio.get_event_loop().create_task(_post_chunk(
                    chunk_url, send_ref, resolved_session_id or session_id,
                    agent_ref, namespace, sequence, "info",
                    f"📚 Skill loaded: **{name}**",
                ))
                sequence += 1

        # Sub-agent events
        elif etype in ("subagent.started", "subagent.completed", "subagent.failed"):
            agent_name = getattr(data, "agent_display_name", "") or getattr(data, "agent_name", "?")
            if chunk_url and send_ref:
                if etype == "subagent.started":
                    msg = f"🤖 Sub-agent started: **{agent_name}**"
                elif etype == "subagent.completed":
                    msg = f"✅ Sub-agent completed: **{agent_name}**"
                else:
                    err = getattr(data, "error", "unknown error")
                    msg = f"❌ Sub-agent failed: **{agent_name}** — {err}"
                asyncio.get_event_loop().create_task(_post_chunk(
                    chunk_url, send_ref, resolved_session_id or session_id,
                    agent_ref, namespace, sequence, "info", msg,
                ))
                sequence += 1

        # Session idle = done
        elif etype == "session.idle":
            done.set()

        # Session disconnected (cancelled)
        elif etype == "session.deleted":
            cancelled = True
            done.set()

    session.on(on_event)

    # Send the message
    await session.send(message)

    # Wait for completion (generous timeout for complex tool-use chains)
    try:
        await asyncio.wait_for(done.wait(), timeout=300)
    except asyncio.TimeoutError:
        if chunk_url and send_ref:
            await _post_chunk(
                chunk_url, send_ref, resolved_session_id or session_id,
                agent_ref, namespace, sequence, "error",
                "⏱️ Request timed out after 300s",
            )
        await session.disconnect()
        raise HTTPException(status_code=504, detail="SDK session timed out")
    finally:
        if queue_id:
            _active_sessions.pop(queue_id, None)

    if cancelled:
        cancelled_msg = "⛔ Request cancelled by user."
        if chunk_url and send_ref:
            await _post_chunk(
                chunk_url, send_ref, resolved_session_id or session_id,
                agent_ref, namespace, sequence, "error", cancelled_msg,
            )
        return cancelled_msg, resolved_session_id or session_id or ""

    # Disconnect session (cleanup) — don't delete CLI state
    await session.disconnect()

    if not response_text:
        response_text = "No response captured"

    # Flush any remaining thinking text that didn't end with punctuation
    if _thinking_buffer.strip() and chunk_url and send_ref:
        await _post_chunk(
            chunk_url, send_ref, resolved_session_id or session_id,
            agent_ref, namespace, sequence, "thinking",
            f"🤔 {_thinking_buffer.strip()[:300]}",
        )

    return response_text, resolved_session_id or session_id or ""


# ── Async queue processing ───────────────────────────────────────────────────

_queue: asyncio.Queue = asyncio.Queue()


async def _process_queue():
    """Background worker: process async chat requests with bounded concurrency."""
    while True:
        item = await _queue.get()
        asyncio.create_task(_handle_async_item(item))


async def _handle_async_item(item: dict):
    """Process a single async chat item under the concurrency semaphore."""
    queue_id = item["queue_id"]
    async with _concurrency:
        try:
            response_text, resolved_session_id = await _run_sdk_streaming(
                message=item["message"],
                session_id=item.get("session_id"),
                send_ref=item.get("send_ref"),
                namespace=item.get("namespace"),
                agent_ref=item.get("agent_ref"),
                queue_id=queue_id,
                session_config=item.get("session_config"),
            )

            history = load_session(resolved_session_id)
            history.append({"user": item["message"], "assistant": response_text})
            save_session(resolved_session_id, history)

            if WEBHOOK_URL:
                payload = {
                    "queue_id": queue_id,
                    "session_id": resolved_session_id,
                    "prompt": item["message"],
                    "response": response_text,
                    "send_ref": item.get("send_ref"),
                    "namespace": item.get("namespace"),
                    "agent_ref": item.get("agent_ref"),
                }
                try:
                    async with httpx.AsyncClient(timeout=10.0) as http:
                        await http.post(WEBHOOK_URL, json=payload)
                except Exception as e:
                    print(f"[asyncchat] webhook POST failed for queue_id={queue_id}: {e}")
        except Exception as e:
            print(f"[asyncchat] processing failed for queue_id={queue_id}: {e}")
        finally:
            _active_sessions.pop(queue_id, None)
            _queue.task_done()


# ── Background Task Framework ────────────────────────────────────────────────

_background_tasks: dict[str, dict] = {}
_task_runner_started = False


def _load_tasks() -> dict[str, dict]:
    """Load persisted tasks from disk."""
    if TASKS_FILE.exists():
        try:
            data = json.loads(TASKS_FILE.read_text())
            if isinstance(data, dict):
                return data
        except (json.JSONDecodeError, OSError):
            pass
    return {}


def _save_tasks():
    """Persist active tasks to disk for pod restart survival."""
    serialisable = {}
    for tid, task in _background_tasks.items():
        serialisable[tid] = {k: v for k, v in task.items() if k != "_asyncio_task"}
    try:
        TASKS_FILE.write_text(json.dumps(serialisable, indent=2))
    except OSError as e:
        print(f"[tasks] Failed to persist tasks: {e}")


async def _post_notification(
    session_id: str, agent_ref: str, namespace: str,
    message: str, notification_type: str = "info",
    title: str = "", task_ref: str = "",
):
    """POST a one-way notification to the operator webhook."""
    notification_url = WEBHOOK_URL.replace("/response", "/notification") if WEBHOOK_URL else ""
    if not notification_url:
        print(f"[tasks] No WEBHOOK_URL configured, cannot send notification")
        return
    try:
        async with httpx.AsyncClient(timeout=10.0) as http:
            await http.post(notification_url, json={
                "session_id": session_id,
                "agent_ref": agent_ref,
                "namespace": namespace,
                "message": message,
                "notification_type": notification_type,
                "title": title,
                "task_ref": task_ref,
            })
    except Exception as e:
        print(f"[tasks] Notification POST failed: {e}")


async def _check_resource_condition(task: dict) -> bool:
    """Check if a Kubernetes resource meets the desired condition.

    Connects to the cluster API server using the in-cluster or local kubeconfig
    and inspects status.conditions on the target resource.
    """
    config = task.get("config", {})
    resource_type = config.get("resource_type", "nodes")
    resource_name = config.get("resource_name", "")
    condition_type = config.get("condition_type", "Ready")
    condition_status = config.get("condition_status", "True")
    api_version = config.get("api_version", "v1")
    resource_namespace = config.get("resource_namespace", "")

    # Build the API URL
    kube_host = os.environ.get("KUBERNETES_SERVICE_HOST", "")
    kube_port = os.environ.get("KUBERNETES_SERVICE_PORT", "443")

    if not kube_host:
        print(f"[tasks] Not running in cluster, skipping resource check")
        return False

    base_url = f"https://{kube_host}:{kube_port}"
    token_path = Path("/var/run/secrets/kubernetes.io/serviceaccount/token")
    ca_path = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

    if not token_path.exists():
        print(f"[tasks] No service account token found")
        return False

    token = token_path.read_text().strip()
    headers = {"Authorization": f"Bearer {token}"}

    # Build resource URL
    if api_version == "v1":
        api_prefix = f"{base_url}/api/v1"
    else:
        api_prefix = f"{base_url}/apis/{api_version}"

    if resource_namespace:
        url = f"{api_prefix}/namespaces/{resource_namespace}/{resource_type}/{resource_name}"
    else:
        url = f"{api_prefix}/{resource_type}/{resource_name}"

    try:
        async with httpx.AsyncClient(verify=ca_path, timeout=10.0) as http:
            resp = await http.get(url, headers=headers)
            if resp.status_code != 200:
                print(f"[tasks] Resource check failed: HTTP {resp.status_code}")
                return False

            data = resp.json()
            conditions = data.get("status", {}).get("conditions", [])
            for cond in conditions:
                if cond.get("type") == condition_type and cond.get("status") == condition_status:
                    return True
    except Exception as e:
        print(f"[tasks] Resource check error: {e}")
    return False


async def _check_pod_phase(task: dict) -> bool:
    """Check if a pod has reached the target phase."""
    config = task.get("config", {})
    pod_name = config.get("pod_name", "")
    pod_namespace = config.get("pod_namespace", "default")
    target_phase = config.get("target_phase", "Running")

    kube_host = os.environ.get("KUBERNETES_SERVICE_HOST", "")
    kube_port = os.environ.get("KUBERNETES_SERVICE_PORT", "443")

    if not kube_host:
        return False

    base_url = f"https://{kube_host}:{kube_port}"
    token_path = Path("/var/run/secrets/kubernetes.io/serviceaccount/token")
    ca_path = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"

    if not token_path.exists():
        return False

    token = token_path.read_text().strip()
    headers = {"Authorization": f"Bearer {token}"}
    url = f"{base_url}/api/v1/namespaces/{pod_namespace}/pods/{pod_name}"

    try:
        async with httpx.AsyncClient(verify=ca_path, timeout=10.0) as http:
            resp = await http.get(url, headers=headers)
            if resp.status_code != 200:
                return False
            data = resp.json()
            phase = data.get("status", {}).get("phase", "")
            return phase == target_phase
    except Exception as e:
        print(f"[tasks] Pod phase check error: {e}")
    return False


_TASK_CHECKERS = {
    "monitor_resource": _check_resource_condition,
    "monitor_pod_phase": _check_pod_phase,
}


async def _run_background_task(task_id: str):
    """Background loop that periodically checks the task condition."""
    task = _background_tasks.get(task_id)
    if not task:
        return

    check_interval = task.get("check_interval", 30)
    timeout = task.get("timeout", 3600)
    task_type = task.get("task_type", "monitor_resource")
    checker = _TASK_CHECKERS.get(task_type)

    if not checker:
        task["status"] = "failed"
        task["error"] = f"Unknown task type: {task_type}"
        _save_tasks()
        return

    elapsed = 0
    task["status"] = "running"
    _save_tasks()

    while elapsed < timeout:
        await asyncio.sleep(check_interval)
        elapsed += check_interval

        # Check if task was cancelled
        if task_id not in _background_tasks:
            return

        try:
            condition_met = await checker(task)
        except Exception as e:
            print(f"[tasks] Check failed for {task_id}: {e}")
            condition_met = False

        if condition_met:
            task["status"] = "completed"
            _save_tasks()

            # Send notification
            msg = task.get("notification_message", "Background task completed")
            notif_type = task.get("notification_type", "success")
            title = task.get("title", "Task Completed")
            await _post_notification(
                session_id=task.get("session_id", ""),
                agent_ref=task.get("agent_ref", ""),
                namespace=task.get("namespace", ""),
                message=msg,
                notification_type=notif_type,
                title=title,
                task_ref=task_id,
            )
            return

    # Timeout reached
    task["status"] = "timed_out"
    _save_tasks()
    await _post_notification(
        session_id=task.get("session_id", ""),
        agent_ref=task.get("agent_ref", ""),
        namespace=task.get("namespace", ""),
        message=f"Background monitoring task timed out after {timeout}s",
        notification_type="warning",
        title="Task Timed Out",
        task_ref=task_id,
    )


def _restore_tasks():
    """Restore persisted tasks on startup and re-launch runners."""
    global _background_tasks
    loaded = _load_tasks()
    for tid, task in loaded.items():
        if task.get("status") == "running":
            _background_tasks[tid] = task
            asyncio.create_task(_run_background_task(tid))
        elif task.get("status") in ("completed", "timed_out", "failed"):
            _background_tasks[tid] = task


# ── FastAPI lifecycle ────────────────────────────────────────────────────────

@app.on_event("startup")
async def startup_event():
    # Copy skills from read-only ConfigMap staging into writable PVC
    if SKILLS_STAGING_DIR.exists():
        for item in SKILLS_STAGING_DIR.iterdir():
            dest = SKILLS_DIR / item.name
            if item.is_dir():
                shutil.copytree(item, dest, dirs_exist_ok=True)
            else:
                dest.parent.mkdir(parents=True, exist_ok=True)
                shutil.copy2(item, dest)

    # Copy AGENT.md from read-only ConfigMap staging into writable PVC.
    # Only copy if the destination is missing or empty (preserve user edits).
    agent_src = AGENT_STAGING_DIR / "AGENT.md"
    if agent_src.exists():
        if not INSTRUCTIONS_FILE.exists() or INSTRUCTIONS_FILE.stat().st_size == 0:
            shutil.copy2(agent_src, INSTRUCTIONS_FILE)

    asyncio.create_task(_process_queue())
    _restore_tasks()


@app.on_event("shutdown")
async def shutdown_event():
    global _client
    if _client:
        await _client.stop()
        _client = None


# ── Health ───────────────────────────────────────────────────────────────────

@app.get("/health")
def health():
    return {"status": "ok"}


# ── Models endpoint ──────────────────────────────────────────────────────────

@app.get("/models")
async def list_models():
    """Return available models from the Copilot CLI."""
    try:
        client = await _get_client()
        models = await client.list_models()
        return {"models": models}
    except Exception as e:
        return {"models": [], "error": str(e)}


# ── Async chat ───────────────────────────────────────────────────────────────

@app.post("/asyncchat")
async def async_chat(req: AsyncChatRequest):
    """Fire-and-forget: enqueue message for background processing."""
    queue_id = str(uuid.uuid4())
    await _queue.put({
        "queue_id": queue_id,
        "message": req.message,
        "session_id": req.session_id,
        "send_ref": req.send_ref,
        "namespace": req.namespace,
        "agent_ref": req.agent_ref,
        "session_config": req.session_config,
    })
    return {"queue_id": queue_id, "status": "queued"}


# ── Cancel ───────────────────────────────────────────────────────────────────

@app.delete("/cancel/{queue_id}")
async def cancel_queue_item(queue_id: str):
    """Disconnect the SDK session for the given queue_id."""
    session = _active_sessions.get(queue_id)
    if session is None:
        return {"status": "not_found", "queue_id": queue_id}
    try:
        await session.disconnect()
        print(f"[cancel] disconnected session for queue_id={queue_id}")
    except Exception as e:
        print(f"[cancel] disconnect failed for queue_id={queue_id}: {e}")
    _active_sessions.pop(queue_id, None)
    return {"status": "cancelled", "queue_id": queue_id}


# ── Sync chat ────────────────────────────────────────────────────────────────

@app.post("/chat", response_model=ChatResponse)
async def chat(req: ChatRequest):
    """Synchronous chat — blocks until the agent responds."""
    client = await _get_client()
    opts = await _build_session_opts(req.session_config)

    if req.session_id:
        session = await client.resume_session(req.session_id, **opts)
    else:
        session = await client.create_session(**opts)

    response_text = ""
    resolved_session_id = getattr(session, "session_id", None) or req.session_id or ""
    done = asyncio.Event()

    def on_event(event):
        nonlocal response_text, resolved_session_id
        etype = event.type.value if hasattr(event.type, "value") else str(event.type)
        data = event.data if hasattr(event, "data") else None

        if etype == "session.created" and data and hasattr(data, "session_id"):
            resolved_session_id = data.session_id or resolved_session_id
        elif etype == "assistant.message":
            content = getattr(data, "content", "") or ""
            tool_requests = getattr(data, "tool_requests", None)
            if content and not tool_requests:
                response_text = content
        elif etype == "session.idle":
            done.set()

    session.on(on_event)
    await session.send(req.message)

    try:
        await asyncio.wait_for(done.wait(), timeout=120)
    except asyncio.TimeoutError:
        await session.disconnect()
        raise HTTPException(status_code=504, detail="Copilot SDK timed out")

    await session.disconnect()

    session_id = resolved_session_id or req.session_id or ""

    # Migrate history if session IDs don't match
    if req.session_id and resolved_session_id and req.session_id != resolved_session_id:
        old_path = SESSIONS_DIR / f"{req.session_id}.json"
        new_path = SESSIONS_DIR / f"{resolved_session_id}.json"
        if old_path.exists() and not new_path.exists():
            old_path.rename(new_path)

    history = load_session(session_id)
    history.append({"user": req.message, "assistant": response_text or "No response"})
    save_session(session_id, history)

    return ChatResponse(response=response_text or "No response", session_id=session_id)


# ── Background Task API ──────────────────────────────────────────────────────

class MonitorTaskRequest(BaseModel):
    session_id: str
    agent_ref: str = ""
    namespace: str = ""
    task_type: str = "monitor_resource"
    config: dict = {}
    check_interval: int = 30
    timeout: int = 3600
    notification_message: str = "Background task completed"
    notification_type: str = "success"
    title: str = "Task Completed"


@app.post("/tasks/monitor")
async def create_monitor_task(req: MonitorTaskRequest):
    """Register a background monitoring task."""
    task_id = f"task-{uuid.uuid4().hex[:12]}"

    if req.task_type not in _TASK_CHECKERS:
        raise HTTPException(
            status_code=400,
            detail=f"Unknown task_type: {req.task_type}. Available: {list(_TASK_CHECKERS.keys())}",
        )

    task = {
        "task_id": task_id,
        "session_id": req.session_id,
        "agent_ref": req.agent_ref,
        "namespace": req.namespace,
        "task_type": req.task_type,
        "config": req.config,
        "check_interval": max(req.check_interval, 5),
        "timeout": min(req.timeout, 86400),
        "notification_message": req.notification_message,
        "notification_type": req.notification_type,
        "title": req.title,
        "status": "pending",
    }

    _background_tasks[task_id] = task
    _save_tasks()
    asyncio.create_task(_run_background_task(task_id))

    return {"task_id": task_id, "status": "created"}


@app.get("/tasks")
def list_tasks():
    """List all background tasks."""
    tasks = []
    for tid, task in _background_tasks.items():
        tasks.append({
            "task_id": tid,
            "task_type": task.get("task_type", ""),
            "status": task.get("status", ""),
            "session_id": task.get("session_id", ""),
            "title": task.get("title", ""),
            "config": task.get("config", {}),
        })
    return {"tasks": tasks}


@app.get("/tasks/{task_id}")
def get_task(task_id: str):
    """Get details of a specific background task."""
    task = _background_tasks.get(task_id)
    if not task:
        raise HTTPException(status_code=404, detail="Task not found")
    return {
        "task_id": task_id,
        "task_type": task.get("task_type", ""),
        "status": task.get("status", ""),
        "session_id": task.get("session_id", ""),
        "agent_ref": task.get("agent_ref", ""),
        "title": task.get("title", ""),
        "config": task.get("config", {}),
        "check_interval": task.get("check_interval", 30),
        "timeout": task.get("timeout", 3600),
        "notification_message": task.get("notification_message", ""),
    }


@app.delete("/tasks/{task_id}")
def delete_task(task_id: str):
    """Cancel and remove a background task."""
    task = _background_tasks.pop(task_id, None)
    if not task:
        raise HTTPException(status_code=404, detail="Task not found")
    _save_tasks()
    return {"status": "deleted", "task_id": task_id}


# ══════════════════════════════════════════════════════════════════════════════
# File management endpoints for skills, instructions, custom agents
# ══════════════════════════════════════════════════════════════════════════════

def _parse_skill_frontmatter(content: str) -> tuple[str, str]:
    """Extract name and description from YAML frontmatter in a SKILL.md."""
    name, description = "", ""
    if content.startswith("---"):
        parts = content.split("---", 2)
        if len(parts) >= 3:
            for line in parts[1].strip().splitlines():
                line = line.strip()
                if line.startswith("name:"):
                    name = line[len("name:"):].strip().strip('"').strip("'")
                elif line.startswith("description:"):
                    description = line[len("description:"):].strip().strip('"').strip("'")
    return name, description


# ── Instructions ─────────────────────────────────────────────────────────────

@app.get("/config/instructions")
def get_instructions():
    if INSTRUCTIONS_FILE.exists():
        return {"content": INSTRUCTIONS_FILE.read_text()}
    return {"content": ""}


@app.put("/config/instructions")
async def put_instructions(request: Request):
    body = await request.json()
    content = body.get("content", "")
    INSTRUCTIONS_FILE.write_text(content)
    return {"status": "saved"}


# ── Skills ───────────────────────────────────────────────────────────────────

@app.get("/config/skills")
def list_skills():
    skills = []
    if SKILLS_DIR.exists():
        for skill_dir in sorted(SKILLS_DIR.iterdir()):
            skill_file = skill_dir / "SKILL.md"
            if skill_dir.is_dir() and skill_file.exists():
                content = skill_file.read_text()
                name, description = _parse_skill_frontmatter(content)
                skills.append({
                    "name": name or skill_dir.name,
                    "dir_name": skill_dir.name,
                    "description": description,
                    "content": content,
                })
    return {"skills": skills}


@app.get("/config/skills/{skill_name}")
def get_skill(skill_name: str):
    if "/" in skill_name or "\\" in skill_name or ".." in skill_name:
        raise HTTPException(status_code=400, detail="Invalid skill name")
    skill_file = SKILLS_DIR / skill_name / "SKILL.md"
    if not skill_file.exists():
        raise HTTPException(status_code=404, detail="Skill not found")
    content = skill_file.read_text()
    name, description = _parse_skill_frontmatter(content)
    return {
        "name": name or skill_name,
        "dir_name": skill_name,
        "description": description,
        "content": content,
    }


@app.put("/config/skills/{skill_name}")
async def put_skill(skill_name: str, request: Request):
    if "/" in skill_name or "\\" in skill_name or ".." in skill_name:
        raise HTTPException(status_code=400, detail="Invalid skill name")
    body = await request.json()
    content = body.get("content", "")
    skill_dir = SKILLS_DIR / skill_name
    skill_dir.mkdir(parents=True, exist_ok=True)
    (skill_dir / "SKILL.md").write_text(content)
    return {"status": "saved", "name": skill_name}


@app.delete("/config/skills/{skill_name}")
def delete_skill(skill_name: str):
    if "/" in skill_name or "\\" in skill_name or ".." in skill_name:
        raise HTTPException(status_code=400, detail="Invalid skill name")
    skill_dir = SKILLS_DIR / skill_name
    if not skill_dir.exists():
        raise HTTPException(status_code=404, detail="Skill not found")
    shutil.rmtree(skill_dir)
    return {"status": "deleted", "name": skill_name}


# ── Custom agents ────────────────────────────────────────────────────────────

@app.get("/config/agents")
def get_custom_agents():
    agents = _load_custom_agents_from_file()
    return {"agents": agents}


@app.put("/config/agents")
async def put_custom_agents(request: Request):
    body = await request.json()
    agents = body.get("agents", [])
    if not isinstance(agents, list):
        raise HTTPException(status_code=400, detail="agents must be a list")
    CUSTOM_AGENTS_FILE.write_text(json.dumps(agents, indent=2))
    return {"status": "saved"}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8080)
