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

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
)

type resourceInfo struct {
	kind                string
	relation            string
	technology          string
	namespace           string
	federationContextId string
	state               string
}

func describe(obj client.Object) (resourceInfo, bool) {
	switch o := obj.(type) {
	case *v1beta1.Federation:
		relation, technology := federationLabels(o)
		return resourceInfo{
			kind:                KindFederation,
			relation:            relation,
			technology:          technology,
			namespace:           o.Namespace,
			federationContextId: o.Status.FederationContextId,
			state:               string(o.Status.State),
		}, true
	case *v1beta1.AvailabilityZone:
		return resourceInfo{KindAvailabilityZone, o.Spec.RelationType, "", o.Namespace, o.Spec.FederationContextId, string(o.Status.State)}, true
	case *v1beta1.Image:
		return resourceInfo{KindImage, o.Spec.RelationType, "", o.Namespace, o.Spec.FederationContextId, string(o.Status.State)}, true
	case *v1beta1.Artefact:
		return resourceInfo{KindArtefact, o.Spec.RelationType, "", o.Namespace, o.Spec.FederationContextId, string(o.Status.State)}, true
	case *v1beta1.ApplicationOnboarding:
		return resourceInfo{KindApplicationOnboarding, o.Spec.RelationType, "", o.Namespace, o.Spec.FederationContextId, string(o.Status.State)}, true
	case *v1beta1.ApplicationDeployment:
		return resourceInfo{KindApplicationDeployment, o.Spec.RelationType, "", o.Namespace, o.Spec.FederationContextId, string(o.Status.AppInstanceInfo.AppInstanceState)}, true
	}
	return resourceInfo{}, false
}

func federationLabels(fed *v1beta1.Federation) (relation, technology string) {
	if fed.Spec.FederationData == nil {
		return "", ""
	}
	return fed.Spec.FederationData.RelationType, fed.Spec.FederationData.TechnologyType
}

var readyStates = map[string]string{
	KindFederation:            string(v1beta1.FederationStateAvailable),
	KindAvailabilityZone:      string(v1beta1.ZoneStateAvailable),
	KindImage:                 string(v1beta1.ImageStateReady),
	KindArtefact:              string(v1beta1.ArtefactStateReady),
	KindApplicationOnboarding: string(v1beta1.ApplicationOnboardingStateOnboarded),
	KindApplicationDeployment: string(v1beta1.ApplicationDeploymentStateReady),
}

var initialStates = map[string]string{
	KindFederation:            string(v1beta1.FederationStateNotAvailable),
	KindAvailabilityZone:      string(v1beta1.ZoneStateNotAvailable),
	KindImage:                 string(v1beta1.ImageStatePending),
	KindArtefact:              string(v1beta1.ArtefactStatePending),
	KindApplicationOnboarding: string(v1beta1.ApplicationOnboardingStatePending),
	KindApplicationDeployment: string(v1beta1.ApplicationDeploymentStatePending),
}

func isInitialState(kind, state string) bool {
	return state == "" || state == initialStates[kind]
}

func isFailedState(state string) bool {
	switch state {
	case string(v1beta1.FederationStateFailed), string(v1beta1.ImageStateError):
		return true
	}
	return false
}

type technologyIndex map[string]string

func newTechnologyIndex(feds []v1beta1.Federation) technologyIndex {
	index := technologyIndex{}
	for i := range feds {
		if feds[i].Status.FederationContextId == "" {
			continue
		}
		_, technology := federationLabels(&feds[i])
		index[feds[i].Namespace+"/"+feds[i].Status.FederationContextId] = technology
	}
	return index
}

func (idx technologyIndex) technologyOf(info resourceInfo) string {
	if info.technology != "" {
		return info.technology
	}
	return labelOr(idx[info.namespace+"/"+info.federationContextId], labelUnknown)
}

func listTechnologies(ctx context.Context, reader client.Reader, namespace string) (technologyIndex, error) {
	var feds v1beta1.FederationList
	if err := reader.List(ctx, &feds, client.InNamespace(namespace)); err != nil {
		return nil, err
	}
	return newTechnologyIndex(feds.Items), nil
}
