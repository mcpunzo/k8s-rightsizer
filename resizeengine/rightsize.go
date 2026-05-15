package resizeengine

import (
	"context"

	"github.com/mcpunzo/k8s-rightsizer/model"
)

// Resizer defines the interface for the resizer that processes recommendations and performs resizing operations on workloads.
type Resizer interface {
	Resize(ctx context.Context, rec []model.Recommendation, numberOfWorkers int) error
}
