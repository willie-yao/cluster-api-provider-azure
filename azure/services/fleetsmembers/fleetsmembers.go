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

package fleetsmembers

import (
	asocontainerservicev1 "github.com/Azure/azure-service-operator/v2/api/containerservice/v1api20230315preview"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/aso"
)

const serviceName = "fleetsmember"

// FleetsMemberScope defines the scope interface for a Fleet host service.
type FleetsMemberScope interface {
	azure.ClusterScoper
	aso.Scope
	AzureFleetsMemberSpec() azure.ASOResourceSpecGetter[*asocontainerservicev1.FleetsMember]
}

// Service provides operations on Azure resources.
type Service struct {
	Scope FleetsMemberScope
	*aso.Service[*asocontainerservicev1.FleetsMember, FleetsMemberScope]
}

// New creates a new service.
func New(scope FleetsMemberScope) *Service {
	svc := aso.NewService[*asocontainerservicev1.FleetsMember, FleetsMemberScope](serviceName, scope)
	spec := scope.AzureFleetsMemberSpec()
	if spec != nil {
		svc.Specs = []azure.ASOResourceSpecGetter[*asocontainerservicev1.FleetsMember]{spec}
	}
	svc.ConditionType = infrav1.FleetReadyCondition
	return &Service{
		Scope:   scope,
		Service: svc,
	}
}
