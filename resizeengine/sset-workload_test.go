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

func TestStatefulSetWorkload_FindWorkload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		rec          *model.Recommendation
		initialObjs  []runtime.Object // Oggetti presenti nel cluster fake
		wantErr      bool
		expectedType WorkloadType
	}{
		{
			name: "Success - Found StatefulSet",
			rec:  &model.Recommendation{Namespace: "prod", WorkloadName: "db", Container: "postgres"},
			initialObjs: []runtime.Object{
				&appsv1.StatefulSet{
					ObjectMeta: metav1.ObjectMeta{Name: "db", Namespace: "prod"},
					Spec:       appsv1.StatefulSetSpec{Template: corev1.PodTemplateSpec{}},
				},
			},
			wantErr:      false,
			expectedType: StatefulSet,
		},
		{
			name:        "Failure - StatefulSet Not Found",
			rec:         &model.Recommendation{Namespace: "prod", WorkloadName: "ghost"},
			initialObjs: []runtime.Object{}, // Cluster vuoto
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Inizializza il fake client con gli oggetti iniziali
			fakeClient := fake.NewSimpleClientset(tt.initialObjs...)

			w := &StatefulSetWorkload{client: fakeClient}
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
	t.Parallel()
	tests := []struct {
		name       string
		initialSts *appsv1.StatefulSet
		rec        *model.Recommendation
		wantErr    bool
	}{
		{
			name: "Success - Resize Main Container",
			initialSts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-sts", Namespace: "default"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "main-app"}},
						},
					},
				},
			},
			rec: &model.Recommendation{
				WorkloadName:                "my-sts",
				Namespace:                   "default",
				Container:                   "main-app",
				CpuRequestRecommendation:    "500m",
				MemoryRequestRecommendation: "1Gi",
			},
			wantErr: false,
		},
		{
			name: "Failure - Container Name Not Found",
			initialSts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "my-sts", Namespace: "default"},
				Spec: appsv1.StatefulSetSpec{
					Template: corev1.PodTemplateSpec{
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{{Name: "main-app"}},
						},
					},
				},
			},
			rec: &model.Recommendation{
				WorkloadName: "my-sts",
				Namespace:    "default",
				Container:    "wrong-container",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.initialSts)

			// Prepariamo l'oggetto Workload che verrebbe creato da FindWorkload
			wObj := &Workload{
				WorkloadType:     StatefulSet,
				Namespace:        tt.initialSts.Namespace,
				Name:             tt.initialSts.Name,
				Template:         &tt.initialSts.Spec.Template,
				OriginalResource: tt.initialSts,
			}

			w := &StatefulSetWorkload{client: fakeClient}
			err := w.ResizeWorkload(context.Background(), wObj, tt.rec)

			if (err != nil) != tt.wantErr {
				t.Errorf("ResizeWorkload() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestStatefulSetWorkload_GetStatus(t *testing.T) {
	t.Parallel()
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
				ObjectMeta: metav1.ObjectMeta{Name: "sts", Namespace: "default"},
				Spec:       appsv1.StatefulSetSpec{Replicas: &replicas},
				Status:     appsv1.StatefulSetStatus{UpdatedReplicas: 3, AvailableReplicas: 3},
			},
			expectedReady:   3,
			expectedDesired: 3,
		},
		{
			name: "Status - Zero Replicas (Pointer Check)",
			mockSts: &appsv1.StatefulSet{
				ObjectMeta: metav1.ObjectMeta{Name: "sts", Namespace: "default"},
				Spec:       appsv1.StatefulSetSpec{Replicas: nil},
				Status:     appsv1.StatefulSetStatus{UpdatedReplicas: 0, AvailableReplicas: 0},
			},
			expectedReady:   0,
			expectedDesired: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fakeClient := fake.NewSimpleClientset(tt.mockSts)

			w := &StatefulSetWorkload{client: fakeClient}
			workload := &Workload{
				WorkloadType: StatefulSet,
				Namespace:    "default",
				Name:         "sts",
			}
			status, err := w.GetStatus(context.Background(), workload)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if status.AvailableReplicas != tt.expectedReady {
				t.Errorf("AvailableReplicas mismatch: got %d, want %d", status.AvailableReplicas, tt.expectedReady)
			}
			if status.ExpectedReplicas != tt.expectedDesired {
				t.Errorf("Replicas mismatch: got %d, want %d", status.ExpectedReplicas, tt.expectedDesired)
			}
		})
	}
}
