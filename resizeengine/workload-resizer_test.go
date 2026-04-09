package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// --- MOCKS ---

// Manteniamo solo questo perché astrae la logica di alto livello (Find/Resize/Status)
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

// Helper per i test di errore
func contains(str, substr string) bool {
	return len(str) >= len(substr) && str[:len(substr)] == substr
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
						Replicas:           1,
						UpdatedReplicas:    1,
						AvailableReplicas:  1,
						Generation:         1,
						ObservedGeneration: 1,
						Namespace:          "default",
						LabelSelector:      &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
					}, nil
				},
			},
			wantErr: false,
		},
		{
			name: "Rollback - Polling Failure (Crash Detected)",
			rec:  &model.Recommendation{WorkloadName: "fail", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*Workload, error) {
					return &Workload{Name: "fail", Namespace: "default", Template: baseTemplate.DeepCopy()}, nil
				},
				resizeFunc: func() error { return nil },
				statusFunc: func() (*WorkloadStatus, error) {
					// Ritorniamo un errore per simulare il fallimento del polling che innesca il rollback
					return nil, errors.New("crash detected")
				},
			},
			wantErr:     true,
			errContains: "update canceled and rollback completed successfully",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Usiamo il fakeClient ufficiale
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

func TestWorkloadResizer_CheckPodCriticalErrors(t *testing.T) {
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

			isError, reason := resizer.CheckPodCriticalErrors(
				context.Background(),
				"default",
				&metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
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
				Replicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1,
				Generation: 1, ObservedGeneration: 1, Namespace: "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
			},
			mockPods:  []runtime.Object{},
			wantReady: true,
		},
		{
			name: "Rollout failed - Pod Error",
			mockStatus: &WorkloadStatus{
				Replicas: 1, Namespace: "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "test"}},
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

			pollFunc := resizer.CheckWorkloadStatus(context.Background(), mOps, "default", "test")
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