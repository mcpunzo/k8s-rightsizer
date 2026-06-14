package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NodeService provides methods to interact with Kubernetes Nodes.
type NodeService struct {
	client K8sClient
}

// NewNodeService creates a new instance of NodeService with the provided Kubernetes client.
// param client The Kubernetes client to use for interacting with the cluster. Can be nil, but methods will return errors if called.
// returns A new instance of NodeService.
func NewNodeService(client K8sClient) *NodeService {
	return &NodeService{
		client: client,
	}
}

// NodeStats represents the statistics of nodes in the cluster, including the total number of nodes, the count of compatible nodes based on architecture, and the count of ready nodes.
type NodeStats struct {
	NumberOfNodes        uint32
	CompatibleNodesCount uint32
	ReadyNodesCount      uint32
}

// Find retrieves the count of compatible and ready nodes in the cluster based on the specified architecture.
// param ctx The context for controlling cancellation and timeouts.
// param architecture The desired architecture (e.g., "amd64", "arm64"). If empty, any ready and schedulable architecture is considered compatible.
// returns compatibleNodesCount The number of nodes that are compatible with the specified architecture and are schedulable.
// returns readyNodesCount The total number of nodes that are in Ready state, regardless of compatibility.
// returns error An error if there was an issue retrieving the nodes or processing them.
func (s *NodeService) Find(ctx context.Context, architecture string) (*NodeStats, error) {
	nodes, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %v", err)
	}

	var compatibleNodesCount uint32
	var readyNodesCount uint32

	for _, node := range nodes.Items {
		// A. Check Basic Health (Node Ready)
		isReady := false
		for _, cond := range node.Status.Conditions {
			if cond.Type == corev1.NodeReady && cond.Status == corev1.ConditionTrue {
				isReady = true
				readyNodesCount++
				break
			}
		}

		if !isReady || node.Spec.Unschedulable {
			continue
		}

		nodeArch := node.Labels["kubernetes.io/arch"]

		if architecture != "" {
			if nodeArch == architecture {
				compatibleNodesCount++
			}
		} else {
			// If no architecture is requested, any ready/schedulable node is considered compatible.
			compatibleNodesCount++
		}
	}
	return &NodeStats{
		NumberOfNodes:        uint32(len(nodes.Items)),
		CompatibleNodesCount: compatibleNodesCount,
		ReadyNodesCount:      readyNodesCount,
	}, nil
}
