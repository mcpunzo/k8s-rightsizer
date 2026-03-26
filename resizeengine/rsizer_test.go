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

// mockAppsV1 implements v1.AppsV1Interface using embedding
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
	appsV1 v1.AppsV1Interface
}

func (m *mockK8sClient) AppsV1() v1.AppsV1Interface {
	return m.appsV1
}

// --- TABLE-DRIVEN TESTS ---

func TestResizeDeployment_TableDriven(t *testing.T) {
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

			resizer := NewWorkloadResizer(mClient)

			// Execute
			err := resizer.ResizeDeployment(context.Background(), tt.deployment, tt.recommendation)

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
