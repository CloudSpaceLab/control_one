// Package tracing initializes OpenTelemetry tracing for the control plane.
// It intentionally depends only on the already-vendored contrib packages and
// emits spans via OTLP HTTP when OTEL_EXPORTER_OTLP_ENDPOINT is set. With no
// endpoint configured the Init call is a no-op and returns a shutdown that
// does nothing, so adding tracing to the server has zero impact on users
// who have not opted in.
package tracing

import (
	"context"
	"fmt"
	"os"
	"strings"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

// Options controls tracer init. Fields may be empty; Init falls back to
// environment variables (OTEL_EXPORTER_OTLP_ENDPOINT, OTEL_SERVICE_NAME).
type Options struct {
	Endpoint    string
	ServiceName string
	Insecure    bool
}

// Init configures a global tracer provider. Returns a shutdown function that
// the caller must run on process exit to flush spans. Safe to call even when
// tracing is disabled — in that case shutdown is a no-op.
func Init(ctx context.Context, opts Options) (func(context.Context) error, error) {
	endpoint := strings.TrimSpace(opts.Endpoint)
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}
	if endpoint == "" {
		// Tracing disabled — return a no-op shutdown.
		return func(context.Context) error { return nil }, nil
	}

	service := opts.ServiceName
	if service == "" {
		if v := os.Getenv("OTEL_SERVICE_NAME"); v != "" {
			service = v
		} else {
			service = "control-one-controlplane"
		}
	}

	exporterOpts := []otlptracehttp.Option{otlptracehttp.WithEndpoint(stripScheme(endpoint))}
	if opts.Insecure || strings.HasPrefix(endpoint, "http://") {
		exporterOpts = append(exporterOpts, otlptracehttp.WithInsecure())
	}
	exp, err := otlptrace.New(ctx, otlptracehttp.NewClient(exporterOpts...))
	if err != nil {
		return nil, fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(service)),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	tp := tracesdk.NewTracerProvider(
		tracesdk.WithBatcher(exp),
		tracesdk.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp.Shutdown, nil
}

func stripScheme(endpoint string) string {
	for _, prefix := range []string{"http://", "https://"} {
		if strings.HasPrefix(endpoint, prefix) {
			return endpoint[len(prefix):]
		}
	}
	return endpoint
}
