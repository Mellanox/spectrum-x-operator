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
	"encoding/json"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	uns "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

var _ = Describe("generateOVSNetwork", func() {
	const namespace = "test-ns"

	ctx := context.Background()
	r := &SpectrumXRailPoolConfigHostFlowsReconciler{}

	DescribeTable("MetaPluginsConfig rdma plugin",
		func(rtName string, addVRF bool) {
			rt := &v1alpha2.RailTopology{Name: rtName, MTU: 9000}
			ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, addVRF, namespace)
			Expect(ovs.Spec.MetaPluginsConfig).To(ContainSubstring(`"type": "rdma"`))
			Expect(ovs.Spec.MetaPluginsConfig).To(ContainSubstring(fmt.Sprintf(`"rdmaDeviceName": "rdma_%s"`, rtName)))
		},
		Entry("addVRF=false", "rail-1", false),
		Entry("addVRF=true", "rail-1", true),
	)

	DescribeTable("VRF meta-plugin",
		func(addVRF bool, expectVRF bool) {
			rt := &v1alpha2.RailTopology{Name: "rail-1", MTU: 9000}
			ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, addVRF, namespace)
			if expectVRF {
				Expect(ovs.Spec.MetaPluginsConfig).To(ContainSubstring(`"type": "vrf"`))
				Expect(ovs.Spec.MetaPluginsConfig).To(ContainSubstring(`"vrfname": "rail-1"`))
			} else {
				Expect(ovs.Spec.MetaPluginsConfig).NotTo(ContainSubstring(`"type": "vrf"`))
			}
		},
		Entry("not added when addVRF is false", false, false),
		Entry("added when addVRF is true", true, true),
	)

	It("VRF vrfname matches the rail topology name", func() {
		rt := &v1alpha2.RailTopology{Name: "my-custom-rail", MTU: 9000}
		ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, true, namespace)
		Expect(ovs.Spec.MetaPluginsConfig).To(ContainSubstring(`"vrfname": "my-custom-rail"`))
	})

	DescribeTable("bridge field",
		func(addBridge bool, expectedBridge string) {
			rt := &v1alpha2.RailTopology{Name: "rail-1", MTU: 9000}
			ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, addBridge, false, namespace)
			Expect(ovs.Spec.Bridge).To(Equal(expectedBridge))
		},
		Entry("bridge set when addBridge=true", true, "br-rail-rail-1"),
		Entry("bridge empty when addBridge=false", false, ""),
	)

	DescribeTable("IPAM configuration",
		func(cidrPoolRef, ipam, expectedIPAM string) {
			rt := &v1alpha2.RailTopology{Name: "rail-1", MTU: 9000, CidrPoolRef: cidrPoolRef, IPAM: ipam}
			ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, false, namespace)
			Expect(ovs.Spec.IPAM).To(Equal(expectedIPAM))
		},
		Entry("empty when neither set", "", "", ""),
		Entry("cidrpool IPAM from CidrPoolRef", "my-pool", "", `{"type": "nv-ipam","poolName": "my-pool", "poolType": "cidrpool"}`),
		Entry("custom IPAM from IPAM field", "", `{"type":"custom"}`, `{"type":"custom"}`),
		Entry("IPAM field takes precedence over CidrPoolRef", "my-pool", `{"type":"custom"}`, `{"type":"custom"}`),
	)

	It("sets object metadata correctly", func() {
		rt := &v1alpha2.RailTopology{Name: "rail-1", MTU: 9000}
		ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, false, namespace)
		Expect(ovs.Name).To(Equal("rail-1"))
		Expect(ovs.Namespace).To(Equal(namespace))
		Expect(ovs.Spec.ResourceName).To(Equal("rail-1"))
	})

	It("sets MTU and interface type", func() {
		rt := &v1alpha2.RailTopology{Name: "rail-1", MTU: 9000}
		ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, false, namespace)
		Expect(ovs.Spec.MTU).To(Equal(uint(9000)))
		Expect(ovs.Spec.InterfaceType).To(Equal(ovsNetworkInterfaceType))
	})

	// MetaPluginsConfig is injected verbatim as array elements inside the plugins array of the
	// OVS CNI NAD template (see ovs-cni-config.yaml). Wrapping it in [...] replicates that
	// context and confirms the rendered output is valid JSON regardless of plugin count.
	DescribeTable("MetaPluginsConfig produces valid JSON array elements",
		func(addVRF bool) {
			rt := &v1alpha2.RailTopology{Name: "rail-1", MTU: 9000}
			ovs := r.generateOVSNetwork(ctx, &v1alpha2.SpectrumXRailPoolConfigSpec{}, rt, false, addVRF, namespace)
			Expect(json.Valid([]byte("["+ovs.Spec.MetaPluginsConfig+"]"))).To(BeTrue(),
				"MetaPluginsConfig must be valid JSON array elements; got: %s", ovs.Spec.MetaPluginsConfig)
		},
		Entry("single plugin (addVRF=false)", false),
		Entry("two plugins (addVRF=true)", true),
	)
})

var _ = Describe("isCidrPoolIPv6", func() {
	const (
		nsName   = "ipv6-test-ns"
		poolName = "test-cidrpool"
	)

	var (
		r          *SpectrumXRailPoolConfigHostFlowsReconciler
		fakeClient *fake.ClientBuilder
	)

	BeforeEach(func() {
		fakeClient = fake.NewClientBuilder().WithScheme(scheme.Scheme)
		r = &SpectrumXRailPoolConfigHostFlowsReconciler{Client: fakeClient.Build()}
	})

	createCIDRPool := func(name, namespace, cidr string) {
		pool := &uns.Unstructured{}
		pool.SetAPIVersion("nv-ipam.nvidia.com/v1alpha1")
		pool.SetKind("CIDRPool")
		pool.SetName(name)
		pool.SetNamespace(namespace)
		Expect(uns.SetNestedField(pool.Object, cidr, "spec", "cidr")).To(Succeed())
		Expect(r.Client.Create(ctx, pool)).To(Succeed())
	}

	It("returns false for an IPv4 CIDR", func() {
		createCIDRPool(poolName, nsName, "192.168.0.0/24")
		result, err := r.isCidrPoolIPv6(ctx, poolName, nsName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeFalse())
	})

	It("returns true for an IPv6 CIDR", func() {
		createCIDRPool(poolName, nsName, "fd00::/64")
		result, err := r.isCidrPoolIPv6(ctx, poolName, nsName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("returns true for an IPv6 /128 CIDR", func() {
		createCIDRPool(poolName, nsName, "2001:db8::1/128")
		result, err := r.isCidrPoolIPv6(ctx, poolName, nsName)
		Expect(err).NotTo(HaveOccurred())
		Expect(result).To(BeTrue())
	})

	It("returns error when the CIDRPool is not found", func() {
		_, err := r.isCidrPoolIPv6(ctx, "nonexistent", nsName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get CIDRPool"))
		Expect(apierrors.IsNotFound(err)).To(BeTrue())
	})

	It("returns error when the CIDR is invalid", func() {
		createCIDRPool(poolName, nsName, "not-a-cidr")
		_, err := r.isCidrPoolIPv6(ctx, poolName, nsName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to parse CIDR"))
	})

	It("returns error when spec.cidr is absent", func() {
		pool := &uns.Unstructured{}
		pool.SetAPIVersion("nv-ipam.nvidia.com/v1alpha1")
		pool.SetKind("CIDRPool")
		pool.SetName(poolName)
		pool.SetNamespace(nsName)
		Expect(r.Client.Create(ctx, pool)).To(Succeed())

		_, err := r.isCidrPoolIPv6(ctx, poolName, nsName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.cidr not found"))
	})

	It("returns error when spec.cidr has wrong type", func() {
		pool := &uns.Unstructured{}
		pool.SetAPIVersion("nv-ipam.nvidia.com/v1alpha1")
		pool.SetKind("CIDRPool")
		pool.SetName(poolName)
		pool.SetNamespace(nsName)
		Expect(uns.SetNestedField(pool.Object, int64(42), "spec", "cidr")).To(Succeed())
		Expect(r.Client.Create(ctx, pool)).To(Succeed())

		_, err := r.isCidrPoolIPv6(ctx, poolName, nsName)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to read spec.cidr"))
	})
})

var _ = Describe("cidrPoolToRailConfigs", func() {
	const (
		nsName = "cidrpool-lister-ns"
	)

	var r *SpectrumXRailPoolConfigHostFlowsReconciler

	newRPC := func(name, namespace, cidrPoolRef string) *v1alpha2.SpectrumXRailPoolConfig {
		return &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
				NumVfs: 1,
				RailTopology: []v1alpha2.RailTopology{{
					Name:        "rail-1",
					CidrPoolRef: cidrPoolRef,
					NicSelector: v1alpha2.NicSelector{PfNames: []string{"eth0"}},
				}},
			},
		}
	}

	cidrPoolObj := func(name, namespace string) client.Object {
		pool := &uns.Unstructured{}
		pool.SetAPIVersion("nv-ipam.nvidia.com/v1alpha1")
		pool.SetKind("CIDRPool")
		pool.SetName(name)
		pool.SetNamespace(namespace)
		return pool
	}

	BeforeEach(func() {
		fc := fake.NewClientBuilder().WithScheme(scheme.Scheme).Build()
		r = &SpectrumXRailPoolConfigHostFlowsReconciler{Client: fc}
	})

	It("enqueues the RPC that references the CIDRPool", func() {
		rpc := newRPC("rpc-1", nsName, "my-pool")
		Expect(r.Client.Create(ctx, rpc)).To(Succeed())

		requests := r.cidrPoolToRailConfigs(ctx, cidrPoolObj("my-pool", nsName))
		Expect(requests).To(ConsistOf(reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: nsName, Name: "rpc-1"},
		}))
	})

	It("does not enqueue RPCs with a different CidrPoolRef", func() {
		rpc := newRPC("rpc-other", nsName, "other-pool")
		Expect(r.Client.Create(ctx, rpc)).To(Succeed())

		requests := r.cidrPoolToRailConfigs(ctx, cidrPoolObj("my-pool", nsName))
		Expect(requests).To(BeEmpty())
	})

	It("does not enqueue RPCs with no CidrPoolRef", func() {
		rpc := newRPC("rpc-no-ref", nsName, "")
		Expect(r.Client.Create(ctx, rpc)).To(Succeed())

		requests := r.cidrPoolToRailConfigs(ctx, cidrPoolObj("my-pool", nsName))
		Expect(requests).To(BeEmpty())
	})

	It("enqueues only the matching RPC among multiple", func() {
		rpc1 := newRPC("rpc-match", nsName, "my-pool")
		rpc2 := newRPC("rpc-no-match", nsName, "other-pool")
		Expect(r.Client.Create(ctx, rpc1)).To(Succeed())
		Expect(r.Client.Create(ctx, rpc2)).To(Succeed())

		requests := r.cidrPoolToRailConfigs(ctx, cidrPoolObj("my-pool", nsName))
		Expect(requests).To(ConsistOf(reconcile.Request{
			NamespacedName: types.NamespacedName{Namespace: nsName, Name: "rpc-match"},
		}))
	})

	It("does not enqueue RPCs from a different namespace", func() {
		rpc := newRPC("rpc-other-ns", "other-ns", "my-pool")
		Expect(r.Client.Create(ctx, rpc)).To(Succeed())

		requests := r.cidrPoolToRailConfigs(ctx, cidrPoolObj("my-pool", nsName))
		Expect(requests).To(BeEmpty())
	})
})
