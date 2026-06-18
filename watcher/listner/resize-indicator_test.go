package listner

import (
	"sync"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
)

func TestNewResizeIndicator(t *testing.T) {
	t.Parallel()

	indicator := NewResizeIndicator(10)

	if indicator == nil {
		t.Fatal("NewResizeIndicator() returned nil")
	}

	if indicator.NumberOfRecommendations != 10 {
		t.Fatalf("NumberOfRecommendations = %d, want 10", indicator.NumberOfRecommendations)
	}

	if indicator.RecommendationProcessed != 0 {
		t.Fatalf("RecommendationProcessed = %d, want 0", indicator.RecommendationProcessed)
	}

	if indicator.StatusMap == nil {
		t.Fatal("StatusMap was not initialized")
	}

	if len(indicator.StatusMap) != 0 {
		t.Fatalf("StatusMap length = %d, want 0", len(indicator.StatusMap))
	}

	if indicator.lock == nil {
		t.Fatal("lock was not initialized")
	}
}

func TestResizeIndicator_HandleResizeEvent(t *testing.T) {
	t.Parallel()

	indicator := NewResizeIndicator(5)
	event := watcher.CreateResizeEvent([]*model.Recommendation{{}, {}}, watcher.ResizeSucceeded, "ok")

	indicator.HandleResizeEvent(event)

	if indicator.RecommendationProcessed != 2 {
		t.Fatalf("RecommendationProcessed = %d, want 2", indicator.RecommendationProcessed)
	}

	if indicator.StatusMap[watcher.ResizeSucceeded] != 2 {
		t.Fatalf("StatusMap[%s] = %d, want 2", watcher.ResizeSucceeded, indicator.StatusMap[watcher.ResizeSucceeded])
	}
}

func TestResizeIndicator_HandleResizeEvent_EmptyRecommendations(t *testing.T) {
	t.Parallel()

	indicator := NewResizeIndicator(3)
	event := watcher.CreateResizeEvent([]*model.Recommendation{}, watcher.ResizeSkipped, "no-op")

	indicator.HandleResizeEvent(event)

	if indicator.RecommendationProcessed != 0 {
		t.Fatalf("RecommendationProcessed = %d, want 0", indicator.RecommendationProcessed)
	}

	if indicator.StatusMap[watcher.ResizeSkipped] != 0 {
		t.Fatalf("StatusMap[%s] = %d, want 0", watcher.ResizeSkipped, indicator.StatusMap[watcher.ResizeSkipped])
	}
}

func TestResizeIndicator_HandleResizeEvent_Concurrent(t *testing.T) {
	t.Parallel()

	indicator := NewResizeIndicator(100)

	const goroutines = 20
	const recsPerEvent = 3

	eventRecs := make([]*model.Recommendation, recsPerEvent)
	for i := range eventRecs {
		eventRecs[i] = &model.Recommendation{}
	}

	event := watcher.CreateResizeEvent(eventRecs, watcher.ResizeSucceeded, "ok")

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			indicator.HandleResizeEvent(event)
		}()
	}
	wg.Wait()

	want := goroutines * recsPerEvent
	if indicator.RecommendationProcessed != want {
		t.Fatalf("RecommendationProcessed = %d, want %d", indicator.RecommendationProcessed, want)
	}

	if indicator.StatusMap[watcher.ResizeSucceeded] != want {
		t.Fatalf("StatusMap[%s] = %d, want %d", watcher.ResizeSucceeded, indicator.StatusMap[watcher.ResizeSucceeded], want)
	}
}

func TestResizeIndicator_HandleResizeEvent_StatusBreakdown(t *testing.T) {
	t.Parallel()

	indicator := NewResizeIndicator(10)

	indicator.HandleResizeEvent(watcher.CreateResizeEvent(
		[]*model.Recommendation{{}, {}},
		watcher.ResizeSucceeded,
		"ok",
	))
	indicator.HandleResizeEvent(watcher.CreateResizeEvent(
		[]*model.Recommendation{{}},
		watcher.ResizeFailed,
		"failed",
	))
	indicator.HandleResizeEvent(watcher.CreateResizeEvent(
		[]*model.Recommendation{{}, {}, {}},
		watcher.ResizeSkipped,
		"skipped",
	))

	if indicator.RecommendationProcessed != 6 {
		t.Fatalf("RecommendationProcessed = %d, want 6", indicator.RecommendationProcessed)
	}

	if indicator.StatusMap[watcher.ResizeSucceeded] != 2 {
		t.Fatalf("StatusMap[%s] = %d, want 2", watcher.ResizeSucceeded, indicator.StatusMap[watcher.ResizeSucceeded])
	}

	if indicator.StatusMap[watcher.ResizeFailed] != 1 {
		t.Fatalf("StatusMap[%s] = %d, want 1", watcher.ResizeFailed, indicator.StatusMap[watcher.ResizeFailed])
	}

	if indicator.StatusMap[watcher.ResizeSkipped] != 3 {
		t.Fatalf("StatusMap[%s] = %d, want 3", watcher.ResizeSkipped, indicator.StatusMap[watcher.ResizeSkipped])
	}

	if indicator.StatusMap[watcher.ResizeRollbackSucceeded] != 0 {
		t.Fatalf("StatusMap[%s] = %d, want 0", watcher.ResizeRollbackSucceeded, indicator.StatusMap[watcher.ResizeRollbackSucceeded])
	}

	if indicator.StatusMap[watcher.ResizeRollbackFailed] != 0 {
		t.Fatalf("StatusMap[%s] = %d, want 0", watcher.ResizeRollbackFailed, indicator.StatusMap[watcher.ResizeRollbackFailed])
	}
}
