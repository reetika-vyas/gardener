// Copyright (c) 2018 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//      http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package bootstraptoken

import (
	"context"
	"regexp"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	bootstraptokenapi "k8s.io/cluster-bootstrap/token/api"
	bootstraptokenutil "k8s.io/cluster-bootstrap/token/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/gardener/gardener/pkg/controllerutils"
	"github.com/gardener/gardener/pkg/utils"
	"github.com/gardener/gardener/pkg/utils/kubernetes"
)

// ComputeBootstrapToken computes and creates a new bootstrap token, and returns it.
func ComputeBootstrapToken(ctx context.Context, c client.Client, tokenID, description string, validity time.Duration) (secret *corev1.Secret, err error) {
	var (
		bootstrapTokenSecretKey string
	)

	secret = &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      bootstraptokenutil.BootstrapTokenSecretName(tokenID),
			Namespace: metav1.NamespaceSystem,
		},
	}

	if err = c.Get(ctx, kubernetes.Key(secret.Namespace, secret.Name), secret); client.IgnoreNotFound(err) != nil {
		return nil, err
	}

	validBootstrapTokenSecret, _ := regexp.Compile(`[a-z0-9]{16}`)
	if existingSecretToken, ok := secret.Data[bootstraptokenapi.BootstrapTokenSecretKey]; ok && validBootstrapTokenSecret.Match(existingSecretToken) {
		bootstrapTokenSecretKey = string(existingSecretToken)
	} else {
		bootstrapTokenSecretKey, err = utils.GenerateRandomStringFromCharset(16, "0123456789abcdefghijklmnopqrstuvwxyz")
		if err != nil {
			return nil, err
		}
	}

	data := map[string][]byte{
		bootstraptokenapi.BootstrapTokenDescriptionKey:      []byte(description),
		bootstraptokenapi.BootstrapTokenIDKey:               []byte(tokenID),
		bootstraptokenapi.BootstrapTokenSecretKey:           []byte(bootstrapTokenSecretKey),
		bootstraptokenapi.BootstrapTokenExpirationKey:       []byte(metav1.Now().Add(validity).Format(time.RFC3339)),
		bootstraptokenapi.BootstrapTokenUsageAuthentication: []byte("true"),
		bootstraptokenapi.BootstrapTokenUsageSigningKey:     []byte("true"),
	}

	_, err2 := controllerutils.GetAndCreateOrMergePatch(ctx, c, secret, func() error {
		secret.Type = bootstraptokenapi.SecretTypeBootstrapToken
		secret.Data = data
		return nil
	})

	return secret, err2
}

// FromSecretData returns the bootstrap token based on the secret data.
func FromSecretData(data map[string][]byte) string {
	return bootstraptokenutil.TokenFromIDAndSecret(string(data[bootstraptokenapi.BootstrapTokenIDKey]), string(data[bootstraptokenapi.BootstrapTokenSecretKey]))
}
