package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/model"
)

// mockResizer is a mock implementation of the Resizer interface for testing
type mockResizer struct {
	resizeFunc func(ctx context.Context, recs []model.Recommendation, numberOfWorkers int) error
}

func (m *mockResizer) Resize(ctx context.Context, recs []model.Recommendation, numberOfWorkers int) error {
	return m.resizeFunc(ctx, recs, numberOfWorkers)
}

// TestWorkloadRightsizer_Rightsize tests the Rightsize method
func TestWorkloadRightsizer_Rightsize(t *testing.T) {
	t.Parallel()

	recs := []model.Recommendation{
		{Namespace: "default", WorkloadName: "api", Container: "app"},
	}

	tests := []struct {
		name        string
		ctxValues   map[any]any
		recs        []model.Recommendation
		mockFn      func(ctx context.Context, recs []model.Recommendation, workers int) error
		wantWorkers int
		wantErr     bool
		errContains string
	}{
		{
			name:        "Success - default 1 worker when not set in context",
			recs:        recs,
			wantWorkers: 1,
			mockFn: func(_ context.Context, _ []model.Recommendation, workers int) error {
				if workers != 1 {
					t.Errorf("expected 1 worker, got %d", workers)
				}
				return nil
			},
			wantErr: false,
		},
		{
			name:        "Success - number of workers read from context",
			recs:        recs,
			ctxValues:   map[any]any{ctxkeys.NumberOfWorkersKey: 5},
			wantWorkers: 5,
			mockFn: func(_ context.Context, _ []model.Recommendation, workers int) error {
				if workers != 5 {
					t.Errorf("expected 5 workers, got %d", workers)
				}
				return nil
			},
			wantErr: false,
		},
		{
			name:        "Success - empty recommendations list",
			recs:        []model.Recommendation{},
			wantWorkers: 1,
			mockFn:      func(_ context.Context, _ []model.Recommendation, _ int) error { return nil },
			wantErr:     false,
		},
		{
			name:        "Failure - resizer returns error",
			recs:        recs,
			wantWorkers: 1,
			mockFn:      func(_ context.Context, _ []model.Recommendation, _ int) error { return errors.New("resize failed") },
			wantErr:     true,
			errContains: "resize failed",
		},
		{
			name:        "Success - wrong type in context falls back to 1 worker",
			recs:        recs,
			ctxValues:   map[any]any{ctxkeys.NumberOfWorkersKey: "not-an-int"},
			wantWorkers: 1,
			mockFn: func(_ context.Context, _ []model.Recommendation, workers int) error {
				if workers != 1 {
					t.Errorf("expected fallback to 1 worker, got %d", workers)
				}
				return nil
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockResizer{resizeFunc: tt.mockFn}
			r := NewWorkloadRightsizer(mock)

			ctx := context.Background()
			for k, v := range tt.ctxValues {
				ctx = context.WithValue(ctx, k, v)
			}

			err := r.Rightsize(ctx, tt.recs)

			if (err != nil) != tt.wantErr {
				t.Errorf("Rightsize() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || err.Error() != tt.errContains) {
				t.Errorf("Rightsize() error = %v, want %q", err, tt.errContains)
			}
		})
	}
}
