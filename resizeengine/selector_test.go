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

// mockDeploymentSelectorClient implements v1.DeploymentInterface
type mockDeploymentSelectorClient struct {
	v1.DeploymentInterface
	getFunc  func(name string) (*appsv1.Deployment, error)
	listFunc func() (*appsv1.DeploymentList, error)
}

func (m *mockDeploymentSelectorClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error) {
	return m.getFunc(name)
}

func (m *mockDeploymentSelectorClient) List(ctx context.Context, opts metav1.ListOptions) (*appsv1.DeploymentList, error) {
	return m.listFunc()
}

// Reuse the same mockAppsV1 from the previous example,
// ensuring it returns our specialized selector mock.
type mockSelectorAppsV1 struct {
	v1.AppsV1Interface
	deployClient v1.DeploymentInterface
}

func (m *mockSelectorAppsV1) Deployments(ns string) v1.DeploymentInterface {
	return m.deployClient
}

// --- TABLE-DRIVEN TESTS ---

func TestFindDeployment_TableDriven(t *testing.T) {
	const (
		namespace     = "default"
		containerName = "app-container"
		workloadName  = "main-deploy"
	)

	tests := []struct {
		name           string
		ctx            context.Context
		recommendation model.Recommendation
		mockGet        func(name string) (*appsv1.Deployment, error)
		mockList       func() (*appsv1.DeploymentList, error)
		wantErr        bool
		expectedName   string
	}{
		{
			name: "Success - Found by exact name",
			ctx:  context.Background(),
			recommendation: model.Recommendation{
				Namespace:    namespace,
				WorkloadName: workloadName,
			},
			mockGet: func(name string) (*appsv1.Deployment, error) {
				return &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: workloadName}}, nil
			},
			wantErr:      false,
			expectedName: workloadName,
		},
		{
			name: "Success - Deep Search finds container",
			ctx:  context.WithValue(context.Background(), "deepResize", true),
			recommendation: model.Recommendation{
				Namespace:    namespace,
				WorkloadName: "", // Trigger deep search
				Container:    containerName,
			},
			mockList: func() (*appsv1.DeploymentList, error) {
				return &appsv1.DeploymentList{
					Items: []appsv1.Deployment{
						{
							ObjectMeta: metav1.ObjectMeta{Name: "wrong-deploy"},
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "other"}}},
								},
							},
						},
						{
							ObjectMeta: metav1.ObjectMeta{Name: "correct-deploy"},
							Spec: appsv1.DeploymentSpec{
								Template: corev1.PodTemplateSpec{
									Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: containerName}}},
								},
							},
						},
					},
				}, nil
			},
			wantErr:      false,
			expectedName: "correct-deploy",
		},
		{
			name: "Error - Deep Search disabled but name missing",
			ctx:  context.Background(), // deepResize is nil/false
			recommendation: model.Recommendation{
				Namespace:    namespace,
				WorkloadName: "",
				Container:    containerName,
			},
			wantErr: true,
		},
		{
			name: "Error - API Get returns 404",
			ctx:  context.Background(),
			recommendation: model.Recommendation{
				Namespace:    namespace,
				WorkloadName: "ghost-deploy",
			},
			mockGet: func(name string) (*appsv1.Deployment, error) {
				return nil, errors.New("deployments.apps \"ghost-deploy\" not found")
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup Mocks
			mDeploy := &mockDeploymentSelectorClient{
				getFunc:  tt.mockGet,
				listFunc: tt.mockList,
			}
			mApps := &mockSelectorAppsV1{deployClient: mDeploy}
			mClient := &mockK8sClient{appsV1: mApps}

			selector := NewWorkloadSelector(mClient)

			// Execute
			got, err := selector.FindDeployment(tt.ctx, tt.recommendation)

			// Assert Error
			if (err != nil) != tt.wantErr {
				t.Errorf("FindDeployment() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Assert Name Result
			if !tt.wantErr && got.Name != tt.expectedName {
				t.Errorf("FindDeployment() got = %v, want %v", got.Name, tt.expectedName)
			}
		})
	}
}
