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
	"fmt"
	"strings"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha1"

	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const hostFlowsCookie uint64 = 0x2

// SpectrumXRailPoolConfigHostFlowsReconciler reconciles a SpectrumXRailPoolConfig object
type SpectrumXRailPoolConfigHostFlowsReconciler struct {
	client.Client
	flows FlowsAPI
}

func NewSpectrumXRailPoolConfigHostFlowsReconciler(
	client client.Client,
	flows FlowsAPI,
) *SpectrumXRailPoolConfigHostFlowsReconciler {
	return &SpectrumXRailPoolConfigHostFlowsReconciler{
		Client: client,
		flows:  flows,
	}
}

// +kubebuilder:rbac:groups=spectrumx.nvidia.com,resources=spectrumxrailpoolconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=spectrumx.nvidia.com,resources=spectrumxrailpoolconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=sriovnetworknodepolicies,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SpectrumXRailPoolConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *SpectrumXRailPoolConfigHostFlowsReconciler) Reconcile(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig) (ctrl.Result, error) {
	log := log.FromContext(ctx)

	// Get the SriovNetworkNodePolicy
	nsn := types.NamespacedName{Namespace: rpc.Namespace, Name: rpc.Spec.SriovNetworkNodePolicyRef}
	sriovNetworkNodePolicy := &sriovv1.SriovNetworkNodePolicy{}

	if err := r.Client.Get(ctx, nsn, sriovNetworkNodePolicy); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get SriovNetworkNodePolicy %s: %v", nsn, err)
	}

	if len(sriovNetworkNodePolicy.Spec.NicSelector.PfNames) < 1 {
		return ctrl.Result{}, fmt.Errorf("expected 1 PF name in SriovNetworkNodePolicy, got %d", len(sriovNetworkNodePolicy.Spec.NicSelector.PfNames))
	}

	pfName := sriovNetworkNodePolicy.Spec.NicSelector.PfNames[0]

	bridgeName, err := r.flows.GetBridgeNameFromPortName(pfName)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get bridge name for port %s: %v", pfName, err)
	}

	// TODO: Add a cleanup mechanism.
	// Because we have no finalizer for the SpectrumXRailPoolConfig, we might miss the deletion event
	// and not cleanup the flows.
	if rpc.DeletionTimestamp != nil {
		// Delete the flows
		return ctrl.Result{}, r.flows.DeleteFlowsByCookie(bridgeName, hostFlowsCookie)
	}

	switch rpc.Spec.MultiplaneMode {
	case "none", "swplb":
		if err = r.flows.AddSoftwareMultiplaneFlows(bridgeName, hostFlowsCookie, pfName); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add software multiplane flows: %v", err)
		}
	case "hwplb":
		pfNames := sriovNetworkNodePolicy.Spec.NicSelector.PfNames

		// TODO: remove when muti-uplink bridges are supported
		additionalUplinksStr := sriovNetworkNodePolicy.Annotations["spectrumx.nvidia.com/bridge-additional-uplinks"]

		if additionalUplinksStr != "" {
			pfNames = append(pfNames, strings.Split(additionalUplinksStr, ",")...)
		}

		if err := r.flows.AddHardwareMultiplaneGroups(bridgeName, pfNames); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add hardware multiplane groups: %v", err)
		}

		if err = r.flows.AddHardwareMultiplaneFlows(bridgeName, hostFlowsCookie, pfNames); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to add hardware multiplane flows: %v", err)
		}
	default:
		log.Info("Unhandled multiplane mode", "mode", rpc.Spec.MultiplaneMode)
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SpectrumXRailPoolConfigHostFlowsReconciler) SetupWithManager(
	mgr ctrl.Manager,
	nodeName string,
	ovsWatcher <-chan event.GenericEvent,
) error {
	railListerHandler := handler.EnqueueRequestsFromMapFunc(NewNodeRailLister(r.Client, nodeName).ListRailPoolConfigsForNode)

	nodeNameFilter := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return nodeName == obj.GetName()
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.SpectrumXRailPoolConfig{}). // TODO: only reconcile objects that are related to this node
		Watches(
			&v1.Node{},
			railListerHandler,
			builder.WithPredicates(
				predicate.And(predicate.LabelChangedPredicate{}, nodeNameFilter),
			),
		).
		WatchesRawSource(source.Channel(ovsWatcher, railListerHandler)).
		Named("spectrumxrailpoolconfig-host-flows").Complete(reconcile.AsReconciler[*v1alpha1.SpectrumXRailPoolConfig](r.Client, r))
}

type nodeRailLister struct {
	client   client.Client
	nodeName string
}

func NewNodeRailLister(client client.Client, nodeName string) *nodeRailLister {
	return &nodeRailLister{client: client, nodeName: nodeName}
}

func (r *nodeRailLister) ListRailPoolConfigsForNode(ctx context.Context, _ client.Object) []reconcile.Request {
	logger := log.FromContext(ctx)

	node := &v1.Node{}
	if err := r.client.Get(ctx, types.NamespacedName{Name: r.nodeName}, node); err != nil {
		return nil
	}

	list := &v1alpha1.SpectrumXRailPoolConfigList{}
	if err := r.client.List(ctx, list); err != nil {
		logger.Error(err, "failed to list SpectrumXRailPoolConfigs")
		return nil
	}

	requests := make([]reconcile.Request, 0)

	for _, rpc := range list.Items {
		// Get the SriovNetworkNodePolicy
		nsn := types.NamespacedName{Namespace: rpc.Namespace, Name: rpc.Spec.SriovNetworkNodePolicyRef}
		snnp := sriovv1.SriovNetworkNodePolicy{}

		if err := r.client.Get(ctx, nsn, &snnp); err != nil {
			logger.Error(err, "failed to get SriovNetworkNodePolicy", "nsn", nsn)
			continue
		}

		// If the SriovNetworkNodePolicy selects this node, add the SpectrumXRailPoolConfig to the requests
		if labels.Set(snnp.Spec.NodeSelector).AsSelector().Matches(labels.Set(node.Labels)) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Namespace: rpc.Namespace, Name: rpc.Name},
			})
		}
	}

	return requests
}
