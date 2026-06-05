package watcher

import "github.com/mcpunzo/k8s-rightsizer/model"

// ResizeWatcher is a struct that represents an event watcher for resize events.
type ResizeWatcher struct {
}

type ResizeEvent struct {
	recommendation *model.Recommendation
}

// NewResizeWatcher creates a new instance of ResizeWatcher.
// returns a pointer to the newly created ResizeWatcher instance.
func NewResizeWatcher() *ResizeWatcher {
	return &ResizeWatcher{}
}
