package resizeengine

import (
	"context"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/model"
)

// WorkloadRightsizer is an implementation of the Rightsizer interface that processes recommendations and performs rightsizing operations on Kubernetes workloads (Deployments and StatefulSets).
type WorkloadRightsizer struct {
	resizer Resizer
}

// NewWorkloadRightsizer creates a new instance of WorkloadRightsizer with the provided Kubernetes client.
// param resizer: An instance of Resizer that provides methods for resizing workloads and checking their status.
// returns: A pointer to a new instance of WorkloadRightsizer.
func NewWorkloadRightsizer(resizer Resizer) *WorkloadRightsizer {
	return &WorkloadRightsizer{
		resizer: resizer,
	}
}

// Rightsize processes a list of recommendations and performs resizing operations on the corresponding workloads concurrently using worker goroutines.
// It creates a pool of worker goroutines that listen for recommendations on a channel, processes them, and sends the results to another channel.
// The function waits for all workers to complete before closing the results channel and logging the outcomes.
// param ctx: The context for managing request deadlines and cancellation.
// param recs: A slice of Recommendations to be processed for resizing workloads.
// returns: An error if any issues occur during processing, or nil if all recommendations are processed successfully.
func (r *WorkloadRightsizer) Rightsize(ctx context.Context, recs []model.Recommendation) error {
	numberOfWorkers := ctxkeys.NumberOfWorkersFromContext(ctx, 1)

	return r.resizer.Resize(ctx, recs, numberOfWorkers)
}
