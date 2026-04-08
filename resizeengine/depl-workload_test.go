package resizeengine

import (
	"context"
	"errors"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestDeploymentWorkload_FindWorkload(t *testing.T) {
	tests := []struct {
		name         string
		rec          *model.Recommendation
		mockDeploy   *appsv1.Deployment
		mockErr      error
		wantErr      bool
		expectedType WorkloadType
	}{
		{
			name: "Success - Found Deployment",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "web-app", Container: "nginx"},
			mockDeploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "web-app", Namespace: "default"},
				Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{}},
			},
			mockErr:      nil,
			wantErr:      false,
			expectedType: Deployment,
		},
		{
			name:       "Failure - Deployment Not Found",
			rec:        &model.Recommendation{Namespace: "default", WorkloadName: "missing"},
			mockDeploy: nil,
			mockErr:    errors.New("deployments.apps \"missing\" not found"),
			wantErr:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mDep := &mockDeployClient{getFunc: func() (*appsv1.Deployment, error) { return tt.mockDeploy, tt.mockErr }}
			mApps := &mockAppsV1{deployClient: mDep}
			mClient := &mockK8sClient{appsV1: mApps}

			w := &DeploymentWorkload{client: mClient}
			got, err := w.FindWorkload(context.Background(), tt.rec)

			if (err != nil) != tt.wantErr {
				t.Errorf("FindWorkload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.WorkloadType != tt.expectedType {
					t.Errorf("WorkloadType mismatch: got %v, want %v", got.WorkloadType, tt.expectedType)
				}
				if got.Name != tt.rec.WorkloadName {
					t.Errorf("Name mismatch: got %s, want %s", got.Name, tt.rec.WorkloadName)
				}
			}
		})
	}
}

func TestDeploymentWorkload_ResizeWorkload(t *testing.T) {
	baseDeploy := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "api-container"}},
				},
			},
		},
	}

	tests := []struct {
		name     string
		workload *Workload
		rec      *model.Recommendation
		wantErr  bool
	}{
		{
			name: "Success - Valid Resize",
			workload: &Workload{
				WorkloadType:     Deployment,
				Template:         &baseDeploy.Spec.Template,
				originalResource: baseDeploy,
			},
			rec: &model.Recommendation{
				Container:                   "api-container",
				CpuRequestRecommendation:    "250m",
				MemoryRequestRecommendation: "512Mi",
			},
			wantErr: false,
		},
		{
			name: "Failure - Wrong Container Name",
			workload: &Workload{
				WorkloadType:     Deployment,
				Template:         &baseDeploy.Spec.Template,
				originalResource: baseDeploy,
			},
			rec: &model.Recommendation{
				Container: "invalid-container",
			},
			wantErr: true,
		},
		{
			name: "Failure - Wrong Workload Type",
			workload: &Workload{
				WorkloadType:     StatefulSet, // Passiamo STS a un handler Deployment
				Template:         &baseDeploy.Spec.Template,
				originalResource: baseDeploy,
			},
			rec:     &model.Recommendation{Container: "api-container"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mDep := &mockDeployClient{
				updateFunc: func(d *appsv1.Deployment) (*appsv1.Deployment, error) { return d, nil },
			}
			mApps := &mockAppsV1{deployClient: mDep}
			mClient := &mockK8sClient{appsV1: mApps}

			w := &DeploymentWorkload{client: mClient}
			err := w.ResizeWorkload(context.Background(), tt.workload, tt.rec)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestDeploymentWorkload_GetStatus(t *testing.T) {
	replicas := int32(2)

	tests := []struct {
		name            string
		mockDeploy      *appsv1.Deployment
		expectedReady   int32
		expectedDesired int32
	}{
		{
			name: "Status - Partial Rollout",
			mockDeploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: &replicas},
				Status: appsv1.DeploymentStatus{UpdatedReplicas: 2, AvailableReplicas: 1},
			},
			expectedReady:   1,
			expectedDesired: 2,
		},
		{
			name: "Status - Replicas Nil",
			mockDeploy: &appsv1.Deployment{
				Spec:   appsv1.DeploymentSpec{Replicas: nil},
				Status: appsv1.DeploymentStatus{UpdatedReplicas: 0, AvailableReplicas: 0},
			},
			expectedReady:   0,
			expectedDesired: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mDep := &mockDeployClient{getFunc: func() (*appsv1.Deployment, error) { return tt.mockDeploy, nil }}
			mApps := &mockAppsV1{deployClient: mDep}
			mClient := &mockK8sClient{appsV1: mApps}

			w := &DeploymentWorkload{client: mClient}
			status, err := w.GetStatus(context.Background(), "default", "api")

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if status.AvailableReplicas != tt.expectedReady {
				t.Errorf("AvailableReplicas: got %d, want %d", status.AvailableReplicas, tt.expectedReady)
			}
			if status.Replicas != tt.expectedDesired {
				t.Errorf("Replicas: got %d, want %d", status.Replicas, tt.expectedDesired)
			}
		})
	}
}
