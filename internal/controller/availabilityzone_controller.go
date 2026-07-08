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

// AvailabilityZoneReconciler reconciles a AvailabilityZone object
type AvailabilityZoneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
	K8sClient  *k8s.ZoneReconciler
	RestClient *rest.ZoneReconciler
}

type ExternalAzClient interface {
	AcceptZone(ctx context.Context, az *v1beta1.AvailabilityZone, fed *v1beta1.Federation) error
	DeleteZone(ctx context.Context, az *v1beta1.AvailabilityZone, fed *v1beta1.Federation) error
	UpdateZoneStatus(ctx context.Context, az *v1beta1.AvailabilityZone, fed *v1beta1.Federation) error //Callback for REST and GET for K8s
}

func (r *AvailabilityZoneReconciler) getExternalClient(isRest bool) ExternalAzClient {
	if isRest {
		return r.RestClient
	}
	return r.K8sClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *AvailabilityZoneReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.AvailabilityZone{}).
		Named("availabilityzone").
		WatchesRawSource(
			source.Channel(
				k8s.AvailabilityZoneRemoteEvents,
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=availabilityzones,verbs=*,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=availabilityzones/status,verbs=get;update;patch,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=availabilityzones/finalizers,verbs=update,namespace=foo

func (r *AvailabilityZoneReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (res ctrl.Result, err error) {
	log := ctrl.Log
	log.Info(">>> [AZ] Starting RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)
	defer log.Info(">>> [AZ] End RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)

	// Getting main AZ or requeue
	var zone v1beta1.AvailabilityZone
	if err := r.Get(ctx, req.NamespacedName, &zone); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, ">>> [AZ] Error getting resource.", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}
	//Helper function to set the status to NotAvailable and update the resource
	skipStatusPatch := false
	originalZone := zone.DeepCopy()
	defer func() {
		if skipStatusPatch {
			return
		}
		if err != nil {
			log.Error(err, ">>> [AZ] UNEXPECTED ERROR detected in Reconcile, setting state to Failed before patching", "name", zone.Name, "namespace", zone.Namespace)
			zone.Status.State = v1beta1.ZoneStateFailed
		}
		if patchErr := r.Status().Patch(ctx, &zone, client.MergeFrom(originalZone)); patchErr != nil {
			if !apierrors.IsNotFound(patchErr) {
				log.Error(patchErr, ">>> [AZ] UNEXPECTED ERROR during AZ UPDATE.", "name", zone.Name, "namespace", zone.Namespace)
			}
			if err == nil {
				err = patchErr
			}
		} else {
			log.Info(">>> [AZ] SUCCESSFULLY.", "name", zone.Name, "namespace", zone.Namespace)
		}
	}()

	isGuest := IsGuestResource(zone.Spec.RelationType)
	fed, isRest, err := GetFederation(ctx, isGuest, r.Client, zone.Spec.FederationContextId, zone.Namespace)
	extClient := r.getExternalClient(isRest)
	if err != nil {
		log.Error(err, ">>> [AZ] Should always have a parent federation.", "name", zone.Name, "namespace", zone.Namespace)
		return ctrl.Result{}, err
	}

	// Check if the federation is locked and stop the watcher if it is (K8s only) or stop the callbacks if it is (REST only)
	if !CheckFederationState(fed, isRest, "AZ", zone.Name, zone.Namespace) {
		return ctrl.Result{}, nil
	}

	// Handle deletion of the AZ resource
	if !zone.GetDeletionTimestamp().IsZero() {
		if err := extClient.DeleteZone(ctx, &zone, fed); err != nil {
			log.Error(err, ">>> [AZ] Error deleting external AZ.", "name", zone.Name, "namespace", zone.Namespace)
			return ctrl.Result{}, err
		}
		if controllerutil.RemoveFinalizer(&zone, v1beta1.AvailabilityZoneFinalizer) {
			log.Info(">>> [AZ] Removed basic finalizer for AZ, exiting...", "name", zone.Name, "namespace", zone.Namespace)
			if err := r.Update(ctx, zone.DeepCopy()); err != nil {
				log.Error(err, ">>> [AZ] Unable to update while removing finalizers.", "name", zone.Name, "namespace", zone.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [AZ] Successfully removed finalizer from AZ.", "name", zone.Name, "namespace", zone.Namespace)
		}
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	// Handle creation/finalizer
	if controllerutil.AddFinalizer(&zone, v1beta1.AvailabilityZoneFinalizer) {
		log.Info(">>> [AZ] Added finalizer to AZ", "name", zone.Name, "namespace", zone.Namespace)
		if err := r.Update(ctx, zone.DeepCopy()); err != nil {
			log.Info(">>> [AZ] Unable to Update AZ with finalizer", "name", zone.Name, "namespace", zone.Namespace)
			return ctrl.Result{}, err
		}
		log.Info(">>> [AZ] Successfully added finalizer to AZ", "name", zone.Name, "namespace", zone.Namespace)
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	isNewZone := zone.Status.State == ""

	if !isGuest {
		// Host AZ handling
		if isNewZone {
			zone.Status.State = v1beta1.ZoneStateNotAvailable
		} else {
			if isRest {
				if err := extClient.UpdateZoneStatus(ctx, &zone, fed); err != nil {
					log.Error(err, ">>> [AZ] Error during CALLBACK OPERATION via OPG EWBI API.", "name", zone.Name, "namespace", zone.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				log.Info(">>> [AZ] Resource updated (GUEST via watcher update through the resource)", "name", zone.Name, "namespace", zone.Namespace)
			}
		}
	} else {
		// Guest AZ handling
		if isNewZone {
			if err := extClient.AcceptZone(ctx, &zone, fed); err != nil {
				log.Error(err, ">>> [AZ] Error accepting Zone", "name", zone.Name, "namespace", zone.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [AZ] SUCCESSFULLY APPLIED SPEC AND SET INITIAL STATUS.", "name", zone.Name, "namespace", zone.Namespace)
		} else {
			if isRest {
				log.Info(">>> [AZ] Received UPDATEs via CALLBACK OPERATION with OPG EWBI API", "name", zone.Name, "namespace", zone.Namespace)
			} else {
				// Watcher
				if err := extClient.UpdateZoneStatus(ctx, &zone, fed); err != nil {
					log.Error(err, ">>> [AZ] Error updating Zone.", "name", zone.Name, "namespace", zone.Namespace)
					return ctrl.Result{}, err
				}
			}
		}
	}
	return ctrl.Result{}, nil
}
