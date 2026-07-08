/*
Copyright 2025.

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

package k8s

import (
	"context"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/opg"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ApplicationDeploymentReconciler reconciles an Artefact object
type ApplicationDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
}

func (r *ApplicationDeploymentReconciler) CreateApplicationDeployment(ctx context.Context, appDeploy *v1beta1.ApplicationDeployment, fed *v1beta1.Federation) error {
	log := log.FromContext(ctx)
	appDeployHost := &v1beta1.ApplicationDeployment{
		TypeMeta: appDeploy.TypeMeta,
		ObjectMeta: metav1.ObjectMeta{
			Name:      appDeploy.Name,
			Namespace: fed.Spec.FederationData.K8sOptions.Namespace,
		},
		Spec: v1beta1.ApplicationDeploymentSpec{
			RelationType:        string(v1beta1.FederationRelationHost),
			FederationContextId: fed.Status.FederationContextId,
			AppId:               appDeploy.Spec.AppId,
			AppInstanceId:       appDeploy.Spec.AppInstanceId,
			ZoneId:              appDeploy.Spec.ZoneId,
			AppProviderId:       appDeploy.Spec.AppProviderId,
			AppDetails:          appDeploy.Spec.AppDetails,
		},
	}
	err := ApplyRemoteResource(ctx, r.Client, r.Scheme, fed, appDeployHost, &v1beta1.ApplicationDeployment{}, appDeploy.Name, appDeploy.Namespace, v1beta1.GroupVersion.Group, v1beta1.GroupVersion.Version, v1beta1.PluralApplicationDeployment, "app-deploy-controller", "[AppDep][K8s]")
	if err != nil {
		return err
	}
	appDeploy.Status.AppInstanceInfo.AppInstanceState = v1beta1.ApplicationDeploymentStatePending
	upErr := r.Status().Update(ctx, appDeploy.DeepCopy())
	if upErr != nil {
		log.Error(upErr, ">>> [AppDep][K8s] UNEXPECTED ERROR during SETTING THE STATUS.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		return upErr
	}
	log.Info(">>> [AppDep][K8s] SUCCESSFULLY SET STATUS.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)

	return nil
}

func (r *ApplicationDeploymentReconciler) UpdateApplicationDeploymentStatus(ctx context.Context, appDeploy *v1beta1.ApplicationDeployment, fed *v1beta1.Federation) error {
	log := log.FromContext(ctx)
	appDeployHost := &v1beta1.ApplicationDeployment{}
	remoteName := appDeploy.Name
	if err := GetRemoteResource(ctx, r.Client, r.Scheme, fed, appDeployHost, remoteName, appDeploy.Name, appDeploy.Namespace, "[AppDep][K8s]"); err != nil {
		log.Error(err, ">>> [AppDep][K8s] Error retrieving remote resource.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		return err
	}
	appDeploy.Status = appDeployHost.Status
	upErr := r.Status().Update(ctx, appDeploy.DeepCopy())
	if upErr != nil {
		log.Error(upErr, ">>> [AppDep][K8s] UNEXPECTED ERROR during STATUS UPDATE.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		return upErr
	}
	log.Info(">>> [AppDep][K8s] SUCCESSFULLY UPDATED.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
	return nil
}

func (r *ApplicationDeploymentReconciler) DeleteApplicationDeployment(ctx context.Context, appDeploy *v1beta1.ApplicationDeployment, fed *v1beta1.Federation) error {
	remoteName := appDeploy.Name
	return DeleteRemoteResource(ctx, r.Client, r.Scheme, fed, &v1beta1.ApplicationDeployment{}, remoteName, appDeploy.Name, appDeploy.Namespace, "[AppDep][K8s]")
}
