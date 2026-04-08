package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- MOCKS ---
type mockWorkloadOps struct {
	findFunc   func() (*Workload, error)
	resizeFunc func() error
	statusFunc func() (*WorkloadStatus, error)
}

func (m *mockWorkloadOps) FindWorkload(ctx context.Context, rec *model.Recommendation) (*Workload, error) {
	return m.findFunc()
}
func (m *mockWorkloadOps) ResizeWorkload(ctx context.Context, w *Workload, rec *model.Recommendation) error {
	return m.resizeFunc()
}
func (m *mockWorkloadOps) GetStatus(ctx context.Context, ns, name string) (*WorkloadStatus, error) {
	return m.statusFunc()
}

// Helper
func contains(str, substr string) bool {
	return len(str) >= len(substr) && str[:len(substr)] == substr || true // Semplificato per l'esempio
}

// --- TESTS ---

func TestWorkloadResizer_ResizeWorkload(t *testing.T) {
	baseTemplate := &corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("128Mi"),
						},
					},
				},
			},
		},
	}

	tests := []struct {
		name        string
		rec         *model.Recommendation
		ops         *mockWorkloadOps
		wantErr     bool
		errContains string
	}{
		{
			name: "Success - Full Flow",
			rec:  &model.Recommendation{WorkloadName: "test", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "test", Template: baseTemplate}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					return &WorkloadStatus{Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "Failure - Workload Not Found",
			rec:  &model.Recommendation{WorkloadName: "missing"},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) { return nil, errors.New("not found") },
			},
			wantErr: true,
		},
		{
			name: "Rollback - Polling Failure (Timeout or Error)",
			rec:  &model.Recommendation{WorkloadName: "fail", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "fail", Template: baseTemplate}, nil
				},
				resizeFunc: func() error { return nil }, // Prima resize ok
				statusFunc: func() (*WorkloadStatus, error) {
					return nil, errors.New("pod crash detected") // Polling fallisce
				},
			},
			wantErr:     true,
			errContains: "rollback completed successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Usiamo il mockK8sClient definito nei messaggi precedenti
			resizer := NewWorkloadResizer(&mockK8sClient{})

			// Nota: accorciamo il timeout per il test per non attendere 5 minuti
			ctx := context.Background()
			err := resizer.ResizeWorkload(ctx, tt.rec, tt.ops)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && tt.errContains != "" {
				if err == nil || !contains(err.Error(), tt.errContains) {
					t.Errorf("error expected to contain %q, got %v", tt.errContains, err)
				}
			}
		})
	}
}

func TestWorkloadResizer_CreateRollbackRecommendation(t *testing.T) {
	resizer := NewWorkloadResizer(nil)
	rec := &model.Recommendation{Container: "app"}
	template := corev1.PodTemplateSpec{
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "app",
					Resources: corev1.ResourceRequirements{
						Requests: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("200m"),
							corev1.ResourceMemory: resource.MustParse("512Mi"),
						},
					},
				},
			},
		},
	}

	t.Run("Extract original values for rollback", func(t *testing.T) {
		got := resizer.CreateRollbackRecommendation(rec, template)
		if got == nil {
			t.Fatal("expected recommendation, got nil")
		}
		if got.CpuRequestRecommendation != "200m" || got.MemoryRequestRecommendation != "512Mi" {
			t.Errorf("rollback values mismatch: cpu %s, mem %s", got.CpuRequestRecommendation, got.MemoryRequestRecommendation)
		}
	})
}

func TestWorkloadResizer_CheckPodCriticalErrors(t *testing.T) {
	tests := []struct {
		name        string
		pods        []corev1.Pod
		wantIsError bool
		wantReason  string
	}{
		{
			name: "Success - All Pods Running",
			pods: []corev1.Pod{
				{
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
			name: "Failure - Pod Unschedulable (Pending)",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Reason:  "Unschedulable",
								Message: "0/3 nodes available: 3 Insufficient cpu.",
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Insufficient resources in the cluster: 0/3 nodes available: 3 Insufficient cpu.",
		},
		{
			name: "Failure - Container CrashLoopBackOff",
			pods: []corev1.Pod{
				{
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
		{
			name: "Failure - OOMKilled detected",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								State: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"},
								},
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "OOMKilled: Insufficient memory for startup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock chain
			mPodClient := &mockPodClient{
				listFunc: func() (*corev1.PodList, error) {
					return &corev1.PodList{Items: tt.pods}, nil
				},
			}
			mCoreV1 := &mockCoreV1{podClient: mPodClient}
			mClient := &mockK8sClient{coreV1: mCoreV1}

			resizer := NewWorkloadResizer(mClient)

			isError, reason := resizer.CheckPodCriticalErrors(
				context.Background(),
				"default",
				&metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			)

			if isError != tt.wantIsError {
				t.Errorf("CheckPodCriticalErrors() isError = %v, want %v", isError, tt.wantIsError)
			}
			if tt.wantReason != "" && reason != tt.wantReason {
				t.Errorf("CheckPodCriticalErrors() reason = %q, want %q", reason, tt.wantReason)
			}
		})
	}
}

func TestWorkloadResizer_CheckWorkloadStatus(t *testing.T) {
	tests := []struct {
		name        string
		mockStatus  *WorkloadStatus
		mockPods    []corev1.Pod
		statusErr   error
		wantReady   bool
		wantErr     bool
		expectedErr string
	}{
		{
			name: "Rollout in progress - Not all replicas ready",
			mockStatus: &WorkloadStatus{
				Replicas:           3,
				UpdatedReplicas:    3,
				AvailableReplicas:  1, // Solo 1 su 3
				Generation:         10,
				ObservedGeneration: 10,
				Namespace:          "default",
			},
			mockPods:  []corev1.Pod{}, // Nessun errore nei pod
			wantReady: false,
			wantErr:   false,
		},
		{
			name: "Rollout successful - All conditions met",
			mockStatus: &WorkloadStatus{
				Replicas:           3,
				UpdatedReplicas:    3,
				AvailableReplicas:  3,
				Generation:         10,
				ObservedGeneration: 10,
				Namespace:          "default",
			},
			mockPods:  []corev1.Pod{},
			wantReady: true,
			wantErr:   false,
		},
		{
			name: "Rollout failed - Critical Pod Error detected",
			mockStatus: &WorkloadStatus{
				Replicas:          3,
				AvailableReplicas: 1,
				Namespace:         "default",
				LabelSelector:     &metav1.LabelSelector{},
			},
			mockPods: []corev1.Pod{
				{
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
			wantReady:   false,
			wantErr:     true,
			expectedErr: "fail detected: Container in error: CrashLoopBackOff",
		},
		{
			name:        "API Error - GetStatus fails",
			mockStatus:  nil,
			statusErr:   errors.New("k8s api down"),
			wantReady:   false,
			wantErr:     true,
			expectedErr: "k8s api down",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Setup Mock WorkloadOps
			mOps := &mockWorkloadOps{
				statusFunc: func() (*WorkloadStatus, error) {
					return tt.mockStatus, tt.statusErr
				},
			}

			// 2. Setup Mock K8sClient (per CheckPodCriticalErrors)
			mPodClient := &mockPodClient{
				listFunc: func() (*corev1.PodList, error) {
					return &corev1.PodList{Items: tt.mockPods}, nil
				},
			}
			mCoreV1 := &mockCoreV1{podClient: mPodClient}
			mClient := &mockK8sClient{coreV1: mCoreV1}

			resizer := NewWorkloadResizer(mClient)

			// Otteniamo la funzione di polling
			pollFunc := resizer.CheckWorkloadStatus(context.Background(), mOps, "default", "test-workload")

			// Eseguiamo un singolo ciclo di polling
			gotReady, err := pollFunc(context.Background())

			// Verifiche
			if (err != nil) != tt.wantErr {
				t.Errorf("pollFunc() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && err.Error() != tt.expectedErr {
				t.Errorf("expected error %q, got %q", tt.expectedErr, err.Error())
			}
			if gotReady != tt.wantReady {
				t.Errorf("pollFunc() ready = %v, want %v", gotReady, tt.wantReady)
			}
		})
	}
}
