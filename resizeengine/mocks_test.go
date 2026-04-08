package resizeengine

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1typed "k8s.io/client-go/kubernetes/typed/core/v1"
)

// --- MOCK GLOBALI CONDIVISI ---

type mockK8sClient struct {
	appsV1 v1.AppsV1Interface
	coreV1 corev1typed.CoreV1Interface
}

func (m *mockK8sClient) AppsV1() v1.AppsV1Interface          { return m.appsV1 }
func (m *mockK8sClient) CoreV1() corev1typed.CoreV1Interface { return m.coreV1 }

// Mock per il gruppo AppsV1
type mockAppsV1 struct {
	v1.AppsV1Interface
	deployClient v1.DeploymentInterface
	stsClient    v1.StatefulSetInterface
}

func (m *mockAppsV1) Deployments(ns string) v1.DeploymentInterface   { return m.deployClient }
func (m *mockAppsV1) StatefulSets(ns string) v1.StatefulSetInterface { return m.stsClient }

// Mock per le risorse Deployment
type mockDeployClient struct {
	v1.DeploymentInterface
	getFunc    func() (*appsv1.Deployment, error)
	updateFunc func(*appsv1.Deployment) (*appsv1.Deployment, error)
}

func (m *mockDeployClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.Deployment, error) {
	return m.getFunc()
}
func (m *mockDeployClient) Update(ctx context.Context, d *appsv1.Deployment, opts metav1.UpdateOptions) (*appsv1.Deployment, error) {
	return m.updateFunc(d)
}

// Mock per le risorse StatefulSet
type mockStsClient struct {
	v1.StatefulSetInterface
	getFunc    func() (*appsv1.StatefulSet, error)
	updateFunc func(*appsv1.StatefulSet) (*appsv1.StatefulSet, error)
}

func (m *mockStsClient) Get(ctx context.Context, name string, opts metav1.GetOptions) (*appsv1.StatefulSet, error) {
	return m.getFunc()
}
func (m *mockStsClient) Update(ctx context.Context, sts *appsv1.StatefulSet, opts metav1.UpdateOptions) (*appsv1.StatefulSet, error) {
	return m.updateFunc(sts)
}

// Mock for pod resources
type mockPodClient struct {
	corev1typed.PodInterface
	listFunc func() (*corev1.PodList, error)
}

func (m *mockPodClient) List(ctx context.Context, opts metav1.ListOptions) (*corev1.PodList, error) {
	return m.listFunc()
}

type mockCoreV1 struct {
	corev1typed.CoreV1Interface
	podClient corev1typed.PodInterface
}

func (m *mockCoreV1) Pods(ns string) corev1typed.PodInterface {
	return m.podClient
}
