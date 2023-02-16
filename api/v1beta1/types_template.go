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
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/net"
)

// AzureManagedControlPlaneTemplateResourceSpec specifies an Azure managed control plane template resource.
type AzureManagedControlPlaneTemplateResourceSpec struct {
	// Version defines the desired Kubernetes version.
	// +kubebuilder:validation:MinLength:=2
	Version string `json:"version"`

	// VirtualNetwork describes the vnet for the AKS cluster. Will be created if it does not exist.
	// +optional
	VirtualNetwork ManagedControlPlaneVirtualNetworkTemplate `json:"virtualNetwork,omitempty"`

	// SubscriptionID is the GUID of the Azure subscription to hold this cluster.
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
	APIServerAccessProfile *APIServerAccessProfileTemplate `json:"apiServerAccessProfile,omitempty"`

	// AutoscalerProfile is the parameters to be applied to the cluster-autoscaler when enabled
	// +optional
	AutoScalerProfile *AutoScalerProfile `json:"autoscalerProfile,omitempty"`
}

type APIServerAccessProfileTemplate struct {
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

type ManagedControlPlaneVirtualNetworkTemplate struct {
	Name      string `json:"name"`
	CIDRBlock string `json:"cidrBlock"`
	// +optional
	Subnet ManagedControlPlaneSubnet `json:"subnet,omitempty"`
}

// AzureManagedClusterTemplateResourceSpec specifies an Azure managed cluster template resource.
type AzureManagedClusterTemplateResourceSpec struct{}

// AzureClusterTemplateResourceSpec specifies an Azure cluster template resource.
type AzureClusterTemplateResourceSpec struct {
	AzureClusterClassSpec `json:",inline"`

	// NetworkSpec encapsulates all things related to Azure network.
	// +optional
	NetworkSpec NetworkTemplateSpec `json:"networkSpec,omitempty"`

	// BastionSpec encapsulates all things related to the Bastions in the cluster.
	// +optional
	BastionSpec BastionTemplateSpec `json:"bastionSpec,omitempty"`
}

// NetworkTemplateSpec specifies a network template.
type NetworkTemplateSpec struct {
	NetworkClassSpec `json:",inline"`

	// Vnet is the configuration for the Azure virtual network.
	// +optional
	Vnet VnetTemplateSpec `json:"vnet,omitempty"`

	// Subnets is the configuration for the control-plane subnet and the node subnet.
	// +optional
	Subnets SubnetTemplatesSpec `json:"subnets,omitempty"`

	// APIServerLB is the configuration for the control-plane load balancer.
	// +optional
	APIServerLB LoadBalancerClassSpec `json:"apiServerLB,omitempty"`

	// NodeOutboundLB is the configuration for the node outbound load balancer.
	// +optional
	NodeOutboundLB *LoadBalancerClassSpec `json:"nodeOutboundLB,omitempty"`

	// ControlPlaneOutboundLB is the configuration for the control-plane outbound load balancer.
	// This is different from APIServerLB, and is used only in private clusters (optionally) for enabling outbound traffic.
	// +optional
	ControlPlaneOutboundLB *LoadBalancerClassSpec `json:"controlPlaneOutboundLB,omitempty"`
}

// GetControlPlaneSubnetTemplate returns the cluster control plane subnet template.
func (n *NetworkTemplateSpec) GetControlPlaneSubnetTemplate() (SubnetTemplateSpec, error) {
	for _, sn := range n.Subnets {
		if sn.Role == SubnetControlPlane {
			return sn, nil
		}
	}
	return SubnetTemplateSpec{}, errors.Errorf("no subnet template found with role %s", SubnetControlPlane)
}

// UpdateControlPlaneSubnetTemplate updates the cluster control plane subnet template.
func (n *NetworkTemplateSpec) UpdateControlPlaneSubnetTemplate(subnet SubnetTemplateSpec) {
	for i, sn := range n.Subnets {
		if sn.Role == SubnetControlPlane {
			n.Subnets[i] = subnet
		}
	}
}

// VnetTemplateSpec defines the desired state of a virtual network.
type VnetTemplateSpec struct {
	VnetClassSpec `json:",inline"`

	// Peerings defines a list of peerings of the newly created virtual network with existing virtual networks.
	// +optional
	Peerings VnetPeeringsTemplateSpec `json:"peerings,omitempty"`
}

// VnetPeeringsTemplateSpec defines a list of peerings of the newly created virtual network with existing virtual networks.
type VnetPeeringsTemplateSpec []VnetPeeringClassSpec

// SubnetTemplateSpec specifies a template for a subnet.
type SubnetTemplateSpec struct {
	SubnetClassSpec `json:",inline"`

	// SecurityGroup defines the NSG (network security group) that should be attached to this subnet.
	// +optional
	SecurityGroup SecurityGroupClass `json:"securityGroup,omitempty"`

	// NatGateway associated with this subnet.
	// +optional
	NatGateway NatGatewayClassSpec `json:"natGateway,omitempty"`
}

// IsNatGatewayEnabled returns true if the NAT gateway is enabled.
func (s SubnetTemplateSpec) IsNatGatewayEnabled() bool {
	return s.NatGateway.Name != ""
}

// IsIPv6Enabled returns whether or not IPv6 is enabled on the subnet.
func (s SubnetTemplateSpec) IsIPv6Enabled() bool {
	for _, cidr := range s.CIDRBlocks {
		if net.IsIPv6CIDRString(cidr) {
			return true
		}
	}
	return false
}

// SubnetTemplatesSpec specifies a list of subnet templates.
// +listType=map
// +listMapKey=name
type SubnetTemplatesSpec []SubnetTemplateSpec

// BastionTemplateSpec specifies a template for a bastion host.
type BastionTemplateSpec struct {
	// +optional
	AzureBastion *AzureBastionTemplateSpec `json:"azureBastion,omitempty"`
}

// AzureBastionTemplateSpec specifies a template for an Azure Bastion host.
type AzureBastionTemplateSpec struct {
	// +optional
	Subnet SubnetTemplateSpec `json:"subnet,omitempty"`
}
