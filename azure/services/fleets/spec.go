/*
Copyright 2021 The Kubernetes Authors.

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

package fleets

import (
	"context"

	asocontainerservicev1 "github.com/Azure/azure-service-operator/v2/api/containerservice/v1api20230315preview"
	"github.com/Azure/azure-service-operator/v2/pkg/genruntime"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
)

// AzureFleetSpec defines the specification for azure fleet feature.
type AzureFleetSpec struct {
	Name          string
	Namespace     string
	ResourceGroup string
	Location      string
	ClusterName   string
	DNSPrefix     string
}

// ResourceRef implements azure.ASOResourceSpecGetter.
func (s *AzureFleetSpec) ResourceRef() *asocontainerservicev1.Fleet {
	return &asocontainerservicev1.Fleet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.Name,
			Namespace: s.Namespace,
		},
	}
}

// Parameters implements azure.ASOResourceSpecGetter.
func (s *AzureFleetSpec) Parameters(ctx context.Context, existingFleet *asocontainerservicev1.Fleet) (parameters *asocontainerservicev1.Fleet, err error) {
	if existingFleet != nil {
		return existingFleet, nil
	}

	fleet := &asocontainerservicev1.Fleet{}
	fleet.Spec = asocontainerservicev1.Fleet_Spec{}

	fleet.Spec.AzureName = s.Name
	fleet.Spec.Location = ptr.To(s.Location)
	fleet.Spec.Owner = &genruntime.KnownResourceReference{
		Name: s.ClusterName,
	}
	fleet.Spec.Tags = infrav1.Build(infrav1.BuildParams{
		ClusterName: s.ClusterName,
		Lifecycle:   infrav1.ResourceLifecycleOwned,
		Name:        ptr.To(s.Name),
		Role:        ptr.To("Fleet"),
	})
	fleet.Spec.HubProfile = &asocontainerservicev1.FleetHubProfile{
		DnsPrefix: ptr.To(s.DNSPrefix),
	}
	// TODO: Figure out secrets for fleet
	fleet.Spec.OperatorSpec = &asocontainerservicev1.FleetOperatorSpec{}

	return fleet, nil
}

// WasManaged implements azure.ASOResourceSpecGetter.
func (s *AzureFleetSpec) WasManaged(resource *asocontainerservicev1.Fleet) bool {
	// returns always returns true as CAPZ does not support BYO fleet.
	return true
}
