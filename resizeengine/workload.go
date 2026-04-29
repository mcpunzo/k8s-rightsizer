package resizeengine

import (
	"context"
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

// WorkloadOps defines the interface for operations on workloads (Deployment, StatefulSet, etc.) that the resizer will use to find, resize and check status.
type WorkloadOps interface {
	FindWorkload(ctx context.Context, rec *model.Recommendation) (*Workload, error)
	ResizeWorkload(ctx context.Context, workload *Workload, rec *model.Recommendation) error
	GetStatus(ctx context.Context, workload *Workload) (*WorkloadStatus, error)
	IsWorkloadInPausedState(ctx context.Context, workload *Workload) (bool, error)
}

type WorkloadType string

const (
	Deployment  WorkloadType = "Deployment"
	StatefulSet WorkloadType = "StatefulSet"
)

// Workload is a generic struct that represents a workload (Deployment, StatefulSet, etc.) with the necessary information for resizing and status checking.
type Workload struct {
	WorkloadType     WorkloadType
	Namespace        string
	Name             string
	ContainerName    string
	Template         *corev1.PodTemplateSpec
	LabelSelector    *metav1.LabelSelector
	UpdateStrategy   string
	OriginalResource any
}

// ResizeContainer modifies the PodTemplateSpec based on the recommendation.
// It updates the resource requests for the specified container.
// Returns true if the container was found and updated.
// param ctx: The context for managing request deadlines and cancellation.
// param podTemplate: The PodTemplateSpec to be modified.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: A boolean indicating whether the container was found and updated.
func ResizeContainer(ctx context.Context, podTemplate *corev1.PodTemplateSpec, rec *model.Recommendation) bool {
	for i, c := range podTemplate.Spec.Containers {
		if c.Name == rec.Container {

			recommendedCPU := resource.MustParse(rec.CpuRequestRecommendation)
			recommendedMem := resource.MustParse(rec.MemoryRequestRecommendation)

			currentCPU := c.Resources.Requests.Cpu()
			currentMem := c.Resources.Requests.Memory()

			if currentCPU.Equal(recommendedCPU) && currentMem.Equal(recommendedMem) {
				log.Printf("Container %s in workload %s: resources match recommendation", c.Name, rec.WorkloadName)
				return false
			}

			podTemplate.Spec.Containers[i].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    recommendedCPU,
				corev1.ResourceMemory: recommendedMem,
			}
			return true
		}
	}

	log.Printf("Container %s not found in %s %s", rec.Container, rec.Kind, rec.WorkloadName)
	return false
}
