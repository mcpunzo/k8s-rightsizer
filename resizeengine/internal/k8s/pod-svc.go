package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const lastTerminationOOMWindow = 30 * time.Minute

var criticalWaitingReasons = map[string]struct{}{
	"CrashLoopBackOff":           {},
	"ImagePullBackOff":           {},
	"ErrImagePull":               {},
	"CreateContainerConfigError": {},
	"CreateContainerError":       {},
	"RunContainerError":          {},
	"ContainerCannotRun":         {},
}

type Pod struct {
	Name      string
	Namespace string
}

// PodService is a service that provides methods to interact with Kubernetes Pods.
type PodService struct {
	client K8sClient
}

// NewPodService creates a new instance of PodService with the provided Kubernetes client.
// param client: The Kubernetes client used to interact with the cluster.
// returns: A pointer to a new instance of PodService.
func NewPodService(client K8sClient) *PodService {
	return &PodService{client: client}
}

// Find retrieves a list of pods in the specified namespace that match the given label selector. It returns a slice of Pod structs or an error if the operation fails.
// param ctx: The context for managing request deadlines and cancellation.
// param namespace: The namespace in which to search for pods.
// param selector: The label selector used to filter pods based on their labels.
// returns: A slice of Pod structs representing the matching pods, or an error if the operation fails.
func (s *PodService) Find(ctx context.Context, namespace, fieldselector string) ([]*Pod, error) {
	podList, err := s.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fieldselector,
	})
	if err != nil {
		return nil, err
	}

	var pods []*Pod
	for _, item := range podList.Items {
		pods = append(pods, &Pod{
			Name:      item.Name,
			Namespace: item.Namespace,
		})
	}

	return pods, nil
}

// CheckPodCriticalErrors checks for critical errors in the Pods associated with the workload, such as scheduling issues, CrashLoopBackOff, OOMKilled, etc.
// It returns a boolean indicating whether a critical error was detected and a string with the reason for the error if applicable.
// param ctx: The context for managing request deadlines and cancellation.
// param client: The Kubernetes client used to interact with the cluster.
// param namespace: The namespace of the workload.
// returns: A boolean indicating whether a critical error was detected, and a string with the reason for the error if applicable.
func (s *PodService) CheckPodCriticalErrors(ctx context.Context, workload *Workload) (bool, string) {
	selector, err := metav1.LabelSelectorAsSelector(workload.LabelSelector)
	if err != nil {
		return false, fmt.Sprintf("[WARN] failed to create label selector for %s: %v. Skipping critical error check, but be aware of potential undetected issues.", workload.Id, err)
	}

	pods, err := s.client.CoreV1().Pods(workload.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})

	if err != nil {
		return false, fmt.Sprintf("[WARN] failed to list pods for %s: %v. Skipping critical error check, but be aware of potential undetected issues.", workload.Id, err)
	}

	for _, p := range pods.Items {
		// Ignore pods that are terminating or already terminal to avoid stale failures
		// from old replicas during rollouts.
		if p.DeletionTimestamp != nil || p.Status.Phase == v1.PodSucceeded || p.Status.Phase == v1.PodFailed {
			continue
		}

		// 1. Check if the Pod is stuck in scheduling (Cluster full or insufficient resources)
		if p.Status.Phase == v1.PodPending {
			for _, cond := range p.Status.Conditions {
				if cond.Type == v1.PodScheduled && cond.Status == v1.ConditionFalse && cond.Reason == "Unschedulable" {
					if autoscalerCanLikelyHelpUnschedulable(cond.Message) {
						return false, fmt.Sprintf("[WARN] Cluster saturation detected for %s: %s. Autoscaler may add nodes, continuing.", workload.Id, cond.Message)
					}

					return true, fmt.Sprintf("Unschedulable pod for %s: %s. Likely not recoverable via autoscaler", workload.Id, cond.Message)
				}
			}
		}

		// 2. Check the status of individual containers (including init containers)
		if isError, reason := checkContainerStatusesForCriticalErrors(p.Status.ContainerStatuses, time.Now()); isError {
			return true, reason
		}
		if isError, reason := checkContainerStatusesForCriticalErrors(p.Status.InitContainerStatuses, time.Now()); isError {
			return true, reason
		}
	}
	return false, ""
}

func checkContainerStatusesForCriticalErrors(statuses []v1.ContainerStatus, now time.Time) (bool, string) {
	for _, cs := range statuses {
		if cs.State.Waiting != nil {
			reason := cs.State.Waiting.Reason
			if _, ok := criticalWaitingReasons[reason]; ok {
				return true, fmt.Sprintf("Container in error: %s", reason)
			}
		}

		if cs.State.Terminated != nil {
			if isCriticalTerminationReason(cs.State.Terminated.Reason) {
				return true, fmt.Sprintf("Container terminated with reason: %s", cs.State.Terminated.Reason)
			}
		}

		if cs.LastTerminationState.Terminated != nil &&
			isCriticalTerminationReason(cs.LastTerminationState.Terminated.Reason) &&
			isRecentTermination(cs.LastTerminationState.Terminated, now, lastTerminationOOMWindow) {
			return true, fmt.Sprintf("Container recently terminated with reason: %s", cs.LastTerminationState.Terminated.Reason)
		}
	}

	return false, ""
}

func isCriticalTerminationReason(reason string) bool {
	return reason == "OOMKilled" || reason == "Error" || reason == "ContainerCannotRun"
}

// isRecentTermination checks if a container termination event occurred within a specified time window. It returns true if the termination event is recent, indicating a potential ongoing issue with insufficient memory for startup.
// param t: The ContainerStateTerminated object representing the termination event of a container.
// param now: The current time used as a reference point for determining the recency of the termination event.
// param window: The duration defining the time window within which a termination event is considered recent.
// returns: A boolean indicating whether the termination event occurred within the specified time window.
func isRecentTermination(t *v1.ContainerStateTerminated, now time.Time, window time.Duration) bool {
	if t == nil {
		return false
	}

	if t.FinishedAt.IsZero() {
		return true
	}

	return t.FinishedAt.Time.After(now.Add(-window))
}

func autoscalerCanLikelyHelpUnschedulable(message string) bool {
	msg := strings.ToLower(message)

	// Hard scheduling constraints usually cannot be fixed by adding generic nodes.
	nonRecoverableHints := []string{
		"didn't match node selector",
		"did not match node selector",
		"didn't match pod affinity",
		"did not match pod affinity",
		"didn't match pod anti-affinity",
		"did not match pod anti-affinity",
		"untolerated taint",
		"had taint",
		"volume node affinity conflict",
		"persistentvolumeclaim is not bound",
	}

	for _, hint := range nonRecoverableHints {
		if strings.Contains(msg, hint) {
			return false
		}
	}

	// Resource pressure can often be mitigated by Cluster Autoscaler scale-out.
	recoverableHints := []string{
		"insufficient cpu",
		"insufficient memory",
		"insufficient ephemeral-storage",
		"too many pods",
	}

	for _, hint := range recoverableHints {
		if strings.Contains(msg, hint) {
			return true
		}
	}

	return false
}
