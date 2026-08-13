package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

const serviceName = "dispatch"

// ScopeName is the instrumentation scope shared by every span Dispatch emits.
const ScopeName = "github.com/ronitanilkumar/dispatch"

// Attribute keys used across submission, enqueue, and delivery spans.
const (
	AttrJobID      = attribute.Key("dispatch.job.id")
	AttrPriority   = attribute.Key("dispatch.job.priority")
	AttrAttempt    = attribute.Key("dispatch.job.attempt")
	AttrStatusCode = attribute.Key("http.response.status_code")
	AttrHost       = attribute.Key("server.address")
)

// Tracer returns the shared tracer for Dispatch spans.
func Tracer() trace.Tracer {
	return otel.Tracer(ScopeName)
}

// Init wires up the global tracer provider, exporting spans over OTLP/HTTP to
// the collector at endpoint (host:port, no scheme). The returned func flushes
// and shuts the provider down; callers should defer it.
func Init(ctx context.Context, endpoint string) (func(context.Context) error, error) {
	exporter, err := otlptracehttp.New(
		ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)

	if err != nil {
		return nil, fmt.Errorf("build otlp trace exporter: %w", err)
	}

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(serviceName),
		),
	)

	if err != nil {
		return nil, fmt.Errorf("build trace resource: %w", err)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(provider)

	return provider.Shutdown, nil
}
