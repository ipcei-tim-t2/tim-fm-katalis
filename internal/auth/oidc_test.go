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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequestToken(t *testing.T) {
	t.Run("Successfully requests and parses token", func(t *testing.T) {
		var receivedGrantType, receivedClientID, receivedClientSecret, receivedAudience, receivedScope string

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, http.MethodPost, r.Method)
			assert.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))

			err := r.ParseForm()
			assert.NoError(t, err)

			receivedGrantType = r.FormValue("grant_type")
			receivedClientID = r.FormValue("client_id")
			receivedClientSecret = r.FormValue("client_secret")
			receivedAudience = r.FormValue("audience")
			receivedScope = r.FormValue("scope")

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "mock-jwt-bearer-token-12345",
				"token_type":   "Bearer",
				"expires_in":   300,
				"scope":        "openid",
			})
		}))
		defer server.Close()

		client := NewHTTPTokenClient(nil)
		opts := TokenRequestOptions{
			TokenURL:     server.URL,
			ClientID:     "guest-operator-client",
			ClientSecret: "super-secret-from-eso",
			Audience:     "https://host-controlplane:6443",
			Scopes:       []string{"openid", "profile"},
		}

		resp, err := client.RequestToken(context.Background(), opts)
		require.NoError(t, err)
		require.NotNil(t, resp)

		assert.Equal(t, "mock-jwt-bearer-token-12345", resp.AccessToken)
		assert.Equal(t, "Bearer", resp.TokenType)
		assert.Equal(t, int64(300), resp.ExpiresIn)
		assert.False(t, resp.Expiry.IsZero())
		assert.True(t, resp.Expiry.After(time.Now()))

		// Verify sent parameters
		assert.Equal(t, "client_credentials", receivedGrantType)
		assert.Equal(t, "guest-operator-client", receivedClientID)
		assert.Equal(t, "super-secret-from-eso", receivedClientSecret)
		assert.Equal(t, "https://host-controlplane:6443", receivedAudience)
		assert.Equal(t, "openid profile", receivedScope)
	})

	t.Run("Fails on IdP 401 Unauthorized", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":             "unauthorized_client",
				"error_description": "Invalid client secret provided",
			})
		}))
		defer server.Close()

		client := NewHTTPTokenClient(nil)
		opts := TokenRequestOptions{
			TokenURL:     server.URL,
			ClientID:     "guest-operator",
			ClientSecret: "invalid-secret",
		}

		resp, err := client.RequestToken(context.Background(), opts)
		require.Error(t, err)
		assert.Nil(t, resp)
		assert.Contains(t, err.Error(), "unauthorized_client")
		assert.Contains(t, err.Error(), "Invalid client secret provided")
	})

	t.Run("Fails when required options are missing", func(t *testing.T) {
		client := NewHTTPTokenClient(nil)

		_, err := client.RequestToken(context.Background(), TokenRequestOptions{
			ClientID:     "client",
			ClientSecret: "secret",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "tokenUrl cannot be empty")

		_, err = client.RequestToken(context.Background(), TokenRequestOptions{
			TokenURL:     "http://idp.local",
			ClientSecret: "secret",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "clientId cannot be empty")

		_, err = client.RequestToken(context.Background(), TokenRequestOptions{
			TokenURL: "http://idp.local",
			ClientID: "client",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "clientSecret cannot be empty")
	})
}

func TestCalculateRefreshDuration(t *testing.T) {
	issuedAt := time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)
	expiry := issuedAt.Add(300 * time.Second) // 5 minutes TTL = 300s

	t.Run("Calculates 80% refresh duration from issue time", func(t *testing.T) {
		// 80% of 300s is 240s
		now := issuedAt
		duration := CalculateRefreshDuration(issuedAt, expiry, now, 0.8)
		assert.Equal(t, 240*time.Second, duration)
	})

	t.Run("Calculates remaining refresh duration midway through TTL", func(t *testing.T) {
		// If 100s elapsed, remaining to 240s is 140s
		now := issuedAt.Add(100 * time.Second)
		duration := CalculateRefreshDuration(issuedAt, expiry, now, 0.8)
		assert.Equal(t, 140*time.Second, duration)
	})

	t.Run("Returns 0 when current time is past 80% mark", func(t *testing.T) {
		// Past 240s mark (e.g. 250s)
		now := issuedAt.Add(250 * time.Second)
		duration := CalculateRefreshDuration(issuedAt, expiry, now, 0.8)
		assert.Equal(t, time.Duration(0), duration)
	})

	t.Run("Returns 0 when current time is past expiry", func(t *testing.T) {
		now := issuedAt.Add(350 * time.Second)
		duration := CalculateRefreshDuration(issuedAt, expiry, now, 0.8)
		assert.Equal(t, time.Duration(0), duration)
	})
}
