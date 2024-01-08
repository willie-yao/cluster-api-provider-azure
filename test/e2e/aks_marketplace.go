//go:build e2e
// +build e2e

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

package e2e

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/kubernetesconfiguration/armkubernetesconfiguration"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AKSMarketplaceExtensionSpecInput struct {
	Cluster                *clusterv1.Cluster
	WaitIntervals          []interface{}
	WaitExtensionIntervals []interface{}
}

const (
	extensionName = "aks-marketplace-extension"
)

func AKSMarketplaceExtensionSpec(ctx context.Context, inputGetter func() AKSMarketplaceExtensionSpecInput) {
	input := inputGetter()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	Expect(err).NotTo(HaveOccurred())

	mgmtClient := bootstrapClusterProxy.GetClient()
	Expect(mgmtClient).NotTo(BeNil())

	amcp := &infrav1.AzureManagedControlPlane{}
	err = mgmtClient.Get(ctx, types.NamespacedName{
		Namespace: input.Cluster.Spec.ControlPlaneRef.Namespace,
		Name:      input.Cluster.Spec.ControlPlaneRef.Name,
	}, amcp)
	Expect(err).NotTo(HaveOccurred())

	extensionClient, err := armkubernetesconfiguration.NewExtensionsClient(amcp.Spec.SubscriptionID, cred, nil)
	Expect(err).NotTo(HaveOccurred())

	By("Adding an AKS Marketplace Extension to the AzureManagedControlPlane")
	var infraControlPlane = &infrav1.AzureManagedControlPlane{}
	Eventually(func(g Gomega) {
		err = mgmtClient.Get(ctx, client.ObjectKey{Namespace: input.Cluster.Spec.ControlPlaneRef.Namespace, Name: input.Cluster.Spec.ControlPlaneRef.Name}, infraControlPlane)
		g.Expect(err).NotTo(HaveOccurred())
		infraControlPlane.Spec.MarketplaceExtensions = []infrav1.MarketplaceExtension{
			{
				Name:                    extensionName,
				ExtensionType:           ptr.To("testtestindustryexperiencestest.azurecomps"),
				AKSAssignedIdentityType: infrav1.AKSAssignedIdentitySystemAssigned,
				Identity:                infrav1.ExtensionIdentitySystemAssigned,
				Plan: &infrav1.MarketplacePlan{
					Name:      "publicplanforprivatepo",
					Product:   "msalemcontainerdemo1",
					Publisher: "testtestindustryexperiencestest",
				},
			},
		}
		g.Expect(mgmtClient.Update(ctx, infraControlPlane)).To(Succeed())
	}, input.WaitExtensionIntervals...).Should(Succeed())

	Eventually(func(g Gomega) {
		err = mgmtClient.Get(ctx, client.ObjectKey{Namespace: input.Cluster.Spec.ControlPlaneRef.Namespace, Name: input.Cluster.Spec.ControlPlaneRef.Name}, infraControlPlane)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(conditions.IsTrue(infraControlPlane, infrav1.AKSExtensionsReadyCondition)).To(BeTrue())
	}, input.WaitExtensionIntervals...).Should(Succeed())

	By("Ensuring the AKS Marketplace Extension is added to the AzureManagedControlPlane")
	Eventually(func(g Gomega) {
		resp, err := extensionClient.Get(ctx, amcp.Spec.ResourceGroupName, "Microsoft.ContainerService", "managedClusters", input.Cluster.Name, extensionName, nil)
		g.Expect(err).NotTo(HaveOccurred())
		g.Expect(resp.Properties.ProvisioningState).To(Equal(ptr.To(armkubernetesconfiguration.ProvisioningStateSucceeded)))
		extension := resp.Extension
		g.Expect(extension.Properties).NotTo(BeNil())
		g.Expect(extension.Name).To(Equal(extensionName))
		g.Expect(extension.Properties.AksAssignedIdentity).NotTo(BeNil())
		g.Expect(extension.Properties.AksAssignedIdentity.Type).To(Equal(ptr.To(armkubernetesconfiguration.AKSIdentityTypeSystemAssigned)))
		g.Expect(extension.Properties.AutoUpgradeMinorVersion).To(Equal(ptr.To(true)))
		g.Expect(extension.Properties.ExtensionType).To(Equal(ptr.To("testtestindustryexperiencestest.azurecomps")))
	}, input.WaitIntervals...).Should(Succeed())
}
