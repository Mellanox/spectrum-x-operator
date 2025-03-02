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
	"strings"
	"time"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
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
	Exec exec.API
}

var topologyConfigMap = types.NamespacedName{Namespace: "spcx-cni-system", Name: "config"}

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch
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

	cfg, err := r.getConfig(ctx)
	if err != nil {
		logr.Error(err, "failed to get network config")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	hostConfig, err := r.getHostConfig(cfg)
	if err != nil {
		logr.Error(err, "failed to get host network config")
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	logr.Info(fmt.Sprintf("host confg %v", hostConfig))

	result := ctrl.Result{}

	for _, rail := range hostConfig.Rails {
		bridge, err := r.getBridgeToRail(&rail, cfg)
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to get bridge for rail %s", rail))
			result = ctrl.Result{RequeueAfter: 5 * time.Second}
			continue
		}

		logr.Info(fmt.Sprintf("Found bridge %s ip for rail %s", bridge, rail))

		// make sure there is a pod interface related to this bridge
		iface, ns, err := r.ifaceToRail(bridge, relevantNetworkStatus)
		if err != nil {
			logr.Error(err, fmt.Sprintf("failed to get relevant interface for bridge %s", bridge))
			result = ctrl.Result{RequeueAfter: 5 * time.Second}
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

		if err = r.handleRailFlows(ctx, &rail, cfg, ns, bridge, iface); err != nil {
			logr.Error(err, fmt.Sprintf("failed to add flows to rail [%s]", rail))
			result = ctrl.Result{RequeueAfter: 5 * time.Second}
			continue
		}
	}

	return result, nil
}

func (r *FlowReconciler) handleRailFlows(ctx context.Context, rail *config.HostRail, cfg *config.Config, ns *netdefv1.NetworkStatus, bridge, iface string) error {
	// remove all flows (mainly to remove the default normal action flows)
	_, err := r.Exec.Execute(fmt.Sprintf("ovs-ofctl del-flows %s", bridge))
	if err != nil {
		return fmt.Errorf("failed to delete flows for bridge %s: %v", bridge, err)
	}

	pf, err := r.getRailDevice(rail.Name, cfg)
	if err != nil {
		return err
	}

	// add arp handling flows
	// TODO: move local flows to a separate controller
	if err = r.addArpFlows(bridge, pf); err != nil {
		return fmt.Errorf("failed to add arp flows to bridge %s: %v", bridge, err)
	}

	if err := r.addRailFlows(ctx, bridge, pf, iface, rail, ns); err != nil {
		return fmt.Errorf("failed to add flows to rail [%s]: %v", rail, err)
	}
	return nil
}

func (r *FlowReconciler) ifaceToRail(bridge string, networkStatus []netdefv1.NetworkStatus) (string, *netdefv1.NetworkStatus, error) {
	for _, ns := range networkStatus {
		// TODO change that to check for pod id once ovs-cni support that
		// it will work now because we have a single pod inside each node,
		// for multi pod it can cause issues because multiple interfaces can have the same name
		iface, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=%s",
			ns.Interface))
		if err != nil {
			return "", nil, err
		}
		br, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl iface-to-br %s", iface))
		if err != nil {
			return "", nil, err
		}
		if br == bridge {
			return iface, &ns, nil
		}
	}

	return "", nil, nil
}

func (r *FlowReconciler) addArpFlows(bridge string, pf string) error {
	localIP, err := r.Exec.ExecutePrivileged(fmt.Sprintf(`ip -o -4 addr show %s |  awk '{print $4}' | cut -d'/' -f1`,
		bridge))
	if err != nil {
		return err
	}

	// ovs-ofctl add-flow br-0000_08_00.0 "table=0,priority=100,arp,arp_tpa=3.0.0.2,actions=output:local"
	// ovs-ofctl add-flow br-0000_08_00.0 "table=0,priority=100,arp,arp_spa=3.0.0.2,actions=output:eth2"
	flow := fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=100,arp,arp_tpa=%s,actions=output:local"`,
		bridge, localIP)
	if _, err := r.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to exec [%s]: %s", flow, err)
	}

	flow = fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=100,arp,arp_spa=%s,actions=output:%s"`,
		bridge, localIP, pf)
	if _, err := r.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to exec [%s]: %s", flow, err)
	}

	return nil
}

func (r *FlowReconciler) getRailDevice(railName string, cfg *config.Config) (string, error) {
	railDevice := ""
	for _, mapping := range cfg.RailDeviceMapping {
		if mapping.RailName == railName {
			railDevice = mapping.DevName
			break
		}
	}

	if railDevice == "" {
		return "", fmt.Errorf("failed to find device for rail %s", railName)
	}

	return railDevice, nil
}

func (r *FlowReconciler) getBridgeToRail(rail *config.HostRail, cfg *config.Config) (string, error) {
	railDevice, err := r.getRailDevice(rail.Name, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to get rail device for rail %s: %s", rail.Name, err)
	}

	bridge, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl port-to-br %s", railDevice))
	if err != nil {
		return "", fmt.Errorf("failed to get bridge to rail %s, device %s: %s", rail.Name, railDevice, err)
	}

	return bridge, nil
}

func (r *FlowReconciler) getTorMac(ctx context.Context, rail *config.HostRail) (string, error) {
	logr := log.FromContext(ctx)
	// nsenter --target 1 --net -- arping 2.0.0.3 -c 1
	// nsenter --target 1 --net -- ip neighbor | grep 2.0.0.3 | awk '{print $5}'
	// TODO: check why it always return an error
	_, _ = r.Exec.ExecutePrivileged(fmt.Sprintf("arping %s -c 1", rail.PeerLeafPortIP))
	// if err != nil {
	// 	logr.Error(err, fmt.Sprintf("failed to exec: arping %s -c 1", rail.Tor))
	// 	return "", err
	// }

	mac, err := r.Exec.ExecutePrivileged(fmt.Sprintf("ip neighbor | grep %s | awk '{print $5}'", rail.PeerLeafPortIP))
	if err != nil {
		logr.Error(err, fmt.Sprintf("failed to exec: ip neighbor | grep %s | awk '{print $5}'", rail.PeerLeafPortIP))
		return "", err
	}
	if mac == "" {
		return "", fmt.Errorf("mac is empty for TOR %s", rail.PeerLeafPortIP)
	}

	return mac, nil
}

func (r *FlowReconciler) addRailFlows(ctx context.Context, bridge, pf, podIface string, rail *config.HostRail, ns *netdefv1.NetworkStatus) error {
	logr := log.FromContext(ctx)

	torMAC, err := r.getTorMac(ctx, rail)
	if err != nil {
		logr.Error(err, fmt.Sprintf("failed to get tor mac for rail %s", rail))
		return err
	}

	// every arp will point to tor
	cmd := fmt.Sprintf(`ovs-ofctl add-flow -OOpenFlow13 %s  "table=0, priority=100, arp,in_port=%s, `+
		`arp_op=1 actions=move:NXM_OF_ETH_SRC[]->NXM_OF_ETH_DST[],mod_dl_src:%s, load:0x2->NXM_OF_ARP_OP[], `+
		`move:NXM_NX_ARP_SHA[]->NXM_NX_ARP_THA[],load:0x%s->NXM_NX_ARP_SHA[], `+
		`move:NXM_OF_ARP_TPA[]->NXM_OF_ARP_SPA[],output:IN_PORT"`,
		bridge, podIface, torMAC, strings.ReplaceAll(torMAC, ":", ""))
	logr.Info(cmd)

	if _, err = r.Exec.Execute(cmd); err != nil {
		return err
	}

	// arp reply to tor
	cmd = fmt.Sprintf(`ovs-ofctl add-flow -OOpenFlow13 %s  "table=0, priority=100, arp,in_port=%s, arp_tpa=%s, `+
		`arp_op=1 actions=move:NXM_OF_ETH_SRC[]->NXM_OF_ETH_DST[],mod_dl_src:%s, load:0x2->NXM_OF_ARP_OP[], `+
		`move:NXM_NX_ARP_SHA[]->NXM_NX_ARP_THA[],load:0x%s->NXM_NX_ARP_SHA[], `+
		`move:NXM_OF_ARP_TPA[]->NXM_OF_ARP_SPA[],output:IN_PORT"`,
		bridge, pf, ns.IPs[0], ns.Mac, strings.ReplaceAll(ns.Mac, ":", ""))
	logr.Info(cmd)

	if _, err = r.Exec.Execute(cmd); err != nil {
		return err
	}

	cmd = fmt.Sprintf(`ovs-ofctl add-flow -OOpenFlow13 %s "table=0, priority=90, ip, in_port=%s, actions=output:%s"`,
		bridge, podIface, pf)
	logr.Info(cmd)
	if _, err = r.Exec.Execute(cmd); err != nil {
		return err
	}

	cmd = fmt.Sprintf(`ovs-ofctl add-flow -OOpenFlow13 %s "table=0, priority=90, ip, in_port=%s, `+
		`nw_dst=%s, actions=mod_dl_dst:%s, output:%s"`,
		bridge, pf, ns.IPs[0], ns.Mac, podIface)
	logr.Info(cmd)
	if _, err = r.Exec.Execute(cmd); err != nil {
		return err
	}

	return nil
}

func (r *FlowReconciler) getConfig(ctx context.Context) (*config.Config, error) {
	cfgMap := &corev1.ConfigMap{}
	logr := log.FromContext(ctx)
	if err := r.Get(ctx, topologyConfigMap, cfgMap); err != nil {
		logr.Error(err, "failed to get network configmap")
		return nil, err
	}

	cfg, err := config.ParseConfig(cfgMap.Data["config"])
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
				return configMap.Name == topologyConfigMap.Name &&
					configMap.Namespace == topologyConfigMap.Namespace
			})),
		).
		Complete(
			reconcile.AsReconciler(r.Client, r),
		)
}
