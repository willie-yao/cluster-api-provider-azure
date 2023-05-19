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
	"testing"

	"github.com/Azure/azure-sdk-for-go/services/network/mgmt/2021-08-01/network"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"k8s.io/utils/pointer"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/async/mock_async"
	"sigs.k8s.io/cluster-api-provider-azure/azure/services/securitygroups/mock_securitygroups"
	gomockinternal "sigs.k8s.io/cluster-api-provider-azure/internal/test/matchers/gomock"
)

var (
	annotation = azure.SecurityRuleLastAppliedAnnotation
	fakeNSG    = NSGSpec{
		Name:        "test-nsg",
		Location:    "test-location",
		ClusterName: "my-cluster",
		SecurityRulesSpecs: []azure.SecurityRulesSpec{
			{
				SecurityRule: securityRule1,
				Annotation:   annotation,
			},
		},
		ResourceGroup: "test-group",
	}
	multipleRulesNSG = NSGSpec{
		Name:        "multiple-rules-nsg",
		Location:    "test-location",
		ClusterName: "my-cluster",
		SecurityRulesSpecs: []azure.SecurityRulesSpec{
			{
				SecurityRule: securityRule1,
				Annotation:   annotation,
			},
			{
				SecurityRule: securityRule2,
				Annotation:   annotation,
			},
		},
		ResourceGroup: "test-group",
	}
	fakeNSG2 = NSGSpec{
		Name:               "test-nsg-2",
		Location:           "test-location",
		ClusterName:        "my-cluster",
		SecurityRulesSpecs: []azure.SecurityRulesSpec{},
		ResourceGroup:      "test-group",
	}
	securityRule1 = infrav1.SecurityRule{
		Name:             "allow_ssh",
		Description:      "Allow SSH",
		Priority:         2200,
		Protocol:         infrav1.SecurityGroupProtocolTCP,
		Direction:        infrav1.SecurityRuleDirectionInbound,
		Source:           pointer.String("*"),
		SourcePorts:      pointer.String("*"),
		Destination:      pointer.String("*"),
		DestinationPorts: pointer.String("22"),
	}
	securityRule2 = infrav1.SecurityRule{
		Name:             "other_rule",
		Description:      "Test Rule",
		Priority:         500,
		Protocol:         infrav1.SecurityGroupProtocolTCP,
		Direction:        infrav1.SecurityRuleDirectionInbound,
		Source:           pointer.String("*"),
		SourcePorts:      pointer.String("*"),
		Destination:      pointer.String("*"),
		DestinationPorts: pointer.String("80"),
	}
	errFake      = errors.New("this is an error")
	notDoneError = azure.NewOperationNotDoneError(&infrav1.Future{})
)

func TestReconcileSecurityGroups(t *testing.T) {
	testcases := []struct {
		name          string
		expectedError string
		expect        func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder)
	}{
		{
			name:          "create single security group with single rule succeeds, should return no error",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{}, nil).Times(1)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}).Times(1)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil, nil)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, nil)
			},
		},
		{
			name:          "create single security group with multiple rules succeeds, should return no error",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&multipleRulesNSG})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{}, nil).Times(1)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{multipleRulesNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1, securityRule2.Name: securityRule2}}).Times(1)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &multipleRulesNSG, serviceName).Return(nil, nil)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, nil)
			},
		},
		{
			name:          "create multiple security groups, should return no error",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG, &fakeNSG2})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{}, nil).Times(1)
				s.AnnotationJSON(annotation).Return(map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}, nil).Times(1)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}).Times(2)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil, nil)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG2, serviceName).Return(nil, nil)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, nil)
			},
		},
		{
			name:          "should delete security rule successfully if it is no longer in the spec",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1, securityRule2.Name: securityRule2}}, nil).Times(1)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}).Times(1)
				c.DeleteRule(gomockinternal.AContext(), &fakeNSG, securityRule2.Name).Return(nil, nil)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil, nil)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, nil)
			},
		},
		{
			name:          "first security groups create fails, should return error",
			expectedError: errFake.Error(),
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG, &fakeNSG2})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{}, nil).Times(1)
				s.AnnotationJSON(annotation).Return(map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}, nil).Times(1)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}).Times(2)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil, errFake)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG2, serviceName).Return(nil, nil)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, errFake)
			},
		},
		{
			name:          "first sg create fails, second sg create not done, should return create error",
			expectedError: errFake.Error(),
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG, &fakeNSG2})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{}, nil).Times(1)
				s.AnnotationJSON(annotation).Return(map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}, nil).Times(1)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}}).Times(2)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil, errFake)
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG2, serviceName).Return(nil, notDoneError)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, errFake)
			},
		},
		{
			name:          "security groups create not done, should return not done error",
			expectedError: notDoneError.Error(),
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG})
				s.AnnotationJSON(annotation).Return(map[string]interface{}{}, nil)
				s.UpdateAnnotationJSON(annotation, map[string]interface{}{fakeNSG.Name: map[string]interface{}{securityRule1.Name: securityRule1}})
				r.CreateOrUpdateResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil, notDoneError)
				s.UpdatePutStatus(infrav1.SecurityGroupsReadyCondition, serviceName, notDoneError)
			},
		},
		{
			name:          "vnet is not managed, should skip reconcile",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder, c *mock_securitygroups.MockclientMockRecorder) {
				s.IsVnetManaged().Return(false)
			},
		},
	}
	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			t.Parallel()
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			scopeMock := mock_securitygroups.NewMockNSGScope(mockCtrl)
			reconcilerMock := mock_async.NewMockReconciler(mockCtrl)
			clientMock := mock_securitygroups.NewMockclient(mockCtrl)

			tc.expect(scopeMock.EXPECT(), reconcilerMock.EXPECT(), clientMock.EXPECT())

			s := &Service{
				Scope:      scopeMock,
				Reconciler: reconcilerMock,
				client:     clientMock,
			}

			err := s.Reconcile(context.TODO())
			if tc.expectedError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestDeleteSecurityGroups(t *testing.T) {
	testcases := []struct {
		name          string
		expectedError string
		expect        func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder)
	}{
		{
			name:          "delete multiple security groups succeeds, should return no error",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG, &fakeNSG2})
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(nil)
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG2, serviceName).Return(nil)
				s.UpdateDeleteStatus(infrav1.SecurityGroupsReadyCondition, serviceName, nil)
			},
		},
		{
			name:          "first security groups delete fails, should return an error",
			expectedError: errFake.Error(),
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG, &fakeNSG2})
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(errFake)
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG2, serviceName).Return(nil)
				s.UpdateDeleteStatus(infrav1.SecurityGroupsReadyCondition, serviceName, errFake)
			},
		},
		{
			name:          "first security groups delete fails and second security groups create not done, should return an error",
			expectedError: errFake.Error(),
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG, &fakeNSG2})
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(errFake)
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG2, serviceName).Return(notDoneError)
				s.UpdateDeleteStatus(infrav1.SecurityGroupsReadyCondition, serviceName, errFake)
			},
		},
		{
			name:          "security groups delete not done, should return not done error",
			expectedError: notDoneError.Error(),
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder) {
				s.IsVnetManaged().Return(true)
				s.NSGSpecs().Return([]azure.ResourceSpecGetter{&fakeNSG})
				r.DeleteResource(gomockinternal.AContext(), &fakeNSG, serviceName).Return(notDoneError)
				s.UpdateDeleteStatus(infrav1.SecurityGroupsReadyCondition, serviceName, notDoneError)
			},
		},
		{
			name:          "vnet is not managed, should skip delete",
			expectedError: "",
			expect: func(s *mock_securitygroups.MockNSGScopeMockRecorder, r *mock_async.MockReconcilerMockRecorder) {
				s.IsVnetManaged().Return(false)
			},
		},
	}
	for _, tc := range testcases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			t.Parallel()
			mockCtrl := gomock.NewController(t)
			defer mockCtrl.Finish()

			scopeMock := mock_securitygroups.NewMockNSGScope(mockCtrl)
			reconcilerMock := mock_async.NewMockReconciler(mockCtrl)

			tc.expect(scopeMock.EXPECT(), reconcilerMock.EXPECT())

			s := &Service{
				Scope:      scopeMock,
				Reconciler: reconcilerMock,
			}

			err := s.Delete(context.TODO())
			if tc.expectedError != "" {
				g.Expect(err).To(HaveOccurred())
				g.Expect(err).To(MatchError(tc.expectedError))
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

var (
	ruleA = network.SecurityRule{
		Name: pointer.String("A"),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Description:              pointer.String("this is rule A"),
			Protocol:                 network.SecurityRuleProtocolTCP,
			DestinationPortRange:     pointer.String("*"),
			SourcePortRange:          pointer.String("*"),
			DestinationAddressPrefix: pointer.String("*"),
			SourceAddressPrefix:      pointer.String("*"),
			Priority:                 pointer.Int32(100),
			Direction:                network.SecurityRuleDirectionInbound,
		},
	}
	ruleB = network.SecurityRule{
		Name: pointer.String("B"),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Description:              pointer.String("this is rule B"),
			Protocol:                 network.SecurityRuleProtocolTCP,
			DestinationPortRange:     pointer.String("*"),
			SourcePortRange:          pointer.String("*"),
			DestinationAddressPrefix: pointer.String("*"),
			SourceAddressPrefix:      pointer.String("*"),
			Priority:                 pointer.Int32(100),
			Direction:                network.SecurityRuleDirectionOutbound,
		},
	}
	ruleBModified = network.SecurityRule{
		Name: pointer.String("B"),
		SecurityRulePropertiesFormat: &network.SecurityRulePropertiesFormat{
			Description:              pointer.String("this is rule B"),
			Protocol:                 network.SecurityRuleProtocolTCP,
			DestinationPortRange:     pointer.String("80"),
			SourcePortRange:          pointer.String("*"),
			DestinationAddressPrefix: pointer.String("*"),
			SourceAddressPrefix:      pointer.String("*"),
			Priority:                 pointer.Int32(100),
			Direction:                network.SecurityRuleDirectionOutbound,
		},
	}
)
