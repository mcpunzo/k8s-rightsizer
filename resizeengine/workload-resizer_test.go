package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// --- MOCKS ---

// Manteniamo solo questo perché astrae la logica di alto livello (Find/Resize/Status)
type mockWorkloadOps struct {
	findFunc     func() (*Workload, error)
	resizeFunc   func() error
	statusFunc   func() (*WorkloadStatus, error)
	isPausedFunc func() (bool, error)
}

func (m *mockWorkloadOps) FindWorkload(ctx context.Context, rec *model.Recommendation) (*Workload, error) {
	return m.findFunc()
}
func (m *mockWorkloadOps) IsWorkloadInPausedState(ctx context.Context, w *Workload) (bool, error) {
	return m.isPausedFunc()
}
func (m *mockWorkloadOps) ResizeWorkload(ctx context.Context, w *Workload, rec *model.Recommendation) error {
	return m.resizeFunc()
}
func (m *mockWorkloadOps) GetStatus(ctx context.Context, w *Workload) (*WorkloadStatus, error) {
	return m.statusFunc()
}

// Helper per i test di errore
func contains(str, substr string) bool {
	return len(str) >= len(substr) && str[:len(substr)] == substr
}

func GetBasePodTemplate(cpu, mem string) *corev1.PodTemplateSpec {
	return &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse(cpu),
							corev1.ResourceMemory: resource.MustParse(mem),
						},
					},
				},
			},
		},
	}
}

// --- TESTS ---

func TestWorkloadResizer_ResizeWorkload(t *testing.T) {
	t.Parallel()
	baseTemplate := GetBasePodTemplate("100m", "128Mi")

	tests := []struct {
		name        string
		rec         *model.Recommendation
		ops         *mockWorkloadOps
		wantErr     bool
		errContains string
	}{

		{
			name: "Success - Full Flow",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "test", Namespace: "default", Template: baseTemplate.DeepCopy()}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					return &WorkloadStatus{
						ExpectedReplicas:   1,
						UpdatedReplicas:    1,
						AvailableReplicas:  1,
						Generation:         1,
						ObservedGeneration: 1,
					}, nil
				},
				isPausedFunc: func() (bool, error) {
					return false, nil
				},
			},
			wantErr: false,
		},
		{
			name: "Rollback - Polling Failure (Crash Detected)",
			rec:  &model.Recommendation{WorkloadName: "fail", Container: "app"},
			ops: func() *mockWorkloadOps {
				callCount := 0
				return &mockWorkloadOps{
					findFunc: func() (*Workload, error) {
						return &Workload{Name: "fail", Namespace: "default", Template: baseTemplate.DeepCopy(), UpdateStrategy: "RollingUpdate"}, nil
					},
					resizeFunc: func() error { return nil },
					statusFunc: func() (*WorkloadStatus, error) {
						callCount++
						if callCount == 1 { // Simulate first successful status check i.e. for the precheck phase
							// First call returns normal status
							return &WorkloadStatus{
								ExpectedReplicas:   1,
								UpdatedReplicas:    1,
								AvailableReplicas:  1,
								Generation:         1,
								ObservedGeneration: 1,
							}, nil
						}
						// Second call simulates a crash detected during polling, which should trigger rollback
						return nil, errors.New("crash detected")
					},
					isPausedFunc: func() (bool, error) {
						return false, nil
					},
				}
			}(),
			wantErr:     true,
			errContains: "update canceled and rollback completed successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize the fake client with the PDBs defined in the test case
			fakeClient := fake.NewSimpleClientset()
			resizer := NewWorkloadResizer(fakeClient)

			err := resizer.ResizeWorkload(context.Background(), tt.rec, tt.ops)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestWorkloadResizer_ResizeOnRecreateStrategy(t *testing.T) {
	t.Parallel()
	baseTemplate := GetBasePodTemplate("100m", "128Mi")

	tests := []struct {
		name             string
		rec              *model.Recommendation
		ops              *mockWorkloadOps
		wantErr          bool
		errContains      string
		resizeOnRecreate bool
	}{
		{
			name: "Resize skipped due to Recreate strategy",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "test", Namespace: "default", Template: baseTemplate.DeepCopy(), UpdateStrategy: "Recreate"}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					return &WorkloadStatus{
						ExpectedReplicas:   1,
						UpdatedReplicas:    1,
						AvailableReplicas:  1,
						Generation:         1,
						ObservedGeneration: 1,
					}, nil
				},
				isPausedFunc: func() (bool, error) {
					return false, nil
				},
			},
			wantErr:     true,
			errContains: "skipping resize due to UpdateStrategy set on Recreate",
		},
		{
			name: "Resize done as per configuration due to Recreate strategy",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "test", Namespace: "default", Template: baseTemplate.DeepCopy(), UpdateStrategy: "Recreate"}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					return &WorkloadStatus{
						ExpectedReplicas:   1,
						UpdatedReplicas:    1,
						AvailableReplicas:  1,
						Generation:         1,
						ObservedGeneration: 1,
					}, nil
				},
				isPausedFunc: func() (bool, error) {
					return false, nil
				},
			},
			wantErr:          false,
			errContains:      "",
			resizeOnRecreate: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset()
			resizer := NewWorkloadResizer(fakeClient)

			ctx := context.Background()
			ctx = context.WithValue(ctx, "resizeOnRecreate", tt.resizeOnRecreate)
			err := resizer.ResizeWorkload(ctx, tt.rec, tt.ops)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestWorkloadResizer_ResizeWorkload_With_PDB(t *testing.T) {
	t.Parallel()
	baseTemplate := GetBasePodTemplate("100m", "128Mi")

	appLabels := map[string]string{"app": "my-service"}

	tests := []struct {
		name          string
		rec           *model.Recommendation
		ops           *mockWorkloadOps
		labelSelector *metav1.LabelSelector
		initialObjs   []runtime.Object
		wantErr       bool
		errContains   string
	}{
		{
			name: "Success - Full Flow with PDB allowing disruption",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "test", Namespace: "default", Template: baseTemplate.DeepCopy(), LabelSelector: &metav1.LabelSelector{MatchLabels: appLabels}}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					return &WorkloadStatus{
						ExpectedReplicas:   1,
						UpdatedReplicas:    1,
						AvailableReplicas:  1,
						Generation:         1,
						ObservedGeneration: 1,
					}, nil
				},
				isPausedFunc: func() (bool, error) {
					return false, nil
				},
			},
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "app-pdb", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: appLabels},
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 1, // Allowed!
					},
				},
			}, // PDB allowing disruption in the cluster
			wantErr: false,
		},
		{
			name: "Success - Full Flow with PDB NOT allowing disruption",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "test", Namespace: "default", Template: baseTemplate.DeepCopy(), LabelSelector: &metav1.LabelSelector{MatchLabels: appLabels}}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					return &WorkloadStatus{
						ExpectedReplicas:   1,
						UpdatedReplicas:    1,
						AvailableReplicas:  1,
						Generation:         1,
						ObservedGeneration: 1,
					}, nil
				},
				isPausedFunc: func() (bool, error) {
					return false, nil
				},
			},
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "app-pdb", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: appLabels},
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 0, // Not allowed!
					},
				},
			}, // PDB not allowing disruption in the cluster
			wantErr:     true,
			errContains: "skipping resize due to PDB restrictions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize the fake client with the PDBs defined in the test case
			fakeClient := fake.NewSimpleClientset(tt.initialObjs...)
			resizer := NewWorkloadResizer(fakeClient)

			err := resizer.ResizeWorkload(context.Background(), tt.rec, tt.ops)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestWorkloadResizer_CheckPodCriticalErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		pods        []runtime.Object // Fake client accetta runtime.Object
		wantIsError bool
		wantReason  string
	}{
		{
			name: "Success - All Pods Running",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default", Labels: map[string]string{"app": "test"}},
					Status: corev1.PodStatus{
						Phase: corev1.PodRunning,
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
						},
					},
				},
			},
			wantIsError: false,
		},
		{
			name: "Failure - Container CrashLoopBackOff",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-fail", Namespace: "default", Labels: map[string]string{"app": "test"}},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
								},
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: CrashLoopBackOff",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inizializziamo il fake client con i pod del test case
			fakeClient := fake.NewSimpleClientset(tt.pods...)
			resizer := NewWorkloadResizer(fakeClient)

			workload := &Workload{
				Namespace:     "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			}
			isError, reason := resizer.CheckPodCriticalErrors(
				context.Background(),
				workload,
			)

			if isError != tt.wantIsError {
				t.Errorf("CheckPodCriticalErrors() gotIsError = %v, want %v", isError, tt.wantIsError)
			}
			if tt.wantReason != "" && reason != tt.wantReason {
				t.Errorf("CheckPodCriticalErrors() gotReason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestWorkloadResizer_CheckWorkloadStatus(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		mockStatus *WorkloadStatus
		mockPods   []runtime.Object
		statusErr  error
		wantReady  bool
		wantErr    bool
	}{
		{
			name: "Rollout successful",
			mockStatus: &WorkloadStatus{
				ExpectedReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1,
				Generation: 1, ObservedGeneration: 1,
			},
			mockPods:  []runtime.Object{},
			wantReady: true,
		},
		{
			name: "Rollout failed - Pod Error",
			mockStatus: &WorkloadStatus{
				ExpectedReplicas: 1,
			},
			mockPods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default", Labels: map[string]string{"app": "test"}},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}},
						},
					},
				},
			},
			wantReady: false,
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.mockPods...)
			resizer := NewWorkloadResizer(fakeClient)

			mOps := &mockWorkloadOps{
				statusFunc: func() (*WorkloadStatus, error) {
					return tt.mockStatus, tt.statusErr
				},
			}

			workload := &Workload{
				WorkloadType: StatefulSet,
				Namespace:    "default",
				Name:         "test",
			}

			pollFunc := resizer.CheckWorkloadStatus(context.Background(), mOps, workload)
			ready, err := pollFunc(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("pollFunc() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ready != tt.wantReady {
				t.Errorf("pollFunc() ready = %v, want %v", ready, tt.wantReady)
			}
		})
	}
}

func TestWorkloadResizer_IsPDBTooRestrictive(t *testing.T) {
	t.Parallel()
	appLabels := map[string]string{"app": "my-service"}
	otherLabels := map[string]string{"app": "other-service"}

	tests := []struct {
		name          string
		namespace     string
		labelSelector *metav1.LabelSelector
		initialObjs   []runtime.Object
		wantResult    bool
		wantErr       bool
	}{
		{
			name:      "No PDB present",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: appLabels,
			},
			initialObjs: []runtime.Object{}, // No PDB in the cluster
			wantResult:  false,
			wantErr:     false,
		},
		{
			name:      "PDB allowing disruption",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: appLabels,
			},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "app-pdb", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: appLabels},
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 1, // Allowed!
					},
				},
			},
			wantResult: false,
			wantErr:    false,
		},
		{
			name:      "PDB too restrictive (DisruptionsAllowed = 0)",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: appLabels,
			},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "app-pdb", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: appLabels},
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 0, // Blocked!
					},
				},
			},
			wantResult: true,
			wantErr:    false,
		},
		{
			name:      "PDB present but for a different app",
			namespace: "default",
			labelSelector: &metav1.LabelSelector{
				MatchLabels: appLabels,
			},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "other-pdb", Namespace: "default"},
					Spec: policyv1.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: otherLabels},
					},
					Status: policyv1.PodDisruptionBudgetStatus{
						DisruptionsAllowed: 0,
					},
				},
			},
			wantResult: false, // Should not block our app
			wantErr:    false,
		},
		{
			name:          "Nil label selector",
			namespace:     "default",
			labelSelector: nil,
			wantResult:    false,
			wantErr:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize the fake client with the PDBs defined in the test case
			fakeClient := fake.NewSimpleClientset(tt.initialObjs...)

			r := &WorkloadResizer{
				client: fakeClient,
			}

			got, err := r.IsPDBTooRestrictive(context.Background(), tt.namespace, tt.labelSelector)

			if (err != nil) != tt.wantErr {
				t.Errorf("IsPDBTooRestrictive() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.wantResult {
				t.Errorf("IsPDBTooRestrictive() got = %v, want %v", got, tt.wantResult)
			}
		})
	}
}
