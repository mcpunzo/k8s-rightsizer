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
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		fmt.Println("Error resolving executable path")
		return
	}

	log.Println("--- Start Rightsizer  ---")

	inputFile := filepath.Join(filepath.Dir(currentFile), "..", "data", "test.xlsx")
	recFile := flag.String("file-path", inputFile, "Recommendation file path (xlsx, xsl")
	deepResize := flag.Bool("deep-resize", false, "Enable finding deployment from namespace and container name if not present")

	flag.Parse()

	log.Printf("Recommendation File target: %s", *recFile)
	recReader, err := reader.NewReader(*recFile)
	if err != nil {
		log.Fatalf("Error creating reader: %v", err)
	}

	clientset, err := getClientset()
	if err != nil {
		log.Fatalf("Error creating Kubernetes client: %v", err)
	}

	recs, err := recReader.Read()
	if err != nil {
		log.Fatalf("Error reading recommendations: %v", err)
	}

	engine := resizeEngineBuolder(clientset)
	ctx := context.Background()
	ctx = context.WithValue(ctx, "deepResize", *deepResize)
	engine.Resize(ctx, recs)

	log.Println("--- Rightsizer Complete ---")
}

func getClientset() (*kubernetes.Clientset, error) {
	// Try the in-cluster configuration first (for the Job)
	config, err := rest.InClusterConfig()
	if err != nil {
		// If it fails, try to use the local kubeconfig (for testing from PC)
		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, err
		}
	}

	return kubernetes.NewForConfig(config)
}

// resizeEngineBuolder is a helper function to create a new ResizerEngine instance with the provided Kubernetes client.
// It initializes the WorkloadSelector and WorkloadResizer with the same client to ensure consistent interactions with the Kubernetes cluster.
// param client: The Kubernetes client used for interacting with the cluster.
// returns: A new instance of ResizerEngine.
func resizeEngineBuolder(client re.K8sClient) *re.ResizerEngine {
	selector := re.NewWorkloadSelector(client)
	resizer := re.NewWorkloadResizer(client)
	return re.NewResizerEngine(selector, resizer)
}
