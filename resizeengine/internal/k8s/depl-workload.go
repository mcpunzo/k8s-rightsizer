package k8s

import (
	"context"
	"fmt"
	"log"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DeploymentWorkload implements the WorkloadOps interface for Kubernetes Deployments.
type DeploymentWorkload struct {
	client K8sClient
}

// NewDeploymentWorkload creates a new instance of DeploymentWorkload with the provided Kubernetes client.
// param client: The Kubernetes client used to interact with the cluster.
// returns: A pointer to a new instance of DeploymentWorkload.
func NewDeploymentWorkload(client K8sClient) *DeploymentWorkload {
	return &DeploymentWorkload{client: client}
}

// FindWorkload retrieves the current state of the Deployment based on the recommendation and constructs a Workload struct with the necessary information for resizing and status checking.
// It returns an error if the Deployment cannot be retrieved.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the namespace, workload name, and container information.
// returns: A pointer to a Workload struct representing the Deployment, or an error if the Deployment cannot be retrieved.
func (w *DeploymentWorkload) FindWorkload(ctx context.Context, rec *model.Recommendation) (*Workload, error) {
	deployment, err := w.client.AppsV1().Deployments(rec.Namespace).Get(ctx, rec.WorkloadName, metav1.GetOptions{})
	if err != nil {
		log.Printf("failed to get deployment for %s: %v\n", rec, err)
		return nil, err
	}

	workload := &Workload{
		Id:               rec.WorkloadID(),
		WorkloadType:     Deployment,
		Namespace:        rec.Namespace,
		Name:             rec.WorkloadName,
		Template:         &deployment.Spec.Template,
		LabelSelector:    deployment.Spec.Selector,
		UpdateStrategy:   string(deployment.Spec.Strategy.Type),
		OriginalResource: deployment}

	return workload, nil

}

// ResizeWorkload modifies the Deployment's PodTemplateSpec based on the recommendation and updates the Deployment in the cluster.
// It returns an error if the container specified in the recommendation is not found in the Deployment or if the update operation fails.
// param ctx: The context for managing request deadlines and cancellation.
// param workload: The Workload struct representing the Deployment to be resized.
// param recs: The slice of Recommendations containing the new resource requests and target container information.
// returns: An error if the container is not found or if the update operation fails.
func (w *DeploymentWorkload) ResizeWorkload(ctx context.Context, workload *Workload, recs []*model.Recommendation) error {
	log.Printf("Resizing Workload: %s/%s\n", workload.Namespace, workload.Name)

	if workload.WorkloadType != Deployment {
		return fmt.Errorf("invalid workload type: expected Deployment, got %s", workload.WorkloadType)
	}

	errorResizingContainers := true
	for _, rec := range recs {
		updated, err := workload.ResizeContainer(ctx, rec)
		if err != nil {
			log.Printf("skipping resize for container %s in deployment %s: %v", rec.Container, workload.Name, err)
		}
		if !updated {
			log.Printf("skipping resize for container %s in deployment %s: container not found or resources already match recommendation", rec.Container, workload.Name)
		}

		if updated {
			errorResizingContainers = false
		}
	}

	if errorResizingContainers {
		return fmt.Errorf("failed to resize any containers for deployment %s: all recommendations were invalid", workload.Name)
	}

	deployment, ok := workload.OriginalResource.(*appsv1.Deployment)
	if !ok {
		return fmt.Errorf("failed to cast original resource to Deployment for %s", workload.Name)
	}

	if ctxkeys.DryRunFromContext(ctx) {
		log.Printf("[Dry-Run] Would update deployment %s/%s with new resources\n", workload.Namespace, workload.Name)
		return nil
	}

	_, err := w.client.AppsV1().Deployments(workload.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment %s: %w", workload.Name, err)
	}

	return nil
}

// GetStatus retrieves the current status of the Deployment and normalizes it into a WorkloadStatus struct.
// It returns an error if the Deployment cannot be retrieved.
// param ctx: The context for managing request deadlines and cancellation.
// param workload: The Workload struct representing the Deployment.
// returns: A pointer to a WorkloadStatus struct representing the current status of the Deployment, or an error if the Deployment cannot be retrieved.
func (w *DeploymentWorkload) GetStatus(ctx context.Context, workload *Workload) (*WorkloadStatus, error) {
	d, err := w.client.AppsV1().Deployments(workload.Namespace).Get(ctx, workload.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var expectedReplicas int32
	if d.Spec.Replicas != nil {
		expectedReplicas = *d.Spec.Replicas
	}

	return &WorkloadStatus{
		ExpectedReplicas: expectedReplicas, UpdatedReplicas: d.Status.UpdatedReplicas,
		AvailableReplicas: d.Status.AvailableReplicas, Generation: d.Generation,
		ObservedGeneration: d.Status.ObservedGeneration,
	}, nil
}

// IsWorkloadInPausedState checks if the Deployment is currently in a paused state.
// It returns a boolean indicating whether the Deployment is paused, and an error if the Deployment cannot be retrieved.
// param ctx: The context for managing request deadlines and cancellation.
// param workload: The Workload struct representing the Deployment.
// returns: A boolean indicating whether the Deployment is paused, and an error if the Deployment cannot be retrieved.
func (w *DeploymentWorkload) IsWorkloadInPausedState(ctx context.Context, workload *Workload) (bool, error) {
	d, err := w.client.AppsV1().Deployments(workload.Namespace).Get(ctx, workload.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}
	return d.Spec.Paused, nil
}
