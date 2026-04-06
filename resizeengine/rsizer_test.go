package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	core "k8s.io/client-go/kubernetes/typed/core/v1"
)

// --- MOCKS ---

// mockDeploymentClient implements v1.DeploymentInterface using embedding
type mockDeploymentClient struct {
	v1.DeploymentInterface
	updateFunc func(d *appsv1.Deployment) (*appsv1.Deployment, error)
}

func (m *mockDeploymentClient) Update(ctx context.Context, d *appsv1.Deployment, opts metav1.UpdateOptions) (*appsv1.Deployment, error) {
	return m.updateFunc(d)
}

// mockStatefulSetClient implements v1.StatefulSetInterface using embedding
type mockStatefulSetClient struct {
	v1.StatefulSetInterface // Embedding per soddisfare l'interfaccia automaticamente
	updateFunc              func(s *appsv1.StatefulSet) (*appsv1.StatefulSet, error)
}

func (m *mockStatefulSetClient) Update(ctx context.Context, s *appsv1.StatefulSet, opts metav1.UpdateOptions) (*appsv1.StatefulSet, error) {
	return m.updateFunc(s)
}

type mockPodClient struct {
	core.PodInterface
	listFunc func() (*corev1.PodList, error)
}

func (m *mockPodClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
	return m.listFunc()
}

type mockCoreV1 struct {
	core.CoreV1Interface
	podClient core.PodInterface
}

func (m *mockCoreV1) Pods(ns string) core.PodInterface {
	return m.podClient
}

// mockAppsV1 implements v1.AppsV1Interface
type mockAppsV1 struct {
	v1.AppsV1Interface
	deployClient v1.DeploymentInterface
	stsClient    v1.StatefulSetInterface
}

func (m *mockAppsV1) Deployments(ns string) v1.DeploymentInterface {
	return m.deployClient
}

func (m *mockAppsV1) StatefulSets(ns string) v1.StatefulSetInterface {
	return m.stsClient
}

// mockK8sClient is our top-level entry point for the Resizer
type mockK8sClient struct {
	K8sClient
	appsV1 v1.AppsV1Interface
	coreV1 core.CoreV1Interface
}

func (m *mockK8sClient) AppsV1() v1.AppsV1Interface {
	return m.appsV1
}

func (m *mockK8sClient) CoreV1() core.CoreV1Interface {
	return m.coreV1
}

// --- TABLE-DRIVEN TESTS ---

func TestResizeDeployment(t *testing.T) {
	const (
		namespace     = "default"
		deployName    = "test-deploy"
		containerName = "web-container"
	)

	tests := []struct {
		name           string
		deployment     *appsv1.Deployment
		recommendation model.Recommendation
		mockUpdateErr  error
		wantErr        bool
		expectedCPU    string
	}{
		{
			name: "Success - Container resources updated",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: deployName},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: containerName}},
						},
					},
				},
			},
			recommendation: model.Recommendation{
				Namespace:                   namespace,
				Container:                   containerName,
				CpuRequestRecommendation:    "500m",
				MemoryRequestRecommendation: "256Mi",
			},
			mockUpdateErr: nil,
			wantErr:       false,
			expectedCPU:   "500m",
		},
		{
			name: "Error - Container not found in pod spec",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: deployName},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "wrong-name"}},
						},
					},
				},
			},
			recommendation: model.Recommendation{
				Namespace: namespace,
				Container: containerName,
			},
			wantErr: true,
		},
		{
			name: "Error - K8s API update failure",
			deployment: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: deployName},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: containerName}},
						},
					},
				},
			},
			recommendation: model.Recommendation{
				Namespace:                   namespace,
				Container:                   containerName,
				CpuRequestRecommendation:    "100m",
				MemoryRequestRecommendation: "100Mi",
			},
			mockUpdateErr: errors.New("api error: conflict"),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Mocks
			mDeploy := &mockDeploymentClient{
				updateFunc: func(d *appsv1.Deployment) (*appsv1.Deployment, error) {
					return d, tt.mockUpdateErr
				},
			}
			mAppsV1 := &mockAppsV1{deployClient: mDeploy}
			mClient := &mockK8sClient{appsV1: mAppsV1}

			resizer := NewK8sWorkloadResizer(mClient)

			// Execute
			err := resizer.ResizeDeployment(context.Background(), tt.deployment, &tt.recommendation)

			// Assert Error
			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeDeployment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Assert Results on Success
			if !tt.wantErr {
				cpu := tt.deployment.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU]
				if cpu.String() != tt.expectedCPU {
					t.Errorf("Expected CPU %s, got %s", tt.expectedCPU, cpu.String())
				}
			}
		})
	}
}

func TestResizeStatefulSet(t *testing.T) {
	const (
		namespace     = "database"
		stsName       = "postgres-cluster"
		containerName = "postgres"
	)

	tests := []struct {
		name           string
		statefulSet    *appsv1.StatefulSet
		recommendation model.Recommendation
		mockUpdateErr  error
		wantErr        bool
		expectedMem    string
	}{
		{
			name: "Success - StatefulSet resources updated correctly",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: stsName, Namespace: namespace},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: containerName}},
						},
					},
				},
			},
			recommendation: model.Recommendation{
				Namespace:                   namespace,
				Container:                   containerName,
				CpuRequestRecommendation:    "2000m",
				MemoryRequestRecommendation: "4Gi",
			},
			mockUpdateErr: nil,
			wantErr:       false,
			expectedMem:   "4Gi",
		},
		{
			name: "Error - Container name not found in StatefulSet",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: stsName},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "redis"}},
						},
					},
				},
			},
			recommendation: model.Recommendation{
				Container: containerName,
			},
			wantErr: true,
		},
		{
			name: "Error - API Server returns Conflict (409)",
			statefulSet: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: stsName},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: containerName}},
						},
					},
				},
			},
			recommendation: model.Recommendation{
				Container:                   containerName,
				CpuRequestRecommendation:    "1",
				MemoryRequestRecommendation: "1Gi",
			},
			mockUpdateErr: errors.New("Operation cannot be fulfilled on statefulsets.apps: the object has been modified"),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inizializzazione Mock
			mSts := &mockStatefulSetClient{
				updateFunc: func(s *appsv1.StatefulSet) (*appsv1.StatefulSet, error) {
					return s, tt.mockUpdateErr
				},
			}
			mApps := &mockAppsV1{stsClient: mSts}
			mClient := &mockK8sClient{appsV1: mApps} // Utilizza il mockK8sClient definito in precedenza

			resizer := NewK8sWorkloadResizer(mClient)

			// Esecuzione
			err := resizer.ResizeStatefulSet(context.Background(), tt.statefulSet, &tt.recommendation)

			// Verifiche
			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeStatefulSet() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				mem := tt.statefulSet.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceMemory]
				if mem.String() != tt.expectedMem {
					t.Errorf("expected Memory %s, got %s", tt.expectedMem, mem.String())
				}
			}
		})
	}
}

func TestCheckPodCriticalErrors(t *testing.T) {
	const ns = "prod"

	tests := []struct {
		name          string
		pods          []corev1.Pod
		expectedError bool
		expectedMsg   string
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
			expectedError: false,
			expectedMsg:   "",
		},
		{
			name: "Error - Pod Unschedulable (Cluster Full)",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						Phase: corev1.PodPending,
						Conditions: []corev1.PodCondition{
							{
								Type:    corev1.PodScheduled,
								Status:  corev1.ConditionFalse,
								Reason:  "Unschedulable",
								Message: "Insufficient cpu",
							},
						},
					},
				},
			},
			expectedError: true,
			expectedMsg:   "Insufficient resources in the cluster: Insufficient cpu",
		},
		{
			name: "Error - Container CrashLoopBackOff",
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
			expectedError: true,
			expectedMsg:   "Container in error: CrashLoopBackOff",
		},
		{
			name: "Error - Immediate OOMKilled",
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
			expectedError: true,
			expectedMsg:   "OOMKilled: Insufficient memory for startup",
		},
		{
			name: "Error - OOMKilled in LastTerminationState",
			pods: []corev1.Pod{
				{
					Status: corev1.PodStatus{
						ContainerStatuses: []corev1.ContainerStatus{
							{
								LastTerminationState: corev1.ContainerState{
									Terminated: &corev1.ContainerStateTerminated{Reason: "OOMKilled"},
								},
							},
						},
					},
				},
			},
			expectedError: true,
			expectedMsg:   "OOMKilled detected in the last restart: Insufficient memory for startup",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Mock
			mPod := &mockPodClient{
				listFunc: func() (*corev1.PodList, error) {
					return &corev1.PodList{Items: tt.pods}, nil
				},
			}
			mCore := &mockCoreV1{podClient: mPod}
			client := &mockK8sClient{coreV1: mCore}

			resizer := NewK8sWorkloadResizer(client)
			deploy := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "test", Namespace: ns},
				Spec:       appsv1.DeploymentSpec{Selector: &metav1.LabelSelector{}},
			}

			// Execute
			isErr, msg := resizer.CheckPodCriticalErrors(context.Background(), deploy.Namespace, deploy.Spec.Selector)

			// Assert
			if isErr != tt.expectedError {
				t.Errorf("expected error status %v, got %v", tt.expectedError, isErr)
			}
			if msg != tt.expectedMsg {
				t.Errorf("expected message '%s', got '%s'", tt.expectedMsg, msg)
			}
		})
	}
}
