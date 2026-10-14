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
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
)

const testNamespace = "test"

func federation(name, ctxId, relation, technology string, state v1beta1.FederationState) *v1beta1.Federation {
	fed := &v1beta1.Federation{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	fed.Spec.FederationData = &v1beta1.FederationData{RelationType: relation, TechnologyType: technology}
	fed.Status.FederationContextId = ctxId
	fed.Status.State = state
	return fed
}

func image(name, ctxId, relation string, state v1beta1.ImageState) *v1beta1.Image {
	img := &v1beta1.Image{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: testNamespace}}
	img.Spec.FederationContextId = ctxId
	img.Spec.RelationType = relation
	img.Status.State = state
	return img
}

func fixedTechnology(technology string) func(resourceInfo) string {
	return func(resourceInfo) string { return technology }
}

func TestObservePartnerOperation(t *testing.T) {
	PartnerOperations.Reset()
	PartnerOperationDuration.Reset()

	require.NoError(t, ObservePartnerOperation(KindImage, "create", true, func() error { return nil }))
	err := ObservePartnerOperation(KindImage, "create", true, func() error { return errors.New("boom") })
	assert.EqualError(t, err, "boom")

	assert.Equal(t, 1.0, testutil.ToFloat64(PartnerOperations.WithLabelValues(KindImage, "create", "REST", ResultSuccess)))
	assert.Equal(t, 1.0, testutil.ToFloat64(PartnerOperations.WithLabelValues(KindImage, "create", "REST", ResultError)))
	assert.Equal(t, 1, testutil.CollectAndCount(PartnerOperationDuration))
}

func TestRecordTransitionTimeToReady(t *testing.T) {
	TimeToReady.Reset()
	created := time.Now().Add(-30 * time.Second)

	oldImg := image("img", "ctx", "GUEST", v1beta1.ImageStatePending)
	oldImg.CreationTimestamp = metav1.NewTime(created)
	newImg := oldImg.DeepCopy()
	newImg.Status.State = v1beta1.ImageStateReady

	recordTransition(oldImg, newImg, fixedTechnology("K8S"), created.Add(30*time.Second))

	assert.Equal(t, 1, testutil.CollectAndCount(TimeToReady))
	expected := `
# HELP katalis_fm_time_to_ready_seconds Time from resource creation to its first ready state (AVAILABLE, READY or ONBOARDED).
# TYPE katalis_fm_time_to_ready_seconds histogram
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="1"} 0
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="5"} 0
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="15"} 0
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="30"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="60"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="120"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="300"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="600"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="1800"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="3600"} 1
katalis_fm_time_to_ready_seconds_bucket{kind="image",relation="GUEST",technology="K8S",le="+Inf"} 1
katalis_fm_time_to_ready_seconds_sum{kind="image",relation="GUEST",technology="K8S"} 30
katalis_fm_time_to_ready_seconds_count{kind="image",relation="GUEST",technology="K8S"} 1
`
	assert.NoError(t, testutil.CollectAndCompare(TimeToReady, strings.NewReader(expected)))
}

func TestRecordTransitionSkipsRenewedFederation(t *testing.T) {
	TimeToReady.Reset()

	oldFed := federation("fed", "ctx", "GUEST", "K8S", v1beta1.FederationStateNotAvailable)
	oldFed.Annotations = map[string]string{v1beta1.FederationRenewalAnnotation: "not-required"}
	newFed := oldFed.DeepCopy()
	newFed.Status.State = v1beta1.FederationStateAvailable

	recordTransition(oldFed, newFed, fixedTechnology("K8S"), time.Now())

	assert.Equal(t, 0, testutil.CollectAndCount(TimeToReady))
}

func TestRecordTransitionFederationLifecycle(t *testing.T) {
	FederationLocks.Reset()
	FederationRenewals.Reset()
	ResourceFailures.Reset()
	expiry := time.Now()

	oldFed := federation("fed", "ctx", "HOST", "REST", v1beta1.FederationStateAvailable)
	oldFed.Status.FederationExpiryDate = metav1.NewTime(expiry)
	renewed := oldFed.DeepCopy()
	renewed.Status.FederationExpiryDate = metav1.NewTime(expiry.Add(24 * time.Hour))
	recordTransition(oldFed, renewed, fixedTechnology("REST"), time.Now())

	locked := renewed.DeepCopy()
	locked.Status.State = v1beta1.FederationStateLocked
	recordTransition(renewed, locked, fixedTechnology("REST"), time.Now())

	failed := locked.DeepCopy()
	failed.Status.State = v1beta1.FederationStateFailed
	recordTransition(locked, failed, fixedTechnology("REST"), time.Now())

	assert.Equal(t, 1.0, testutil.ToFloat64(FederationRenewals.WithLabelValues("HOST")))
	assert.Equal(t, 1.0, testutil.ToFloat64(FederationLocks.WithLabelValues("HOST")))
	assert.Equal(t, 1.0, testutil.ToFloat64(ResourceFailures.WithLabelValues(KindFederation, "HOST", "REST")))
}

func TestInventoryCollector(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, v1beta1.AddToScheme(scheme))

	fed := federation("fed", "ctx", "GUEST", "K8S", v1beta1.FederationStateAvailable)
	fed.Status.FederationExpiryDate = metav1.NewTime(time.Unix(1700000000, 0))
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(
		fed,
		image("img-1", "ctx", "GUEST", v1beta1.ImageStateReady),
		image("img-2", "ctx", "GUEST", v1beta1.ImageStateReady),
		image("img-3", "other", "GUEST", ""),
	).Build()

	registry := prometheus.NewPedanticRegistry()
	require.NoError(t, registry.Register(&inventoryCollector{reader: c}))

	expected := `
# HELP katalis_fm_federation_expiry_timestamp_seconds Federation expiry date as a Unix timestamp.
# TYPE katalis_fm_federation_expiry_timestamp_seconds gauge
katalis_fm_federation_expiry_timestamp_seconds{federation="fed",namespace="test",relation="GUEST"} 1.7e+09
# HELP katalis_fm_resources Federated resources by kind, relation, technology and state.
# TYPE katalis_fm_resources gauge
katalis_fm_resources{kind="federation",relation="GUEST",state="AVAILABLE",technology="K8S"} 1
katalis_fm_resources{kind="image",relation="GUEST",state="NONE",technology="UNKNOWN"} 1
katalis_fm_resources{kind="image",relation="GUEST",state="READY",technology="K8S"} 2
`
	assert.NoError(t, testutil.GatherAndCompare(registry, strings.NewReader(expected)))
}
