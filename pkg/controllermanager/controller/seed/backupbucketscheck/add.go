// Copyright (c) 2022 SAP SE or an SAP affiliate company. All rights reserved. This file is licensed under the Apache Software License, v. 2 except as noted otherwise in the LICENSE file
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

package backupbucketscheck

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/clock"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	gardencorev1beta1 "github.com/gardener/gardener/pkg/apis/core/v1beta1"
	"github.com/gardener/gardener/pkg/controllerutils/mapper"
)

// ControllerName is the name of this controller.
const ControllerName = "seed-backupbuckets-check"

// AddToManager adds Reconciler to the given manager.
func (r *Reconciler) AddToManager(mgr manager.Manager) error {
	if r.Client == nil {
		r.Client = mgr.GetClient()
	}
	if r.Clock == nil {
		r.Clock = clock.RealClock{}
	}

	// It's not possible to overwrite the event handler when using the controller builder. Hence, we have to build up
	// the controller manually.
	c, err := controller.New(
		ControllerName,
		mgr,
		controller.Options{
			Reconciler:              r,
			MaxConcurrentReconciles: pointer.IntDeref(r.Config.ConcurrentSyncs, 0),
			RecoverPanic:            true,
			// if going into exponential backoff, wait at most the configured sync period
			RateLimiter: workqueue.NewWithMaxWaitRateLimiter(workqueue.DefaultControllerRateLimiter(), r.Config.SyncPeriod.Duration),
		},
	)
	if err != nil {
		return err
	}

	return c.Watch(
		&source.Kind{Type: &gardencorev1beta1.BackupBucket{}},
		mapper.EnqueueRequestsFrom(mapper.MapFunc(r.MapBackupBucketToSeed), mapper.UpdateWithNew, c.GetLogger()),
		r.BackupBucketPredicate(),
	)
}

// BackupBucketPredicate reacts only on 'CREATE' and 'UPDATE' events. It returns false if .spec.seedName == nil. For
// updates, it only returns true when the .stauts.lastError changed.
func (r *Reconciler) BackupBucketPredicate() predicate.Predicate {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			backupBucket, ok := e.Object.(*gardencorev1beta1.BackupBucket)
			if !ok {
				return false
			}
			return backupBucket.Spec.SeedName != nil
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			backupBucket, ok := e.ObjectNew.(*gardencorev1beta1.BackupBucket)
			if !ok {
				return false
			}

			oldBackupBucket, ok := e.ObjectOld.(*gardencorev1beta1.BackupBucket)
			if !ok {
				return false
			}

			if backupBucket.Spec.SeedName == nil {
				return false
			}
			return lastErrorChanged(oldBackupBucket.Status.LastError, backupBucket.Status.LastError)
		},
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

func lastErrorChanged(oldLastError, newLastError *gardencorev1beta1.LastError) bool {
	return (oldLastError == nil && newLastError != nil) ||
		(oldLastError != nil && newLastError == nil) ||
		(oldLastError != nil && newLastError != nil && oldLastError.Description != newLastError.Description)
}

// MapBackupBucketToSeed is a mapper.MapFunc for mapping a BackupBucket to the referenced Seed.
func (r *Reconciler) MapBackupBucketToSeed(_ context.Context, _ logr.Logger, _ client.Reader, obj client.Object) []reconcile.Request {
	backupBucket, ok := obj.(*gardencorev1beta1.BackupBucket)
	if !ok {
		return nil
	}

	if backupBucket.Spec.SeedName == nil {
		return nil
	}

	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: *backupBucket.Spec.SeedName}}}
}
