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

package httpmetrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
)

func TestMiddleware(t *testing.T) {
	requests.Reset()

	e := echo.New()
	e.Use(Middleware())
	e.GET("/:federationContextId/zones", func(c echo.Context) error {
		return c.NoContent(http.StatusOK)
	})
	e.POST("/:federationContextId/zones", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusConflict)
	})
	e.DELETE("/:federationContextId/zones", func(c echo.Context) error {
		return errors.New("boom")
	})

	for _, method := range []string{http.MethodGet, http.MethodGet, http.MethodPost, http.MethodDelete} {
		e.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/ctx-"+method+"/zones", nil))
	}

	route := "/:federationContextId/zones"
	assert.Equal(t, 2.0, testutil.ToFloat64(requests.WithLabelValues(http.MethodGet, route, "200")))
	assert.Equal(t, 1.0, testutil.ToFloat64(requests.WithLabelValues(http.MethodPost, route, "409")))
	assert.Equal(t, 1.0, testutil.ToFloat64(requests.WithLabelValues(http.MethodDelete, route, "500")))
	assert.Equal(t, 3, testutil.CollectAndCount(requests))
	assert.Equal(t, 0.0, testutil.ToFloat64(inFlight))
}
