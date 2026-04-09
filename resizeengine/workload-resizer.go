package resizeengine

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/model"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	workloadCheckInterval = 10 * time.Second
	workloadCheckTimeout  = 5 * time.Minute
)

// Resizer defines the interface for the resizer that processes recommendations and performs resizing operations on workloads.
type Resizer interface {
	Resize(ctx context.Context, rec []model.Recommendation) error
}

// WorkloadResizer is an implementation of the Resizer interface that processes recommendations and performs resizing operations on Kubernetes workloads (Deployments and StatefulSets).
type WorkloadResizer struct {
	client K8sClient
}

// NewWorkloadResizer creates a new instance of WorkloadResizer with the provided Kubernetes client.
// param client: The Kubernetes client used to interact with the cluster.
// returns: A pointer to a new instance of WorkloadResizer.
func NewWorkloadResizer(client K8sClient) *WorkloadResizer {
	return &WorkloadResizer{client: client}
}

// Resize processes a list of recommendations and performs resizing operations on the corresponding workloads based on their type (Deployment or StatefulSet).
// It retrieves the current state of each workload, applies the resizing changes, and checks the status of the workload after the update.
// If any critical errors are detected during the status check, it attempts to roll back to the original resource values.
// param ctx: The context for managing request deadlines and cancellation.
// param recs: A slice of Recommendations to be processed.
// returns: An error if any issues occur during processing, resizing, or rollback operations.
func (r *WorkloadResizer) Resize(ctx context.Context, recs []model.Recommendation) error {
	for _, rec := range recs {
		log.Printf("Processing %s\n", rec)

		var err error
		var workload WorkloadOps
		switch rec.Type {
		case model.ReplicaSet:
			workload = &DeploymentWorkload{client: r.client}
		case model.StatefuSet:
			workload = &StatefulSetWorkload{client: r.client}
		default:
			err = fmt.Errorf("unsupported resource type for %s", rec.Type)
		}

		if err != nil {
			log.Printf("[KO] failed to resize %s: %v\n", rec, err)
			continue
		}

		err = r.ResizeWorkload(ctx, &rec, workload)
		if err != nil {
			log.Printf("[KO] failed to resize %s: %v\n", rec, err)
			continue
		}

		log.Printf("[OK] Resource resized for %s\n", rec)
	}

	return nil
}

// ResizeWorkload performs the resizing operation on a specific workload based on the provided recommendation and workload operations.
// It retrieves the current state of the workload, applies the resizing changes, and checks the status of the workload after the update.
// If any critical errors are detected during the status check, it attempts to roll back to the original resource values.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// param w: An instance of WorkloadOps that provides methods for finding, resizing, and checking the status of workloads.
// returns: An error if any issues occur during processing, resizing, or rollback operations.
func (r *WorkloadResizer) ResizeWorkload(ctx context.Context, rec *model.Recommendation, w WorkloadOps) error {
	//1. retrieve the current workload
	workload, err := w.FindWorkload(ctx, rec)
	if err != nil {
		return err
	}
	if workload == nil {
		return fmt.Errorf("workload not found for %s", rec)
	}

	// 2. Deep copy in case of rollback
	originalTemplate := workload.Template.DeepCopy()

	err = w.ResizeWorkload(ctx, workload, rec)
	if err != nil {
		return fmt.Errorf("failed to update workload for %s: %v", rec, err)
	}

	// 4. Check status with polling and timeout
	checkStatusFunc := r.CheckWorkloadStatus(ctx, w, workload.Namespace, workload.Name)
	err = wait.PollUntilContextTimeout(ctx, workloadCheckInterval, workloadCheckTimeout, false, checkStatusFunc)

	if err != nil {
		log.Printf("[!!!] ERROR: %v. Rollback Started %s/%s", err, workload.Namespace, workload.Name)

		// Create a new rollback recommendation based on the original template values (the backup)
		rollbackRec := r.CreateRollbackRecommendation(rec, *originalTemplate)
		if rollbackRec == nil {
			return fmt.Errorf("impossible to create rollback recommendation for %s", workload.Name)
		}

		// Retrieve the fresh workload object from the cluster to avoid ResourceVersion conflicts
		freshWorkload, errFetch := w.FindWorkload(ctx, rec)
		if errFetch != nil {
			return fmt.Errorf("critical error: failed to retrieve workload for rollback: %v", errFetch)
		}

		// Perform the resize to the original values (rollback)
		errRollback := w.ResizeWorkload(ctx, freshWorkload, rollbackRec)
		if errRollback != nil {
			return fmt.Errorf("failed update (%v) and failed rollback (%v)", err, errRollback)
		}

		return fmt.Errorf("update canceled and rollback completed successfully: %v", err)
	}

	log.Printf("[SUCCESS] %s/%s updated and stable", workload.Namespace, workload.Name)
	return nil
}

// CreateRollbackRecommendation creates a new Recommendation based on the original resource values from the PodTemplateSpec for rollback purposes.
// It returns a new Recommendation with the original CPU and Memory requests for the target container, or nil if the container is not found in the template.
// param rec: The original Recommendation containing the namespace, workload name, and container information.
// param template: The PodTemplateSpec from which to extract the original resource values for rollback.
// returns: A pointer to a new Recommendation with the original resource values for rollback, or nil if the container is not found in the template.
func (r *WorkloadResizer) CreateRollbackRecommendation(rec *model.Recommendation, template v1.PodTemplateSpec) *model.Recommendation {
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

// CheckWorkloadStatus checks the status of the workload to determine if the rollout has completed successfully or if there are any critical errors.
// It returns a function that can be used with wait.PollUntilContextTimeout to periodically check the status of the workload until it is stable or an error is detected.
// param ctx: The context for managing request deadlines and cancellation.
// param client: The Kubernetes client used to interact with the cluster.
// param w: An instance of WorkloadOps that provides methods for finding, resizing, and checking the status of workloads.
// param namespace: The namespace of the workload.
// param name: The name of the workload.
// returns: A function that can be used with wait.PollUntilContextTimeout to check the status of the workload.
func (r *WorkloadResizer) CheckWorkloadStatus(ctx context.Context, w WorkloadOps, namespace, name string) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		// 1. retrieve the current status of the workload
		status, err := w.GetStatus(ctx, namespace, name)
		if err != nil {
			return false, err
		}

		// 2. Check for critical errors (OOM, CrashLoop, etc.)
		// The CheckPodCriticalErrors function is already generic because it accepts the LabelSelector
		isError, reason := r.CheckPodCriticalErrors(ctx, status.Namespace, status.LabelSelector)
		if isError {
			return false, fmt.Errorf("fail detected: %s", reason)
		}

		// 3. Check if the rollout has completed
		if status.UpdatedReplicas == status.Replicas &&
			status.AvailableReplicas == status.Replicas &&
			status.ObservedGeneration >= status.Generation {
			log.Printf("[%s] Rollout successfully completed", name)
			return true, nil
		}

		log.Printf("[%s] Rollout in progress... (%d/%d ready)", name, status.AvailableReplicas, status.Replicas)
		return false, nil
	}
}

// CheckPodCriticalErrors checks for critical errors in the Pods associated with the workload, such as scheduling issues, CrashLoopBackOff, OOMKilled, etc.
// It returns a boolean indicating whether a critical error was detected and a string with the reason for the error if applicable.
// param ctx: The context for managing request deadlines and cancellation.
// param client: The Kubernetes client used to interact with the cluster.
// param namespace: The namespace of the workload.
// param labelSelector: The label selector used to identify the Pods associated with the workload.
// returns: A boolean indicating whether a critical error was detected, and a string with the reason for the error if applicable.
func (r *WorkloadResizer) CheckPodCriticalErrors(ctx context.Context, namespace string, labelSelector *metav1.LabelSelector) (bool, string) {
	selector, _ := metav1.LabelSelectorAsSelector(labelSelector)
	pods, _ := r.client.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector.String(),
	})

	for _, p := range pods.Items {
		// 1. Check if the Pod is stuck in scheduling (Cluster full or insufficient resources)
		if p.Status.Phase == v1.PodPending {
			for _, cond := range p.Status.Conditions {
				if cond.Type == v1.PodScheduled && cond.Status == v1.ConditionFalse && cond.Reason == "Unschedulable" {
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
