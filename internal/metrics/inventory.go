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
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/api/meta"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
)

var (
	resourcesDesc = prometheus.NewDesc(
		namespace+"_resources",
		"Federated resources by kind, relation, technology and state.",
		[]string{"kind", "relation", "technology", "state"}, nil,
	)
	federationExpiryDesc = prometheus.NewDesc(
		namespace+"_federation_expiry_timestamp_seconds",
		"Federation expiry date as a Unix timestamp.",
		[]string{"federation", "namespace", "relation"}, nil,
	)
)

type inventoryCollector struct {
	reader client.Reader
}

func (c *inventoryCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- resourcesDesc
	ch <- federationExpiryDesc
}

func (c *inventoryCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var feds v1beta1.FederationList
	if err := c.reader.List(ctx, &feds); err != nil {
		log.V(1).Info("Skipping inventory metrics", "error", err.Error())
		return
	}
	techs := newTechnologyIndex(feds.Items)

	type key struct{ kind, relation, technology, state string }
	counts := map[key]int{}
	count := func(obj client.Object) {
		info, ok := describe(obj)
		if !ok {
			return
		}
		counts[key{info.kind, labelOr(info.relation, labelUnknown), techs.technologyOf(info), labelOr(info.state, labelNone)}]++
	}

	for i := range feds.Items {
		fed := &feds.Items[i]
		count(fed)
		if !fed.Status.FederationExpiryDate.IsZero() {
			relation, _ := federationLabels(fed)
			ch <- prometheus.MustNewConstMetric(federationExpiryDesc, prometheus.GaugeValue,
				float64(fed.Status.FederationExpiryDate.Unix()),
				fed.Name, fed.Namespace, labelOr(relation, labelUnknown))
		}
	}

	children := []client.ObjectList{
		&v1beta1.AvailabilityZoneList{},
		&v1beta1.ImageList{},
		&v1beta1.ArtefactList{},
		&v1beta1.ApplicationOnboardingList{},
		&v1beta1.ApplicationDeploymentList{},
	}
	for _, list := range children {
		if err := c.reader.List(ctx, list); err != nil {
			log.V(1).Info("Skipping inventory metrics", "error", err.Error())
			continue
		}
		items, err := meta.ExtractList(list)
		if err != nil {
			continue
		}
		for _, item := range items {
			if obj, ok := item.(client.Object); ok {
				count(obj)
			}
		}
	}

	for k, n := range counts {
		ch <- prometheus.MustNewConstMetric(resourcesDesc, prometheus.GaugeValue, float64(n), k.kind, k.relation, k.technology, k.state)
	}
}
