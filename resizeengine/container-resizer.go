package resizeengine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/mcpunzo/k8s-rightsizer/model"
	k8s "github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	"github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/sharding"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
)

// ContainerResizer is an implementation of the Resizer interface that processes recommendations and performs resizing operations on Kubernetes workloads at the container level, allowing for more granular control over resource adjustments.
type ContainerResizer struct {
	BaseResizer
}

// NewContainerResizer creates a new instance of ContainerResizer with the provided Kubernetes client.
// param client: The Kubernetes client used to interact with the cluster.
// param resizeWatcher: The ResizeWatcher used to monitor resize events.
// returns: A pointer to a new instance of ContainerResizer.
func NewContainerResizer(client k8s.K8sClient, resizeWatcher *watcher.ResizeWatcher) *ContainerResizer {
	return &ContainerResizer{
		BaseResizer: BaseResizer{
			client:              client,
			deploymentWorkload:  k8s.NewDeploymentWorkload(client),
			statefulSetWorkload: k8s.NewStatefulSetWorkload(client),
			podSvc:              k8s.NewPodService(client),
			nodeSvc:             k8s.NewNodeService(client),
			resizeWatcher:       resizeWatcher,
		},
	}
}

// Resize processes a list of recommendations to resize workloads container by containerin parallel using a specified number of workers.
// It distributes the recommendations into shards based on the workload ID to avoid update conflicts when multiple goroutines attempt to update the same workload.
// Each worker processes recommendations from its assigned shard and sends results to a shared results channel, which are printed as they come in.
// param ctx: The context for managing request deadlines and cancellation.
// param recs: A slice of Recommendations to be processed for resizing.
// param numberOfWorkers: The number of parallel workers to use for processing the recommendations.
// returns: An error if any issues occur during the resizing process.
func (r *ContainerResizer) Resize(ctx context.Context, recs []model.Recommendation, numberOfWorkers int) error {
	// Problem: with parallel resizing executions and a resize strategy by container
	// we can incurr in situation where more than 1 goroutines update the same workload causing an update conflict
	// Solution: Create Shards, i.e. dedicated channel per worker and recs for the same workload are always send to
	// the same shard and then processed by the same worker, avoiding update conflicts and retries.
	resizeJobShards := make([]chan *model.Recommendation, numberOfWorkers)
	for i := range resizeJobShards {
		resizeJobShards[i] = make(chan *model.Recommendation, len(recs))
	}

	results := make(chan string, len(recs))

	// Start a goroutine to print results as they come
	donePrinting := make(chan struct{})
	go func() {
		for res := range results {
			log.Info().Msg(res)
		}
		donePrinting <- struct{}{}
	}()

	var wg sync.WaitGroup
	for i := range resizeJobShards {
		wg.Go(func() {
			r.ResizeJob(ctx, resizeJobShards[i], results)
		})
	}

	//distribute recs to channels according to the WorkloadId
	for _, rec := range recs {
		shardId := sharding.GetShardIndex(rec.WorkloadID(), numberOfWorkers)
		resizeJobShards[shardId] <- &rec
	}

	//cleanup
	for i := range resizeJobShards {
		close(resizeJobShards[i])
	}

	wg.Wait()
	close(results)

	<-donePrinting // Wait for the printing goroutine to finish before exiting
	return nil
}

// ResizeJob is a worker function that processes recommendations from the recs channel, performs resizing operations on the corresponding workloads, and sends the results to the results channel.
// It listens for recommendations and context cancellation, and handles errors appropriately while processing each recommendation.
// param ctx: The context for managing request deadlines and cancellation.
// param recs: A channel from which to receive Recommendations to be processed.
// param results: A channel to which to send the results of the resizing operations.
func (r *ContainerResizer) ResizeJob(ctx context.Context, recs <-chan *model.Recommendation, results chan<- string) {
	for rec := range recs {
		select {
		case <-ctx.Done():
			log.Info().Msg("Context canceled, stopping ResizeJob")
			return
		default:
			log.Debug().Msgf("Processing recommendation for %s", rec.ContainerID())

			workloadSvc, err := r.lookupWorkloadOps(rec.Kind)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", rec.ContainerID(), err)
				resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			//retrieve the current workload
			log.Debug().Msgf("Find Workload %s", rec.ContainerID())
			workload, err := workloadSvc.FindWorkload(ctx, rec)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", rec.ContainerID(), err)
				resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			//validate the recommendations before trying to resize, to fail fast if there are any issues with the recs (e.g. invalid values, or values that would cause errors if applied)
			log.Debug().Msgf("Validate recommendations for %s", rec.ContainerID())
			if err := workload.ValidateRecommendations(ctx, rec); err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", rec.ContainerID(), err)
				resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			log.Debug().Msgf("PreCheck assessment for %s", rec.ContainerID())
			err = r.ResizePrecheck(ctx, workloadSvc, workload)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", rec.ContainerID(), err)
				resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			log.Debug().Msgf("Cluster nodes assessment for %s", rec.ContainerID())
			err = r.NodeCheck(ctx, workload)
			if err != nil {
				errMsg := fmt.Sprintf("[SKIP] skip resizing %s: %v", rec.ContainerID(), err)
				resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeSkipped, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			log.Info().Msgf("Try resizing %s", rec.ContainerID())
			err = r.ApplyResize(ctx, []*model.Recommendation{rec}, workloadSvc, workload)
			if err != nil {
				errMsg := fmt.Sprintf("[KO] failed to resize %s: %v", rec.ContainerID(), err)
				resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeFailed, errMsg)
				r.resizeWatcher.Notify(resizeEvent)
				results <- errMsg
				continue
			}

			okMsg := fmt.Sprintf("[OK] Resource resized for %s", rec.ContainerID())
			resizeEvent := watcher.CreateResizeEvent([]*model.Recommendation{rec}, watcher.ResizeSucceeded, okMsg)
			r.resizeWatcher.Notify(resizeEvent)
			results <- okMsg

			select {
			case <-time.After(InterRecommendationDelay):
			case <-ctx.Done():
				log.Info().Msg("Context canceled during delay, stopping ResizeJob")
				return
			}
		}
	}
}
