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

// ApplicationOnboardingReconciler reconciles a ApplicationOnboarding object
type ApplicationOnboardingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
	K8sClient  *k8s.ApplicationOnboardingReconciler
	RestClient *rest.ApplicationOnboardingReconciler
}

type ExternalAppOnboardingClient interface {
	CreateApplicationOnboarding(ctx context.Context, f *v1beta1.ApplicationOnboarding, fed *v1beta1.Federation) error
	DeleteApplicationOnboarding(ctx context.Context, f *v1beta1.ApplicationOnboarding, fed *v1beta1.Federation) error
	UpdateApplicationOnboardingStatus(ctx context.Context, f *v1beta1.ApplicationOnboarding, fed *v1beta1.Federation) error //Callback for REST and GET for K8s
}

func (r *ApplicationOnboardingReconciler) getExternalClient(isRest bool) ExternalAppOnboardingClient {
	if isRest {
		return r.RestClient
	}
	return r.K8sClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *ApplicationOnboardingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.ApplicationOnboarding{}).
		Named("applicationonboarding").
		WatchesRawSource(
			source.Channel(
				k8s.ApplicationOnboardingRemoteEvents,
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=applicationonboardings,verbs=*,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=applicationonboardings/status,verbs=get;update;patch,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=applicationonboardings/finalizers,verbs=update,namespace=foo

func (r *ApplicationOnboardingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := ctrl.Log
	log.Info(">>> [AppOnboard] Starting RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)
	defer log.Info(">>> [AppOnboard] End RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)

	// Getting main ApplicationOnboarding or requeue
	var appOnboard v1beta1.ApplicationOnboarding
	if err := r.Get(ctx, req.NamespacedName, &appOnboard); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, ">>> [AppOnboard] Error getting object.", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}
	//Helper function to set the status to NotAvailable and update the resource
	skipStatusPatch := false
	originalAppOnboard := appOnboard.DeepCopy()
	defer func() {
		if skipStatusPatch {
			return
		}
		if err != nil {
			log.Error(err, ">>> [AppOnboard] UNEXPECTED ERROR detected in Reconcile, setting state to Failed before patching", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
			appOnboard.Status.State = v1beta1.ApplicationOnboardingStateFailed

		}
		if patchErr := r.Status().Patch(ctx, &appOnboard, client.MergeFrom(originalAppOnboard)); patchErr != nil {
			if !apierrors.IsNotFound(patchErr) {
				log.Error(patchErr, ">>> [AppOnboard] UNEXPECTED ERROR during AppOnboard UPDATE.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
			}
			if err == nil {
				err = patchErr
			}
		} else {
			log.Info(">>> [AppOnboard] SUCCESSFULLY.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
		}
	}()
	isGuest := IsGuestResource(appOnboard.Spec.RelationType)
	fed, isRest, err := GetFederation(ctx, isGuest, r.Client, appOnboard.Spec.FederationContextId, appOnboard.Namespace)
	extClient := r.getExternalClient(isRest)

	if err != nil {
		log.Error(err, ">>> [AppOnboard] Should always have a parent federation.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
		return ctrl.Result{}, err
	}
	// Check if the federation is locked and stop the watcher if it is (K8s only) or stop the callbacks if it is (REST only)
	if !CheckFederationState(fed, isRest, "AppOnboard", appOnboard.Name, appOnboard.Namespace) {
		return ctrl.Result{}, nil
	}

	// Handle deletion of the ApplicationOnboarding resource
	if !appOnboard.GetDeletionTimestamp().IsZero() {
		if isGuest {
			if err := extClient.DeleteApplicationOnboarding(ctx, &appOnboard, fed); err != nil {
				log.Error(err, ">>> [AppOnboard] Error deleting ApplicationOnboarding.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
				appOnboard.Status.State = v1beta1.ApplicationOnboardingStateFailed
				return ctrl.Result{}, err
			}
		}
		if controllerutil.RemoveFinalizer(&appOnboard, v1beta1.ApplicationOnboardingFinalizer) {
			log.Info(">>> [AppOnboard] Removed basic finalizer for ApplicationOnboarding, exiting...", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
			if err := r.Update(ctx, appOnboard.DeepCopy()); err != nil {
				log.Error(err, ">>> [AppOnboard] Unable to update ApplicationOnboarding while removing finalizers.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [AppOnboard] Successfully removed finalizer from ApplicationOnboarding.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
		}
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	// Handle creation/finalizer
	if controllerutil.AddFinalizer(&appOnboard, v1beta1.ApplicationOnboardingFinalizer) {
		log.Info(">>> [AppOnboard] Added finalizer to ApplicationOnboarding.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
		if err := r.Update(ctx, appOnboard.DeepCopy()); err != nil {
			log.Info(">>> [AppOnboard] Unable to Update ApplicationOnboarding with finalizer.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
			return ctrl.Result{}, err
		}
		log.Info(">>> [AppOnboard] Successfully added finalizer to ApplicationOnboarding.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	isNewAppOnboard := appOnboard.Status.State == ""
	if !isGuest {
		// Host ApplicationOnboarding handling
		if isNewAppOnboard {
			appOnboard.Status.State = v1beta1.ApplicationOnboardingStatePending
		} else {
			// Callback for REST and GET for K8s
			if isRest {
				if err := extClient.UpdateApplicationOnboardingStatus(ctx, &appOnboard, fed); err != nil {
					log.Error(err, ">>> [AppOnboard] Error during CALLBACK OPERATION via OPG EWBI API.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				log.Info(">>> [AppOnboard] Resource updated (GUEST via watcher update through the resource)", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
			}
		}
		return ctrl.Result{}, nil
	} else {
		// Guest ApplicationOnboarding handling
		if isNewAppOnboard {
			appComponentSpec := appOnboard.Spec.AppInfo.AppComponentSpecs
			for _, appComponent := range appComponentSpec {
				artefactId := appComponent.ArtefactId
				artObj := &v1beta1.Artefact{}
				if err := r.Get(ctx, client.ObjectKey{Name: artefactId, Namespace: artObj.Namespace}, artObj); err != nil {
					if apierrors.IsNotFound(err) {
						log.Error(err, ">>> [AppOnboard] Artefact not found for ApplicationOnboarding.", "name", artObj.Name, "namespace", artObj.Namespace, "artefactId", artefactId)
						return ctrl.Result{}, err
					}
					log.Error(err, ">>> [AppOnboard] Error getting Artefact for ApplicationOnboarding.", "name", artObj.Name, "namespace", artObj.Namespace, "artefactId", artefactId)
					return ctrl.Result{}, err
				}
				if artObj.Status.State != v1beta1.ArtefactStateReady {
					log.Info(">>> [AppOnboard] Artefact is not READY for ApplicationOnboarding.", "name", artObj.Name, "namespace", artObj.Namespace, "artefactId", artefactId, "state", artObj.Status.State)
					skipStatusPatch = true
					return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
				}
			}
			if err := extClient.CreateApplicationOnboarding(ctx, &appOnboard, fed); err != nil {
				log.Error(err, ">>> [AppOnboard] Error APPLYING/UPDATING SPEC ApplicationOnboarding.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [AppOnboard] SUCCESSFULLY APPLIED SPEC AND SET INITIAL STATUS.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
		} else {
			if isRest {
				log.Info(">>> [AppOnboard] Received UPDATEs via CALLBACK OPERATION with OPG EWBI API.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
			} else {
				if err := extClient.UpdateApplicationOnboardingStatus(ctx, &appOnboard, fed); err != nil {
					log.Error(err, ">>> [AppOnboard] Error updating ApplicationOnboarding.", "name", appOnboard.Name, "namespace", appOnboard.Namespace)
					return ctrl.Result{}, err
				}
			}
		}
	}
	return ctrl.Result{}, nil
}
