package resizeengine

import (
	"context"
	"fmt"
	"log"

	"github.com/mcpunzo/k8s-rightsizer/model"
)

// ResizerEngine is the main engine that orchestrates the resizing of Kubernetes workloads based on recommendations.
type ResizerEngine struct {
	selector *WorkloadSelector
	resizer  *WorkloadResizer
}

// NewResizerEngine creates a new ResizerEngine instance.
// It accepts a WorkloadSelector and WorkloadResizer which are used to interact with the Kubernetes API.
// The WorkloadSelector and WorkloadResizer are initialized with the same client to ensure consistent interactions with the Kubernetes cluster.
// param selector: The WorkloadSelector used for selecting workloads.
// param resizer: The WorkloadResizer used for resizing workloads.
// returns: A new instance of ResizerEngine.
func NewResizerEngine(selector *WorkloadSelector, resizer *WorkloadResizer) *ResizerEngine {
	return &ResizerEngine{
		selector: selector,
		resizer:  resizer,
	}
}

// Resize performs the resizing operation based on the recommendations
// For each recommendation, it attempts to find the corresponding workload and apply the new resource requests.
// If any operation fails, it logs the error and continues processing the next recommendation.
// param ctx: The context for managing request deadlines and cancellation.
// param recommendations: A slice of Recommendation objects that contain the details for resizing workloads.
// returns: An error if any of the resizing operations fail, otherwise nil.
func (e *ResizerEngine) Resize(ctx context.Context, recommendations []model.Recommendation) error {
	for _, rec := range recommendations {
		log.Printf("Processing %s\n", rec)

		var err error
		switch rec.Type {
		case model.ReplicaSet:
			err = e.resizeDeployment(ctx, rec)
		case model.StatefuSet:
			err = e.resizeStatefulSet(ctx, rec)
		default:
			err = fmt.Errorf("unsupported resource type for %s", rec.Type)
		}

		if err != nil {
			log.Printf("[KO] failed to resize %s: %v\n", rec, err)
			continue
		}

		log.Printf("[OK] Resource resized for %s\n", rec)
	}

	return nil
}

// resizeDeployment finds the target Deployment based on the recommendation and applies the new resource requests.
func (e *ResizerEngine) resizeDeployment(ctx context.Context, rec model.Recommendation) error {
	deployment, err := e.selector.FindDeployment(ctx, rec)
	if err != nil {
		return err
	}
	if deployment == nil {
		return fmt.Errorf("deployment not found for %s", rec)
	}

	// Placeholder for actual resizing logic for Deployment
	log.Printf("Resizing Deployment: %s\n", deployment.Name)
	err = e.resizer.ResizeDeployment(ctx, deployment, rec)

	if err != nil {
		return fmt.Errorf("failed to update deployment for %s: %v", rec, err)
		// TODO: implement logic to wait for the deployment to be updated and check if the new resource requests are applied
		// If the deployment is not up after the update, we should consider rolling back to the previous state to avoid downtime
	}

	return nil
}

// resizeStatefulSet finds the target StatefulSet based on the recommendation and applies the new resource requests.
func (e *ResizerEngine) resizeStatefulSet(ctx context.Context, rec model.Recommendation) error {
	statefulSet, err := e.selector.FindStatefulSet(ctx, rec)
	if err != nil {
		return err
	}
	if statefulSet == nil {
		return fmt.Errorf("statefulset not found for %s", rec)
	}

	log.Printf("Resizing StatefulSet: %s\n", statefulSet.Name)
	err = e.resizer.ResizeStatefulSet(ctx, statefulSet, rec)

	if err != nil {
		return fmt.Errorf("failed to update statefulset for %s: %v", rec, err)
		// TODO: implement logic to wait for the statefulset to be updated and check if the new resource requests are applied
		// If the statefulset is not up after the update, we should consider rolling back to the previous state to avoid downtime

	}

	return nil
}
