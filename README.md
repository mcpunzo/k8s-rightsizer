# K8s Rightsizer

A robust Kubernetes automation tool designed to apply resource recommendations (CPU/Memory) to **Deployments** and **StatefulSets** with an integrated **automatic rollback mechanism**.

The tool reads a list of recommendations from an Excel file, applies them, and monitors the rollout. If a Pod fails to start (OOMKilled, CrashLoopBackOff, Unschedulable, etc.), it immediately restores the previous stable configuration.

## 🚀 Key Features

* **Bulk Updates**: Process multiple resource changes via recommendation file (supported format .xlsx, .xsl).
* **Safety First**: Automatic rollback if the new resources cause deployment failures.
* **Smart Monitoring**: Detects `OOMKilled`, `CrashLoopBackOff`, and `Insufficient Resources` in real-time.
* **Cross-Controller Support**: Works seamlessly with both Deployments and StatefulSets.
* **Helm Powered**: Easy distribution and configuration for Local and Remote environments.


## 🛠 Prerequisites

* **Kubernetes Cluster** (v1.34+)
* **Helm** (v4.1+)
* **Go** (v1.25+) - *Only for local development*
* **Make**
* **Podman or Docker**



## 💻 Local Environment (Minikube)

To test the Rightsizer engine locally, you need to sync your container image and recommendation data with a Minikube node. We provide an automated script to spin up a pre-configured environment.

### 1. Setup the local environment

The setup script initializes a single-node Minikube cluster using the Podman driver, enables necessary addons (YAKD Dashboard, Metrics Server), and mounts your local data folder.
This script will automatically detect the driver to set up the cluster (docker, podman) but you can override this selection by setting the variable DRIVER.

```bash
# Navigate to the test directory
cd ./test-env/local

# Run the setup script by passing the local folder containing your recommendation data
# Usage: ./setup-rightsizer-env.sh <absolute_or_relative_path>
./setup-rightsizer-env.sh ~/my-project-data

# Force using podman driver
# DRIVER=podman ./setup-rightsizer-env.sh ~/my-project-data

```
**Note**: The script mounts your local folder to /mnt/data inside the Minikube node. Ensure your Kubernetes PersistentVolume manifests point to this path.


### 2. Build and Load the Image
Minikube needs the image in its internal registry. When using Podman, the most reliable way is via a tarball:

```bash
# 1. Build the image with a local tag
make image-build REGISTRY_USER=localhost VERSION=local

# 2. Load image into Minikube and deploy via helm
make deploy
```

### 3. Cleanup

```bash
make undeploy
```


## ☁️ Remote Environment

### 1. Build and push to registry

```bash
#1. set env variables
export REGISTRY_USER=<registry_user>
export VERSION=<image_ver>

#2. build and push the image to your image registry
make image-build  
make image-push
```

### 2. Deploy

```bash
# 3. Deploy
make deploy ENV=dev
```

### 3. Cleanup

```bash
make undeploy
```


## <img src="https://git-scm.com/images/logos/downloads/Git-Icon-1788C.png" width="25" height="25" /> Load recommendations from Git
You can enable the k8s-rightsizer to download the recommendations file from your git repo on startup.

```bash
#1. set env variables
export GIT_RECOMMENDATIONS_REPO=<your_repo>
export GIT_RECOMMENDATIONS_FILE_PATH=<recommendations_file_path in your repo> #default is recommendations.xslx
export GIT_BRANCH=<repo_branch> #default is main
```


# 🛡️ Rollback Logic Specification

The **K8s Rightsizer** is built with a "Safety-First" approach. Instead of simply applying changes, it treats every resource update as a monitored transaction.


## 🔄 The Lifecycle of an Update

The tool follows a strict state machine for each entry in the Excel file:

### 1. Pre-checks
Before applying any recommendation, the engine performs a series of safety checks to ensure that resizing won't cause service disruptions or violate cluster policies.

A resize operation is automatically skipped if any of the following conditions are met:

* **Paused State**: The workload (Deployment/StatefulSet) is currently paused by the user.
* **PDB Restrictions**: A PodDisruptionBudget is active and too restrictive (e.g., maxUnavailable: 0 or current available replicas at the limit), making any pod restart unsafe.

* **Unsupported Update Strategies**: Only RollingUpdate is currently supported to ensure zero-downtime transitions.
  - OnDelete: Skipped because the update wouldn't trigger automatically.
  - Recreate: Skipped to avoid the full downtime typical of this strategy.

* **Degraded Health**: The workload is not healthy. We don't resize unstable systems.
* **Ongoing Rollout**: A deployment is already in progress. We wait for the system to reach a stable state.

* **Critical Pod Errors**: Critical issues are detected in the existing pods (e.g., CrashLoopBackOff, ImagePullBackOff). The resizer won't interfere with workloads that are already failing.


### 2. Snapshot Phase (Pre-Check)
Before any modification, the tool fetches the current resource configuration of the target (Deployment or StatefulSet).
* **Action**: Saves `cpu` and `memory` limits/requests into an in-memory backup.
* **Metadata**: Records the current `generation` of the resource.

### 3. Application Phase
The tool applies the new values using a **Strategic Merge Patch**.
* **Trigger**: Updates the container spec with values from the Excel file.
* **Wait**: Triggers a new Rollout in Kubernetes.

### 4. Monitoring Phase (The "Watch" Loop)
This is the core of the rollback logic. The tool monitors the new Pods for a configurable `timeout` (default: **3 minutes**).

The system identifies a failure if any of the following conditions are met:
* **CrashLoopBackOff**: The application crashes immediately after start.
* **OOMKilled**: The new memory limit is too low for the application's heap.
* **ImagePullBackOff**: Issues with the container registry.
* **Unschedulable**: The requested resources are too high for the available nodes (Insufficient CPU/Memory).
* **Timeout**: The Pods do not reach the `Ready` state within the time limit.


### 5. Rollback Phase (Recovery)
If a failure is detected, the tool immediately aborts the monitoring and initiates recovery.
* **Action**: Re-applies the **Snapshot** taken in Phase 1.
* **Verification**: Ensures the resource returns to its original `Ready` state.
* **Reporting**: Logs the specific error (e.g., "OOMKilled detected") and marks the Excel row as `FAILED - ROLLED BACK`.



## 📊 Logic Flowchart

1. **START** ➔ Read row from Excel.
2. **RETRIEVE** ➔ Retrieve current `resourcee`.
3. **PRECHECK** ➔ Check current `resources` conditions 
4. **BACKUP** ➔ Create current `resources` backup.
5. **PATCH** ➔ Apply new `resources`.
6. **MONITOR** ➔ Watch Pod status.
   * ✅ **IF READY** within 3m ➔ **COMMIT** (Next row).
   * ❌ **IF ERROR** (OOM/Crash/Timeout) ➔ **ROLLBACK**.
7. **RESTORE** ➔ Re-apply backup ➔ **LOG ERROR**.



## ⚙️ Failure Detection Parameters

| Condition | Detection Method | System Response |
| :--- | :--- | :--- |
| **Out of Memory** | Container status `OOMKilled` | Immediate Rollback |
| **Startup Crash** | Container status `CrashLoopBackOff` | Immediate Rollback |
| **Resource Starvation** | Event `FailedScheduling` | Immediate Rollback |
| **Liveness Failure** | Container `Unhealthy` events | Rollback after 3 retries |


# Configuration

## 📊 Excel File Structure

Recommendation file (.xslx, .xsl) must contain the following columns (order is important).

| Column Name | Description | Example Value |
| :--- | :--- | :--- |
| **Environment** | The stage environment. | `production` |
| **Namespace** | The K8s namespace where the resource resides. | `prod-app` |
| **Kind** | The type of resource (`Deployment` or `StatefulSet`). | `Deployment` |
| **Workload Name** | The name of the resource. | `api-gateway` |
| **Container** | The name of the container in this workload. | `api-gateway` |
| **Replicas** | The number of replicas. | `2` |
| **CPU Request** | The current CPU request value. | `250m` |
| **CPU Limit** | The current CPU limit value. | `500m` |
| **CPU Request Recommendation** | The new CPU request value recommended. | `150m` |
| **CPU Limit Recommendation** | The new CPU limit value recommended. | `300m` |
| **Mem Request** | The current Memory request value. | `256Mi` |
| **Mem Limit** | The current Memory limit value. | `512Mi` |
| **Mem Request Recommendation** | The new Memory request value recommended. | `256Mi` |
| **Mem Limit Recommendation** | The new Memory limit value recommended. | `512Mi` |

**Note** Empty values for recommended columns are not allowed. Therefore set 
  - **CPU Limit Recommended** to 0m and 
  - **Memory Limit Recommended** to 0Mi