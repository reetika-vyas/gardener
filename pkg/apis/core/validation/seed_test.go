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

package validation_test

import (
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
	gomegatypes "github.com/onsi/gomega/types"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	"k8s.io/utils/pointer"

	"github.com/gardener/gardener/pkg/apis/core"
	. "github.com/gardener/gardener/pkg/apis/core/validation"
	"github.com/gardener/gardener/pkg/features"
	"github.com/gardener/gardener/pkg/utils/test"
	. "github.com/gardener/gardener/pkg/utils/test/matchers"
)

var _ = Describe("Seed Validation Tests", func() {
	var (
		seed         *core.Seed
		seedTemplate *core.SeedTemplate
		backup       *core.SeedBackup
	)

	BeforeEach(func() {
		region := "some-region"
		pods := "10.240.0.0/16"
		services := "10.241.0.0/16"
		nodesCIDR := "10.250.0.0/16"
		seed = &core.Seed{
			ObjectMeta: metav1.ObjectMeta{
				Name: "seed-1",
			},
			Spec: core.SeedSpec{
				Provider: core.SeedProvider{
					Type:   "foo",
					Region: "eu-west-1",
				},
				DNS: core.SeedDNS{
					IngressDomain: pointer.String("ingress.my-seed-1.example.com"),
				},
				SecretRef: &corev1.SecretReference{
					Name:      "seed-foo",
					Namespace: "garden",
				},
				Taints: []core.SeedTaint{
					{Key: "foo"},
				},
				Networks: core.SeedNetworks{
					Nodes:    &nodesCIDR,
					Pods:     "100.96.0.0/11",
					Services: "100.64.0.0/13",
					ShootDefaults: &core.ShootNetworks{
						Pods:     &pods,
						Services: &services,
					},
				},
				Backup: &core.SeedBackup{
					Provider: "foo",
					Region:   &region,
					SecretRef: corev1.SecretReference{
						Name:      "backup-foo",
						Namespace: "garden",
					},
				},
			},
		}
		seedTemplate = &core.SeedTemplate{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"foo": "bar",
				},
			},
			Spec: seed.Spec,
		}
	})

	Describe("#ValidateSeed, #ValidateSeedUpdate", func() {
		It("should not return any errors", func() {
			errorList := ValidateSeed(seed)

			Expect(errorList).To(HaveLen(0))
		})

		DescribeTable("Seed metadata",
			func(objectMeta metav1.ObjectMeta, matcher gomegatypes.GomegaMatcher) {
				seed.ObjectMeta = objectMeta

				errorList := ValidateSeed(seed)

				Expect(errorList).To(matcher)
			},

			Entry("should forbid Seed with empty metadata",
				metav1.ObjectMeta{},
				ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("metadata.name"),
				}))),
			),
			Entry("should forbid Seed with empty name",
				metav1.ObjectMeta{Name: ""},
				ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("metadata.name"),
				}))),
			),
			Entry("should forbid Seed with '.' in the name (not a DNS-1123 label compliant name)",
				metav1.ObjectMeta{Name: "seed.test"},
				ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("metadata.name"),
				}))),
			),
			Entry("should forbid Seed with '_' in the name (not a DNS-1123 subdomain)",
				metav1.ObjectMeta{Name: "seed_test"},
				ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("metadata.name"),
				}))),
			),
		)

		It("should forbid Seed specification with empty or invalid keys", func() {
			invalidCIDR := "invalid-cidr"
			seed.Spec.Provider = core.SeedProvider{
				Zones: []string{"a", "a"},
			}
			seed.Spec.DNS.IngressDomain = pointer.String("invalid_dns1123-subdomain")
			seed.Spec.SecretRef = &corev1.SecretReference{}
			seed.Spec.Networks = core.SeedNetworks{
				Nodes:    &invalidCIDR,
				Pods:     "300.300.300.300/300",
				Services: invalidCIDR,
				ShootDefaults: &core.ShootNetworks{
					Pods:     &invalidCIDR,
					Services: &invalidCIDR,
				},
			}
			seed.Spec.Taints = []core.SeedTaint{
				{Key: "foo"},
				{Key: "foo"},
				{Key: ""},
			}
			seed.Spec.Backup.SecretRef = corev1.SecretReference{}
			seed.Spec.Backup.Provider = ""
			minSize := resource.MustParse("-1")
			seed.Spec.Volume = &core.SeedVolume{
				MinimumSize: &minSize,
				Providers: []core.SeedVolumeProvider{
					{
						Purpose: "",
						Name:    "",
					},
					{
						Purpose: "duplicate",
						Name:    "value1",
					},
					{
						Purpose: "duplicate",
						Name:    "value2",
					},
				},
			}

			errorList := ValidateSeed(seed)

			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(field.ErrorTypeRequired),
					"Field":  Equal("spec.backup.provider"),
					"Detail": Equal(`must provide a backup cloud provider name`),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(field.ErrorTypeRequired),
					"Field":  Equal("spec.backup.secretRef.name"),
					"Detail": Equal(`must provide a name`),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(field.ErrorTypeRequired),
					"Field":  Equal("spec.backup.secretRef.namespace"),
					"Detail": Equal(`must provide a namespace`),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.provider.type"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.provider.region"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeDuplicate),
					"Field": Equal("spec.provider.zones[1]"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.dns.ingressDomain"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.secretRef.name"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.secretRef.namespace"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeDuplicate),
					"Field": Equal("spec.taints[1]"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.taints[2].key"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networks.nodes"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networks.pods"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networks.services"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networks.shootDefaults.pods"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networks.shootDefaults.services"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.volume.minimumSize"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.volume.providers[0].purpose"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.volume.providers[0].name"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeDuplicate),
					"Field": Equal("spec.volume.providers[2].purpose"),
				})),
			))
		})

		Context("networks", func() {
			It("should forbid specifying unsupported IP family", func() {
				seed.Spec.Networks.IPFamilies = []core.IPFamily{"IPv5"}

				errorList := ValidateSeed(seed)
				Expect(errorList).To(ConsistOf(PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeNotSupported),
					"Field": Equal("spec.networks.ipFamilies[0]"),
				}))))
			})

			Context("IPv4", func() {
				It("should allow valid networking configuration", func() {
					seed.Spec.Networks.Nodes = pointer.String("10.1.0.0/16")
					seed.Spec.Networks.Pods = "10.2.0.0/16"
					seed.Spec.Networks.Services = "10.3.0.0/16"
					seed.Spec.Networks.ShootDefaults.Pods = pointer.String("10.4.0.0/16")
					seed.Spec.Networks.ShootDefaults.Services = pointer.String("10.5.0.0/16")

					errorList := ValidateSeed(seed)
					Expect(errorList).To(BeEmpty())
				})

				It("should forbid invalid network CIDRs", func() {
					invalidCIDR := "invalid-cidr"

					seed.Spec.Networks.Nodes = &invalidCIDR
					seed.Spec.Networks.Pods = invalidCIDR
					seed.Spec.Networks.Services = invalidCIDR
					seed.Spec.Networks.ShootDefaults.Pods = &invalidCIDR
					seed.Spec.Networks.ShootDefaults.Services = &invalidCIDR

					errorList := ValidateSeed(seed)
					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.nodes"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.pods"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}))
				})

				It("should forbid IPv6 CIDRs with IPv4 IP family", func() {
					seed.Spec.Networks.Nodes = pointer.String("2001:db8:11::/48")
					seed.Spec.Networks.Pods = "2001:db8:12::/48"
					seed.Spec.Networks.Services = "2001:db8:13::/48"
					seed.Spec.Networks.ShootDefaults.Pods = pointer.String("2001:db8:1::/48")
					seed.Spec.Networks.ShootDefaults.Services = pointer.String("2001:db8:3::/48")

					errorList := ValidateSeed(seed)
					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.nodes"),
						"Detail": ContainSubstring("must be a valid IPv4 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.pods"),
						"Detail": ContainSubstring("must be a valid IPv4 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": ContainSubstring("must be a valid IPv4 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": ContainSubstring("must be a valid IPv4 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": ContainSubstring("must be a valid IPv4 address"),
					}))
				})

				It("should forbid Seed with overlapping networks", func() {
					shootDefaultPodCIDR := "10.0.1.128/28"     // 10.0.1.128 -> 10.0.1.13
					shootDefaultServiceCIDR := "10.0.1.144/30" // 10.0.1.144 -> 10.0.1.17

					nodesCIDR := "10.0.0.0/8" // 10.0.0.0 -> 10.255.255.25
					// Pods CIDR overlaps with Nodes network
					// Services CIDR overlaps with Nodes and Pods
					// Shoot default pod CIDR overlaps with services
					// Shoot default pod CIDR overlaps with shoot default pod CIDR
					seed.Spec.Networks = core.SeedNetworks{
						Nodes:    &nodesCIDR,     // 10.0.0.0 -> 10.255.255.25
						Pods:     "10.0.1.0/24",  // 10.0.1.0 -> 10.0.1.25
						Services: "10.0.1.64/26", // 10.0.1.64 -> 10.0.1.17
						ShootDefaults: &core.ShootNetworks{
							Pods:     &shootDefaultPodCIDR,
							Services: &shootDefaultServiceCIDR,
						},
					}

					errorList := ValidateSeed(seed)

					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":     Equal(field.ErrorTypeInvalid),
						"Field":    Equal("spec.networks.nodes"),
						"BadValue": Equal("10.0.0.0/8"),
						"Detail":   Equal("must not overlap with \"spec.networks.pods\" (\"10.0.1.0/24\")"),
					}, Fields{
						"Type":     Equal(field.ErrorTypeInvalid),
						"Field":    Equal("spec.networks.nodes"),
						"BadValue": Equal("10.0.0.0/8"),
						"Detail":   Equal("must not overlap with \"spec.networks.services\" (\"10.0.1.64/26\")"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": Equal(`must not overlap with "spec.networks.nodes" ("10.0.0.0/8")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": Equal(`must not overlap with "spec.networks.nodes" ("10.0.0.0/8")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": Equal(`must not overlap with "spec.networks.pods" ("10.0.1.0/24")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": Equal(`must not overlap with "spec.networks.pods" ("10.0.1.0/24")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": Equal(`must not overlap with "spec.networks.pods" ("10.0.1.0/24")`),
					}))
				})

				It("should forbid Seed with overlap to default vpn range (subset)", func() {
					shootDefaultPodCIDR := "192.168.123.128/28"     // 192.168.123.128 -> 192.168.123.143
					shootDefaultServiceCIDR := "192.168.123.200/32" // 192.168.123.200 -> 192.168.123.200

					nodesCIDR := "192.168.123.0/27" // 192.168.123.0 -> 192.168.123.31
					// Nodes network overlaps with default vpn range
					// Pods CIDR overlaps with default vpn range
					// Services CIDR overlaps with default vpn range
					// Shoot default pod CIDR overlaps with default vpn range
					// Shoot default service CIDR overlaps with default vpn range
					seed.Spec.Networks = core.SeedNetworks{
						Nodes:    &nodesCIDR,          // 192.168.123.0  -> 192.168.123.31
						Pods:     "192.168.123.32/30", // 192.168.123.32 -> 192.168.123.35
						Services: "192.168.123.64/26", // 192.168.123.64 -> 192.168.123.127
						ShootDefaults: &core.ShootNetworks{
							Pods:     &shootDefaultPodCIDR,     // 192.168.123.128 -> 192.168.123.143
							Services: &shootDefaultServiceCIDR, // 192.168.123.200 -> 192.168.123.200
						},
					}

					errorList := ValidateSeed(seed)

					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.pods"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.nodes"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}))
				})

				It("should forbid Seed with overlap to default vpn range (equality)", func() {
					// Services CIDR overlaps with default vpn range
					seed.Spec.Networks.Services = "192.168.123.0/24" // 192.168.123.0 -> 192.168.123.255

					errorList := ValidateSeed(seed)

					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}))
				})

				It("should forbid Seed with overlap to default vpn range (superset)", func() {
					// Pods CIDR overlaps with default vpn range
					seed.Spec.Networks.Pods = "192.168.0.0/16" // 192.168.0.0 -> 192.168.255.255

					errorList := ValidateSeed(seed)

					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.pods"),
						"Detail": Equal(`must not overlap with "[]" ("192.168.123.0/24")`),
					}))
				})
			})

			Context("IPv6", func() {
				BeforeEach(func() {
					DeferCleanup(test.WithFeatureGate(utilfeature.DefaultMutableFeatureGate, features.IPv6SingleStack, true))
					seed.Spec.Networks.IPFamilies = []core.IPFamily{core.IPFamilyIPv6}
				})

				It("should allow valid networking configuration", func() {
					seed.Spec.Networks.Nodes = pointer.String("2001:db8:11::/48")
					seed.Spec.Networks.Pods = "2001:db8:12::/48"
					seed.Spec.Networks.Services = "2001:db8:13::/48"
					seed.Spec.Networks.ShootDefaults.Pods = pointer.String("2001:db8:1::/48")
					seed.Spec.Networks.ShootDefaults.Services = pointer.String("2001:db8:3::/48")

					errorList := ValidateSeed(seed)
					Expect(errorList).To(BeEmpty())
				})

				It("should forbid invalid network CIDRs", func() {
					invalidCIDR := "invalid-cidr"

					seed.Spec.Networks.Nodes = &invalidCIDR
					seed.Spec.Networks.Pods = invalidCIDR
					seed.Spec.Networks.Services = invalidCIDR
					seed.Spec.Networks.ShootDefaults.Pods = &invalidCIDR
					seed.Spec.Networks.ShootDefaults.Services = &invalidCIDR

					errorList := ValidateSeed(seed)
					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.nodes"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.pods"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": ContainSubstring("invalid CIDR address"),
					}))
				})

				It("should forbid IPv4 CIDRs with IPv6 IP family", func() {
					seed.Spec.Networks.Nodes = pointer.String("10.1.0.0/16")
					seed.Spec.Networks.Pods = "10.2.0.0/16"
					seed.Spec.Networks.Services = "10.3.0.0/16"
					seed.Spec.Networks.ShootDefaults.Pods = pointer.String("10.4.0.0/16")
					seed.Spec.Networks.ShootDefaults.Services = pointer.String("10.5.0.0/16")

					errorList := ValidateSeed(seed)
					Expect(errorList).To(ConsistOfFields(Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.nodes"),
						"Detail": ContainSubstring("must be a valid IPv6 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.pods"),
						"Detail": ContainSubstring("must be a valid IPv6 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.services"),
						"Detail": ContainSubstring("must be a valid IPv6 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.services"),
						"Detail": ContainSubstring("must be a valid IPv6 address"),
					}, Fields{
						"Type":   Equal(field.ErrorTypeInvalid),
						"Field":  Equal("spec.networks.shootDefaults.pods"),
						"Detail": ContainSubstring("must be a valid IPv6 address"),
					}))
				})
			})
		})

		Context("networks update", func() {
			var oldSeed *core.Seed

			BeforeEach(func() {
				oldSeed = seed.DeepCopy()
			})

			It("should fail updating immutable fields with featureGate IPv6SingleStack enabled", func() {
				defer test.WithFeatureGate(utilfeature.DefaultMutableFeatureGate, features.IPv6SingleStack, true)()

				oldSeed.Spec.Networks.IPFamilies = []core.IPFamily{core.IPFamilyIPv4}

				newSeed := prepareSeedForUpdate(oldSeed)
				newSeed.Spec.Networks.IPFamilies = []core.IPFamily{core.IPFamilyIPv6}

				errorList := ValidateSeedUpdate(newSeed, oldSeed)

				Expect(errorList).To(ContainElement(HaveFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.networks.ipFamilies"),
					"Detail": ContainSubstring(`field is immutable`),
				})))
			})

			It("should fail updating immutable fields with featureGate IPv6SingleStack disabled", func() {
				oldSeed.Spec.Networks.IPFamilies = []core.IPFamily{core.IPFamilyIPv4}

				newSeed := prepareSeedForUpdate(oldSeed)
				newSeed.Spec.Networks.IPFamilies = []core.IPFamily{core.IPFamilyIPv6}

				errorList := ValidateSeedUpdate(newSeed, oldSeed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.networks.ipFamilies"),
					"Detail": ContainSubstring(`field is immutable`),
				}, Fields{
					"Type":     Equal(field.ErrorTypeInvalid),
					"Field":    Equal("spec.networks.ipFamilies"),
					"BadValue": Equal([]core.IPFamily{core.IPFamilyIPv6}),
					"Detail":   Equal("IPv6 single-stack networking is not supported"),
				}))
			})

			It("should allow adding a nodes CIDR", func() {
				oldSeed.Spec.Networks.Nodes = nil

				nodesCIDR := "10.1.0.0/16"
				newSeed := prepareSeedForUpdate(oldSeed)
				newSeed.Spec.Networks.Nodes = &nodesCIDR

				errorList := ValidateSeedUpdate(newSeed, oldSeed)

				Expect(errorList).To(BeEmpty())
			})

			It("should forbid removing the nodes CIDR", func() {
				newSeed := prepareSeedForUpdate(oldSeed)
				newSeed.Spec.Networks.Nodes = nil

				errorList := ValidateSeedUpdate(newSeed, oldSeed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.networks.nodes"),
					"Detail": Equal(`field is immutable`),
				}))
			})

			It("should forbid changing the nodes CIDR", func() {
				newSeed := prepareSeedForUpdate(oldSeed)

				differentNodesCIDR := "12.1.0.0/16"
				newSeed.Spec.Networks.Nodes = &differentNodesCIDR

				errorList := ValidateSeedUpdate(newSeed, oldSeed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.networks.nodes"),
					"Detail": Equal(`field is immutable`),
				}))
			})
		})

		Context("settings", func() {
			It("should allow valid load balancer service annotations", func() {
				seed.Spec.Settings = &core.SeedSettings{
					LoadBalancerServices: &core.SeedSettingLoadBalancerServices{
						Annotations: map[string]string{
							"simple":                          "bar",
							"now-with-dashes":                 "bar",
							"1-starts-with-num":               "bar",
							"1234":                            "bar",
							"simple/simple":                   "bar",
							"now-with-dashes/simple":          "bar",
							"now-with-dashes/now-with-dashes": "bar",
							"now.with.dots/simple":            "bar",
							"now-with.dashes-and.dots/simple": "bar",
							"1-num.2-num/3-num":               "bar",
							"1234/5678":                       "bar",
							"1.2.3.4/5678":                    "bar",
							"UpperCase123":                    "bar",
						},
					},
				}

				errorList := ValidateSeed(seed)

				Expect(errorList).To(BeEmpty())
			})

			It("should prevent invalid load balancer service annotations", func() {
				seed.Spec.Settings = &core.SeedSettings{
					LoadBalancerServices: &core.SeedSettingLoadBalancerServices{
						Annotations: map[string]string{
							"nospecialchars^=@":      "bar",
							"cantendwithadash-":      "bar",
							"only/one/slash":         "bar",
							strings.Repeat("a", 254): "bar",
						},
					},
				}

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.annotations"),
					})),
				))
			})

			It("should allow valid load balancer service traffic policy", func() {
				for _, p := range []string{"Cluster", "Local"} {
					policy := corev1.ServiceExternalTrafficPolicyType(p)
					seed.Spec.Settings = &core.SeedSettings{
						LoadBalancerServices: &core.SeedSettingLoadBalancerServices{
							ExternalTrafficPolicy: &policy,
						},
					}

					errorList := ValidateSeed(seed)

					Expect(errorList).To(BeEmpty(), fmt.Sprintf("seed validation should succeed with load balancer service traffic policy '%s' and have no errors", p))
				}
			})

			It("should prevent invalid load balancer service traffic policy", func() {
				policy := corev1.ServiceExternalTrafficPolicyType("foobar")
				seed.Spec.Settings = &core.SeedSettings{
					LoadBalancerServices: &core.SeedSettingLoadBalancerServices{
						ExternalTrafficPolicy: &policy,
					},
				}

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeNotSupported),
						"Field": Equal("spec.settings.loadBalancerServices.externalTrafficPolicy"),
					})),
				))
			})

			It("should allow valid zonal load balancer service annotations and traffic policy", func() {
				for _, p := range []string{"Cluster", "Local"} {
					policy := corev1.ServiceExternalTrafficPolicyType(p)
					zoneName := "a"
					seed.Spec.Provider.Zones = []string{zoneName, "b"}
					seed.Spec.Settings = &core.SeedSettings{
						LoadBalancerServices: &core.SeedSettingLoadBalancerServices{
							Zones: []core.SeedSettingLoadBalancerServicesZones{{
								Name: zoneName,
								Annotations: map[string]string{
									"simple":                          "bar",
									"now-with-dashes":                 "bar",
									"1-starts-with-num":               "bar",
									"1234":                            "bar",
									"simple/simple":                   "bar",
									"now-with-dashes/simple":          "bar",
									"now-with-dashes/now-with-dashes": "bar",
									"now.with.dots/simple":            "bar",
									"now-with.dashes-and.dots/simple": "bar",
									"1-num.2-num/3-num":               "bar",
									"1234/5678":                       "bar",
									"1.2.3.4/5678":                    "bar",
									"UpperCase123":                    "bar",
								},
								ExternalTrafficPolicy: &policy,
							}},
						},
					}

					errorList := ValidateSeed(seed)

					Expect(errorList).To(BeEmpty(), fmt.Sprintf("seed validation should succeed with valid zonal load balancer traffic policy '%s' and have no errors", p))
				}
			})

			It("should prevent invalid zonal load balancer service annotations, traffic policy and duplicate zones", func() {
				policy := corev1.ServiceExternalTrafficPolicyType("foobar")
				zoneName := "a"
				incorrectZoneName := "b"
				seed.Spec.Provider.Zones = []string{zoneName}
				seed.Spec.Settings = &core.SeedSettings{
					LoadBalancerServices: &core.SeedSettingLoadBalancerServices{
						Zones: []core.SeedSettingLoadBalancerServicesZones{
							{
								Name: incorrectZoneName,
								Annotations: map[string]string{
									"nospecialchars^=@":      "bar",
									"cantendwithadash-":      "bar",
									"only/one/slash":         "bar",
									strings.Repeat("a", 254): "bar",
								},
								ExternalTrafficPolicy: &policy,
							},
							{
								Name: incorrectZoneName,
							},
						},
					},
				}

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeForbidden),
						"Field": Equal("spec.settings.loadBalancerServices.zones"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.zones[0].annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.zones[0].annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.zones[0].annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeInvalid),
						"Field": Equal("spec.settings.loadBalancerServices.zones[0].annotations"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeNotSupported),
						"Field": Equal("spec.settings.loadBalancerServices.zones[0].externalTrafficPolicy"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeNotFound),
						"Field": Equal("spec.settings.loadBalancerServices.zones[0].name"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeNotFound),
						"Field": Equal("spec.settings.loadBalancerServices.zones[1].name"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"Type":  Equal(field.ErrorTypeDuplicate),
						"Field": Equal("spec.settings.loadBalancerServices.zones[1].name"),
					})),
				))
			})
		})

		It("should fail updating immutable fields", func() {
			nodesCIDR := "10.1.0.0/16"

			newSeed := prepareSeedForUpdate(seed)
			newSeed.Spec.Networks = core.SeedNetworks{
				Nodes:    &nodesCIDR,
				Pods:     "10.2.0.0/16",
				Services: "10.3.1.64/26",
			}
			otherRegion := "other-region"
			newSeed.Spec.Backup.Provider = "other-provider"
			newSeed.Spec.Backup.Region = &otherRegion
			otherDomain := "other-domain"
			newSeed.Spec.DNS.IngressDomain = &otherDomain

			errorList := ValidateSeedUpdate(newSeed, seed)

			Expect(errorList).To(ConsistOfFields(Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("spec.networks.pods"),
				"Detail": Equal(`field is immutable`),
			}, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("spec.networks.services"),
				"Detail": Equal(`field is immutable`),
			}, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("spec.networks.nodes"),
				"Detail": Equal(`field is immutable`),
			}, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("spec.backup.region"),
				"Detail": Equal(`field is immutable`),
			}, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("spec.backup.provider"),
				"Detail": Equal(`field is immutable`),
			}, Fields{
				"Type":   Equal(field.ErrorTypeInvalid),
				"Field":  Equal("spec.dns.ingressDomain"),
				"Detail": Equal(`field is immutable`),
			}))
		})

		Context("#validateSeedBackupUpdate", func() {
			It("should allow adding backup profile", func() {
				seed.Spec.Backup = nil
				newSeed := prepareSeedForUpdate(seed)
				newSeed.Spec.Backup = backup

				errorList := ValidateSeedUpdate(newSeed, seed)

				Expect(errorList).To(BeEmpty())
			})

			It("should forbid removing backup profile", func() {
				newSeed := prepareSeedForUpdate(seed)
				newSeed.Spec.Backup = nil

				errorList := ValidateSeedUpdate(newSeed, seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.backup"),
					"Detail": Equal(`field is immutable`),
				}))
			})
		})

		Context("ingress config", func() {
			BeforeEach(func() {
				seed.Spec.Ingress = &core.Ingress{
					Domain: "foo.bar.com",
					Controller: core.IngressController{
						Kind: "nginx",
					},
				}
				seed.Spec.DNS.IngressDomain = nil
				seed.Spec.DNS.Provider = &core.SeedDNSProvider{
					Type: "some-type",
					SecretRef: corev1.SecretReference{
						Name:      "foo",
						Namespace: "bar",
					},
				}
			})

			It("should fail if immutable spec.ingress.domain gets changed", func() {
				newSeed := prepareSeedForUpdate(seed)
				newSeed.Spec.Ingress.Domain = "other-domain"

				errorList := ValidateSeedUpdate(newSeed, seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.ingress.domain"),
					"Detail": Equal(`field is immutable`),
				}))
			})

			It("should succeed if ingress domain stays the same when migrating from dns.ingressDomain to ingress.domain", func() {
				newSeed := prepareSeedForUpdate(seed)
				seed.Spec.DNS.IngressDomain = pointer.String(seed.Spec.Ingress.Domain)
				seed.Spec.Ingress = nil

				Expect(ValidateSeedUpdate(newSeed, seed)).To(BeEmpty())
			})

			It("should fail if ingress domain changed when migrating from dns.ingressDomain to ingress.domain", func() {
				newSeed := prepareSeedForUpdate(seed)
				otherDomain := "other-domain"
				seed.Spec.Ingress = nil
				seed.Spec.DNS.IngressDomain = &otherDomain

				errorList := ValidateSeedUpdate(newSeed, seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.ingress.domain"),
					"Detail": Equal(`field is immutable`),
				}))
			})

			It("should succeed if ingress domain stays the same when migrating from ingress.domain to dns.ingressDomain", func() {
				newSeed := prepareSeedForUpdate(seed)
				currentDomain := seed.Spec.Ingress.Domain
				newSeed.Spec.Ingress = nil
				newSeed.Spec.DNS.IngressDomain = &currentDomain

				Expect(ValidateSeedUpdate(newSeed, seed)).To(BeEmpty())
			})

			It("should fail if ingress domain changed when migrating from ingress.domain to dns.ingressDomain", func() {
				newSeed := prepareSeedForUpdate(seed)
				otherDomain := "other-domain"
				newSeed.Spec.Ingress = nil
				newSeed.Spec.DNS.IngressDomain = &otherDomain

				errorList := ValidateSeedUpdate(newSeed, seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.dns.ingressDomain"),
					"Detail": Equal(`field is immutable`),
				}))
			})

			It("should succeed if ingress config is correct", func() {
				Expect(ValidateSeed(seed)).To(BeEmpty())
			})

			It("both nil", func() {
				seed.Spec.Ingress = nil

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec"),
				}))
			})

			It("both set", func() {
				seed.Spec.DNS.IngressDomain = pointer.String("foo")

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.dns.ingressDomain"),
				}))
			})

			It("requires to specify a DNS provider if ingress is specified", func() {
				seed.Spec.DNS.Provider = nil

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.dns.provider"),
				}))
			})

			It("should fail if kind is different to nginx", func() {
				seed.Spec.Ingress.Controller.Kind = "new-kind"

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":  Equal(field.ErrorTypeNotSupported),
					"Field": Equal("spec.ingress.controller.kind"),
				}))
			})

			It("should fail if the ingress domain is invalid", func() {
				seed.Spec.Ingress.Domain = "invalid_dns1123-subdomain"

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.ingress.domain"),
				}))
			})

			It("should fail if the ingress domain is empty", func() {
				seed.Spec.Ingress.Domain = ""

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Detail": Equal("either specify spec.ingress or spec.dns.ingressDomain"),
				}, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.ingress.domain"),
				}))
			})

			It("should fail if DNS provider config is invalid", func() {
				seed.Spec.DNS.Provider = &core.SeedDNSProvider{}

				errorList := ValidateSeed(seed)

				Expect(errorList).To(ConsistOfFields(Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.dns.provider.type"),
				}, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.dns.provider.secretRef.name"),
				}, Fields{
					"Type":  Equal(field.ErrorTypeRequired),
					"Field": Equal("spec.dns.provider.secretRef.namespace"),
				}))
			})
		})
	})

	Describe("#ValidateSeedStatusUpdate", func() {
		Context("validate .status.clusterIdentity updates", func() {
			newSeed := &core.Seed{
				Status: core.SeedStatus{
					ClusterIdentity: pointer.String("newClusterIdentity"),
				},
			}

			It("should fail to update seed status cluster identity if it already exists", func() {
				oldSeed := &core.Seed{Status: core.SeedStatus{
					ClusterIdentity: pointer.String("clusterIdentityExists"),
				}}
				allErrs := ValidateSeedStatusUpdate(newSeed, oldSeed)
				Expect(allErrs).To(ConsistOfFields(Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("status.clusterIdentity"),
					"Detail": Equal(`field is immutable`),
				}))
			})

			It("should not fail to update seed status cluster identity if it is missing", func() {
				oldSeed := &core.Seed{}
				allErrs := ValidateSeedStatusUpdate(newSeed, oldSeed)
				Expect(allErrs).To(BeEmpty())
			})
		})
	})

	Describe("#ValidateSeedTemplate", func() {
		It("should allow valid resources", func() {
			errorList := ValidateSeedTemplate(seedTemplate, nil)

			Expect(errorList).To(HaveLen(0))
		})

		It("should forbid invalid metadata or spec fields", func() {
			seedTemplate.Labels = map[string]string{"foo!": "bar"}
			seedTemplate.Annotations = map[string]string{"foo!": "bar"}
			seedTemplate.Spec.Networks.Nodes = pointer.String("")

			errorList := ValidateSeedTemplate(seedTemplate, nil)

			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("metadata.labels"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("metadata.annotations"),
				})),
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":  Equal(field.ErrorTypeInvalid),
					"Field": Equal("spec.networks.nodes"),
				})),
			))
		})
	})

	Describe("#ValidateSeedTemplateUpdate", func() {
		It("should allow valid updates", func() {
			errorList := ValidateSeedTemplateUpdate(seedTemplate, seedTemplate, nil)

			Expect(errorList).To(HaveLen(0))
		})

		It("should forbid changes to immutable fields in spec", func() {
			newSeedTemplate := *seedTemplate
			newSeedTemplate.Spec.Networks.Pods = "100.97.0.0/11"

			errorList := ValidateSeedTemplateUpdate(&newSeedTemplate, seedTemplate, nil)

			Expect(errorList).To(ConsistOf(
				PointTo(MatchFields(IgnoreExtras, Fields{
					"Type":   Equal(field.ErrorTypeInvalid),
					"Field":  Equal("spec.networks.pods"),
					"Detail": Equal("field is immutable"),
				})),
			))
		})
	})
})

func prepareSeedForUpdate(seed *core.Seed) *core.Seed {
	s := seed.DeepCopy()
	s.ResourceVersion = "1"
	return s
}
