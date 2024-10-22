package tracing

import (
	"context"
	"time"

	"github.com/uptrace/uptrace-go/uptrace"
	"go.opentelemetry.io/contrib/propagators/aws/xray"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/encoding/gzip"
)

func InitTracing(ctx context.Context) (sdktrace.SpanProcessor, error) {
	dsn := "https://r9bxofQ3DPgtrPmizIdO_g@api.uptrace.dev?grpc=4317"
	uptrace.ConfigureOpentelemetry(
		uptrace.WithDSN(dsn),
		uptrace.WithServiceName("cosmos-indexer"),
		uptrace.WithServiceVersion("1.0.0"),
	)

	creds := credentials.NewClientTLSFromCert(nil, "")
	exporter, err := otlptracegrpc.New(
		ctx,
		otlptracegrpc.WithEndpoint("otlp.uptrace.dev:4317"),
		otlptracegrpc.WithTLSCredentials(creds),
		otlptracegrpc.WithHeaders(map[string]string{
			// Set the Uptrace DSN here or use UPTRACE_DSN env var.
			"uptrace-dsn": dsn,
		}),
		otlptracegrpc.WithCompressor(gzip.Name),
	)
	if err != nil {
		return nil, err
	}

	bsp := sdktrace.NewBatchSpanProcessor(exporter,
		sdktrace.WithMaxQueueSize(10_000),
		sdktrace.WithMaxExportBatchSize(10_000))

	resource, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithHost(),
		resource.WithAttributes(
			attribute.String("service.name", "cosmos-indexer"),
			attribute.String("service.version", "1.0.0"),
		))
	if err != nil {
		return nil, err
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(resource),
		sdktrace.WithIDGenerator(xray.NewIDGenerator()),
	)
	tracerProvider.RegisterSpanProcessor(bsp)
	otel.SetTracerProvider(tracerProvider)

	// meter provider

	exporterMetric, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint("otlp.uptrace.dev:4317"),
		otlpmetricgrpc.WithHeaders(map[string]string{
			// Set the Uptrace DSN here or use UPTRACE_DSN env var.
			"uptrace-dsn": dsn,
		}),
		otlpmetricgrpc.WithCompressor(gzip.Name),
		otlpmetricgrpc.WithTemporalitySelector(preferDeltaTemporalitySelector),
	)
	if err != nil {
		return nil, err
	}

	reader := sdkmetric.NewPeriodicReader(
		exporterMetric,
		sdkmetric.WithInterval(15*time.Second),
	)
	provider := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(reader),
		sdkmetric.WithResource(resource),
	)
	otel.SetMeterProvider(provider)

	return bsp, nil
}

func preferDeltaTemporalitySelector(kind sdkmetric.InstrumentKind) metricdata.Temporality {
	switch kind {
	case sdkmetric.InstrumentKindCounter,
		sdkmetric.InstrumentKindObservableCounter,
		sdkmetric.InstrumentKindHistogram:
		return metricdata.DeltaTemporality
	default:
		return metricdata.CumulativeTemporality
	}
}
