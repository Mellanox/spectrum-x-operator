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
	"hash/fnv"

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// FlowReconciler reconciles a Pod object
type FlowReconciler struct {
	NodeName string
	client.Client
	Exec       exec.API
	Flows      FlowsAPI
	OVSWatcher <-chan event.TypedGenericEvent[struct{}]
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups="",resources=events,verbs=get;create;patch;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Pod object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.17.3/pkg/reconcile
func (r *FlowReconciler) Reconcile(ctx context.Context, pod *corev1.Pod) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	logr.Info("reconcile", "namespace", pod.Namespace, "name", pod.Name)

	if pod.DeletionTimestamp != nil {
		logr.Info("pod is being deleted, deleting flows if needed")
		if err := r.Flows.DeletePodRailFlows(GenerateUint64FromString(string(pod.UID)), string(pod.UID)); err != nil {
			logr.Error(err, "failed to delete flows")
		}
		return ctrl.Result{}, nil
	}

	if pod.Annotations == nil {
		return reconcile.Result{}, nil
	}

	networkStatus := []netdefv1.NetworkStatus{}
	// no need to retry in case of error, it will be reconciled again if someone fix the annotation
	if err := json.Unmarshal([]byte(pod.Annotations[netdefv1.NetworkStatusAnnot]), &networkStatus); err != nil {
		logr.Error(err, "failed to unmarshal network status")
		return ctrl.Result{}, nil
	}

	relevantNetworkStatus := []netdefv1.NetworkStatus{}
	for _, ns := range networkStatus {
		// TODO: need to find a better way to identify releavnt interfaces
		if ns.DeviceInfo != nil && ns.DeviceInfo.Type == "pci" {
			relevantNetworkStatus = append(relevantNetworkStatus, ns)
		}
	}

	if len(relevantNetworkStatus) == 0 {
		return reconcile.Result{}, nil
	}

	logr.Info(fmt.Sprintf("pod network status: %+v", relevantNetworkStatus))

	return ctrl.Result{}, r.handlePodFlows(ctx, pod, relevantNetworkStatus)
}

func (r *FlowReconciler) handlePodFlows(ctx context.Context, pod *corev1.Pod, relevantNetworkStatus []netdefv1.NetworkStatus) error {
	logr := log.FromContext(ctx)

	cookie := GenerateUint64FromString(string(pod.UID))

	var errs error

	for _, ns := range relevantNetworkStatus {
		rep, err := r.getIfaceRep(ns.Interface, pod.UID)
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to get rep for interface %s", ns.Interface))
			errs = multierr.Append(errs, err)
			continue
		}

		bridge, err := r.repToBridge(rep)
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to get bridge for interface %s", ns.Interface))
			errs = multierr.Append(errs, err)
			continue
		}

		isManaged, err := r.Flows.IsBridgeManagedByRailCNI(bridge, string(pod.UID))
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to check if bridge %s is managed by rail cni", bridge))
			errs = multierr.Append(errs, err)
			continue
		}

		if !isManaged {
			logr.Info(fmt.Sprintf("skipping bridge [%s], not managed by rail cni", bridge))
			continue
		}

		if err = r.Flows.AddPodRailFlows(cookie, rep, bridge, ns.IPs[0], ns.Mac); err != nil {
			logr.Error(err, fmt.Sprintf("failed to add flows to rail %s", ns.Interface))
			errs = multierr.Append(errs, err)
			continue
		}
	}

	return errs
}

func (r *FlowReconciler) getIfaceRep(iface string, podUID types.UID) (string, error) {
	return r.Exec.Execute(fmt.Sprintf(`ovs-vsctl --no-heading --columns=name find Port `+
		`external_ids:contIface=%s external_ids:contPodUid=%s`,
		iface, podUID))
}

func (r *FlowReconciler) repToBridge(rep string) (string, error) {
	br, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl iface-to-br %s", rep))
	if err != nil {
		return "", err
	}
	return br, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *FlowReconciler) SetupWithManager(mgr ctrl.Manager) error {
	isPodRelevant := func(pod *corev1.Pod) bool {
		if pod.Spec.NodeName != r.NodeName {
			return false
		}

		if pod.Annotations == nil {
			return false
		}

		if _, ok := pod.Annotations[netdefv1.NetworkStatusAnnot]; !ok {
			return false
		}

		return true
	}

	hostPodMapFunc := handler.TypedEnqueueRequestsFromMapFunc(func(ctx context.Context, obj struct{}) []reconcile.Request {
		pods := &corev1.PodList{}
		if err := r.List(ctx, pods, client.MatchingFields{"spec.nodeName": r.NodeName}); err != nil {
			log.Log.Error(err, "FlowReconciler failed to list pods")
			return nil
		}

		requests := []reconcile.Request{}
		for _, pod := range pods.Items {
			if isPodRelevant(&pod) {
				requests = append(requests, reconcile.Request{
					NamespacedName: client.ObjectKey{
						Namespace: pod.Namespace,
						Name:      pod.Name,
					},
				})
			}
		}

		return requests
	})

	return ctrl.NewControllerManagedBy(mgr).
		Named("FlowReconciler").
		For(&corev1.Pod{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			pod, ok := object.(*corev1.Pod)
			if !ok {
				return false
			}

			return isPodRelevant(pod)
		})).
		WatchesRawSource(source.Channel(r.OVSWatcher, hostPodMapFunc)).
		Complete(
			reconcile.AsReconciler(r.Client, r),
		)
}

// GenerateUint64FromString hashes a string and returns a uint64
func GenerateUint64FromString(input string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(input))
	return h.Sum64()
}
