package resizeengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/mcpunzo/k8s-rightsizer/model"
	k8s "github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
)

// WorkloadResizer is an implementation of the Resizer interface that processes recommendations and performs resizing operations on Kubernetes workloads at the workload level, allowing for more coarse-grained control over resource adjustments.
type WorkloadResizer struct {
	BaseResizer
}

// NewWorkloadResizer creates a new instance of WorkloadResizer with the provided Kubernetes client.
// param client: The Kubernetes client used to interact with the cluster.
// param resizeWatcher: The ResizeWatcher used to monitor resize operations.
// param config: The ResizerConfig containing timing and policy configuration.
// returns: A pointer to a new instance of WorkloadResizer.
func NewWorkloadResizer(client k8s.K8sClient, resizeWatcher *watcher.ResizeWatcher, config ResizerConfig) *WorkloadResizer {
	return &WorkloadResizer{
		BaseResizer: BaseResizer{
			config:              config,
			client:              client,
			deploymentWorkload:  k8s.NewDeploymentWorkload(client),
			statefulSetWorkload: k8s.NewStatefulSetWorkload(client),
			podSvc:              k8s.NewPodService(client),
			nodeSvc:             k8s.NewNodeService(client),
			resizeWatcher:       resizeWatcher,
		},
	}
}

// Resize processes a list of recommendations to resize workloads parallel using a specified number of workers.
// It distributes the recommendations into shards based on the workload ID to avoid update conflicts when multiple goroutines attempt to update the same workload.
// Each worker processes recommendations from its assigned shard and sends results to a shared results channel, which are printed as they come in.
// param ctx: The context for managing request deadlines and cancellation.
// param recs: A slice of Recommendations to be processed for resizing.
// param numberOfWorkers: The number of parallel workers to use for processing the recommendations.
// returns: An error if any issues occur during the resizing process.
func (r *WorkloadResizer) Resize(ctx context.Context, recs []model.Recommendation, numberOfWorkers int) error {
	//1. arrange recommendations by workload
	workloadRecs := r.arrangeRecsByWorkload(recs)

	resizeJobs := make(chan []*model.Recommendation, numberOfWorkers) // Buffered channel to hold resize jobs
	results := make(chan string, len(workloadRecs))

	var wg sync.WaitGroup

	// Start a goroutine to print results as they come
	donePrinting := make(chan struct{})
	go func() {
		for res := range results {
			log.Info().Msg(res)
		}
		donePrinting <- struct{}{}
	}()

	// 2. Start worker goroutines
	for range numberOfWorkers {
		wg.Go(func() {
			r.ResizeJob(ctx, resizeJobs, results)
		})
	}

	// 3. Send jobs to the channel
	for _, recGroup := range workloadRecs {
		resizeJobs <- recGroup
	}

	// 4. close the jobs channel and wait for workers to finish
	close(resizeJobs) // Signal to workers that there are no more jobs
	wg.Wait()         // Wait for all workers to finish
	close(results)    // Close the results channel: this will terminate the for loop in the printing goroutine

	<-donePrinting // Wait for the final printing to complete
	return nil

}

// ResizeJob processes a batch of recommendations for a specific workload, performing validation, pre-checks, and resizing operations as needed. It retrieves the current state of the workload, validates the recommendations against the workload's configuration, and attempts to apply the recommended changes. The results of each operation are sent to a shared results channel for logging.
// param ctx: The context for managing request deadlines and cancellation.
// param workloadRecs: A channel that provides batches of recommendations grouped by workload.
// param results: A channel for sending the results of the resizing operations, which will be logged by a separate goroutine.
func (r *WorkloadResizer) ResizeJob(ctx context.Context, workloadRecs <-chan []*model.Recommendation, results chan<- string) {
	for recs := range workloadRecs {
		select {
		case <-ctx.Done():
			log.Info().Msg("Context canceled, stopping ResizeJob")
			return
		default:
			// all rec elements have the same WorkloadID, environment, Kind and Namespace and workload name
			log.Debug().Msgf("Processing recommendation for %s", recs[0].WorkloadID())
			workloadSvc, err := r.lookupWorkloadOps(recs[0].Kind)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", recs[0].WorkloadID(), err)
				resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)

				results <- errMsg
				continue
			}

			//retrieve the current workload
			log.Debug().Msgf("Find Workload %s", recs[0].WorkloadID())
			workload, err := workloadSvc.FindWorkload(ctx, recs[0])
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", recs[0].WorkloadID(), err)
				resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			//validate recommendations returning only the valid ones
			newRecs := r.validateRecommendation(ctx, workload, recs)
			if len(newRecs) == 0 {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: no valid recommendations", workload.Id)
				resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			log.Debug().Msgf("PreCheck assessment for %s", workload.Id)
			err = r.ResizePrecheck(ctx, workloadSvc, workload)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", workload.Id, err)
				resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			log.Debug().Msgf("Cluster nodes assessment for %s", workload.Id)
			err = r.NodeCheck(ctx, workload)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", workload.Id, err)
				resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			log.Info().Msgf("Try resizing %s", workload.Id)
			err = r.ApplyResize(ctx, newRecs, workloadSvc, workload)
			if err != nil {
				errMsg := fmt.Sprintf("[KO] failed to resize %s: %v", workload.Id, err)
				resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeFailed, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			okMsg := fmt.Sprintf("[OK] Resource resized for %s", workload.Id)
			resizeEvent := watcher.CreateResizeEvent(recs, watcher.ResizeSucceeded, okMsg)
			r.resizeWatcher.Notify(resizeEvent)
			results <- okMsg
			select {
			case <-time.After(r.config.InterRecommendationDelay):
			case <-ctx.Done():
				log.Info().Msg("Context canceled during delay, stopping ResizeJob")
				return
			}

		}
	}
}

// validateRecommendation checks the validity of the recommendations for a given workload and returns a filtered list of valid recommendations. It iterates through the provided recommendations, validates each one against the workload's current configuration, and logs any issues encountered during validation. If a recommendation is invalid, it is skipped and not included in the returned list of valid recommendations.
// param ctx: The context for managing request deadlines and cancellation.
// param w: The Workload against which the recommendations will be validated.
// param recs: A slice of Recommendations to be validated.
// returns: A slice of pointers to Recommendations that are valid for the given workload.
func (r *WorkloadResizer) validateRecommendation(ctx context.Context, w *k8s.Workload, recs []*model.Recommendation) []*model.Recommendation {
	newRecs := make([]*model.Recommendation, 0, len(recs))
	for _, rec := range recs {
		err := w.ValidateRecommendations(ctx, rec)
		if err != nil {
			log.Warn().Err(err).Msgf("skipping invalid recommendation for container %s in workload %s", rec.ContainerID(), rec.WorkloadID())
			continue
		}
		newRecs = append(newRecs, rec)
	}

	return newRecs
}

// ArrangeRecsByWorkload takes a slice of Recommendations and organizes them into a map where the key is a unique identifier for each workload (combining namespace, workload name, and workload kind) and the value is a slice of pointers to Recommendations that belong to that workload. This allows for efficient grouping of recommendations by workload, which can be useful for processing them in batches or avoiding conflicts when updating workloads in parallel.
// param recs: A slice of Recommendations to be arranged by workload.
// returns: A map where the key is a unique identifier for each workload and the value is a slice of pointers to Recommendations that belong to that workload.
func (r *WorkloadResizer) arrangeRecsByWorkload(recs []model.Recommendation) map[string][]*model.Recommendation {
	workloadRecs := make(map[string][]*model.Recommendation)
	for _, rec := range recs {
		workloadID := rec.WorkloadID()
		workloadRecs[workloadID] = append(workloadRecs[workloadID], &rec)
	}
	return workloadRecs
}
