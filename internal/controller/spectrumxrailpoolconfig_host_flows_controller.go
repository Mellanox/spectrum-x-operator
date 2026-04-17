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

	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/apply"
	sriovhosttypes "github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/host/types"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/render"
	"k8s.io/apimachinery/pkg/api/equality"
	uns "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	kscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha1"
	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	"github.com/Mellanox/spectrum-x-operator/pkg/lib"
)

const (
	hostFlowsCookie uint64 = 0x2
	xplaneBridge           = "br-xplane"
)

const SpectrumXRailPoolConfigControllerName = "SpectrumXRailPoolConfigController"

const (
	sriovNodePolicyType     = "SriovNetworkNodePolicy"
	sriovNetworkPoolConfig  = "SriovNetworkPoolConfig"
	sriovOVSNetworkType     = "OVSNetwork"
	ovsDataPathType         = "netdev"
	ovsNetworkInterfaceType = "dpdk"
)

const (
	DaemonSet      = "DaemonSet"
	Role           = "Role"
	RoleBinding    = "RoleBinding"
	ServiceAccount = "ServiceAccount"
)

const (
	finalizerName  = "spectrumx.nvidia.com/spectrumxrailpoolconfig"
	labelOwnerName = "spectrumx.nvidia.com/owner-name"
)

const (
	rdmaQoSToS = 96
	rdmaQoSTC  = 102
)

// SpectrumXRailPoolConfigHostFlowsReconciler reconciles a SpectrumXRailPoolConfig object
type SpectrumXRailPoolConfigHostFlowsReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	flows    FlowsAPI
	exec     exec.API
	bridge   sriovhosttypes.BridgeInterface
	nodeName string
}

func NewSpectrumXRailPoolConfigHostFlowsReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	flows FlowsAPI,
	execAPI exec.API,
	bridge sriovhosttypes.BridgeInterface,
	nodeName string,
) *SpectrumXRailPoolConfigHostFlowsReconciler {
	return &SpectrumXRailPoolConfigHostFlowsReconciler{
		Client:   client,
		Scheme:   scheme,
		flows:    flows,
		exec:     execAPI,
		bridge:   bridge,
		nodeName: nodeName,
	}
}

// +kubebuilder:rbac:groups=spectrumx.nvidia.com,resources=spectrumxrailpoolconfigs,verbs=get;list;watch
// +kubebuilder:rbac:groups=spectrumx.nvidia.com,resources=spectrumxrailpoolconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=sriovnetworknodepolicies,verbs=create;patch;get;list;watch;update;delete
// +kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=sriovnetworkpoolconfigs,verbs=create;patch;get;list;watch;update;delete
// +kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=ovsnetworks,verbs=create;patch;get;list;watch;update;delete
// +kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=sriovnetworknodestates,verbs=get;list;watch
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
	err := r.doReconcile(ctx, rpc)
	if apierrors.IsConflict(err) {
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, err
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) doReconcile(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig) error {
	log := log.FromContext(ctx)

	if !controllerutil.ContainsFinalizer(rpc, finalizerName) {
		patch := client.MergeFrom(rpc.DeepCopy())
		controllerutil.AddFinalizer(rpc, finalizerName)
		return r.Patch(ctx, rpc, patch)
	}

	if !rpc.DeletionTimestamp.IsZero() {
		xplaneDeleted := false
		for _, rt := range rpc.Spec.RailTopology {
			if err := r.deleteRailTopologyResources(ctx, rpc.Namespace, rt.Name); err != nil {
				log.Error(err, "failed to delete rail topology resources", "rail topology", rt)
				return err
			}
			if err := r.cleanupXPlaneBridges(ctx, &rt, !xplaneDeleted); err != nil {
				log.Error(err, "failed to cleanup xplane bridges", "rail topology", rt)
				return err
			}
			xplaneDeleted = true
		}
		patch := client.MergeFrom(rpc.DeepCopy())
		controllerutil.RemoveFinalizer(rpc, finalizerName)
		return r.Patch(ctx, rpc, patch)
	}

	if rpc.Status.SyncStatus != v1alpha1.SyncStatusInProgress {
		patch := client.MergeFrom(rpc.DeepCopy())
		rpc.Status.SyncStatus = v1alpha1.SyncStatusInProgress
		if err := r.Status().Patch(ctx, rpc, patch); err != nil {
			return fmt.Errorf("failed to set SyncStatus to InProgress: %w", err)
		}
	}

	if len(rpc.Spec.RailTopology) < 1 {
		return fmt.Errorf("expected one or more rail topologies to be specified")
	}

	ownerLabels := map[string]string{labelOwnerName: rpc.Name}
	poolConfig := r.generateSRIOVNetworkPoolConfig(rpc)
	poolConfig.SetGroupVersionKind(sriovv1.GroupVersion.WithKind(sriovNetworkPoolConfig))
	poolConfig.Labels = ownerLabels
	if err := r.Patch(ctx, poolConfig, client.Apply, client.ForceOwnership, client.FieldOwner(SpectrumXRailPoolConfigControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", poolConfig.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(poolConfig), err)
	}

	for _, rt := range rpc.Spec.RailTopology {
		err := r.reconcileRailTopology(ctx, rpc, rt)
		if err != nil {
			log.Error(err, "failed to reconcile rail topology", "rail topology", rt)
			return err
		}
	}

	if err := r.deleteRemovedRailTopologies(ctx, rpc); err != nil {
		log.Error(err, "failed to delete removed rail topologies")
		return err
	}

	if err := r.updateSyncStatus(ctx, rpc); err != nil {
		log.Error(err, "failed to update sync status")
		return err
	}

	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) reconcileRailTopology(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig, rt v1alpha1.RailTopology) error {
	spec := &rpc.Spec
	namespace := rpc.Namespace
	if len(rt.NicSelector.PfNames) == 0 {
		return fmt.Errorf("no PF names are specified in rail topology")
	}

	ownerLabels := map[string]string{labelOwnerName: rpc.Name}

	var policy *sriovv1.SriovNetworkNodePolicy
	if len(rt.NicSelector.PfNames) == 1 {
		// sw plb or no multiplane
		policy = r.generateSRIOVNetworkNodePolicy(spec, &rt, false, namespace)
	} else {
		// hw multiplane
		policy = r.generateSRIOVNetworkNodePolicy(spec, &rt, true, namespace)
	}
	policy.Labels = ownerLabels

	if err := r.Patch(ctx, policy, client.Apply, client.ForceOwnership, client.FieldOwner(SpectrumXRailPoolConfigControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", policy.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(policy), err)
	}

	addBridge := len(rt.NicSelector.PfNames) == 1
	ovsNetwork := r.generateOVSNetwork(spec, &rt, addBridge, namespace)
	ovsNetwork.SetGroupVersionKind(sriovv1.GroupVersion.WithKind(sriovOVSNetworkType))
	ovsNetwork.Labels = ownerLabels
	if err := r.Patch(ctx, ovsNetwork, client.Apply, client.ForceOwnership, client.FieldOwner(SpectrumXRailPoolConfigControllerName)); err != nil {
		return fmt.Errorf("error while patching %s %s: %w", ovsNetwork.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(ovsNetwork), err)
	}

	if len(rt.NicSelector.PfNames) > 1 {
		if err := r.configureXPlane(ctx, rpc, spec, &rt, namespace); err != nil {
			return fmt.Errorf("failed to configure xplane for rail topology %s: %w", rt.Name, err)
		}
	}

	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) configureXPlane(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig, spec *v1alpha1.SpectrumXRailPoolConfigSpec, rt *v1alpha1.RailTopology, namespace string) error {
	nodeList := &v1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabels(spec.NodeSelector)); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	var localNodeState *sriovv1.SriovNetworkNodeState

	for _, node := range nodeList.Items {
		nodeState := &sriovv1.SriovNetworkNodeState{}
		nsn := types.NamespacedName{Name: node.Name, Namespace: namespace}
		if err := r.Get(ctx, nsn, nodeState); err != nil {
			if apierrors.IsNotFound(err) {
				return nil
			}
			return fmt.Errorf("failed to get SriovNetworkNodeState for node %s: %w", node.Name, err)
		}
		if nodeState.Status.SyncStatus != v1alpha1.SyncStatusSucceeded {
			return nil
		}
		if node.Name == r.nodeName {
			localNodeState = nodeState
		}
	}

	if localNodeState == nil {
		// local node is not part of this pool
		return nil
	}

	err := r.createXPlaneBridges(ctx, rt, localNodeState)
	if err != nil {
		return fmt.Errorf("failed to create X-Plane bridges: %w", err)
	}
	if err := r.deployXplane(ctx, r.Client, rpc, r.Scheme, namespace, config.FromEnv()); err != nil {
		return fmt.Errorf("failed to deploy xplane: %w", err)
	}

	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) deployXplane(ctx context.Context, client client.Client, poolConfig *v1alpha1.SpectrumXRailPoolConfig,
	scheme *runtime.Scheme, namespace string, cfg *config.OperatorConfig,
) error {
	logger := log.Log.WithName("deployXplane")
	logger.Info("Deploying Xplane Service")
	data := render.MakeRenderData()
	data.Data["Namespace"] = namespace
	data.Data["ImagePullSecrets"] = cfg.ImagePullSecrets
	data.Data["Image"] = fmt.Sprintf("%s/%s:%s", cfg.XPlaneRepository, cfg.XPlaneImage, cfg.XPlaneVersion)

	objs, err := render.RenderDir("manifests/state-xplane", &data)
	if err != nil {
		return fmt.Errorf("failed to render xplane manifests: %w", err)
	}
	// Sync DaemonSets
	for _, obj := range objs {
		err = syncDsObject(ctx, client, scheme, poolConfig, obj)
		if err != nil {
			logger.Error(err, "Couldn't sync SR-IoV daemons objects")
			return err
		}
	}
	return nil
}

func syncDsObject(ctx context.Context, client client.Client, scheme *runtime.Scheme, rpc *v1alpha1.SpectrumXRailPoolConfig, obj *uns.Unstructured) error {
	logger := log.Log.WithName("syncDsObject")
	kind := obj.GetKind()
	logger.V(1).Info("Start to sync Objects", "Kind", kind)
	switch kind {
	case ServiceAccount, Role, RoleBinding:
		if err := controllerutil.SetControllerReference(rpc, obj, scheme); err != nil {
			return err
		}
		if err := apply.ApplyObject(ctx, client, obj); err != nil {
			logger.Error(err, "Fail to sync", "Kind", kind)
			return err
		}
	case DaemonSet:
		ds := &appsv1.DaemonSet{}
		err := updateDaemonsetNodeSelector(obj, rpc.Spec.NodeSelector)
		if err != nil {
			logger.Error(err, "Fail to update DaemonSet's node selector")
			return err
		}
		err = scheme.Convert(obj, ds, nil)
		if err != nil {
			logger.Error(err, "Fail to convert to DaemonSet")
			return err
		}
		err = syncDaemonSet(ctx, client, scheme, rpc, ds)
		if err != nil {
			logger.Error(err, "Fail to sync DaemonSet", "Namespace", ds.Namespace, "Name", ds.Name)
			return err
		}
	}
	return nil
}

func syncDaemonSet(ctx context.Context, client client.Client, scheme *runtime.Scheme, rpc *v1alpha1.SpectrumXRailPoolConfig, in *appsv1.DaemonSet) error {
	logger := log.Log.WithName("syncDaemonSet")
	logger.V(1).Info("Start to sync DaemonSet", "Namespace", in.Namespace, "Name", in.Name)
	var err error

	if err = controllerutil.SetControllerReference(rpc, in, scheme); err != nil {
		return err
	}
	ds := &appsv1.DaemonSet{}
	err = client.Get(ctx, types.NamespacedName{Namespace: in.Namespace, Name: in.Name}, ds)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Created DaemonSet", in.Namespace, in.Name)
			err = client.Create(ctx, in)
			if err != nil {
				logger.Error(err, "Fail to create Daemonset", "Namespace", in.Namespace, "Name", in.Name)
				return err
			}
		} else {
			logger.Error(err, "Fail to get Daemonset", "Namespace", in.Namespace, "Name", in.Name)
			return err
		}
	} else {
		logger.V(1).Info("DaemonSet already exists, updating")
		// DeepDerivative checks for changes only comparing non-zero fields in the source struct.
		// This skips default values added by the api server.
		// References in https://github.com/kubernetes-sigs/kubebuilder/issues/592#issuecomment-625738183

		// Note(Adrianc): we check Equality of OwnerReference as we changed sriov-device-plugin owner ref
		// from SriovNetworkNodePolicy to SriovOperatorConfig, hence even if there is no change in spec,
		// we need to update the obj's owner reference.

		if equality.Semantic.DeepEqual(in.OwnerReferences, ds.OwnerReferences) &&
			equality.Semantic.DeepDerivative(in.Spec, ds.Spec) {
			logger.V(1).Info("Daemonset spec did not change, not updating")
			return nil
		}
		err = client.Update(ctx, in)
		if err != nil {
			logger.Error(err, "Fail to update DaemonSet", "Namespace", in.Namespace, "Name", in.Name)
			return err
		}
	}
	return nil
}

func updateDaemonsetNodeSelector(obj *uns.Unstructured, nodeSelector map[string]string) error {
	if len(nodeSelector) == 0 {
		return nil
	}

	ds := &appsv1.DaemonSet{}
	scheme := kscheme.Scheme
	err := scheme.Convert(obj, ds, nil)
	if err != nil {
		return fmt.Errorf("failed to convert Unstructured [%s] to DaemonSet: %v", obj.GetName(), err)
	}

	ds.Spec.Template.Spec.NodeSelector = nodeSelector

	err = scheme.Convert(ds, obj, nil)
	if err != nil {
		return fmt.Errorf("failed to convert DaemonSet [%s] to Unstructured: %v", obj.GetName(), err)
	}
	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) createXPlaneBridges(ctx context.Context, rt *v1alpha1.RailTopology, nodeState *sriovv1.SriovNetworkNodeState) error {
	// Build map of PF name -> interface info from node state
	ifaceByName := make(map[string]*sriovv1.InterfaceExt, len(nodeState.Status.Interfaces))
	for i := range nodeState.Status.Interfaces {
		iface := &nodeState.Status.Interfaces[i]
		ifaceByName[iface.Name] = iface
	}

	pfNames, err := lib.FilterNICs(ctx, rt.NicSelector.PfNames)
	if err != nil {
		return fmt.Errorf("failed to filter NICSs for specified selector: %w", err)
	}

	// Build desired bridge configs — one bridge per NIC
	// Use ConfigureBridges from sriov-network-operator to manage them via OVSDB.
	for idx, pfName := range pfNames {
		brName := fmt.Sprintf("br-%s-%d", pfName, idx)
		if _, err := r.exec.Execute(fmt.Sprintf(
			"ovs-vsctl --may-exist add-br %s -- set bridge %s datapath_type=%s",
			brName, brName, ovsDataPathType,
		)); err != nil {
			return fmt.Errorf("failed to create bridge %s: %w", brName, err)
		}
	}

	// Create br-xplane bridge. It connects all bridges via patch ports and
	// has no single physical uplink, so it cannot be created via ConfigureBridges.
	if _, err := r.exec.Execute(fmt.Sprintf(
		"ovs-vsctl --may-exist add-br %s -- set bridge %s datapath_type=%s",
		xplaneBridge, xplaneBridge, ovsDataPathType,
	)); err != nil {
		return fmt.Errorf("failed to create bridge %s: %w", xplaneBridge, err)
	}

	// Connect br-xplane to each bridge via patch ports and add VF representors.
	for idx, pfName := range pfNames {
		railBridge := fmt.Sprintf("br-%s-%d", pfName, idx)
		patchXplanePort := fmt.Sprintf("patch-xplane-to-%s-%d", pfName, idx)
		patchRailPort := fmt.Sprintf("patch-%s-%d-to-xplane", pfName, idx)

		if _, err := r.exec.Execute(fmt.Sprintf(
			"ovs-vsctl --may-exist add-port %s %s -- set interface %s type=patch options:peer=%s",
			xplaneBridge, patchXplanePort, patchXplanePort, patchRailPort,
		)); err != nil {
			return fmt.Errorf("failed to add patch port %s to bridge %s: %w", patchXplanePort, xplaneBridge, err)
		}

		if _, err := r.exec.Execute(fmt.Sprintf(
			"ovs-vsctl --may-exist add-port %s %s -- set interface %s type=patch options:peer=%s",
			railBridge, patchRailPort, patchRailPort, patchXplanePort,
		)); err != nil {
			return fmt.Errorf("failed to add patch port %s to bridge %s: %w", patchRailPort, railBridge, err)
		}
	}

	return nil
}

// cleanupXPlaneBridges tears down host OVS bridges created by createXPlaneBridges.
// deleteXplane controls whether br-xplane itself is deleted (only on the last rail topology).
func (r *SpectrumXRailPoolConfigHostFlowsReconciler) cleanupXPlaneBridges(ctx context.Context, rt *v1alpha1.RailTopology, deleteXplane bool) error {
	pfNames, err := lib.FilterNICs(ctx, rt.NicSelector.PfNames)
	if err != nil {
		return fmt.Errorf("failed to filter NICSs for specified selector: %w", err)
	}

	for idx, pfName := range pfNames {
		railBridge := fmt.Sprintf("br-%s-%d", pfName, idx)
		if _, err := r.exec.Execute(fmt.Sprintf(
			"ovs-vsctl --if-exists del-br %s", railBridge,
		)); err != nil {
			return fmt.Errorf("failed to delete bridge %s: %w", railBridge, err)
		}
	}
	if deleteXplane {
		if _, err := r.exec.Execute(fmt.Sprintf(
			"ovs-vsctl --if-exists del-br %s", xplaneBridge,
		)); err != nil {
			return fmt.Errorf("failed to delete bridge %s: %w", xplaneBridge, err)
		}
	}
	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) deleteRailTopologyResources(ctx context.Context, namespace, rtName string) error {
	policy := &sriovv1.SriovNetworkNodePolicy{ObjectMeta: metav1.ObjectMeta{Name: rtName, Namespace: namespace}}
	if err := r.Delete(ctx, policy); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete SriovNetworkNodePolicy %s/%s: %w", namespace, rtName, err)
	}

	poolConfig := &sriovv1.SriovNetworkPoolConfig{ObjectMeta: metav1.ObjectMeta{Name: rtName, Namespace: namespace}}
	if err := r.Delete(ctx, poolConfig); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete SriovNetworkPoolConfig %s/%s: %w", namespace, rtName, err)
	}

	ovsNetwork := &sriovv1.OVSNetwork{ObjectMeta: metav1.ObjectMeta{Name: rtName, Namespace: namespace}}
	if err := r.Delete(ctx, ovsNetwork); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("failed to delete OVSNetwork %s/%s: %w", namespace, rtName, err)
	}

	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) deleteRemovedRailTopologies(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig) error {
	currentTopologies := make(map[string]struct{}, len(rpc.Spec.RailTopology))
	for _, rt := range rpc.Spec.RailTopology {
		currentTopologies[rt.Name] = struct{}{}
	}

	policyList := &sriovv1.SriovNetworkNodePolicyList{}
	if err := r.List(ctx, policyList,
		client.InNamespace(rpc.Namespace),
		client.MatchingLabels{labelOwnerName: rpc.Name},
	); err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to list SriovNetworkNodePolicies: %w", err)
	}

	for _, policy := range policyList.Items {
		if _, exists := currentTopologies[policy.Name]; !exists {
			if err := r.deleteRailTopologyResources(ctx, rpc.Namespace, policy.Name); err != nil {
				return err
			}
			rt := v1alpha1.RailTopology{
				Name: policy.Name,
				NicSelector: v1alpha1.NicSelector{
					PfNames: policy.Spec.NicSelector.PfNames,
				},
			}
			if err := r.cleanupXPlaneBridges(ctx, &rt, false); err != nil {
				return fmt.Errorf("failed to cleanup xplane bridges for removed rail topology %s: %w", policy.Name, err)
			}
		}
	}

	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) updateSyncStatus(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig) error {
	nodeList := &v1.NodeList{}
	if err := r.List(ctx, nodeList, client.MatchingLabels(rpc.Spec.NodeSelector)); err != nil {
		return fmt.Errorf("failed to list nodes: %w", err)
	}

	newStatus := v1alpha1.SyncStatusSucceeded
	for _, node := range nodeList.Items {
		nodeState := &sriovv1.SriovNetworkNodeState{}
		nsn := types.NamespacedName{Name: node.Name, Namespace: rpc.Namespace}
		if err := r.Get(ctx, nsn, nodeState); err != nil {
			if apierrors.IsNotFound(err) {
				newStatus = v1alpha1.SyncStatusInProgress
				continue
			}
			return fmt.Errorf("failed to get SriovNetworkNodeState for node %s: %w", node.Name, err)
		}
		switch nodeState.Status.SyncStatus {
		case v1alpha1.SyncStatusFailed:
			return r.patchSyncStatus(ctx, rpc, v1alpha1.SyncStatusFailed)
		case v1alpha1.SyncStatusInProgress:
			newStatus = v1alpha1.SyncStatusInProgress
		}
	}

	return r.patchSyncStatus(ctx, rpc, newStatus)
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) patchSyncStatus(ctx context.Context, rpc *v1alpha1.SpectrumXRailPoolConfig, newStatus string) error {
	if rpc.Status.SyncStatus == newStatus && rpc.Status.ObservedGeneration == rpc.Generation {
		return nil
	}
	patch := client.MergeFrom(rpc.DeepCopy())
	rpc.Status.SyncStatus = newStatus
	rpc.Status.ObservedGeneration = rpc.Generation
	return r.Status().Patch(ctx, rpc, patch)
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) generateSRIOVNetworkPoolConfig(rpc *v1alpha1.SpectrumXRailPoolConfig) *sriovv1.SriovNetworkPoolConfig {
	nodeSelector := &metav1.LabelSelector{
		MatchLabels: rpc.Spec.NodeSelector,
	}

	nodePool := &sriovv1.SriovNetworkPoolConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rpc.Name,
			Namespace: rpc.Namespace,
		},
		Spec: sriovv1.SriovNetworkPoolConfigSpec{
			NodeSelector: nodeSelector,
			RdmaMode:     "exclusive",
			OvsHardwareOffloadConfig: sriovv1.OvsHardwareOffloadConfig{
				Name: "otherConfig",
				OvsConfig: map[string]string{
					"doca-init":          "true",
					"hw-offload":         "true",
					"hw-offload-ct-size": "0",
					"max-idle":           "300000",
				},
			},
		},
	}

	return nodePool
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) generateSRIOVNetworkNodePolicy(spec *v1alpha1.SpectrumXRailPoolConfigSpec, rt *v1alpha1.RailTopology, hardwarePLB bool, namespace string) *sriovv1.SriovNetworkNodePolicy {
	nicSelector := &sriovv1.SriovNetworkNicSelector{
		PfNames: rt.NicSelector.PfNames,
	}
	nodeSelector := spec.NodeSelector

	devlinkParams := []sriovv1.DevlinkParam{{Name: "esw_multiport", Value: "true", Cmode: "runtime", ApplyOn: "PF"}}

	nodePolicy := &sriovv1.SriovNetworkNodePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rt.Name,
			Namespace: namespace,
		},
		// According to NVIDIA Spectrum-X architecture we need only VF per PF to be created
		// which would be used for GPU to GPU traffic so IsRDMA flag is required
		Spec: sriovv1.SriovNetworkNodePolicySpec{
			ResourceName: rt.Name,
			Mtu:          rt.MTU,
			NumVfs:       spec.NumVfs,
			NicSelector:  *nicSelector,
			NodeSelector: nodeSelector,
			IsRdma:       true,
			EswitchMode:  "switchdev",
			DevlinkParams: sriovv1.DevlinkParams{
				Params: devlinkParams,
			},
		},
	}
	if !hardwarePLB {
		bridge := &sriovv1.Bridge{
			GroupingPolicy: "perPF",
			OVS: &sriovv1.OVSConfig{
				Bridge: sriovv1.OVSBridgeConfig{
					DatapathType: ovsDataPathType,
					// TODO: groupingPolicy=perPF option
				},
				Uplink: sriovv1.OVSUplinkConfig{
					Interface: sriovv1.OVSInterfaceConfig{
						Type:       ovsNetworkInterfaceType,
						MTURequest: &rt.MTU,
					},
				},
			},
		}

		nodePolicy.Spec.Bridge = *bridge
	}

	nodePolicy.ManagedFields = nil
	nodePolicy.SetGroupVersionKind(sriovv1.GroupVersion.WithKind(sriovNodePolicyType))
	return nodePolicy
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) generateOVSNetwork(spec *v1alpha1.SpectrumXRailPoolConfigSpec, rt *v1alpha1.RailTopology, addBridge bool, namespace string) *sriovv1.OVSNetwork {
	var ipam string
	switch {
	case rt.IPAM != "":
		ipam = rt.IPAM
	case rt.CidrPoolRef != "":
		ipam = fmt.Sprintf(`{"type": "nv-ipam","poolName": %q, "poolType": "cidrpool"}`, rt.CidrPoolRef)
	}

	ovsNetwork := &sriovv1.OVSNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rt.Name,
			Namespace: namespace,
		},
		Spec: sriovv1.OVSNetworkSpec{
			ResourceName:      rt.Name,
			InterfaceType:     ovsNetworkInterfaceType,
			NetworkNamespace:  spec.NetworkNamespace,
			MTU:               uint(rt.MTU), //nolint:gosec // MTU is always non-negative
			IPAM:              ipam,
			MetaPluginsConfig: fmt.Sprintf(`{"type": "rdma", "rdmaQoS": {"tos": %d,"tc": %d}}`, rdmaQoSToS, rdmaQoSTC),
		},
	}
	if addBridge {
		ovsNetwork.Spec.Bridge = fmt.Sprintf("br-%s", rt.Name)
	}
	return ovsNetwork
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
		for _, rt := range rpc.Spec.RailTopology {
			// Get the SriovNetworkNodePolicy
			nsn := types.NamespacedName{Namespace: rpc.Namespace, Name: rt.Name}
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
	}

	return requests
}
