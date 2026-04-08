package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"runtime"

	"github.com/mcpunzo/k8s-rightsizer/recommendation/reader"
	re "github.com/mcpunzo/k8s-rightsizer/resizeengine"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1typed "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

// --- MOCK IMPLEMENTATION (Per evitare il Panic) ---

type LocalDryRunClient struct{}

func (m *LocalDryRunClient) AppsV1() v1.AppsV1Interface          { return &mockAppsV1{} }
func (m *LocalDryRunClient) CoreV1() corev1typed.CoreV1Interface { return &mockCoreV1{} }

type mockAppsV1 struct{ v1.AppsV1Interface }

func (m *mockAppsV1) Deployments(ns string) v1.DeploymentInterface   { return &mockDeploy{ns: ns} }
func (m *mockAppsV1) StatefulSets(ns string) v1.StatefulSetInterface { return &mockSts{ns: ns} }

type mockCoreV1 struct{ corev1typed.CoreV1Interface }

func (m *mockCoreV1) Pods(ns string) corev1typed.PodInterface { return &mockPods{ns: ns} }

// Mock Deployments
type mockDeploy struct {
	v1.DeploymentInterface
	ns string
}

func (m *mockDeploy) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error) {
	log.Printf("[DRY-RUN] GET Deployment: %s/%s", m.ns, name)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.ns, Generation: 1},
		Spec:       appsv1.DeploymentSpec{Replicas: int32Ptr(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}},
		Status:     appsv1.DeploymentStatus{AvailableReplicas: 1, UpdatedReplicas: 1, ObservedGeneration: 1},
	}, nil
}
func (m *mockDeploy) Update(ctx context.Context, d *appsv1.Deployment, opts metav1.UpdateOptions) (*appsv1.Deployment, error) {
	log.Printf("[DRY-RUN] UPDATE Deployment: %s", d.Name)
	return d, nil
}

// Mock StatefulSets
type mockSts struct {
	v1.StatefulSetInterface
	ns string
}

func (m *mockSts) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.StatefulSet, error) {
	log.Printf("[DRY-RUN] GET StatefulSet: %s/%s", m.ns, name)
	return &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: m.ns, Generation: 1},
		Spec:       appsv1.StatefulSetSpec{Replicas: int32Ptr(1), Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": name}}},
		Status:     appsv1.StatefulSetStatus{AvailableReplicas: 1, UpdatedReplicas: 1, ObservedGeneration: 1},
	}, nil
}
func (m *mockSts) Update(ctx context.Context, s *appsv1.StatefulSet, opts metav1.UpdateOptions) (*appsv1.StatefulSet, error) {
	log.Printf("[DRY-RUN] UPDATE StatefulSet: %s", s.Name)
	return s, nil
}

// Mock Pods
type mockPods struct {
	corev1typed.PodInterface
	ns string
}

func (m *mockPods) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
	return &corev1.PodList{Items: []corev1.Pod{}}, nil
}

func int32Ptr(i int32) *int32 { return &i }

// --- MAIN LOGIC ---

func main() {
	_, currentFile, _, _ := runtime.Caller(0)
	log.Println("--- Start Rightsizer ---")

	inputFile := filepath.Join(filepath.Dir(currentFile), "..", "data", "recommendations.xlsx")
	recFile := flag.String("file-path", inputFile, "Path to recommendations")
	dryRun := flag.Bool("dry-run", false, "Enable dry-run mode")
	flag.Parse()

	// 1. Client Initialization
	k8sClient, err := getClientset(*dryRun)
	if err != nil {
		log.Fatalf("Fatal: %v", err)
	}

	// 2. Reading Recommendations
	recReader, err := reader.NewReader(*recFile)
	if err != nil {
		log.Fatalf("Error reader: %v", err)
	}
	recs, err := recReader.Read()
	if err != nil {
		log.Fatalf("Error reading: %v", err)
	}

	// 3. Execute Engine
	engine := re.NewWorkloadResizer(k8sClient)
	ctx := context.WithValue(context.Background(), "dryRun", *dryRun)

	if err := engine.Resize(ctx, recs); err != nil {
		log.Printf("Resize process completed with some issues: %v", err)
	}

	log.Println("--- Rightsizer Complete ---")
}

func getClientset(dryRun bool) (re.K8sClient, error) {
	if dryRun {
		return &LocalDryRunClient{}, nil
	}

	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("kubeconfig not found: %w", err)
		}
	}
	return kubernetes.NewForConfig(config)
}
