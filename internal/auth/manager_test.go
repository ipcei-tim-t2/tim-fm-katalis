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

package auth

import (
	"context"
	"testing"
	"time"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type mockOidcClient struct {
	callCount int
	tokenResp *TokenResponse
	err       error
}

func (m *mockOidcClient) RequestToken(ctx context.Context, opts TokenRequestOptions) (*TokenResponse, error) {
	m.callCount++
	if m.err != nil {
		return nil, m.err
	}
	resp := *m.tokenResp
	resp.IssuedAt = time.Now()
	resp.Expiry = resp.IssuedAt.Add(time.Duration(resp.ExpiresIn) * time.Second)
	return &resp, nil
}

func TestTokenManager(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, v1beta1.AddToScheme(scheme))

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"client-secret": []byte("valid-secret"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(secret).
		Build()

	fed := &v1beta1.Federation{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "fed-test",
			Namespace: "test-ns",
		},
		Spec: v1beta1.FederationSpec{
			FederationData: &v1beta1.FederationData{
				K8sOptions: &v1beta1.K8sOptions{
					Namespace: "host-ns",
					Auth: &v1beta1.OidcAuthOptions{
						TokenUrl: "http://idp.local/token",
						ClientId: "client-1",
						ClientSecretRef: corev1.SecretKeySelector{
							LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
							Key:                  "client-secret",
						},
						Audience: "aud-host",
					},
				},
			},
		},
	}

	ctx := context.Background()

	t.Run("Fetches token and caches it on first call", func(t *testing.T) {
		mockClient := &mockOidcClient{
			tokenResp: &TokenResponse{
				AccessToken: "token-1",
				TokenType:   "Bearer",
				ExpiresIn:   300,
			},
		}
		tm := NewTokenManager(mockClient)

		// Call 1: Should call mockClient
		resp1, err := tm.GetToken(ctx, fakeClient, fed)
		require.NoError(t, err)
		assert.Equal(t, "token-1", resp1.AccessToken)
		assert.Equal(t, 1, mockClient.callCount)

		// Call 2: Should use cached token, no new request
		resp2, err := tm.GetToken(ctx, fakeClient, fed)
		require.NoError(t, err)
		assert.Equal(t, "token-1", resp2.AccessToken)
		assert.Equal(t, 1, mockClient.callCount)
	})

	t.Run("Invalidate clears cache forcing new request", func(t *testing.T) {
		mockClient := &mockOidcClient{
			tokenResp: &TokenResponse{
				AccessToken: "token-new",
				TokenType:   "Bearer",
				ExpiresIn:   300,
			},
		}
		tm := NewTokenManager(mockClient)

		_, err := tm.GetToken(ctx, fakeClient, fed)
		require.NoError(t, err)
		assert.Equal(t, 1, mockClient.callCount)

		tm.Invalidate("test-ns/fed-test")

		_, err = tm.GetToken(ctx, fakeClient, fed)
		require.NoError(t, err)
		assert.Equal(t, 2, mockClient.callCount)
	})
}
