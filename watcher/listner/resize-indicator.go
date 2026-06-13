package listner

import (
	"fmt"
	"sync"

	"github.com/mcpunzo/k8s-rightsizer/watcher"
)

// ResizeIndicator is a struct that implements the Listener interface and keeps track of the number of recommendations processed.
type ResizeIndicator struct {
	// NumberOfRecommendations is the total number of recommendations that have been processed.
	NumberOfRecommendations int
	// RecommendationProcessed is the number of recommendations that have been processed so far.
	RecommendationProcessed int
	lock                    *sync.RWMutex
}

// NewResizeIndicator creates a new instance of ResizeIndicator with the specified number of recommendations.
// param numberOfRecommendations The total number of recommendations to be processed.
// returns a pointer to the newly created ResizeIndicator instance.
func NewResizeIndicator(numberOfRecommendations int) *ResizeIndicator {
	return &ResizeIndicator{
		NumberOfRecommendations: numberOfRecommendations,
		RecommendationProcessed: 0,
		lock:                    &sync.RWMutex{},
	}
}

func (r *ResizeIndicator) HandleResizeEvent(event *watcher.ResizeEvent) {
	r.lock.Lock()
	defer r.lock.Unlock()

	r.RecommendationProcessed += len(event.Recommendation)
	fmt.Printf("ResizeIndicator: Processed %d/%d recommendations\n", r.RecommendationProcessed, r.NumberOfRecommendations)
}
