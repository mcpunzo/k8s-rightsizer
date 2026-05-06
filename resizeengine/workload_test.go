package resizeengine

import (
	"context"
	"strings"
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
			gotUpdated, err := ResizeContainer(ctx, tt.podTemplate, tt.recommendation)

			if err != nil && gotUpdated {
				t.Fatalf("ResizeContainer() error = %v", err)
			}

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

func TestWorkload_ValidateRecommendations(t *testing.T) {
	t.Parallel()

	baseTemplate := func() *corev1.PodTemplateSpec {
		return &corev1.PodTemplateSpec{
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{
						Name: "app",
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("512Mi"),
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name        string
		rec         *model.Recommendation
		template    *corev1.PodTemplateSpec
		wantErr     bool
		errContains string
	}{
		{
			name: "valid recommendations within limits",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			template: baseTemplate(),
		},
		{
			name: "invalid cpu quantity",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "not-a-quantity",
				MemoryRequestRecommendation: "256Mi",
			},
			template:    baseTemplate(),
			wantErr:     true,
			errContains: "invalid cpu request recommendation",
		},
		{
			name: "invalid memory quantity",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "not-a-quantity",
			},
			template:    baseTemplate(),
			wantErr:     true,
			errContains: "invalid memory request recommendation",
		},
		{
			name: "container not found",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "sidecar",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "256Mi",
			},
			template:    baseTemplate(),
			wantErr:     true,
			errContains: "container sidecar not found in workload test",
		},
		{
			name: "cpu request greater than limit",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "600m",
				MemoryRequestRecommendation: "256Mi",
			},
			template:    baseTemplate(),
			wantErr:     true,
			errContains: "cpu request (600m) cannot be greater than current limit (500m)",
		},
		{
			name: "memory request greater than limit",
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				MemoryRequestRecommendation: "1Gi",
			},
			template:    baseTemplate(),
			wantErr:     true,
			errContains: "memory request (1Gi) cannot be greater than current limit (512Mi)",
		},
	}

	workload := &Workload{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := workload.ValidateRecommendations(context.Background(), tt.template, tt.rec)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRecommendations() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("ValidateRecommendations() error = %v, want error containing %q", err, tt.errContains)
			}
		})
	}
}
