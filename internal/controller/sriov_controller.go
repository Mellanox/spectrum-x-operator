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
	"time"

	apiErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"

	sriovnetworkv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"
)

const (
	spectrumXSRIOVControllerName = "spectrumxsriovcontroller"
	ovsNetworkType               = "OVSNetwork"
	ovsNetworkNamespace          = "default"
	ovsDataPathType              = "netdev"
	ovsNetworkInterfaceType      = "dpdk"
	sriovNodePolicyType          = "SriovNetworkNodePolicy"
	sriovNodePolicyNodeSelector  = "node-role.kubernetes.io/worker"
	spectrumXSRIOVFinalizer      = "spectrumx.nvidia.com/sriovnetwork-finalizer"
)

// SRIOVReconciler reconciles Spectrum-X-Config from ConfigMap
type SRIOVReconciler struct {
	client.Client
	Scheme             *runtime.Scheme
	ConfigMapNamespace string
	ConfigMapName      string
	SriovObjNamespace  string
}

//+kubebuilder:rbac:groups="",resources=configmaps/finalizers,verbs=update
//+kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=ovsnetworks,verbs=create;get;list;patch;delete;deletecollection
//+kubebuilder:rbac:groups=sriovnetwork.openshift.io,resources=sriovnetworknodepolicies,verbs=create;get;list;patch;delete;deletecollection

func (r *SRIOVReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logr := log.FromContext(ctx)
	logr.Info("Reconciling Spectrum-X ConfigMap")
	// Fetch the ConfigMap
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, req.NamespacedName, cm)
	if err != nil {
		if apiErrors.IsNotFound(err) {
			// Return early if the object is not found.
			return ctrl.Result{}, nil
		}
	}
	// Handle deletion reconciliation loop.
	if !cm.ObjectMeta.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, cm)
	}

	spectrumXConfig, err := config.ParseConfig(cm.Data[config.ConfigMapKey])
	if err != nil {
		logr.Info("Failed to parse Spectrum-X ConfigMap Data")
		return ctrl.Result{}, err
	}
	logr.Info("Parsed Spectrum-X Config", "config", spectrumXConfig)

	// Add finalizer if not set.
	if !controllerutil.ContainsFinalizer(cm, spectrumXSRIOVFinalizer) {
		logr.Info("Adding finalizer to configmap")
		controllerutil.AddFinalizer(cm, spectrumXSRIOVFinalizer)
		cm.ObjectMeta.ManagedFields = nil
		cm.SetGroupVersionKind(schema.GroupVersionKind{Group: "", Version: "v1", Kind: "ConfigMap"})
		if err := r.Client.Patch(ctx, cm, client.Apply, client.ForceOwnership, client.FieldOwner(spectrumXSRIOVControllerName)); err != nil {
			return ctrl.Result{}, fmt.Errorf("error while adding finalizer to configmap %w", err)
		}
		return ctrl.Result{}, nil
	}

	err = r.reconcileSRIOVNetworkNodePolicy(ctx, spectrumXConfig)
	if err != nil {
		logr.Info("Failed to reconcile SRIOVNetworkNodePolicy")
		return ctrl.Result{}, err
	}

	err = r.reconcileOVSNetwork(ctx, spectrumXConfig)
	if err != nil {
		logr.Info("Failed to reconcile OVSNetwork")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// reconcileOVSNetwork generates OVSNetworks according to defined Rails
func (r *SRIOVReconciler) reconcileOVSNetwork(ctx context.Context, cfg *config.Config) error {
	logr := log.FromContext(ctx)
	for _, rail := range cfg.SpectrumXNetworks.Rails {
		network := r.generateOVSNetwork(&rail, cfg.SpectrumXNetworks.MTU)
		logr.Info("Creating OVSNetwork", "name", network.Name)
		if err := r.Client.Patch(ctx, network, client.Apply, client.ForceOwnership, client.FieldOwner(spectrumXSRIOVControllerName)); err != nil {
			return fmt.Errorf("error while patching %s %s: %w", network.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(network), err)
		}
	}
	return nil
}

// reconcileSRIOVNetworkNodePolicy generates SRIOVNetworkNodePolicy according to defined Rails
func (r *SRIOVReconciler) reconcileSRIOVNetworkNodePolicy(ctx context.Context, cfg *config.Config) error {
	logr := log.FromContext(ctx)
	for _, railDevMapping := range cfg.RailDeviceMapping {
		network := r.generateSRIOVNetworkNodePolicy(railDevMapping.RailName, railDevMapping.DevName, cfg.SpectrumXNetworks.MTU)
		logr.Info("Creating SRIOVNetworkNodePolicy", "name", network.Spec)
		if err := r.Client.Patch(ctx, network, client.Apply, client.ForceOwnership, client.FieldOwner(spectrumXSRIOVControllerName)); err != nil {
			return fmt.Errorf("error while patching %s %s: %w", network.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(network), err)
		}
	}
	return nil
}

// generateOVSNetwork generates a OVSNetwork object for the given Rail
func (r *SRIOVReconciler) generateOVSNetwork(rail *config.Rail, mtu uint) *sriovnetworkv1.OVSNetwork {
	network := &sriovnetworkv1.OVSNetwork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rail.Name,
			Namespace: r.SriovObjNamespace,
			Labels: map[string]string{
				spectrumXSRIOVControllerName: "",
			},
		},
		Spec: sriovnetworkv1.OVSNetworkSpec{
			NetworkNamespace: ovsNetworkNamespace,
			ResourceName:     rail.Name,
			IPAM:             fmt.Sprintf(`{"type": "nv-ipam", "poolName": "%s", "poolType": "cidrpool"}`, rail.Name),
			MTU:              mtu,
			InterfaceType:    ovsNetworkInterfaceType,
		},
	}

	network.ObjectMeta.ManagedFields = nil
	network.SetGroupVersionKind(sriovnetworkv1.GroupVersion.WithKind(ovsNetworkType))
	return network
}

// generateSRIOVNetworkNodePolicy generates a SRIOVNetworkNodePolicy object for the given Rail
func (r *SRIOVReconciler) generateSRIOVNetworkNodePolicy(railName, deviceName string, mtu uint) *sriovnetworkv1.SriovNetworkNodePolicy {
	nicSelector := &sriovnetworkv1.SriovNetworkNicSelector{
		PfNames: []string{deviceName},
	}
	nodeSelector := map[string]string{
		sriovNodePolicyNodeSelector: "",
	}
	bridge := &sriovnetworkv1.Bridge{
		OVS: &sriovnetworkv1.OVSConfig{
			Bridge: sriovnetworkv1.OVSBridgeConfig{
				DatapathType: ovsDataPathType,
			},
			Uplink: sriovnetworkv1.OVSUplinkConfig{
				Interface: sriovnetworkv1.OVSInterfaceConfig{
					Type: ovsNetworkInterfaceType,
				},
			},
		},
	}
	nodePolicy := &sriovnetworkv1.SriovNetworkNodePolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      railName,
			Namespace: r.SriovObjNamespace,
			Labels: map[string]string{
				spectrumXSRIOVControllerName: "",
			},
		},
		// According to NVIDIA Spectrum-X architecture we need only VF per PF to be created
		// which would be used for GPU to GPU traffic so IsRDMA flag is required
		// NOTE: Temporary ignore MTU type conversion until we address it in SR-IOV Network Operator
		//nolint:gosec
		Spec: sriovnetworkv1.SriovNetworkNodePolicySpec{
			ResourceName: railName,
			Mtu:          int(mtu),
			NumVfs:       1,
			NicSelector:  *nicSelector,
			NodeSelector: nodeSelector,
			IsRdma:       true,
			EswitchMode:  "switchdev",
			Bridge:       *bridge,
		},
	}

	nodePolicy.ObjectMeta.ManagedFields = nil
	nodePolicy.SetGroupVersionKind(sriovnetworkv1.GroupVersion.WithKind(sriovNodePolicyType))
	return nodePolicy
}

// reconcileDelete delete created OVS Network remove finalizer
func (r *SRIOVReconciler) reconcileDelete(ctx context.Context, cm *corev1.ConfigMap) (ctrl.Result, error) {
	logr := log.FromContext(ctx)
	logr.Info("Reconciling delete")
	if err := r.deleteOVSNetworks(ctx); err != nil {
		return ctrl.Result{}, err
	}
	if err := r.deleteSRIOVNetworkNodePolicies(ctx); err != nil {
		return ctrl.Result{}, err
	}
	logr.Info("Removing finalizer")
	controllerutil.RemoveFinalizer(cm, spectrumXSRIOVFinalizer)
	cm.ObjectMeta.ManagedFields = nil
	if err := r.Client.Patch(ctx, cm, client.Apply, client.ForceOwnership, client.FieldOwner(spectrumXSRIOVControllerName)); err != nil {
		return ctrl.Result{}, fmt.Errorf("error while removing finalizer to configmap %w", err)
	}
	return ctrl.Result{}, nil
}

// deleteSRIOVNetworkNodePolocies deletes all the SRIOVNetworkNodePolocies created by controller
func (r *SRIOVReconciler) deleteSRIOVNetworkNodePolicies(ctx context.Context) error {
	p := &unstructured.Unstructured{}
	p.SetGroupVersionKind(sriovnetworkv1.GroupVersion.WithKind(sriovNodePolicyType))
	if err := r.Client.DeleteAllOf(ctx, p, client.InNamespace(r.SriovObjNamespace), client.MatchingLabels{
		spectrumXSRIOVControllerName: "",
	}); err != nil {
		return fmt.Errorf("error while removing all %s: %w", p.GetObjectKind().GroupVersionKind().String(), err)
	}
	// Wait for all instances to be deleted
	timeout := 30 * time.Second
	interval := 1 * time.Second
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(sriovnetworkv1.GroupVersion.WithKind(sriovNodePolicyType))

		if err := r.Client.List(ctx, list, client.InNamespace(r.SriovObjNamespace), client.MatchingLabels{
			spectrumXSRIOVControllerName: "",
		}); err != nil {
			return false, fmt.Errorf("error while listing %s: %w", list.GetObjectKind().GroupVersionKind().String(), err)
		}

		// If no resources remain, deletion is complete
		return len(list.Items) == 0, nil
	})
}

// deleteOVSNetworks deletes all the OVS Networks created by controller
func (r *SRIOVReconciler) deleteOVSNetworks(ctx context.Context) error {
	p := &unstructured.Unstructured{}
	p.SetGroupVersionKind(sriovnetworkv1.GroupVersion.WithKind(ovsNetworkType))
	if err := r.Client.DeleteAllOf(ctx, p, client.InNamespace(r.SriovObjNamespace), client.MatchingLabels{
		spectrumXSRIOVControllerName: "",
	}); err != nil {
		return fmt.Errorf("error while removing all %s: %w", p.GetObjectKind().GroupVersionKind().String(), err)
	}
	// Wait for all instances to be deleted
	timeout := 30 * time.Second
	interval := 1 * time.Second
	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		list := &unstructured.UnstructuredList{}
		list.SetGroupVersionKind(sriovnetworkv1.GroupVersion.WithKind(ovsNetworkType))

		if err := r.Client.List(ctx, list, client.InNamespace(r.SriovObjNamespace), client.MatchingLabels{
			spectrumXSRIOVControllerName: "",
		}); err != nil {
			return false, fmt.Errorf("error while listing %s: %w", list.GetObjectKind().GroupVersionKind().String(), err)
		}

		// If no resources remain, deletion is complete
		return len(list.Items) == 0, nil
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *SRIOVReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named("SRIOVReconciler").
		For(&corev1.ConfigMap{}).
		Complete(r)
}
