// CAPZ Alpha Kubernetes Preview — single-deploy Bicep template.
//
// Provisions an AKS management cluster, installs CAPI Operator + CAPZ via
// a deploymentScript, and asks CAPZ to build an alpha-version Kubernetes
// workload cluster in the same resource group. The workload cluster
// downloads alpha kubelet/kubeadm binaries from dl.k8s.io/ci at boot and
// enables AllAlpha=true,AllBeta=true,api/all=true so users can play with
// every gate-only alpha feature with no per-release maintenance.
//
// Scope is resourceGroup: create the RG first, then deploy this template
// into it. Cleanup is `az group delete -n <rg>`.

targetScope = 'resourceGroup'

// ---------- Parameters ----------

@description('Public for default Azure subscriptions. Internal for MSFT-internal subs that require peered VNet + apiserver-ILB and have NRMS NSG policy applied.')
@allowed([
  'public'
  'internal'
])
param networkingMode string = 'public'

@description('Kubernetes alpha CI version (e.g. v1.37.0-alpha.0). Pick from https://github.com/kubernetes/kubernetes/releases (the alpha stream lives on the next minor; 1.36.x is in RC, so alpha features land in 1.37+).')
param ciVersion string = 'v1.37.0-alpha.0'

@description('Override the default AllAlpha/AllBeta feature-gate string. Leave blank to use the defaults.')
param featureGatesOverride string = ''

@description('Space-separated list of opt-in feature recipes to apply (filenames in templates/preview/features without the .yaml). Currently supports: dra.')
param extraFeatureRecipes string = ''

@description('Replica count for the workload cluster control plane. Internal mode is auto-bumped to 3 at deploy time for apiserver-ILB hairpin routing if you leave this at 1.')
@allowed([
  1
  3
  5
])
param controlPlaneMachineCount int = 1

@description('Replica count for the workload cluster worker MachineDeployment.')
@minValue(0)
@maxValue(10)
param workerMachineCount int = 2

@description('AKS Kubernetes version for the management cluster (NOT the workload cluster).')
param aksKubernetesVersion string = '1.33.2'

@description('AKS node VM size. Defaults to Standard_D2s_v3 because Standard_B2s and other burstable SKUs are policy-restricted in some subscriptions/regions and fail AKS preflight validation with "VM size … is not allowed in your subscription in location …". Switch to a burstable SKU (e.g. Standard_B2s) on subs that allow it to save cost.')
param aksNodeVmSize string = 'Standard_D2s_v3'

@description('AKS node count.')
@minValue(1)
@maxValue(5)
param aksNodeCount int = 2

@description('Control plane VM size for the workload cluster.')
param workloadControlPlaneVmSize string = 'Standard_D2s_v3'

@description('Worker VM size for the workload cluster.')
param workloadNodeVmSize string = 'Standard_D2s_v3'

@description('CIDR for the AKS management VNet. Only used in internal mode.')
param mgmtVnetCidr string = '10.100.0.0/16'

@description('CIDR for the AKS subnet. Only used in internal mode.')
param aksSubnetCidr string = '10.100.0.0/24'

@description('CIDR for the workload cluster VNet. Only used in internal mode.')
param workloadVnetCidr string = '10.200.0.0/16'

@description('CIDR for the workload cluster control plane subnet. Only used in internal mode.')
param workloadCpSubnetCidr string = '10.200.0.0/24'

@description('CIDR for the workload cluster node subnet. Only used in internal mode.')
param workloadNodeSubnetCidr string = '10.200.1.0/24'

@description('Private IP for the workload cluster apiserver ILB frontend. Must be within workloadCpSubnetCidr and outside its first 4 addresses. Only used in internal mode.')
param workloadApiserverIlbIp string = '10.200.0.100'

@description('Repository hosting the templates/preview tree (kustomize + features). The deploymentScript clones this at the ref below. Defaults point at the preview fork until this lands upstream.')
param previewRepoUrl string = 'https://github.com/willie-yao/cluster-api-provider-azure.git'

@description('Git ref (branch or tag) to clone from previewRepoUrl. Pin to a release tag for reproducible deploys.')
param previewRepoRef string = 'alpha-features'

@description('Workload cluster name (also used as the Cluster CR name).')
param workloadClusterName string = 'capz-preview'

// ---------- Variables ----------

var nameSuffix               = uniqueString(resourceGroup().id, deployment().name)
var location                 = resourceGroup().location
var bootstrapForceUpdateTag  = deployment().name
var effectiveControlPlaneMachineCount = (networkingMode == 'internal' && controlPlaneMachineCount < 3) ? 3 : controlPlaneMachineCount

var aksName              = 'capz-preview-aks-${nameSuffix}'
var aksNodeRg            = '${resourceGroup().name}-aks-nodes'
var uamiName             = 'capz-preview-mi-${nameSuffix}'
var keyVaultName         = take('capzpv${nameSuffix}', 24)
var mgmtVnetName         = 'mgmt-vnet'
var aksSubnetName        = 'aks-subnet'
var workloadVnetName     = 'workload-vnet'
var cpSubnetName         = 'control-plane-subnet'
var nodeSubnetName       = 'node-subnet'
var privateDnsZoneName   = '${location}.cloudapp.azure.com'
var apiserverDnsLabel    = 'apiserver'

var subscriptionId = subscription().subscriptionId
var tenantId       = subscription().tenantId

// Built-in role definition IDs (well-known).
var roleContributor             = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b24988ac-6180-42a0-ab88-20f7382dd24c')
var roleKeyVaultSecretsOfficer  = subscriptionResourceId('Microsoft.Authorization/roleDefinitions', 'b86a8fe4-44ce-4948-aee5-eccb2c155cd7')

// ---------- Networking (internal mode only) ----------

resource mgmtVnet 'Microsoft.Network/virtualNetworks@2023-11-01' = if (networkingMode == 'internal') {
  name: mgmtVnetName
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: [
        mgmtVnetCidr
      ]
    }
    subnets: [
      {
        name: aksSubnetName
        properties: {
          addressPrefix: aksSubnetCidr
        }
      }
    ]
  }
}

resource workloadVnet 'Microsoft.Network/virtualNetworks@2023-11-01' = if (networkingMode == 'internal') {
  name: workloadVnetName
  location: location
  properties: {
    addressSpace: {
      addressPrefixes: [
        workloadVnetCidr
      ]
    }
    subnets: [
      {
        name: cpSubnetName
        properties: {
          addressPrefix: workloadCpSubnetCidr
        }
      }
      {
        name: nodeSubnetName
        properties: {
          addressPrefix: workloadNodeSubnetCidr
        }
      }
    ]
  }
}

resource peerMgmtToWorkload 'Microsoft.Network/virtualNetworks/virtualNetworkPeerings@2023-11-01' = if (networkingMode == 'internal') {
  parent: mgmtVnet
  name: 'mgmt-to-workload'
  properties: {
    remoteVirtualNetwork: {
      id: workloadVnet.id
    }
    allowVirtualNetworkAccess: true
    allowForwardedTraffic: true
    allowGatewayTransit: false
    useRemoteGateways: false
  }
}

resource peerWorkloadToMgmt 'Microsoft.Network/virtualNetworks/virtualNetworkPeerings@2023-11-01' = if (networkingMode == 'internal') {
  parent: workloadVnet
  name: 'workload-to-mgmt'
  properties: {
    remoteVirtualNetwork: {
      id: mgmtVnet.id
    }
    allowVirtualNetworkAccess: true
    allowForwardedTraffic: true
    allowGatewayTransit: false
    useRemoteGateways: false
  }
}

resource privateDnsZone 'Microsoft.Network/privateDnsZones@2024-06-01' = if (networkingMode == 'internal') {
  name: privateDnsZoneName
  location: 'global'
}

resource privateDnsARecord 'Microsoft.Network/privateDnsZones/A@2024-06-01' = if (networkingMode == 'internal') {
  parent: privateDnsZone
  name: '${workloadClusterName}-${apiserverDnsLabel}'
  properties: {
    ttl: 300
    aRecords: [
      {
        ipv4Address: workloadApiserverIlbIp
      }
    ]
  }
}

resource privateDnsLinkMgmt 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = if (networkingMode == 'internal') {
  parent: privateDnsZone
  name: 'link-mgmt'
  location: 'global'
  properties: {
    virtualNetwork: {
      id: mgmtVnet.id
    }
    registrationEnabled: false
  }
}

resource privateDnsLinkWorkload 'Microsoft.Network/privateDnsZones/virtualNetworkLinks@2024-06-01' = if (networkingMode == 'internal') {
  parent: privateDnsZone
  name: 'link-workload'
  location: 'global'
  properties: {
    virtualNetwork: {
      id: workloadVnet.id
    }
    registrationEnabled: false
  }
}

// ---------- Identity ----------

resource uami 'Microsoft.ManagedIdentity/userAssignedIdentities@2023-01-31' = {
  name: uamiName
  location: location
}

resource uamiRgContributor 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(resourceGroup().id, uami.id, 'Contributor')
  scope: resourceGroup()
  properties: {
    principalId: uami.properties.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: roleContributor
  }
}

// ---------- AKS management cluster ----------

resource aks 'Microsoft.ContainerService/managedClusters@2024-05-01' = {
  name: aksName
  location: location
  identity: {
    type: 'SystemAssigned'
  }
  properties: {
    kubernetesVersion: aksKubernetesVersion
    dnsPrefix: aksName
    nodeResourceGroup: aksNodeRg
    enableRBAC: true
    oidcIssuerProfile: {
      enabled: true
    }
    securityProfile: {
      workloadIdentity: {
        enabled: true
      }
    }
    agentPoolProfiles: [
      {
        name: 'sys'
        count: aksNodeCount
        vmSize: aksNodeVmSize
        mode: 'System'
        osType: 'Linux'
        type: 'VirtualMachineScaleSets'
        vnetSubnetID: (networkingMode == 'internal') ? '${mgmtVnet.id}/subnets/${aksSubnetName}' : null
      }
    ]
    networkProfile: {
      networkPlugin: 'azure'
      serviceCidr: '10.0.0.0/16'
      dnsServiceIP: '10.0.0.10'
    }
  }
}

// Federated credential: AKS OIDC issuer → CAPZ controller SA in capz-system.
resource fedCredCapz 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  parent: uami
  name: 'capz-federated-identity'
  properties: {
    issuer: aks.properties.oidcIssuerProfile.issuerURL
    subject: 'system:serviceaccount:capz-system:capz-manager'
    audiences: [
      'api://AzureADTokenExchange'
    ]
  }
}

// Federated credential for ASO too — CAPZ uses ASO under the hood for
// managed-cluster (AKS) flavors and some IaaS resources. Cheap to create.
//
// NOTE: Azure MSI rejects concurrent federated-credential writes against the
// same UAMI with code `ConcurrentFederatedIdentityCredentialsWritesForSingleManagedIdentity`.
// Explicit `dependsOn` forces ARM to serialize these two FIC creations.
resource fedCredAso 'Microsoft.ManagedIdentity/userAssignedIdentities/federatedIdentityCredentials@2023-01-31' = {
  parent: uami
  name: 'aso-federated-identity'
  properties: {
    issuer: aks.properties.oidcIssuerProfile.issuerURL
    subject: 'system:serviceaccount:capz-system:azureserviceoperator-default'
    audiences: [
      'api://AzureADTokenExchange'
    ]
  }
  dependsOn: [
    fedCredCapz
  ]
}

// ---------- Key Vault ----------

resource keyVault 'Microsoft.KeyVault/vaults@2024-04-01-preview' = {
  name: keyVaultName
  location: location
  properties: {
    tenantId: tenantId
    sku: {
      family: 'A'
      name: 'standard'
    }
    enableRbacAuthorization: true
    enabledForDeployment: false
    enableSoftDelete: true
    softDeleteRetentionInDays: 7
    publicNetworkAccess: 'Enabled'
  }
}

resource kvSecretsOfficerForUami 'Microsoft.Authorization/roleAssignments@2022-04-01' = {
  name: guid(keyVault.id, uami.id, 'KeyVaultSecretsOfficer')
  scope: keyVault
  properties: {
    principalId: uami.properties.principalId
    principalType: 'ServicePrincipal'
    roleDefinitionId: roleKeyVaultSecretsOfficer
  }
}

// ---------- Bootstrap deployment script ----------

resource bootstrap 'Microsoft.Resources/deploymentScripts@2023-08-01' = {
  name: 'capz-preview-bootstrap-${nameSuffix}'
  location: location
  kind: 'AzureCLI'
  identity: {
    type: 'UserAssigned'
    userAssignedIdentities: {
      '${uami.id}': {}
    }
  }
  properties: {
    azCliVersion: '2.61.0'
    timeout: 'PT2H'
    retentionInterval: 'PT1H'
    cleanupPreference: 'OnSuccess'
    forceUpdateTag: bootstrapForceUpdateTag
    environmentVariables: [
      { name: 'AKS_NAME',                       value: aksName }
      { name: 'AKS_RG',                         value: resourceGroup().name }
      { name: 'AKS_NODE_RG',                    value: aksNodeRg }
      { name: 'UAMI_CLIENT_ID',                 value: uami.properties.clientId }
      { name: 'KV_NAME',                        value: keyVault.name }
      { name: 'AZURE_LOCATION',                 value: location }
      { name: 'AZURE_SUBSCRIPTION_ID',          value: subscriptionId }
      { name: 'AZURE_TENANT_ID',                value: tenantId }
      { name: 'CLUSTER_NAME',                   value: workloadClusterName }
      { name: 'CLUSTER_NAMESPACE',              value: 'default' }
      { name: 'CI_VERSION',                     value: ciVersion }
      { name: 'K8S_FEATURE_GATES',              value: empty(featureGatesOverride) ? 'AllAlpha=true,AllBeta=true' : featureGatesOverride }
      { name: 'K8S_RUNTIME_CONFIG',             value: 'api/all=true' }
      { name: 'CONTROL_PLANE_MACHINE_COUNT',    value: string(effectiveControlPlaneMachineCount) }
      { name: 'WORKER_MACHINE_COUNT',           value: string(workerMachineCount) }
      { name: 'AZURE_CONTROL_PLANE_MACHINE_TYPE', value: workloadControlPlaneVmSize }
      { name: 'AZURE_NODE_MACHINE_TYPE',        value: workloadNodeVmSize }
      { name: 'NETWORKING_MODE',                value: networkingMode }
      { name: 'AZURE_VNET_CIDR',                value: workloadVnetCidr }
      { name: 'AZURE_CP_SUBNET_CIDR',           value: workloadCpSubnetCidr }
      { name: 'AZURE_NODE_SUBNET_CIDR',         value: workloadNodeSubnetCidr }
      { name: 'AZURE_INTERNAL_LB_PRIVATE_IP',   value: workloadApiserverIlbIp }
      { name: 'APISERVER_LB_DNS_SUFFIX',        value: apiserverDnsLabel }
      { name: 'PREVIEW_REPO_URL',               value: previewRepoUrl }
      { name: 'PREVIEW_REPO_REF',               value: previewRepoRef }
      { name: 'EXTRA_FEATURE_RECIPES',          value: extraFeatureRecipes }
    ]
    scriptContent: loadTextContent('bootstrap.sh')
  }
  dependsOn: [
    // Role assignments must be effective before the script runs.
    uamiRgContributor
    kvSecretsOfficerForUami
    fedCredCapz
    fedCredAso
    // In internal mode, the workload VNet + private DNS must exist before
    // CAPZ tries to use them and before NSG-rule patching can run.
    peerMgmtToWorkload
    peerWorkloadToMgmt
    privateDnsARecord
    privateDnsLinkMgmt
    privateDnsLinkWorkload
  ]
}

// ---------- Outputs ----------

@description('Key Vault name where the workload cluster kubeconfig is stored.')
output kubeconfigVault string = keyVault.name

@description('Command to retrieve the workload cluster kubeconfig.')
output kubeconfigCommand string = 'az keyvault secret show --vault-name ${keyVault.name} --name workload-kubeconfig --query value -o tsv > workload.kubeconfig'

@description('Command to delete everything created by this template.')
output cleanupCommand string = 'az group delete --name ${resourceGroup().name} --yes --no-wait'

@description('AKS management cluster name. kubectl context after `az aks get-credentials -g <rg> -n <aksName>`.')
output aksClusterName string = aksName

@description('Workload cluster name (CAPI Cluster CR name in the mgmt cluster).')
output workloadClusterName string = workloadClusterName
