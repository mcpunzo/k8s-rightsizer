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

// WorkloadResizer defines the methods required for resizing Kubernetes workloads and checking for critical pod errors.
type WorkloadResizer interface {
	ResizeDeployment(ctx context.Context, deploy *appsv1.Deployment, rec *model.Recommendation) error
	ResizeStatefulSet(ctx context.Context, sts *appsv1.StatefulSet, rec *model.Recommendation) error
	CheckPodCriticalErrors(ctx context.Context, ns string, selector *metav1.LabelSelector) (bool, string)
}

// K8sWorkloadResizer is responsible for resizing Kubernetes workloads based on recommendations.
type K8sWorkloadResizer struct {
	client K8sClient
}

// NewK8sWorkloadResizer creates a new K8sWorkloadResizer.
// It accepts the K8sClient interface which is satisfied by the standard kubernetes.Clientset.
// param client: The Kubernetes client used for interacting with the cluster.
// returns: A new instance of K8sWorkloadResizer.
func NewK8sWorkloadResizer(client K8sClient) *K8sWorkloadResizer {
	return &K8sWorkloadResizer{
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
func (r *K8sWorkloadResizer) ResizeDeployment(ctx context.Context, deployment *appsv1.Deployment, rec *model.Recommendation) error {
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
func (r *K8sWorkloadResizer) ResizeStatefulSet(ctx context.Context, sts *appsv1.StatefulSet, rec *model.Recommendation) error {
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
func (r *K8sWorkloadResizer) ResizeContainer(ctx context.Context, podTemplate *corev1.PodTemplateSpec, rec *model.Recommendation) bool {
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

// CheckPodCriticalErrors checks if any pods associated with the given Deployment are experiencing critical errors such as OOMKilled, CrashLoopBackOff, or scheduling issues.
// It lists the pods matching the Deployment's selector and inspects their status for common failure conditions.
// If any critical error is detected, it returns true along with a descriptive reason.
// param ctx: The context for managing request deadlines and cancellation.
// param namespace: The namespace to search for pods.
// param labelSelector: The label selector to filter pods.
// returns: A boolean indicating if a critical error was detected, and a string describing the reason if an error is found.
func (r *K8sWorkloadResizer) CheckPodCriticalErrors(ctx context.Context, namespace string, labelSelector *metav1.LabelSelector) (bool, string) {
	selector, _ := metav1.LabelSelectorAsSelector(labelSelector)
	pods, _ := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})

	for _, p := range pods.Items {
		// 1. Check if the Pod is stuck in scheduling (Cluster full or insufficient resources)
		if p.Status.Phase == corev1.PodPending {
			for _, cond := range p.Status.Conditions {
				if cond.Type == corev1.PodScheduled && cond.Status == corev1.ConditionFalse && cond.Reason == "Unschedulable" {
					return true, fmt.Sprintf("Insufficient resources in the cluster: %s", cond.Message)
				}
			}
		}

		// 2. Check the status of individual containers
		for _, cs := range p.Status.ContainerStatuses {
			// Waiting cases
			if cs.State.Waiting != nil {
				reason := cs.State.Waiting.Reason
				if reason == "CrashLoopBackOff" || reason == "ImagePullBackOff" || reason == "CreateContainerConfigError" {
					return true, fmt.Sprintf("Container in error: %s", reason)
				}
			}

			// Terminated cases
			if cs.State.Terminated != nil {
				if cs.State.Terminated.Reason == "OOMKilled" {
					return true, "OOMKilled: Insufficient memory for startup"
				}
			}

			// Check the last termination state (if it crashed recently)
			if cs.LastTerminationState.Terminated != nil && cs.LastTerminationState.Terminated.Reason == "OOMKilled" {
				return true, "OOMKilled detected in the last restart: Insufficient memory for startup"
			}
		}
	}
	return false, ""
}
