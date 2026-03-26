#!/usr/bin/env bash
# Migration: AzureManagedControlPlane → AzureASOManagedControlPlane
#
# This script migrates a CAPZ-managed AKS cluster from the legacy
# AzureManagedControlPlane API to the new AzureASOManagedControlPlane API.
# The underlying Azure resources are never created or deleted — only
# the management-cluster Kubernetes objects change.
#
# Prerequisites:
#   - kubectl configured to talk to the CAPI management cluster
#   - jq installed
#   - Existing cluster is healthy and using AzureManagedControlPlane
#
# Usage:
#   export CLUSTER_NAME="my-cluster"
#   export OLD_NAMESPACE="default"
#   export NEW_NAMESPACE="my-cluster-aso"
#   bash migration-commands.sh

set -euo pipefail

# ============================================================
# Configuration — set these before running
# ============================================================
: "${CLUSTER_NAME:?Set CLUSTER_NAME to the name of the CAPI Cluster resource}"
: "${OLD_NAMESPACE:?Set OLD_NAMESPACE to the namespace of the existing cluster}"
: "${NEW_NAMESPACE:?Set NEW_NAMESPACE to the target namespace for the migrated cluster}"

echo "Migrating cluster '${CLUSTER_NAME}' from namespace '${OLD_NAMESPACE}' to '${NEW_NAMESPACE}'"
echo ""

# ============================================================
# Preflight checks
# ============================================================
echo ">>> Preflight checks..."

# Verify jq is available
if ! command -v jq &>/dev/null; then
  echo "ERROR: jq is required but not installed."
  exit 1
fi

# Verify the cluster exists and is available
if ! kubectl get cluster "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" &>/dev/null; then
  echo "ERROR: Cluster '${CLUSTER_NAME}' not found in namespace '${OLD_NAMESPACE}'."
  exit 1
fi

# Verify the cluster uses AzureManagedControlPlane (not already migrated)
CP_KIND=$(kubectl get cluster "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" -o jsonpath='{.spec.controlPlaneRef.kind}')
if [ "${CP_KIND}" != "AzureManagedControlPlane" ]; then
  echo "ERROR: Cluster control plane is '${CP_KIND}', expected 'AzureManagedControlPlane'."
  echo "       This cluster may already be migrated or is not using the legacy API."
  exit 1
fi

# Verify the new namespace doesn't already have a cluster with this name
if kubectl get cluster "${CLUSTER_NAME}" -n "${NEW_NAMESPACE}" &>/dev/null 2>&1; then
  echo "ERROR: A cluster named '${CLUSTER_NAME}' already exists in namespace '${NEW_NAMESPACE}'."
  exit 1
fi

echo "  Preflight checks passed."
echo ""

# ============================================================
# Auto-discover cluster configuration
# ============================================================
echo ">>> Discovering existing cluster configuration..."

# Get the ASO credential secret name from the existing ManagedCluster
ASO_SECRET=$(kubectl get managedcluster.containerservice.azure.com/"${CLUSTER_NAME}" \
  -n "${OLD_NAMESPACE}" -o jsonpath='{.metadata.annotations.serviceoperator\.azure\.com/credential-from}')
echo "  ASO credential secret: ${ASO_SECRET}"

# Get the ResourceGroup spec
RG_LOCATION=$(kubectl get resourcegroup.resources.azure.com/"${CLUSTER_NAME}" \
  -n "${OLD_NAMESPACE}" -o jsonpath='{.spec.location}')
RG_AZURE_NAME=$(kubectl get resourcegroup.resources.azure.com/"${CLUSTER_NAME}" \
  -n "${OLD_NAMESPACE}" -o jsonpath='{.spec.azureName}')
echo "  Resource group: ${RG_AZURE_NAME} (${RG_LOCATION})"

# Discover pool names from AzureManagedMachinePools
POOL_NAMES=$(kubectl get azuremanagedmachinepools -n "${OLD_NAMESPACE}" \
  -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}" \
  -o jsonpath='{.items[*].metadata.name}')
echo "  Machine pools: ${POOL_NAMES}"

# Export the existing ASO ManagedCluster spec (strip status, managed fields, etc.)
# Also translate fields that changed between API versions (e.g. sku.name "Basic" → "Base")
# and include agentPoolProfiles from the status (required by newer Azure API versions).
MC_SPEC=$(kubectl get managedcluster.containerservice.azure.com/"${CLUSTER_NAME}" \
  -n "${OLD_NAMESPACE}" -o json | jq '{
    spec: (
      (.spec | del(.conditions) | del(.operatorSpec))
      + {agentPoolProfiles: [.status.agentPoolProfiles[] | {name, mode, vmSize, count, osType, osDiskSizeGB, osDiskType, type}]}
    )
  } | if .spec.sku.name == "Basic" then .spec.sku.name = "Base" else . end')

# Export each ASO ManagedClustersAgentPool spec
declare -A POOL_SPECS
declare -A POOL_AZURE_NAMES
for pool in ${POOL_NAMES}; do
  POOL_SPECS[$pool]=$(kubectl get managedclustersagentpool.containerservice.azure.com/"${pool}" \
    -n "${OLD_NAMESPACE}" -o json | jq '{spec: .spec}')
  POOL_AZURE_NAMES[$pool]=$(kubectl get managedclustersagentpool.containerservice.azure.com/"${pool}" \
    -n "${OLD_NAMESPACE}" -o jsonpath='{.spec.azureName}')
  echo "  Pool ${pool}: azureName=${POOL_AZURE_NAMES[$pool]}"
done

echo ""

# ============================================================
# Step 1: Pause the old cluster
# ============================================================
echo ">>> Step 1: Pausing cluster..."
kubectl patch cluster "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" \
  --type merge -p '{"spec": {"paused": true}}'

# ============================================================
# Step 2: Verify block-move annotations are removed
# ============================================================
echo ">>> Step 2: Verifying block-move annotations are cleared..."
for i in $(seq 1 30); do
  AMCP_ANNOTATION=$(kubectl get azuremanagedcontrolplane "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" \
    -o jsonpath='{.metadata.annotations.clusterctl\.cluster\.x-k8s\.io/block-move}' 2>/dev/null || true)
  if [ -z "${AMCP_ANNOTATION}" ]; then
    break
  fi
  echo "  Waiting for block-move annotation removal... (attempt ${i}/30)"
  sleep 2
done

# Also check machine pools
for pool in ${POOL_NAMES}; do
  POOL_ANNOTATION=$(kubectl get azuremanagedmachinepool "${pool}" -n "${OLD_NAMESPACE}" \
    -o jsonpath='{.metadata.annotations.clusterctl\.cluster\.x-k8s\.io/block-move}' 2>/dev/null || true)
  if [ -n "${POOL_ANNOTATION}" ]; then
    echo "ERROR: block-move annotation still present on ${pool}"
    exit 1
  fi
done
echo "  Annotations cleared."

# ============================================================
# Step 3: Disarm old ASO resources
# ============================================================
# CRITICAL SAFETY STEP: This must happen BEFORE creating new resources and BEFORE
# deleting any old resources. Old ASO resources have finalizers that would cause ASO
# to delete the actual Azure resources if the K8s objects are garbage collected.
# Setting reconcile-policy to "skip" and removing the finalizer makes them inert.
echo ">>> Step 3: Disarming old ASO resources..."

for pool in ${POOL_NAMES}; do
  kubectl patch managedclustersagentpool.containerservice.azure.com/"${pool}" -n "${OLD_NAMESPACE}" \
    --type merge -p '{"metadata": {"annotations": {"serviceoperator.azure.com/reconcile-policy": "skip"}, "finalizers": null}}'
done

kubectl patch managedcluster.containerservice.azure.com/"${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" \
  --type merge -p '{"metadata": {"annotations": {"serviceoperator.azure.com/reconcile-policy": "skip"}, "finalizers": null}}'

kubectl patch resourcegroup.resources.azure.com/"${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" \
  --type merge -p '{"metadata": {"annotations": {"serviceoperator.azure.com/reconcile-policy": "skip"}, "finalizers": null}}'

echo "  Old ASO resources disarmed."

# ============================================================
# Step 4: Create new namespace and copy credentials
# ============================================================
echo ">>> Step 4: Creating namespace '${NEW_NAMESPACE}' and copying credentials..."
kubectl create namespace "${NEW_NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -

kubectl get secret "${ASO_SECRET}" -n "${OLD_NAMESPACE}" -o json | \
  jq --arg ns "${NEW_NAMESPACE}" '.metadata = {name: .metadata.name, namespace: $ns}' | \
  kubectl apply -f -

# ============================================================
# Step 5a: Create ResourceGroup in new namespace
# ============================================================
echo ">>> Step 5a: Creating ResourceGroup..."
cat <<RGEOF | kubectl apply -f -
apiVersion: resources.azure.com/v1api20200601
kind: ResourceGroup
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NEW_NAMESPACE}
  annotations:
    serviceoperator.azure.com/credential-from: ${ASO_SECRET}
spec:
  azureName: ${RG_AZURE_NAME}
  location: ${RG_LOCATION}
RGEOF

echo "  Waiting for ResourceGroup to be Ready..."
kubectl wait --for=condition=Ready \
  resourcegroup.resources.azure.com/"${CLUSTER_NAME}" \
  -n "${NEW_NAMESPACE}" --timeout=120s

# ============================================================
# Step 5b: Create ManagedCluster with adopt annotation
# ============================================================
echo ">>> Step 5b: Creating ManagedCluster with adopt annotation..."
echo "${MC_SPEC}" | jq --arg name "${CLUSTER_NAME}" \
  --arg ns "${NEW_NAMESPACE}" \
  --arg secret "${ASO_SECRET}" \
  '{
    apiVersion: "containerservice.azure.com/v1api20240901",
    kind: "ManagedCluster",
    metadata: {
      name: $name,
      namespace: $ns,
      annotations: {
        "serviceoperator.azure.com/credential-from": $secret,
        "sigs.k8s.io/cluster-api-provider-azure-adopt": "true"
      }
    }
  } + .' | kubectl apply -f -

echo "  Waiting for ManagedCluster to be Ready..."
kubectl wait --for=condition=Ready \
  managedcluster.containerservice.azure.com/"${CLUSTER_NAME}" \
  -n "${NEW_NAMESPACE}" --timeout=300s

echo "  Waiting for CAPZ to scaffold Cluster, AzureASOManagedControlPlane, AzureASOManagedCluster..."
for i in $(seq 1 60); do
  if kubectl get cluster.cluster.x-k8s.io/"${CLUSTER_NAME}" -n "${NEW_NAMESPACE}" &>/dev/null; then
    break
  fi
  sleep 5
done
kubectl get clusters.cluster.x-k8s.io,azureasomanagedcontrolplanes,azureasomanagedclusters -n "${NEW_NAMESPACE}"

# ============================================================
# Step 5c: Create ManagedClustersAgentPools with adopt annotation
# ============================================================
echo ">>> Step 5c: Creating ManagedClustersAgentPools with adopt annotation..."
for pool in ${POOL_NAMES}; do
  echo "  Creating pool ${pool} (azureName: ${POOL_AZURE_NAMES[$pool]})..."
  echo "${POOL_SPECS[$pool]}" | jq --arg name "${pool}" \
    --arg ns "${NEW_NAMESPACE}" \
    --arg secret "${ASO_SECRET}" \
    '{
      apiVersion: "containerservice.azure.com/v1api20240901",
      kind: "ManagedClustersAgentPool",
      metadata: {
        name: $name,
        namespace: $ns,
        annotations: {
          "serviceoperator.azure.com/credential-from": $secret,
          "sigs.k8s.io/cluster-api-provider-azure-adopt": "true"
        }
      }
    } + .' | kubectl apply -f -
done

for pool in ${POOL_NAMES}; do
  echo "  Waiting for ${pool} to be Ready..."
  kubectl wait --for=condition=Ready \
    managedclustersagentpool.containerservice.azure.com/"${pool}" \
    -n "${NEW_NAMESPACE}" --timeout=120s
done

# ============================================================
# Step 5d: Wait for new cluster to be fully available
# ============================================================
echo ">>> Step 5d: Waiting for new cluster to be Available..."
kubectl wait --for=condition=Available \
  cluster.cluster.x-k8s.io/"${CLUSTER_NAME}" \
  -n "${NEW_NAMESPACE}" --timeout=300s

echo "  New cluster status:"
kubectl get clusters.cluster.x-k8s.io,machinepools -n "${NEW_NAMESPACE}" -o wide

# ============================================================
# Step 6: Delete old CAPI/CAPZ resources
# ============================================================
# Old ASO resources were already disarmed in Step 3, so garbage collection
# of ASO resources will not trigger Azure resource deletion.
echo ">>> Step 6: Deleting old CAPI/CAPZ resources..."

kubectl delete cluster "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" --wait=false
kubectl delete azuremanagedcluster "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" --wait=false
kubectl delete azuremanagedcontrolplane "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" --wait=false
kubectl delete machinepool -n "${OLD_NAMESPACE}" -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}" --wait=false
kubectl delete azuremanagedmachinepool -n "${OLD_NAMESPACE}" -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}" --wait=false

# Remove finalizers so stuck resources actually delete (paused clusters won't reconcile)
echo "  Removing finalizers..."
for pool in ${POOL_NAMES}; do
  kubectl patch azuremanagedmachinepool "${pool}" -n "${OLD_NAMESPACE}" \
    --type merge -p '{"metadata": {"finalizers": null}}' 2>/dev/null || true
  kubectl patch machinepool "${pool}" -n "${OLD_NAMESPACE}" \
    --type merge -p '{"metadata": {"finalizers": null}}' 2>/dev/null || true
done

kubectl patch azuremanagedcontrolplane "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" \
  --type merge -p '{"metadata": {"finalizers": null}}' 2>/dev/null || true
kubectl patch cluster "${CLUSTER_NAME}" -n "${OLD_NAMESPACE}" \
  --type merge -p '{"metadata": {"finalizers": null}}' 2>/dev/null || true

# ============================================================
# Step 7: Verify
# ============================================================
echo ""
echo ">>> Step 7: Verification"
echo ""
echo "=== Old resources in '${OLD_NAMESPACE}' (should be empty) ==="
kubectl get clusters.cluster.x-k8s.io,azuremanagedcontrolplanes,azuremanagedclusters,machinepools,azuremanagedmachinepools \
  -n "${OLD_NAMESPACE}" -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}" 2>&1 || true
kubectl get managedclusters.containerservice.azure.com,managedclustersagentpools.containerservice.azure.com,resourcegroups.resources.azure.com \
  -n "${OLD_NAMESPACE}" -l "cluster.x-k8s.io/cluster-name=${CLUSTER_NAME}" 2>&1 || true

echo ""
echo "=== New cluster in '${NEW_NAMESPACE}' ==="
kubectl get clusters.cluster.x-k8s.io,azureasomanagedcontrolplanes,azureasomanagedclusters,machinepools,azureasomanagedmachinepools \
  -n "${NEW_NAMESPACE}" -o wide

echo ""
echo "=== Workload cluster nodes ==="
kubectl get secret "${CLUSTER_NAME}-kubeconfig" -n "${NEW_NAMESPACE}" -o jsonpath='{.data.value}' | \
  base64 -d > "/tmp/${CLUSTER_NAME}-migration-verify.kubeconfig"
KUBECONFIG="/tmp/${CLUSTER_NAME}-migration-verify.kubeconfig" kubectl get nodes -o wide
rm -f "/tmp/${CLUSTER_NAME}-migration-verify.kubeconfig"

echo ""
echo "✅ Migration complete! Cluster '${CLUSTER_NAME}' is now managed by AzureASOManagedControlPlane in namespace '${NEW_NAMESPACE}'."
