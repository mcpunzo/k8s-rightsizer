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
	_, currentFile, _, _ := runtime.Caller(0)
	log.Println("--- Start Rightsizer ---")

	inputFile := filepath.Join(filepath.Dir(currentFile), "..", "data", "recommendations.xlsx")
	recFile := flag.String("file-path", inputFile, "Path to recommendations")
	dryRun := flag.Bool("dry-run", false, "Enable dry-run mode")
	flag.Parse()

	// 1. Client Initialization
	k8sClient, err := getClientset()
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

func getClientset() (re.K8sClient, error) {
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
