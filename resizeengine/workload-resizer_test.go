package resizeengine

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/model"
	k8s "github.com/mcpunzo/k8s-rightsizer/resizeengine/internal/k8s"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

// --- helpers ---

func wrRec(ns, workload, container, kind string) model.Recommendation {
	return model.Recommendation{
		Namespace:                   ns,
		WorkloadName:                workload,
		Container:                   container,
		Kind:                        model.Kind(kind),
		CpuRequestRecommendation:    "200m",
		MemoryRequestRecommendation: "256Mi",
	}
}

func wrDeployment(name, ns string, containers []corev1.Container) *appsv1.Deployment {
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
		Spec: appsv1.DeploymentSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{Containers: containers},
			},
		},
	}
}

func containerWithLimits(name, cpuReq, memReq, cpuLim, memLim string) corev1.Container {
	return corev1.Container{
		Name: name,
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpuReq),
				corev1.ResourceMemory: resource.MustParse(memReq),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse(cpuLim),
				corev1.ResourceMemory: resource.MustParse(memLim),
			},
		},
	}
}

func wrReadyNode(name, arch string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"kubernetes.io/arch":               arch,
				"node.kubernetes.io/instance-type": "c5.x86",
			},
		},
		Status: corev1.NodeStatus{Conditions: []corev1.NodeCondition{{
			Type:   corev1.NodeReady,
			Status: corev1.ConditionTrue,
		}}},
	}
}

func newTestWorkloadResizer(objs ...runtime.Object) *WorkloadResizer {
	return NewWorkloadResizer(fake.NewSimpleClientset(objs...), watcher.NewResizeWatcher())
}

// --- TestWorkloadResizer_ArrangeRecsByWorkload ---

func TestWorkloadResizer_ArrangeRecsByWorkload(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		recs       []model.Recommendation
		wantCounts map[string]int
	}{
		{
			name: "Single workload single container",
			recs: []model.Recommendation{wrRec("default", "api", "app", "Deployment")},
			wantCounts: map[string]int{
				"-default-Deployment-api": 1,
			},
		},
		{
			name: "Single workload multiple containers grouped together",
			recs: []model.Recommendation{
				wrRec("default", "api", "app", "Deployment"),
				wrRec("default", "api", "sidecar", "Deployment"),
			},
			wantCounts: map[string]int{
				"-default-Deployment-api": 2,
			},
		},
		{
			name: "Multiple distinct workloads",
			recs: []model.Recommendation{
				wrRec("default", "api", "app", "Deployment"),
				wrRec("default", "db", "postgres", "StatefulSet"),
			},
			wantCounts: map[string]int{
				"-default-Deployment-api": 1,
				"-default-StatefulSet-db": 1,
			},
		},
		{
			name:       "Empty recommendation list",
			recs:       []model.Recommendation{},
			wantCounts: map[string]int{},
		},
		{
			name: "Same workload name but different namespaces are distinct",
			recs: []model.Recommendation{
				wrRec("ns-a", "api", "app", "Deployment"),
				wrRec("ns-b", "api", "app", "Deployment"),
			},
			wantCounts: map[string]int{
				"-ns-a-Deployment-api": 1,
				"-ns-b-Deployment-api": 1,
			},
		},
		{
			name: "Same workload name but different kinds are distinct",
			recs: []model.Recommendation{
				wrRec("default", "api", "app", "Deployment"),
				wrRec("default", "api", "app", "StatefulSet"),
			},
			wantCounts: map[string]int{
				"-default-Deployment-api":  1,
				"-default-StatefulSet-api": 1,
			},
		},
		{
			name: "Same workload with different environments are distinct",
			recs: []model.Recommendation{
				{Environment: "dev", Namespace: "default", WorkloadName: "api", Container: "app", Kind: model.Deployment},
				{Environment: "prod", Namespace: "default", WorkloadName: "api", Container: "app", Kind: model.Deployment},
			},
			wantCounts: map[string]int{
				"dev-default-Deployment-api":  1,
				"prod-default-Deployment-api": 1,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestWorkloadResizer()
			got := r.arrangeRecsByWorkload(tt.recs)

			if len(got) != len(tt.wantCounts) {
				t.Fatalf("arrangeRecsByWorkload() len = %d, want %d (keys: %v)", len(got), len(tt.wantCounts), keys(got))
			}

			for key, wantCount := range tt.wantCounts {
				recs, ok := got[key]
				if !ok {
					t.Errorf("expected key %q not found; got keys: %v", key, keys(got))
					continue
				}
				if len(recs) != wantCount {
					t.Errorf("key %q: got %d recommendations, want %d", key, len(recs), wantCount)
				}
			}
		})
	}
}

// --- TestWorkloadResizer_ValidateRecommendation ---

func TestWorkloadResizer_ValidateRecommendation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		containers []corev1.Container
		recs       []*model.Recommendation
		wantCount  int
	}{
		{
			name:       "All recommendations valid",
			containers: []corev1.Container{containerWithLimits("app", "100m", "128Mi", "2", "2Gi"), containerWithLimits("sidecar", "50m", "64Mi", "1", "1Gi")},
			recs: []*model.Recommendation{
				{WorkloadName: "api", Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"},
				{WorkloadName: "api", Container: "sidecar", CpuRequestRecommendation: "100m", MemoryRequestRecommendation: "128Mi"},
			},
			wantCount: 2,
		},
		{
			name:       "One container not found in template",
			containers: []corev1.Container{containerWithLimits("app", "100m", "128Mi", "2", "2Gi")},
			recs: []*model.Recommendation{
				{WorkloadName: "api", Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"},
				{WorkloadName: "api", Container: "ghost", CpuRequestRecommendation: "50m", MemoryRequestRecommendation: "64Mi"},
			},
			wantCount: 1,
		},
		{
			name:       "Recommendation matches current requests - filtered out as no-op",
			containers: []corev1.Container{containerWithLimits("app", "200m", "256Mi", "2", "2Gi")},
			recs: []*model.Recommendation{
				{WorkloadName: "api", Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"},
			},
			wantCount: 0,
		},
		{
			name:       "CPU recommendation exceeds container limit",
			containers: []corev1.Container{containerWithLimits("app", "100m", "128Mi", "500m", "1Gi")},
			recs: []*model.Recommendation{
				{WorkloadName: "api", Container: "app", CpuRequestRecommendation: "4", MemoryRequestRecommendation: "256Mi"},
			},
			wantCount: 0,
		},
		{
			name:       "Memory recommendation exceeds container limit",
			containers: []corev1.Container{containerWithLimits("app", "100m", "128Mi", "2", "512Mi")},
			recs: []*model.Recommendation{
				{WorkloadName: "api", Container: "app", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "2Gi"},
			},
			wantCount: 0,
		},
		{
			name:       "Empty recommendations list",
			containers: []corev1.Container{containerWithLimits("app", "100m", "128Mi", "2", "2Gi")},
			recs:       []*model.Recommendation{},
			wantCount:  0,
		},
		{
			name:       "Mixed valid and invalid",
			containers: []corev1.Container{containerWithLimits("app", "100m", "128Mi", "2", "2Gi"), containerWithLimits("sidecar", "200m", "256Mi", "2", "2Gi")},
			recs: []*model.Recommendation{
				{WorkloadName: "api", Container: "app", CpuRequestRecommendation: "300m", MemoryRequestRecommendation: "512Mi"},
				{WorkloadName: "api", Container: "sidecar", CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"}, // matches current
				{WorkloadName: "api", Container: "ghost", CpuRequestRecommendation: "50m", MemoryRequestRecommendation: "64Mi"},     // not found
			},
			wantCount: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newTestWorkloadResizer()

			workload := &k8s.Workload{
				Name: "api",
				Template: &corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{Containers: tt.containers},
				},
			}

			got := r.validateRecommendation(context.Background(), workload, tt.recs)

			if len(got) != tt.wantCount {
				t.Errorf("validateRecommendation() len = %d, want %d", len(got), tt.wantCount)
			}
		})
	}
}

// --- TestWorkloadResizer_ResizeJob ---

func TestWorkloadResizer_ResizeJob(t *testing.T) {
	t.Parallel()
	originalInterval := WorkloadCheckInterval
	originalDepTimeout := DeploymentCheckTimeout
	originalStsTimeout := StatefulsetCheckTimeout
	originalDelay := InterRecommendationDelay
	originalWindow := NodeCompatibilityRecheckWindow
	originalPoll := NodeCompatibilityRecheckPollInterval
	WorkloadCheckInterval = 10 * time.Millisecond
	DeploymentCheckTimeout = 120 * time.Millisecond
	StatefulsetCheckTimeout = 120 * time.Millisecond
	InterRecommendationDelay = 1 * time.Millisecond
	NodeCompatibilityRecheckWindow = 20 * time.Millisecond
	NodeCompatibilityRecheckPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		WorkloadCheckInterval = originalInterval
		DeploymentCheckTimeout = originalDepTimeout
		StatefulsetCheckTimeout = originalStsTimeout
		InterRecommendationDelay = originalDelay
		NodeCompatibilityRecheckWindow = originalWindow
		NodeCompatibilityRecheckPollInterval = originalPoll
	})

	tests := []struct {
		name        string
		jobs        [][]*model.Recommendation
		k8sObjs     []runtime.Object
		cancelCtx   bool
		wantResults []string
	}{
		{
			name: "Success - deployment resized",
			jobs: [][]*model.Recommendation{
				{{Namespace: "prod", WorkloadName: "api", Container: "app", Kind: "Deployment",
					CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"}},
			},
			k8sObjs: []runtime.Object{
				wrDeployment("api", "prod", []corev1.Container{
					containerWithLimits("app", "100m", "128Mi", "1", "1Gi"),
				}),
				wrReadyNode("node-1", "amd64"),
			},
			wantResults: []string{"[OK]"},
		},
		{
			name: "Failure - unsupported kind skipped",
			jobs: [][]*model.Recommendation{
				{{Namespace: "default", WorkloadName: "job-1", Container: "worker", Kind: "CronJob",
					CpuRequestRecommendation: "100m", MemoryRequestRecommendation: "128Mi"}},
			},
			k8sObjs:     []runtime.Object{},
			wantResults: []string{"[SKIP]"},
		},
		{
			name: "Failure - workload not found in cluster",
			jobs: [][]*model.Recommendation{
				{{Namespace: "default", WorkloadName: "missing", Container: "app", Kind: "Deployment",
					CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"}},
			},
			k8sObjs:     []runtime.Object{},
			wantResults: []string{"[SKIP]"},
		},
		{
			name: "Failure - all recommendations invalid (no valid recs after validation)",
			jobs: [][]*model.Recommendation{
				{{Namespace: "default", WorkloadName: "api", Container: "ghost-container", Kind: "Deployment",
					CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"}},
			},
			k8sObjs: []runtime.Object{
				wrDeployment("api", "default", []corev1.Container{
					containerWithLimits("app", "100m", "128Mi", "1", "1Gi"),
				}),
			},
			wantResults: []string{"[SKIP]"},
		},
		{
			name: "Success - multiple jobs processed",
			jobs: [][]*model.Recommendation{
				{{Namespace: "default", WorkloadName: "api", Container: "app", Kind: "Deployment",
					CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"}},
				{{Namespace: "default", WorkloadName: "worker", Container: "job", Kind: "Deployment",
					CpuRequestRecommendation: "300m", MemoryRequestRecommendation: "512Mi"}},
			},
			k8sObjs: []runtime.Object{
				wrDeployment("api", "default", []corev1.Container{containerWithLimits("app", "100m", "128Mi", "1", "1Gi")}),
				wrDeployment("worker", "default", []corev1.Container{containerWithLimits("job", "100m", "128Mi", "1", "1Gi")}),
				wrReadyNode("node-1", "amd64"),
			},
			wantResults: []string{"[OK]", "[OK]"},
		},
		{
			name: "Context canceled - no results produced",
			jobs: [][]*model.Recommendation{
				{{Namespace: "default", WorkloadName: "api", Container: "app", Kind: "Deployment",
					CpuRequestRecommendation: "200m", MemoryRequestRecommendation: "256Mi"}},
			},
			k8sObjs:     []runtime.Object{},
			cancelCtx:   true,
			wantResults: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fakeClient := fake.NewSimpleClientset(tt.k8sObjs...)
			r := NewWorkloadResizer(fakeClient, watcher.NewResizeWatcher())

			ctx := context.Background()
			if tt.cancelCtx {
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}

			jobs := make(chan []*model.Recommendation, len(tt.jobs))
			results := make(chan string, len(tt.jobs)+2)

			for _, job := range tt.jobs {
				jobs <- job
			}
			close(jobs)

			r.ResizeJob(ctx, jobs, results)
			close(results)

			var got []string
			for res := range results {
				got = append(got, res)
			}

			if len(got) != len(tt.wantResults) {
				t.Fatalf("ResizeJob() got %d results %v, want %d %v", len(got), got, len(tt.wantResults), tt.wantResults)
			}
			for i, want := range tt.wantResults {
				if !strings.Contains(got[i], want) {
					t.Errorf("result[%d] = %q, want to contain %q", i, got[i], want)
				}
			}
		})
	}
}

// --- TestWorkloadResizer_Resize ---

func TestWorkloadResizer_Resize(t *testing.T) {
	t.Parallel()
	originalInterval := WorkloadCheckInterval
	originalDepTimeout := DeploymentCheckTimeout
	originalStsTimeout := StatefulsetCheckTimeout
	originalDelay := InterRecommendationDelay
	originalWindow := NodeCompatibilityRecheckWindow
	originalPoll := NodeCompatibilityRecheckPollInterval
	WorkloadCheckInterval = 10 * time.Millisecond
	DeploymentCheckTimeout = 120 * time.Millisecond
	StatefulsetCheckTimeout = 120 * time.Millisecond
	InterRecommendationDelay = 1 * time.Millisecond
	NodeCompatibilityRecheckWindow = 20 * time.Millisecond
	NodeCompatibilityRecheckPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		WorkloadCheckInterval = originalInterval
		DeploymentCheckTimeout = originalDepTimeout
		StatefulsetCheckTimeout = originalStsTimeout
		InterRecommendationDelay = originalDelay
		NodeCompatibilityRecheckWindow = originalWindow
		NodeCompatibilityRecheckPollInterval = originalPoll
	})

	tests := []struct {
		name    string
		recs    []model.Recommendation
		k8sObjs []runtime.Object
		workers int
		wantErr bool
	}{
		{
			name: "Success - single workload resized",
			recs: []model.Recommendation{wrRec("prod", "api", "app", "Deployment")},
			k8sObjs: []runtime.Object{
				wrDeployment("api", "prod", []corev1.Container{containerWithLimits("app", "100m", "128Mi", "1", "1Gi")}),
				wrReadyNode("node-1", "amd64"),
			},
			workers: 1,
			wantErr: false,
		},
		{
			name: "Success - two workloads with 2 parallel workers",
			recs: []model.Recommendation{
				wrRec("prod", "api", "app", "Deployment"),
				wrRec("prod", "worker", "job", "Deployment"),
			},
			k8sObjs: []runtime.Object{
				wrDeployment("api", "prod", []corev1.Container{containerWithLimits("app", "100m", "128Mi", "1", "1Gi")}),
				wrDeployment("worker", "prod", []corev1.Container{containerWithLimits("job", "100m", "128Mi", "1", "1Gi")}),
				wrReadyNode("node-1", "amd64"),
			},
			workers: 2,
			wantErr: false,
		},
		{
			name:    "Success - empty recommendations list",
			recs:    []model.Recommendation{},
			k8sObjs: []runtime.Object{},
			workers: 1,
			wantErr: false,
		},
		{
			name:    "Success - zero workers with empty recommendations",
			recs:    []model.Recommendation{},
			k8sObjs: []runtime.Object{},
			workers: 0,
			wantErr: false,
		},
		{
			name: "Success - unsupported kind skipped without error",
			recs: []model.Recommendation{
				{Namespace: "default", WorkloadName: "cj", Container: "app", Kind: "CronJob",
					CpuRequestRecommendation: "100m", MemoryRequestRecommendation: "128Mi"},
			},
			k8sObjs: []runtime.Object{},
			workers: 1,
			wantErr: false,
		},
		{
			name: "Success - multiple containers in same workload grouped and resized together",
			recs: []model.Recommendation{
				wrRec("default", "api", "app", "Deployment"),
				wrRec("default", "api", "sidecar", "Deployment"),
			},
			k8sObjs: []runtime.Object{
				wrDeployment("api", "default", []corev1.Container{
					containerWithLimits("app", "100m", "128Mi", "1", "1Gi"),
					containerWithLimits("sidecar", "50m", "64Mi", "500m", "512Mi"),
				}),
				wrReadyNode("node-1", "amd64"),
			},
			workers: 1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fakeClient := fake.NewSimpleClientset(tt.k8sObjs...)
			r := NewWorkloadResizer(fakeClient, watcher.NewResizeWatcher())

			err := r.Resize(context.Background(), tt.recs, tt.workers)

			if (err != nil) != tt.wantErr {
				t.Fatalf("Resize() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// --- utility ---

func keys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
