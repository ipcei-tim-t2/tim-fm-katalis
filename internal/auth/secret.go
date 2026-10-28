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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ResolveSecretKey resolves the string value of a given corev1.SecretKeySelector within the specified namespace.
// It retrieves the Kubernetes Secret (typically populated by External Secrets Operator) and extracts the key.
func ResolveSecretKey(ctx context.Context, c client.Client, namespace string, sel corev1.SecretKeySelector) (string, error) {
	if sel.Name == "" {
		return "", fmt.Errorf("secretKeyRef name cannot be empty")
	}
	if sel.Key == "" {
		return "", fmt.Errorf("secretKeyRef key cannot be empty for secret %q", sel.Name)
	}

	var secret corev1.Secret
	namespacedName := types.NamespacedName{
		Namespace: namespace,
		Name:      sel.Name,
	}

	if err := c.Get(ctx, namespacedName, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return "", fmt.Errorf("secret %q not found in namespace %q: %w", sel.Name, namespace, err)
		}
		return "", fmt.Errorf("failed to get secret %q in namespace %q: %w", sel.Name, namespace, err)
	}

	// First check binary Data
	if val, exists := secret.Data[sel.Key]; exists {
		return string(val), nil
	}

	// Fallback to StringData if present
	if val, exists := secret.StringData[sel.Key]; exists {
		return val, nil
	}

	return "", fmt.Errorf("key %q not found in secret %q/%q", sel.Key, namespace, sel.Name)
}

// ResolveOptionalSecretBytes retrieves the byte content of a secret key, returning nil if the selector is nil.
func ResolveOptionalSecretBytes(ctx context.Context, c client.Client, namespace string, sel *corev1.SecretKeySelector) ([]byte, error) {
	if sel == nil {
		return nil, nil
	}
	val, err := ResolveSecretKey(ctx, c, namespace, *sel)
	if err != nil {
		return nil, err
	}
	return []byte(val), nil
}
