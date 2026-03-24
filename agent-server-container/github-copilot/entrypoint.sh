#!/bin/bash
set -e

# The Copilot CLI checks these env vars in order of precedence:
# COPILOT_GITHUB_TOKEN > GH_TOKEN > GITHUB_TOKEN
export COPILOT_GITHUB_TOKEN="${GITHUB_TOKEN}"
export GH_TOKEN="${GITHUB_TOKEN}"

# All agent state lives under COPILOT_HOME (backed by the PV at /copilot)
export COPILOT_HOME="${COPILOT_HOME:-/copilot}"

# gh CLI and node/copilot binary need writable config/cache dirs
export GH_CONFIG_DIR="${COPILOT_HOME}/.config/gh"
export XDG_CONFIG_HOME="${COPILOT_HOME}/.config"
export XDG_CACHE_HOME="${COPILOT_HOME}/.cache"

# Create required directories
mkdir -p \
  "${COPILOT_HOME}/sessions" \
  "${COPILOT_HOME}/.config/gh" \
  "${COPILOT_HOME}/.cache" \
  "${COPILOT_HOME}/skills" \
  "${COPILOT_HOME}/.kube"

# Restructure skills from ConfigMap staging dir into native Agent Skills format.
# Copilot requires: /copilot/skills/<skill-name>/SKILL.md
# ConfigMap keys can't contain '/', so each key (e.g. kubernetes.md) is
# copied to /copilot/skills/kubernetes/SKILL.md
if [ -d /copilot-skills-staging ]; then
  for f in /copilot-skills-staging/*.md; do
    [ -f "$f" ] || continue
    skill_name="$(basename "$f" .md)"
    mkdir -p "${COPILOT_HOME}/skills/${skill_name}"
    cp "$f" "${COPILOT_HOME}/skills/${skill_name}/SKILL.md"
  done
fi

# Copy agent instructions from ConfigMap staging if present
if [ -f /copilot-agent-md-staging/agent.md ] && [ ! -f "${COPILOT_HOME}/copilot-instructions.md" ]; then
  cp /copilot-agent-md-staging/agent.md "${COPILOT_HOME}/copilot-instructions.md"
fi

# Run the SDK-backed agent server
exec /opt/venv/bin/python /server.py
