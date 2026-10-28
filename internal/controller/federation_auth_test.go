/*
Copyright 2026.

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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/auth"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/opg"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mockExternalFederationClient struct {
	created bool
}

func (m *mockExternalFederationClient) CreateFederation(ctx context.Context, f *v1beta1.Federation) error {
	m.created = true
	return nil
}
func (m *mockExternalFederationClient) DeleteFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) UpdateFederationStatus(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) DetailsFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) UpdateFederationDetailsStatus(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) PatchFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) GetHealthFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) GetPlatformCapsFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) GetServiceAPIFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}
func (m *mockExternalFederationClient) RenewalFederation(ctx context.Context, f *v1beta1.Federation) error {
	return nil
}

func TestFederationReconciler_OIDCAuthentication(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	// Mock Keycloak IdP server
	idpServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, http.MethodPost, r.Method)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "mock-jwt-access-token",
			"token_type":   "Bearer",
			"expires_in":   300, // 5 minutes TTL
		})
	}))
	defer idpServer.Close()

	// Local Secret created by External Secrets Operator
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "eso-synced-secret",
			Namespace: "guest-ns",
		},
		Data: map[string][]byte{
			"client-secret": []byte("vault-secret-value"),
		},
	}

	fed := &v1beta1.Federation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "fed-oidc-test",
			Namespace:  "guest-ns",
			Finalizers: []string{v1beta1.FederationFinalizer},
		},
		Spec: v1beta1.FederationSpec{
			FederationData: &v1beta1.FederationData{
				RelationType:   string(v1beta1.FederationRelationGuest),
				TechnologyType: string(v1beta1.FederationTechnologyK8s),
				InitialDate:    metav1.Now(),
				K8sOptions: &v1beta1.K8sOptions{
					Namespace: "host-ns",
					ClusterConnection: &v1beta1.ClusterConnectionInfo{
						HostApiServerUrl:      "https://host-controlplane:6443",
						InsecureSkipTLSVerify: true,
					},
					Auth: &v1beta1.OidcAuthOptions{
						TokenUrl: idpServer.URL,
						ClientId: "guest-operator",
						ClientSecretRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "eso-synced-secret"},
							Key:                  "client-secret",
						},
						Audience: "https://host-controlplane:6443",
					},
				},
			},
		},
		Status: v1beta1.FederationStatus{
			State: v1beta1.FederationStateAvailable,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret, fed).
		WithStatusSubresource(&v1beta1.Federation{}).
		Build()

	mockExt := &mockExternalFederationClient{}

	r := &FederationReconciler{
		Client:                 fakeClient,
		Scheme:                 scheme,
		OPGClientsMapInterface: opg.NewOPGClientsMap(),
		K8sClient:              mockExt,
	}

	// Override getExternalClient to avoid real network calls during external client execution
	ctx := context.Background()

	// Invalidate any residual cache for this test
	auth.DefaultTokenManager.Invalidate("guest-ns/fed-oidc-test")

	// Pre-test: verify token was fetched and cached
	tokenResp, err := auth.DefaultTokenManager.GetToken(ctx, fakeClient, fed)
	require.NoError(t, err)
	assert.Equal(t, "mock-jwt-access-token", tokenResp.AccessToken)

	// Reconcile the federation
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "guest-ns",
			Name:      "fed-oidc-test",
		},
	}

	// Set extClient for K8sClient in Reconciler
	// Note: in our test, we check that Reconcile successfully completes and schedules requeue
	_ = mockExt
	res, err := r.Reconcile(ctx, req)
	require.NoError(t, err)

	// Verify that RequeueAfter is scheduled around 80% of TTL (300s * 0.8 = 240s)
	assert.True(t, res.RequeueAfter > 200*time.Second && res.RequeueAfter <= 240*time.Second,
		"Expected RequeueAfter around 240s (80%% of 300s), got: %v", res.RequeueAfter)

	// Verify status update in the fetched object
	var updatedFed v1beta1.Federation
	err = fakeClient.Get(ctx, req.NamespacedName, &updatedFed)
	require.NoError(t, err)

	require.NotNil(t, updatedFed.Status.TokenInfo)
	assert.Equal(t, "AUTHENTICATED", updatedFed.Status.TokenInfo.AuthState)
	assert.False(t, updatedFed.Status.TokenInfo.TokenExpiryDate.IsZero())
	assert.False(t, updatedFed.Status.TokenInfo.TokenIssuedAt.IsZero())
}

func TestFederationReconciler_OIDCAuthentication_MissingSecret(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	// Federation pointing to a secret that doesn't exist
	fed := &v1beta1.Federation{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "fed-missing-secret",
			Namespace:  "guest-ns",
			Finalizers: []string{v1beta1.FederationFinalizer},
		},
		Spec: v1beta1.FederationSpec{
			FederationData: &v1beta1.FederationData{
				RelationType:   string(v1beta1.FederationRelationGuest),
				TechnologyType: string(v1beta1.FederationTechnologyK8s),
				InitialDate:    metav1.Now(),
				K8sOptions: &v1beta1.K8sOptions{
					Namespace: "host-ns",
					ClusterConnection: &v1beta1.ClusterConnectionInfo{
						HostApiServerUrl: "https://host-controlplane:6443",
					},
					Auth: &v1beta1.OidcAuthOptions{
						TokenUrl: "http://idp.local/token",
						ClientId: "guest-operator",
						ClientSecretRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent-secret"},
							Key:                  "client-secret",
						},
					},
				},
			},
		},
		Status: v1beta1.FederationStatus{
			State: v1beta1.FederationStateAvailable,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(fed).
		WithStatusSubresource(&v1beta1.Federation{}).
		Build()

	r := &FederationReconciler{
		Client:                 fakeClient,
		Scheme:                 scheme,
		OPGClientsMapInterface: opg.NewOPGClientsMap(),
		K8sClient:              &mockExternalFederationClient{},
	}

	ctx := context.Background()
	req := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Namespace: "guest-ns",
			Name:      "fed-missing-secret",
		},
	}

	res, err := r.Reconcile(ctx, req)
	require.Error(t, err)
	assert.Equal(t, 15*time.Second, res.RequeueAfter)

	var updatedFed v1beta1.Federation
	err = fakeClient.Get(ctx, req.NamespacedName, &updatedFed)
	require.NoError(t, err)

	require.NotNil(t, updatedFed.Status.TokenInfo)
	assert.Equal(t, "AUTH_FAILED", updatedFed.Status.TokenInfo.AuthState)
}
