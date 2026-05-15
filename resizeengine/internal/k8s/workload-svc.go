package k8s

import (
	"context"

	"github.com/mcpunzo/k8s-rightsizer/model"
)

// WorkloadService defines the interface for operations on workloads (Deployment, StatefulSet, etc.) that the resizer will use to find, resize and check status.
type WorkloadService interface {
	FindWorkload(ctx context.Context, rec *model.Recommendation) (*Workload, error)
	ResizeWorkload(ctx context.Context, workload *Workload, recs []*model.Recommendation) error
	GetStatus(ctx context.Context, workload *Workload) (*WorkloadStatus, error)
	IsWorkloadInPausedState(ctx context.Context, workload *Workload) (bool, error)
}
