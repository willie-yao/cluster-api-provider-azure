/*
Copyright 2022 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1beta1

import (
	corev1 "k8s.io/api/core/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// AzureClusterClassSpec defines the AzureCluster properties that may be shared across several Azure clusters.
type AzureClusterClassSpec struct {
	// +optional
	SubscriptionID string `json:"subscriptionID,omitempty"`

	Location string `json:"location"`

	// ExtendedLocation is an optional set of ExtendedLocation properties for clusters on Azure public MEC.
	// +optional
	ExtendedLocation *ExtendedLocationSpec `json:"extendedLocation,omitempty"`

	// AdditionalTags is an optional set of tags to add to Azure resources managed by the Azure provider, in addition to the
	// ones added by default.
	// +optional
	AdditionalTags Tags `json:"additionalTags,omitempty"`

	// IdentityRef is a reference to an AzureIdentity to be used when reconciling this cluster
	// +optional
	IdentityRef *corev1.ObjectReference `json:"identityRef,omitempty"`

	// AzureEnvironment is the name of the AzureCloud to be used.
	// The default value that would be used by most users is "AzurePublicCloud", other values are:
	// - ChinaCloud: "AzureChinaCloud"
	// - GermanCloud: "AzureGermanCloud"
	// - PublicCloud: "AzurePublicCloud"
	// - USGovernmentCloud: "AzureUSGovernmentCloud"
	// +optional
	AzureEnvironment string `json:"azureEnvironment,omitempty"`

	// CloudProviderConfigOverrides is an optional set of configuration values that can be overridden in azure cloud provider config.
	// This is only a subset of options that are available in azure cloud provider config.
	// Some values for the cloud provider config are inferred from other parts of cluster api provider azure spec, and may not be available for overrides.
	// See: https://cloud-provider-azure.sigs.k8s.io/install/configs
	// Note: All cloud provider config values can be customized by creating the secret beforehand. CloudProviderConfigOverrides is only used when the secret is managed by the Azure Provider.
	// +optional
	CloudProviderConfigOverrides *CloudProviderConfigOverrides `json:"cloudProviderConfigOverrides,omitempty"`

	// FailureDomains is a list of failure domains in the cluster's region, used to restrict
	// eligibility to host the control plane. A FailureDomain maps to an availability zone,
	// which is a separated group of datacenters within a region.
	// See: https://learn.microsoft.com/azure/reliability/availability-zones-overview
	// +optional
	FailureDomains clusterv1.FailureDomains `json:"failureDomains,omitempty"`
}

// AzureManagedControlPlaneClassSpec defines the AzureManagedControlPlane properties that may be shared across several azure managed control planes.
type AzureManagedControlPlaneClassSpec struct {
	// MachineTemplate contains information about how machines
	// should be shaped when creating or updating a control plane.
	// For the AzureManagedControlPlaneTemplate, this field is used
	// only to fulfill the CAPI contract.
	// +optional
	MachineTemplate *AzureManagedControlPlaneTemplateMachineTemplate `json:"machineTemplate,omitempty"`

	// Version defines the desired Kubernetes version.
	// +kubebuilder:validation:MinLength:=2
	Version string `json:"version"`

	// VirtualNetwork describes the virtual network for the AKS cluster. It will be created if it does not already exist.
	// +optional
	VirtualNetwork ManagedControlPlaneVirtualNetwork `json:"virtualNetwork,omitempty"`

	// SubscriptionID is the GUID of the Azure subscription that owns this cluster.
	// +optional
	SubscriptionID string `json:"subscriptionID,omitempty"`

	// Location is a string matching one of the canonical Azure region names. Examples: "westus2", "eastus".
	Location string `json:"location"`

	// AdditionalTags is an optional set of tags to add to Azure resources managed by the Azure provider, in addition to the
	// ones added by default.
	// +optional
	AdditionalTags Tags `json:"additionalTags,omitempty"`

	// NetworkPlugin used for building Kubernetes network.
	// +kubebuilder:validation:Enum=azure;kubenet
	// +optional
	NetworkPlugin *string `json:"networkPlugin,omitempty"`

	// NetworkPolicy used for building Kubernetes network.
	// +kubebuilder:validation:Enum=azure;calico
	// +optional
	NetworkPolicy *string `json:"networkPolicy,omitempty"`

	// Outbound configuration used by Nodes.
	// +kubebuilder:validation:Enum=loadBalancer;managedNATGateway;userAssignedNATGateway;userDefinedRouting
	// +optional
	OutboundType *ManagedControlPlaneOutboundType `json:"outboundType,omitempty"`

	// DNSServiceIP is an IP address assigned to the Kubernetes DNS service.
	// It must be within the Kubernetes service address range specified in serviceCidr.
	// +optional
	DNSServiceIP *string `json:"dnsServiceIP,omitempty"`

	// LoadBalancerSKU is the SKU of the loadBalancer to be provisioned.
	// +kubebuilder:validation:Enum=Basic;Standard
	// +optional
	LoadBalancerSKU *string `json:"loadBalancerSKU,omitempty"`

	// IdentityRef is a reference to a AzureClusterIdentity to be used when reconciling this cluster
	// +optional
	IdentityRef *corev1.ObjectReference `json:"identityRef,omitempty"`

	// AadProfile is Azure Active Directory configuration to integrate with AKS for aad authentication.
	// +optional
	AADProfile *AADProfile `json:"aadProfile,omitempty"`

	// AddonProfiles are the profiles of managed cluster add-on.
	// +optional
	AddonProfiles []AddonProfile `json:"addonProfiles,omitempty"`

	// SKU is the SKU of the AKS to be provisioned.
	// +optional
	SKU *AKSSku `json:"sku,omitempty"`

	// LoadBalancerProfile is the profile of the cluster load balancer.
	// +optional
	LoadBalancerProfile *LoadBalancerProfile `json:"loadBalancerProfile,omitempty"`

	// APIServerAccessProfile is the access profile for AKS API server.
	// +optional
	APIServerAccessProfile *APIServerAccessProfile `json:"apiServerAccessProfile,omitempty"`

	// AutoscalerProfile is the parameters to be applied to the cluster-autoscaler when enabled
	// +optional
	AutoScalerProfile *AutoScalerProfile `json:"autoscalerProfile,omitempty"`
}

type ManagedControlPlaneVirtualNetworkClassSpec struct {
	Name      string `json:"name"`
	CIDRBlock string `json:"cidrBlock"`
	// +optional
	Subnet ManagedControlPlaneSubnet `json:"subnet,omitempty"`
}

type APIServerAccessProfileClassSpec struct {
	// EnablePrivateCluster - Whether to create the cluster as a private cluster or not.
	// +optional
	EnablePrivateCluster *bool `json:"enablePrivateCluster,omitempty"`
	// PrivateDNSZone - Private dns zone mode for private cluster.
	// +kubebuilder:validation:Enum=System;None
	// +optional
	PrivateDNSZone *string `json:"privateDNSZone,omitempty"`
	// EnablePrivateClusterPublicFQDN - Whether to create additional public FQDN for private cluster or not.
	// +optional
	EnablePrivateClusterPublicFQDN *bool `json:"enablePrivateClusterPublicFQDN,omitempty"`
}

// ExtendedLocationSpec defines the ExtendedLocation properties to enable CAPZ for Azure public MEC.
type ExtendedLocationSpec struct {
	// Name defines the name for the extended location.
	Name string `json:"name"`

	// Type defines the type for the extended location.
	// +kubebuilder:validation:Enum=EdgeZone
	Type string `json:"type"`
}

// NetworkClassSpec defines the NetworkSpec properties that may be shared across several Azure clusters.
type NetworkClassSpec struct {
	// PrivateDNSZoneName defines the zone name for the Azure Private DNS.
	// +optional
	PrivateDNSZoneName string `json:"privateDNSZoneName,omitempty"`
}

// VnetClassSpec defines the VnetSpec properties that may be shared across several Azure clusters.
type VnetClassSpec struct {
	// CIDRBlocks defines the virtual network's address space, specified as one or more address prefixes in CIDR notation.
	// +optional
	CIDRBlocks []string `json:"cidrBlocks,omitempty"`

	// Tags is a collection of tags describing the resource.
	// +optional
	Tags Tags `json:"tags,omitempty"`
}

// SubnetClassSpec defines the SubnetSpec properties that may be shared across several Azure clusters.
type SubnetClassSpec struct {
	// Name defines a name for the subnet resource.
	Name string `json:"name"`

	// Role defines the subnet role (eg. Node, ControlPlane)
	// +kubebuilder:validation:Enum=node;control-plane;bastion
	Role SubnetRole `json:"role"`

	// CIDRBlocks defines the subnet's address space, specified as one or more address prefixes in CIDR notation.
	// +optional
	CIDRBlocks []string `json:"cidrBlocks,omitempty"`

	// ServiceEndpoints is a slice of Virtual Network service endpoints to enable for the subnets.
	// +optional
	ServiceEndpoints ServiceEndpoints `json:"serviceEndpoints,omitempty"`

	// PrivateEndpoints defines a list of private endpoints that should be attached to this subnet.
	// +optional
	PrivateEndpoints PrivateEndpoints `json:"privateEndpoints,omitempty"`
}

// LoadBalancerClassSpec defines the LoadBalancerSpec properties that may be shared across several Azure clusters.
type LoadBalancerClassSpec struct {
	// +optional
	SKU SKU `json:"sku,omitempty"`
	// +optional
	Type LBType `json:"type,omitempty"`
	// IdleTimeoutInMinutes specifies the timeout for the TCP idle connection.
	// +optional
	IdleTimeoutInMinutes *int32 `json:"idleTimeoutInMinutes,omitempty"`
}

// SecurityGroupClass defines the SecurityGroup properties that may be shared across several Azure clusters.
type SecurityGroupClass struct {
	// +optional
	SecurityRules SecurityRules `json:"securityRules,omitempty"`
	// +optional
	Tags Tags `json:"tags,omitempty"`
}

// FrontendIPClass defines the FrontendIP properties that may be shared across several Azure clusters.
type FrontendIPClass struct {
	// +optional
	PrivateIPAddress string `json:"privateIP,omitempty"`
}

// setDefaults sets default values for AzureClusterClassSpec.
func (acc *AzureClusterClassSpec) setDefaults() {
	if acc.AzureEnvironment == "" {
		acc.AzureEnvironment = DefaultAzureCloud
	}
}

// setDefaults sets default values for AzureManagedControlPlaneSpec.
func (amcp *AzureManagedControlPlaneSpec) setDefaults() {
	if amcp.AzureEnvironment == "" {
		amcp.AzureEnvironment = DefaultAzureCloud
	}
}

// setDefaults sets default values for VnetClassSpec.
func (vc *VnetClassSpec) setDefaults() {
	if len(vc.CIDRBlocks) == 0 {
		vc.CIDRBlocks = []string{DefaultVnetCIDR}
	}
}

// setDefaults sets default values for SubnetClassSpec.
func (sc *SubnetClassSpec) setDefaults(cidr string) {
	if len(sc.CIDRBlocks) == 0 {
		sc.CIDRBlocks = []string{cidr}
	}
}

// setDefaults sets default values for SecurityGroupClass.
func (sgc *SecurityGroupClass) setDefaults() {
	for i := range sgc.SecurityRules {
		if sgc.SecurityRules[i].Direction == "" {
			sgc.SecurityRules[i].Direction = SecurityRuleDirectionInbound
		}
	}
}
