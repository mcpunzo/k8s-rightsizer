package resizeengine

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/model"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
)

const (
	WorkloadCheckInterval   = 10 * time.Second
	DeploymentCheckTimeout  = 5 * time.Minute
	StatefulsetCheckTimeout = 15 * time.Minute
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
	deploymentWorkload := &DeploymentWorkload{client: r.client}
	statefulSetWorkload := &StatefulSetWorkload{client: r.client}

	for _, rec := range recs {
		log.Printf("Processing %s\n", rec)

		var err error
		var workload WorkloadOps
		switch rec.Kind {
		case model.ReplicaSet:
			workload = deploymentWorkload
		case model.StatefulSet:
			workload = statefulSetWorkload
		default:
			err = fmt.Errorf("unsupported resource Kind for %s", rec.Kind)
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
		time.Sleep(1 * time.Second) // Small delay between processing recommendations to avoid overwhelming the cluster
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
	//retrieve the current workload
	workload, err := w.FindWorkload(ctx, rec)
	if err != nil {
		return err
	}

	tryResize, err := r.ResizePrecheck(ctx, rec, w, workload)
	if err != nil {
		return err
	}

	if !tryResize {
		return nil
	}

	// Deep copy in case of rollback
	originalTemplate := workload.Template.DeepCopy()

	err = w.ResizeWorkload(ctx, workload, rec)
	if err != nil {
		return fmt.Errorf("failed to update workload for %s: %v", rec, err)
	}

	// Check status with polling and timeout
	workloadCheckTimeout := DeploymentCheckTimeout
	if workload.WorkloadType == StatefulSet {
		workloadCheckTimeout = StatefulsetCheckTimeout
	}

	checkStatusFunc := r.CheckWorkloadStatus(ctx, w, workload)
	err = wait.PollUntilContextTimeout(ctx, WorkloadCheckInterval, workloadCheckTimeout, false, checkStatusFunc)

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

// ResizePrecheck performs pre-checks before resizing the workload, such as checking for PDB restrictions and UpdateStrategy settings.
// It returns a boolean indicating whether the resize operation should proceed and an error if any issues are detected that would prevent resizing.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// param w: An instance of WorkloadOps that provides methods for finding, resizing, and checking the status of workloads.
// param workload: The Workload struct representing the workload to be resized.
// returns: A boolean indicating whether the resize operation should proceed, and an error if any issues are detected that would prevent resizing.
func (r *WorkloadResizer) ResizePrecheck(ctx context.Context, rec *model.Recommendation, w WorkloadOps, workload *Workload) (bool, error) {
	if workload == nil {
		return false, fmt.Errorf("workload not found for %s", rec)
	}

	// check if the workload is in a paused state
	pause, err := w.IsWorkloadInPausedState(ctx, workload)
	if err != nil {
		return false, fmt.Errorf("failed to check if workload is paused for %s: %v", rec, err)
	}
	if pause {
		return false, fmt.Errorf("skipping resize as workload is paused for %s", rec)
	}

	// Check any PDB restrictions before resizing
	IsPDBTooRestrictive, err := r.IsPDBTooRestrictive(ctx, workload.Namespace, workload.LabelSelector)
	if err != nil {
		//TODO this may be configurable: we can decide to fail immediately if we cannot check PDBs, or to proceed with a warning. For now, let's proceed with a warning, but log the error.
		log.Printf("failed to check PDB restrictions for %s: %v. Proceeding with resize, but be aware of potential disruptions.\n", rec, err)
	}
	if IsPDBTooRestrictive {
		return false, fmt.Errorf("skipping resize due to PDB restrictions for %s: resizing may cause disruption", rec)
	}

	// check UpdateStrategy of the workload and skip if it's set to OnDelete (for Statefulset) or Recreate (for Deployment) to avoid triggering a rollout that may cause downtime. This can be done by retrieving the current UpdateStrategy of the workload and checking its type before proceeding with the resize operation.
	switch workload.UpdateStrategy {
	case "OnDelete":
		return false, fmt.Errorf("skipping resize due to UpdateStrategy set OnDelete for %s: resizing would have no effect", rec)
	case "Recreate":
		//TODO: we could make this configurable by allowing the user to choose whether to proceed with a warning or to skip, but for now let's skip to be safe.
		return false, fmt.Errorf("skipping resize due to UpdateStrategy set Recreate for %s: resizing may cause downtime", rec)
	}
	// if we are here, it means that the UpdateStrategy is RollingUpdate, so we can proceed with the resize operation.
	// we could add another check on statefulset and check if the partition is set to 0, which means that all pods will be updated at once, and in that case we could skip the resize to avoid potential downtime. This can be done by retrieving the current UpdateStrategy of the statefulset and checking the partition value before proceeding with the resize operation.

	status, err := w.GetStatus(ctx, workload)
	if err != nil {
		return false, fmt.Errorf("failed to get status for %s: %v", rec, err)
	}
	if status.AvailableReplicas < status.ExpectedReplicas {
		return false, fmt.Errorf("skipping resize due to adegraded state observed for %s: %v", rec, err)
	}

	if status.UpdatedReplicas < status.ExpectedReplicas {
		return false, fmt.Errorf("skipping resize as we are in the middle of a rollout for %s: %v", rec, err)
	}

	isThereAnError, reason := r.CheckPodCriticalErrors(ctx, workload)
	if isThereAnError {
		return false, fmt.Errorf("skipping resize due to critical error detected in pods for %s: %s", rec, reason)
	}

	return true, nil
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
// param workload: The Workload struct representing the workload.
// returns: A function that can be used with wait.PollUntilContextTimeout to check the status of the workload.
func (r *WorkloadResizer) CheckWorkloadStatus(ctx context.Context, w WorkloadOps, workload *Workload) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		// 1. retrieve the current status of the workload
		status, err := w.GetStatus(ctx, workload)
		if err != nil {
			return false, err
		}

		// 2. Check for critical errors (OOM, CrashLoop, etc.)
		// The CheckPodCriticalErrors function is already generic because it accepts the LabelSelector
		isError, reason := r.CheckPodCriticalErrors(ctx, workload)
		if isError {
			return false, fmt.Errorf("fail detected: %s", reason)
		}

		// 3. Check if the rollout has completed
		if status.UpdatedReplicas == status.ExpectedReplicas &&
			status.AvailableReplicas == status.ExpectedReplicas &&
			status.ObservedGeneration >= status.Generation {
			log.Printf("[%s] Rollout successfully completed", workload.Name)
			return true, nil
		}

		log.Printf("[%s] Rollout in progress... (%d/%d ready)", workload.Name, status.AvailableReplicas, status.ExpectedReplicas)
		return false, nil
	}
}

// CheckPodCriticalErrors checks for critical errors in the Pods associated with the workload, such as scheduling issues, CrashLoopBackOff, OOMKilled, etc.
// It returns a boolean indicating whether a critical error was detected and a string with the reason for the error if applicable.
// param ctx: The context for managing request deadlines and cancellation.
// param client: The Kubernetes client used to interact with the cluster.
// param namespace: The namespace of the workload.
// returns: A boolean indicating whether a critical error was detected, and a string with the reason for the error if applicable.
func (r *WorkloadResizer) CheckPodCriticalErrors(ctx context.Context, workload *Workload) (bool, string) {
	selector, _ := metav1.LabelSelectorAsSelector(workload.LabelSelector)
	pods, _ := r.client.CoreV1().Pods(workload.Namespace).List(ctx, metav1.ListOptions{
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

// IsPDBRestrictions checks if there are any Pod Disruption Budget restrictions that would prevent the workload from being safely resized.
// It returns a boolean indicating whether PDB restrictions are present and an error if the check fails.
// param ctx: The context for managing request deadlines and cancellation.
// param namespace: The namespace of the workload.
// param labelSelector: The label selector used to identify the Pods associated with the workload.
// returns: A boolean indicating whether PDB restrictions are present and too restrictive, false if they are not, and an error if the check fails.
func (r *WorkloadResizer) IsPDBTooRestrictive(ctx context.Context, namespace string, labelSelector *metav1.LabelSelector) (bool, error) {
	if labelSelector == nil || len(labelSelector.MatchLabels) == 0 {
		return false, nil // No selectors, we assume no PDB
	}

	pdbs, err := r.client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list Pod Disruption Budgets: %v", err)
	}

	workloadLabels := labels.Set(labelSelector.MatchLabels)

	for _, pdb := range pdbs.Items {
		pdbSelector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
		if err != nil {
			continue
		}

		// check if the PDB protects our Pods by matching the labels
		if pdbSelector.Matches(workloadLabels) {
			// The correct field is DisruptionsAllowed, not DesiredHealthy.
			// If DisruptionsAllowed is 0, it means that no disruptions are currently allowed,
			// which would prevent us from safely resizing the workload.
			if pdb.Status.DisruptionsAllowed == 0 {
				return true, nil
			}
		}
	}

	return false, nil
}
