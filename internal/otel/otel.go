package otel

// TODO: wire up OpenTelemetry tracing/exporters once observability requirements are defined.

// Init sets up tracing and returns a shutdown function.
func Init() (func(), error) {
	return func() {}, nil
}
