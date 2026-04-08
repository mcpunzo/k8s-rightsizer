package resizeengine

import (
	"context"
	"fmt"
	"log"

	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StatefulSetWorkload implements the WorkloadOps interface for Kubernetes StatefulSets.
type StatefulSetWorkload struct {
	client K8sClient
}

// FindWorkload retrieves the current state of the StatefulSet based on the recommendation and constructs a Workload struct with the necessary information for resizing and status checking.
// It returns an error if the StatefulSet cannot be retrieved.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the namespace, workload name, and container information.
// returns: A pointer to a Workload struct representing the StatefulSet, or an error if the StatefulSet cannot be retrieved.
func (w *StatefulSetWorkload) FindWorkload(ctx context.Context, rec *model.Recommendation) (*Workload, error) {
	statefulSet, err := w.client.AppsV1().StatefulSets(rec.Namespace).Get(ctx, rec.WorkloadName, metav1.GetOptions{})
	if err != nil {
		log.Printf("failed to get statefulset for %s: %v\n", rec, err)
		return nil, err
	}

	return &Workload{
		WorkloadType:     StatefulSet,
		Namespace:        rec.Namespace,
		Name:             rec.WorkloadName,
		ContainerName:    rec.Container,
		Template:         &statefulSet.Spec.Template,
		labelSelector:    statefulSet.Spec.Selector,
		originalResource: statefulSet}, nil
}

// ResizeWorkload modifies the StatefulSet's PodTemplateSpec based on the recommendation and updates the StatefulSet in the cluster.
// It returns an error if the container specified in the recommendation is not found in the StatefulSet or if the update operation fails.
// param ctx: The context for managing request deadlines and cancellation.
// param workload: The Workload struct representing the StatefulSet to be resized.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: An error if the container is not found or if the update operation fails.
func (w *StatefulSetWorkload) ResizeWorkload(ctx context.Context, workload *Workload, rec *model.Recommendation) error {
	log.Printf("Resizing Workload: %s/%s\n", workload.Namespace, workload.Name)

	if !ResizeContainer(ctx, workload.Template, rec) {
		return fmt.Errorf("container %s not found in statefulset %s", rec.Container, workload.Name)
	}

	if workload.WorkloadType != StatefulSet {
		return fmt.Errorf("invalid workload type: expected StatefulSet, got %s", workload.WorkloadType)
	}

	statefulSet, ok := workload.originalResource.(*appsv1.StatefulSet)
	if !ok {
		return fmt.Errorf("failed to cast original resource to StatefulSet for %s", workload.Name)
	}
	_, err := w.client.AppsV1().StatefulSets(rec.Namespace).Update(ctx, statefulSet, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update statefulset %s: %w", workload.Name, err)
	}

	return nil
}

// GetStatus retrieves the current status of the StatefulSet and normalizes it into a WorkloadStatus struct.
// It returns an error if the StatefulSet cannot be retrieved.
// param ctx: The context for managing request deadlines and cancellation.
// param ns: The namespace of the StatefulSet.
// param name: The name of the StatefulSet.
// returns: A pointer to a WorkloadStatus struct representing the current status of the StatefulSet, or an error if the StatefulSet cannot be retrieved.
func (w *StatefulSetWorkload) GetStatus(ctx context.Context, ns, name string) (*WorkloadStatus, error) {
	s, err := w.client.AppsV1().StatefulSets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	var expectedReplicas int32
	if s.Spec.Replicas != nil {
		expectedReplicas = *s.Spec.Replicas
	}

	return &WorkloadStatus{
		Replicas: expectedReplicas, UpdatedReplicas: s.Status.UpdatedReplicas,
		AvailableReplicas: s.Status.AvailableReplicas, Generation: s.Generation,
		ObservedGeneration: s.Status.ObservedGeneration, Namespace: s.Namespace,
		LabelSelector: s.Spec.Selector,
	}, nil
}
