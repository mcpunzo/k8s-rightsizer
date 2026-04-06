package resizeengine

import (
	"context"
	"testing"

	"github.com/mcpunzo/k8s-rightsizer/model"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// --- MOCKS ---

type mockSelector struct {
	findDeployFn func() (*v1.Deployment, error)
}

func (m *mockSelector) FindDeployment(ctx context.Context, r *model.Recommendation) (*v1.Deployment, error) {
	return m.findDeployFn()
}

func (m *mockSelector) FindStatefulSet(ctx context.Context, r *model.Recommendation) (*v1.StatefulSet, error) {
	return nil, nil // Omesso per brevità
}

type mockResizer struct {
	resizeErr error
	isCritErr bool
}

func (m *mockResizer) ResizeDeployment(ctx context.Context, d *v1.Deployment, r *model.Recommendation) error {
	return m.resizeErr
}
func (m *mockResizer) CheckPodCriticalErrors(ctx context.Context, ns string, s *metav1.LabelSelector) (bool, string) {
	if m.isCritErr {
		return true, "OOMKilled"
	}
	return false, ""
}
func (m *mockResizer) ResizeStatefulSet(ctx context.Context, s *v1.StatefulSet, r *model.Recommendation) error {
	return nil
}

// Helper functions
func int32Ptr(i int32) *int32 { return &i }

// --- TEST CASE ---

func TestResizerEngine(t *testing.T) {
	tests := []struct {
		name      string
		rec       model.Recommendation
		mockSel   *mockSelector
		mockRes   *mockResizer
		expectErr bool
	}{
		{
			name: "Success Resize",
			rec:  model.Recommendation{Type: model.ReplicaSet, WorkloadName: "web"},
			mockSel: &mockSelector{
				findDeployFn: func() (*v1.Deployment, error) {
					// This is a mock deployment already "healthy" to end the polling immediately
					return &v1.Deployment{
						ObjectMeta: metav1.ObjectMeta{Name: "web", Generation: 1},
						Spec:       v1.DeploymentSpec{Replicas: int32Ptr(1)},
						Status:     v1.DeploymentStatus{UpdatedReplicas: 1, AvailableReplicas: 1, ObservedGeneration: 1},
					}, nil
				},
			},
			mockRes:   &mockResizer{resizeErr: nil, isCritErr: false},
			expectErr: false,
		},
		{
			name: "Rollback on Critical Error",
			rec:  model.Recommendation{Type: model.ReplicaSet, WorkloadName: "crash-app"},
			mockSel: &mockSelector{
				findDeployFn: func() (*v1.Deployment, error) {
					return &v1.Deployment{
						ObjectMeta: metav1.ObjectMeta{Name: "crash-app"},
						Spec:       v1.DeploymentSpec{Replicas: int32Ptr(1)},
					}, nil
				},
			},
			mockRes:   &mockResizer{resizeErr: nil, isCritErr: true}, //  Force the critical error
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := NewResizerEngine(tt.mockSel, tt.mockRes)
			err := engine.Resize(context.Background(), []model.Recommendation{tt.rec})

			if (err != nil) != tt.expectErr {
				t.Errorf("got error %v, want %v", err, tt.expectErr)
			}
		})
	}
}
