package watcher

import (
	"testing"
)

// MOCKS
type MockListener struct {
	HandleResizeEventFunc func(event *ResizeEvent)
}

func (m *MockListener) HandleResizeEvent(event *ResizeEvent) {
	if m.HandleResizeEventFunc != nil {
		m.HandleResizeEventFunc(event)
	}
}

//~ MOCKS

func SetupTest(t *testing.T) *ResizeWatcher {
	w := NewResizeWatcher()
	if w == nil {
		t.Fatal("Failed to create resize watcher")
	}
	return w
}

func TestResizeWatcher(t *testing.T) {
	SetupTest(t)
}

func TestAddListener(t *testing.T) {
	w := SetupTest(t)

	table := []struct {
		listener Listener
		err      error
	}{
		{
			listener: nil,
			err:      ErrNilListener,
		},
		{
			listener: &MockListener{
				HandleResizeEventFunc: func(event *ResizeEvent) {},
			},
			err: nil,
		},
	}

	for _, tt := range table {
		err := w.AddListener(tt.listener)
		if err != tt.err {
			t.Errorf("expected error %v, got %v", tt.err, err)
		}

		if tt.err == nil {
			if len(w.listeners) != 1 {
				t.Errorf("expected 1 listener, got %d", len(w.listeners))
			}
			if w.listeners[0] != tt.listener {
				t.Errorf("expected listener %v, got %v", tt.listener, w.listeners[0])
			}
		}
	}
}

func TestNotify(t *testing.T) {
	w := SetupTest(t)
	event := &ResizeEvent{}

	notifyCount := 0
	listenerOne := &MockListener{
		HandleResizeEventFunc: func(receivedEvent *ResizeEvent) {
			notifyCount++
			if receivedEvent != event {
				t.Errorf("expected event %v, got %v", event, receivedEvent)
			}
		},
	}

	listenerTwo := &MockListener{
		HandleResizeEventFunc: func(receivedEvent *ResizeEvent) {
			notifyCount++
			if receivedEvent != event {
				t.Errorf("expected event %v, got %v", event, receivedEvent)
			}
		},
	}

	if err := w.AddListener(listenerOne); err != nil {
		t.Fatalf("unexpected error adding first listener: %v", err)
	}

	if err := w.AddListener(listenerTwo); err != nil {
		t.Fatalf("unexpected error adding second listener: %v", err)
	}

	w.Notify(event)

	if notifyCount != 2 {
		t.Fatalf("expected 2 notifications, got %d", notifyCount)
	}
}
