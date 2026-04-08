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

func TestStatefulSetWorkload_FindWorkload(t *testing.T) {
	tests := []struct {
		name         string
		rec          *model.Recommendation
		mockSts      *appsv1.StatefulSet
		mockErr      error
		wantErr      bool
		expectedType WorkloadType
	}{
		{
			name: "Success - Found StatefulSet",
			rec:  &model.Recommendation{Namespace: "prod", WorkloadName: "db", Container: "postgres"},
			mockSts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
				Spec:       appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{}},
			},
			mockErr:      nil,
			wantErr:      false,
			expectedType: StatefulSet,
		},
		{
			name:    "Failure - StatefulSet Not Found",
			rec:     &model.Recommendation{Namespace: "prod", WorkloadName: "ghost"},
			mockSts: nil,
			mockErr: errors.New("statefulsets.apps \"ghost\" not found"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup mock chain
			mSts := &mockStsClient{getFunc: func() (*appsv1.StatefulSet, error) { return tt.mockSts, tt.mockErr }}
			mApps := &mockAppsV1{stsClient: mSts}
			mClient := &mockK8sClient{appsV1: mApps}

			w := &StatefulSetWorkload{client: mClient}
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

func TestStatefulSetWorkload_ResizeWorkload(t *testing.T) {
	// Template di base per i test di resize
	baseSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: "my-sts", Namespace: "default"},
		Spec: appsv1.StatefulSetSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "main-app"}},
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
			name: "Success - Resize Main Container",
			workload: &Workload{
				WorkloadType:     StatefulSet,
				Template:         &baseSts.Spec.Template,
				originalResource: baseSts,
			},
			rec: &model.Recommendation{
				Container:                   "main-app",
				CpuRequestRecommendation:    "500m",
				MemoryRequestRecommendation: "1Gi",
			},
			wantErr: false,
		},
		{
			name: "Failure - Container Name Not Found",
			workload: &Workload{
				WorkloadType:     StatefulSet,
				Template:         &baseSts.Spec.Template,
				originalResource: baseSts,
			},
			rec: &model.Recommendation{
				Container: "wrong-container",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mSts := &mockStsClient{
				updateFunc: func(s *appsv1.StatefulSet) (*appsv1.StatefulSet, error) { return s, nil },
			}
			mApps := &mockAppsV1{stsClient: mSts}
			mClient := &mockK8sClient{appsV1: mApps}

			w := &StatefulSetWorkload{client: mClient}
			err := w.ResizeWorkload(context.Background(), tt.workload, tt.rec)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStatefulSetWorkload_GetStatus(t *testing.T) {
	replicas := int32(3)

	tests := []struct {
		name            string
		mockSts         *appsv1.StatefulSet
		expectedReady   int32
		expectedDesired int32
	}{
		{
			name: "Status - Fully Ready",
			mockSts: &appsv1.StatefulSet{
				Spec:   appsv1.StatefulSetSpec{Replicas: &replicas},
				Status: appsv1.StatefulSetStatus{UpdatedReplicas: 3, AvailableReplicas: 3},
			},
			expectedReady:   3,
			expectedDesired: 3,
		},
		{
			name: "Status - Zero Replicas (Pointer Check)",
			mockSts: &appsv1.StatefulSet{
				Spec:   appsv1.StatefulSetSpec{Replicas: nil}, // Simuliamo puntatore nil
				Status: appsv1.StatefulSetStatus{UpdatedReplicas: 0, AvailableReplicas: 0},
			},
			expectedReady:   0,
			expectedDesired: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mSts := &mockStsClient{getFunc: func() (*appsv1.StatefulSet, error) { return tt.mockSts, nil }}
			mApps := &mockAppsV1{stsClient: mSts}
			mClient := &mockK8sClient{appsV1: mApps}

			w := &StatefulSetWorkload{client: mClient}
			status, err := w.GetStatus(context.Background(), "default", "sts")

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if status.AvailableReplicas != tt.expectedReady {
				t.Errorf("AvailableReplicas mismatch: got %d, want %d", status.AvailableReplicas, tt.expectedReady)
			}
			if status.Replicas != tt.expectedDesired {
				t.Errorf("Replicas mismatch: got %d, want %d", status.Replicas, tt.expectedDesired)
			}
		})
	}
}
