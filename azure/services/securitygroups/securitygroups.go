/*
Copyright 2019 The Kubernetes Authors.

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

package securitygroups

import (
	"context"

	"github.com/pkg/errors"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async"
	"sigs.k8s.io/cluster-api-provider-azure/util/reconciler"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

const serviceName = "securitygroups"

// NSGScope defines the scope interface for a security groups service.
type NSGScope interface {
	azure.Authorizer
	azure.AsyncStatusUpdater
	NSGSpecs() []azure.ResourceSpecGetter
	IsVnetManaged() bool
	AnnotationJSON(string) (map[string]interface{}, error)
	UpdateAnnotationJSON(string, map[string]interface{}) error
}

// Service provides operations on Azure resources.
type Service struct {
	Scope NSGScope
	async.Reconciler
	client *azureClient
}

// New creates a new service.
func New(scope NSGScope) *Service {
	client := newClient(scope)
	return &Service{
		Scope:      scope,
		Reconciler: async.New(scope, client, client),
		client:     client,
	}
}

// Name returns the service name.
func (s *Service) Name() string {
	return serviceName
}

// Some resource types are always assumed to be managed by CAPZ whether or not
// they have the canonical "owned" tag applied to most resources. The annotation
// key for those types should be listed here so their tags are always
// interpreted as managed.
var securityRuleLastAppliedAnnotation = map[string]struct{}{
	azure.SecurityRuleLastAppliedAnnotation: {},
}

// Reconcile idempotently creates or updates a set of network security groups.
func (s *Service) Reconcile(ctx context.Context) error {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "securitygroups.Service.Reconcile")
	defer done()

	ctx, cancel := context.WithTimeout(ctx, reconciler.DefaultAzureServiceReconcileTimeout)
	defer cancel()

	// Only create the NSGs if their lifecycle is managed by this controller.
	if managed, err := s.IsManaged(ctx); err == nil && !managed {
		log.V(4).Info("Skipping network security groups reconcile in custom VNet mode")
		return nil
	} else if err != nil {
		return errors.Wrap(err, "failed to check if security groups are managed")
	}

	specs := s.Scope.NSGSpecs()
	if len(specs) == 0 {
		return nil
	}

	var resErr error

	// We go through the list of security groups to reconcile each one, independently of the result of the previous one.
	// If multiple errors occur, we return the most pressing one.
	//  Order of precedence (highest -> lowest) is: error that is not an operationNotDoneError (i.e. error creating) -> operationNotDoneError (i.e. creating in progress) -> no error (i.e. created)
	for _, resourceSpec := range specs {
		nsgSpec, ok := resourceSpec.(*NSGSpec)
		if !ok {
			return errors.New("cannot convert network security group spec")
		}

		newAnnotation := map[string]interface{}{}

		// We only want to update annotations for the security rules that are always managed by CAPZ.
		for _, securityRuleSpec := range nsgSpec.SecurityRulesSpecs {
			if _, alwaysManaged := securityRuleLastAppliedAnnotation[securityRuleSpec.Annotation]; !alwaysManaged {
				continue
			}
			newAnnotation[securityRuleSpec.SecurityRule.Name] = securityRuleSpec.SecurityRule
		}

		// Retrieve the last applied security rules for all NSGs.
		lastAppliedSecurityRulesAll, err := s.Scope.AnnotationJSON(azure.SecurityRuleLastAppliedAnnotation)
		if err != nil {
			return err
		}

		// Retrieve the last applied security rules for this NSG.
		lastAppliedSecurityRules, ok := lastAppliedSecurityRulesAll[nsgSpec.Name].(map[string]interface{})
		if !ok {
			lastAppliedSecurityRules = map[string]interface{}{}
		}

		// Delete the security rules were removed from the spec.
		for ruleName := range lastAppliedSecurityRules {
			if _, ok := newAnnotation[ruleName]; !ok {
				s.client.securityrules.Delete(ctx, nsgSpec.ResourceGroupName(), nsgSpec.Name, ruleName)
			}
		}

		// Update the last applied security rules annotation.
		if len(newAnnotation) > 0 {
			lastAppliedSecurityRulesAll[nsgSpec.Name] = newAnnotation
		}
		if err := s.Scope.UpdateAnnotationJSON(azure.SecurityRuleLastAppliedAnnotation, lastAppliedSecurityRulesAll); err != nil {
			return err
		}

		if _, err := s.CreateOrUpdateResource(ctx, nsgSpec, serviceName); err != nil {
			if !azure.IsOperationNotDoneError(err) || resErr == nil {
				resErr = err
			}
		}
	}

	s.Scope.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, resErr)
	return resErr
}

// Delete deletes network security groups.
func (s *Service) Delete(ctx context.Context) error {
	ctx, log, done := tele.StartSpanWithLogger(ctx, "securitygroups.Service.Delete")
	defer done()

	ctx, cancel := context.WithTimeout(ctx, reconciler.DefaultAzureServiceReconcileTimeout)
	defer cancel()

	// Only delete the security groups if their lifecycle is managed by this controller.
	if managed, err := s.IsManaged(ctx); err == nil && !managed {
		log.V(4).Info("Skipping network security groups delete in custom VNet mode")
		return nil
	} else if err != nil {
		return errors.Wrap(err, "failed to check if security groups are managed")
	}

	specs := s.Scope.NSGSpecs()
	if len(specs) == 0 {
		return nil
	}

	var result error

	// We go through the list of security groups to delete each one, independently of the result of the previous one.
	// If multiple errors occur, we return the most pressing one.
	//  Order of precedence (highest -> lowest) is: error that is not an operationNotDoneError (i.e. error deleting) -> operationNotDoneError (i.e. deleting in progress) -> no error (i.e. deleted)
	for _, nsgSpec := range specs {
		if err := s.DeleteResource(ctx, nsgSpec, serviceName); err != nil {
			if !azure.IsOperationNotDoneError(err) || result == nil {
				result = err
			}
		}
	}

	s.Scope.UpdateDeleteStatus(infrav1.SecurityGroupsReadyCondition, serviceName, result)
	return result
}

// IsManaged returns true if the security groups' lifecycles are managed.
func (s *Service) IsManaged(ctx context.Context) (bool, error) {
	_, _, done := tele.StartSpanWithLogger(ctx, "securitygroups.Service.IsManaged")
	defer done()

	return s.Scope.IsVnetManaged(), nil
}
