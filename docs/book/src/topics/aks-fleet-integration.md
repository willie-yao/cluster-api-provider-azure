# AKS Fleet Integration

CAPZ supports joining your managed AKS clusters to a single AKS fleet. Azure Kubernetes Fleet Manager (Fleet) enables at-scale management of multiple Azure Kubernetes Service (AKS) clusters. For more documentation on Azure Kubernetes Fleet Manager, refer [AKS Docs](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/overview)

## Joining a CAPZ Cluster to an AKS Fleet Manager

To join a CAPZ cluster to an AKS fleet, you must first create an AKS fleet manager. For more information on how to create an AKS fleet manager, refer [AKS Docs](https://learn.microsoft.com/en-us/azure/kubernetes-fleet/quickstart-create-fleet-and-members). This fleet manager will be your point of reference for managing any CAPZ clusters that you join to the fleet.

Once you have created an AKS fleet manager, you can join your CAPZ cluster to the fleet by adding the `fleetManager` field to your AzureManagedControlPlane resource spec:

```yaml
apiVersion: infrastructure.cluster.x-k8s.io/v1beta1
kind: AzureManagedControlPlane
metadata:
  name: ${CLUSTER_NAME}
  namespace: default
spec:
  fleetManager: 
    group: fleet-update-group
    managerName: fleet-manager-name
    managerResourceGroup: fleet-manager-resource-group
```

The `managerName` and `managerResourceGroup` fields are the name and resource group of your AKS fleet manager. The `group` field is the name of the update group for the cluster, not to be confused with the resource group.

When the `fleetManager` field is included, CAPZ will create an AKS fleet member resource which will join the CAPZ cluster to the AKS fleet. The AKS fleet member resource will be created in the same resource group as the CAPZ cluster.
