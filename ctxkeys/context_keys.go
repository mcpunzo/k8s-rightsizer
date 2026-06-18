package ctxkeys

import (
	"context"
	"time"
)

type contextKey string

const (
	DryRunKey                   contextKey = "dryRun"
	ResizeOnRecreateKey         contextKey = "resizeOnRecreate"
	NumberOfWorkersKey          contextKey = "numberOfWorkers"
	UseLimitsKey                contextKey = "useLimits"
	PostRolloutCheckKey         contextKey = "postRolloutCheck"
	PostRolloutCheckIntervalKey contextKey = "postRolloutCheckInterval"
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
	return context.WithValue(ctx, UseLimitsKey, enabled)
}

// UseLimitsFromContext retrieves the use limits flag from the context. It returns false if the flag is not set or if the value is not a boolean.
func UseLimitsFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(UseLimitsKey).(bool); ok {
		return v
	}
	return false
}

// WithPostRolloutCheck adds the post-rollout check flag to the context.
func WithPostRolloutCheck(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, PostRolloutCheckKey, enabled)
}

// PostRolloutCheckFromContext retrieves the post-rollout check flag from the context. It returns false if the flag is not set or if the value is not a boolean.
func PostRolloutCheckFromContext(ctx context.Context) bool {
	if v, ok := ctx.Value(PostRolloutCheckKey).(bool); ok {
		return v
	}
	return false
}

// WithPostRolloutCheckInterval adds the post-rollout check interval to the context.
func WithPostRolloutCheckInterval(ctx context.Context, duration time.Duration) context.Context {
	return context.WithValue(ctx, PostRolloutCheckIntervalKey, duration)
}

// PostRolloutCheckIntervalFromContext retrieves the post-rollout check interval from the context. It returns the default interval if the value is not set or if it is not a time.Duration.
func PostRolloutCheckIntervalFromContext(ctx context.Context, defaultInterval time.Duration) time.Duration {
	if v, ok := ctx.Value(PostRolloutCheckIntervalKey).(time.Duration); ok {
		return v
	}
	return defaultInterval
}
