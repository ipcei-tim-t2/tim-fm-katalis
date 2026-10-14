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
	"strconv"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	requests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "http_requests_total",
		Help: "HTTP requests handled by the federation API, by method, route and status code.",
	}, []string{"method", "route", "code"})

	duration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "http_request_duration_seconds",
		Help:    "Duration of the HTTP requests handled by the federation API.",
		Buckets: prometheus.DefBuckets,
	}, []string{"method", "route"})

	inFlight = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "HTTP requests currently being handled by the federation API.",
	})
)

func init() {
	prometheus.MustRegister(requests, duration, inFlight)
}

func Middleware() echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			start := time.Now()
			inFlight.Inc()
			defer inFlight.Dec()

			err := next(c)

			route := c.Path()
			if route == "" {
				route = "unmatched"
			}
			method := c.Request().Method
			duration.WithLabelValues(method, route).Observe(time.Since(start).Seconds())
			requests.WithLabelValues(method, route, strconv.Itoa(statusCode(c, err))).Inc()
			return err
		}
	}
}

func statusCode(c echo.Context, err error) int {
	if err == nil || c.Response().Committed {
		return c.Response().Status
	}
	var httpErr *echo.HTTPError
	if errors.As(err, &httpErr) {
		return httpErr.Code
	}
	return http.StatusInternalServerError
}

func Handler() http.Handler {
	return promhttp.Handler()
}
