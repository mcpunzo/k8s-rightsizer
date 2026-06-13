package resizeengine

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/model"
	k8s "github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
)

const (
	WorkloadCheckInterval   = 10 * time.Second
	DeploymentCheckTimeout  = 15 * time.Minute
	StatefulsetCheckTimeout = 30 * time.Minute
)

// BaseResizer is a struct that holds common fields for resizers, such as references to deployment and stateful set workloads.
type BaseResizer struct {
	client              k8s.K8sClient
	deploymentWorkload  *k8s.DeploymentWorkload
	statefulSetWorkload *k8s.StatefulSetWorkload
	podSvc              *k8s.PodService
	nodeSvc             *k8s.NodeService
	resizeWatcher       *watcher.ResizeWatcher
}

// ResizePrecheck performs pre-checks before resizing the workload, such as checking for PDB restrictions and UpdateStrategy settings.
// It returns a boolean indicating whether the resize operation should proceed and an error if any issues are detected that would prevent resizing.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the new resource requests and target container information.
// param w: An instance of WorkloadOps that provides methods for finding, resizing, and checking the status of workloads.
// param workload: The Workload struct representing the workload to be resized.
// returns: An error if any issues are detected that would prevent resizing.
func (r *BaseResizer) ResizePrecheck(ctx context.Context, w k8s.WorkloadService, workload *k8s.Workload) error {
	log.Print("Performing ResizePrecheck")
	if workload == nil {
		return fmt.Errorf("workload cannot be nil")
	}

	log.Printf("Performing pre-checks before resizing %s\n", workload.Id)

	// check if the workload is in a paused state
	pause, err := w.IsWorkloadInPausedState(ctx, workload)
	if err != nil {
		return fmt.Errorf("failed to check as workload is paused for %s: %v", workload.Id, err)
	}
	if pause {
		return fmt.Errorf("skipping resize as workload is paused for %s", workload.Id)
	}

	// Check any PDB restrictions before resizing
	IsPDBTooRestrictive, err := r.IsPDBTooRestrictive(ctx, workload.Namespace, workload.LabelSelector)
	if err != nil {
		//TODO this may be configurable: we can decide to fail immediately if we cannot check PDBs, or to proceed with a warning. For now, let's proceed with a warning, but log the error.
		log.Printf("failed to check PDB restrictions for %s: %v. Proceeding with resize, but be aware of potential disruptions.\n", workload.Id, err)
	}
	if IsPDBTooRestrictive {
		return fmt.Errorf("skipping resize due to PDB restrictions for %s: resizing may cause disruption", workload.Id)
	}

	// check UpdateStrategy of the workload and skip if it's set to OnDelete (for Statefulset) or Recreate (for Deployment) to avoid triggering a rollout that may cause downtime. This can be done by retrieving the current UpdateStrategy of the workload and checking its type before proceeding with the resize operation.
	switch workload.UpdateStrategy {
	case "OnDelete":
		return fmt.Errorf("skipping resize due to UpdateStrategy set OnDelete for %s: resizing would have no effect", workload.Id)
	case "Recreate":
		if !ctxkeys.ResizeOnRecreateFromContext(ctx) {
			return fmt.Errorf("skipping resize due to UpdateStrategy set on Recreate for %s: resizing may cause downtime", workload.Id)
		}
		log.Printf("Warning: UpdateStrategy is set to Recreate for %s. Resizing may cause downtime, but proceeding as per configuration.\n", workload.Id)
	}
	// if we are here, it means that the UpdateStrategy is RollingUpdate, so we can proceed with the resize operation.
	// we could add another check on statefulset and check if the partition is set to 0, which means that all pods will be updated at once, and in that case we could skip the resize to avoid potential downtime. This can be done by retrieving the current UpdateStrategy of the statefulset and checking the partition value before proceeding with the resize operation.

	status, err := w.GetStatus(ctx, workload)
	if err != nil {
		return fmt.Errorf("failed to get status for %s: %v", workload.Id, err)
	}
	if status.AvailableReplicas < status.ExpectedReplicas {
		return fmt.Errorf("skipping resize due to a degraded state observed for %s", workload.Id)
	}

	if status.UpdatedReplicas < status.ExpectedReplicas {
		return fmt.Errorf("skipping resize as we are in the middle of a rollout for %s", workload.Id)
	}

	isThereAnError, reason := r.podSvc.CheckPodCriticalErrors(ctx, workload)
	if isThereAnError {
		return fmt.Errorf("skipping resize due to critical error detected in pods for %s: %s", workload.Id, reason)
	}

	return nil
}

// NodeCheck performs checks on the cluster nodes to ensure that there are enough compatible and ready nodes to accommodate the resized pods after the resize operation.
// It checks for namespace congestion by counting the number of pending pods in the namespace and returns an error if there are too many pending pods, which may indicate a cluster-wide scheduling issue. It also checks for architectural constraints by counting the number of compatible and ready nodes based on the specified architecture and returns an error if there are not enough compatible or ready nodes to accommodate the resized pods.
// param ctx: The context for managing request deadlines and cancellation.
// param workload: The Workload struct representing the workload to be resized.
// returns: An error if any issues are detected that would prevent resizing.
func (r *BaseResizer) NodeCheck(ctx context.Context, workload *k8s.Workload) error {
	// 1. Check namespace congestion: if there are already too many pending pods in the namespace, it may indicate a cluster-wide scheduling issue that would prevent our pods from starting up successfully after the resize. In that case, we should fail fast and avoid triggering a rollout that is likely to fail.
	podList, err := r.podSvc.Find(ctx, workload.Namespace, "status.phase=Pending")
	if err != nil {
		return fmt.Errorf("failed to find pending pods in namespace %s: %v", workload.Namespace, err)
	}

	if len(podList) > 3 {
		return fmt.Errorf("namespace congestion: %d pods already pending", len(podList))
	}

	//2. architectural constraints: if the workload is currently running on nodes with specific architectural constraints (e.g., GPU, ARM), we should check if there are enough available nodes with those constraints to accommodate the resized pods. If not, we should fail fast to avoid triggering a rollout that is likely to fail due to scheduling issues.
	architecture := workload.Template.Spec.NodeSelector["kubernetes.io/arch"]
	nodeStats, err := r.nodeSvc.Find(ctx, architecture)
	if err != nil {
		return fmt.Errorf("failed to find nodes for architecture %s: %v", architecture, err)
	}

	// if less than 50% of the total nodes are Ready, the cluster is unstable
	if nodeStats.ReadyNodesCount < (nodeStats.NumberOfNodes / 2) {
		return fmt.Errorf("cluster instability: more than 50%% of nodes are NotReady")
	}

	// if there are no nodes compatible with the pod's architecture, the rollout will fail due to scheduling issues, so we should fail fast with a clear message about the potential architecture mismatch (e.g., Graviton vs x86).
	if nodeStats.CompatibleNodesCount == 0 {
		return fmt.Errorf("no compatible architecture nodes available (possible Graviton/x86 mismatch)")
	}

	return nil
}

// CreateRollbackRecommendation processes the recommendations for resizing workloads. It iterates through the provided recommendations, retrieves the current state of the workload, validates the recommendations, performs pre-checks, and attempts to resize the workload. The results of each operation are sent to the results channel.
// param recs: The slice of Recommendations containing the new resource requests and target container information. The function assumes that all recommendations in the slice are for the same workload and container, as they are processed together in the ResizeJob.
// param template: The original PodTemplateSpec of the workload, used to create rollback recommendations in case of errors during resizing.
// returns: A slice of Recommendations that can be used for rolling back to the original resource values if needed.
func (r *BaseResizer) CreateRollbackRecommendation(recs []*model.Recommendation, template v1.PodTemplateSpec) []*model.Recommendation {
	newRecs := make([]*model.Recommendation, 0, len(template.Spec.Containers))

	for _, c := range template.Spec.Containers {
		newRec := &model.Recommendation{
			Namespace:                   recs[0].Namespace,
			WorkloadName:                recs[0].WorkloadName,
			Container:                   c.Name,
			CpuRequestRecommendation:    c.Resources.Requests.Cpu().String(),
			CpuLimitRecommendation:      c.Resources.Limits.Cpu().String(),
			MemoryRequestRecommendation: c.Resources.Requests.Memory().String(),
			MemoryLimitRecommendation:   c.Resources.Limits.Memory().String(),
			Kind:                        recs[0].Kind,
		}

		newRecs = append(newRecs, newRec)
	}

	return newRecs
}

// CheckWorkloadStatus checks the status of the workload to determine if the rollout has completed successfully or if there are any critical errors.
// It returns a function that can be used with wait.PollUntilContextTimeout to periodically check the status of the workload until it is stable or an error is detected.
// param ctx: The context for managing request deadlines and cancellation.
// param client: The Kubernetes client used to interact with the cluster.
// param w: An instance of WorkloadOps that provides methods for finding, resizing, and checking the status of workloads.
// param workload: The Workload struct representing the workload.
// returns: A function that can be used with wait.PollUntilContextTimeout to check the status of the workload.
func (r *BaseResizer) CheckWorkloadStatus(ctx context.Context, w k8s.WorkloadService, workload *k8s.Workload) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		// 1. retrieve the current status of the workload
		status, err := w.GetStatus(ctx, workload)
		if err != nil {
			return false, err
		}

		// 2. Check for critical errors (OOM, CrashLoop, etc.)
		// The CheckPodCriticalErrors function is already generic because it accepts the LabelSelector
		isError, reason := r.podSvc.CheckPodCriticalErrors(ctx, workload)
		if isError {
			return false, fmt.Errorf("fail detected in workload %s: %s", workload.Id, reason)
		}

		// 3. Check if the rollout has completed
		if status.UpdatedReplicas == status.ExpectedReplicas &&
			status.AvailableReplicas == status.ExpectedReplicas &&
			status.ObservedGeneration >= status.Generation {
			log.Printf("[%s] Rollout successfully completed", workload.Id)
			return true, nil
		}

		log.Printf("[%s] Rollout in progress... (%d/%d ready)", workload.Id, status.AvailableReplicas, status.ExpectedReplicas)
		return false, nil
	}
}

// IsPDBRestrictions checks if there are any Pod Disruption Budget restrictions that would prevent the workload from being safely resized.
// It returns a boolean indicating whether PDB restrictions are present and an error if the check fails.
// param ctx: The context for managing request deadlines and cancellation.
// param namespace: The namespace of the workload.
// param labelSelector: The label selector used to identify the Pods associated with the workload.
// returns: A boolean indicating whether PDB restrictions are present and too restrictive, false if they are not, and an error if the check fails.
func (r *BaseResizer) IsPDBTooRestrictive(ctx context.Context, namespace string, labelSelector *metav1.LabelSelector) (bool, error) {
	if labelSelector == nil {
		return false, nil // No selector, we assume no PDB applies
	}

	// Convert the workload selector (supports both MatchLabels and MatchExpressions)
	// to a labels.Selector so we can convert it to a label set for matching against PDB selectors.
	workloadSelector, err := metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return false, fmt.Errorf("failed to parse workload label selector: %v", err)
	}

	// An empty selector (no MatchLabels and no MatchExpressions) matches everything,
	// which is semantically equivalent to having no PDB selector — skip the check.
	if workloadSelector.Empty() {
		return false, nil
	}

	pdbs, err := r.client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, fmt.Errorf("failed to list Pod Disruption Budgets: %v", err)
	}

	// Build a label set from MatchLabels for PDB matching.
	// MatchExpressions are already covered via the workloadSelector check above,
	// but PDB selectors are matched against the workload's labels, not vice-versa,
	// so we need the raw label map here.
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

// lookupWorkloadOps returns the appropriate WorkloadOps implementation based on the Kind of the workload in the recommendation.
// It supports Deployment, ReplicaSet, and StatefulSet kinds, and returns an error for unsupported kinds.
// param kind: The Kind of the workload (e.g., Deployment, ReplicaSet, StatefulSet).
// returns: An instance of WorkloadOps corresponding to the workload kind, or an error if the kind is unsupported.
func (r *BaseResizer) lookupWorkloadOps(kind model.Kind) (k8s.WorkloadService, error) {
	switch kind {
	case model.Deployment:
		return r.deploymentWorkload, nil
	case model.ReplicaSet:
		return r.deploymentWorkload, nil
	case model.StatefulSet:
		return r.statefulSetWorkload, nil
	default:
		return nil, fmt.Errorf("unsupported resource Kind for %s", kind)
	}
}

// ApplyResize performs the resizing operation on a specific workload based on the provided recommendation and workload operations.
// It retrieves the current state of the workload, applies the resizing changes, and checks the status of the workload after the update.
// If any critical errors are detected during the status check, it attempts to roll back to the original resource values.
// param ctx: The context for managing request deadlines and cancellation.
// param recs: The slice of Recommendations containing the new resource requests and target container information. The function assumes that all recommendations in the slice are for the same workload and container, as they are processed together in the ResizeJob.
// param w: An instance of WorkloadOps that provides methods for finding, resizing, and checking the status of workloads.
// param workload: The Workload struct representing the workload to be resized.
// returns: An error if any issues occur during processing, resizing, or rollback operations.
func (r *BaseResizer) ApplyResize(ctx context.Context, recs []*model.Recommendation, w k8s.WorkloadService, workload *k8s.Workload) error {
	if len(recs) == 0 {
		return fmt.Errorf("failed to update workload: no recommendations provided")
	}

	if workload == nil {
		return fmt.Errorf("failed to update workload: workload cannot be nil")
	}

	if workload.Template == nil {
		return fmt.Errorf("failed to update workload for %s: workload template is nil", workload.Id)
	}

	// Deep copy in case of rollback
	originalTemplate := workload.Template.DeepCopy()

	err := w.ResizeWorkload(ctx, workload, recs)
	if err != nil {
		return fmt.Errorf("failed to update workload for %s: %v", workload.Id, err)
	}

	// Check status with polling and timeout
	workloadCheckTimeout := DeploymentCheckTimeout
	if workload.WorkloadType == k8s.StatefulSet {
		workloadCheckTimeout = StatefulsetCheckTimeout
	}

	checkStatusFunc := r.CheckWorkloadStatus(ctx, w, workload)
	err = wait.PollUntilContextTimeout(ctx, WorkloadCheckInterval, workloadCheckTimeout, false, checkStatusFunc)

	if err != nil {
		log.Printf("[!!!] ERROR: %v. Rollback Started for %s", err, workload.Id)

		// Create a new rollback recommendation based on the original template values (the backup)
		rollbackRecs := r.CreateRollbackRecommendation(recs, *originalTemplate)
		if len(rollbackRecs) == 0 {
			return fmt.Errorf("impossible to create rollback recommendation for %s", workload.Id)
		}

		// Use a fresh context for rollback: the polling context may already be expired.
		rollbackCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), workloadCheckTimeout)
		defer cancel()
		// Retry on conflict: the deployment may still be rolling out when we try to update it,
		// causing a ResourceVersion conflict ("object has been modified"). Re-fetch and retry.
		errRollback := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			// Retrieve the fresh workload object from the cluster to avoid ResourceVersion conflicts
			freshWorkload, errFetch := w.FindWorkload(rollbackCtx, recs[0])
			if errFetch != nil {
				return fmt.Errorf("failed to retrieve workload for rollback: %v", errFetch)
			}

			// Perform the resize to the original values (rollback)
			return w.ResizeWorkload(rollbackCtx, freshWorkload, rollbackRecs)
		})

		if errRollback != nil {
			return fmt.Errorf("failed update (%v) and failed rollback (%v)", err, errRollback)
		}

		// After a successful rollback API call, verify that the workload has actually
		// converged back to a stable state.
		rollbackVerifyCtx, cancelVerify := context.WithTimeout(context.WithoutCancel(ctx), workloadCheckTimeout)
		defer cancelVerify()

		rollbackWorkload, errFetchRollback := w.FindWorkload(rollbackVerifyCtx, recs[0])
		if errFetchRollback != nil {
			return fmt.Errorf("update failed (%v), rollback completed but failed to retrieve workload for verification (%v)", err, errFetchRollback)
		}

		errRollbackVerification := wait.PollUntilContextTimeout(
			rollbackVerifyCtx,
			WorkloadCheckInterval,
			workloadCheckTimeout,
			false,
			r.CheckWorkloadStatus(rollbackVerifyCtx, w, rollbackWorkload),
		)
		if errRollbackVerification != nil {
			return fmt.Errorf("update failed (%v), rollback completed but workload is not stable (%v)", err, errRollbackVerification)
		}

		return fmt.Errorf("update canceled and rollback completed successfully: %v", err)
	}

	log.Printf("[SUCCESS] %s/%s updated and stable", workload.Namespace, workload.Name)
	return nil
}
