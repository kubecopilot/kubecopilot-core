/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package otel provides OpenTelemetry initialisation helpers for the operator.
// Tracing is configured via standard OTEL environment variables:
//
//	OTEL_EXPORTER_OTLP_ENDPOINT  – OTLP collector endpoint (e.g. http://jaeger:4317)
//	OTEL_SERVICE_NAME             – service name reported in traces (default: kube-copilot-agent)
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is empty, a no-op tracer is registered so
// the rest of the code compiles and runs without any OTEL infrastructure.
package otel

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

const defaultServiceName = "kube-copilot-agent"

// ShutdownFunc is returned by Setup and must be called on process exit to
// flush and close the tracer provider.
type ShutdownFunc func(context.Context) error

// Tracer returns a named tracer from the global provider.
func Tracer(name string) trace.Tracer {
	return otel.Tracer(name)
}

// Setup initialises the global OpenTelemetry tracer provider. It reads
// configuration from standard environment variables. If
// OTEL_EXPORTER_OTLP_ENDPOINT is unset or empty, a no-op provider is
// registered and the returned ShutdownFunc is a no-op.
//
// Supported exporters (auto-detected from the endpoint scheme):
//   - gRPC  – endpoint uses grpc:// or :4317 (default)
//   - HTTP  – endpoint uses http:// or https://
func Setup(ctx context.Context) (ShutdownFunc, error) {
	endpoint := strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT"))
	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return func(context.Context) error { return nil }, nil
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = defaultServiceName
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
		resource.WithFromEnv(),
		resource.WithProcess(),
		resource.WithOS(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL resource: %w", err)
	}

	exporter, err := buildExporter(ctx, endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTEL exporter: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	shutdown := func(ctx context.Context) error {
		shutCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		return tp.Shutdown(shutCtx)
	}
	return shutdown, nil
}

// buildExporter creates an OTLP span exporter. HTTP exporters are used when
// the endpoint starts with "http://" or "https://"; gRPC otherwise.
func buildExporter(ctx context.Context, endpoint string) (sdktrace.SpanExporter, error) {
	if strings.HasPrefix(endpoint, "http://") || strings.HasPrefix(endpoint, "https://") {
		return otlptracehttp.New(ctx,
			otlptracehttp.WithEndpointURL(endpoint),
		)
	}
	// gRPC exporter (default for :4317 / grpc:// endpoints)
	return otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
}
