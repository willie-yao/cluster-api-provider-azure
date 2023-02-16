/*
Copyright 2022 The Kubernetes Authors.

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
	"testing"

	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/component-base/featuregate/testing"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/cluster-api-provider-azure/feature"
	capifeature "sigs.k8s.io/cluster-api/feature"
)

func TestAzureManagedControlPlaneTemplate_ValidateCreate(t *testing.T) {
	// NOTE: AzureManageControlPlaneTemplate is behind AKS feature gate flag; the webhook
	// must prevent creating new objects in case the feature flag is disabled.
	defer utilfeature.SetFeatureGateDuringTest(t, feature.Gates, capifeature.MachinePool, true)()
	g := NewWithT(t)

	tests := []struct {
		name    string
		amcpt   *AzureManagedControlPlaneTemplate
		wantErr bool
	}{
		{
			name:    "all valid",
			amcpt:   getKnownValidAzureManagedControlPlaneTemplate(),
			wantErr: false,
		},
		{
			name:    "invalid DNSServiceIP",
			amcpt:   createAzureManagedControlPlaneTemplate(metav1.ObjectMeta{}, "192.168.0.0.3", "v1.18.0"),
			wantErr: true,
		},
		{
			name:    "invalid version",
			amcpt:   createAzureManagedControlPlaneTemplate(metav1.ObjectMeta{}, "192.168.0.0", "honk.version"),
			wantErr: true,
		},
		{
			name:    "invalid name with microsoft",
			amcpt:   createAzureManagedControlPlaneTemplate(metav1.ObjectMeta{Name: "microsoft-cluster"}, "192.168.0.0", "v1.18.0"),
			wantErr: true,
		},
		{
			name:    "invalid name with windows",
			amcpt:   createAzureManagedControlPlaneTemplate(metav1.ObjectMeta{Name: "a-windows-cluster"}, "192.168.0.0", "v1.18.0"),
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.amcpt.ValidateCreate(nil)
			if tc.wantErr {
				g.Expect(err).To(HaveOccurred())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
			}
		})
	}
}

func TestAzureManagedControlPlaneTemplate_ValidateUpdate(t *testing.T) {

	oldManagedControlPlaneTemplate := &AzureManagedControlPlaneTemplate{
		Spec: AzureManagedControlPlaneTemplateSpec{
			Template: AzureManagedControlPlaneTemplateResource{
				Spec: AzureManagedControlPlaneTemplateResourceSpec{
					NetworkPolicy: pointer.String("azure"),
				},
			},
		},
	}

	newManagedControlPlaneTemplate := &AzureManagedControlPlaneTemplate{
		Spec: AzureManagedControlPlaneTemplateSpec{
			Template: AzureManagedControlPlaneTemplateResource{
				Spec: AzureManagedControlPlaneTemplateResourceSpec{
					NetworkPolicy: pointer.String("calico"),
				},
			},
		},
	}

	t.Run("template is immutable", func(t *testing.T) {
		g := NewWithT(t)
		g.Expect(newManagedControlPlaneTemplate.ValidateUpdate(oldManagedControlPlaneTemplate, nil)).NotTo(Succeed())
	})
}

func createAzureManagedControlPlaneTemplate(objectMeta metav1.ObjectMeta, serviceIP, version string) *AzureManagedControlPlaneTemplate {
	return &AzureManagedControlPlaneTemplate{
		ObjectMeta: objectMeta,
		Spec: AzureManagedControlPlaneTemplateSpec{
			Template: AzureManagedControlPlaneTemplateResource{
				Spec: AzureManagedControlPlaneTemplateResourceSpec{
					DNSServiceIP: pointer.String(serviceIP),
					Version:      version,
				},
			},
		},
	}
}

func getKnownValidAzureManagedControlPlaneTemplate() *AzureManagedControlPlaneTemplate {
	return &AzureManagedControlPlaneTemplate{
		Spec: AzureManagedControlPlaneTemplateSpec{
			Template: AzureManagedControlPlaneTemplateResource{
				Spec: AzureManagedControlPlaneTemplateResourceSpec{
					DNSServiceIP: pointer.String("192.168.0.0"),
					Version:      "v1.18.0",
					AADProfile: &AADProfile{
						Managed: true,
						AdminGroupObjectIDs: []string{
							"616077a8-5db7-4c98-b856-b34619afg75h",
						},
					},
				},
			},
		},
	}
}
