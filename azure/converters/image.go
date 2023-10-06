/*
Copyright 2020 The Kubernetes Authors.

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

package converters

import (
	"fmt"

	"github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/compute/armcompute/v5"
	asocomputev1 "github.com/Azure/azure-service-operator/v2/api/compute/v1api20220301"
	"github.com/Azure/azure-service-operator/v2/pkg/genruntime"
	"github.com/pkg/errors"
	"k8s.io/utils/ptr"
	infrav1 "sigs.k8s.io/cluster-api-provider-azure/api/v1beta1"
)

func ImageToASOSDK(image *infrav1.Image) (*asocomputev1.ImageReference, error) {
	if image.ID != nil {
		return specificImageToASOSDK(image)
	}
	if image.Marketplace != nil {
		return mpImageToASOSDK(image)
	}
	if image.ComputeGallery != nil || image.SharedGallery != nil {
		return computeImageToASOSDK(image)
	}

	return nil, errors.New("unable to convert image as no options set")
}

// ImageToSDK converts a CAPZ Image (as RawExtension) to a Azure SDK Image Reference.
func ImageToSDK(image *infrav1.Image) (*armcompute.ImageReference, error) {
	if image.ID != nil {
		return specificImageToSDK(image)
	}
	if image.Marketplace != nil {
		return mpImageToSDK(image)
	}
	if image.ComputeGallery != nil || image.SharedGallery != nil {
		return computeImageToSDK(image)
	}

	return nil, errors.New("unable to convert image as no options set")
}

func mpImageToSDK(image *infrav1.Image) (*armcompute.ImageReference, error) {
	return &armcompute.ImageReference{
		Publisher: &image.Marketplace.Publisher,
		Offer:     &image.Marketplace.Offer,
		SKU:       &image.Marketplace.SKU,
		Version:   &image.Marketplace.Version,
	}, nil
}

func mpImageToASOSDK(image *infrav1.Image) (*asocomputev1.ImageReference, error) {
	return &asocomputev1.ImageReference{
		Publisher: &image.Marketplace.Publisher,
		Offer:     &image.Marketplace.Offer,
		Sku:       &image.Marketplace.SKU,
		Version:   &image.Marketplace.Version,
	}, nil
}

func computeImageToSDK(image *infrav1.Image) (*armcompute.ImageReference, error) {
	if image.ComputeGallery == nil && image.SharedGallery == nil {
		return nil, errors.New("unable to convert compute image to SDK as SharedGallery or ComputeGallery fields are not set")
	}

	idTemplate := "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/galleries/%s/images/%s/versions/%s"
	if image.SharedGallery != nil {
		return &armcompute.ImageReference{
			ID: ptr.To(fmt.Sprintf(idTemplate,
				image.SharedGallery.SubscriptionID,
				image.SharedGallery.ResourceGroup,
				image.SharedGallery.Gallery,
				image.SharedGallery.Name,
				image.SharedGallery.Version,
			)),
		}, nil
	}

	// For private Azure Compute Gallery consumption both resource group and subscription ID must be provided.
	// If they are not, we assume use of community gallery.
	if image.ComputeGallery.ResourceGroup != nil && image.ComputeGallery.SubscriptionID != nil {
		return &armcompute.ImageReference{
			ID: ptr.To(fmt.Sprintf(idTemplate,
				ptr.Deref(image.ComputeGallery.SubscriptionID, ""),
				ptr.Deref(image.ComputeGallery.ResourceGroup, ""),
				image.ComputeGallery.Gallery,
				image.ComputeGallery.Name,
				image.ComputeGallery.Version,
			)),
		}, nil
	}

	return &armcompute.ImageReference{
		CommunityGalleryImageID: ptr.To(fmt.Sprintf("/CommunityGalleries/%s/Images/%s/Versions/%s",
			image.ComputeGallery.Gallery,
			image.ComputeGallery.Name,
			image.ComputeGallery.Version)),
	}, nil
}

// TODO: This is probably not correct. What is the equivalent of ID in ASO?
func computeImageToASOSDK(image *infrav1.Image) (*asocomputev1.ImageReference, error) {
	if image.ComputeGallery == nil && image.SharedGallery == nil {
		return nil, errors.New("unable to convert compute image to SDK as SharedGallery or ComputeGallery fields are not set")
	}

	idTemplate := "/subscriptions/%s/resourceGroups/%s/providers/Microsoft.Compute/galleries/%s/images/%s/versions/%s"
	if image.SharedGallery != nil {
		return &asocomputev1.ImageReference{
			Reference: &genruntime.ResourceReference{
				ARMID: fmt.Sprintf(idTemplate,
					image.SharedGallery.SubscriptionID,
					image.SharedGallery.ResourceGroup,
					image.SharedGallery.Gallery,
					image.SharedGallery.Name,
					image.SharedGallery.Version,
				),
			},
		}, nil
	}

	// For private Azure Compute Gallery consumption both resource group and subscription ID must be provided.
	// If they are not, we assume use of community gallery.
	if image.ComputeGallery.ResourceGroup != nil && image.ComputeGallery.SubscriptionID != nil {
		return &asocomputev1.ImageReference{
			Reference: &genruntime.ResourceReference{
				ARMID: fmt.Sprintf(idTemplate,
					ptr.Deref(image.ComputeGallery.SubscriptionID, ""),
					ptr.Deref(image.ComputeGallery.ResourceGroup, ""),
					image.ComputeGallery.Gallery,
					image.ComputeGallery.Name,
					image.ComputeGallery.Version,
				),
			},
		}, nil
	}

	return &asocomputev1.ImageReference{
		Reference: &genruntime.ResourceReference{
			ARMID: fmt.Sprintf("/CommunityGalleries/%s/Images/%s/Versions/%s",
				image.ComputeGallery.Gallery,
				image.ComputeGallery.Name,
				image.ComputeGallery.Version),
		},
	}, nil
}

func specificImageToSDK(image *infrav1.Image) (*armcompute.ImageReference, error) {
	return &armcompute.ImageReference{
		ID: image.ID,
	}, nil
}

func specificImageToASOSDK(image *infrav1.Image) (*asocomputev1.ImageReference, error) {
	return &asocomputev1.ImageReference{
		Reference: &genruntime.ResourceReference{ARMID: *image.ID},
	}, nil
}

// ImageToPlan converts a CAPZ Image to an Azure Compute Plan.
func ImageToPlan(image *infrav1.Image) *armcompute.Plan {
	// Plan is needed when using a Shared Gallery image with Plan details.
	if image.SharedGallery != nil && image.SharedGallery.Publisher != nil && image.SharedGallery.SKU != nil && image.SharedGallery.Offer != nil {
		return &armcompute.Plan{
			Publisher: image.SharedGallery.Publisher,
			Name:      image.SharedGallery.SKU,
			Product:   image.SharedGallery.Offer,
		}
	}

	// Plan is needed for third party Marketplace images.
	if image.Marketplace != nil && image.Marketplace.ThirdPartyImage {
		return &armcompute.Plan{
			Publisher: ptr.To(image.Marketplace.Publisher),
			Name:      ptr.To(image.Marketplace.SKU),
			Product:   ptr.To(image.Marketplace.Offer),
		}
	}

	// Plan is needed when using a Azure Compute Gallery image with Plan details.
	if image.ComputeGallery != nil && image.ComputeGallery.Plan != nil {
		return &armcompute.Plan{
			Publisher: ptr.To(image.ComputeGallery.Plan.Publisher),
			Name:      ptr.To(image.ComputeGallery.Plan.SKU),
			Product:   ptr.To(image.ComputeGallery.Plan.Offer),
		}
	}

	// Otherwise return nil.
	return nil
}

func ImageToPlanASO(image *infrav1.Image) *asocomputev1.Plan {
	// Plan is needed when using a Shared Gallery image with Plan details.
	if image.SharedGallery != nil && image.SharedGallery.Publisher != nil && image.SharedGallery.SKU != nil && image.SharedGallery.Offer != nil {
		return &asocomputev1.Plan{
			Publisher: image.SharedGallery.Publisher,
			Name:      image.SharedGallery.SKU,
			Product:   image.SharedGallery.Offer,
		}
	}

	// Plan is needed for third party Marketplace images.
	if image.Marketplace != nil && image.Marketplace.ThirdPartyImage {
		return &asocomputev1.Plan{
			Publisher: ptr.To(image.Marketplace.Publisher),
			Name:      ptr.To(image.Marketplace.SKU),
			Product:   ptr.To(image.Marketplace.Offer),
		}
	}

	// Plan is needed when using a Azure Compute Gallery image with Plan details.
	if image.ComputeGallery != nil && image.ComputeGallery.Plan != nil {
		return &asocomputev1.Plan{
			Publisher: ptr.To(image.ComputeGallery.Plan.Publisher),
			Name:      ptr.To(image.ComputeGallery.Plan.SKU),
			Product:   ptr.To(image.ComputeGallery.Plan.Offer),
		}
	}

	// Otherwise return nil.
	return nil
}
