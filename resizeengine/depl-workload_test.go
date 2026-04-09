package resizeengine

import (
	"context"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestDeploymentWorkload_FindWorkload(t *testing.T) {
	tests := []struct {
		name         string
		rec          *model.Recommendation
		initialObjs  []runtime.Object
		wantErr      bool
		expectedType WorkloadType
	}{
		{
			name: "Success - Found Deployment",
			rec:  &model.Recommendation{Namespace: "default", WorkloadName: "web-app", Container: "nginx"},
			initialObjs: []runtime.Object{
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "web-app", Namespace: "default"},
					Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{}},
				},
			},
			wantErr:      false,
			expectedType: Deployment,
		},
		{
			name:        "Failure - Deployment Not Found",
			rec:         &model.Recommendation{Namespace: "default", WorkloadName: "missing"},
			initialObjs: []runtime.Object{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inizializziamo il fake client con gli oggetti caricati in memoria
			fakeClient := fake.NewSimpleClientset(tt.initialObjs...)

			w := &DeploymentWorkload{client: fakeClient}
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
	tests := []struct {
		name       string
		initialDep *appsv1.Deployment
		rec        *model.Recommendation
		wantErr    bool
	}{
		{
			name: "Success - Valid Resize",
			initialDep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "api-container"}},
						},
					},
				},
			},
			rec: &model.Recommendation{
				WorkloadName:                "api",
				Namespace:                   "prod",
				Container:                   "api-container",
				CpuRequestRecommendation:    "250m",
				MemoryRequestRecommendation: "512Mi",
			},
			wantErr: false,
		},
		{
			name: "Failure - Wrong Container Name",
			initialDep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
				Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api-container"}}}}},
			},
			rec: &model.Recommendation{
				WorkloadName: "api",
				Namespace:    "prod",
				Container:    "invalid-container",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.initialDep)

			wObj := &Workload{
				WorkloadType:     Deployment,
				Namespace:        tt.initialDep.Namespace,
				Name:             tt.initialDep.Name,
				Template:         &tt.initialDep.Spec.Template,
				originalResource: tt.initialDep,
			}

			w := &DeploymentWorkload{client: fakeClient}
			err := w.ResizeWorkload(context.Background(), wObj, tt.rec)

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
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
				Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
				Status:     appsv1.DeploymentStatus{UpdatedReplicas: 2, AvailableReplicas: 1},
			},
			expectedReady:   1,
			expectedDesired: 2,
		},
		{
			name: "Status - Replicas Nil",
			mockDeploy: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "default"},
				Spec:       appsv1.DeploymentSpec{Replicas: nil},
				Status:     appsv1.DeploymentStatus{UpdatedReplicas: 0, AvailableReplicas: 0},
			},
			expectedReady:   0,
			expectedDesired: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.mockDeploy)

			w := &DeploymentWorkload{client: fakeClient}
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
