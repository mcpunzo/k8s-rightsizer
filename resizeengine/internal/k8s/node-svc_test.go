package k8s

import (
	"context"
	"errors"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sfake "k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNewNodeService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client K8sClient
	}{
		{
			name:   "creates service with non nil client",
			client: k8sfake.NewSimpleClientset(),
		},
		{
			name:   "creates service with nil client",
			client: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			svc := NewNodeService(tt.client)
			if svc == nil {
				t.Fatal("NewNodeService() returned nil")
			}

			if svc.client != tt.client {
				t.Fatalf("NewNodeService() client mismatch: got %v want %v", svc.client, tt.client)
			}
		})
	}
}

func TestNodeService_Find(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		architecture string
		objects      []runtime.Object
		reactor      func(client *k8sfake.Clientset)
		want         *NodeStats
		wantErr      bool
		errContains  string
	}{
		{
			name:         "success explicit architecture counts only matching ready and schedulable",
			architecture: "amd64",
			objects: []runtime.Object{
				mkNode("node-a", "amd64", "m5.large", true, false),
				mkNode("node-b", "arm64", "c7g.large", true, false),
				mkNode("node-c", "amd64", "m5.large", false, false),
				mkNode("node-d", "amd64", "m5.large", true, true),
			},
			want: &NodeStats{
				NumberOfNodes:        4,
				CompatibleNodesCount: 1,
				ReadyNodesCount:      3,
			},
		},
		{
			name:         "success empty architecture counts all ready schedulable nodes",
			architecture: "",
			objects: []runtime.Object{
				mkNode("node-a", "amd64", "c5.x86", true, false),
				mkNode("node-b", "arm64", "c7g.large", true, false),
				mkNode("node-c", "amd64", "c7g.large", true, false),
				mkNode("node-d", "amd64", "c5.x86", false, false),
			},
			want: &NodeStats{
				NumberOfNodes:        4,
				CompatibleNodesCount: 3,
				ReadyNodesCount:      3,
			},
		},
		{
			name:         "success no nodes",
			architecture: "amd64",
			objects:      []runtime.Object{},
			want: &NodeStats{
				NumberOfNodes:        0,
				CompatibleNodesCount: 0,
				ReadyNodesCount:      0,
			},
		},
		{
			name:         "failure list nodes returns error",
			architecture: "amd64",
			reactor: func(client *k8sfake.Clientset) {
				client.PrependReactor("list", "nodes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("nodes list failed")
				})
			},
			wantErr:     true,
			errContains: "failed to list nodes: nodes list failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := k8sfake.NewSimpleClientset(tt.objects...)
			if tt.reactor != nil {
				tt.reactor(client)
			}

			svc := NewNodeService(client)
			got, err := svc.Find(context.Background(), tt.architecture)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Find() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("Find() error = %v, want to contain %q", err, tt.errContains)
			}

			if tt.wantErr {
				return
			}

			if got == nil {
				t.Fatal("Find() returned nil stats")
			}

			if *got != *tt.want {
				t.Fatalf("Find() got = %+v, want %+v", *got, *tt.want)
			}
		})
	}
}

func mkNode(name, arch, instanceType string, ready bool, unschedulable bool) *corev1.Node {
	status := corev1.ConditionFalse
	if ready {
		status = corev1.ConditionTrue
	}

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"kubernetes.io/arch":               arch,
				"node.kubernetes.io/instance-type": instanceType,
			},
		},
		Spec: corev1.NodeSpec{Unschedulable: unschedulable},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{Type: corev1.NodeReady, Status: status},
			},
		},
	}
}
