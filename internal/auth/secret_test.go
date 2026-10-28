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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestResolveSecretKey(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"client-secret": []byte("my-super-secret-password"),
		},
		StringData: map[string]string{
			"string-key": "my-string-value",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingSecret).
		Build()

	ctx := context.Background()

	t.Run("Successfully resolves key from secret.Data", func(t *testing.T) {
		val, err := ResolveSecretKey(ctx, fakeClient, "test-ns", corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
			Key:                  "client-secret",
		})
		require.NoError(t, err)
		assert.Equal(t, "my-super-secret-password", val)
	})

	t.Run("Successfully resolves key from secret.StringData", func(t *testing.T) {
		val, err := ResolveSecretKey(ctx, fakeClient, "test-ns", corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
			Key:                  "string-key",
		})
		require.NoError(t, err)
		assert.Equal(t, "my-string-value", val)
	})

	t.Run("Fails when secret does not exist", func(t *testing.T) {
		_, err := ResolveSecretKey(ctx, fakeClient, "test-ns", corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "non-existent-secret"},
			Key:                  "client-secret",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found")
	})

	t.Run("Fails when key does not exist inside secret", func(t *testing.T) {
		_, err := ResolveSecretKey(ctx, fakeClient, "test-ns", corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
			Key:                  "missing-key",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not found in secret")
	})

	t.Run("Fails when secret name is empty", func(t *testing.T) {
		_, err := ResolveSecretKey(ctx, fakeClient, "test-ns", corev1.SecretKeySelector{
			Key: "some-key",
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "name cannot be empty")
	})

	t.Run("Fails when secret key is empty", func(t *testing.T) {
		_, err := ResolveSecretKey(ctx, fakeClient, "test-ns", corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "test-secret"},
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "key cannot be empty")
	})
}

func TestResolveOptionalSecretBytes(t *testing.T) {
	scheme := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(scheme))

	existingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ca-secret",
			Namespace: "test-ns",
		},
		Data: map[string][]byte{
			"ca.crt": []byte("CERTIFICATE-DATA"),
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(existingSecret).
		Build()

	ctx := context.Background()

	t.Run("Returns nil when selector is nil", func(t *testing.T) {
		bytes, err := ResolveOptionalSecretBytes(ctx, fakeClient, "test-ns", nil)
		require.NoError(t, err)
		assert.Nil(t, bytes)
	})

	t.Run("Returns bytes when selector is present and valid", func(t *testing.T) {
		sel := &corev1.SecretKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: "ca-secret"},
			Key:                  "ca.crt",
		}
		bytes, err := ResolveOptionalSecretBytes(ctx, fakeClient, "test-ns", sel)
		require.NoError(t, err)
		assert.Equal(t, []byte("CERTIFICATE-DATA"), bytes)
	})
}
