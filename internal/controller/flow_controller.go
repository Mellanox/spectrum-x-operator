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

	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	"go.uber.org/multierr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// FlowReconciler reconciles a Pod object
type FlowReconciler struct {
	NodeName string
	client.Client
	Exec               exec.API
	ConfigMapNamespace string
	ConfigMapName      string
	Flows              FlowsAPI
}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=pods/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=core,resources=pods/finalizers,verbs=update

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

	cfg, err := r.getConfig(ctx)
	if err != nil {
		logr.Error(err, "failed to get network config")
		return err
	}

	hostConfig, err := r.getHostConfig(cfg)
	if err != nil {
		logr.Info("no config for this host", "host", r.NodeName, "error", err)
		return nil
	}

	logr.Info(fmt.Sprintf("host confg %v", hostConfig))

	cookie := GenerateUint64FromString(types.NamespacedName{Namespace: pod.Namespace, Name: pod.Name}.String())

	if pod.DeletionTimestamp != nil {
		logr.Info(fmt.Sprintf("pod %s/%s is being deleted, deleting flows", pod.Namespace, pod.Name))
	}

	var errs error

	for _, rail := range hostConfig.Rails {
		bridge, err := getBridgeToRail(&rail, cfg, r.Exec)
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to get bridge for rail %s", rail))
			errs = multierr.Append(errs, err)
			continue
		}

		logr.Info(fmt.Sprintf("Found bridge %s ip for rail %s", bridge, rail))

		if pod.DeletionTimestamp != nil {
			if err = r.Flows.DeletePodRailFlows(cookie, bridge); err != nil {
				logr.Error(err, fmt.Sprintf("failed to delete flows for rail %s", rail))
			}
			continue
		}

		// make sure there is a pod interface related to this bridge
		iface, ns, err := r.ifaceToRail(bridge, pod.UID, relevantNetworkStatus)
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to get relevant interface for bridge %s", bridge))
			errs = multierr.Append(errs, err)
			continue
		}
		if iface == "" {
			logr.Info(fmt.Sprintf("skipping rail [%s] for bridge [%s], couldn't find matching pod interface", rail, bridge))
			continue
		}

		if len(ns.IPs) == 0 {
			logr.Info(fmt.Sprintf("skipping rail [%s] for bridge [%s],"+
				"couldn't network inteface don't have ip address", rail, bridge))
			continue
		}

		logr.Info(fmt.Sprintf("Found interface [%s] from bridge [%s] for rail [%s]", iface, bridge, rail))

		if err = r.Flows.AddPodRailFlows(cookie, &rail, cfg, ns, bridge, iface); err != nil {
			logr.Error(err, fmt.Sprintf("failed to add flows to rail [%s]", rail))
			errs = multierr.Append(errs, err)
			continue
		}
	}

	return errs
}

func (r *FlowReconciler) ifaceToRail(bridge string, podUID types.UID, networkStatus []netdefv1.NetworkStatus) (string, *netdefv1.NetworkStatus, error) {
	var errs error
	for _, ns := range networkStatus {
		iface, err := r.Exec.Execute(fmt.Sprintf(`ovs-vsctl --no-heading --columns=name find Port `+
			`external_ids:contIface=%s external_ids:contPodUid=%s`,
			ns.Interface, podUID))
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}
		br, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl iface-to-br %s", iface))
		if err != nil {
			errs = multierr.Append(errs, err)
			continue
		}
		if br == bridge {
			return iface, &ns, nil
		}
	}

	return "", nil, errs
}

func getRailDevice(railName string, cfg *config.Config) (string, error) {
	for _, mapping := range cfg.RailDeviceMapping {
		if mapping.RailName == railName {
			return mapping.DevName, nil
		}
	}
	return "", fmt.Errorf("failed to find device for rail %s", railName)
}

func getBridgeToRail(rail *config.HostRail, cfg *config.Config, exec exec.API) (string, error) {
	railDevice, err := getRailDevice(rail.Name, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to get rail device for rail %s: %s", rail.Name, err)
	}

	bridge, err := exec.Execute(fmt.Sprintf("ovs-vsctl port-to-br %s", railDevice))
	if err != nil {
		return "", fmt.Errorf("failed to get bridge to rail %s, device %s: %s", rail.Name, railDevice, err)
	}

	return bridge, nil
}

func (r *FlowReconciler) getConfig(ctx context.Context) (*config.Config, error) {
	cfgMap := &corev1.ConfigMap{}
	logr := log.FromContext(ctx)
	if err := r.Get(ctx, types.NamespacedName{Namespace: r.ConfigMapNamespace, Name: r.ConfigMapName}, cfgMap); err != nil {
		logr.Error(err, "failed to get network configmap")
		return nil, err
	}

	cfg, err := config.ParseConfig(cfgMap.Data[config.ConfigMapKey])
	if err != nil {
		return nil, err
	}

	return cfg, nil
}

func (r *FlowReconciler) getHostConfig(cfg *config.Config) (*config.Host, error) {
	for _, host := range cfg.Hosts {
		if host.HostID == r.NodeName {
			return &host, nil
		}
	}

	return nil, fmt.Errorf("missing [%s] host toplogy", r.NodeName)
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			// don't ignore config map changes - it may be topology config map
			if _, ok := object.(*corev1.ConfigMap); ok {
				return true
			}

			pod, ok := object.(*corev1.Pod)
			if !ok {
				return false
			}

			return isPodRelevant(pod)
		})).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(
				func(ctx context.Context, _ client.Object) []reconcile.Request {
					pods := &corev1.PodList{}
					err := r.List(ctx, pods)
					if err != nil {
						return nil
					}

					// Create reconcile requests for the Pods
					var requests []reconcile.Request
					for _, pod := range pods.Items {
						if !isPodRelevant(&pod) {
							continue
						}
						requests = append(requests, reconcile.Request{
							NamespacedName: client.ObjectKey{
								Namespace: pod.Namespace,
								Name:      pod.Name,
							},
						})
					}

					return requests
				},
			),
			builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
				configMap, ok := obj.(*corev1.ConfigMap)
				if !ok {
					return false
				}
				return configMap.Name == r.ConfigMapName &&
					configMap.Namespace == r.ConfigMapNamespace
			})),
		).
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
