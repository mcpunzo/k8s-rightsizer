package ctxkeys

import (
	"context"
	"testing"
	"time"
)

func TestWithDryRun_and_DryRunFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns true when set to true", func(t *testing.T) {
		ctx := WithDryRun(context.Background(), true)
		if got := DryRunFromContext(ctx); !got {
			t.Errorf("DryRunFromContext() = %v, want true", got)
		}
	})

	t.Run("returns false when set to false", func(t *testing.T) {
		ctx := WithDryRun(context.Background(), false)
		if got := DryRunFromContext(ctx); got {
			t.Errorf("DryRunFromContext() = %v, want false", got)
		}
	})

	t.Run("returns false when not set", func(t *testing.T) {
		if got := DryRunFromContext(context.Background()); got {
			t.Errorf("DryRunFromContext() = %v, want false", got)
		}
	})
}

func TestWithResizeOnRecreate_and_ResizeOnRecreateFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns true when set to true", func(t *testing.T) {
		ctx := WithResizeOnRecreate(context.Background(), true)
		if got := ResizeOnRecreateFromContext(ctx); !got {
			t.Errorf("ResizeOnRecreateFromContext() = %v, want true", got)
		}
	})

	t.Run("returns false when set to false", func(t *testing.T) {
		ctx := WithResizeOnRecreate(context.Background(), false)
		if got := ResizeOnRecreateFromContext(ctx); got {
			t.Errorf("ResizeOnRecreateFromContext() = %v, want false", got)
		}
	})

	t.Run("returns false when not set", func(t *testing.T) {
		if got := ResizeOnRecreateFromContext(context.Background()); got {
			t.Errorf("ResizeOnRecreateFromContext() = %v, want false", got)
		}
	})
}

func TestWithNumberOfWorkers_and_NumberOfWorkersFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns set value", func(t *testing.T) {
		ctx := WithNumberOfWorkers(context.Background(), 8)
		if got := NumberOfWorkersFromContext(ctx, 1); got != 8 {
			t.Errorf("NumberOfWorkersFromContext() = %v, want 8", got)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		if got := NumberOfWorkersFromContext(context.Background(), 4); got != 4 {
			t.Errorf("NumberOfWorkersFromContext() = %v, want 4", got)
		}
	})
}

func TestWithUseLimits_and_UseLimitsFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns true when set to true", func(t *testing.T) {
		ctx := WithUseLimits(context.Background(), true)
		if got := UseLimitsFromContext(ctx); !got {
			t.Errorf("UseLimitsFromContext() = %v, want true", got)
		}
	})

	t.Run("returns false when set to false", func(t *testing.T) {
		ctx := WithUseLimits(context.Background(), false)
		if got := UseLimitsFromContext(ctx); got {
			t.Errorf("UseLimitsFromContext() = %v, want false", got)
		}
	})

	t.Run("returns false when not set", func(t *testing.T) {
		if got := UseLimitsFromContext(context.Background()); got {
			t.Errorf("UseLimitsFromContext() = %v, want false", got)
		}
	})
}

func TestWithPostRolloutCheck_and_PostRolloutCheckFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns true when set to true", func(t *testing.T) {
		ctx := WithPostRolloutCheck(context.Background(), true)
		if got := PostRolloutCheckFromContext(ctx); !got {
			t.Errorf("PostRolloutCheckFromContext() = %v, want true", got)
		}
	})

	t.Run("returns false when set to false", func(t *testing.T) {
		ctx := WithPostRolloutCheck(context.Background(), false)
		if got := PostRolloutCheckFromContext(ctx); got {
			t.Errorf("PostRolloutCheckFromContext() = %v, want false", got)
		}
	})

	t.Run("returns false when not set", func(t *testing.T) {
		if got := PostRolloutCheckFromContext(context.Background()); got {
			t.Errorf("PostRolloutCheckFromContext() = %v, want false", got)
		}
	})
}

func TestWithPostRolloutCheckInterval_and_PostRolloutCheckIntervalFromContext(t *testing.T) {
	t.Parallel()

	t.Run("returns set duration", func(t *testing.T) {
		ctx := WithPostRolloutCheckInterval(context.Background(), 30*time.Second)
		if got := PostRolloutCheckIntervalFromContext(ctx, 10*time.Second); got != 30*time.Second {
			t.Errorf("PostRolloutCheckIntervalFromContext() = %v, want 30s", got)
		}
	})

	t.Run("returns default when not set", func(t *testing.T) {
		if got := PostRolloutCheckIntervalFromContext(context.Background(), 10*time.Second); got != 10*time.Second {
			t.Errorf("PostRolloutCheckIntervalFromContext() = %v, want 10s", got)
		}
	})
}
