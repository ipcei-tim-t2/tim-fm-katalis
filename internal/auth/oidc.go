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
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// DefaultRefreshFactor is the default percentage of token TTL at which the token should be proactively refreshed (80%).
	DefaultRefreshFactor = 0.8
	// DefaultHTTPTimeout is the default timeout for OIDC token HTTP requests.
	DefaultHTTPTimeout = 15 * time.Second
)

// TokenRequestOptions specifies the parameters needed to execute a Client Credentials Grant.
type TokenRequestOptions struct {
	TokenURL     string
	ClientID     string
	ClientSecret string
	Audience     string
	Scopes       []string
}

// TokenResponse represents the parsed OAuth2/OIDC token response from the Identity Provider.
type TokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresIn   int64     `json:"expires_in"` // in seconds
	Scope       string    `json:"scope,omitempty"`
	IssuedAt    time.Time `json:"-"`
	Expiry      time.Time `json:"-"`
}

// OidcErrorResponse represents the OAuth2 error payload returned by Keycloak or standard IdPs.
type OidcErrorResponse struct {
	Error            string `json:"error"`
	ErrorDescription string `json:"error_description,omitempty"`
}

// OidcClient is the interface for requesting M2M Access Tokens from an Identity Provider.
type OidcClient interface {
	RequestToken(ctx context.Context, opts TokenRequestOptions) (*TokenResponse, error)
}

// HTTPTokenClient implements OidcClient using standard HTTP requests.
type HTTPTokenClient struct {
	httpClient *http.Client
}

// NewHTTPTokenClient creates a new HTTPTokenClient with the given HTTP client or a default one.
func NewHTTPTokenClient(httpClient *http.Client) *HTTPTokenClient {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: DefaultHTTPTimeout,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					InsecureSkipVerify: true,
				},
			},
		}
	}
	return &HTTPTokenClient{
		httpClient: httpClient,
	}
}

// RequestToken executes a POST request against the IdP token endpoint using the Client Credentials grant.
func (c *HTTPTokenClient) RequestToken(ctx context.Context, opts TokenRequestOptions) (*TokenResponse, error) {
	if opts.TokenURL == "" {
		return nil, fmt.Errorf("tokenUrl cannot be empty")
	}
	if opts.ClientID == "" {
		return nil, fmt.Errorf("clientId cannot be empty")
	}
	if opts.ClientSecret == "" {
		return nil, fmt.Errorf("clientSecret cannot be empty")
	}

	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", opts.ClientID)
	form.Set("client_secret", opts.ClientSecret)

	if opts.Audience != "" {
		form.Set("audience", opts.Audience)
	}

	if len(opts.Scopes) > 0 {
		form.Set("scope", strings.Join(opts.Scopes, " "))
	} else {
		form.Set("scope", "openid")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	now := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request to %q failed: %w", opts.TokenURL, err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read token response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var oidcErr OidcErrorResponse
		if jsonErr := json.Unmarshal(bodyBytes, &oidcErr); jsonErr == nil && oidcErr.Error != "" {
			return nil, fmt.Errorf("IdP returned HTTP %d: error=%q, description=%q", resp.StatusCode, oidcErr.Error, oidcErr.ErrorDescription)
		}
		return nil, fmt.Errorf("IdP returned HTTP %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var tokenResp TokenResponse
	if err := json.Unmarshal(bodyBytes, &tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response JSON: %w", err)
	}

	if tokenResp.AccessToken == "" {
		return nil, fmt.Errorf("token response did not contain access_token")
	}

	// Default fallback to 5 minutes (300s) if expires_in is missing or zero
	if tokenResp.ExpiresIn <= 0 {
		tokenResp.ExpiresIn = 300
	}

	tokenResp.IssuedAt = now
	tokenResp.Expiry = now.Add(time.Duration(tokenResp.ExpiresIn) * time.Second)

	return &tokenResp, nil
}

// CalculateRefreshDuration computes the time duration until the next token refresh according to the given refresh factor (e.g. 0.8).
// If the calculated refresh point is in the past, it returns 0.
func CalculateRefreshDuration(issuedAt, expiry, now time.Time, refreshFactor float64) time.Duration {
	if refreshFactor <= 0 || refreshFactor >= 1.0 {
		refreshFactor = DefaultRefreshFactor
	}

	totalDuration := expiry.Sub(issuedAt)
	if totalDuration <= 0 {
		return 0
	}

	refreshOffset := time.Duration(float64(totalDuration) * refreshFactor)
	refreshPoint := issuedAt.Add(refreshOffset)

	timeUntilRefresh := refreshPoint.Sub(now)
	if timeUntilRefresh < 0 {
		return 0
	}
	return timeUntilRefresh
}
