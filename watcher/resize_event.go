package watcher

import "github.com/mcpunzo/k8s-rightsizer/model"

// ResizeStatus is a type that represents the status of a resize recommendation.
type ResizeStatus string

// Constants representing possible resize statuses.
const (
	// ResizeSucceeded indicates that the resize recommendation was successful.
	ResizeSucceeded ResizeStatus = "Succeeded"
	// ResizeFailed indicates that the resize recommendation failed.
	ResizeFailed ResizeStatus = "Failed"
	// ResizeRollbackSucceeded indicates that the rollback of a resize recommendation was successful.
	ResizeRollbackSucceeded ResizeStatus = "RollbackSucceeded"
	// ResizeRollbackFailed indicates that the rollback of a resize recommendation failed.
	ResizeRollbackFailed ResizeStatus = "RollbackFailed"
	// ResizeSkipped indicates that the resize recommendation was skipped.
	ResizeSkipped ResizeStatus = "Skipped"
)

// ResizeEvent is a struct that represents an event related to a resize recommendation.
type ResizeEvent struct {
	recommendation []*model.Recommendation
	status         ResizeStatus
	msg            string
}

// CreateResizeEvent creates a new ResizeEvent based on the provided recommendations, status, and message.
// param recommendations The recommendations associated with the resize event.
// param status The status of the resize event.
// param msg A message providing additional information about the resize event.
// returns a pointer to the newly created ResizeEvent instance.
func CreateResizeEvent(recommendations []*model.Recommendation, status ResizeStatus, msg string) *ResizeEvent {
	return &ResizeEvent{
		recommendation: recommendations,
		status:         status,
		msg:            msg,
	}
}
