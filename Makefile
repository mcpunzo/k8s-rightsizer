# Makefile for k8s-rightsizer project

# Check for container engine (podman or docker)
CONTAINER_ENGINE ?= $(shell which docker >/dev/null 2>&1 && echo docker || echo podman)

# Variables
APP_NAME := k8s-rightsizer
REGISTRY_USER := mcpunzo
VERSION := v0.0.1
IMG := $(REGISTRY_USER)/$(APP_NAME):$(VERSION)

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
	$(CONTAINER_ENGINE) b\uild -t $(IMG) .

.PHONY: image-push
image-push: ## Push the image to the registry
	@echo "Pushing image..."
	$(CONTAINER_ENGINE) push $(IMG)

.PHONY: helm-local-deploy
helm-local-deploy: ## Deploy with Helm (local)
	@echo "Exporting image for local deployment..."
	$(CONTAINER_ENGINE) save $(IMG) -o rightsizer.tar
	@echo "Loading image into Minikube..."
	minikube image load rightsizer.tar --profile k8s-rightsizer-lab
	@echo "Cleaning up..."
	rm rightsizer.tar
	@echo "Deploying with Helm (local)..."
	helm upgrade --install $(APP_NAME) ./k8s-rightsizer-helm \
		--create-namespace \
		-f ./k8s-rightsizer-helm/values.yaml \
  		-f ./k8s-rightsizer-helm/local/values.yaml \
		

.PHONY: helm-dev-deploy
helm-dev-deploy: ## Deploy with Helm (development)
	@echo "Deploying with Helm (development)..."
	helm upgrade --install $(APP_NAME) ./k8s-rightsizer-helm \
		--create-namespace \
		-f ./k8s-rightsizer-helm/values.yaml \
  		-f ./k8s-rightsizer-helm/dev/values.yaml \
		--set image.repository=$(DOCKER_USER)/$(APP_NAME) \
		--set image.tag=$(VERSION)


.PHONY: all
all: image-build image-push helm-dev-deploy ## Perform all steps: build, push, and deploy
	

.DEFAULT_GOAL := help
.PHONY: help
help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)