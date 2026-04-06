package resizeengine

import (
	"context"
	"fmt"
	"log"

	"github.com/mcpunzo/k8s-rightsizer/model"
	v1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WorkloadSelector defines the methods required for finding Kubernetes workloads (Deployments and StatefulSets) based on recommendations.
type WorkloadSelector interface {
	FindStatefulSet(ctx context.Context, rec *model.Recommendation) (*v1.StatefulSet, error)
	FindDeployment(ctx context.Context, rec *model.Recommendation) (*v1.Deployment, error)
}

// K8sWorkloadSelector is responsible for finding Kubernetes workloads (Deployments and StatefulSets) based on recommendations.
type K8sWorkloadSelector struct {
	client K8sClient
}

// NewK8sWorkloadSelector creates a new K8sWorkloadSelector.
// It accepts the K8sClient interface which is satisfied by the standard kubernetes.Clientset.
// param client: The Kubernetes client used for interacting with the cluster.
// returns: A new instance of K8sWorkloadSelector.
func NewK8sWorkloadSelector(client K8sClient) *K8sWorkloadSelector {
	return &K8sWorkloadSelector{
		client: client,
	}
}

// FindStatefulSet retrieves a StatefulSet based on the provided recommendation.
// It first attempts to find the StatefulSet by name. If the name is not provided and deepResize is enabled in the context,
// it will search for a StatefulSet in the specified namespace that contains a container matching the recommendation's container name.
// Returns an error if the StatefulSet cannot be found.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the target namespace, StatefulSet name (optional), and container name.
// returns: A pointer to the found StatefulSet or an error if not found.
func (s *K8sWorkloadSelector) FindStatefulSet(ctx context.Context, rec *model.Recommendation) (*v1.StatefulSet, error) {
	if rec.WorkloadName != "" {
		statefulSet, err := s.client.AppsV1().StatefulSets(rec.Namespace).Get(ctx, rec.WorkloadName, metav1.GetOptions{})
		if err != nil {
			log.Printf("failed to get statefulset for %s: %v\n", rec, err)
			return nil, err
		}
		return statefulSet, nil
	} else if deepResize, ok := ctx.Value("deepResize").(bool); ok && deepResize {
		log.Println("StatefulSet name is not present -> find a statefulset by namespace and container name...")

		list, err := s.client.AppsV1().StatefulSets(rec.Namespace).List(ctx, metav1.ListOptions{})

		if err != nil {
			log.Printf("failed to find statefulset for %s: %v\n", rec, err)
			return nil, err
		}

		// Iterate through statefulsets to find a match based on container name
		for _, ss := range list.Items {
			for _, c := range ss.Spec.Template.Spec.Containers {
				if c.Name == rec.Container {
					log.Printf("Found statefulset %s for %s\n", ss.Name, rec)
					return &ss, nil
				}
			}
		}

	}

	return nil, fmt.Errorf("statefulset not found for %s", rec)
}

// FindDeployment retrieves a Deployment based on the provided recommendation.
// It first attempts to find the Deployment by name. If the name is not provided and deepResize is enabled in the context,
// it will search for a Deployment in the specified namespace that contains a container matching the recommendation's container name.
// Returns an error if the Deployment cannot be found.
// param ctx: The context for managing request deadlines and cancellation.
// param rec: The Recommendation containing the target namespace, Deployment name (optional), and container name.
// returns: A pointer to the found Deployment or an error if not found.
func (s *K8sWorkloadSelector) FindDeployment(ctx context.Context, rec *model.Recommendation) (*v1.Deployment, error) {
	if rec.WorkloadName != "" {
		deployment, err := s.client.AppsV1().Deployments(rec.Namespace).Get(ctx, rec.WorkloadName, metav1.GetOptions{})
		if err != nil {
			log.Printf("failed to get deployment for %s: %v\n", rec, err)
			return nil, err
		}
		return deployment, nil

	} else if deepResize, ok := ctx.Value("deepResize").(bool); ok && deepResize {
		log.Println("Deployment name is not present -> find a deployment by namespace and container name...")

		list, err := s.client.AppsV1().Deployments(rec.Namespace).List(ctx, metav1.ListOptions{})

		if err != nil {
			log.Printf("failed to find deployment for %s: %v\n", rec, err)
			return nil, err
		}

		// Iterate through deployments to find a match based on container name
		for _, deploy := range list.Items {
			for _, c := range deploy.Spec.Template.Spec.Containers {
				if c.Name == rec.Container {
					log.Printf("Found deployment %s for %s\n", deploy.Name, rec)
					return &deploy, nil
				}
			}
		}

	}
	return nil, fmt.Errorf("deployment not found for %s", rec)

}
