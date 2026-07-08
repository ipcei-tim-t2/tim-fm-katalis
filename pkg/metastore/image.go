package metastore

import (
	"context"
	"encoding/json"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	k8scli "sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/google/uuid"
	"github.com/neonephos-katalis/opg-ewbi-operator/api/ewbi/models"
	camara "github.com/neonephos-katalis/opg-ewbi-operator/api/ewbi/server"
	v1beta1 "github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	uu "github.com/neonephos-katalis/opg-ewbi-operator/pkg/uuid"
	"github.com/pkg/errors"
)

type Image struct {
	*camara.ViewFile200JSONResponse
	FederationContextId models.FederationContextId
}
type UploadImage struct {
	*models.UploadFileMultipartBody
	FederationContextId models.FederationContextId
}

func (f *UploadImage) MarshalJSON() ([]byte, error) {
	cp := *f.UploadFileMultipartBody
	cp.File = nil
	return json.Marshal(&cp)
}

// func isValidImageStatus(status string) bool {
// 	switch v1beta1.ImageState(status) {
// 	case v1beta1.ImageStatePending, v1beta1.ImageStateReady, v1beta1.ImageStateError, v1beta1.ImageStateUnknown:
// 		return true
// 	}
// 	return false
// }

func (c *k8sClient) searchImage(ctx context.Context, federationContextId string, imageId string, role string, listObj client.ObjectList) (*v1beta1.Image, error) {
	fields := map[string]string{
		FedIdIndex:   federationContextId,
		RelationType: role,
		ImageIdIndex: imageId,
	}
	obj, err := c.getResourceByFields(ctx, &v1beta1.ImageList{}, fields)
	if err != nil {
		return nil, err
	}
	image, ok := obj.(*v1beta1.Image)
	if !ok {
		return nil, missMatchErr("image", federationContextId, imageId, &v1beta1.Image{}, obj)
	}
	return image, nil
}

func (c *k8sClient) GetImage(ctx context.Context, federationContextID, id string) (*Image, error) {
	if _, err := c.searchFederation(ctx, federationContextID, "HOST", &v1beta1.FederationList{}); err != nil {
		return nil, err
	}
	image, err := c.searchImage(ctx, federationContextID, id, "HOST", &v1beta1.ImageList{})
	if err != nil {
		return nil, err
	}
	parsedUUID, err := uuid.Parse(id)
	if err != nil {
		return nil, err
	}
	return &Image{
		ViewFile200JSONResponse: &camara.ViewFile200JSONResponse{
			AppProviderId: image.Spec.ImageBody.AppProviderId,
			FileId:        models.FileId(parsedUUID),
			FileName:      image.Spec.ImageBody.ImageName,
			FileRepoLocation: &models.ObjectRepoLocation{
				Password: &image.Spec.ImageBody.ImageRepoLocation.Password,
				RepoURL:  &image.Spec.ImageBody.ImageRepoLocation.RepoURL,
				Token:    &image.Spec.ImageBody.ImageRepoLocation.Token,
				UserName: &image.Spec.ImageBody.ImageRepoLocation.UserName,
			},
			FileType:        models.VirtImageType(image.Spec.ImageBody.ImageType),
			FileVersionInfo: image.Spec.ImageBody.ImageVersionInfo,
			ImgInsSetArch:   models.CPUArchType(image.Spec.ImageBody.ImgInsSetArch),
			ImgOSType: models.OSType{
				Architecture: models.OSTypeArchitecture(image.Spec.ImageBody.ImgOSType.Architecture),
				Distribution: models.OSTypeDistribution(image.Spec.ImageBody.ImgOSType.Distribution),
				License:      models.OSTypeLicense(image.Spec.ImageBody.ImgOSType.License),
				Version:      models.OSTypeVersion(image.Spec.ImageBody.ImgOSType.Version),
			},
			RepoType: (*models.RepoType)(&image.Spec.ImageBody.RepoType),
		},
		FederationContextId: image.Labels[opgLabel(federationContextIDLabel)],
	}, nil
}

func (c *k8sClient) UploadImage(ctx context.Context, image *UploadImage) (*v1beta1.Image, error) {
	if _, err := c.searchFederation(ctx, image.FederationContextId, "HOST", &v1beta1.FederationList{}); err != nil {
		return nil, err
	}
	imageId, err := uuid.Parse("fed-" + uu.V5(image.FederationContextId+string(image.FileId[:])))
	if err != nil {
		return nil, err
	}
	obj := &v1beta1.Image{
		ObjectMeta: metav1.ObjectMeta{
			Name:      imageId.String(),
			Namespace: c.getNamespace(),
		},
		Spec: v1beta1.ImageSpec{
			RelationType:        string(host),
			ImageId:             string(image.FileId[:]),
			FederationContextId: image.FederationContextId,
			ImageBody: v1beta1.ImageBody{
				AppProviderId:    image.AppProviderId,
				ImageName:        image.FileName,
				ImageVersionInfo: image.FileVersionInfo,
				ImageType:        string(image.FileType),
				RepoType:         defaultIfNil((*string)(image.RepoType)),
				ImageRepoLocation: v1beta1.ImageRepoLocation{
					RepoURL:  defaultIfNil(image.FileRepoLocation.RepoURL),
					Password: defaultIfNil(image.FileRepoLocation.Password),
					Token:    defaultIfNil(image.FileRepoLocation.Token),
					UserName: defaultIfNil(image.FileRepoLocation.UserName),
				},
				ImgInsSetArch: string(image.ImgInsSetArch),
				ImgOSType: v1beta1.ImgOSType{
					Architecture: string(image.ImgOSType.Architecture),
					Distribution: string(image.ImgOSType.Distribution),
					License:      string(image.ImgOSType.License),
					Version:      string(image.ImgOSType.Version),
				},
			},
		},
	}

	if err := c.createK8sObject(obj); err != nil {
		return nil, err
	}
	return obj, nil
}

func (c *k8sClient) RemoveImage(ctx context.Context, federationContextID, id string) error {
	if _, err := c.searchFederation(ctx, federationContextID, "HOST", &v1beta1.FederationList{}); err != nil {
		return err
	}
	image, err := c.searchImage(ctx, federationContextID, id, "HOST", &v1beta1.ImageList{})
	if err != nil {
		return err
	}
	if err := c.kubernetes.Delete(context.TODO(), image, &k8scli.DeleteOptions{}); err != nil {
		return errors.Wrapf(err, "unable to remove image")
	}
	return nil
}

func (c *k8sClient) UpdateImageStatus(ctx context.Context, federationCallbackID string, updates *models.FileStatusCallbackLinkJSONRequestBody) error {
	id := string(updates.FileId[:])
	image, err := c.searchImage(ctx, federationCallbackID, id, "GUEST", &v1beta1.ImageList{})
	if err != nil {
		return err
	}
	state := string(updates.UpdateStatus)
	//if isValidImageStatus(state) {
	return c.updateK8sObjectStatus(image, state)
	// }
	// return nil
}
