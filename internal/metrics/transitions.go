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

	toolscache "k8s.io/client-go/tools/cache"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlmetrics "sigs.k8s.io/controller-runtime/pkg/metrics"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
)

func Setup(mgr ctrl.Manager) error {
	if err := ctrlmetrics.Registry.Register(&inventoryCollector{reader: mgr.GetCache()}); err != nil {
		return err
	}
	return mgr.Add(&transitionObserver{cache: mgr.GetCache()})
}

type transitionObserver struct {
	cache cache.Cache
}

func (o *transitionObserver) NeedLeaderElection() bool {
	return true
}

func (o *transitionObserver) Start(ctx context.Context) error {
	objs := []client.Object{
		&v1beta1.Federation{},
		&v1beta1.AvailabilityZone{},
		&v1beta1.Image{},
		&v1beta1.Artefact{},
		&v1beta1.ApplicationOnboarding{},
		&v1beta1.ApplicationDeployment{},
	}
	for _, obj := range objs {
		informer, err := o.cache.GetInformer(ctx, obj)
		if err != nil {
			return err
		}
		if _, err := informer.AddEventHandler(toolscache.ResourceEventHandlerFuncs{
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldO, okOld := oldObj.(client.Object)
				newO, okNew := newObj.(client.Object)
				if !okOld || !okNew {
					return
				}
				recordTransition(oldO, newO, func(info resourceInfo) string {
					techs, err := listTechnologies(ctx, o.cache, info.namespace)
					if err != nil {
						return labelUnknown
					}
					return techs.technologyOf(info)
				}, time.Now())
			},
		}); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}

func recordTransition(oldObj, newObj client.Object, technologyOf func(resourceInfo) string, now time.Time) {
	prev, ok := describe(oldObj)
	if !ok {
		return
	}
	cur, _ := describe(newObj)
	relation := labelOr(cur.relation, labelUnknown)

	oldFed, isFed := oldObj.(*v1beta1.Federation)
	if isFed {
		newFed := newObj.(*v1beta1.Federation)
		oldExpiry := oldFed.Status.FederationExpiryDate
		if !oldExpiry.IsZero() && newFed.Status.FederationExpiryDate.After(oldExpiry.Time) {
			FederationRenewals.WithLabelValues(relation).Inc()
		}
	}

	if prev.state == cur.state {
		return
	}
	technology := technologyOf(cur)

	if isFed && cur.state == string(v1beta1.FederationStateLocked) {
		FederationLocks.WithLabelValues(relation).Inc()
	}
	if isFailedState(cur.state) {
		ResourceFailures.WithLabelValues(cur.kind, relation, technology).Inc()
	}
	renewed := isFed && oldFed.Annotations[v1beta1.FederationRenewalAnnotation] != ""
	if cur.state == readyStates[cur.kind] && isInitialState(cur.kind, prev.state) && !renewed {
		TimeToReady.WithLabelValues(cur.kind, relation, technology).
			Observe(now.Sub(newObj.GetCreationTimestamp().Time).Seconds())
	}
}
