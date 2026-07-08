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

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/indexer"
	k8s "github.com/neonephos-katalis/opg-ewbi-operator/internal/k8s"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/opg"
	rest "github.com/neonephos-katalis/opg-ewbi-operator/internal/rest"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// ImageReconciler reconciles an Image object
type ImageReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
	K8sClient  *k8s.ImageReconciler
	RestClient *rest.ImageReconciler
}
type ExternalImageClient interface {
	CreateImage(ctx context.Context, f *v1beta1.Image, fed *v1beta1.Federation) error
	DeleteImage(ctx context.Context, f *v1beta1.Image, fed *v1beta1.Federation) error
	UpdateImageStatus(ctx context.Context, f *v1beta1.Image, fed *v1beta1.Federation) error //Callback for REST and GET for K8s
}

func (r *ImageReconciler) getExternalClient(isRest bool) ExternalImageClient {
	if isRest {
		return r.RestClient
	}
	return r.K8sClient
}

// SetupWithManager sets up the controller with the Manager.
func (r *ImageReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := indexer.GetFederationIndexers(context.Background(), mgr); err != nil {
		return err
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.Image{}).
		Named("image").
		WatchesRawSource(
			source.Channel(
				k8s.ImageRemoteEvents,
				&handler.EnqueueRequestForObject{},
			),
		).
		Complete(r)
}

// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=images,verbs=*,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=images/status,verbs=get;update;patch,namespace=foo
// +kubebuilder:rbac:groups=opg.ewbi.katalis.com,resources=images/finalizers,verbs=update,namespace=foo

func (r *ImageReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := ctrl.Log
	log.Info(">>> [Image] Starting RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)
	defer log.Info(">>> [Image] End RECONCILE FUNCTION.", "name", req.Name, "namespace", req.Namespace)

	// Getting main Image or requeue
	var image v1beta1.Image
	if err := r.Get(ctx, req.NamespacedName, &image); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		log.Error(err, ">>> [Image] Error getting object.", "name", req.Name, "namespace", req.Namespace)
		return ctrl.Result{}, err
	}
	//Helper function to set the status to NotAvailable and update the resource
	skipStatusPatch := false
	originalImage := image.DeepCopy()
	defer func() {
		if skipStatusPatch {
			return
		}
		if err != nil {
			log.Error(err, ">>> [Image] UNEXPECTED ERROR detected in Reconcile, setting state to Failed before patching", "name", image.Name, "namespace", image.Namespace)
			image.Status.State = v1beta1.ImageStateError

		}
		if patchErr := r.Status().Patch(ctx, &image, client.MergeFrom(originalImage)); patchErr != nil {
			if !apierrors.IsNotFound(patchErr) {
				log.Error(patchErr, ">>> [Image] UNEXPECTED ERROR during Image UPDATE.", "name", image.Name, "namespace", image.Namespace)
			}
			if err == nil {
				err = patchErr
			}
		} else {
			log.Info(">>> [Image] SUCCESSFULLY.", "name", image.Name, "namespace", image.Namespace)
		}
	}()

	isGuest := IsGuestResource(image.Spec.RelationType)
	fed, isRest, err := GetFederation(ctx, isGuest, r.Client, image.Spec.FederationContextId, image.Namespace)
	extClient := r.getExternalClient(isRest) // Get the appropriate external client based on the federation technology
	if err != nil {
		log.Error(err, ">>> [Image] Should always have a parent federation.", "name", image.Name, "namespace", image.Namespace)
		image.Status.State = v1beta1.ImageStateError
		return ctrl.Result{}, err
	}

	// Check if the federation is locked and stop the watcher if it is (K8s only) or stop the callbacks if it is (REST only)
	if !CheckFederationState(fed, isRest, "Image", image.Name, image.Namespace) {
		return ctrl.Result{}, nil
	}

	// Handle deletion of the image resource
	if !image.GetDeletionTimestamp().IsZero() {
		if isGuest {
			if err := extClient.DeleteImage(ctx, &image, fed); err != nil {
				log.Error(err, ">>> [Image] Error deleting Image.", "name", image.Name, "namespace", image.Namespace)
				return ctrl.Result{}, err
			}
		}
		if controllerutil.RemoveFinalizer(&image, v1beta1.ImageFinalizer) {
			log.Info(">>> [Image] Removed basic finalizer for Image, exiting...", "name", image.Name, "namespace", image.Namespace)
			if err := r.Update(ctx, image.DeepCopy()); err != nil {
				log.Error(err, ">>> [Image] Unable to update Image while removing finalizers.", "name", image.Name, "namespace", image.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [Image] Successfully removed finalizer from Image.", "name", image.Name, "namespace", image.Namespace)
		}
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	// Handle creation/finalizer
	if controllerutil.AddFinalizer(&image, v1beta1.ImageFinalizer) {
		log.Info(">>> [Image] Added finalizer to Image.", "name", image.Name, "namespace", image.Namespace)
		if err := r.Update(ctx, image.DeepCopy()); err != nil {
			log.Error(err, ">>> [Image] Unable to Update Image with finalizer.", "name", image.Name, "namespace", image.Namespace)
			return ctrl.Result{}, err
		}
		log.Info(">>> [Image] Successfully added finalizer to Image.", "name", image.Name, "namespace", image.Namespace)
		skipStatusPatch = true
		return ctrl.Result{}, nil
	}

	isNewImage := image.Status.State == ""

	if !isGuest {
		// Host IMAGE handling
		if isNewImage {
			image.Status.State = v1beta1.ImageStatePending
		} else {
			// Callback for REST and GET for K8s
			if isRest {
				if err := extClient.UpdateImageStatus(ctx, &image, fed); err != nil {
					log.Error(err, ">>> [Image] Error during CALLBACK OPERATION via OPG EWBI API.", "name", image.Name, "namespace", image.Namespace)
					return ctrl.Result{}, err
				}
			} else {
				log.Info(">>> [Image] Resource updated (GUEST via watcher update through the resource)", "name", image.Name, "namespace", image.Namespace)
			}

		}
		return ctrl.Result{}, nil
	} else {
		// Guest IMAGE handling
		if isNewImage {
			if err := extClient.CreateImage(ctx, &image, fed); err != nil {
				log.Error(err, ">>> [Image] Error APPLYING/UPDATING SPEC Image.", "name", image.Name, "namespace", image.Namespace)
				return ctrl.Result{}, err
			}
			log.Info(">>> [Image] SUCCESSFULLY APPLIED SPEC AND SET INITIAL STATUS.", "name", image.Name, "namespace", image.Namespace)
		} else {
			if isRest {
				log.Info(">>> [Image] Received UPDATEs via CALLBACK OPERATION with OPG EWBI API.", "name", image.Name, "namespace", image.Namespace)
			} else {
				if err := extClient.UpdateImageStatus(ctx, &image, fed); err != nil {
					log.Error(err, ">>> [Image] Error updating Image.", "name", image.Name, "namespace", image.Namespace)
					return ctrl.Result{}, err
				}
			}
		}
	}
	return ctrl.Result{}, nil
}
