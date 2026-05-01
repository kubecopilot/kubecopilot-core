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

package webhook

import (
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	webhookRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "kubecopilot",
			Subsystem: "webhook",
			Name:      "requests_total",
			Help:      "Total number of requests received by the operator webhook server.",
		},
		[]string{"handler", "status"},
	)

	webhookDurationSeconds = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "kubecopilot",
			Subsystem: "webhook",
			Name:      "duration_seconds",
			Help:      "Duration of operator webhook handler execution in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"handler"},
	)
)

func init() {
	metrics.Registry.MustRegister(webhookRequestsTotal, webhookDurationSeconds)
}
