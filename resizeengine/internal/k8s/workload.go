package k8s

import (
	"context"
	"errors"
	"fmt"
	"log"

	"github.com/mcpunzo/k8s-rightsizer/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadStatus is a normalized struct to represent the status of a workload (Deployment or StatefulSet) in a consistent way.
type WorkloadStatus struct {
	ExpectedReplicas   int32
	UpdatedReplicas    int32
	AvailableReplicas  int32
	ObservedGeneration int64
	Generation         int64
}

type WorkloadType string

const (
	Deployment  WorkloadType = "Deployment"
	StatefulSet WorkloadType = "StatefulSet"
)

// Workload is a generic struct that represents a workload (Deployment, StatefulSet, etc.) with the necessary information for resizing and status checking.
type Workload struct {
	Id               string
	WorkloadType     WorkloadType
	Namespace        string
	Name             string
	Template         *corev1.PodTemplateSpec
	LabelSelector    *metav1.LabelSelector
	UpdateStrategy   string
	OriginalResource any
}

// ResizeContainer modifies the PodTemplateSpec based on the recommendation.
// It updates the resource requests for the specified container.
// Returns true if the container was found and updated.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: A boolean indicating whether the container was found and updated. An error is returned if the container is not found or if the resources already match the recommendation.
func (w *Workload) ResizeContainer(ctx context.Context, rec *model.Recommendation) (bool, error) {
	for i, c := range w.Template.Spec.Containers {
		if c.Name == rec.Container {
			recommendedCPU, err := resource.ParseQuantity(rec.CpuRequestRecommendation)
			if err != nil {
				return false, fmt.Errorf("invalid cpu request recommendation for container %s in workload %s: %v", c.Name, rec.WorkloadName, err)
			}

			recommendedMem, err := resource.ParseQuantity(rec.MemoryRequestRecommendation)
			if err != nil {
				return false, fmt.Errorf("invalid memory request recommendation for container %s in workload %s: %v", c.Name, rec.WorkloadName, err)
			}

			currentCPU := c.Resources.Requests.Cpu()
			currentMem := c.Resources.Requests.Memory()

			if currentCPU.Equal(recommendedCPU) && currentMem.Equal(recommendedMem) {
				msg := fmt.Sprintf("Container %s in workload %s: resources match recommendation", c.Name, rec.WorkloadName)
				log.Print(msg)
				return false, errors.New(msg)
			}

			w.Template.Spec.Containers[i].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    recommendedCPU,
				corev1.ResourceMemory: recommendedMem,
			}
			return true, nil
		}
	}

	msg := fmt.Sprintf("Container %s not found in %s %s", rec.Container, rec.Kind, rec.WorkloadName)
	log.Print(msg)
	return false, errors.New(msg)
}

// ValidateRecommendations checks if the recommendations for CPU and Memory requests are valid for the specified container in the workload.
// It ensures that the recommended requests do not exceed the current limits set on the container.
// Returns an error if the container is not found or if the recommendations are invalid.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: An error if the container is not found or if the recommendations are invalid.
func (w *Workload) ValidateRecommendations(ctx context.Context, rec *model.Recommendation) error {
	recCpu, err := resource.ParseQuantity(rec.CpuRequestRecommendation)
	if err != nil {
		return fmt.Errorf("invalid cpu request recommendation: %v", err)
	}
	recMem, err := resource.ParseQuantity(rec.MemoryRequestRecommendation)
	if err != nil {
		return fmt.Errorf("invalid memory request recommendation: %v", err)
	}

	var container *corev1.Container
	for i, c := range w.Template.Spec.Containers {
		if c.Name == rec.Container {
			container = &w.Template.Spec.Containers[i]
			break
		}
	}

	if container == nil {
		return fmt.Errorf("container %s not found in workload %s", rec.Container, rec.WorkloadName)
	}

	limitCpu := container.Resources.Limits.Cpu()
	limitMem := container.Resources.Limits.Memory()

	if limitCpu != nil && !limitCpu.IsZero() && recCpu.Cmp(*limitCpu) > 0 {
		return fmt.Errorf("cpu request (%s) cannot be greater than current limit (%s)",
			rec.CpuRequestRecommendation, limitCpu.String())
	}

	if limitMem != nil && !limitMem.IsZero() && recMem.Cmp(*limitMem) > 0 {
		return fmt.Errorf("memory request (%s) cannot be greater than current limit (%s)",
			rec.MemoryRequestRecommendation, limitMem.String())
	}

	requestCpu := container.Resources.Requests.Cpu()
	requestMem := container.Resources.Requests.Memory()

	// TODO: add limits management in the future, for now we just check that the new requests are different from the current ones to avoid unnecessary updates that would trigger rollouts without any actual change
	if requestCpu != nil && !requestCpu.IsZero() && recCpu.Cmp(*requestCpu) == 0 &&
		requestMem != nil && !requestMem.IsZero() && recMem.Cmp(*requestMem) == 0 {
		return fmt.Errorf("requests for container %s in workload %s already match the recommendation", rec.Container, rec.WorkloadName)
	}

	return nil
}
