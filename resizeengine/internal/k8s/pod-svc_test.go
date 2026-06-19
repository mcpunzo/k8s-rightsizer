package k8s

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestNewPodService(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		client K8sClient
	}{
		{
			name:   "creates service with non nil client",
			client: fake.NewSimpleClientset(),
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

			svc := NewPodService(tt.client)
			if svc == nil {
				t.Fatal("NewPodService() returned nil")
			}

			if svc.client != tt.client {
				t.Fatalf("NewPodService() client mismatch: got %v want %v", svc.client, tt.client)
			}
		})
	}
}

func TestPodService_Find(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		namespace     string
		fieldSelector string
		objects       []runtime.Object
		reactor       func(client *fake.Clientset)
		want          []*Pod
		wantErr       bool
		errContains   string
	}{
		{
			name:          "success returns pods from namespace",
			namespace:     "default",
			fieldSelector: "",
			objects: []runtime.Object{
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "default"}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "api-1", Namespace: "default"}},
				&corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "db-0", Namespace: "other"}},
			},
			want: []*Pod{
				{Name: "api-0", Namespace: "default"},
				{Name: "api-1", Namespace: "default"},
			},
		},
		{
			name:          "success empty list",
			namespace:     "default",
			fieldSelector: "",
			objects:       []runtime.Object{},
			want:          []*Pod{},
		},
		{
			name:          "failure list pods returns error",
			namespace:     "default",
			fieldSelector: "metadata.name=api-0",
			reactor: func(client *fake.Clientset) {
				client.PrependReactor("list", "pods", func(action k8stesting.Action) (bool, runtime.Object, error) {
					listAction, ok := action.(k8stesting.ListAction)
					if !ok {
						return true, nil, errors.New("unexpected action type")
					}

					if gotSelector := listAction.GetListRestrictions().Fields.String(); gotSelector != "metadata.name=api-0" {
						return true, nil, errors.New("unexpected field selector: " + gotSelector)
					}

					return true, nil, errors.New("list failed")
				})
			},
			wantErr:     true,
			errContains: "list failed",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := fake.NewSimpleClientset(tt.objects...)
			if tt.reactor != nil {
				tt.reactor(client)
			}

			svc := NewPodService(client)
			got, err := svc.Find(context.Background(), tt.namespace, tt.fieldSelector)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Find() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.errContains != "" && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("Find() error = %v, want to contain %q", err, tt.errContains)
			}

			if tt.wantErr {
				return
			}

			if len(got) == 0 && len(tt.want) == 0 {
				return
			}

			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("Find() got = %+v, want %+v", got, tt.want)
			}
		})
	}
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
			name:      "Pod pending with unschedulable due to resource pressure",
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
			wantIsError: false,
			wantReason:  "Autoscaler may add nodes",
		},
		{
			name:      "Pod pending with unschedulable due to node selector mismatch",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-pending-selector",
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
								Message: "0/3 nodes are available: 3 node(s) didn't match node selector.",
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Likely not recoverable via autoscaler",
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
			wantReason:  "Container terminated with reason: OOMKilled",
		},
		{
			name:      "Container waiting ErrImagePull",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-err-image-pull",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{Reason: "ErrImagePull"},
								},
							},
						},
					},
				},
			},
			wantIsError: true,
			wantReason:  "Container in error: ErrImagePull",
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
			wantReason:  "Container recently terminated with reason: OOMKilled",
		},
		{
			name:      "Init container in CrashLoopBackOff",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-init-crash",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						InitContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "init-setup",
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
			name:      "Ignore terminating pod in CrashLoopBackOff",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "pod-terminating-crash",
						Namespace:         "default",
						Labels:            map[string]string{"app": "test"},
						DeletionTimestamp: func() *metav1.Time { t := metav1.Now(); return &t }(),
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								State: corev1.ContainerState{
									Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"},
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
			name:      "Ignore stale last termination OOMKilled",
			namespace: "default",
			labels:    map[string]string{"app": "test"},
			pods: []runtime.Object{
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "pod-stale-oom",
						Namespace: "default",
						Labels:    map[string]string{"app": "test"},
					},
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								Name: "container-1",
								LastTerminationState: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{
										Reason:     "OOMKilled",
										FinishedAt: metav1.NewTime(time.Now().Add(-2 * time.Hour)),
									},
								},
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
			name:        "Pod list API failure is warning, not fatal",
			namespace:   "default",
			labels:      map[string]string{"app": "test"},
			pods:        []runtime.Object{},
			wantIsError: false,
			wantReason:  "[WARN] failed to list pods",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.pods...)
			if tt.name == "Pod list API failure is warning, not fatal" {
				fakeClient.PrependReactor("list", "pods", func(_ k8stesting.Action) (bool, runtime.Object, error) {
					return true, nil, errors.New("client rate limiter Wait returned an error: context deadline exceeded")
				})
			}

			podSvc := &PodService{
				client: fakeClient,
			}

			workload := &Workload{
				Namespace:     tt.namespace,
				LabelSelector: &metav1.LabelSelector{MatchLabels: tt.labels},
			}

			isError, reason := podSvc.CheckPodCriticalErrors(
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

			if !tt.wantIsError {
				if tt.wantReason == "" && reason != "" {
					t.Errorf("CheckPodCriticalErrors() expected no reason, got %q", reason)
				}
				if tt.wantReason != "" && !strings.Contains(reason, tt.wantReason) {
					t.Errorf("CheckPodCriticalErrors() reason = %q, want to contain %q", reason, tt.wantReason)
				}
			}
		})
	}
}
