package resizeengine

import (
	appsv1 "k8s.io/client-go/kubernetes/typed/apps/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	policyv1 "k8s.io/client-go/kubernetes/typed/policy/v1"
)

// K8sClient is an interface that abstracts the Kubernetes client operations needed for resizing workloads.
type K8sClient interface {
	AppsV1() appsv1.AppsV1Interface
	CoreV1() corev1.CoreV1Interface
	PolicyV1() policyv1.PolicyV1Interface
}
