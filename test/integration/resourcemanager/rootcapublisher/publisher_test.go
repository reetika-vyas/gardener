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

package rootcapublisher_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	. "github.com/gardener/gardener/pkg/utils/test/matchers"
)

var _ = Describe("RootCAPublisher tests", func() {
	var (
		testNamespace *corev1.Namespace
		configMap     *corev1.ConfigMap
	)

	BeforeEach(func() {
		testNamespace = &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: testID + "-",
			},
		}
		Expect(testClient.Create(ctx, testNamespace)).To(Succeed())
		log.Info("Created Namespace for test", "namespaceName", testNamespace.Name)

		DeferCleanup(func() {
			By("Delete test Namespace")
			Expect(testClient.Delete(ctx, testNamespace)).To(Or(Succeed(), BeNotFoundError()))
		})

		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "kube-root-ca.crt",
				Namespace: testNamespace.Name,
			},
		}
	})

	Context("kube-root-ca.crt config map", func() {
		BeforeEach(func() {
			Eventually(func() error {
				return testClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
			}).Should(Succeed())
		})

		Context("controller should be active", func() {
			AfterEach(func() {
				Eventually(func(g Gomega) map[string]string {
					g.Expect(testClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)).To(Succeed())
					return configMap.Data
				}).Should(HaveKeyWithValue("ca.crt", string(caCert)))
			})

			It("should create a config map on creating a namespace", func() {})

			It("should revert the config map data after manual changes", func() {
				configMap.Data = nil
				Expect(testClient.Update(ctx, configMap)).To(Succeed())
			})

			It("should recreate the config map if it gets deleted", func() {
				Expect(testClient.Delete(ctx, configMap)).To(Succeed())

				Eventually(func() error {
					return testClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
				}).Should(BeNotFoundError())
			})
		})

		It("should ignore config maps that are managed by the upstream rootcapublisher controller", func() {
			configMap.Data = nil
			configMap.Annotations = map[string]string{"kubernetes.io/description": "test description"}
			Expect(testClient.Update(ctx, configMap)).To(Succeed())

			Consistently(func() map[string]string {
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)).To(Succeed())
				return configMap.Data
			}).Should(BeNil())
		})
	})

	Context("custom config maps", func() {
		It("should ignore config maps with different name", func() {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-other-configmap",
					Namespace: testNamespace.Name,
				},
				Data: map[string]string{"foo": "bar"},
			}
			Expect(testClient.Create(ctx, cm)).To(Succeed())

			Consistently(func() map[string]string {
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				return cm.Data
			}).Should(SatisfyAll(HaveLen(1), HaveKeyWithValue("foo", "bar")))

			patch := client.MergeFrom(cm.DeepCopy())
			cm.Data["foo"] = "newbar"
			Expect(testClient.Patch(ctx, cm, patch)).To(Succeed())

			Consistently(func() map[string]string {
				Expect(testClient.Get(ctx, client.ObjectKeyFromObject(cm), cm)).To(Succeed())
				return cm.Data
			}).Should(SatisfyAll(HaveLen(1), HaveKeyWithValue("foo", "newbar")))
		})
	})
})
