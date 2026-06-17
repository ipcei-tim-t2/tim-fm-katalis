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

package k8sPolicy

import (
	"context"
	"fmt"
	"strings"

	"github.com/neonephos-katalis/opg-ewbi-operator/api/operator/v1beta1"
	"github.com/neonephos-katalis/opg-ewbi-operator/internal/opg"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingadmissionpolicies,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=admissionregistration.k8s.io,resources=validatingadmissionpolicybindings,verbs=get;list;watch;create;update;patch;delete

// FederationReconciler reconciles a Federation object
type FederationReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	opg.OPGClientsMapInterface
}

func (r *FederationReconciler) FederationContextIdPolicy(ctx context.Context, role string, policyName string) error {
	log := log.FromContext(ctx)
	log.Info(">>> [Federation] Updating Federation policy")

	listFed := &v1beta1.FederationList{}
	listOpts := []client.ListOption{
		client.MatchingLabels{
			"opg.ewbi.nby.one/federation-relation": role,
		},
	}
	// Fetch the Federation instance
	if err := r.Client.List(ctx, listFed, listOpts...); err != nil {
		log.Error(err, ">>> [Federation] Failed to list Federations")
		return err
	}
	var federationContextIds []string
	for _, fed := range listFed.Items {
		if fed.Status.FederationContextId != "" {
			federationContextIds = append(federationContextIds, fed.Status.FederationContextId)
		}
	}
	celList := "[]"
	if len(federationContextIds) > 0 {
		var celListItems []string
		for _, id := range federationContextIds {
			celListItems = append(celListItems, fmt.Sprintf(`"%s"`, id))
		}
		celList = fmt.Sprintf("[%s]", strings.Join(celListItems, ","))
	}
	celExpression := fmt.Sprintf(
		"(request.operation == 'DELETE' ? "+
			"(has(oldObject.metadata) && has(oldObject.metadata.labels) && "+
			"('opg.ewbi.nby.one/federation-relation' in oldObject.metadata.labels && oldObject.metadata.labels['opg.ewbi.nby.one/federation-relation'] == '%s' ? "+
			"('opg.ewbi.nby.one/federation-context-id' in oldObject.metadata.labels && oldObject.metadata.labels['opg.ewbi.nby.one/federation-context-id'] in %s) : true)) : "+
			"(has(object.metadata) && has(object.metadata.labels) && "+
			"('opg.ewbi.nby.one/federation-relation' in object.metadata.labels && object.metadata.labels['opg.ewbi.nby.one/federation-relation'] == '%s' ? "+
			"('opg.ewbi.nby.one/federation-context-id' in object.metadata.labels && object.metadata.labels['opg.ewbi.nby.one/federation-context-id'] in %s) : true)))",
		role, celList, role, celList,
	)
	policy := &admissionregistrationv1.ValidatingAdmissionPolicy{
		ObjectMeta: metav1.ObjectMeta{Name: policyName},
	}

	// Create or Update
	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, policy, func() error {
		policy.Spec = admissionregistrationv1.ValidatingAdmissionPolicySpec{
			FailurePolicy: ptr.To(admissionregistrationv1.Fail),
			MatchConstraints: &admissionregistrationv1.MatchResources{
				ResourceRules: []admissionregistrationv1.NamedRuleWithOperations{
					{
						RuleWithOperations: admissionregistrationv1.RuleWithOperations{
							Operations: []admissionregistrationv1.OperationType{
								admissionregistrationv1.Create,
								admissionregistrationv1.Update,
								admissionregistrationv1.Delete,
							},
							Rule: admissionregistrationv1.Rule{
								APIGroups:   []string{"opg.ewbi.nby.one"},
								APIVersions: []string{"v1beta1"},
								Resources:   []string{"files", "artefacts", "applications", "applicationinstances"},
							},
						},
					},
				},
			},
			Validations: []admissionregistrationv1.Validation{
				{
					Expression: celExpression,
					Message:    fmt.Sprintf("Not federationContextId found in the list of federationContextIds accepted"),
				},
			},
		}
		return nil
	})

	if err != nil {
		return err
	}

	bindingName := policyName + "-binding"
	binding := &admissionregistrationv1.ValidatingAdmissionPolicyBinding{
		ObjectMeta: metav1.ObjectMeta{Name: bindingName},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, binding, func() error {
		binding.Spec = admissionregistrationv1.ValidatingAdmissionPolicyBindingSpec{
			PolicyName: policyName,
			ValidationActions: []admissionregistrationv1.ValidationAction{
				admissionregistrationv1.Deny,
			},
			MatchResources: &admissionregistrationv1.MatchResources{}, // Si applica a tutti i namespace
		}
		return nil
	})

	if err != nil {
		log.Error(err, "Impossibile creare/aggiornare il ValidatingAdmissionPolicyBinding")
		return err
	}

	log.Info(">>> [Federation] Policy & Binding updated successfully", "validIDs", federationContextIds)
	return nil
}
