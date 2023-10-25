/*
Copyright 2023 The Kubernetes Authors.

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
	"strings"
)

func (mcp *AzureManagedControlPlaneTemplate) setDefaults() {
	if mcp.Spec.Template.Spec.NetworkPlugin == nil {
		networkPlugin := AzureNetworkPluginName
		mcp.Spec.Template.Spec.NetworkPlugin = &networkPlugin
	}
	if mcp.Spec.Template.Spec.LoadBalancerSKU == nil {
		loadBalancerSKU := "Standard"
		mcp.Spec.Template.Spec.LoadBalancerSKU = &loadBalancerSKU
	}

	if mcp.Spec.Template.Spec.Version != "" && !strings.HasPrefix(mcp.Spec.Template.Spec.Version, "v") {
		normalizedVersion := "v" + mcp.Spec.Template.Spec.Version
		mcp.Spec.Template.Spec.Version = normalizedVersion
	}

	mcp.setDefaultVirtualNetwork()
	mcp.setDefaultSubnet()
	setDefaultSku(mcp.Spec.Template.Spec.SKU)
	setDefaultAutoScalerProfile(mcp.Spec.Template.Spec.AutoScalerProfile)
}

// setDefaultVirtualNetwork sets the default VirtualNetwork for an AzureManagedControlPlaneTemplate.
func (mcp *AzureManagedControlPlaneTemplate) setDefaultVirtualNetwork() {
	if mcp.Spec.Template.Spec.VirtualNetwork.Name == "" {
		mcp.Spec.Template.Spec.VirtualNetwork.Name = mcp.Name
	}
	if mcp.Spec.Template.Spec.VirtualNetwork.CIDRBlock == "" {
		mcp.Spec.Template.Spec.VirtualNetwork.CIDRBlock = defaultAKSVnetCIDR
	}
}

// setDefaultSubnet sets the default Subnet for an AzureManagedControlPlaneTemplate.
func (mcp *AzureManagedControlPlaneTemplate) setDefaultSubnet() {
	if mcp.Spec.Template.Spec.VirtualNetwork.Subnet.Name == "" {
		mcp.Spec.Template.Spec.VirtualNetwork.Subnet.Name = mcp.Name
	}
	if mcp.Spec.Template.Spec.VirtualNetwork.Subnet.CIDRBlock == "" {
		mcp.Spec.Template.Spec.VirtualNetwork.Subnet.CIDRBlock = defaultAKSNodeSubnetCIDR
	}
}
