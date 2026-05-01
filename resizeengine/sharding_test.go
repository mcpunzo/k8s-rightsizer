package resizeengine

import (
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
)

func TestGetShardIndex(t *testing.T) {
	numberOfWorkers := 3

	// Scenario 1: Different containers of the SAME workload
	// They should end up in the same WorkerID to avoid conflicts
	rec1 := model.Recommendation{Kind: "Deployment", Namespace: "default", WorkloadName: "webapp", Container: "nginx"}
	rec2 := model.Recommendation{Kind: "Deployment", Namespace: "default", WorkloadName: "webapp", Container: "sidecar"}

	key1 := rec1.WorkloadID()
	key2 := rec2.WorkloadID()

	workerID1 := GetShardIndex(key1, numberOfWorkers)
	workerID2 := GetShardIndex(key2, numberOfWorkers)

	if workerID1 != workerID2 {
		t.Errorf("FAIL: Containers belonging to the same workload have different workerIDs: %d != %d", workerID1, workerID2)
	} else {
		t.Logf("SUCCESS: Both containers of 'webapp' assigned to Worker %d", workerID1)
	}

	// Scenario 2: Different workloads
	// They should (ideally) be distributed across different workers
	rec3 := model.Recommendation{Kind: "Deployment", Namespace: "default", WorkloadName: "database", Container: "postgres"}
	key3 := rec3.WorkloadID()
	workerID3 := int(hash(key3) % uint32(numberOfWorkers))

	t.Logf("INFO: Workload 'database' assigned to Worker %d", workerID3)
}
