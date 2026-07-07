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
	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	"github.com/Mellanox/spectrum-x-operator/pkg/state"
	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	rpcName  = "test-rpc"
	snnpName = "test-snnp"
)

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

		rpc := v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName,
				Namespace: nsName,
			},
			Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
				RailTopology: []v1alpha2.RailTopology{{
					Name: snnpName,
					NicSelector: v1alpha2.NicSelector{
						PfNames: []string{"test-pf"},
					},
				}},
			},
		}

		Expect(fakeClient.Create(ctx, &node)).Should(Succeed())
		Expect(fakeClient.Create(ctx, &rpc)).Should(Succeed())

		requests := nodeRailLister.ListRailPoolConfigsForNode(ctx, nil)
		Expect(requests).To(BeEmpty())
	})

	It("should return one SpectrumXRailPoolConfig", func() {
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

		rpc0 := &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName0,
				Namespace: nsName,
			},
			Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
				RailTopology: []v1alpha2.RailTopology{{
					Name: snnpName0,
					NicSelector: v1alpha2.NicSelector{
						PfNames: []string{""},
					},
				}},
			},
		}

		rpc1 := &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rpcName1,
				Namespace: nsName,
			},
			Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
				RailTopology: []v1alpha2.RailTopology{{
					Name: snnpName1,
					NicSelector: v1alpha2.NicSelector{
						PfNames: []string{""},
					},
				}},
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

var _ = Describe("SpectrumXRailPoolConfigHostFlowsReconciler status", func() {
	const (
		localNodeName  = "test-node-a"
		remoteNodeName = "test-node-b"
		nsName         = "test-ns"
	)

	var (
		fakeClient client.Client
		reconciler *SpectrumXRailPoolConfigHostFlowsReconciler
	)

	newSelectedNode := func(name string) *v1.Node {
		return &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					"pool": "spectrum-x",
				},
			},
		}
	}

	newUnselectedNode := func(name string) *v1.Node {
		return &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
				Labels: map[string]string{
					"pool": "other",
				},
			},
		}
	}

	newRailPoolConfig := func() *v1alpha2.SpectrumXRailPoolConfig {
		return &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name:       rpcName,
				Namespace:  nsName,
				Generation: 1,
				Finalizers: []string{finalizerName},
			},
			Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
				NodeSelector: map[string]string{"pool": "spectrum-x"},
				NumVfs:       1,
			},
			Status: v1alpha2.SpectrumXRailPoolConfigStatus{
				SyncStatus:         v1alpha2.SyncStatusSucceeded,
				ObservedGeneration: 1,
				NodeStates: []v1alpha2.NodeState{
					{
						Name:               remoteNodeName,
						State:              v1alpha2.SyncStatusSucceeded,
						ObservedGeneration: 1,
					},
				},
			},
		}
	}

	newReconciler := func(objects ...client.Object) *SpectrumXRailPoolConfigHostFlowsReconciler {
		fakeClient = fake.NewClientBuilder().
			WithScheme(scheme.Scheme).
			WithStatusSubresource(&v1alpha2.SpectrumXRailPoolConfig{}).
			WithObjects(objects...).
			Build()
		return NewSpectrumXRailPoolConfigHostFlowsReconciler(fakeClient, scheme.Scheme, nil, nil, nil, localNodeName)
	}

	It("does not skip a selected local node missing current node state when the global status already succeeded", func() {
		rpc := newRailPoolConfig()
		reconciler = newReconciler(
			&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}},
			newSelectedNode(localNodeName),
			newSelectedNode(remoteNodeName),
			rpc,
		)

		err := reconciler.doReconcile(ctx, rpc)
		Expect(err).To(MatchError(ContainSubstring("expected one or more rail topologies to be specified")))

		updated := &v1alpha2.SpectrumXRailPoolConfig{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: nsName, Name: rpcName}, updated)).To(Succeed())
		localState, err := state.GetNodeState(updated.Status.NodeStates, localNodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(localState.State).To(Equal(v1alpha2.SyncStatusFailed))
		Expect(localState.Message).To(ContainSubstring("expected one or more rail topologies to be specified"))
	})

	It("does not skip host reconciliation when selected local node state already succeeded", func() {
		rpc := newRailPoolConfig()
		rpc.Status.NodeStates = append(rpc.Status.NodeStates, v1alpha2.NodeState{
			Name:               localNodeName,
			State:              v1alpha2.SyncStatusSucceeded,
			ObservedGeneration: rpc.Generation,
		})
		reconciler = newReconciler(
			&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}},
			newSelectedNode(localNodeName),
			newSelectedNode(remoteNodeName),
			rpc,
		)

		err := reconciler.doReconcile(ctx, rpc)
		Expect(err).To(MatchError(ContainSubstring("expected one or more rail topologies to be specified")))
	})

	It("keeps aggregate status in progress until every selected node reports current success", func() {
		rpc := newRailPoolConfig()
		rpc.Status.SyncStatus = v1alpha2.SyncStatusInProgress
		rpc.Status.NodeStates = []v1alpha2.NodeState{
			{
				Name:               localNodeName,
				State:              v1alpha2.SyncStatusSucceeded,
				ObservedGeneration: 1,
			},
		}
		reconciler = newReconciler(
			&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}},
			newSelectedNode(localNodeName),
			newSelectedNode(remoteNodeName),
			rpc,
		)

		Expect(reconciler.patchSyncStatus(ctx, rpc, v1alpha2.SyncStatusSucceeded)).To(Succeed())

		updated := &v1alpha2.SpectrumXRailPoolConfig{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: nsName, Name: rpcName}, updated)).To(Succeed())
		Expect(updated.Status.SyncStatus).To(Equal(v1alpha2.SyncStatusInProgress))
	})

	It("sets aggregate status to failed when the local selected node reports current failure", func() {
		rpc := newRailPoolConfig()
		rpc.Status.SyncStatus = v1alpha2.SyncStatusInProgress
		reconciler = newReconciler(
			&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}},
			newSelectedNode(localNodeName),
			newSelectedNode(remoteNodeName),
			rpc,
		)

		Expect(reconciler.patchSyncStatus(ctx, rpc, v1alpha2.SyncStatusFailed)).To(Succeed())

		updated := &v1alpha2.SpectrumXRailPoolConfig{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: nsName, Name: rpcName}, updated)).To(Succeed())
		Expect(updated.Status.SyncStatus).To(Equal(v1alpha2.SyncStatusFailed))
	})

	It("does not advance local observed generation when a newer CR generation appears during status patching", func() {
		staleRPC := newRailPoolConfig()
		staleRPC.Status.NodeStates = []v1alpha2.NodeState{
			{
				Name:               localNodeName,
				State:              v1alpha2.SyncStatusSucceeded,
				ObservedGeneration: staleRPC.Generation,
			},
		}
		latestRPC := staleRPC.DeepCopy()
		latestRPC.Generation = staleRPC.Generation + 1
		reconciler = newReconciler(
			&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}},
			newSelectedNode(localNodeName),
			newSelectedNode(remoteNodeName),
			latestRPC,
		)

		err := reconciler.patchSyncStatus(ctx, staleRPC, v1alpha2.SyncStatusSucceeded)
		Expect(apierrors.IsConflict(err)).To(BeTrue())

		updated := &v1alpha2.SpectrumXRailPoolConfig{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: nsName, Name: rpcName}, updated)).To(Succeed())
		localState, err := state.GetNodeState(updated.Status.NodeStates, localNodeName)
		Expect(err).NotTo(HaveOccurred())
		Expect(localState.ObservedGeneration).To(Equal(staleRPC.Generation))
	})

	It("does not add local node state when an unselected node observes a reconcile error", func() {
		rpc := newRailPoolConfig()
		rpc.Status.SyncStatus = v1alpha2.SyncStatusInProgress
		rpc.Status.NodeStates = nil
		reconciler = newReconciler(
			&v1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: nsName}},
			newUnselectedNode(localNodeName),
			newSelectedNode(remoteNodeName),
			rpc,
		)

		err := reconciler.doReconcile(ctx, rpc)
		Expect(err).To(MatchError(ContainSubstring("expected one or more rail topologies to be specified")))

		updated := &v1alpha2.SpectrumXRailPoolConfig{}
		Expect(fakeClient.Get(ctx, types.NamespacedName{Namespace: nsName, Name: rpcName}, updated)).To(Succeed())
		Expect(updated.Status.NodeStates).To(BeEmpty())
	})
})
