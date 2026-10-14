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

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
)

const namespace = "katalis_fm"

const (
	KindFederation            = "federation"
	KindAvailabilityZone      = "availability_zone"
	KindImage                 = "image"
	KindArtefact              = "artefact"
	KindApplicationOnboarding = "application_onboarding"
	KindApplicationDeployment = "application_deployment"
)

const (
	ResultSuccess = "success"
	ResultError   = "error"
)

const (
	labelNone    = "NONE"
	labelUnknown = "UNKNOWN"
)

var log = ctrl.Log.WithName("metrics")

var (
	PartnerOperations = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "partner_operations_total",
		Help:      "Operations performed towards the partner OP, by resource kind, operation, technology and result.",
	}, []string{"kind", "operation", "technology", "result"})

	PartnerOperationDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "partner_operation_duration_seconds",
		Help:      "Duration of the operations performed towards the partner OP.",
		Buckets:   prometheus.ExponentialBuckets(0.01, 2, 12),
	}, []string{"kind", "operation", "technology"})

	FederationRenewals = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "federation_renewals_total",
		Help:      "Federation renewals, counted when the federation expiry date is moved forward.",
	}, []string{"relation"})

	FederationLocks = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "federation_locks_total",
		Help:      "Federations that moved to the LOCKED state.",
	}, []string{"relation"})

	TimeToReady = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: namespace,
		Name:      "time_to_ready_seconds",
		Help:      "Time from resource creation to its first ready state (AVAILABLE, READY or ONBOARDED).",
		Buckets:   []float64{1, 5, 15, 30, 60, 120, 300, 600, 1800, 3600},
	}, []string{"kind", "relation", "technology"})

	ResourceFailures = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "resource_failures_total",
		Help:      "Resources that moved to a FAILED or ERROR state.",
	}, []string{"kind", "relation", "technology"})

	RemoteWatchersActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: namespace,
		Name:      "remote_watchers_active",
		Help:      "Remote resource watchers currently running (K8S technology, guest side).",
	}, []string{"resource"})

	RemoteWatcherRestarts = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "remote_watcher_restarts_total",
		Help:      "Remote resource watchers restarted after being stopped (e.g. federation back to AVAILABLE).",
	}, []string{"resource"})

	RemoteWatcherErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "remote_watcher_errors_total",
		Help:      "List/watch errors of the remote resource watchers.",
	}, []string{"resource"})

	DeletionBlocked = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "deletion_blocked_total",
		Help:      "Deletions postponed because dependent resources still reference the resource.",
	}, []string{"kind"})

	TransientErrors = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: namespace,
		Name:      "transient_errors_total",
		Help:      "Reconcile errors classified as transient and retried without changing the resource state.",
	}, []string{"kind"})
)

func init() {
	ctrlmetrics.Registry.MustRegister(
		PartnerOperations,
		PartnerOperationDuration,
		FederationRenewals,
		FederationLocks,
		TimeToReady,
		ResourceFailures,
		RemoteWatchersActive,
		RemoteWatcherRestarts,
		RemoteWatcherErrors,
		DeletionBlocked,
		TransientErrors,
	)
}

func ObservePartnerOperation(kind, operation string, isRest bool, op func() error) error {
	technology := string(v1beta1.FederationTechnologyK8s)
	if isRest {
		technology = string(v1beta1.FederationTechnologyRest)
	}
	start := time.Now()
	err := op()
	PartnerOperationDuration.WithLabelValues(kind, operation, technology).Observe(time.Since(start).Seconds())
	result := ResultSuccess
	if err != nil {
		result = ResultError
	}
	PartnerOperations.WithLabelValues(kind, operation, technology, result).Inc()
	return err
}

func labelOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
