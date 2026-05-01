# Observability — OpenTelemetry Tracing & Metrics

KubeCopilot ships with built-in OpenTelemetry (OTEL) support. When an OTLP
collector endpoint is configured, every stage of the agent lifecycle — from
CRD creation through controller reconcile, HTTP webhook, and agent tool
execution — emits a correlated span. All spans flow into any
OTLP-compatible observability backend.

## Architecture

```
KubeCopilotSend CR
        │
        ▼
[controller.Reconcile span]          ← Go operator (this process)
        │ HTTP POST /asyncchat
        ▼
[asyncchat.process span]             ← Python agent server
        │
        ├── [tool.<name> spans]      ← each CLI/API tool call
        │
        └── HTTP POST /response → [webhook handler span]
```

The Python agent server also emits `tool.<tool_name>` child spans for every
tool execution, allowing precise measurement of individual tool latency.

## Configuration

Tracing is controlled entirely through standard OpenTelemetry environment
variables. No code changes are required.

| Variable | Description |
|---|---|
| `OTEL_EXPORTER_OTLP_ENDPOINT` | OTLP collector endpoint. gRPC when the value is `host:port` or `grpc://…`; HTTP when it starts with `http://` or `https://`. Leave unset to disable tracing (no-op). |
| `OTEL_SERVICE_NAME` | Service name attached to operator spans. Default: `kube-copilot-agent`. |

Agent pods receive `OTEL_EXPORTER_OTLP_ENDPOINT` automatically when the
operator is configured (the controller forwards its own value). The agent
server uses `OTEL_SERVICE_NAME` for its own spans (default:
`kube-copilot-agent-server`).

## Helm Installation

```yaml
# values.yaml
tracing:
  enabled: true
  endpoint: "jaeger-collector.observability.svc.cluster.local:4317"
  operatorServiceName: "kube-copilot-operator"
  agentServiceName: "kube-copilot-agent-server"
```

```bash
helm upgrade --install kube-copilot-agent ./helm/kube-copilot-agent \
  --set tracing.enabled=true \
  --set tracing.endpoint="jaeger-collector.observability:4317"
```

## Backend Setup Examples

### Jaeger (all-in-one)

```bash
kubectl create namespace observability

kubectl apply -n observability -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
spec:
  selector:
    matchLabels:
      app: jaeger
  template:
    metadata:
      labels:
        app: jaeger
    spec:
      containers:
      - name: jaeger
        image: jaegertracing/all-in-one:latest
        ports:
        - containerPort: 16686   # UI
        - containerPort: 4317    # OTLP gRPC
        - containerPort: 4318    # OTLP HTTP
        env:
        - name: COLLECTOR_OTLP_ENABLED
          value: "true"
---
apiVersion: v1
kind: Service
metadata:
  name: jaeger
spec:
  selector:
    app: jaeger
  ports:
  - name: ui
    port: 16686
  - name: otlp-grpc
    port: 4317
  - name: otlp-http
    port: 4318
EOF
```

Configure KubeCopilot to send to Jaeger:

```yaml
tracing:
  enabled: true
  endpoint: "jaeger.observability.svc.cluster.local:4317"
```

Access the Jaeger UI:

```bash
kubectl port-forward -n observability svc/jaeger 16686:16686
# open http://localhost:16686
```

### Grafana Tempo + Grafana (OTLP HTTP)

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm upgrade --install tempo grafana/tempo \
  --namespace observability --create-namespace \
  --set tempo.receivers.otlp.protocols.http.endpoint="0.0.0.0:4318"

helm upgrade --install grafana grafana/grafana \
  --namespace observability \
  --set datasources."datasources\.yaml".apiVersion=1 \
  --set-json 'datasources."datasources\.yaml".datasources=[{"name":"Tempo","type":"tempo","url":"http://tempo:3100","access":"proxy"}]'
```

Configure KubeCopilot for HTTP export to Tempo:

```yaml
tracing:
  enabled: true
  endpoint: "http://tempo.observability.svc.cluster.local:4318"
```

### OpenTelemetry Collector (recommended for production)

Use the [OpenTelemetry Operator](https://opentelemetry.io/docs/kubernetes/operator/)
to deploy a collector that can fan-out to multiple backends:

```yaml
apiVersion: opentelemetry.io/v1alpha1
kind: OpenTelemetryCollector
metadata:
  name: otel-collector
  namespace: observability
spec:
  config: |
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: "0.0.0.0:4317"
          http:
            endpoint: "0.0.0.0:4318"
    exporters:
      jaeger:
        endpoint: "jaeger-collector:14250"
        tls:
          insecure: true
      logging:
        loglevel: debug
    service:
      pipelines:
        traces:
          receivers: [otlp]
          exporters: [jaeger, logging]
```

```yaml
# kube-copilot-agent Helm values
tracing:
  enabled: true
  endpoint: "otel-collector-collector.observability.svc.cluster.local:4317"
```

## Prometheus Metrics

The operator also exposes the following Prometheus metrics via the
`/metrics` endpoint (Kubernetes-native service monitors can scrape these):

| Metric | Type | Labels | Description |
|---|---|---|---|
| `kubecopilot_webhook_requests_total` | Counter | `handler`, `status` | Total requests to the operator webhook server |
| `kubecopilot_webhook_duration_seconds` | Histogram | `handler` | Webhook handler duration |

These are registered with the controller-runtime Prometheus registry and are
exposed alongside the standard controller-runtime metrics.

## Span Reference

| Span Name | Component | Key Attributes |
|---|---|---|
| `KubeCopilotSend.Reconcile` | Go operator | `kubecopilot.send.name`, `.namespace`, `.agent_ref`, `.session_id`, `.queue_id` |
| `response` | Go webhook | `http.method`, `http.status_code` |
| `chunk` | Go webhook | `http.method`, `http.status_code` |
| `notification` | Go webhook | `http.method`, `http.status_code` |
| `asyncchat.process` | Python agent | `kubecopilot.queue_id`, `.agent_ref`, `.session_id`, `.send_ref`, `.duration_ms` |
| `tool.<name>` | Python agent | `kubecopilot.tool.name`, `.session_id`, `.tool.success` |
