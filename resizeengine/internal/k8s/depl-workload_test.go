package k8s

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
	t.Parallel()
	tests := []struct {
		name         string
		recs         []*model.Recommendation
		initialObjs  []runtime.Object
		wantErr      bool
		expectedType WorkloadType
	}{
		{
			name: "Success - Found Deployment",
			recs: []*model.Recommendation{{Namespace: "default", WorkloadName: "web-app", Container: "nginx"}},
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
			recs:        []*model.Recommendation{{Namespace: "default", WorkloadName: "missing"}},
			initialObjs: []runtime.Object{},
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.initialObjs...)

			w := &DeploymentWorkload{client: fakeClient}
			got, err := w.FindWorkload(context.Background(), tt.recs[0])

			if (err != nil) != tt.wantErr {
				t.Errorf("FindWorkload() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if got.WorkloadType != tt.expectedType {
					t.Errorf("WorkloadType mismatch: got %v, want %v", got.WorkloadType, tt.expectedType)
				}
				if got.Name != tt.recs[0].WorkloadName {
					t.Errorf("Name mismatch: got %s, want %s", got.Name, tt.recs[0].WorkloadName)
				}
			}
		})
	}
}

func TestDeploymentWorkload_ResizeWorkload(t *testing.T) {
	tests := []struct {
		name           string
		initialDep     *appsv1.Deployment
		recs           []*model.Recommendation
		wantErr        bool
		expectedCPU    map[string]string
		expectedMemory map[string]string
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
			recs: []*model.Recommendation{
				{
					WorkloadName:                "api",
					Namespace:                   "prod",
					Container:                   "api-container",
					CpuRequestRecommendation:    "250m",
					CpuLimitRecommendation:      "500m",
					MemoryRequestRecommendation: "512Mi",
					MemoryLimitRecommendation:   "1Gi",
				},
			},
			wantErr:        false,
			expectedCPU:    map[string]string{"api-container": "250m"},
			expectedMemory: map[string]string{"api-container": "512Mi"},
		},
		{
			name: "Success - Multiple Container Recommendations",
			initialDep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "api-container"},
								{Name: "sidecar"},
							},
						},
					},
				},
			},
			recs: []*model.Recommendation{
				{
					WorkloadName:                "api",
					Namespace:                   "prod",
					Container:                   "api-container",
					CpuRequestRecommendation:    "300m",
					CpuLimitRecommendation:      "600m",
					MemoryRequestRecommendation: "640Mi",
					MemoryLimitRecommendation:   "1280Mi",
				},
				{
					WorkloadName:                "api",
					Namespace:                   "prod",
					Container:                   "sidecar",
					CpuRequestRecommendation:    "100m",
					CpuLimitRecommendation:      "200m",
					MemoryRequestRecommendation: "128Mi",
					MemoryLimitRecommendation:   "256Mi",
				},
			},
			wantErr: false,
			expectedCPU: map[string]string{
				"api-container": "300m",
				"sidecar":       "100m",
			},
			expectedMemory: map[string]string{
				"api-container": "640Mi",
				"sidecar":       "128Mi",
			},
		},
		{
			name: "Success - Multiple Recommendations With One Invalid Container",
			initialDep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
				Spec: appsv1.DeploymentSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "api-container"},
								{Name: "sidecar"},
							},
						},
					},
				},
			},
			recs: []*model.Recommendation{
				{
					WorkloadName:                "api",
					Namespace:                   "prod",
					Container:                   "api-container",
					CpuRequestRecommendation:    "275m",
					CpuLimitRecommendation:      "550m",
					MemoryRequestRecommendation: "576Mi",
					MemoryLimitRecommendation:   "1152Mi",
				},
				{
					WorkloadName:                "api",
					Namespace:                   "prod",
					Container:                   "missing-container",
					CpuRequestRecommendation:    "50m",
					CpuLimitRecommendation:      "100m",
					MemoryRequestRecommendation: "64Mi",
					MemoryLimitRecommendation:   "128Mi",
				},
			},
			wantErr:        false,
			expectedCPU:    map[string]string{"api-container": "275m"},
			expectedMemory: map[string]string{"api-container": "576Mi"},
		},
		{
			name: "Failure - Wrong Container Name",
			initialDep: &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "prod"},
				Spec:       appsv1.DeploymentSpec{Template: corev1.PodTemplateSpec{Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "api-container"}}}}},
			},
			recs: []*model.Recommendation{
				{
					WorkloadName: "api",
					Namespace:    "prod",
					Container:    "invalid-container",
				},
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
				OriginalResource: tt.initialDep,
			}

			w := &DeploymentWorkload{client: fakeClient}
			err := w.ResizeWorkload(context.Background(), wObj, tt.recs)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr {
				return
			}

			for _, c := range tt.initialDep.Spec.Template.Spec.Containers {
				expectedCPU, ok := tt.expectedCPU[c.Name]
				if ok && c.Resources.Requests.Cpu().String() != expectedCPU {
					t.Errorf("container %s cpu request mismatch: got %s, want %s", c.Name, c.Resources.Requests.Cpu().String(), expectedCPU)
				}

				expectedMemory, ok := tt.expectedMemory[c.Name]
				if ok && c.Resources.Requests.Memory().String() != expectedMemory {
					t.Errorf("container %s memory request mismatch: got %s, want %s", c.Name, c.Resources.Requests.Memory().String(), expectedMemory)
				}
			}
		})
	}
}

func TestDeploymentWorkload_GetStatus(t *testing.T) {
	t.Parallel()
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
			workload := &Workload{
				WorkloadType: Deployment,
				Namespace:    "default",
				Name:         "api",
			}
			status, err := w.GetStatus(context.Background(), workload)

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if status.AvailableReplicas != tt.expectedReady {
				t.Errorf("AvailableReplicas: got %d, want %d", status.AvailableReplicas, tt.expectedReady)
			}
			if status.ExpectedReplicas != tt.expectedDesired {
				t.Errorf("Replicas: got %d, want %d", status.ExpectedReplicas, tt.expectedDesired)
			}
		})
	}
}
