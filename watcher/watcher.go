package watcher

import (
	"errors"
	"fmt"
	"sync"
)

// Listener is an interface that defines a method to handle resize events.
type Listener interface {
	// HandleResizeEvent is a method that takes a ResizeEvent as an argument and processes it.
	HandleResizeEvent(event *ResizeEvent)
}

// ResizeWatcher is a struct that represents an event watcher for resize events.
type ResizeWatcher struct {
	listeners []Listener
	lock      *sync.RWMutex
}

var (
	// ErrNilListener is an error that indicates that a nil listener was provided.
	ErrNilListener = errors.New("Listener cannot be nil")
)

// NewResizeWatcher creates a new instance of ResizeWatcher.
// returns a pointer to the newly created ResizeWatcher instance.
func NewResizeWatcher() *ResizeWatcher {
	return &ResizeWatcher{
		listeners: make([]Listener, 0),
		lock:      &sync.RWMutex{},
	}
}

// AddListener adds a new listener to the ResizeWatcher.
// param listener The listener to be added to the ResizeWatcher.
func (w *ResizeWatcher) AddListener(listener Listener) error {
	if listener == nil {
		return ErrNilListener
	}
	w.lock.Lock()
	defer w.lock.Unlock()
	w.listeners = append(w.listeners, listener)
	return nil
}

// Notify notifies all registered listeners of a resize event.
// param event The resize event to be sent to the listeners.
func (w *ResizeWatcher) Notify(event *ResizeEvent) {
	w.lock.RLock()
	listeners := append([]Listener(nil), w.listeners...)
	w.lock.RUnlock()

	for _, listener := range listeners {
		func(listener Listener) {
			defer func() {
				if r := recover(); r != nil {
					fmt.Printf("[Crash Alert] panic caught for listener %T: %v\n", listener, r)
				}
			}()

			listener.HandleResizeEvent(event)
		}(listener)
	}
}
