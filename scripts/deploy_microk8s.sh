#!/bin/bash

# ================================
# Configuration
# ================================

# Define VM names
MASTER="microk8s-master"
WORKERS=("microk8s-worker1" "microk8s-worker2")

# Define resources for VMs
CPUS=2
MEMORY=4G
DISK=10G

# Define Ubuntu image (optional: specify a particular version)
IMAGE="ubuntu:22.04"

# ================================
# Functions
# ================================

# This script will:

# - Launch three Multipass VMs.
# - Install MicroK8s on each VM.
# - Configure the master node.
# - Join the worker nodes to the cluster.
# - Verify the cluster status.

# Function to launch a Multipass VM
launch_vm() {
    local VM_NAME=$1
    echo "Launching VM: $VM_NAME..."
    multipass launch --name "$VM_NAME" --cpus $CPUS --mem $MEMORY --disk $DISK --cloud-init - <<EOF
packages:
  - snapd
EOF
    # Wait until the VM is running
    while true; do
        STATUS=$(multipass info "$VM_NAME" | grep "State:" | awk '{print $2}')
        if [ "$STATUS" == "Running" ]; then
            echo "$VM_NAME is running."
            break
        fi
        sleep 2
    done
}

# Function to install MicroK8s on a VM
install_microk8s() {
    local VM_NAME=$1
    echo "Installing MicroK8s on $VM_NAME..."
    multipass exec "$VM_NAME" -- sudo snap install microk8s --classic
    multipass exec "$VM_NAME" -- sudo usermod -a -G microk8s ubuntu
    multipass exec "$VM_NAME" -- sudo chown -f -R ubuntu ~/.kube
}

# Function to enable MicroK8s services on Master
enable_master_services() {
    local VM_NAME=$1
    echo "Enabling DNS and Storage on Master ($VM_NAME)..."
    multipass exec "$VM_NAME" -- sudo microk8s enable dns storage
}

# Function to retrieve the worker join command from Master
get_worker_join_command() {
    local VM_NAME=$1
    echo "Retrieving worker join command from Master ($VM_NAME)..."
    # Capture only the join command with --worker flag
    JOIN_CMD=$(multipass exec "$VM_NAME" -- sudo microk8s add-node | grep "microk8s join" | grep "--worker" | head -n1)
    echo "Join Command: $JOIN_CMD"
}

# Function to join a worker to the cluster
join_cluster() {
    local WORKER_VM=$1
    local JOIN_COMMAND=$2
    echo "Joining $WORKER_VM to the cluster..."
    multipass exec "$WORKER_VM" -- sudo $JOIN_COMMAND
}

# Function to verify cluster status
verify_cluster() {
    local MASTER_VM=$1
    echo "Verifying cluster status on Master ($MASTER_VM)..."
    multipass exec "$MASTER_VM" -- sudo microk8s kubectl get nodes
}

# ================================
# Deployment Steps
# ================================

echo "=== Starting MicroK8s Cluster Deployment ==="

# Step 1: Launch Master VM
launch_vm "$MASTER"

# Step 2: Launch Worker VMs
for WORKER in "${WORKERS[@]}"; do
    launch_vm "$WORKER"
done

# Step 3: Install MicroK8s on Master
install_microk8s "$MASTER"

# Step 4: Install MicroK8s on Workers
for WORKER in "${WORKERS[@]}"; do
    install_microk8s "$WORKER"
done

# Step 5: Enable DNS and Storage on Master
enable_master_services "$MASTER"

# Step 6: Retrieve Worker Join Command from Master
get_worker_join_command "$MASTER"

# Step 7: Join Workers to the Cluster
for WORKER in "${WORKERS[@]}"; do
    join_cluster "$WORKER" "$JOIN_CMD"
done

# Step 8: Verify Cluster Status
verify_cluster "$MASTER"

echo "=== MicroK8s Cluster Deployment Complete ==="
