package k8s

import (
	"context"
	"errors"
	"fmt"

	"github.com/rs/zerolog/log"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
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

type recommendationQuantity struct {
	CpuRequestRecommendation    resource.Quantity
	CpuLimitRecommendation      resource.Quantity
	MemoryRequestRecommendation resource.Quantity
	MemoryLimitRecommendation   resource.Quantity
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
			recommendationQuantities, err := w.validateRecommendationQuantities(ctx, rec)
			if err != nil {
				return false, err
			}

			currentCPU := c.Resources.Requests.Cpu()
			currentMem := c.Resources.Requests.Memory()
			useLimits := ctxkeys.UseLimitsFromContext(ctx)

			requestsMatch := currentCPU.Equal(recommendationQuantities.CpuRequestRecommendation) && currentMem.Equal(recommendationQuantities.MemoryRequestRecommendation)
			limitsMatch := false
			if useLimits {
				currentCPULimit := c.Resources.Limits.Cpu()
				currentMemLimit := c.Resources.Limits.Memory()
				limitsMatch = currentCPULimit.Equal(recommendationQuantities.CpuLimitRecommendation) && currentMemLimit.Equal(recommendationQuantities.MemoryLimitRecommendation)
			}

			if requestsMatch && (!useLimits || limitsMatch) {
				msg := fmt.Sprintf("Container %s in workload %s: resources match recommendation", c.Name, rec.WorkloadName)
				if useLimits {
					msg = fmt.Sprintf("Container %s in workload %s: resource requests and limits already match the recommendation", c.Name, rec.WorkloadName)
				}
				log.Info().Msg(msg)
				return false, errors.New(msg)
			}

			w.Template.Spec.Containers[i].Resources.Requests = corev1.ResourceList{
				corev1.ResourceCPU:    recommendationQuantities.CpuRequestRecommendation,
				corev1.ResourceMemory: recommendationQuantities.MemoryRequestRecommendation,
			}

			if ctxkeys.UseLimitsFromContext(ctx) {
				w.Template.Spec.Containers[i].Resources.Limits = corev1.ResourceList{
					corev1.ResourceCPU:    recommendationQuantities.CpuLimitRecommendation,
					corev1.ResourceMemory: recommendationQuantities.MemoryLimitRecommendation,
				}
			}

			return true, nil
		}
	}

	msg := fmt.Sprintf("Container %s not found in %s %s", rec.Container, rec.Kind, rec.WorkloadName)
	log.Info().Msg(msg)
	return false, errors.New(msg)
}

// ValidateRecommendations checks if the recommendations for CPU and Memory requests are valid for the specified container in the workload.
// It ensures that the recommended requests do not exceed the current limits set on the container.
// Returns an error if the container is not found or if the recommendations are invalid.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: An error if the container is not found or if the recommendations are invalid.
func (w *Workload) ValidateRecommendations(ctx context.Context, rec *model.Recommendation) error {
	recQuantities, err := w.validateRecommendationQuantities(ctx, rec)
	if err != nil {
		return err
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

	requestCpu := container.Resources.Requests.Cpu()
	requestMem := container.Resources.Requests.Memory()
	limitCpu := container.Resources.Limits.Cpu()
	limitMem := container.Resources.Limits.Memory()

	cpurequestAlreadyMatch := requestCpu != nil && !requestCpu.IsZero() && recQuantities.CpuRequestRecommendation.Cmp(*requestCpu) == 0
	memRequestAlreadyMatch := requestMem != nil && !requestMem.IsZero() && recQuantities.MemoryRequestRecommendation.Cmp(*requestMem) == 0
	cpuLimitAlreadyMatch := limitCpu != nil && !limitCpu.IsZero() && recQuantities.CpuLimitRecommendation.Cmp(*limitCpu) == 0
	memLimitAlreadyMatch := limitMem != nil && !limitMem.IsZero() && recQuantities.MemoryLimitRecommendation.Cmp(*limitMem) == 0

	useLimits := ctxkeys.UseLimitsFromContext(ctx)
	if !useLimits {
		//if limits management is not enabled, we just check that the new requests are not greater than the current limits to avoid unnecessary updates that would trigger rollouts without any actual change, if the current limits are not set we skip this check
		limitCpu := container.Resources.Limits.Cpu()
		limitMem := container.Resources.Limits.Memory()

		if limitCpu != nil && !limitCpu.IsZero() && recQuantities.CpuRequestRecommendation.Cmp(*limitCpu) > 0 {
			return fmt.Errorf("cpu request (%s) cannot be greater than current limit (%s)",
				recQuantities.CpuRequestRecommendation.String(), limitCpu.String())
		}

		if limitMem != nil && !limitMem.IsZero() && recQuantities.MemoryRequestRecommendation.Cmp(*limitMem) > 0 {
			return fmt.Errorf("memory request (%s) cannot be greater than current limit (%s)",
				recQuantities.MemoryRequestRecommendation.String(), limitMem.String())
		}

		if cpurequestAlreadyMatch && memRequestAlreadyMatch {
			msg := fmt.Sprintf("Container %s in workload %s: resource requests already match the recommendation", container.Name, rec.WorkloadName)
			log.Info().Msg(msg)
			return errors.New(msg)
		}

	} else {
		if cpurequestAlreadyMatch && memRequestAlreadyMatch && cpuLimitAlreadyMatch && memLimitAlreadyMatch {
			msg := fmt.Sprintf("Container %s in workload %s: resource requests and limits already match the recommendation", container.Name, rec.WorkloadName)
			log.Info().Msg(msg)
			return errors.New(msg)
		}
	}

	return nil
}

// validateQuantities is a helper function that validates the CPU and Memory recommendations for a container in the workload.
// It checks that the recommended CPU and Memory requests are not greater than their respective limits.
// Returns a recommendationQuantity struct containing the parsed quantities if valid, or an error if any of the recommendations are invalid.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// returns: A recommendationQuantity struct containing the parsed quantities if valid, or an error if any of the recommendations are invalid.
func (w *Workload) validateRecommendationQuantities(ctx context.Context, rec *model.Recommendation) (*recommendationQuantity, error) {
	recCpu, err := resource.ParseQuantity(rec.CpuRequestRecommendation)
	if err != nil {
		return nil, fmt.Errorf("invalid cpu request recommendation: %v", err)
	}
	recMem, err := resource.ParseQuantity(rec.MemoryRequestRecommendation)
	if err != nil {
		return nil, fmt.Errorf("invalid memory request recommendation: %v", err)
	}

	result := &recommendationQuantity{
		CpuRequestRecommendation:    recCpu,
		MemoryRequestRecommendation: recMem,
	}

	useLimits := ctxkeys.UseLimitsFromContext(ctx)
	if useLimits {
		recCpuLimit, err := resource.ParseQuantity(rec.CpuLimitRecommendation)
		if err != nil {
			return nil, fmt.Errorf("invalid cpu limit recommendation: %v", err)
		}
		recMemLimit, err := resource.ParseQuantity(rec.MemoryLimitRecommendation)
		if err != nil {
			return nil, fmt.Errorf("invalid memory limit recommendation: %v", err)
		}
		//cpu request recommendation cannot be greater than cpu limit recommendation
		if !recCpuLimit.IsZero() && recCpuLimit.Cmp(recCpu) < 0 {
			return nil, fmt.Errorf("cpu request recommendation (%s) cannot be greater than cpu limit recommendation (%s)",
				rec.CpuRequestRecommendation, rec.CpuLimitRecommendation)
		}
		//memory request recommendation cannot be greater than memory limit recommendation
		if !recMemLimit.IsZero() && recMemLimit.Cmp(recMem) < 0 {
			return nil, fmt.Errorf("memory request recommendation (%s) cannot be greater than memory limit recommendation (%s)",
				rec.MemoryRequestRecommendation, rec.MemoryLimitRecommendation)
		}
		result.CpuLimitRecommendation = recCpuLimit
		result.MemoryLimitRecommendation = recMemLimit
	}

	return result, nil
}
