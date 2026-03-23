{{/*
Expand the name of the chart.
*/}}
{{- define "github-copilot-agent.name" -}}
{{- .Values.name | default .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Full name (used for secrets and configmaps created by this chart).
*/}}
{{- define "github-copilot-agent.fullname" -}}
{{- .Values.name | default .Chart.Name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "github-copilot-agent.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "github-copilot-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Name of the GitHub token secret to reference in the CR.
*/}}
{{- define "github-copilot-agent.tokenSecretName" -}}
{{- if .Values.githubToken.existingSecret -}}
{{ .Values.githubToken.existingSecret }}
{{- else -}}
{{ include "github-copilot-agent.fullname" . }}-github-token
{{- end }}
{{- end }}

{{/*
Name of the skills ConfigMap to reference in the CR.
*/}}
{{- define "github-copilot-agent.skillsConfigMapName" -}}
{{- if .Values.skillsConfigMap -}}
{{ .Values.skillsConfigMap }}
{{- else if .Values.createSkillsConfigMap -}}
{{ include "github-copilot-agent.fullname" . }}-skills
{{- end }}
{{- end }}

{{/*
Name of the agent ConfigMap to reference in the CR.
*/}}
{{- define "github-copilot-agent.agentConfigMapName" -}}
{{- if .Values.agentConfigMap -}}
{{ .Values.agentConfigMap }}
{{- else if .Values.createAgentConfigMap -}}
{{ include "github-copilot-agent.fullname" . }}-agent
{{- end }}
{{- end }}
