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

package controller

import (
	"context"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	k8s "github.com/neonephos-katalis/opg-ewbi-operator/internal/k8s"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/opg"
	rest "github.com/neonephos-katalis/opg-ewbi-operator/internal/rest"
)

// ApplicationDeploymentReconciler reconciles a ApplicationDeployment object
type ApplicationDeploymentReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
	K8sClient  *k8s.ApplicationDeploymentReconciler
	RestClient *rest.ApplicationDeploymentReconciler
}
type ExternalAppDeployClient interface {
	CreateApplicationDeployment(ctx context.Context, f *v1beta1.ApplicationDeployment, fed *v1beta1.Federation) error
	DeleteApplicationDeployment(ctx context.Context, f *v1beta1.ApplicationDeployment, fed *v1beta1.Federation) error
	UpdateApplicationDeploymentStatus(ctx context.Context, f *v1beta1.ApplicationDeployment, fed *v1beta1.Federation) error //Callback for REST and GET for K8s
}

func (r *ApplicationDeploymentReconciler) getExternalClient(isRest bool) ExternalAppDeployClient {
	if isRest {
		return r.RestClient
	}
	return r.K8sClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationDeploymentReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.ApplicationDeployment{}).
		Named("applicationdeployment").
		WatchesRawSource(
			source.Channel(
				k8s.ApplicationDeploymentRemoteEvents,
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=applicationdeployments,verbs=*,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=applicationdeployments/status,verbs=get;update;patch,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=applicationdeployments/finalizers,verbs=update,namespace=foo

func (r *ApplicationDeploymentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := ctrl.Log
	log.Info(">>> [AppDeploy] Starting RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)
	defer log.Info(">>> [AppDeploy] End RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)

	// Getting main ApplicationDeployment or requeue
	var appDeploy v1beta1.ApplicationDeployment
	if err := r.Get(ctx, req.NamespacedName, &appDeploy); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, ">>> [AppDeploy] Error getting object.", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}
	//Helper function to set the status to NotAvailable and update the resource
	skipStatusPatch := false
	originalAppDeploy := appDeploy.DeepCopy()
	defer func() {
		if skipStatusPatch {
			return
		}
		if err != nil {
			log.Error(err, ">>> [AppDeploy] UNEXPECTED ERROR detected in Reconcile, setting state to Failed before patching", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
			appDeploy.Status.AppInstanceInfo.AppInstanceState = v1beta1.ApplicationDeploymentStateFailed

		}
		if patchErr := r.Status().Patch(ctx, &appDeploy, client.MergeFrom(originalAppDeploy)); patchErr != nil {
			if !apierrors.IsNotFound(patchErr) {
				log.Error(patchErr, ">>> [AppDeploy] UNEXPECTED ERROR during AppDeploy UPDATE.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
			}
			if err == nil {
				err = patchErr
			}
		} else {
			log.Info(">>> [AppDeploy] SUCCESSFULLY.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		}
	}()
	isGuest := IsGuestResource(appDeploy.Spec.RelationType)
	fed, isRest, err := GetFederation(ctx, isGuest, r.Client, appDeploy.Spec.FederationContextId, appDeploy.Namespace)
	extClient := r.getExternalClient(isRest)
	if err != nil {
		log.Error(err, ">>> [AppDeploy] Should always have a parent federation.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		return ctrl.Result{}, err
	}

	// Check if the federation is locked and stop the watcher if it is (K8s only) or stop the callbacks if it is (REST only)
	if !CheckFederationState(fed, isRest, "AppDeploy", appDeploy.Name, appDeploy.Namespace) {
		return ctrl.Result{}, nil
	}
	// Get the appropriate external client based on the federation technology

	// Handle deletion of the ApplicationDeployment resource
	if !appDeploy.GetDeletionTimestamp().IsZero() {
		if isGuest {
			if err := extClient.DeleteApplicationDeployment(ctx, &appDeploy, fed); err != nil {
				log.Error(err, ">>> [AppDeploy] Error deleting ApplicationDeployment.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
				return ctrl.Result{}, err
			}
		}
		if controllerutil.RemoveFinalizer(&appDeploy, v1beta1.ApplicationDeploymentFinalizer) {
			log.Info(">>> [AppDeploy] Removed basic finalizer for ApplicationDeployment, exiting...", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
			if err := r.Update(ctx, appDeploy.DeepCopy()); err != nil {
				log.Error(err, ">>> [AppDeploy] Unable to update ApplicationDeployment while removing finalizers.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [AppDeploy] Successfully removed finalizer from ApplicationDeployment.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		}
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	// Handle creation/finalizer
	if controllerutil.AddFinalizer(&appDeploy, v1beta1.ApplicationDeploymentFinalizer) {
		log.Info(">>> [AppDeploy] Added finalizer to ApplicationDeployment.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		if err := r.Update(ctx, appDeploy.DeepCopy()); err != nil {
			log.Error(err, ">>> [AppDeploy] Unable to Update ApplicationDeployment with finalizer.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
			return ctrl.Result{}, err
		}
		log.Info(">>> [AppDeploy] Successfully added finalizer to ApplicationDeployment.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	isNewAppDeploy := appDeploy.Status.AppInstanceInfo.AppInstanceState == ""
	if !isGuest {
		// Host ApplicationDeployment handling
		if isNewAppDeploy {
			appDeploy.Status.AppInstanceInfo.AppInstanceState = v1beta1.ApplicationDeploymentStatePending
		} else {
			if isRest {
				if err := extClient.UpdateApplicationDeploymentStatus(ctx, &appDeploy, fed); err != nil {
					log.Error(err, ">>> [AppDeploy] Error during CALLBACK OPERATION via OPG EWBI API.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				log.Info(">>> [AppDeploy] Resource updated (GUEST via watcher update through the resource)", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
			}
		}
	} else {
		// Guest ApplicationDeployment handling
		if isNewAppDeploy {
			appId := appDeploy.Spec.AppId
			appObj := &v1beta1.ApplicationOnboarding{}
			if err := r.Get(ctx, client.ObjectKey{Name: appId, Namespace: appObj.Namespace}, appObj); err != nil {
				if apierrors.IsNotFound(err) {
					log.Error(err, ">>> [AppOnboard] ApplicationOnboarding not found for ApplicationDeployment.", "name", appObj.Name, "namespace", appObj.Namespace, "appId", appId)
					return ctrl.Result{}, err
				}
				log.Error(err, ">>> [AppOnboard] Error getting ApplicationOnboarding for ApplicationDeployment.", "name", appObj.Name, "namespace", appObj.Namespace, "appId", appId)
				return ctrl.Result{}, err
			}
			if appObj.Status.State != v1beta1.ApplicationOnboardingStateOnboarded {
				log.Info(">>> [AppOnboard] ApplicationOnboarding is not ONBOARDED for ApplicationDeployment.", "name", appObj.Name, "namespace", appObj.Namespace, "appId", appId, "state", appObj.Status.State)
				skipStatusPatch = true
				return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
			}
			if err := extClient.CreateApplicationDeployment(ctx, &appDeploy, fed); err != nil {
				log.Error(err, ">>> [AppDeploy] Error APPLYING/UPDATING SPEC ApplicationDeployment.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
				return ctrl.Result{}, err
			}
		} else {
			if isRest {
				log.Info(">>> [AppDeploy] Received UPDATEs via CALLBACK OPERATION with OPG EWBI API.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
			} else {
				if err := extClient.UpdateApplicationDeploymentStatus(ctx, &appDeploy, fed); err != nil {
					log.Error(err, ">>> [AppDeploy] Error updating ApplicationDeployment.", "name", appDeploy.Name, "namespace", appDeploy.Namespace)
					return ctrl.Result{}, err
				}
			}
		}

	}
	return ctrl.Result{}, nil
}
