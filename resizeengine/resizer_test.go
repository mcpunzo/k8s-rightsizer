package resizeengine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/model"
	k8s "github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

type mockBaseWorkloadOps struct {
	getStatusFunc func() (*k8s.WorkloadStatus, error)
	isPausedFunc  func() (bool, error)
}

func (m *mockBaseWorkloadOps) FindWorkload(_ context.Context, _ *model.Recommendation) (*k8s.Workload, error) {
	return nil, nil
}

func (m *mockBaseWorkloadOps) ResizeWorkload(_ context.Context, _ *k8s.Workload, _ []*model.Recommendation) error {
	return nil
}

func (m *mockBaseWorkloadOps) GetStatus(_ context.Context, _ *k8s.Workload) (*k8s.WorkloadStatus, error) {
	if m.getStatusFunc == nil {
		return &k8s.WorkloadStatus{}, nil
	}
	return m.getStatusFunc()
}

func (m *mockBaseWorkloadOps) IsWorkloadInPausedState(_ context.Context, _ *k8s.Workload) (bool, error) {
	if m.isPausedFunc == nil {
		return false, nil
	}
	return m.isPausedFunc()
}

// TestCreateRollbackRecommendation tests the CreateRollbackRecommendation method
func TestCreateRollbackRecommendation(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		recs       []*model.Recommendation
		template   corev1.PodTemplateSpec
		wantCount  int
		assertions func(t *testing.T, got []*model.Recommendation)
	}{
		{
			name: "Successfully create rollback recommendations for all containers",
			recs: []*model.Recommendation{{
				Namespace: "default", WorkloadName: "deployment-1", Container: "app", Kind: model.Deployment,
			}},
			template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("500m"),
									corev1.ResourceMemory: mustParseQuantity("256Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("1"),
									corev1.ResourceMemory: mustParseQuantity("1Gi"),
								},
							},
						},
						{
							Name: "sidecar",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("100m"),
									corev1.ResourceMemory: mustParseQuantity("64Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    mustParseQuantity("250m"),
									corev1.ResourceMemory: mustParseQuantity("128Mi"),
								},
							},
						},
					},
				},
			},
			wantCount: 2,
			assertions: func(t *testing.T, got []*model.Recommendation) {
				t.Helper()
				if got[0].Namespace != "default" || got[0].WorkloadName != "deployment-1" || got[0].Kind != model.Deployment {
					t.Fatalf("unexpected metadata in first recommendation: %+v", got[0])
				}
				if got[0].Container != "app" || got[0].CpuRequestRecommendation != "500m" || got[0].MemoryRequestRecommendation != "256Mi" {
					t.Fatalf("unexpected app requests recommendation: %+v", got[0])
				}
				if got[0].CpuLimitRecommendation != "1" || got[0].MemoryLimitRecommendation != "1Gi" {
					t.Fatalf("unexpected app limits recommendation: %+v", got[0])
				}

				if got[1].Container != "sidecar" || got[1].CpuRequestRecommendation != "100m" || got[1].MemoryRequestRecommendation != "64Mi" {
					t.Fatalf("unexpected sidecar requests recommendation: %+v", got[1])
				}
				if got[1].CpuLimitRecommendation != "250m" || got[1].MemoryLimitRecommendation != "128Mi" {
					t.Fatalf("unexpected sidecar limits recommendation: %+v", got[1])
				}
			},
		},
		{
			name: "Container with zero resources",
			recs: []*model.Recommendation{{
				Namespace: "default", WorkloadName: "deployment-1", Container: "app", Kind: model.Deployment,
			}},
			template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:      "app",
							Resources: corev1.ResourceRequirements{},
						},
					},
				},
			},
			wantCount: 1,
			assertions: func(t *testing.T, got []*model.Recommendation) {
				t.Helper()
				if got[0].Container != "app" || got[0].CpuRequestRecommendation != "0" || got[0].MemoryRequestRecommendation != "0" {
					t.Fatalf("unexpected zero-requests recommendation: %+v", got[0])
				}
				if got[0].CpuLimitRecommendation != "0" || got[0].MemoryLimitRecommendation != "0" {
					t.Fatalf("unexpected zero-limits recommendation: %+v", got[0])
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			baseResizer := &BaseResizer{
				client: fakeClient,
			}

			result := baseResizer.CreateRollbackRecommendation(tt.recs, tt.template)

			if len(result) != tt.wantCount {
				t.Fatalf("CreateRollbackRecommendation() len = %d, want %d", len(result), tt.wantCount)
			}

			if tt.assertions != nil {
				tt.assertions(t, result)
			}
		})
	}
}

// TestIsPDBTooRestrictive tests the IsPDBTooRestrictive method
func TestIsPDBTooRestrictive(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		namespace     string
		labelSelector *metav1.LabelSelector
		pdbs          []runtime.Object
		wantTooRest   bool
		wantErr       bool
	}{
		{
			name:          "No PDB restrictions",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			pdbs:          []runtime.Object{},
			wantTooRest:   false,
			wantErr:       false,
		},
		{
			name:          "PDB with disruptions allowed",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			pdbs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pdb-1",
						Namespace: "default",
					},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						MaxUnavailable: mustIntOrPercent(1),
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 1,
					},
				},
			},
			wantTooRest: false,
			wantErr:     false,
		},
		{
			name:          "PDB with zero disruptions allowed",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			pdbs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pdb-restrictive",
						Namespace: "default",
					},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "test"},
						},
						MinAvailable: mustIntOrPercent(1),
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 0,
					},
				},
			},
			wantTooRest: true,
			wantErr:     false,
		},
		{
			name:          "PDB does not match labels",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			pdbs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pdb-other",
						Namespace: "default",
					},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "other"},
						},
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 0,
					},
				},
			},
			wantTooRest: false,
			wantErr:     false,
		},
		{
			name:          "Nil label selector",
			namespace:     "default",
			labelSelector: nil,
			pdbs:          []runtime.Object{},
			wantTooRest:   false,
			wantErr:       false,
		},
		{
			name:          "Empty label selector",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{},
			pdbs:          []runtime.Object{},
			wantTooRest:   false,
			wantErr:       false,
		},
		{
			name:      "MatchExpressions-only selector - no PDB",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"api"}},
				},
			},
			pdbs:        []runtime.Object{},
			wantTooRest: false,
			wantErr:     false,
		},
		{
			name:      "MatchExpressions-only selector - not blocked by old guard",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "app", Operator: metav1.LabelSelectorOpIn, Values: []string{"api"}},
				},
			},
			pdbs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-expr", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "api"},
						},
					},
					Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			// MatchExpressions-only workload selector: MatchLabels is empty so pdbSelector.Matches(empty set) = false → not blocked
			// This documents current behavior: full MatchExpressions matching against PDB is a known limitation.
			wantTooRest: false,
			wantErr:     false,
		},
		{
			name:      "MatchLabels + MatchExpressions selector - PDB matches MatchLabels",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": "api"},
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{Key: "env", Operator: metav1.LabelSelectorOpIn, Values: []string{"prod"}},
				},
			},
			pdbs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-combined", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{"app": "api"},
						},
					},
					Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			wantTooRest: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.pdbs...)
			baseResizer := &BaseResizer{
				client: fakeClient,
			}

			isTooRest, err := baseResizer.IsPDBTooRestrictive(
				context.Background(),
				tt.namespace,
				tt.labelSelector,
			)

			if (err != nil) != tt.wantErr {
				t.Errorf("IsPDBTooRestrictive() error = %v, wantErr %v", err, tt.wantErr)
			}

			if isTooRest != tt.wantTooRest {
				t.Errorf("IsPDBTooRestrictive() = %v, want %v", isTooRest, tt.wantTooRest)
			}
		})
	}
}

// Helper functions

// mustParseQuantity parses a string into a Quantity or panics
func mustParseQuantity(s string) resource.Quantity {
	q, err := resource.ParseQuantity(s)
	if err != nil {
		panic(err)
	}
	return q
}

// mustIntOrPercent converts an int to IntOrString
func mustIntOrPercent(value int) *intstr.IntOrString {
	ios := intstr.FromInt(value)
	return &ios
}

// TestCheckWorkloadStatus tests the CheckWorkloadStatus method
func TestCheckWorkloadStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		workload      *k8s.Workload
		pods          []runtime.Object
		mockGetStatus func() (*k8s.WorkloadStatus, error)
		wantReady     bool
		wantErr       bool
	}{
		{
			name: "Rollout completed successfully",
			workload: &k8s.Workload{
				Name:          "deployment-1",
				Namespace:     "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			pods: []runtime.Object{},
			mockGetStatus: func() (*k8s.WorkloadStatus, error) {
				return &k8s.WorkloadStatus{ExpectedReplicas: 2, UpdatedReplicas: 2, AvailableReplicas: 2, Generation: 3, ObservedGeneration: 3}, nil
			},
			wantReady: true,
			wantErr:   false,
		},
		{
			name: "Rollout in progress",
			workload: &k8s.Workload{
				Name:          "deployment-1",
				Namespace:     "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			pods: []runtime.Object{},
			mockGetStatus: func() (*k8s.WorkloadStatus, error) {
				return &k8s.WorkloadStatus{ExpectedReplicas: 3, UpdatedReplicas: 2, AvailableReplicas: 2, Generation: 1, ObservedGeneration: 1}, nil
			},
			wantReady: false,
			wantErr:   false,
		},
		{
			name: "Pod with critical errors",
			workload: &k8s.Workload{
				Name:          "deployment-1",
				Namespace:     "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-crash",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason: "CrashLoopBackOff",
									},
								},
							},
						},
					},
				},
			},
			wantReady: false,
			wantErr:   true,
			mockGetStatus: func() (*k8s.WorkloadStatus, error) {
				return &k8s.WorkloadStatus{ExpectedReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1, Generation: 1, ObservedGeneration: 1}, nil
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.pods...)
			baseResizer := &BaseResizer{
				client: fakeClient,
				podSvc: k8s.NewPodService(fakeClient),
			}

			mockOps := &mockBaseWorkloadOps{getStatusFunc: tt.mockGetStatus}
			statusFunc := baseResizer.CheckWorkloadStatus(context.Background(), mockOps, tt.workload)
			gotReady, err := statusFunc(context.Background())

			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckWorkloadStatus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if gotReady != tt.wantReady {
				t.Errorf("CheckWorkloadStatus() ready = %v, want %v", gotReady, tt.wantReady)
			}
		})
	}
}

// TestResizePrecheck tests the ResizePrecheck method
func TestResizePrecheck(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		workload    *k8s.Workload
		pods        []runtime.Object
		pdbs        []runtime.Object
		ops         *mockBaseWorkloadOps
		wantProceed bool
		wantErr     bool
	}{
		{
			name:        "Workload not found",
			workload:    nil,
			pods:        []runtime.Object{},
			pdbs:        []runtime.Object{},
			ops:         &mockBaseWorkloadOps{},
			wantProceed: false,
			wantErr:     true,
		},
		{
			name: "Workload paused",
			workload: &k8s.Workload{
				Name:      "deployment-1",
				Namespace: "default",
			},
			pods: []runtime.Object{},
			pdbs: []runtime.Object{},
			ops: &mockBaseWorkloadOps{isPausedFunc: func() (bool, error) {
				return true, nil
			}},
			wantProceed: false,
			wantErr:     true,
		},
		{
			name: "PDB too restrictive",
			workload: &k8s.Workload{
				Name:           "deployment-1",
				Namespace:      "default",
				UpdateStrategy: "RollingUpdate",
				LabelSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			pods: []runtime.Object{},
			pdbs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pdb-restrictive",
						Namespace: "default",
					},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
					},
					Status: policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			ops:         &mockBaseWorkloadOps{},
			wantProceed: false,
			wantErr:     true,
		},
		{
			name: "Pod in degraded state",
			workload: &k8s.Workload{
				Name:           "deployment-1",
				Namespace:      "default",
				UpdateStrategy: "RollingUpdate",
				LabelSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{Phase: corev1.PodRunning},
				},
			},
			pdbs: []runtime.Object{},
			ops: &mockBaseWorkloadOps{getStatusFunc: func() (*k8s.WorkloadStatus, error) {
				return &k8s.WorkloadStatus{ExpectedReplicas: 2, AvailableReplicas: 1, UpdatedReplicas: 2}, nil
			}},
			wantProceed: false,
			wantErr:     true,
		},
		{
			name: "All precheck conditions pass",
			workload: &k8s.Workload{
				Id:             "default-deployment-api",
				Name:           "deployment-1",
				Namespace:      "default",
				UpdateStrategy: "RollingUpdate",
				LabelSelector:  &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-ok",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{{
							Name:  "app",
							State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						}},
					},
				},
			},
			pdbs: []runtime.Object{},
			ops: &mockBaseWorkloadOps{
				isPausedFunc: func() (bool, error) { return false, nil },
				getStatusFunc: func() (*k8s.WorkloadStatus, error) {
					return &k8s.WorkloadStatus{ExpectedReplicas: 2, AvailableReplicas: 2, UpdatedReplicas: 2}, nil
				},
			},
			wantProceed: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(append(tt.pods, tt.pdbs...)...)
			baseResizer := &BaseResizer{client: fakeClient, podSvc: k8s.NewPodService(fakeClient)}

			err := baseResizer.ResizePrecheck(context.Background(), tt.ops, tt.workload)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizePrecheck() error = %v, wantErr %v", err != nil, tt.wantErr)
			}
		})
	}
}

func TestNodeCheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		workload    *k8s.Workload
		pods        []runtime.Object
		nodes       []runtime.Object
		reactor     func(client *fake.Clientset)
		wantErr     bool
		errContains string
	}{
		{
			name:        "Failure - nil workload",
			workload:    nil,
			wantErr:     true,
			errContains: "workload cannot be nil",
		},
		{
			name: "Failure - nil workload template",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  nil,
			},
			wantErr:     true,
			errContains: "workload template cannot be nil",
		},
		{
			name: "Failure - nodes lookup fails",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"}}},
			},
			reactor: func(client *fake.Clientset) {
				client.PrependReactor("list", "nodes", func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("nodes api down")
				})
			},
			wantErr:     true,
			errContains: "failed to find nodes",
		},
		{
			name: "Failure - cluster instability",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"}}},
			},
			nodes: []runtime.Object{
				mkNodeForCheck("n1", "amd64", true, false),
				mkNodeForCheck("n2", "amd64", false, false),
				mkNodeForCheck("n3", "amd64", false, false),
			},
			wantErr:     true,
			errContains: "cluster instability",
		},
		{
			name: "Failure - cluster instability with 3 nodes 1 ready (integer division edge case)",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"}}},
			},
			nodes: []runtime.Object{
				mkNodeForCheck("n1", "amd64", true, false),
				mkNodeForCheck("n2", "amd64", false, false),
				mkNodeForCheck("n3", "amd64", false, false),
			},
			wantErr:     true,
			errContains: "cluster instability",
		},
		{
			name: "Failure - no compatible architecture nodes",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"}}},
			},
			nodes: []runtime.Object{
				mkNodeForCheck("n1", "arm64", true, false),
				mkNodeForCheck("n2", "arm64", true, false),
				mkNodeForCheck("n3", "arm64", true, false),
				mkNodeForCheck("n4", "arm64", true, false),
			},
			wantErr:     true,
			errContains: "no compatible architecture nodes available",
		},
		{
			name: "Failure - no schedulable nodes when architecture is not specified",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{}}},
			},
			nodes: []runtime.Object{
				mkNodeForCheck("n1", "arm64", true, true),
				mkNodeForCheck("n2", "arm64", true, true),
			},
			wantErr:     true,
			errContains: "no ready/schedulable nodes currently available in the cluster after",
		},
		{
			name: "Success - checks pass",
			workload: &k8s.Workload{
				Namespace: "default",
				Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{"kubernetes.io/arch": "amd64"}}},
			},
			nodes: []runtime.Object{
				mkNodeForCheck("n1", "amd64", true, false),
				mkNodeForCheck("n2", "amd64", true, false),
				mkNodeForCheck("n3", "arm64", true, false),
				mkNodeForCheck("n4", "amd64", false, false),
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			objs := append([]runtime.Object{}, tt.pods...)
			objs = append(objs, tt.nodes...)
			fakeClient := fake.NewSimpleClientset(objs...)
			if tt.reactor != nil {
				tt.reactor(fakeClient)
			}

			baseResizer := &BaseResizer{
				config:  testResizerConfig(),
				client:  fakeClient,
				podSvc:  k8s.NewPodService(fakeClient),
				nodeSvc: k8s.NewNodeService(fakeClient),
			}

			err := baseResizer.NodeCheck(context.Background(), tt.workload)

			if (err != nil) != tt.wantErr {
				t.Fatalf("NodeCheck() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("NodeCheck() error = %v, want to contain %q", err, tt.errContains)
			}
		})
	}
}

func TestNodeCheck_CompatibleNodesRecheck(t *testing.T) {
	t.Parallel()

	recheckConfig := testResizerConfig()
	recheckConfig.NodeCompatibilityRecheckWindow = 40 * time.Millisecond
	recheckConfig.NodeCompatibilityRecheckPollInterval = 10 * time.Millisecond
	recheckConfig.NodeCompatibilityRecheckCooldown = 80 * time.Millisecond

	t.Run("Success - compatible nodes appear during recheck", func(t *testing.T) {
		workload := &k8s.Workload{
			Namespace: "default",
			Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{}}},
		}

		fakeClient := fake.NewSimpleClientset(
			mkNodeForCheck("n1", "arm64", true, false),
			mkNodeForCheck("n2", "arm64", true, false),
		)

		listCalls := 0
		fakeClient.PrependReactor("list", "nodes", func(action k8stesting.Action) (bool, runtime.Object, error) {
			listCalls++
			if listCalls < 2 {
				return false, nil, nil
			}

			return true, &corev1.NodeList{Items: []corev1.Node{
				*mkNodeForCheck("n1", "arm64", true, false),
				*mkNodeForCheck("n2", "arm64", true, false),
				*mkNodeForCheck("n3", "amd64", true, false),
			}}, nil
		})

		baseResizer := &BaseResizer{
			config:  recheckConfig,
			client:  fakeClient,
			podSvc:  k8s.NewPodService(fakeClient),
			nodeSvc: k8s.NewNodeService(fakeClient),
		}

		err := baseResizer.NodeCheck(context.Background(), workload)
		if err != nil {
			t.Fatalf("NodeCheck() error = %v, want nil", err)
		}
	})

	t.Run("Failure - compatible nodes do not appear during recheck", func(t *testing.T) {
		workload := &k8s.Workload{
			Namespace: "default",
			Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{}}},
		}

		fakeClient := fake.NewSimpleClientset(
			mkNodeForCheck("n1", "arm64", true, true),
			mkNodeForCheck("n2", "arm64", true, true),
		)

		baseResizer := &BaseResizer{
			config:  recheckConfig,
			client:  fakeClient,
			podSvc:  k8s.NewPodService(fakeClient),
			nodeSvc: k8s.NewNodeService(fakeClient),
		}

		err := baseResizer.NodeCheck(context.Background(), workload)
		if err == nil {
			t.Fatal("NodeCheck() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "no ready/schedulable nodes currently available in the cluster after") {
			t.Fatalf("NodeCheck() error = %v, want no-compatible-nodes timeout", err)
		}
	})

	t.Run("Failure - second check skips long recheck during cooldown", func(t *testing.T) {
		workload := &k8s.Workload{
			Namespace: "default",
			Template:  &corev1.PodTemplateSpec{Spec: corev1.PodSpec{NodeSelector: map[string]string{}}},
		}

		fakeClient := fake.NewSimpleClientset(
			mkNodeForCheck("n1", "arm64", true, true),
			mkNodeForCheck("n2", "arm64", true, true),
		)

		baseResizer := &BaseResizer{
			config:  recheckConfig,
			client:  fakeClient,
			podSvc:  k8s.NewPodService(fakeClient),
			nodeSvc: k8s.NewNodeService(fakeClient),
		}

		start := time.Now()
		err := baseResizer.NodeCheck(context.Background(), workload)
		firstElapsed := time.Since(start)
		if err == nil {
			t.Fatal("first NodeCheck() expected error, got nil")
		}

		start = time.Now()
		err = baseResizer.NodeCheck(context.Background(), workload)
		secondElapsed := time.Since(start)
		if err == nil {
			t.Fatal("second NodeCheck() expected error, got nil")
		}
		if !strings.Contains(err.Error(), "recent compatibility recheck already failed") {
			t.Fatalf("second NodeCheck() error = %v, want cooldown fast-fail", err)
		}

		if secondElapsed >= firstElapsed {
			t.Fatalf("expected second NodeCheck() to be faster, first=%s second=%s", firstElapsed, secondElapsed)
		}
	})
}

func mkNodeForCheck(name, arch string, ready bool, unschedulable bool) *corev1.Node {
	readyStatus := corev1.ConditionFalse
	if ready {
		readyStatus = corev1.ConditionTrue
	}

	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"kubernetes.io/arch":               arch,
				"node.kubernetes.io/instance-type": "c5.x86",
			},
		},
		Spec: corev1.NodeSpec{Unschedulable: unschedulable},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{
			{Type: corev1.NodeReady, Status: readyStatus},
		}},
	}
}
