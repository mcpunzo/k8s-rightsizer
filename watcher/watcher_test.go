package watcher

import (
	"testing"
)

func TestResizeWatcher(t *testing.T) {
	w := NewResizeWatcher()
	if w == nil {
		t.Fatal("Failed to create resize watcher")
	}
}


