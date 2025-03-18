/*
 Copyright 2025, NVIDIA CORPORATION & AFFILIATES

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

package controller

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/client"

	corev1 "k8s.io/api/core/v1"
	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"

	nvipamv1 "github.com/Mellanox/nvidia-k8s-ipam/api/v1alpha1"
)

var _ = Describe("SpectrumXConfig Controller", func() {
	Context("When reconciling Spectrum-x config map", func() {
		var (
			ctx = context.Background()
			ns  *corev1.Namespace
		)

		BeforeEach(func() {
			ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
			Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
		})
		AfterEach(func() {
			Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
		})
		It("should successfully create CIDRPools", func() {
			ipamReconciler.CIDRPoolsNamespace = ns.Name
			ipamReconciler.ConfigMapNamespace = ns.Name
			By("Reconciling the created config map")
			updateConfigMap(ctx, ns.Name, validConfig())
			By("Verify CIDR pools created")
			Eventually(func(g Gomega) {
				pool, err := getCIDRPool(ns.Name, "rail-1")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pool.Spec).To(BeEquivalentTo(rail1ExpectedSpec()))
				pool, err = getCIDRPool(ns.Name, "rail-2")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(pool.Spec).To(BeEquivalentTo(rail2ExpectedSpec()))
			}, time.Second*15).Should(Succeed())

		})
		It("should fail reconcile", func() {
			By("Reconciling the created config map with invalid json")
			updateConfigMap(ctx, ns.Name, "{{")
			By("Verify no CIDR pools created")
			Consistently(func(g Gomega) {
				cidrList := &nvipamv1.CIDRPoolList{}
				g.Expect(k8sClient.List(ctx, cidrList, &client.ListOptions{Namespace: ns.Name})).To(Succeed())
				g.Expect(cidrList.Items).To(BeEmpty())
			}).WithTimeout(2 * time.Second).Should(Succeed())
		})
	})
})

func updateConfigMap(ctx context.Context, ns string, data string) {
	d := map[string]string{config.ConfigMapKey: data}
	err := k8sClient.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: cmName, Namespace: ns},
		Data:       d,
	})
	if err == nil {
		return
	}
	if apiErrors.IsAlreadyExists(err) {
		configMap := &corev1.ConfigMap{}
		Expect(k8sClient.Get(
			ctx, types.NamespacedName{Name: cmName, Namespace: ns}, configMap)).NotTo(HaveOccurred())
		configMap.Data = d
		Expect(k8sClient.Update(
			ctx, configMap)).NotTo(HaveOccurred())
	} else {
		Expect(err).NotTo(HaveOccurred())
	}
}

func getCIDRPool(ns string, poolName string) (*nvipamv1.CIDRPool, error) {
	pool := &nvipamv1.CIDRPool{}
	err := k8sClient.Get(ctx, types.NamespacedName{Namespace: ns, Name: poolName}, pool)
	return pool, err
}

func validConfig() string {
	return `{
		"spectrum-x-networks": {
		  "cross_rail_subnet": "192.0.0.0/8",
		  "mtu": 9000,
		  "rails": [
			{
			  "name": "rail-1",
			  "subnet": "192.0.0.0/11"
			},
			{
			  "name": "rail-2",
			  "subnet": "192.32.0.0/11"
			}
		  ]
		},
		"rail_device_mapping": [
		  {
			"rail_name": "rail-1",
			"dev_name": "eth0"
		  },
		  {
			"rail_name": "rail-2",
			"dev_name": "eth1"
		  }
		],
		"hosts": [
		  {
			"host_id": "host-1",
			"rails": [
			  {
				"name": "rail-1",
				"network": "192.0.0.0/31",
				"peer_leaf_port_ip": "172.0.0.0"
			  },
			  {
				"name": "rail-2",
				"network": "192.32.0.0/31",
				"peer_leaf_port_ip": "172.32.0.0"
			  }
			]
		  },
		  {
			"host_id": "host-2",
			"rails": [
			  {
				"name": "rail-1",
				"network": "192.0.0.2/31",
				"peer_leaf_port_ip": "172.0.0.2"
			  },
			  {
				"name": "rail-2",
				"network": "192.32.0.2/31",
				"peer_leaf_port_ip": "172.32.0.2"
			  }
			]
		  }
		]
	  }`
}

func rail1ExpectedSpec() nvipamv1.CIDRPoolSpec {
	return nvipamv1.CIDRPoolSpec{
		CIDR:                 "192.0.0.0/11",
		GatewayIndex:         ptr.To[int32](0),
		PerNodeNetworkPrefix: 31,
		StaticAllocations: []nvipamv1.CIDRPoolStaticAllocation{
			{
				NodeName: "host-1",
				Gateway:  "192.0.0.1",
				Prefix:   "192.0.0.0/31",
			},
			{
				NodeName: "host-2",
				Gateway:  "192.0.0.3",
				Prefix:   "192.0.0.2/31",
			},
		},
		Routes: []nvipamv1.Route{
			{Dst: "192.0.0.0/11"},
			{Dst: "192.0.0.0/8"},
		},
	}
}

func rail2ExpectedSpec() nvipamv1.CIDRPoolSpec {
	return nvipamv1.CIDRPoolSpec{
		CIDR:                 "192.32.0.0/11",
		GatewayIndex:         ptr.To[int32](0),
		PerNodeNetworkPrefix: 31,
		StaticAllocations: []nvipamv1.CIDRPoolStaticAllocation{
			{
				NodeName: "host-1",
				Gateway:  "192.32.0.1",
				Prefix:   "192.32.0.0/31",
			},
			{
				NodeName: "host-2",
				Gateway:  "192.32.0.3",
				Prefix:   "192.32.0.2/31",
			},
		},
		Routes: []nvipamv1.Route{
			{Dst: "192.32.0.0/11"},
			{Dst: "192.0.0.0/8"},
		},
	}
}
