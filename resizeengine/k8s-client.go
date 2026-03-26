package resizeengine

import (
	v1 "k8s.io/client-go/kubernetes/typed/apps/v1"
)

// K8sClient is an interface that abstracts the Kubernetes client operations needed for resizing workloads.
type K8sClient interface {
	AppsV1() v1.AppsV1Interface
}
