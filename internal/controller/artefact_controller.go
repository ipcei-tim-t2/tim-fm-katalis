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

// ArtefactReconciler reconciles a Artefact object
type ArtefactReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
	K8sClient  *k8s.ArtefactReconciler
	RestClient *rest.ArtefactReconciler
}

type ExternalArtefactClient interface {
	CreateArtefact(ctx context.Context, f *v1beta1.Artefact, fed *v1beta1.Federation) error
	DeleteArtefact(ctx context.Context, f *v1beta1.Artefact, fed *v1beta1.Federation) error
	UpdateArtefactStatus(ctx context.Context, f *v1beta1.Artefact, fed *v1beta1.Federation) error //Callback for REST and GET for K8s
}

func (r *ArtefactReconciler) getExternalClient(isRest bool) ExternalArtefactClient {
	if isRest {
		return r.RestClient
	}
	return r.K8sClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *ArtefactReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.Artefact{}).
		Named("artefact").
		WatchesRawSource(
			source.Channel(
				k8s.ArtefactRemoteEvents,
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=artefacts,verbs=*,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=artefacts/status,verbs=get;update;patch,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=artefacts/finalizers,verbs=update,namespace=foo

func (r *ArtefactReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := ctrl.Log
	log.Info(">>> [Artefact] Starting RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)
	defer log.Info(">>> [Artefact] End RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)

	// Getting main artefact or requeue
	var art v1beta1.Artefact
	if err := r.Get(ctx, req.NamespacedName, &art); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, ">>> [Artefact] Error getting artefact object.", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}

	//Helper function to set the status to NotAvailable and update the resource
	skipStatusPatch := false
	originalArtefact := art.DeepCopy()
	defer func() {
		if skipStatusPatch {
			return
		}
		if err != nil {
			log.Error(err, ">>> [Artefact] UNEXPECTED ERROR detected in Reconcile, setting state to Failed before patching", "name", art.Name, "namespace", art.Namespace)
			art.Status.State = v1beta1.ArtefactStateError

		}
		if patchErr := r.Status().Patch(ctx, &art, client.MergeFrom(originalArtefact)); patchErr != nil {
			if !apierrors.IsNotFound(patchErr) {
				log.Error(patchErr, ">>> [Artefact] UNEXPECTED ERROR during Artefact UPDATE.", "name", art.Name, "namespace", art.Namespace)
			}
			if err == nil {
				err = patchErr
			}
		} else {
			log.Info(">>> [Artefact] SUCCESSFULLY.", "name", art.Name, "namespace", art.Namespace)
		}
	}()

	isGuest := IsGuestResource(art.Spec.RelationType)
	fed, isRest, err := GetFederation(ctx, isGuest, r.Client, art.Spec.FederationContextId, art.Namespace)
	extClient := r.getExternalClient(isRest)

	if err != nil {
		log.Error(err, ">>> [Artefact] Should always have a parent federation.", "name", art.Name, "namespace", art.Namespace)
		art.Status.State = v1beta1.ArtefactStateError
		return ctrl.Result{}, err
	}

	// Check if the federation is locked and stop the watcher if it is (K8s only) or stop the callbacks if it is (REST only)
	if !CheckFederationState(fed, isRest, "Artefact", art.Name, art.Namespace) {
		return ctrl.Result{}, nil
	}

	// Handle deletion of the Artefact resource
	if !art.GetDeletionTimestamp().IsZero() {
		if isGuest {
			if err := extClient.DeleteArtefact(ctx, &art, fed); err != nil {
				log.Error(err, ">>> [Artefact] Error deleting Artefact.", "name", art.Name, "namespace", art.Namespace)
				art.Status.State = v1beta1.ArtefactStateError
				return ctrl.Result{}, err
			}
		}
		if controllerutil.RemoveFinalizer(&art, v1beta1.ArtefactFinalizer) {
			log.Info(">>> [Artefact] Removed basic finalizer for Artefact, exiting...", "name", art.Name, "namespace", art.Namespace)
			if err := r.Update(ctx, art.DeepCopy()); err != nil {
				log.Info(">>> [Artefact] Unable to update Artefact while removing finalizers.", "name", art.Name, "namespace", art.Namespace)
				return ctrl.Result{}, nil
			}
			log.Info(">>> [Artefact] Successfully removed finalizer from Artefact.", "name", art.Name, "namespace", art.Namespace)
		}
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	// Handle creation/finalizer
	if controllerutil.AddFinalizer(&art, v1beta1.ArtefactFinalizer) {
		log.Info(">>> [Artefact] Added finalizer to Artefact.", "name", art.Name, "namespace", art.Namespace)
		if err := r.Update(ctx, art.DeepCopy()); err != nil {
			log.Info(">>> [Artefact] Unable to Update Artefact with finalizer.", "name", art.Name, "namespace", art.Namespace)
			return ctrl.Result{}, err
		}
		log.Info(">>> [Artefact] Successfully added finalizer to Artefact.", "name", art.Name, "namespace", art.Namespace)
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	isNewArtefact := art.Status.State == ""

	if !isGuest {
		// Host Artefact handling
		if isNewArtefact {
			art.Status.State = v1beta1.ArtefactStateReconciling
		} else {
			if isRest {
				if err := extClient.UpdateArtefactStatus(ctx, &art, fed); err != nil {
					log.Error(err, ">>> [Artefact] Error during CALLBACK OPERATION via OPG EWBI API.", "name", art.Name, "namespace", art.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				log.Info(">>> [Artefact] Resource updated (GUEST via watcher update through the resource)", "name", art.Name, "namespace", art.Namespace)
			}
		}
		return ctrl.Result{}, nil
	} else {
		// Guest Artefact handling
		if isNewArtefact {
			componentSpec := art.Spec.ArtefactBody.ComponentSpec
			for _, component := range componentSpec {
				image := component.Images
				for _, imageId := range image {
					imageObj := &v1beta1.Image{}
					if err := r.Get(ctx, client.ObjectKey{Name: imageId, Namespace: art.Namespace}, imageObj); err != nil {
						if apierrors.IsNotFound(err) {
							log.Error(err, ">>> [Artefact] Image not found for Artefact.", "name", art.Name, "namespace", art.Namespace, "imageId", imageId)
							return ctrl.Result{}, err
						}
						log.Error(err, ">>> [Artefact] Error getting Image for Artefact.", "name", art.Name, "namespace", art.Namespace, "imageId", imageId)
						return ctrl.Result{}, err
					}
					if imageObj.Status.State != v1beta1.ImageStateUploaded {
						log.Info(">>> [Artefact] Image is not UPLOADED for Artefact.", "name", art.Name, "namespace", art.Namespace, "imageId", imageId, "imageState", imageObj.Status.State)
						skipStatusPatch = true
						return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
					}
				}
			}
			if err := extClient.CreateArtefact(ctx, &art, fed); err != nil {
				log.Info(">>> [Artefact] Error APPLYING/UPDATING SPEC Artefact.", "name", art.Name, "namespace", art.Namespace)
				return ctrl.Result{}, nil
			}
			log.Info(">>> [Artefact] SUCCESSFULLY APPLIED SPEC AND SET INITIAL STATUS.", "name", art.Name, "namespace", art.Namespace)
			return ctrl.Result{}, nil
		} else {
			if isRest {
				log.Info(">>> [Artefact] Received UPDATEs via CALLBACK OPERATION with OPG EWBI API.", "name", art.Name, "namespace", art.Namespace)
			} else {
				if err := extClient.UpdateArtefactStatus(ctx, &art, fed); err != nil {
					return ctrl.Result{}, nil
				}
			}
		}

	}
	return ctrl.Result{}, nil
}
