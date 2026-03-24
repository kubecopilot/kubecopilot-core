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

SESSIONS_DIR.mkdir(parents=True, exist_ok=True)
SKILLS_DIR.mkdir(parents=True, exist_ok=True)

WEBHOOK_URL = os.environ.get("WEBHOOK_URL", "")

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
    base_url: str
    api_key: str | None = None
    bearer_token: str | None = None


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

    if chunk_url and send_ref:
        await _post_chunk(
            chunk_url, send_ref, session_id, agent_ref, namespace,
            sequence, "info", f"Processing: {message[:120]}",
        )
        sequence += 1

    opts = await _build_session_opts(session_config)

    # Create or resume session
    if session_id:
        session = await client.resume_session(session_id, opts)
    else:
        session = await client.create_session(opts)

    # Register for cancellation
    if queue_id:
        _active_sessions[queue_id] = session

    # Collect response via events
    done = asyncio.Event()
    cancelled = False

    def on_event(event):
        nonlocal sequence, response_text, resolved_session_id, cancelled

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

        # Reasoning deltas
        elif etype == "assistant.reasoning_delta":
            delta = getattr(data, "delta_content", "") or ""
            if delta and chunk_url and send_ref:
                asyncio.get_event_loop().create_task(_post_chunk(
                    chunk_url, send_ref, resolved_session_id or session_id,
                    agent_ref, namespace, sequence, "thinking",
                    f"🤔 {delta[:300]}",
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
        return cancelled_msg, resolved_session_id or "unknown"

    # Disconnect session (cleanup) — don't delete CLI state
    await session.disconnect()

    if not response_text:
        response_text = "No response captured"

    return response_text, resolved_session_id or "unknown"


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


# ── FastAPI lifecycle ────────────────────────────────────────────────────────

@app.on_event("startup")
async def startup_event():
    asyncio.create_task(_process_queue())


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
        session = await client.resume_session(req.session_id, opts)
    else:
        session = await client.create_session(opts)

    response_text = ""
    resolved_session_id = req.session_id or ""
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

    session_id = resolved_session_id or "unknown"

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
