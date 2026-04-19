#!/bin/bash

# Check input parameters
if [ -z "$1" ]; then
    echo "❌ Error: You must specify the local path to mount."
    echo "Usage: $0 /path/to/your/data/folder"
    exit 1
fi

# Automatic driver detection
# Priority to Docker (more stable for mounts), fallback to Podman
if command -v docker >/dev/null 2>&1; then
    DETECTED_DRIVER="docker"
elif command -v podman >/dev/null 2>&1; then
    DETECTED_DRIVER="podman"
else
    echo "❌ Errore: Né Docker né Podman sono installati."
    exit 1
fi

# Variable definitions
DRIVER=${DRIVER:-$DETECTED_DRIVER}
PROFILE_NAME="k8s-rightsizer-lab"
K8S_VERSION=v1.34
CPUS=2
MEMORY="4096mb"
HOST_PATH="$1" # Local folder for volumes
GUEST_PATH="/mnt/data"


# Check if the profile already exists with a different driver
CURRENT_DRIVER=$(minikube profile list -o json | jq -r ".valid[] | select(.Name==\"$PROFILE_NAME\") | .Config.Driver" 2>/dev/null)

if [ ! -z "$CURRENT_DRIVER" ] && [ "$CURRENT_DRIVER" != "$DRIVER" ]; then
    echo "⚠️ Error: The profile already exists with driver $CURRENT_DRIVER, but you are trying to use $DRIVER."
    echo "Run 'minikube delete -p $PROFILE_NAME' to change the driver."
    exit 1
fi

echo "🚀 Starting Rightsizer environment (K8s $K8S_VERSION) with YAKD Addon"

minikube start \
    --profile "$PROFILE_NAME" \
    --driver=$DRIVER \
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
