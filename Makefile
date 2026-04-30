# Makefile for k8s-rightsizer project

# Check for container engine (podman or docker)
CONTAINER_ENGINE ?= $(shell which docker >/dev/null 2>&1 && echo docker || echo podman)

# Variables
APP_NAME := k8s-rightsizer
REGISTRY_USER ?= localhost
VERSION ?=local
IMG := $(REGISTRY_USER)/$(APP_NAME):$(VERSION)
ENV ?= local
RESIZE_ON_RECREATE ?= false
DRY_RUN ?= false
GIT_RECOMMENDATIONS_REPO ?=
GIT_RECOMMENDATIONS_FILE_PATH ?= recommendations.xlsx
GIT_BRANCH ?= main
GIT_EXTRA_ARGS = $(if $(strip $(GIT_RECOMMENDATIONS_REPO)), \
    --set recommendations.git.enabled=true \
    --set recommendations.git.repository=$(GIT_RECOMMENDATIONS_REPO) \
    --set recommendations.git.fileSourcePath=$(GIT_RECOMMENDATIONS_FILE_PATH) \
    --set recommendations.git.branch=$(GIT_BRANCH), \
    --set recommendations.git.enabled=false)	


# check for valid environment
SUPPORTED_ENVS := local dev
ifeq ($(filter $(ENV),$(SUPPORTED_ENVS)),)
    $(error Invalid ENV=$(ENV). Supported envs are: $(SUPPORTED_ENVS))
endif


.PHONY: clean
clean: ## Clean build artifacts
	@echo "Cleaning up..."
	go clean ./...
	rm -rf bin/

.PHONY: test
test: clean ## Run tests
	@echo "Running tests..."
	go test -v --cover ./...

.PHONY: build-bin
build-bin: ## Build the binary
	@echo "Compiling..."
	CGO_ENABLED=0 GOOS=linux go build -o bin/$(APP_NAME) cmd/main.go

.PHONY: image-build
image-build: ## Build the image
	@echo "Building image..."
	$(CONTAINER_ENGINE) build -t $(IMG) .

.PHONY: image-push
image-push: ## Push the image to the registry
	@echo "Pushing image..."
	$(CONTAINER_ENGINE) push $(IMG)


.PHONY: deploy
deploy: ## Deploy with Helm (usage: make deploy [ENV=local|dev] [REGISTRY_USER=your-registry-user] [VERSION=your-version])
ifeq ($(ENV),local)
	@echo "📦 Exporting image for local deployment..."
	$(CONTAINER_ENGINE) save $(IMG) -o rightsizer.tar
	@echo "🚚 Loading image into Minikube..."
	minikube image load rightsizer.tar --profile k8s-rightsizer-lab
	@echo "🧹 Cleaning up..."
	rm rightsizer.tar	
endif
	@echo "🚀 Deploying with Helm to environment: $(ENV)..."
	@echo "ARGS: $(GIT_EXTRA_ARGS)"
	helm upgrade --install $(APP_NAME) ./k8s-rightsizer-helm \
		-n k8s-rightsizer \
		--create-namespace \
		-f ./k8s-rightsizer-helm/values.yaml \
		-f ./k8s-rightsizer-helm/$(ENV)/values.yaml \
		--set image.repository=$(REGISTRY_USER)/$(APP_NAME) \
		--set image.tag=$(VERSION) \
		--set settings.dryRun=$(DRY_RUN) \
		--set settings.resizeOnRecreate=$(RESIZE_ON_RECREATE) \
		$(GIT_EXTRA_ARGS)

.PHONY: undeploy
undeploy: ## Undeploy (usage: make undeploy ENV=local|dev (default local))
	@echo "🧹 Undeploying..."
	helm uninstall $(APP_NAME) --namespace k8s-rightsizer
	kubectl delete ns k8s-rightsizer

.PHONY: all
all: image-build image-push deploy ## Perform all steps: build, push, and deploy
	

.DEFAULT_GOAL := help
.PHONY: help
help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)