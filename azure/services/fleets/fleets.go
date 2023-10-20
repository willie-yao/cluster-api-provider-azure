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
package fleets

import (
	asocontainerservicev1 "github.com/Azure/azure-service-operator/v2/api/containerservice/v1api20230315preview"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/aso"
)

const serviceName = "fleets"

// FleetScope defines the scope interface for a Fleet host service.
type FleetScope interface {
	azure.ClusterScoper
	aso.Scope
	AzureFleetSpec() azure.ASOResourceSpecGetter[*asocontainerservicev1.Fleet]
}

// Service provides operations on Azure resources.
type Service struct {
	Scope FleetScope
	*aso.Service[*asocontainerservicev1.Fleet, FleetScope]
}

// New creates a new service.
func New(scope FleetScope) *Service {
	svc := aso.NewService[*asocontainerservicev1.Fleet, FleetScope](serviceName, scope)
	spec := scope.AzureFleetSpec()
	if spec != nil {
		svc.Specs = []azure.ASOResourceSpecGetter[*asocontainerservicev1.Fleet]{spec}
	}
	svc.ConditionType = infrav1.FleetReadyCondition
	return &Service{
		Scope:   scope,
		Service: svc,
	}
}
