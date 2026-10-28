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
	"fmt"
	"sync"
	"time"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TokenManager manages the in-memory cache and lifecycle of OIDC tokens for Federations.
// Raw JWT tokens are NEVER stored in Kubernetes Status or on disk, preserving Zero-Trust security.
type TokenManager struct {
	client OidcClient
	mu     sync.RWMutex
	tokens map[string]*TokenResponse
}

// DefaultTokenManager is the global token manager instance used across controllers.
var DefaultTokenManager = NewTokenManager(NewHTTPTokenClient(nil))

// NewTokenManager creates a new instance of TokenManager.
func NewTokenManager(client OidcClient) *TokenManager {
	return &TokenManager{
		client: client,
		tokens: make(map[string]*TokenResponse),
	}
}

// GetToken returns a valid OIDC Access Token for the given Federation resource.
// If a cached token is still valid (has not reached its 80% refresh mark), it is returned.
// Otherwise, it resolves the clientSecret from the local Kubernetes Secret and requests a new token from the IdP.
func (m *TokenManager) GetToken(ctx context.Context, localClient client.Client, fed *v1beta1.Federation) (*TokenResponse, error) {
	if fed == nil || fed.Spec.FederationData == nil || fed.Spec.FederationData.K8sOptions == nil || fed.Spec.FederationData.K8sOptions.Auth == nil {
		return nil, fmt.Errorf("federation %s/%s does not have OIDC Auth configured", fed.GetNamespace(), fed.GetName())
	}

	authOpts := fed.Spec.FederationData.K8sOptions.Auth
	cacheKey := fed.GetNamespace() + "/" + fed.GetName()
	now := time.Now()

	// 1. Check if we have a valid cached token that has not reached 80% TTL
	m.mu.RLock()
	cached, exists := m.tokens[cacheKey]
	m.mu.RUnlock()

	if exists && cached != nil {
		remainingUntilRefresh := CalculateRefreshDuration(cached.IssuedAt, cached.Expiry, now, DefaultRefreshFactor)
		if remainingUntilRefresh > 0 && now.Before(cached.Expiry) {
			return cached, nil
		}
	}

	// 2. Fetch a fresh token
	m.mu.Lock()
	defer m.mu.Unlock()

	// Double-check cache in case another goroutine just refreshed it
	cached, exists = m.tokens[cacheKey]
	if exists && cached != nil {
		remainingUntilRefresh := CalculateRefreshDuration(cached.IssuedAt, cached.Expiry, now, DefaultRefreshFactor)
		if remainingUntilRefresh > 0 && now.Before(cached.Expiry) {
			return cached, nil
		}
	}

	// Resolve the clientSecret from the local secret populated by ESO
	clientSecret, err := ResolveSecretKey(ctx, localClient, fed.GetNamespace(), authOpts.ClientSecretRef)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve clientSecretRef for federation %q: %w", cacheKey, err)
	}

	reqOpts := TokenRequestOptions{
		TokenURL:     authOpts.TokenUrl,
		ClientID:     authOpts.ClientId,
		ClientSecret: clientSecret,
		Audience:     authOpts.Audience,
		Scopes:       authOpts.Scopes,
	}

	resp, err := m.client.RequestToken(ctx, reqOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to obtain token from IdP for federation %q: %w", cacheKey, err)
	}

	m.tokens[cacheKey] = resp
	return resp, nil
}

// Invalidate removes any cached token for the given federation key (e.g. upon deletion).
func (m *TokenManager) Invalidate(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.tokens, key)
}
