package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/mcpunzo/k8s-rightsizer/ctxkeys"
	"github.com/mcpunzo/k8s-rightsizer/recommendation/reader"
	re "github.com/mcpunzo/k8s-rightsizer/resizeengine"
	"github.com/mcpunzo/k8s-rightsizer/watcher"
	"github.com/mcpunzo/k8s-rightsizer/watcher/listner"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
)

func main() {
	recFile := flag.String("file-path", "", "Path to recommendations")
	dryRun := flag.Bool("dry-run", false, "Enable dry-run mode")
	resizeOnRecreate := flag.Bool("resize-on-recreate", false, "Allow resizing if the workload update strategy is Recreate (default: false)")
	numberOfWorkers := flag.Int("workers", 1, "Number of concurrent workers for processing recommendations")
	resizeStrategy := flag.String("resize-strategy", "container", "Resize strategy to use (default: container, options: container, workload)")
	useLimits := flag.Bool("use-limits", false, "Use resource limits instead of requests for resizing (default: false)")
	logLevel := flag.String("log-level", "info", "Log level (default: info, options: debug, info, warn, error)")
	postRolloutCheck := flag.Bool("post-rollout-check", false, "Enable post-rollout check (default: false)")
	postRolloutCheckSec := flag.Int("post-rollout-check-sec", 30, "Post-rollout check interval in seconds (default: 60)")
	flag.Parse()

	setLogLevel(*logLevel)
	zerolog.TimeFieldFormat = time.RFC3339
	log.Logger = zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout, TimeFormat: "2006-01-02 15:04:05"}).
		With().
		Timestamp().
		Logger()

	log.Info().Msg("--- Start Rightsizer ---")

	checkRequiredFlags(*recFile, *resizeStrategy, *numberOfWorkers)

	log.Info().Msgf("DryRun: %v", *dryRun)
	log.Info().Msgf("Recommendations file: %v", *recFile)
	log.Info().Msgf("Resize on Recreate: %v", *resizeOnRecreate)
	log.Info().Msgf("Number of workers: %v", *numberOfWorkers)
	log.Info().Msgf("Resize strategy: %v", *resizeStrategy)
	log.Info().Msgf("Use limits: %v", *useLimits)
	log.Info().Msgf("Log level: %v", *logLevel)
	log.Info().Msgf("Post-rollout check: %v", *postRolloutCheck)
	log.Info().Msgf("Post-rollout check interval (sec): %v", *postRolloutCheckSec)

	fileInfo, err := waitForFile(*recFile, 5, 5*time.Second)
	if err != nil {
		log.Fatal().Err(err).Msg("Recommendations file not available after retries")
	}
	log.Info().Msgf("File found! Size: %d bytes", fileInfo.Size())

	// 1. Client Initialization
	k8sClient, err := getClientset()
	if err != nil {
		log.Fatal().Err(err).Msg("Fatal error initializing Kubernetes client")
	}

	// 2. Reading Recommendations
	recReader, err := reader.NewReader(*recFile)
	if err != nil {
		log.Fatal().Err(err).Msg("Error creating recommendation reader")
	}
	recs, err := recReader.Read()
	if err != nil {
		log.Fatal().Err(err).Msg("Error reading recommendations")
	}

	log.Info().Msgf("Recommendations read: %v", len(recs))

	// 3. Execute Engine
	resizeWatcher := watcher.NewResizeWatcher()
	resizeIndicator := listner.NewResizeIndicator(len(recs))
	if err := resizeWatcher.AddListener(resizeIndicator); err != nil {
		log.Fatal().Err(err).Msg("Error adding listener")
	}

	var resizer re.Resizer
	switch *resizeStrategy {
	case "container":
		resizer = re.NewContainerResizer(k8sClient, resizeWatcher)
	case "workload":
		resizer = re.NewWorkloadResizer(k8sClient, resizeWatcher)
	default:
		log.Fatal().Msgf("Invalid resize strategy: %v", *resizeStrategy)
	}

	rightsizer := re.NewWorkloadRightsizer(resizer)

	ctx := ctxkeys.WithDryRun(context.Background(), *dryRun)
	ctx = ctxkeys.WithResizeOnRecreate(ctx, *resizeOnRecreate)
	ctx = ctxkeys.WithNumberOfWorkers(ctx, *numberOfWorkers)
	ctx = ctxkeys.WithUseLimits(ctx, *useLimits)
	ctx = ctxkeys.WithPostRolloutCheck(ctx, *postRolloutCheck)
	ctx = ctxkeys.WithPostRolloutCheckInterval(ctx, time.Duration(*postRolloutCheckSec)*time.Second)
	if err := rightsizer.Rightsize(ctx, recs); err != nil {
		log.Error().Err(err).Msg("Resize process completed with some issues")
	}

	log.Info().Msg("--- Rightsizer Complete ---")

}

func checkRequiredFlags(recFile, resizeStrategy string, numberOfWorkers int) {
	if recFile == "" {
		log.Fatal().Msg("file-path is required")
	}

	if numberOfWorkers <= 0 {
		log.Fatal().Msg("workers parameter must be greater than 0")
	}
	if resizeStrategy != "container" && resizeStrategy != "workload" {
		log.Fatal().Msg("resize-strategy must be either 'container' or 'workload'")
	}
}

func waitForFile(path string, maxRetries int, interval time.Duration) (os.FileInfo, error) {
	for attempt := range maxRetries {
		info, err := os.Stat(path)
		if err == nil {
			return info, nil
		}
		log.Warn().Err(err).Msgf("File not available (attempt %d/%d), retrying in %s...", attempt+1, maxRetries, interval)
		time.Sleep(interval)
	}
	return nil, fmt.Errorf("file %s not available after %d attempts", path, maxRetries)
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

func setLogLevel(level string) {
	switch level {
	case "debug":
		zerolog.SetGlobalLevel(zerolog.DebugLevel)
	case "info":
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	case "warn":
		zerolog.SetGlobalLevel(zerolog.WarnLevel)
	case "error":
		zerolog.SetGlobalLevel(zerolog.ErrorLevel)
	default:
		log.Warn().Msgf("Invalid log level: %v, defaulting to info", level)
		zerolog.SetGlobalLevel(zerolog.InfoLevel)
	}
}
