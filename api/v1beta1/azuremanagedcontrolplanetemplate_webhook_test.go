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
	"context"
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestControlPlaneTemplateDefaultingWebhook(t *testing.T) {
	g := NewWithT(t)

	t.Logf("Testing amcp defaulting webhook with no baseline")
	amcpt := &AzureManagedControlPlaneTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "fooName",
		},
		Spec: AzureManagedControlPlaneTemplateSpec{
			Template: AzureManagedControlPlaneTemplateResource{
				Spec: AzureManagedControlPlaneTemplateResourceSpec{
					Location: "fooLocation",
					Version:  "1.17.5",
				},
			},
		},
	}
	mcptw := &azureManagedControlPlaneTemplateWebhook{}
	err := mcptw.Default(context.Background(), amcpt)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(*amcpt.Spec.Template.Spec.NetworkPlugin).To(Equal("azure"))
	g.Expect(*amcpt.Spec.Template.Spec.LoadBalancerSKU).To(Equal("Standard"))
	g.Expect(amcpt.Spec.Template.Spec.Version).To(Equal("v1.17.5"))
	g.Expect(amcpt.Spec.Template.Spec.VirtualNetwork.Name).To(Equal("fooName"))
	g.Expect(amcpt.Spec.Template.Spec.VirtualNetwork.CIDRBlock).To(Equal(defaultAKSVnetCIDR))
	g.Expect(amcpt.Spec.Template.Spec.VirtualNetwork.Subnet.Name).To(Equal("fooName"))
	g.Expect(amcpt.Spec.Template.Spec.VirtualNetwork.Subnet.CIDRBlock).To(Equal(defaultAKSNodeSubnetCIDR))
	g.Expect(amcpt.Spec.Template.Spec.SKU.Tier).To(Equal(FreeManagedControlPlaneTier))

	t.Logf("Testing amcp defaulting webhook with baseline")
	netPlug := "kubenet"
	lbSKU := "Basic"
	netPol := "azure"
	amcpt.Spec.Template.Spec.NetworkPlugin = &netPlug
	amcpt.Spec.Template.Spec.LoadBalancerSKU = &lbSKU
	amcpt.Spec.Template.Spec.NetworkPolicy = &netPol
	amcpt.Spec.Template.Spec.Version = "9.99.99"
	amcpt.Spec.Template.Spec.VirtualNetwork.Name = "fooVnetName"
	amcpt.Spec.Template.Spec.VirtualNetwork.Subnet.Name = "fooSubnetName"
	amcpt.Spec.Template.Spec.SKU.Tier = PaidManagedControlPlaneTier

	err = mcptw.Default(context.Background(), amcpt)
	g.Expect(err).NotTo(HaveOccurred())
	g.Expect(*amcpt.Spec.Template.Spec.NetworkPlugin).To(Equal(netPlug))
	g.Expect(*amcpt.Spec.Template.Spec.LoadBalancerSKU).To(Equal(lbSKU))
	g.Expect(*amcpt.Spec.Template.Spec.NetworkPolicy).To(Equal(netPol))
	g.Expect(amcpt.Spec.Template.Spec.Version).To(Equal("v9.99.99"))
	g.Expect(amcpt.Spec.Template.Spec.VirtualNetwork.Name).To(Equal("fooVnetName"))
	g.Expect(amcpt.Spec.Template.Spec.VirtualNetwork.Subnet.Name).To(Equal("fooSubnetName"))
	g.Expect(amcpt.Spec.Template.Spec.SKU.Tier).To(Equal(StandardManagedControlPlaneTier))
}
