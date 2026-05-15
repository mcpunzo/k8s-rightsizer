package k8s

import (
	"context"
	"strings"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/model"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

func TestResizeContainer(t *testing.T) {
	tests := []struct {
		name             string
		ctx              context.Context
		podTemplate      *corev1.PodTemplateSpec
		recommendation   *model.Recommendation
		wantUpdated      bool
		wantErr          bool
		errContains      string
		expectedCPU      string
		expectedMem      string
		expectedLimitCPU string
		expectedLimitMem string
	}{
		{
			name: "Success - Container found and resized",
			ctx:  context.Background(),
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
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "500m",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "512Mi",
			},
			wantUpdated: true,
			expectedCPU: "500m",
			expectedMem: "256Mi",
		},
		{
			name: "Failure - Container name not matching",
			ctx:  context.Background(),
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "nginx"},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "wrong-container",
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "100m",
				CpuLimitRecommendation:      "200m",
				MemoryRequestRecommendation: "128Mi",
				MemoryLimitRecommendation:   "256Mi",
			},
			wantUpdated: false,
			wantErr:     true,
			errContains: "Container wrong-container not found in  test-workload",
		},
		{
			name: "Success - Verify resource values are overwritten",
			ctx:  context.Background(),
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
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "512Mi",
				MemoryLimitRecommendation:   "1Gi",
			},
			wantUpdated: true,
			expectedCPU: "200m",
			expectedMem: "512Mi",
		},
		{
			name: "Failure - Recommendation already matches requests",
			ctx:  context.Background(),
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
							},
						},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "512Mi",
				MemoryLimitRecommendation:   "1Gi",
			},
			wantUpdated: false,
			wantErr:     true,
			errContains: "Container app in workload test-workload: resources match recommendation",
		},
		{
			name: "Success - Requests match but limits differ when useLimits enabled",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("500m"),
									corev1.ResourceMemory: resource.MustParse("1Gi"),
								},
							},
						},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "512Mi",
				MemoryLimitRecommendation:   "2Gi",
			},
			wantUpdated:      true,
			expectedCPU:      "200m",
			expectedMem:      "512Mi",
			expectedLimitCPU: "1",
			expectedLimitMem: "2Gi",
		},
		{
			name: "Failure - Requests and limits already match when useLimits enabled",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name: "app",
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("200m"),
									corev1.ResourceMemory: resource.MustParse("512Mi"),
								},
								Limits: corev1.ResourceList{
									corev1.ResourceCPU:    resource.MustParse("1"),
									corev1.ResourceMemory: resource.MustParse("2Gi"),
								},
							},
						},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "512Mi",
				MemoryLimitRecommendation:   "2Gi",
			},
			wantUpdated: false,
			wantErr:     true,
			errContains: "Container app in workload test-workload: resource requests and limits already match the recommendation",
		},
		{
			name: "Failure - Invalid CPU recommendation",
			ctx:  context.Background(),
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Resources: corev1.ResourceRequirements{}},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "not-a-quantity",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "512Mi",
			},
			wantUpdated: false,
			wantErr:     true,
			errContains: "invalid cpu request recommendation",
		},
		{
			name: "Failure - Invalid memory recommendation",
			ctx:  context.Background(),
			podTemplate: &corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{Name: "app", Resources: corev1.ResourceRequirements{}},
					},
				},
			},
			recommendation: &model.Recommendation{
				Container:                   "app",
				WorkloadName:                "test-workload",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "1000m",
				MemoryRequestRecommendation: "not-a-quantity",
				MemoryLimitRecommendation:   "512Mi",
			},
			wantUpdated: false,
			wantErr:     true,
			errContains: "invalid memory request recommendation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := tt.ctx
			if ctx == nil {
				ctx = context.Background()
			}
			workload := &Workload{
				Template: tt.podTemplate,
			}
			gotUpdated, err := workload.ResizeContainer(ctx, tt.recommendation)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ResizeContainer() error = %v, wantErr %v", err, tt.wantErr)
			}

			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("ResizeContainer() error = %v, want error containing %q", err, tt.errContains)
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

				if tt.expectedLimitCPU != "" {
					limitCPU := updatedContainer.Resources.Limits.Cpu().String()
					if limitCPU != tt.expectedLimitCPU {
						t.Errorf("CPU limit mismatch: got %v, want %v", limitCPU, tt.expectedLimitCPU)
					}
				}
				if tt.expectedLimitMem != "" {
					limitMem := updatedContainer.Resources.Limits.Memory().String()
					if limitMem != tt.expectedLimitMem {
						t.Errorf("Memory limit mismatch: got %v, want %v", limitMem, tt.expectedLimitMem)
					}
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
		ctx         context.Context
		rec         *model.Recommendation
		wantErr     bool
		errContains string
	}{
		{
			name: "valid recommendations within limits",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr: false,
		},
		{
			name: "invalid cpu quantity",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "not-a-quantity",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr:     true,
			errContains: "invalid cpu request recommendation",
		},
		{
			name: "invalid cpu limit quantity",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "invalid-limit",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr:     true,
			errContains: "invalid cpu limit recommendation",
		},
		{
			name: "invalid memory quantity",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "not-a-quantity",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr:     true,
			errContains: "invalid memory request recommendation",
		},
		{
			name: "invalid memory limit quantity",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "invalid-limit",
			},
			wantErr:     true,
			errContains: "invalid memory limit recommendation",
		},
		{
			name: "container not found",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "sidecar",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr:     true,
			errContains: "container sidecar not found in workload test",
		},
		{
			name: "cpu request greater than limit",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "600m",
				CpuLimitRecommendation:      "700m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr:     true,
			errContains: "cpu request (600m) cannot be greater than current limit (500m)",
		},
		{
			name: "memory request greater than limit",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "1Gi",
				MemoryLimitRecommendation:   "2Gi",
			},
			wantErr:     true,
			errContains: "memory request (1Gi) cannot be greater than current limit (512Mi)",
		},
		{
			name: "requests already match current requests",
			ctx:  context.Background(),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "100m",
				CpuLimitRecommendation:      "200m",
				MemoryRequestRecommendation: "128Mi",
				MemoryLimitRecommendation:   "256Mi",
			},
			wantErr:     true,
			errContains: "Container app in workload test: resource requests already match the recommendation",
		},
		{
			name: "useLimits enabled with valid recommendations",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "200m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr: false,
		},
		{
			name: "useLimits enabled cpu request greater than cpu limit recommendation",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "400m",
				CpuLimitRecommendation:      "300m",
				MemoryRequestRecommendation: "256Mi",
				MemoryLimitRecommendation:   "300Mi",
			},
			wantErr:     true,
			errContains: "cpu request recommendation (400m) cannot be greater than cpu limit recommendation (300m)",
		},
		{
			name: "useLimits enabled all resources already match",
			ctx:  ctxkeys.WithUseLimits(context.Background(), true),
			rec: &model.Recommendation{
				WorkloadName:                "test",
				Container:                   "app",
				CpuRequestRecommendation:    "100m",
				CpuLimitRecommendation:      "500m",
				MemoryRequestRecommendation: "128Mi",
				MemoryLimitRecommendation:   "512Mi",
			},
			wantErr:     true,
			errContains: "Container app in workload test: resource requests and limits already match the recommendation",
		},
	}

	workload := &Workload{Template: baseTemplate()}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := workload.ValidateRecommendations(tt.ctx, tt.rec)

			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateRecommendations() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && (err == nil || !strings.Contains(err.Error(), tt.errContains)) {
				t.Fatalf("ValidateRecommendations() error = %v, want error containing %q", err, tt.errContains)
			}
		})
	}
}
