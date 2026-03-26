# Migration commands template (AzureManagedControlPlane → AzureASOManagedControlPlane)
# Set the environment variables below before running
#
# WHAT THIS DOES:
# This migrates how the AKS cluster is *managed* on the CAPI management cluster.
# It swaps the old-style CAPZ resources (AzureManagedControlPlane, AzureManagedCluster,
# AzureManagedMachinePool) for the new ASO-based ones (AzureASOManagedControlPlane,
# AzureASOManagedCluster, AzureASOManagedMachinePool). The actual AKS cluster in Azure
# is never touched — same nodes, same pods, same IPs, zero downtime.

# ============================================================
# Step 1: Pause the old cluster
# ============================================================
# Setting spec.paused=true on the Cluster resource tells all CAPI and CAPZ controllers
# to stop reconciling this cluster entirely. This is critical because we're about to
# create a second set of resources representing the same Azure cluster, and we can't
# have two controllers fighting over the same Azure resources simultaneously.
kubectl patch cluster ${CLUSTER_NAME} --type merge -p '{"spec": {"paused": true}}'

# ============================================================
# Step 2: Verify block-move annotations are removed
# ============================================================
# When a cluster is actively reconciling, CAPZ puts a "block-move" annotation on the
# AzureManagedControlPlane and AzureManagedMachinePools to prevent clusterctl from
# moving them mid-reconciliation. Once paused, the controllers remove this annotation
# on their next (final) pass. We need to wait for this to complete before proceeding.
# All three commands below should return empty output.
kubectl get azuremanagedcontrolplane ${CLUSTER_NAME} -o jsonpath='{.metadata.annotations.clusterctl\.cluster\.x-k8s\.io/block-move}'
kubectl get azuremanagedmachinepool ${CLUSTER_NAME}-pool0 -o jsonpath='{.metadata.annotations.clusterctl\.cluster\.x-k8s\.io/block-move}'
kubectl get azuremanagedmachinepool ${CLUSTER_NAME}-pool1 -o jsonpath='{.metadata.annotations.clusterctl\.cluster\.x-k8s\.io/block-move}'

# ============================================================
# Step 3: Create new namespace
# ============================================================
# We put the new resources in a separate namespace to avoid conflicts with the old
# ASO resources. ASO uses the resource name + namespace + Azure resource ID to track
# resources. Having the old and new ASO objects in the same namespace with the same
# name would conflict, so a new namespace cleanly separates them.
kubectl create namespace ${NEW_NAMESPACE}

# ============================================================
# Step 4: Copy ASO credential secret to new namespace
# ============================================================
# ASO resources authenticate to Azure using a credential secret referenced by the
# annotation "serviceoperator.azure.com/credential-from" on each ASO resource.
# The new ASO resources in the new namespace need access to the same credentials.
# Here we clone the existing secret (which contains AZURE_CLIENT_ID, AZURE_TENANT_ID,
# AZURE_SUBSCRIPTION_ID, and AUTH_MODE) into the new namespace.
kubectl get secret ${ASO_CREDENTIAL_SECRET} -o json | \
  python3 -c "
import sys, json
s = json.load(sys.stdin)
s['metadata'] = {'name': s['metadata']['name'], 'namespace': '${NEW_NAMESPACE}'}
json.dump(s, sys.stdout)
" | kubectl apply -f -

# ============================================================
# Step 5a: Create ResourceGroup in new namespace
# ============================================================
# The CAPZ adoption controller requires an ASO ResourceGroup to exist in the namespace
# before it can process a ManagedCluster adoption. This ResourceGroup points at the
# same Azure resource group that already exists — ASO will detect it already exists
# in Azure and simply adopt it (no creation or modification happens in Azure).
cat <<'EOF' | kubectl apply -f -
apiVersion: resources.azure.com/v1api20200601
kind: ResourceGroup
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NEW_NAMESPACE}
  annotations:
    serviceoperator.azure.com/credential-from: ${ASO_CREDENTIAL_SECRET}
spec:
  azureName: ${CLUSTER_NAME}
  location: ${LOCATION}
EOF
kubectl wait --for=condition=Ready resourcegroup.resources.azure.com/${CLUSTER_NAME} -n ${NEW_NAMESPACE} --timeout=120s

# ============================================================
# Step 5b: Create ManagedCluster with adopt annotation
# ============================================================
# This is the key step. We create an ASO ManagedCluster resource that describes the
# existing AKS cluster, with the special annotation:
#   sigs.k8s.io/cluster-api-provider-azure-adopt: "true"
#
# Two things happen:
# 1. ASO sees this resource, computes the Azure resource ID from the spec (subscription
#    + resource group + cluster name), finds the existing AKS cluster, and adopts it
#    rather than trying to create a new one. No Azure changes occur.
# 2. CAPZ's ManagedClusterAdoptReconciler watches for ManagedCluster resources with the
#    adopt annotation. When it sees one, it automatically scaffolds the CAPI resources:
#    - A Cluster (the top-level CAPI object)
#    - An AzureASOManagedControlPlane (wraps the ManagedCluster ASO resource)
#    - An AzureASOManagedCluster (wraps the ResourceGroup ASO resource)
#
# Note: we use v1api20240901 (newer API version) even though the old cluster used
# v1api20210501. ASO handles the version translation. The spec fields here match
# the existing cluster's configuration to avoid any drift.
cat <<'EOF' | kubectl apply -f -
apiVersion: containerservice.azure.com/v1api20240901
kind: ManagedCluster
metadata:
  name: ${CLUSTER_NAME}
  namespace: ${NEW_NAMESPACE}
  annotations:
    serviceoperator.azure.com/credential-from: ${ASO_CREDENTIAL_SECRET}
    sigs.k8s.io/cluster-api-provider-azure-adopt: "true"
spec:
  azureName: ${CLUSTER_NAME}
  dnsPrefix: ${CLUSTER_NAME}
  enableRBAC: true
  identity:
    type: SystemAssigned
  kubernetesVersion: "${KUBERNETES_VERSION}"
  linuxProfile:
    adminUsername: azureuser
    ssh:
      publicKeys:
      - keyData: "${SSH_PUBLIC_KEY}"
  location: ${LOCATION}
  networkProfile:
    dnsServiceIP: ${DNS_SERVICE_IP}
    loadBalancerSku: standard
    networkPlugin: azure
    serviceCidr: ${SERVICE_CIDR}
  nodeResourceGroup: MC_${CLUSTER_NAME}_${CLUSTER_NAME}_${LOCATION}
  owner:
    name: ${CLUSTER_NAME}
  servicePrincipalProfile:
    clientId: msi
  sku:
    name: Base
    tier: Free
EOF
kubectl wait --for=condition=Ready managedcluster.containerservice.azure.com/${CLUSTER_NAME} -n ${NEW_NAMESPACE} --timeout=180s
kubectl get clusters.cluster.x-k8s.io,azureasomanagedcontrolplanes,azureasomanagedclusters -n ${NEW_NAMESPACE}

# ============================================================
# Step 5c: Create ManagedClustersAgentPools with adopt annotation
# ============================================================
# Same pattern as the ManagedCluster above, but for the node pools. Each agent pool
# resource also gets the adopt annotation. CAPZ's AgentPoolAdoptReconciler watches
# for these and scaffolds:
#   - A MachinePool (the CAPI object representing a group of identical machines)
#   - An AzureASOManagedMachinePool (wraps the ManagedClustersAgentPool ASO resource)
#
# Important: these MUST be created AFTER the ManagedCluster adoption completes,
# because the AgentPoolAdoptReconciler needs the ManagedCluster to already be owned
# by an AzureASOManagedControlPlane. The owner reference chain is:
#   ManagedClustersAgentPool → ManagedCluster (via spec.owner)
#   AgentPoolAdoptReconciler looks up ManagedCluster → finds its CAPZ controller owner
#   → uses that to set up the MachinePool/AzureASOManagedMachinePool correctly.
cat <<'EOF' | kubectl apply -f -
apiVersion: containerservice.azure.com/v1api20240901
kind: ManagedClustersAgentPool
metadata:
  name: ${CLUSTER_NAME}-pool0
  namespace: ${NEW_NAMESPACE}
  annotations:
    serviceoperator.azure.com/credential-from: ${ASO_CREDENTIAL_SECRET}
    sigs.k8s.io/cluster-api-provider-azure-adopt: "true"
spec:
  azureName: pool0
  count: 2
  enableAutoScaling: false
  mode: System
  orchestratorVersion: "${KUBERNETES_VERSION}"
  osDiskSizeGB: 0
  osDiskType: Managed
  osType: Linux
  owner:
    name: ${CLUSTER_NAME}
  type: VirtualMachineScaleSets
  vmSize: ${VM_SIZE}
---
apiVersion: containerservice.azure.com/v1api20240901
kind: ManagedClustersAgentPool
metadata:
  name: ${CLUSTER_NAME}-pool1
  namespace: ${NEW_NAMESPACE}
  annotations:
    serviceoperator.azure.com/credential-from: ${ASO_CREDENTIAL_SECRET}
    sigs.k8s.io/cluster-api-provider-azure-adopt: "true"
spec:
  azureName: pool1
  count: 2
  enableAutoScaling: false
  mode: User
  orchestratorVersion: "${KUBERNETES_VERSION}"
  osDiskSizeGB: 0
  osDiskType: Managed
  osType: Linux
  owner:
    name: ${CLUSTER_NAME}
  type: VirtualMachineScaleSets
  vmSize: ${VM_SIZE}
EOF
kubectl wait --for=condition=Ready managedclustersagentpool.containerservice.azure.com/${CLUSTER_NAME}-pool0 -n ${NEW_NAMESPACE} --timeout=120s
kubectl wait --for=condition=Ready managedclustersagentpool.containerservice.azure.com/${CLUSTER_NAME}-pool1 -n ${NEW_NAMESPACE} --timeout=120s

# ============================================================
# Step 5d: Wait for new cluster to be fully available
# ============================================================
# At this point, CAPZ has created all the new CAPI resources and is reconciling them.
# The AzureASOManagedControlPlane controller generates a kubeconfig secret, connects
# to the workload cluster to verify it's reachable, and reports the cluster as
# Available. We wait for this to confirm the new management plane is fully working
# before tearing down the old one.
kubectl wait --for=condition=Available cluster.cluster.x-k8s.io/${CLUSTER_NAME} -n ${NEW_NAMESPACE} --timeout=300s
kubectl get clusters.cluster.x-k8s.io,azureasomanagedcontrolplanes,azureasomanagedclusters,machinepools,azureasomanagedmachinepools -n ${NEW_NAMESPACE} -o wide

# ============================================================
# Step 6: Prevent old ASO resources from deleting Azure resources
# ============================================================
# THIS IS THE MOST CRITICAL SAFETY STEP.
#
# The old ASO resources (ResourceGroup, ManagedCluster, ManagedClustersAgentPools) in
# the default namespace have:
#   1. Owner references pointing to old CAPZ resources (AzureManagedControlPlane, etc.)
#   2. ASO finalizers (serviceoperator.azure.com/finalizer)
#
# Without this step, the chain of destruction would be:
#   Delete old AzureManagedControlPlane → Kubernetes GC sees the old ASO ManagedCluster
#   has a dangling ownerReference → GC tries to delete the ASO ManagedCluster →
#   ASO's finalizer kicks in → ASO sends a DELETE to the Azure API →
#   YOUR ACTUAL AKS CLUSTER GETS DELETED
#
# We prevent this by:
#   - Setting reconcile-policy to "skip" (tells ASO to ignore this resource entirely)
#   - Removing the ASO finalizer (so the K8s object can be deleted without ASO acting)
# After this, the old ASO objects are inert — they can be garbage collected safely.
for pool in ${CLUSTER_NAME}-pool0 ${CLUSTER_NAME}-pool1; do
  kubectl patch managedclustersagentpool.containerservice.azure.com/$pool -n default \
    --type merge -p '{"metadata": {"annotations": {"serviceoperator.azure.com/reconcile-policy": "skip"}, "finalizers": null}}'
done
kubectl patch managedcluster.containerservice.azure.com/${CLUSTER_NAME} -n default \
  --type merge -p '{"metadata": {"annotations": {"serviceoperator.azure.com/reconcile-policy": "skip"}, "finalizers": null}}'
kubectl patch resourcegroup.resources.azure.com/${CLUSTER_NAME} -n default \
  --type merge -p '{"metadata": {"annotations": {"serviceoperator.azure.com/reconcile-policy": "skip"}, "finalizers": null}}'

# ============================================================
# Step 7: Delete old CAPI/CAPZ resources
# ============================================================
# Now we delete the old CAPI/CAPZ resources. We use --wait=false because CAPI
# controllers refuse to reconcile paused resources, even for deletion. The resources
# will sit in a "Terminating" state because they have finalizers that the controllers
# won't process (since the cluster is paused).
#
# So after issuing the deletes, we manually remove the finalizers from each resource.
# This lets Kubernetes complete the deletion immediately. The order matters:
#   1. Delete all resources (they enter Terminating state)
#   2. Remove finalizers bottom-up (infra resources → machine pools → control plane → cluster)
#
# The old ASO resources (ResourceGroup, ManagedCluster, AgentPools) will be garbage
# collected automatically because their owner references point to the CAPZ resources
# we're deleting. Since we already disarmed them in Step 6, this is safe.
kubectl delete cluster ${CLUSTER_NAME} -n default --wait=false
kubectl delete azuremanagedcluster ${CLUSTER_NAME} -n default --wait=false
kubectl delete azuremanagedcontrolplane ${CLUSTER_NAME} -n default --wait=false
kubectl delete machinepool ${CLUSTER_NAME}-pool0 ${CLUSTER_NAME}-pool1 -n default --wait=false
kubectl delete azuremanagedmachinepool ${CLUSTER_NAME}-pool0 ${CLUSTER_NAME}-pool1 -n default --wait=false
kubectl patch azuremanagedmachinepool ${CLUSTER_NAME}-pool0 -n default --type merge -p '{"metadata": {"finalizers": null}}'
kubectl patch azuremanagedmachinepool ${CLUSTER_NAME}-pool1 -n default --type merge -p '{"metadata": {"finalizers": null}}'
kubectl patch machinepool ${CLUSTER_NAME}-pool0 -n default --type merge -p '{"metadata": {"finalizers": null}}'
kubectl patch machinepool ${CLUSTER_NAME}-pool1 -n default --type merge -p '{"metadata": {"finalizers": null}}'
kubectl patch azuremanagedcontrolplane ${CLUSTER_NAME} -n default --type merge -p '{"metadata": {"finalizers": null}}'
kubectl patch cluster ${CLUSTER_NAME} -n default --type merge -p '{"metadata": {"finalizers": null}}'

# ============================================================
# Step 8: Verify
# ============================================================
# Confirm:
#   1. All old resources are gone from the default namespace (CAPI and ASO)
#   2. New resources are healthy in ${NEW_NAMESPACE} namespace
#   3. The workload cluster is still accessible with the same nodes and workloads
kubectl get clusters.cluster.x-k8s.io,azuremanagedcontrolplanes,azuremanagedclusters,machinepools,azuremanagedmachinepools -n default
kubectl get managedclusters.containerservice.azure.com,managedclustersagentpools.containerservice.azure.com,resourcegroups.resources.azure.com -n default
kubectl get clusters.cluster.x-k8s.io,azureasomanagedcontrolplanes,azureasomanagedclusters,machinepools,azureasomanagedmachinepools -n ${NEW_NAMESPACE} -o wide
kubectl get secret ${CLUSTER_NAME}-kubeconfig -n ${NEW_NAMESPACE} -o jsonpath='{.data.value}' | base64 -d > /tmp/${CLUSTER_NAME}-new.kubeconfig
KUBECONFIG=/tmp/${CLUSTER_NAME}-new.kubeconfig kubectl get nodes
