package resizeengine

import (
	"context"
	"strings"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	k8s "github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
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

// TestCheckPodCriticalErrors tests the CheckPodCriticalErrors method with various pod states
func TestCheckPodCriticalErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		pods        []runtime.Object
		namespace   string
		labels      map[string]string
		wantIsError bool
		wantReason  string
	}{
		{
			name:      "All pods running successfully",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-1",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								State: corev1.ContainerState{
									Running: &corev1.ContainerStateRunning{},
								},
							},
						},
					},
				},
			},
			wantIsError: false,
			wantReason:  "",
		},
		{
			name:      "Container in CrashLoopBackOff",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
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
								Name: "container-1",
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
			wantIsError: true,
			wantReason:  "Container in error: CrashLoopBackOff",
		},
		{
			name:      "Pod pending with unschedulable",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-pending",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Reason:  "Unschedulable",
								Message: "Insufficient memory",
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Insufficient resources in the cluster",
		},
		{
			name:      "Container OOMKilled",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-oom",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason: "OOMKilled",
									},
								},
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "OOMKilled: Insufficient memory for startup",
		},
		{
			name:      "ImagePullBackOff",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-image-fail",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{
										Reason: "ImagePullBackOff",
									},
								},
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: ImagePullBackOff",
		},
		{
			name:      "Last termination state OOMKilled",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-restarted-oom",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								LastTerminationState: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"},
								},
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "OOMKilled detected in the last restart: Insufficient memory for startup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.pods...)

			baseResizer := &BaseResizer{
				client: fakeClient,
			}

			workload := &k8s.Workload{
				Namespace:     tt.namespace,
				LabelSelector: &metav1.LabelSelector{MatchLabels: tt.labels},
			}

			isError, reason := baseResizer.CheckPodCriticalErrors(
				context.Background(),
				workload,
			)

			if isError != tt.wantIsError {
				t.Errorf("CheckPodCriticalErrors() gotIsError = %v, want %v", isError, tt.wantIsError)
			}

			if tt.wantIsError && reason == "" {
				t.Errorf("CheckPodCriticalErrors() expected reason, got empty string")
			}

			if tt.wantIsError && tt.wantReason != "" && !strings.Contains(reason, tt.wantReason) {
				t.Errorf("CheckPodCriticalErrors() reason = %q, want to contain %q", reason, tt.wantReason)
			}

			if !tt.wantIsError && reason != "" {
				t.Errorf("CheckPodCriticalErrors() expected no reason, got %q", reason)
			}
		})
	}
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
			baseResizer := &BaseResizer{client: fakeClient}

			err := baseResizer.ResizePrecheck(context.Background(), tt.ops, tt.workload)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizePrecheck() error = %v, wantErr %v", err != nil, tt.wantErr)
			}
		})
	}
}
