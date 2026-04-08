package resizeengine

import (
	"context"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResizeContainer(t *testing.T) {
	tests := []struct {
		name           string
		podTemplate    *corev1.PodTemplateSpec
		recommendation *model.Recommendation
		wantUpdated    bool
		expectedCPU    string
		expectedMem    string
	}{
		{
			name: "Success - Container found and resized",
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Resources: corev1.ResourceRequirements{}},
						{Name: "sidecar", Resources: corev1.ResourceRequirements{}},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				CpuRequestRecommendation:    "500m",
				MemoryRequestRecommendation: "256Mi",
			},
			wantUpdated: true,
			expectedCPU: "500m",
			expectedMem: "256Mi",
		},
		{
			name: "Failure - Container name not matching",
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "nginx"},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "wrong-container",
				CpuRequestRecommendation:    "100m",
				MemoryRequestRecommendation: "128Mi",
			},
			wantUpdated: false,
		},
		{
			name: "Success - Verify resource values are overwritten",
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "512Mi",
			},
			wantUpdated: true,
			expectedCPU: "200m",
			expectedMem: "512Mi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			gotUpdated := ResizeContainer(ctx, tt.podTemplate, tt.recommendation)

			if gotUpdated != tt.wantUpdated {
				t.Errorf("ResizeContainer() gotUpdated = %v, want %v", gotUpdated, tt.wantUpdated)
			}

			if tt.wantUpdated {
				// Trova il container aggiornato per verificare i valori
				var updatedContainer corev1.Container
				for _, c := range tt.podTemplate.Spec.Containers {
					if c.Name == tt.recommendation.Container {
						updatedContainer = c
						break
					}
				}

				cpu := updatedContainer.Resources.Requests.Cpu().String()
				mem := updatedContainer.Resources.Requests.Memory().String()

				if cpu != tt.expectedCPU {
					t.Errorf("CPU request mismatch: got %v, want %v", cpu, tt.expectedCPU)
				}
				if mem != tt.expectedMem {
					t.Errorf("Memory request mismatch: got %v, want %v", mem, tt.expectedMem)
				}
			}
		})
	}
}
