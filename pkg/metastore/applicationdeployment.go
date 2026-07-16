package metastore

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/ewbi/models"
	camara "github.com/neonephos-katalis/opg-ewbi-operator/api/ewbi/server"
	v1beta1 "github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	uu "github.com/neonephos-katalis/opg-ewbi-operator/pkg/uuid"
)

type ApplicationInstanceDetails struct {
	*camara.GetAppInstanceDetails200JSONResponse
}

type ApplicationInstance struct {
	*models.InstallAppJSONBody
	FederationContextId models.FederationContextId `json:"-"`
}

// func isValidApplicationDeploymentStatus(status string) bool {
// 	switch v1beta1.ApplicationDeploymentState(status) {
// 	case v1beta1.ApplicationDeploymentStatePending, v1beta1.ApplicationDeploymentStateReady, v1beta1.ApplicationDeploymentStateFailed, v1beta1.ApplicationDeploymentStateTerminating:
// 		return true
// 	}
// 	return false
// }

func (d *ApplicationInstance) k8sCustomResource(namespace string, opts ...Opt) (*v1beta1.ApplicationDeployment, error) {
	appId := "appdeploy-" + uu.V5(d.FederationContextId+d.AppProviderId+string(d.AppId[:]))
	obj := &v1beta1.ApplicationDeployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      appId,
			Namespace: namespace,
		},
		Spec: v1beta1.ApplicationDeploymentSpec{
			FederationContextId: d.FederationContextId,
			RelationType:        string(v1beta1.FederationRelationHost),
			AppProviderId:       d.AppProviderId,
			AppId:               d.AppId,
			ZoneId:              d.ZoneInfo.ZoneId,
			AppDetails: &v1beta1.AppDetails{
				AppVersion: d.AppVersion,
				ZoneInfo: &v1beta1.ZoneInfo{
					FlavourId:           d.ZoneInfo.FlavourId,
					ResourceConsumption: defaultIfNil((*string)(d.ZoneInfo.ResourceConsumption)),
					ResPool:             defaultIfNil(d.ZoneInfo.ResPool),
				},
				AppInstCallbackLink: d.AppInstCallbackLink,
			},
		},
	}
	for _, opt := range opts {
		if err := opt(&obj.ObjectMeta); err != nil {
			return nil, err
		}
	}

	return obj, nil
}

func k8sCustomResourceNameFromApplicationDeployment(federationContextID, appID string) string {
	return fmt.Sprintf("%s-%s", applicationDeploymentPrefix, uuidV5Fn(federationContextID+"/"+appID))
}

func applicationDeploymentFromK8sCustomResource(appInstanceID string, appInstance v1beta1.ApplicationDeployment) (*ApplicationInstanceDetails, error) {
	return &ApplicationInstanceDetails{}, nil
}
