package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/recommendation/reader"
	re "github.com/mcpunzo/k8s-rightsizer/resizeengine"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
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
	resizeStrategy := flag.String("resize-strategy", "container", "Resize strategy to use (default: container, options: container, workload)")
	useLimits := flag.Bool("use-limits", false, "Use resource limits instead of requests for resizing (default: false)")
	flag.Parse()

	checkRequiredFlags(*recFile, *resizeStrategy, *numberOfWorkers)

	log.Printf("DryRun: %v", *dryRun)
	log.Printf("Recommendations file: %v", *recFile)
	log.Printf("Resize on Recreate: %v", *resizeOnRecreate)
	log.Printf("Number of workers: %v", *numberOfWorkers)
	log.Printf("Resize strategy: %v", *resizeStrategy)
	log.Printf("Use limits: %v", *useLimits)

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
	resizeWatcher := watcher.NewResizeWatcher()

	var resizer re.Resizer
	switch *resizeStrategy {
	case "container":
		resizer = re.NewContainerResizer(k8sClient, resizeWatcher)
	case "workload":
		resizer = re.NewWorkloadResizer(k8sClient, resizeWatcher)
	default:
		log.Fatalf("Invalid resize strategy: %v", *resizeStrategy)
	}

	rightsizer := re.NewWorkloadRightsizer(resizer)

	ctx := ctxkeys.WithDryRun(context.Background(), *dryRun)
	ctx = ctxkeys.WithResizeOnRecreate(ctx, *resizeOnRecreate)
	ctx = ctxkeys.WithNumberOfWorkers(ctx, *numberOfWorkers)
	ctx = ctxkeys.WithUseLimits(ctx, *useLimits)
	if err := rightsizer.Rightsize(ctx, recs); err != nil {
		log.Printf("Resize process completed with some issues: %v", err)
	}

	log.Println("--- Rightsizer Complete ---")

}

func checkRequiredFlags(recFile, resizeStrategy string, numberOfWorkers int) {
	if recFile == "" {
		log.Fatal("file-path is required")
	}

	if numberOfWorkers <= 0 {
		log.Fatal("workers parameter must be greater than 0")
	}
	if resizeStrategy != "container" && resizeStrategy != "workload" {
		log.Fatal("resize-strategy must be either 'container' or 'workload'")
	}
}

func getClientset() (*kubernetes.Clientset, error) {
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
