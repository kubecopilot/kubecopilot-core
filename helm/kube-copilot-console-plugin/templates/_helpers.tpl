{{/*
Expand the name of the chart.
*/}}
{{- define "kube-copilot-console-plugin.name" -}}
{{- default (default .Chart.Name .Release.Name) .Values.plugin.name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kube-copilot-console-plugin.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "kube-copilot-console-plugin.labels" -}}
helm.sh/chart: {{ include "kube-copilot-console-plugin.chart" . }}
{{ include "kube-copilot-console-plugin.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "kube-copilot-console-plugin.selectorLabels" -}}
app: {{ include "kube-copilot-console-plugin.name" . }}
app.kubernetes.io/name: {{ include "kube-copilot-console-plugin.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/part-of: {{ include "kube-copilot-console-plugin.name" . }}
{{- end }}

{{/*
Create the name of the TLS certificate secret
*/}}
{{- define "kube-copilot-console-plugin.certificateSecret" -}}
{{ default (printf "%s-cert" (include "kube-copilot-console-plugin.name" .)) .Values.plugin.certificateSecretName }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "kube-copilot-console-plugin.serviceAccountName" -}}
{{- if .Values.plugin.serviceAccount.create }}
{{- default (include "kube-copilot-console-plugin.name" .) .Values.plugin.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.plugin.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Create the name of the patcher service account
*/}}
{{- define "kube-copilot-console-plugin.patcherName" -}}
{{- printf "%s-patcher" (include "kube-copilot-console-plugin.name" .) }}
{{- end }}

{{/*
Create the name of the patcher service account to use
*/}}
{{- define "kube-copilot-console-plugin.patcherServiceAccountName" -}}
{{- if .Values.plugin.patcherServiceAccount.create }}
{{- default (printf "%s-patcher" (include "kube-copilot-console-plugin.name" .)) .Values.plugin.patcherServiceAccount.name }}
{{- else }}
{{- default "default" .Values.plugin.patcherServiceAccount.name }}
{{- end }}
{{- end }}

{{/*
Web UI service namespace
*/}}
{{- define "kube-copilot-console-plugin.webUINamespace" -}}
{{- default .Release.Namespace .Values.webUI.serviceNamespace }}
{{- end }}
