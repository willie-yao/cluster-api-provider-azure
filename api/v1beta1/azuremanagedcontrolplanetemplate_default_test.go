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
	"encoding/json"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
)

func TestDefaultVirtualNetworkTemplate(t *testing.T) {
	cases := []struct {
		name                 string
		controlPlaneTemplate *AzureManagedControlPlaneTemplate
		outputTemplate       *AzureManagedControlPlaneTemplate
	}{
		{
			name: "virtual network not specified",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Name:      "test-cluster-template",
								CIDRBlock: defaultAKSVnetCIDR,
							},
						},
					},
				},
			},
		},
		{
			name: "custom name",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Name: "custom-vnet-name",
							},
						},
					},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Name:      "custom-vnet-name",
								CIDRBlock: defaultAKSVnetCIDR,
							},
						},
					},
				},
			},
		},
		{
			name: "custom cidr block",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								CIDRBlock: "10.0.0.16/24",
							},
						},
					},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Name:      "test-cluster-template",
								CIDRBlock: "10.0.0.16/24",
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		tc := c
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.controlPlaneTemplate.setDefaultVirtualNetwork()
			if !reflect.DeepEqual(tc.controlPlaneTemplate, tc.outputTemplate) {
				expected, _ := json.MarshalIndent(tc.outputTemplate, "", "\t")
				actual, _ := json.MarshalIndent(tc.controlPlaneTemplate, "", "\t")
				t.Errorf("Expected %s, got %s", string(expected), string(actual))
			}
		})
	}
}

func TestDefaultSubnetTemplate(t *testing.T) {
	cases := []struct {
		name                 string
		controlPlaneTemplate *AzureManagedControlPlaneTemplate
		outputTemplate       *AzureManagedControlPlaneTemplate
	}{
		{
			name: "subnet not specified",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Subnet: ManagedControlPlaneSubnet{
									Name:      "test-cluster-template",
									CIDRBlock: defaultAKSNodeSubnetCIDR,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "custom name",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Subnet: ManagedControlPlaneSubnet{
									Name: "custom-subnet-name",
								},
							},
						},
					},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Subnet: ManagedControlPlaneSubnet{
									Name:      "custom-subnet-name",
									CIDRBlock: defaultAKSNodeSubnetCIDR,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "custom cidr block",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Subnet: ManagedControlPlaneSubnet{
									CIDRBlock: "10.0.0.16/24",
								},
							},
						},
					},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							VirtualNetwork: ManagedControlPlaneVirtualNetworkTemplate{
								Subnet: ManagedControlPlaneSubnet{
									Name:      "test-cluster-template",
									CIDRBlock: "10.0.0.16/24",
								},
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		tc := c
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.controlPlaneTemplate.setDefaultSubnet()
			if !reflect.DeepEqual(tc.controlPlaneTemplate, tc.outputTemplate) {
				expected, _ := json.MarshalIndent(tc.outputTemplate, "", "\t")
				actual, _ := json.MarshalIndent(tc.controlPlaneTemplate, "", "\t")
				t.Errorf("Expected %s, got %s", string(expected), string(actual))
			}
		})
	}
}

func TestDefaultSkuTemplate(t *testing.T) {
	cases := []struct {
		name                 string
		controlPlaneTemplate *AzureManagedControlPlaneTemplate
		outputTemplate       *AzureManagedControlPlaneTemplate
	}{
		{
			name: "sku not specified",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							SKU: &AKSSku{
								Tier: FreeManagedControlPlaneTier,
							},
						},
					},
				},
			},
		},
		{
			name: "paid sku",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							SKU: &AKSSku{
								Tier: PaidManagedControlPlaneTier,
							},
						},
					},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							SKU: &AKSSku{
								Tier: PaidManagedControlPlaneTier,
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		tc := c
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.controlPlaneTemplate.setDefaultSku()
			if !reflect.DeepEqual(tc.controlPlaneTemplate, tc.outputTemplate) {
				expected, _ := json.MarshalIndent(tc.outputTemplate, "", "\t")
				actual, _ := json.MarshalIndent(tc.controlPlaneTemplate, "", "\t")
				t.Errorf("Expected %s, got %s", string(expected), string(actual))
			}
		})
	}
}

func TestDefaultAutoScalerProfile(t *testing.T) {
	cases := []struct {
		name                 string
		controlPlaneTemplate *AzureManagedControlPlaneTemplate
		outputTemplate       *AzureManagedControlPlaneTemplate
	}{
		{
			name: "autoscaler profile not specified",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{},
				},
			},
		},
		{
			name: "autoscaler profile empty but specified",
			controlPlaneTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							AutoScalerProfile: &AutoScalerProfile{},
						},
					},
				},
			},
			outputTemplate: &AzureManagedControlPlaneTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-cluster-template",
				},
				Spec: AzureManagedControlPlaneTemplateSpec{
					Template: AzureManagedControlPlaneTemplateResource{
						Spec: AzureManagedControlPlaneTemplateResourceSpec{
							AutoScalerProfile: &AutoScalerProfile{
								BalanceSimilarNodeGroups:      (*BalanceSimilarNodeGroups)(pointer.String(string(BalanceSimilarNodeGroupsFalse))),
								Expander:                      (*Expander)(pointer.String(string(ExpanderRandom))),
								MaxEmptyBulkDelete:            pointer.String("10"),
								MaxGracefulTerminationSec:     pointer.String("600"),
								MaxNodeProvisionTime:          pointer.String("15m"),
								MaxTotalUnreadyPercentage:     pointer.String("45"),
								NewPodScaleUpDelay:            pointer.String("0s"),
								OkTotalUnreadyCount:           pointer.String("3"),
								ScanInterval:                  pointer.String("10s"),
								ScaleDownDelayAfterAdd:        pointer.String("10m"),
								ScaleDownDelayAfterDelete:     pointer.String("10s"),
								ScaleDownDelayAfterFailure:    pointer.String("3m"),
								ScaleDownUnneededTime:         pointer.String("10m"),
								ScaleDownUnreadyTime:          pointer.String("20m"),
								ScaleDownUtilizationThreshold: pointer.String("0.5"),
								SkipNodesWithLocalStorage:     (*SkipNodesWithLocalStorage)(pointer.String(string(SkipNodesWithLocalStorageFalse))),
								SkipNodesWithSystemPods:       (*SkipNodesWithSystemPods)(pointer.String(string(SkipNodesWithSystemPodsTrue))),
							},
						},
					},
				},
			},
		},
	}

	for _, c := range cases {
		tc := c
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.controlPlaneTemplate.setDefaultAutoScalerProfile()
			if !reflect.DeepEqual(tc.controlPlaneTemplate, tc.outputTemplate) {
				expected, _ := json.MarshalIndent(tc.outputTemplate, "", "\t")
				actual, _ := json.MarshalIndent(tc.controlPlaneTemplate, "", "\t")
				t.Errorf("Expected %s, got %s", string(expected), string(actual))
			}
		})
	}
}
