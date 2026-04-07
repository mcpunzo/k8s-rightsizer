package resizeengine

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/model"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

// ResizerEngine is the main engine that orchestrates the resizing of Kubernetes workloads based on recommendations.
type ResizerEngine struct {
	selector WorkloadSelector
	resizer  WorkloadResizer
}

// NewResizerEngine creates a new ResizerEngine instance.
// It accepts a WorkloadSelector and WorkloadResizer which are used to interact with the Kubernetes API.
// The WorkloadSelector and WorkloadResizer are initialized with the same client to ensure consistent interactions with the Kubernetes cluster.
// param selector: The WorkloadSelector used for selecting workloads.
// param resizer: The WorkloadResizer used for resizing workloads.
// returns: A new instance of ResizerEngine.
func NewResizerEngine(selector WorkloadSelector, resizer WorkloadResizer) *ResizerEngine {
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
			err = e.resizeDeployment(ctx, &rec)
		case model.StatefuSet:
			err = e.resizeStatefulSet(ctx, &rec)
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
// It also waits for the rollout to complete and checks for any critical errors during the process. If any error occurs, it attempts to rollback to the previous state.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation object that contains the details for resizing the Deployment.
// returns: An error if the resizing operation fails, otherwise nil.
func (e *ResizerEngine) resizeDeployment(ctx context.Context, rec *model.Recommendation) error {
	deployment, err := e.selector.FindDeployment(ctx, rec)
	if err != nil {
		return err
	}
	if deployment == nil {
		return fmt.Errorf("deployment not found for %s", rec)
	}

	// create a deep copy in case of rollback needs
	deploymentCopy := deployment.DeepCopy()

	log.Printf("Resizing Deployment: %s\n", deployment.Name)
	err = e.resizer.ResizeDeployment(ctx, deployment, rec)

	if err != nil {
		return fmt.Errorf("failed to update deployment for %s: %v", rec, err)
	}

	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 5*time.Minute, false, func(childCtx context.Context) (bool, error) {
		d, err := e.selector.FindDeployment(childCtx, &model.Recommendation{Namespace: deployment.Namespace, WorkloadName: deployment.Name})
		if err != nil {
			return false, err
		}

		// Check for critical errors (OOM, CrashLoop, Unschedulable)
		isError, reason := e.resizer.CheckPodCriticalErrors(childCtx, d.Namespace, d.Spec.Selector)
		if isError {
			// Return the error to stop polling immediately
			return false, fmt.Errorf("fail detected: %s", reason)
		}

		// Check if the rollout is complete
		if d.Status.UpdatedReplicas == *(d.Spec.Replicas) &&
			d.Status.AvailableReplicas == *(d.Spec.Replicas) &&
			d.Status.ObservedGeneration >= d.Generation {
			return true, nil
		}

		log.Printf("[%s] Rollout in progress... (%d/%d ready)", d.Name, d.Status.AvailableReplicas, *(d.Spec.Replicas))
		return false, nil
	})

	// Rollback in case of error or timeout
	if err != nil {
		log.Printf("[!!!] ERROR: %v. Start Rollback for %s", err, deploymentCopy.Name)

		// Reset ResourceVersion to avoid conflicts during rollback
		deploymentCopy.ResourceVersion = ""

		rollbackRec := e.createRollbackRecommendation(rec, deploymentCopy.Spec.Template)
		if rollbackRec == nil {
			return fmt.Errorf("failed to create rollback recommendation for %s", rec)
		}

		err = e.resizer.ResizeDeployment(ctx, deploymentCopy, rollbackRec)

		if err != nil {
			return fmt.Errorf("Update failed (%v) and rollback failed (%v)", err, err)
		}

		return fmt.Errorf("update canceled: %v", err)
	}

	log.Printf("[SUCCESS] %s updated and stable", rec.WorkloadName)
	return nil
}

// resizeStatefulSet finds the target StatefulSet based on the recommendation and applies the new resource requests.
// It also waits for the rollout to complete and checks for any critical errors during the process. If any error occurs, it attempts to rollback to the previous state.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation object that contains the details for resizing the StatefulSet.
// returns: An error if the resizing operation fails, otherwise nil.
func (e *ResizerEngine) resizeStatefulSet(ctx context.Context, rec *model.Recommendation) error {
	statefulSet, err := e.selector.FindStatefulSet(ctx, rec)
	if err != nil {
		return err
	}
	if statefulSet == nil {
		return fmt.Errorf("statefulset not found for %s", rec)
	}

	statefulSetCopy := statefulSet.DeepCopy()

	log.Printf("Resizing StatefulSet: %s\n", statefulSet.Name)
	err = e.resizer.ResizeStatefulSet(ctx, statefulSet, rec)

	if err != nil {
		return fmt.Errorf("failed to update statefulset for %s: %v", rec, err)
	}

	err = wait.PollUntilContextTimeout(ctx, 10*time.Second, 5*time.Minute, false, func(childCtx context.Context) (bool, error) {
		s, err := e.selector.FindStatefulSet(childCtx, &model.Recommendation{Namespace: statefulSet.Namespace, WorkloadName: statefulSet.Name})
		if err != nil {
			return false, err
		}

		// Check for critical errors (OOM, CrashLoop, Unschedulable)
		isError, reason := e.resizer.CheckPodCriticalErrors(childCtx, s.Namespace, s.Spec.Selector)
		if isError {
			// Return the error to stop polling immediately
			return false, fmt.Errorf("fail detected: %s", reason)
		}

		// Check if the rollout is complete
		if s.Status.UpdatedReplicas == *(s.Spec.Replicas) &&
			s.Status.AvailableReplicas == *(s.Spec.Replicas) &&
			s.Status.ObservedGeneration >= s.Generation {
			return true, nil
		}

		log.Printf("[%s] Rollout in progress... (%d/%d ready)", s.Name, s.Status.AvailableReplicas, *(s.Spec.Replicas))
		return false, nil
	})

	// Rollback in case of error or timeout
	if err != nil {
		log.Printf("[!!!] ERROR: %v. Start Rollback for %s", err, statefulSetCopy.Name)

		// Reset ResourceVersion to avoid conflicts during rollback
		statefulSetCopy.ResourceVersion = ""

		rollbackRec := e.createRollbackRecommendation(rec, statefulSetCopy.Spec.Template)
		if rollbackRec == nil {
			return fmt.Errorf("failed to create rollback recommendation for %s", rec)
		}

		err = e.resizer.ResizeStatefulSet(ctx, statefulSetCopy, rollbackRec)
		if err != nil {
			return fmt.Errorf("Update failed (%v) and rollback failed (%v)", err, err)
		}

		return fmt.Errorf("update canceled: %v", err)
	}

	log.Printf("[SUCCESS] %s updated and stable", rec.WorkloadName)
	return nil
}

func (e *ResizerEngine) createRollbackRecommendation(rec *model.Recommendation, template v1.PodTemplateSpec) *model.Recommendation {
	newRec := rec.DeepCopy()

	for _, c := range template.Spec.Containers {
		if c.Name == rec.Container {
			newRec.CpuRequestRecommendation = c.Resources.Requests.Cpu().String()
			newRec.MemoryRequestRecommendation = c.Resources.Requests.Memory().String()
			return newRec
		}
	}

	return nil
}
