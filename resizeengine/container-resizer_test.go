package resizeengine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/model"
	"github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

// --- MOCKS ---

type mockWorkloadOps struct {
	findFunc     func() (*k8s.Workload, error)
	resizeFunc   func() error
	statusFunc   func() (*k8s.WorkloadStatus, error)
	isPausedFunc func() (bool, error)
}

func (m *mockWorkloadOps) FindWorkload(_ context.Context, _ *model.Recommendation) (*k8s.Workload, error) {
	return m.findFunc()
}
func (m *mockWorkloadOps) IsWorkloadInPausedState(_ context.Context, _ *k8s.Workload) (bool, error) {
	return m.isPausedFunc()
}
func (m *mockWorkloadOps) ResizeWorkload(_ context.Context, _ *k8s.Workload, _ []*model.Recommendation) error {
	return m.resizeFunc()
}
func (m *mockWorkloadOps) GetStatus(_ context.Context, _ *k8s.Workload) (*k8s.WorkloadStatus, error) {
	return m.statusFunc()
}

// basePodTemplate creates a simple PodTemplateSpec for testing
func basePodTemplate(cpu, mem string) *corev1.PodTemplateSpec {
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

// stableStatus returns a WorkloadStatus where the rollout is complete
func stableStatus() *k8s.WorkloadStatus {
	return &k8s.WorkloadStatus{
		ExpectedReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 1,
		Generation: 1, ObservedGeneration: 1,
	}
}

func newTestContainerResizer(objs ...runtime.Object) *ContainerResizer {
	return NewContainerResizer(fake.NewSimpleClientset(objs...), watcher.NewResizeWatcher())
}

// --- TESTS ---

// TestContainerResizer_ResizeWorkload tests the ResizeWorkload method with various scenarios
func TestContainerResizer_ResizeWorkload(t *testing.T) {
	t.Parallel()
	originalSoak := PostRolloutSoakDuration
	PostRolloutSoakDuration = 25 * time.Millisecond
	t.Cleanup(func() {
		PostRolloutSoakDuration = originalSoak
	})

	tmpl := basePodTemplate("100m", "128Mi")

	tests := []struct {
		name        string
		rec         *model.Recommendation
		ops         *mockWorkloadOps
		wantErr     bool
		errContains string
	}{
		{
			name: "Success - resize applied and rollout completes",
			rec: &model.Recommendation{
				Namespace: "default", WorkloadName: "api", Container: "app",
				CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy()}, nil
				},
				resizeFunc:   func() error { return nil },
				statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			wantErr: false,
		},
		{
			name: "Failure - empty recommendations",
			rec:  nil,
			ops: &mockWorkloadOps{
				resizeFunc:   func() error { return nil },
				statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			wantErr:     true,
			errContains: "no recommendations provided",
		},
		{
			name: "Failure - nil template in workload",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: nil}, nil
				},
				resizeFunc:   func() error { return nil },
				statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			wantErr:     true,
			errContains: "workload template is nil",
		},
		{
			name: "Failure - resize API call fails",
			rec: &model.Recommendation{
				Namespace: "default", WorkloadName: "api", Container: "app",
				CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi",
			},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy()}, nil
				},
				resizeFunc:   func() error { return errors.New("conflict") },
				statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			wantErr:     true,
			errContains: "failed to update workload",
		},
		{
			name: "Rollback - soak detects post-rollout crash and rollback succeeds",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: func() *mockWorkloadOps {
				return &mockWorkloadOps{
					findFunc: func() (*k8s.Workload, error) {
						return &k8s.Workload{
							Id:            "default-deployment-api",
							Name:          "api",
							Namespace:     "default",
							Template:      tmpl.DeepCopy(),
							LabelSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
						}, nil
					},
					resizeFunc:   func() error { return nil },
					statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
					isPausedFunc: func() (bool, error) { return false, nil },
				}
			}(),
			wantErr:     true,
			errContains: "update canceled and rollback completed successfully",
		},
		{
			name: "Rollback - crash detected during polling, rollback succeeds",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: func() *mockWorkloadOps {
				statusCall := 0
				return &mockWorkloadOps{
					findFunc: func() (*k8s.Workload, error) {
						return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy()}, nil
					},
					resizeFunc: func() error { return nil },
					statusFunc: func() (*k8s.WorkloadStatus, error) {
						statusCall++
						if statusCall == 1 {
							return nil, errors.New("crash detected")
						}
						return stableStatus(), nil
					},
					isPausedFunc: func() (bool, error) { return false, nil },
				}
			}(),
			wantErr:     true,
			errContains: "update canceled and rollback completed successfully",
		},
		{
			name: "Rollback - rollback applies but workload remains unstable",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: func() *mockWorkloadOps {
				return &mockWorkloadOps{
					findFunc: func() (*k8s.Workload, error) {
						return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy()}, nil
					},
					resizeFunc:   func() error { return nil },
					statusFunc:   func() (*k8s.WorkloadStatus, error) { return nil, errors.New("crash detected") },
					isPausedFunc: func() (bool, error) { return false, nil },
				}
			}(),
			wantErr:     true,
			errContains: "rollback completed but workload is not stable",
		},
		{
			name: "Rollback - crash detected during polling, rollback also fails",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: func() *mockWorkloadOps {
				call := 0
				return &mockWorkloadOps{
					findFunc: func() (*k8s.Workload, error) {
						call++
						if call > 1 {
							return nil, errors.New("cluster unreachable")
						}
						return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy()}, nil
					},
					resizeFunc:   func() error { return nil },
					statusFunc:   func() (*k8s.WorkloadStatus, error) { return nil, errors.New("crash detected") },
					isPausedFunc: func() (bool, error) { return false, nil },
				}
			}(),
			wantErr:     true,
			errContains: "failed update",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestContainerResizer()

			if tt.name == "Rollback - soak detects post-rollout crash and rollback succeeds" {
				var podListCalls int32
				fakeClient := fake.NewSimpleClientset()
				fakeClient.PrependReactor("list", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
					call := atomic.AddInt32(&podListCalls, 1)
					if call == 2 {
						return true, &corev1.PodList{Items: []corev1.Pod{{
							ObjectMeta: metav1.ObjectMeta{Name: "pod-crash", Namespace: "default", Labels: map[string]string{"app": "api"}},
							Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
								State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}},
							}}},
						}}}, nil
					}

					return true, &corev1.PodList{Items: []corev1.Pod{{
						ObjectMeta: metav1.ObjectMeta{Name: "pod-ok", Namespace: "default", Labels: map[string]string{"app": "api"}},
						Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{{
							State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}},
						}}},
					}}}, nil
				})

				r = NewContainerResizer(fakeClient, watcher.NewResizeWatcher())
			}

			var workload *k8s.Workload
			if tt.ops.findFunc != nil {
				workload, _ = tt.ops.findFunc()
			} else {
				workload = &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy()}
			}

			recs := []*model.Recommendation{}
			if tt.rec != nil {
				recs = []*model.Recommendation{tt.rec}
			}

			err := r.ApplyResize(context.Background(), recs, tt.ops, workload)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Errorf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestContainerResizer_ResizePrecheck tests the ResizePrecheck method
func TestContainerResizer_ResizePrecheck(t *testing.T) {
	t.Parallel()
	tmpl := basePodTemplate("100m", "128Mi")
	appLabels := map[string]string{"app": "my-service"}

	tests := []struct {
		name        string
		rec         *model.Recommendation
		ops         *mockWorkloadOps
		extraObjs   []runtime.Object
		ctxValues   map[any]any
		wantProceed bool
		wantErr     bool
		errContains string
	}{
		{
			name: "Success - all checks pass",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy(),
						UpdateStrategy: "RollingUpdate",
						LabelSelector:  &metav1.LabelSelector{MatchLabels: appLabels},
					}, nil
				},
				statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			extraObjs:   []runtime.Object{},
			wantProceed: true,
			wantErr:     false,
		},
		{
			name:        "Failure - workload is nil",
			rec:         &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops:         &mockWorkloadOps{},
			extraObjs:   []runtime.Object{},
			wantProceed: false,
			wantErr:     true,
			errContains: "workload cannot be nil",
		},
		{
			name: "Failure - workload is paused",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc:     func() (*k8s.Workload, error) { return &k8s.Workload{Name: "api"}, nil },
				isPausedFunc: func() (bool, error) { return true, nil },
			},
			extraObjs:   []runtime.Object{},
			wantProceed: false,
			wantErr:     true,
			errContains: "workload is paused",
		},
		{
			name: "Failure - PDB blocks disruption",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy(),
						UpdateStrategy: "RollingUpdate",
						LabelSelector:  &metav1.LabelSelector{MatchLabels: appLabels},
					}, nil
				},
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			extraObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "strict-pdb", Namespace: "default"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: appLabels}},
					Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			wantProceed: false,
			wantErr:     true,
			errContains: "skipping resize due to PDB restrictions",
		},
		{
			name: "Failure - UpdateStrategy is OnDelete",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy(),
						UpdateStrategy: "OnDelete",
					}, nil
				},
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			extraObjs:   []runtime.Object{},
			wantProceed: false,
			wantErr:     true,
			errContains: "OnDelete",
		},
		{
			name: "Failure - UpdateStrategy Recreate without override",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy(),
						UpdateStrategy: "Recreate",
					}, nil
				},
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			extraObjs:   []runtime.Object{},
			wantProceed: false,
			wantErr:     true,
			errContains: "Recreate",
		},
		{
			name: "Success - UpdateStrategy Recreate with resizeOnRecreate=true",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy(),
						UpdateStrategy: "Recreate",
						LabelSelector:  &metav1.LabelSelector{MatchLabels: appLabels},
					}, nil
				},
				statusFunc:   func() (*k8s.WorkloadStatus, error) { return stableStatus(), nil },
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			extraObjs:   []runtime.Object{},
			ctxValues:   map[any]any{ctxkeys.ResizeOnRecreateKey: true},
			wantProceed: true,
			wantErr:     false,
		},
		{
			name: "Failure - workload in degraded state",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "api", Container: "app"},
			ops: &mockWorkloadOps{
				findFunc: func() (*k8s.Workload, error) {
					return &k8s.Workload{Name: "api", Namespace: "default", Template: tmpl.DeepCopy(),
						UpdateStrategy: "RollingUpdate",
						LabelSelector:  &metav1.LabelSelector{MatchLabels: appLabels},
					}, nil
				},
				statusFunc: func() (*k8s.WorkloadStatus, error) {
					return &k8s.WorkloadStatus{ExpectedReplicas: 2, AvailableReplicas: 1}, nil
				},
				isPausedFunc: func() (bool, error) { return false, nil },
			},
			extraObjs:   []runtime.Object{},
			wantProceed: false,
			wantErr:     true,
			errContains: "degraded state",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestContainerResizer(tt.extraObjs...)

			ctx := context.Background()
			for k, v := range tt.ctxValues {
				ctx = context.WithValue(ctx, k, v)
			}

			var workload *k8s.Workload
			if tt.ops.findFunc != nil {
				workload, _ = tt.ops.findFunc()
			}

			err := r.ResizePrecheck(ctx, tt.ops, workload)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizePrecheck() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Errorf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestContainerResizer_CheckPodCriticalErrors tests the CheckPodCriticalErrors method
func TestContainerResizer_CheckPodCriticalErrors(t *testing.T) {
	t.Parallel()
	appLabels := map[string]string{"app": "test"}

	tests := []struct {
		name        string
		pods        []runtime.Object
		wantIsError bool
		wantReason  string
	}{
		{
			name: "No errors - all pods running",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-1", Namespace: "default", Labels: appLabels},
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
			name: "CrashLoopBackOff detected",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-crash", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: CrashLoopBackOff",
		},
		{
			name: "ImagePullBackOff detected",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-image", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ImagePullBackOff"}}},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: ImagePullBackOff",
		},
		{
			name: "OOMKilled detected in current state",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-oom", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container terminated with reason: OOMKilled",
		},
		{
			name: "OOMKilled detected in last termination state",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-oom-last", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{LastTerminationState: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container recently terminated with reason: OOMKilled",
		},
		{
			name: "ErrImagePull detected",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-err-image", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"}}},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: ErrImagePull",
		},
		{
			name: "Init container failure detected",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-init-fail", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						InitContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: CrashLoopBackOff",
		},
		{
			name: "Pod unschedulable due to resource pressure",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-pending", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "Insufficient memory"},
						},
					},
				},
			},
			wantIsError: false,
			wantReason:  "Autoscaler may add nodes",
		},
		{
			name: "Pod unschedulable due to hard scheduling constraint",
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-pending-hard", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: "Unschedulable", Message: "0/3 nodes are available: 3 node(s) didn't match node selector."},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Likely not recoverable via autoscaler",
		},
		{
			name:        "No pods - no errors",
			pods:        []runtime.Object{},
			wantIsError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestContainerResizer(tt.pods...)
			workload := &k8s.Workload{
				Namespace:     "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			}

			isError, reason := r.podSvc.CheckPodCriticalErrors(context.Background(), workload)

			if isError != tt.wantIsError {
				t.Errorf("CheckPodCriticalErrors() isError = %v, want %v", isError, tt.wantIsError)
			}
			if tt.wantReason != "" && !strings.Contains(reason, tt.wantReason) {
				t.Errorf("CheckPodCriticalErrors() reason = %q, want to contain %q", reason, tt.wantReason)
			}
		})
	}
}

// TestContainerResizer_CheckWorkloadStatus tests the function returned by CheckWorkloadStatus
func TestContainerResizer_CheckWorkloadStatus(t *testing.T) {
	t.Parallel()
	appLabels := map[string]string{"app": "test"}

	tests := []struct {
		name        string
		mockStatus  *k8s.WorkloadStatus
		statusErr   error
		pods        []runtime.Object
		wantReady   bool
		wantErr     bool
		errContains string
	}{
		{
			name:       "Rollout complete",
			mockStatus: stableStatus(),
			pods:       []runtime.Object{},
			wantReady:  true,
			wantErr:    false,
		},
		{
			name: "Rollout in progress",
			mockStatus: &k8s.WorkloadStatus{
				ExpectedReplicas: 3, UpdatedReplicas: 1, AvailableReplicas: 2,
				Generation: 2, ObservedGeneration: 1,
			},
			pods:      []runtime.Object{},
			wantReady: false,
			wantErr:   false,
		},
		{
			name:        "GetStatus returns error",
			statusErr:   errors.New("connection refused"),
			pods:        []runtime.Object{},
			wantReady:   false,
			wantErr:     true,
			errContains: "connection refused",
		},
		{
			name:       "OOMKilled pod detected during rollout",
			mockStatus: &k8s.WorkloadStatus{ExpectedReplicas: 1, UpdatedReplicas: 1, AvailableReplicas: 0},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-oom", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"}}},
						},
					},
				},
			},
			wantReady:   false,
			wantErr:     true,
			errContains: "fail detected",
		},
		{
			name:       "CrashLoopBackOff pod detected during rollout",
			mockStatus: &k8s.WorkloadStatus{ExpectedReplicas: 2, UpdatedReplicas: 1, AvailableReplicas: 1},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "pod-crash", Namespace: "default", Labels: appLabels},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
						},
					},
				},
			},
			wantReady:   false,
			wantErr:     true,
			errContains: "fail detected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestContainerResizer(tt.pods...)

			mOps := &mockWorkloadOps{
				statusFunc: func() (*k8s.WorkloadStatus, error) { return tt.mockStatus, tt.statusErr },
			}
			workload := &k8s.Workload{
				Name:          "test",
				Namespace:     "default",
				LabelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			}

			pollFn := r.CheckWorkloadStatus(context.Background(), mOps, workload)
			ready, err := pollFn(context.Background())

			if (err != nil) != tt.wantErr {
				t.Errorf("pollFn() error = %v, wantErr %v", err, tt.wantErr)
			}
			if ready != tt.wantReady {
				t.Errorf("pollFn() ready = %v, want %v", ready, tt.wantReady)
			}
			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Errorf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

// TestContainerResizer_IsPDBTooRestrictive tests the IsPDBTooRestrictive method
func TestContainerResizer_IsPDBTooRestrictive(t *testing.T) {
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
			name:          "No PDB in the cluster",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs:   []runtime.Object{},
			wantResult:    false,
		},
		{
			name:          "Nil label selector - no check performed",
			namespace:     "default",
			labelSelector: nil,
			initialObjs:   []runtime.Object{},
			wantResult:    false,
		},
		{
			name:          "PDB allows disruption",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-ok", Namespace: "default"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: appLabels}},
					Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 1},
				},
			},
			wantResult: false,
		},
		{
			name:          "PDB blocks disruption",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-strict", Namespace: "default"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: appLabels}},
					Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			wantResult: true,
		},
		{
			name:          "PDB for a different workload - no impact",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-other", Namespace: "default"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: otherLabels}},
					Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			wantResult: false,
		},
		{
			name:          "Multiple PDBs - one blocks",
			namespace:     "default",
			labelSelector: &metav1.LabelSelector{MatchLabels: appLabels},
			initialObjs: []runtime.Object{
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-ok", Namespace: "default"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: appLabels}},
					Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 1},
				},
				&policyv1.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "pdb-strict", Namespace: "default"},
					Spec:       policyv1.PodDisruptionBudgetSpec{Selector: &metav1.LabelSelector{MatchLabels: appLabels}},
					Status:     policyv1.PodDisruptionBudgetStatus{DisruptionsAllowed: 0},
				},
			},
			wantResult: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestContainerResizer(tt.initialObjs...)

			got, err := r.IsPDBTooRestrictive(context.Background(), tt.namespace, tt.labelSelector)

			if (err != nil) != tt.wantErr {
				t.Errorf("IsPDBTooRestrictive() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got != tt.wantResult {
				t.Errorf("IsPDBTooRestrictive() = %v, want %v", got, tt.wantResult)
			}
		})
	}
}

// TestContainerResizer_ResizeJob tests the ResizeJob worker method
func TestContainerResizer_ResizeJob(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		initialObjs   []runtime.Object
		rec           model.Recommendation
		expectedInRes string
	}{
		{
			name: "Success - Deployment resized",
			initialObjs: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"kubernetes.io/arch":               "amd64",
							"node.kubernetes.io/instance-type": "c5.x86",
						},
					},
					Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				},
			},
			rec: model.Recommendation{
				WorkloadName: "api", Namespace: "default", Kind: model.Deployment,
				Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi",
			},
			expectedInRes: "[OK]",
		},
		{
			name: "Success - StatefulSet resized",
			initialObjs: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
					Spec: appsv1.StatefulSetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "db"}},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "postgres"}}},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-1",
						Labels: map[string]string{
							"kubernetes.io/arch":               "amd64",
							"node.kubernetes.io/instance-type": "c5.x86",
						},
					},
					Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}}},
				},
			},
			rec: model.Recommendation{
				WorkloadName: "db", Namespace: "prod", Kind: model.StatefulSet,
				Container: "postgres", CpuRequestRecommendation: "500m", MemoryRequestRecommendation: "1Gi",
			},
			expectedInRes: "[OK]",
		},
		{
			name:        "Failure - workload not found",
			initialObjs: []runtime.Object{},
			rec: model.Recommendation{
				WorkloadName: "missing", Namespace: "default", Kind: model.Deployment,
				Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi",
			},
			expectedInRes: "[SKIP]",
		},
		{
			name:        "Failure - unsupported Kind",
			initialObjs: []runtime.Object{},
			rec: model.Recommendation{
				WorkloadName: "job-1", Namespace: "default", Kind: "CronJob",
				Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi",
			},
			expectedInRes: "unsupported resource Kind",
		},
		{
			name: "Failure - container not found in workload",
			initialObjs: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
						},
					},
				},
			},
			rec: model.Recommendation{
				WorkloadName: "api", Namespace: "default", Kind: model.Deployment,
				Container: "nonexistent", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi",
			},
			expectedInRes: "[SKIP]",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := newTestContainerResizer(tc.initialObjs...)

			recsChan := make(chan *model.Recommendation, 1)
			resultsChan := make(chan string, 1)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			recsChan <- &tc.rec
			close(recsChan)

			r.ResizeJob(ctx, recsChan, resultsChan)
			close(resultsChan)

			res, ok := <-resultsChan
			if !ok {
				t.Fatal("no result received from the results channel")
			}
			if !strings.Contains(res, tc.expectedInRes) {
				t.Errorf("ResizeJob() result = %q, expected to contain %q", res, tc.expectedInRes)
			}
		})
	}
}

// TestContainerResizer_Resize tests the top-level Resize method
func TestContainerResizer_Resize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		initialObjs []runtime.Object
		recs        []model.Recommendation
		numWorkers  int
		wantErr     bool
	}{
		{
			name: "Success - single recommendation processed",
			initialObjs: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "api"}},
						Template: corev1.PodTemplateSpec{
							Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}},
						},
					},
				},
			},
			recs: []model.Recommendation{
				{WorkloadName: "api", Namespace: "default", Kind: model.Deployment, Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"},
			},
			numWorkers: 1,
			wantErr:    false,
		},
		{
			name:        "Success - empty recommendations list",
			initialObjs: []runtime.Object{},
			recs:        []model.Recommendation{},
			numWorkers:  2,
			wantErr:     false,
		},
		{
			name: "Success - multiple recommendations with multiple workers",
			initialObjs: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-a", Namespace: "default"},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "svc-a"}},
						Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "svc-b", Namespace: "default"},
					Spec: appsv1.DeploymentSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "svc-b"}},
						Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "app"}}}},
					},
				},
			},
			recs: []model.Recommendation{
				{WorkloadName: "svc-a", Namespace: "default", Kind: model.Deployment, Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"},
				{WorkloadName: "svc-b", Namespace: "default", Kind: model.Deployment, Container: "app", CpuRequestRecommendation: "300m", MemoryRequestRecommendation: "512Mi"},
			},
			numWorkers: 2,
			wantErr:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestContainerResizer(tt.initialObjs...)

			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			err := r.Resize(ctx, tt.recs, tt.numWorkers)

			if (err != nil) != tt.wantErr {
				t.Errorf("Resize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestContainerResizer_ResizeJob_ContextCanceledSkipsProcessing(t *testing.T) {
	t.Parallel()

	r := newTestContainerResizer()
	recsChan := make(chan *model.Recommendation, 1)
	resultsChan := make(chan string, 1)

	recsChan <- &model.Recommendation{WorkloadName: "api", Namespace: "default", Kind: model.Deployment, Container: "app"}
	close(recsChan)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	r.ResizeJob(ctx, recsChan, resultsChan)
	close(resultsChan)

	if len(resultsChan) != 0 {
		t.Fatalf("expected no results when context is canceled, got %d", len(resultsChan))
	}
}

func TestContainerResizer_Resize_ZeroWorkersPanics(t *testing.T) {
	t.Parallel()

	r := newTestContainerResizer()
	recs := []model.Recommendation{{WorkloadName: "api", Namespace: "default", Kind: model.Deployment, Container: "app"}}

	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when numberOfWorkers is 0")
		}
	}()

	_ = r.Resize(context.Background(), recs, 0)
}
