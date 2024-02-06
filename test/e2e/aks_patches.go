//go:build e2e
// +build e2e

/*
Copyright 2024 The Kubernetes Authors.

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
	"sync"

	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v4"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	expv1 "sigs.k8s.io/cluster-api/exp/api/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type AKSPatchSpecInput struct {
	Cluster       *clusterv1.Cluster
	MachinePools  []*expv1.MachinePool
	WaitForUpdate []interface{}
}

func AKSPatchSpec(ctx context.Context, inputGetter func() AKSPatchSpecInput) {
	input := inputGetter()

	cred, err := azidentity.NewDefaultAzureCredential(nil)
	Expect(err).NotTo(HaveOccurred())

	managedclustersClient, err := armcontainerservice.NewManagedClustersClient(getSubscriptionID(Default), cred, nil)
	Expect(err).NotTo(HaveOccurred())

	agentpoolsClient, err := armcontainerservice.NewAgentPoolsClient(getSubscriptionID(Default), cred, nil)
	Expect(err).NotTo(HaveOccurred())

	mgmtClient := bootstrapClusterProxy.GetClient()
	Expect(mgmtClient).NotTo(BeNil())

	infraControlPlane := &infrav1.AzureManagedControlPlane{}
	err = mgmtClient.Get(ctx, client.ObjectKey{
		Namespace: input.Cluster.Spec.ControlPlaneRef.Namespace,
		Name:      input.Cluster.Spec.ControlPlaneRef.Name,
	}, infraControlPlane)
	Expect(err).NotTo(HaveOccurred())

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer GinkgoRecover()
		defer wg.Done()

		checkTags := func(exist map[string]string) func(Gomega) {
			return func(g Gomega) {
				resp, err := managedclustersClient.Get(ctx, infraControlPlane.Spec.ResourceGroupName, infraControlPlane.Name, nil)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(resp.Properties.ProvisioningState).To(Equal(ptr.To("Succeeded")))
				for k, v := range exist {
					g.Expect(resp.ManagedCluster.Tags).To(HaveKeyWithValue(k, ptr.To(v)))
				}
			}
		}

		var initialPatches []string
		By("Deleting patches for control plane")
		Eventually(func(g Gomega) {
			g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(infraControlPlane), infraControlPlane)).To(Succeed())
			initialPatches = infraControlPlane.Spec.ASOManagedClusterPatches
			infraControlPlane.Spec.ASOManagedClusterPatches = nil
			g.Expect(mgmtClient.Update(ctx, infraControlPlane)).To(Succeed())
		}, inputGetter().WaitForUpdate...).Should(Succeed())
		Eventually(checkTags(nil), input.WaitForUpdate...).Should(Succeed())

		By("Creating patches for control plane")
		Eventually(func(g Gomega) {
			g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(infraControlPlane), infraControlPlane)).To(Succeed())
			infraControlPlane.Spec.ASOManagedClusterPatches = []string{
				`{"spec": {"tags": {"capzpatchtest": "value"}}}`,
			}
			g.Expect(mgmtClient.Update(ctx, infraControlPlane)).To(Succeed())
		}, inputGetter().WaitForUpdate...).Should(Succeed())
		Eventually(checkTags(map[string]string{"capzpatchtest": "value"}), input.WaitForUpdate...).Should(Succeed())

		By("Updating patches for control plane")
		Eventually(func(g Gomega) {
			g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(infraControlPlane), infraControlPlane)).To(Succeed())
			infraControlPlane.Spec.ASOManagedClusterPatches = append(infraControlPlane.Spec.ASOManagedClusterPatches, `{"spec": {"tags": {"capzpatchtest": "updated"}}}`)
			g.Expect(mgmtClient.Update(ctx, infraControlPlane)).To(Succeed())
		}, inputGetter().WaitForUpdate...).Should(Succeed())
		Eventually(checkTags(map[string]string{"capzpatchtest": "updated"}), input.WaitForUpdate...).Should(Succeed())

		By("Restoring initial patches for control plane")
		Eventually(func(g Gomega) {
			g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(infraControlPlane), infraControlPlane)).To(Succeed())
			infraControlPlane.Spec.ASOManagedClusterPatches = initialPatches
			g.Expect(mgmtClient.Update(ctx, infraControlPlane)).To(Succeed())
		}, inputGetter().WaitForUpdate...).Should(Succeed())
		Eventually(checkTags(nil), input.WaitForUpdate...).Should(Succeed())
	}()

	for _, mp := range input.MachinePools {
		wg.Add(1)
		go func(mp *expv1.MachinePool) {
			defer GinkgoRecover()
			defer wg.Done()

			ammp := &infrav1.AzureManagedMachinePool{}
			Expect(mgmtClient.Get(ctx, types.NamespacedName{
				Namespace: mp.Spec.Template.Spec.InfrastructureRef.Namespace,
				Name:      mp.Spec.Template.Spec.InfrastructureRef.Name,
			}, ammp)).To(Succeed())

			nonAdditionalTagKeys := map[string]struct{}{}
			resp, err := agentpoolsClient.Get(ctx, infraControlPlane.Spec.ResourceGroupName, infraControlPlane.Name, *ammp.Spec.Name, nil)
			Expect(err).NotTo(HaveOccurred())
			for k := range resp.AgentPool.Properties.Tags {
				if _, exists := infraControlPlane.Spec.AdditionalTags[k]; !exists {
					nonAdditionalTagKeys[k] = struct{}{}
				}
			}

			checkTags := func(exist map[string]string) func(Gomega) {
				return func(g Gomega) {
					resp, err := agentpoolsClient.Get(ctx, infraControlPlane.Spec.ResourceGroupName, infraControlPlane.Name, *ammp.Spec.Name, nil)
					g.Expect(err).NotTo(HaveOccurred())
					g.Expect(resp.Properties.ProvisioningState).To(Equal(ptr.To("Succeeded")))
					for k, v := range exist {
						g.Expect(resp.AgentPool.Properties.Tags).To(HaveKeyWithValue(k, ptr.To(v)))
					}
				}
			}

			var initialPatches []string
			Byf("Deleting all patches for machine pool %s", mp.Name)
			Eventually(func(g Gomega) {
				g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(ammp), ammp)).To(Succeed())
				initialPatches = ammp.Spec.ASOManagedClustersAgentPoolPatches
				ammp.Spec.ASOManagedClustersAgentPoolPatches = nil
				g.Expect(mgmtClient.Update(ctx, ammp)).To(Succeed())
			}, inputGetter().WaitForUpdate...).Should(Succeed())
			Eventually(checkTags(nil), input.WaitForUpdate...).Should(Succeed())

			Byf("Creating patches for machine pool %s", mp.Name)
			Eventually(func(g Gomega) {
				g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(ammp), ammp)).To(Succeed())
				ammp.Spec.ASOManagedClustersAgentPoolPatches = []string{
					`{"spec": {"tags": {"capzpatchtest": "value"}}}`,
				}
				g.Expect(mgmtClient.Update(ctx, ammp)).To(Succeed())
			}, inputGetter().WaitForUpdate...).Should(Succeed())
			Eventually(checkTags(map[string]string{"capzpatchtest": "value"}), input.WaitForUpdate...).Should(Succeed())

			Byf("Updating patches for machine pool %s", mp.Name)
			Eventually(func(g Gomega) {
				g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(ammp), ammp)).To(Succeed())
				ammp.Spec.ASOManagedClustersAgentPoolPatches = append(ammp.Spec.ASOManagedClustersAgentPoolPatches, `{"spec": {"tags": {"capzpatchtest": "updated"}}}`)
				g.Expect(mgmtClient.Update(ctx, ammp)).To(Succeed())
			}, inputGetter().WaitForUpdate...).Should(Succeed())
			Eventually(checkTags(map[string]string{"capzpatchtest": "updated"}), input.WaitForUpdate...).Should(Succeed())

			Byf("Restoring initial patches for machine pool %s", mp.Name)
			Eventually(func(g Gomega) {
				g.Expect(mgmtClient.Get(ctx, client.ObjectKeyFromObject(ammp), ammp)).To(Succeed())
				ammp.Spec.ASOManagedClustersAgentPoolPatches = initialPatches
				g.Expect(mgmtClient.Update(ctx, ammp)).To(Succeed())
			}, inputGetter().WaitForUpdate...).Should(Succeed())
			Eventually(checkTags(nil), input.WaitForUpdate...).Should(Succeed())
		}(mp)
	}

	wg.Wait()
}
