# Variabili
APP_NAME := k8s-rightsizer
DOCKER_USER := mcpunzo
VERSION := v0.0.1
IMG := $(DOCKER_USER)/$(APP_NAME):$(VERSION)

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

.PHONY: docker-build
docker-build: ## Build the Docker image
	@echo "Docker Image creation..."
	docker build -t $(IMG) .

.PHONY: docker-push
docker-push: ## Push the Docker image to the registry
	@echo "Pushing Docker image..."
	docker push $(IMG)

.PHONY: helm-deploy
helm-deploy: ## Deploy with Helm
	@echo "Deploying with Helm..."
	helm upgrade --install $(APP_NAME) ./k8s-rightsizer-helm \
		--set image.repository=$(DOCKER_USER)/$(APP_NAME) \
		--set image.tag=$(VERSION)


.PHONY: all
all: docker-build docker-push helm-deploy ## Perform all steps: build, push, and deploy
	

.DEFAULT_GOAL := help
.PHONY: help
help: ## Show this help screen
	@echo 'Usage: make <OPTIONS> ... <TARGETS>'
	@echo ''
	@echo 'Available targets are:'
	@echo ''
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z0-9_-]+:.*?##/ { printf "  \033[36m%-25s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)