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
	"errors"
	"time"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha1"
	gomock "github.com/golang/mock/gomock"
	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	bridgeName = "test-bridge"
	pfName     = "test-pf"
	rpcName    = "test-rpc"
	snnpName   = "test-snnp"
)

var _ = Describe("SpectrumXRailPoolConfigHostFlowsReconciler", func() {
	var (
		controller *SpectrumXRailPoolConfigHostFlowsReconciler
		ctrl       *gomock.Controller
		flowsMock  *MockFlowsAPI
		ns         *v1.Namespace
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		flowsMock = NewMockFlowsAPI(ctrl)
		controller = NewSpectrumXRailPoolConfigHostFlowsReconciler(k8sClient, flowsMock)

		ns = &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"},
		}

		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
	})

	AfterEach(func() {
		ctrl.Finish()
		Expect(
			k8sClient.Delete(ctx, ns, client.PropagationPolicy(metav1.DeletePropagationForeground)),
		).Should(Succeed())
	})

	It("should fail if the SriovNetworkNodePolicy is not found", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: "test-sriov-network-node-policy",
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).To(HaveOccurred())
	})

	It("should fail if the SriovNetworkNodePolicy has no PFs", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).To(HaveOccurred())
	})

	It("should remove flows if the SpectrumXRailPoolConfig is being deleted", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())
		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())

		rpc.DeletionTimestamp = &metav1.Time{Time: time.Now()}

		flowsMock.EXPECT().GetBridgeNameFromPortName(pfName).Return(bridgeName, nil)
		flowsMock.EXPECT().DeleteFlowsByCookie(bridgeName, hostFlowsCookie).Return(nil)

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should add the software multiplane flows", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}
		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		flowsMock.EXPECT().GetBridgeNameFromPortName(pfName).Return(bridgeName, nil)
		flowsMock.EXPECT().AddSoftwareMultiplaneFlows(bridgeName, hostFlowsCookie, pfName).Return(nil)

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return an error if fails to get bridge name from port name", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		flowsMock.EXPECT().GetBridgeNameFromPortName(pfName).Return("", errors.New("failed to get bridge name from port name"))

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to get bridge name from port name"))
	})

	It("should return an error if fails to add software multiplane flows", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		gomock.InOrder(
			flowsMock.
				EXPECT().
				GetBridgeNameFromPortName(pfName).
				Return(bridgeName, nil),
			flowsMock.
				EXPECT().
				AddSoftwareMultiplaneFlows(bridgeName, hostFlowsCookie, pfName).
				Return(errors.New("failed to add software multiplane flows")),
		)

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to add software multiplane flows: failed to add software multiplane flows"))
	})

	It("should add the hardware multiplane groups andflows", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "hwplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
				Annotations: map[string]string{
					"spectrumx.nvidia.com/bridge-additional-uplinks": "test-pf2,test-pf3",
				},
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		flowsMock.EXPECT().GetBridgeNameFromPortName(pfName).Return(bridgeName, nil)
		flowsMock.EXPECT().AddHardwareMultiplaneGroups(bridgeName, []string{pfName, "test-pf2", "test-pf3"}).Return(nil)
		flowsMock.EXPECT().AddHardwareMultiplaneFlows(bridgeName, hostFlowsCookie, []string{pfName, "test-pf2", "test-pf3"}).Return(nil)

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).NotTo(HaveOccurred())
	})

	It("should return an error if fails to add hardware multiplane groups", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "hwplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		gomock.InOrder(
			flowsMock.EXPECT().GetBridgeNameFromPortName(pfName).Return(bridgeName, nil),
			flowsMock.
				EXPECT().
				AddHardwareMultiplaneGroups(bridgeName, []string{pfName}).
				Return(errors.New("test error")),
		)

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to add hardware multiplane groups: test error"))
	})

	It("should return an error if fails to add hardware multiplane flows", func() {
		rpc := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: ns.Name,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "hwplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName,
				Namespace: ns.Name,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{pfName},
				},
				NodeSelector: make(map[string]string),
			},
		}

		Expect(k8sClient.Create(ctx, snnp)).Should(Succeed())
		Expect(k8sClient.Create(ctx, rpc)).Should(Succeed())

		gomock.InOrder(
			flowsMock.EXPECT().GetBridgeNameFromPortName(pfName).Return(bridgeName, nil),
			flowsMock.EXPECT().AddHardwareMultiplaneGroups(bridgeName, []string{pfName}).Return(nil),
			flowsMock.EXPECT().AddHardwareMultiplaneFlows(bridgeName, hostFlowsCookie, []string{pfName}).Return(errors.New("test error")),
		)

		_, err := controller.Reconcile(ctx, rpc)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("failed to add hardware multiplane flows: test error"))
	})
})

// This block uses the controller-runtime fake client instead of the envtest k8sClient.
// Previous tests create a lot of namespaces and other resources that might be deleted but not yet garbage collected.
// Because the node lister interacts with cluster-scoped objects, it might be affected by these resources.
// There is unfortunately no easy way to clean up these resources, so we use the fake client instead.
var _ = Describe("nodeRailLister", func() {
	const (
		nodeName = "test-node"
		nsName   = "test-ns"
	)

	var (
		nodeRailLister *nodeRailLister
		fakeClient     client.Client
	)

	BeforeEach(func() {
		ns := v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: nsName},
		}

		fakeClient = fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(&ns).Build()
		nodeRailLister = NewNodeRailLister(fakeClient, nodeName)
	})

	It("should return nothing if the node is not found", func() {
		requests := nodeRailLister.ListRailPoolConfigsForNode(ctx, nil)
		Expect(requests).To(BeEmpty())
	})

	It("should return nothing if there is no SpectrumXRailPoolConfig", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
			},
		}

		Expect(fakeClient.Create(ctx, &node)).Should(Succeed())

		requests := nodeRailLister.ListRailPoolConfigsForNode(ctx, nil)
		Expect(requests).To(BeEmpty())
	})

	It("should return nothing if no SriovNetworkNodePolicy is found for the SpectrumXRailPoolConfig", func() {
		node := v1.Node{
			ObjectMeta: metav1.ObjectMeta{Name: nodeName},
		}

		rpc := v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: nsName,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		Expect(fakeClient.Create(ctx, &node)).Should(Succeed())
		Expect(fakeClient.Create(ctx, &rpc)).Should(Succeed())

		requests := nodeRailLister.ListRailPoolConfigsForNode(ctx, nil)
		Expect(requests).To(BeEmpty())
	})

	It("should one SpectrumXRailPoolConfig", func() {
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: nodeName,
				Labels: map[string]string{
					"this selector": "should match",
				},
			},
		}

		const (
			rpcName0  = rpcName + "0"
			rpcName1  = rpcName + "1"
			snnpName0 = snnpName + "0"
			snnpName1 = snnpName + "1"
		)

		rpc0 := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName0,
				Namespace: nsName,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName0,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		rpc1 := &v1alpha1.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName1,
				Namespace: nsName,
			},
			Spec: v1alpha1.SpectrumXRailPoolConfigSpec{
				SriovNetworkNodePolicyRef: snnpName1,
				MultiplaneMode:            "swplb",
				CidrPoolRef:               "test-cidr-pool",
			},
		}

		snnp0 := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName0,
				Namespace: nsName,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{""},
				},
				NodeSelector: map[string]string{"this selector": "should match"},
			},
		}

		snnp1 := &sriovv1.SriovNetworkNodePolicy{
			ObjectMeta: metav1.ObjectMeta{
				Name:      snnpName1,
				Namespace: nsName,
			},
			Spec: sriovv1.SriovNetworkNodePolicySpec{
				NicSelector: sriovv1.SriovNetworkNicSelector{
					PfNames: []string{""},
				},
				NodeSelector: map[string]string{"this selector": "should not match"},
			},
		}

		for _, obj := range []client.Object{node, rpc0, rpc1, snnp0, snnp1} {
			Expect(fakeClient.Create(ctx, obj)).Should(Succeed())
		}

		requests := nodeRailLister.ListRailPoolConfigsForNode(ctx, nil)
		Expect(requests).To(ConsistOf(reconcile.Request{NamespacedName: types.NamespacedName{Namespace: nsName, Name: rpcName0}}))
	})
})
