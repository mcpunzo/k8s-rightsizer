package ctxkeys

import "context"

type contextKey string

const (
	DryRunKey           contextKey = "dryRun"
	ResizeOnRecreateKey contextKey = "resizeOnRecreate"
	NumberOfWorkersKey  contextKey = "numberOfWorkers"
)

// WithDryRun adds the dry run flag to the context.
func WithDryRun(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, DryRunKey, enabled)
}

// DryRunFromContext retrieves the dry run flag from the context. It returns false if the flag is not set or if the value is not a boolean.
func DryRunFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(DryRunKey).(bool); ok {
		return v
	}
	return false
}

// WithResizeOnRecreate adds the resize on recreate flag to the context.
func WithResizeOnRecreate(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, ResizeOnRecreateKey, enabled)
}

// ResizeOnRecreateFromContext retrieves the resize on recreate flag from the context. It returns false if the flag is not set or if the value is not a boolean.
func ResizeOnRecreateFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(ResizeOnRecreateKey).(bool); ok {
		return v
	}
	return false
}

// WithNumberOfWorkers adds the number of workers to the context.
func WithNumberOfWorkers(ctx context.Context, workers int) context.Context {
	return context.WithValue(ctx, NumberOfWorkersKey, workers)
}

// NumberOfWorkersFromContext retrieves the number of workers from the context. It returns the default number of workers if the value is not set or if it is not an integer.
func NumberOfWorkersFromContext(ctx context.Context, defaultWorkers int) int {
	if v, ok := ctx.Value(NumberOfWorkersKey).(int); ok {
		return v
	}
	return defaultWorkers
}

// WithUseLimits adds the use limits flag to the context.
func WithUseLimits(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, "useLimits", enabled)
}

// UseLimitsFromContext retrieves the use limits flag from the context. It returns false if the flag is not set or if the value is not a boolean.
func UseLimitsFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value("useLimits").(bool); ok {
		return v
	}
	return false
}
