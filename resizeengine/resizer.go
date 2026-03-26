package resizeengine

import (
	"context"
	"fmt"
	"log"

	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadResizer is responsible for resizing Kubernetes workloads based on recommendations.
type WorkloadResizer struct {
	client K8sClient
}

// NewWorkloadResizer creates a new WorkloadResizer.
// It accepts the K8sClient interface which is satisfied by the standard kubernetes.Clientset.
func NewWorkloadResizer(client K8sClient) *WorkloadResizer {
	return &WorkloadResizer{
		client: client,
	}
}

// ResizeDeployment updates container resource requests in a Deployment.
// It finds the target Deployment based on the recommendation and applies the new resource requests.
// Returns an error if the Deployment or container is not found, or if the update operation fails.
// param ctx: The context for managing request deadlines and cancellation.
// param deployment: The Deployment object to be resized.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: An error if the resizing operation fails, otherwise nil.
func (r *WorkloadResizer) ResizeDeployment(ctx context.Context, deployment *appsv1.Deployment, rec model.Recommendation) error {
	log.Printf("Resizing Deployment: %s/%s\n", rec.Namespace, deployment.Name)

	if !r.ResizeContainer(ctx, &deployment.Spec.Template, rec) {
		return fmt.Errorf("container %s not found in deployment %s", rec.Container, deployment.Name)
	}

	_, err := r.client.AppsV1().Deployments(rec.Namespace).Update(ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update deployment %s: %w", deployment.Name, err)
	}

	return nil
}

// ResizeStatefulSet updates container resource requests in a StatefulSet.
// It finds the target StatefulSet based on the recommendation and applies the new resource requests.
// Returns an error if the StatefulSet or container is not found, or if the update operation fails.
// param ctx: The context for managing request deadlines and cancellation.
// param sts: The StatefulSet object to be resized.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: An error if the resizing operation fails, otherwise nil.
func (r *WorkloadResizer) ResizeStatefulSet(ctx context.Context, sts *appsv1.StatefulSet, rec model.Recommendation) error {
	log.Printf("Resizing StatefulSet: %s/%s\n", rec.Namespace, sts.Name)

	if !r.ResizeContainer(ctx, &sts.Spec.Template, rec) {
		return fmt.Errorf("container %s not found in statefulset %s", rec.Container, sts.Name)
	}

	_, err := r.client.AppsV1().StatefulSets(rec.Namespace).Update(ctx, sts, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update statefulset %s: %w", sts.Name, err)
	}

	return nil
}

// ResizeContainer modifies the PodTemplateSpec based on the recommendation.
// It updates the resource requests for the specified container.
// Returns true if the container was found and updated.
// param ctx: The context for managing request deadlines and cancellation.
// param podTemplate: The PodTemplateSpec to be modified.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: A boolean indicating whether the container was found and updated.
func (r *WorkloadResizer) ResizeContainer(ctx context.Context, podTemplate *corev1.PodTemplateSpec, rec model.Recommendation) bool {
	for i, c := range podTemplate.Spec.Containers {
		if c.Name == rec.Container {
			podTemplate.Spec.Containers[i].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(rec.CpuRequestRecommendation),
				corev1.ResourceMemory: resource.MustParse(rec.MemoryRequestRecommendation),
			}
			return true
		}
	}
	return false
}
