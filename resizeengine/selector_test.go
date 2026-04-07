package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
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

type mockStatefulSetGetter struct {
	v1.StatefulSetInterface
	getFunc func(name string) (*appsv1.StatefulSet, error)
}

func (m *mockStatefulSetGetter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.StatefulSet, error) {
	return m.getFunc(name)
}

type mockAppsV1Selector struct {
	v1.AppsV1Interface
	stsClient v1.StatefulSetInterface
}

func (m *mockAppsV1Selector) StatefulSets(ns string) v1.StatefulSetInterface {
	return m.stsClient
}

// --- TABLE-DRIVEN TESTS ---

func TestFindDeployment(t *testing.T) {
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

			selector := NewK8sWorkloadSelector(mClient)

			// Execute
			got, err := selector.FindDeployment(tt.ctx, &tt.recommendation)

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

func TestK8sWorkloadSelector_FindStatefulSet_TableDriven(t *testing.T) {
	const (
		testNs   = "prod-db"
		testName = "postgres"
	)

	tests := []struct {
		name           string
		recommendation *model.Recommendation
		mockResponse   *appsv1.StatefulSet
		mockErr        error
		wantErr        bool
		expectedName   string
	}{
		{
			name: "Success - StatefulSet found",
			recommendation: &model.Recommendation{
				Namespace:    testNs,
				WorkloadName: testName,
			},
			mockResponse: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{
					Name:      testName,
					Namespace: testNs,
				},
			},
			mockErr:      nil,
			wantErr:      false,
			expectedName: testName,
		},
		{
			name: "Error - StatefulSet not found (API Error)",
			recommendation: &model.Recommendation{
				Namespace:    testNs,
				WorkloadName: "non-existent",
			},
			mockResponse: nil,
			mockErr:      errors.New("statefulsets.apps \"non-existent\" not found"),
			wantErr:      true,
		},
		{
			name: "Error - Namespace mismatch or empty name",
			recommendation: &model.Recommendation{
				Namespace:    "wrong-ns",
				WorkloadName: "",
			},
			mockResponse: nil,
			mockErr:      errors.New("invalid request"),
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 1. Configurazione del Mock
			mSts := &mockStatefulSetGetter{
				getFunc: func(name string) (*appsv1.StatefulSet, error) {
					return tt.mockResponse, tt.mockErr
				},
			}
			mApps := &mockAppsV1Selector{stsClient: mSts}
			mClient := &mockK8sClient{appsV1: mApps}

			// 2. Inizializzazione del selettore
			selector := NewK8sWorkloadSelector(mClient)

			// 3. Esecuzione
			got, err := selector.FindStatefulSet(context.Background(), tt.recommendation)

			// 4. Verifiche
			if (err != nil) != tt.wantErr {
				t.Errorf("FindStatefulSet() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if got == nil {
					t.Fatal("expected result, got nil")
				}
				if got.Name != tt.expectedName {
					t.Errorf("expected name %s, got %s", tt.expectedName, got.Name)
				}
			}
		})
	}
}
