// Copyright (c) 2021 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package clusteridentity

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1beta1constants "github.com/gardener/gardener/pkg/apis/core/v1beta1/constants"
	"github.com/gardener/gardener/pkg/client/kubernetes"
	"github.com/gardener/gardener/pkg/operation/botanist/component"
	"github.com/gardener/gardener/pkg/utils/managedresources"
)

const (
	// ManagedResourceControlName is the name of the ManagedResource containing the resource specifications.
	ManagedResourceControlName = "cluster-identity"
	// ShootManagedResourceName is the name of the ManagedResource containing the resource specifications.
	ShootManagedResourceName = "shoot-core-" + ManagedResourceControlName
)

// Interface contains functions for managing cluster identities.
type Interface interface {
	component.DeployWaiter
	SetIdentity(string)
}

type clusterIdentity struct {
	client                  client.Client
	namespace               string
	identity                string
	identityType            string
	managedResourceRegistry *managedresources.Registry
	managedResourceName     string
	managedResourceDeleteFn func(ctx context.Context, client client.Client, namespace string, name string) error
}

func new(
	c client.Client,
	namespace string,
	identity string,
	identityType string,
	managedResourceRegistry *managedresources.Registry,
	managedResourceName string,
	managedResourceDeleteFn func(ctx context.Context, client client.Client, namespace string, name string) error,
) Interface {
	return &clusterIdentity{
		client:                  c,
		namespace:               namespace,
		identity:                identity,
		identityType:            identityType,
		managedResourceRegistry: managedResourceRegistry,
		managedResourceName:     managedResourceName,
		managedResourceDeleteFn: managedResourceDeleteFn,
	}
}

// NewForSeed creates new instance of Deployer for the seed's cluster identity.
func NewForSeed(c client.Client, namespace, identity string) Interface {
	return new(
		c,
		namespace,
		identity,
		"seed",
		managedresources.NewRegistry(kubernetes.SeedScheme, kubernetes.SeedCodec, kubernetes.SeedSerializer),
		ManagedResourceControlName,
		managedresources.DeleteForSeed,
	)
}

// NewForShoot creates new instance of Deployer for the shoot's cluster identity.
func NewForShoot(c client.Client, namespace, identity string) Interface {
	return new(
		c,
		namespace,
		identity,
		"shoot",
		managedresources.NewRegistry(kubernetes.ShootScheme, kubernetes.ShootCodec, kubernetes.ShootSerializer),
		ShootManagedResourceName,
		managedresources.DeleteForShoot,
	)
}

func (c *clusterIdentity) Deploy(ctx context.Context) error {
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      v1beta1constants.ClusterIdentity,
			Namespace: metav1.NamespaceSystem,
		},
		Data: map[string]string{
			v1beta1constants.ClusterIdentity: c.identity,
		},
	}

	resources, err := c.managedResourceRegistry.AddAllAndSerialize(configMap)
	if err != nil {
		return err
	}

	switch c.identityType {
	case "shoot":
		return managedresources.CreateForShoot(ctx, c.client, c.namespace, c.managedResourceName, managedresources.LabelValueGardener, false, resources)
	case "seed":
		return managedresources.CreateForSeed(ctx, c.client, c.namespace, c.managedResourceName, false, resources)
	default:
		// this should never happen
		return fmt.Errorf("unknown cluster identity type %s", c.identityType)
	}
}

func (c *clusterIdentity) Destroy(ctx context.Context) error {
	return c.managedResourceDeleteFn(ctx, c.client, c.namespace, c.managedResourceName)
}

func (c *clusterIdentity) SetIdentity(identity string) {
	c.identity = identity
}

// TimeoutWaitForManagedResource is the timeout used while waiting for the ManagedResources to become healthy
// or deleted.
var TimeoutWaitForManagedResource = 2 * time.Minute

func (c *clusterIdentity) Wait(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilHealthy(timeoutCtx, c.client, c.namespace, c.managedResourceName)
}

func (c *clusterIdentity) WaitCleanup(ctx context.Context) error {
	timeoutCtx, cancel := context.WithTimeout(ctx, TimeoutWaitForManagedResource)
	defer cancel()

	return managedresources.WaitUntilDeleted(timeoutCtx, c.client, c.namespace, c.managedResourceName)
}
