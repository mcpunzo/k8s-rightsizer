package watcher

import (
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
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

func TestCreateResizeEvent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		status ResizeStatus
		msg    string
	}{
		{name: "succeeded event", status: ResizeSucceeded, msg: "[OK] resized"},
		{name: "failed event", status: ResizeFailed, msg: "[KO] failed"},
		{name: "skipped event", status: ResizeSkipped, msg: "[SKIP] skipped"},
		{name: "rollback succeeded", status: ResizeRollbackSucceeded, msg: "rollback ok"},
		{name: "rollback failed", status: ResizeRollbackFailed, msg: "rollback failed"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recs := []*model.Recommendation{
				{Namespace: "default", WorkloadName: "api", Container: "app"},
			}
			event := CreateResizeEvent(recs, tt.status, tt.msg)

			if event == nil {
				t.Fatal("CreateResizeEvent returned nil")
			}
			if event.Status != tt.status {
				t.Errorf("Status = %v, want %v", event.Status, tt.status)
			}
			if event.Msg != tt.msg {
				t.Errorf("Msg = %q, want %q", event.Msg, tt.msg)
			}
			if len(event.Recommendation) != 1 {
				t.Errorf("Recommendation count = %d, want 1", len(event.Recommendation))
			}
		})
	}
}
