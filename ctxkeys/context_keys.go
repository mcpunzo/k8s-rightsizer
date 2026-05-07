package ctxkeys

import "context"

type contextKey string

const (
	DryRunKey           contextKey = "dryRun"
	ResizeOnRecreateKey contextKey = "resizeOnRecreate"
	NumberOfWorkersKey  contextKey = "numberOfWorkers"
)

func WithDryRun(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, DryRunKey, enabled)
}

func DryRunFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(DryRunKey).(bool); ok {
		return v
	}
	return false
}

func WithResizeOnRecreate(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, ResizeOnRecreateKey, enabled)
}

func ResizeOnRecreateFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(ResizeOnRecreateKey).(bool); ok {
		return v
	}
	return false
}

func WithNumberOfWorkers(ctx context.Context, workers int) context.Context {
	return context.WithValue(ctx, NumberOfWorkersKey, workers)
}

func NumberOfWorkersFromContext(ctx context.Context, defaultWorkers int) int {
	if v, ok := ctx.Value(NumberOfWorkersKey).(int); ok {
		return v
	}
	return defaultWorkers
}
