package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mcpunzo/k8s-rightsizer/recommendation/reader"
	re "github.com/mcpunzo/k8s-rightsizer/resizeengine"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	log.Println("--- Start Rightsizer ---")

	recFile := flag.String("file-path", "", "Path to recommendations")
	dryRun := flag.Bool("dry-run", false, "Enable dry-run mode")
	resizeOnRecreate := flag.Bool("resize-on-recreate", false, "Allow resizing if the workload update strategy is Recreate (default: false)")
	numberOfWorkers := flag.Int("workers", 1, "Number of concurrent workers for processing recommendations")
	flag.Parse()

	if *numberOfWorkers <= 0 {
		log.Fatal("workers parameters must be greater than 0")
	}

	log.Printf("DryRun: %v", *dryRun)
	log.Printf("Recommendations file: %v", *recFile)
	log.Printf("Resize on Recreate: %v", *resizeOnRecreate)
	log.Printf("Number of workers: %v", *numberOfWorkers)

	

	fileInfo, err := os.Stat(*recFile)
	if err != nil {
		log.Fatalf("Error in file stats: %v", err)
	}
	log.Printf("File found! Size: %d bytes", fileInfo.Size())

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

	log.Printf("Recommendations read: %v", len(recs))

	// 3. Execute Engine
	engine := re.NewWorkloadResizer(k8sClient)
	ctx := context.WithValue(context.Background(), "dryRun", *dryRun)
	ctx = context.WithValue(ctx, "resizeOnRecreate", *resizeOnRecreate)
	ctx = context.WithValue(ctx, "numberOfWorkers", *numberOfWorkers)

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
