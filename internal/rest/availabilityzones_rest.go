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

package rest

import (
	"context"

	"github.com/google/uuid"
	opgmodels "github.com/neonephos-katalis/opg-ewbi-operator/api/ewbi/models"
	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/opg"
	uu "github.com/neonephos-katalis/opg-ewbi-operator/pkg/uuid"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ZoneReconciler reconciles a Zone object
type ZoneReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
}

// Accept/Create
func (r *ZoneReconciler) AcceptZone(ctx context.Context, zone *v1beta1.AvailabilityZone, fed *v1beta1.Federation) error {
	log := ctrl.Log
	log.Info(">>> [AZ][REST] Accepting AZ", "zone", zone.Name, "namespace", zone.Namespace, "federation", fed.Name)

	zoneReq := opgmodels.ZoneRegistrationRequestData{
		AcceptedAvailabilityZones: []opgmodels.ZoneIdentifier{zone.Spec.ZoneId},
	}
	res, err := r.GetOPGClient(
		fed.Status.FederationContextId,
		fed.Spec.FederationData.RestOptions.TokenUrl,
		fed.Spec.FederationData.ClientId,
	).ZoneSubscribeWithResponse(
		context.TODO(),
		zone.Spec.FederationContextId,
		zoneReq,
	)
	if err != nil {
		log.Error(err, ">>> [AZ][REST] Error accepting AZ", "zone", zone.Name, "namespace", zone.Namespace)
		return err
	}

	statusCode := res.StatusCode()

	switch {
	case statusCode == 200:
		zoneResponse := res.JSON200
		log.Info(">>> [AZ][REST] 200 (POST) - Zone registered successfully", "zone", zone.Name, "namespace", zone.Namespace)
		acceptedZoneResourceInfo := zoneResponse.AcceptedZoneResourceInfo[0]
		// Update the zone status with the accepted resource info
		if acceptedZoneResourceInfo.ComputeResourceQuotaLimits != nil {
			for _, limits := range acceptedZoneResourceInfo.ComputeResourceQuotaLimits {
				var gpuSupported []v1beta1.GPUResource
				for _, gpu := range *limits.Gpu {
					gpuSupported = append(gpuSupported, v1beta1.GPUResource{
						GpuVendorType: string(gpu.GpuVendorType),
						GpuModeName:   string(gpu.GpuModeName),
						GpuMemory:     gpu.GpuMemory,
						NumGPU:        gpu.NumGPU,
					})
				}
				var hfSupported []v1beta1.Hugepage
				for _, hf := range *limits.Hugepages {
					hfSupported = append(hfSupported, v1beta1.Hugepage{
						PageSize: string(hf.PageSize),
						Number:   hf.Number,
					})
				}
				zone.Status.ComputeResourceQuotaLimits = append(zone.Status.ComputeResourceQuotaLimits, v1beta1.ComputeResourceInfo{
					CpuArchType:    string(limits.CpuArchType),
					CpuExclusivity: *limits.CpuExclusivity,
					DiskStorage:    *limits.DiskStorage,
					Fpga:           *limits.Fpga,
					Gpu:            gpuSupported,
					Hugepages:      hfSupported,
					Memory:         limits.Memory,
					NumCPU:         string(limits.NumCPU),
					Vpu:            *limits.Vpu,
				})
			}
		}
		if acceptedZoneResourceInfo.FlavoursSupported != nil {
			for _, flavour := range acceptedZoneResourceInfo.FlavoursSupported {
				var osTypes []v1beta1.SupportedOSType
				for _, osType := range flavour.SupportedOSTypes {
					osTypes = append(osTypes, v1beta1.SupportedOSType{
						Architecture: string(osType.Architecture),
						Distribution: string(osType.Distribution),
						License:      string(osType.License),
						Version:      string(osType.Version),
					})
				}
				var gpuSupported []v1beta1.GPUResource
				for _, gpu := range *flavour.Gpu {
					gpuSupported = append(gpuSupported, v1beta1.GPUResource{
						GpuVendorType: string(gpu.GpuVendorType),
						GpuModeName:   string(gpu.GpuModeName),
						GpuMemory:     gpu.GpuMemory,
						NumGPU:        gpu.NumGPU,
					})
				}
				var hfSupported []v1beta1.Hugepage
				for _, hf := range *flavour.Hugepages {
					hfSupported = append(hfSupported, v1beta1.Hugepage{
						PageSize: string(hf.PageSize),
						Number:   hf.Number,
					})
				}
				// Ora aggiungiamo l'intero flavour con la slice osTypes già pronta
				zone.Status.FlavoursSupported = append(zone.Status.FlavoursSupported, v1beta1.FlavourSupported{
					FlavourId:        flavour.FlavourId,
					CpuArchType:      string(flavour.CpuArchType),
					SupportedOSTypes: osTypes, // Assegnazione pulita
					NumCPU:           flavour.NumCPU,
					MemorySize:       flavour.MemorySize,
					StorageSize:      flavour.StorageSize,
					Gpu:              gpuSupported,
					Fpga:             *flavour.Fpga,
					Hugepages:        hfSupported,
					Vpu:              *flavour.Vpu,
					CpuExclusivity:   *flavour.CpuExclusivity,
				})
			}
		}
		if acceptedZoneResourceInfo.ReservedComputeResources != nil {
			for _, limits := range acceptedZoneResourceInfo.ReservedComputeResources {
				var gpuSupported []v1beta1.GPUResource
				for _, gpu := range *limits.Gpu {
					gpuSupported = append(gpuSupported, v1beta1.GPUResource{
						GpuVendorType: string(gpu.GpuVendorType),
						GpuModeName:   string(gpu.GpuModeName),
						GpuMemory:     gpu.GpuMemory,
						NumGPU:        gpu.NumGPU,
					})
				}
				var hfSupported []v1beta1.Hugepage
				for _, hf := range *limits.Hugepages {
					hfSupported = append(hfSupported, v1beta1.Hugepage{
						PageSize: string(hf.PageSize),
						Number:   hf.Number,
					})
				}
				zone.Status.ReservedComputeResources = append(zone.Status.ReservedComputeResources, v1beta1.ComputeResourceInfo{
					CpuArchType:    string(limits.CpuArchType),
					CpuExclusivity: *limits.CpuExclusivity,
					DiskStorage:    *limits.DiskStorage,
					Fpga:           *limits.Fpga,
					Gpu:            gpuSupported,
					Hugepages:      hfSupported,
					Memory:         limits.Memory,
					NumCPU:         string(limits.NumCPU),
					Vpu:            *limits.Vpu,
				})
			}
		}
		if acceptedZoneResourceInfo.NetworkResources != nil {
			zone.Status.NetworkResources = &v1beta1.NetworkResources{
				EgressBandWidth: acceptedZoneResourceInfo.NetworkResources.EgressBandWidth,
				DedicatedNIC:    acceptedZoneResourceInfo.NetworkResources.DedicatedNIC,
				SupportSriov:    acceptedZoneResourceInfo.NetworkResources.SupportSriov,
				SupportDPDK:     acceptedZoneResourceInfo.NetworkResources.SupportDPDK,
			}
		}
		if acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo != nil {
			zone.Status.ZoneServiceLevelObjsInfo = &v1beta1.ZoneServiceLevelObjsInfo{
				LatencyRanges: &v1beta1.LatencyRanges{
					MinLatency: *acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo.LatencyRanges.MinLatency,
					MaxLatency: *acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo.LatencyRanges.MaxLatency,
				},
				JitterRanges: &v1beta1.JitterRanges{
					MinJitter: *acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo.JitterRanges.MinJitter,
					MaxJitter: *acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo.JitterRanges.MaxJitter,
				},
				ThroughputRanges: &v1beta1.ThroughputRanges{
					MinThroughput: *acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo.ThroughputRanges.MinThroughput,
					MaxThroughput: *acceptedZoneResourceInfo.ZoneServiceLevelObjsInfo.ThroughputRanges.MaxThroughput,
				},
			}
		}
		zone.Status.State = v1beta1.ZoneStateAvailable
	case statusCode == 400:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON400)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 401:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON401)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 404:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON404)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 409:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON409)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 422:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON422)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 500:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON500)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 503:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON503)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 520:
		handleFederationProblemDetails(log, statusCode, res.ApplicationproblemJSON520)
		zone.Status.State = v1beta1.ZoneStateFailed
	default:
		log.Info(">>> [AZ][REST] Unexpected Status Code", "status", statusCode, "body", string(res.Body))
		zone.Status.State = v1beta1.ZoneStateNotAvailable
	}
	return nil
}

// Delete
func (r *ZoneReconciler) DeleteZone(ctx context.Context, zone *v1beta1.AvailabilityZone, fed *v1beta1.Federation) error {
	log := ctrl.Log
	log.Info(">>> [AZ][REST] DELETING ZONE", "name", zone.Name, "namespace", zone.Namespace)
	// we should delete the zone from the federation partner via REST API
	zoneId, err := uuid.Parse("fed-" + uu.V5(zone.Spec.FederationContextId+zone.Spec.ZoneId))
	if err != nil {
		return err
	}
	res, err := r.GetOPGClient(
		fed.Status.FederationContextId,
		fed.Spec.FederationData.RestOptions.TokenUrl,
		fed.Spec.FederationData.ClientId,
	).ZoneUnsubscribeWithResponse(
		context.TODO(),
		zone.Spec.FederationContextId,
		zoneId.String(),
	)
	if err != nil {
		log.Error(err, ">>> [AZ][REST] Error DELETING", "name", zone.Name, "namespace", zone.Namespace)
		return err
	}

	statusCode := res.StatusCode()

	switch {
	case statusCode == 200:
		log.Info(">>> [AZ][REST] 200 (DELETE) - Zone deregistered successfully", "name", zone.Name, "namespace", zone.Namespace)
	case statusCode == 400:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON400)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 401:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON401)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 404:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON404)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 409:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON409)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 422:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON422)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 500:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON500)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 503:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON503)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 520:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON520)
		zone.Status.State = v1beta1.ZoneStateFailed
	default:
		log.Info(">>> [AZ][REST] UNEXPECTED STATUS during zone DELETION", "name", zone.Name, "namespace", zone.Namespace, "status", statusCode)
		zone.Status.State = v1beta1.ZoneStateNotAvailable
	}
	return nil
}

// Update (Callback)
func (r *ZoneReconciler) UpdateZoneStatus(ctx context.Context, zone *v1beta1.AvailabilityZone, fed *v1beta1.Federation) error {
	log := ctrl.Log
	// Check if callback is configured
	if zone.Spec.ZoneNotifLink == "" {
		log.Info(">>> [AZ][REST] No ZoneNotifLink, skipping CALLBACK", "name", zone.Name, "namespace", zone.Namespace)
		return nil
	}
	zoneId, err := uuid.Parse("fed-" + uu.V5(zone.Spec.FederationContextId+zone.Spec.ZoneId))
	if err != nil {
		return err
	}
	log.Info(">>> [AZ][REST] Sending CALLBACK to Federation Partner", "name", zone.Name, "namespace", zone.Namespace, "callbackURL", zone.Spec.ZoneNotifLink)
	callbackBody := opgmodels.AvailZoneNotifLinkJSONRequestBody{
		ZoneId:              zoneId.String(),
		FederationContextId: &zone.Spec.FederationContextId,
		// Other fields can be added here as needed, depending on the requirements of the callback.
	}
	// Get callback client (pointing to Guest's callback URL via Federation.spec.partner.statusLink)
	res, err := r.GetOPGClient(
		fed.Status.FederationContextId,
		zone.Spec.ZoneNotifLink,
		"host",
	).AvailZoneNotifLinkWithResponse(
		context.TODO(),
		zone.Spec.FederationContextId,
		callbackBody)
	if err != nil {
		log.Error(err, ">>> [AZ][REST] Error CALLBACK", "name", zone.Name, "namespace", zone.Namespace)
		return err
	}
	statusCode := res.StatusCode()
	switch {
	case statusCode == 200:
		log.Info(">>> [AZ][REST] 200 (Callback) - Zone info notification acknowledged", "status", statusCode)
	case statusCode == 400:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON400)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 401:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON401)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 404:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON404)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 409:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON409)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 422:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON422)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 500:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON500)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 503:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON503)
		zone.Status.State = v1beta1.ZoneStateFailed
	case statusCode == 520:
		handleFileProblemDetails(log, statusCode, res.ApplicationproblemJSON520)
		zone.Status.State = v1beta1.ZoneStateFailed
	default:
		log.Info(">>> [AZ][REST] CALLBACK UNEXPECTED STATUS", "name", zone.Name, "namespace", zone.Namespace, "status", statusCode)
		zone.Status.State = v1beta1.ZoneStateNotAvailable
	}
	return nil
}
