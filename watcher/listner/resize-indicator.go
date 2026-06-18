package listner

import (
	"sync"

	"github.com/mcpunzo/k8s-rightsizer/watcher"
	"github.com/rs/zerolog/log"
)

// ResizeIndicator is a struct that implements the Listener interface and keeps track of the number of recommendations processed.
type ResizeIndicator struct {
	// NumberOfRecommendations is the total number of recommendations that have been processed.
	NumberOfRecommendations int
	// RecommendationProcessed is the number of recommendations that have been processed so far.
	RecommendationProcessed int
	// StatusMap is a map that keeps track of the count of each resize status.
	StatusMap map[watcher.ResizeStatus]int

	lock *sync.RWMutex
}

// NewResizeIndicator creates a new instance of ResizeIndicator with the specified number of recommendations.
// param numberOfRecommendations The total number of recommendations to be processed.
// returns a pointer to the newly created ResizeIndicator instance.
func NewResizeIndicator(numberOfRecommendations int) *ResizeIndicator {
	return &ResizeIndicator{
		NumberOfRecommendations: numberOfRecommendations,
		RecommendationProcessed: 0,
		StatusMap:               make(map[watcher.ResizeStatus]int),
		lock:                    &sync.RWMutex{},
	}
}

// HandleResizeEvent is a method that handles resize events and updates the count of processed recommendations accordingly.
// It also updates the status map with the count of each resize status and logs the progress of processing recommendations.
// param event The resize event that contains information about the recommendations processed and their status.
func (r *ResizeIndicator) HandleResizeEvent(event *watcher.ResizeEvent) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.RecommendationProcessed += len(event.Recommendation)
	r.StatusMap[event.Status] += len(event.Recommendation)

	log.Info().Msgf("ResizeIndicator: Processed %d/%d recommendations", r.RecommendationProcessed, r.NumberOfRecommendations)

	log.Info().Msgf("%s/%d, %s/%d, %s/%d, %s/%d, %s/%d", watcher.ResizeSucceeded, r.StatusMap[watcher.ResizeSucceeded],
		watcher.ResizeFailed, r.StatusMap[watcher.ResizeFailed], watcher.ResizeRollbackSucceeded, r.StatusMap[watcher.ResizeRollbackSucceeded], watcher.ResizeRollbackFailed, r.StatusMap[watcher.ResizeRollbackFailed], watcher.ResizeSkipped, r.StatusMap[watcher.ResizeSkipped])

}
