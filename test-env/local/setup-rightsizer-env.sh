#!/bin/bash

# Check input parameters
if [ -z "$1" ]; then
    echo "❌ Error: You must specify the local path to mount."
    echo "Usage: $0 /path/to/your/data/folder"
    exit 1
fi

# Variable definitions
PROFILE_NAME="k8s-rightsizer-lab"
K8S_VERSION=v1.34
CPUS=2
MEMORY="4096mb"
HOST_PATH="$1" # Local folder for volumes
GUEST_PATH="/mnt/data"

echo "🚀 Starting Rightsizer environment (K8s $K8S_VERSION) with YAKD Addon"

minikube start \
    --profile "$PROFILE_NAME" \
    --driver=podman \
    --cpus=$CPUS \
    --memory=$MEMORY \
    --kubernetes-version=$K8S_VERSION \
    --addons=metrics-server \


if [ $? -ne 0 ]; then
    echo "❌ Error starting Minikube."
    exit 1
fi

echo "🔗 Mounting filesystem ($HOST_PATH -> $GUEST_PATH)..."
minikube mount "$HOST_PATH:$GUEST_PATH" --profile "$PROFILE_NAME" &
MOUNT_PID=$!

# Enabling YAKD via official Addon
echo "🖥️ Enabling YAKD addon..."
minikube addons enable yakd --profile "$PROFILE_NAME"

echo "⏳ Waiting for the dashboard to be ready..."
kubectl wait --for=condition=available --timeout=180s deployment/yakd-dashboard -n yakd-dashboard --context "$PROFILE_NAME"

echo "--------------------------------------------------------"
echo "✅ Environment ready!"
echo "🌐 Opening YAKD dashboard via official service..."
echo "--------------------------------------------------------"

minikube service yakd-dashboard -n yakd-dashboard --profile "$PROFILE_NAME"
