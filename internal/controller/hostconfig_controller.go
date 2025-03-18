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

	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	libnetlink "github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

const (
	hostConfigCookie = 0x1
	defaultPriority  = 32768
)

type HostConfigReconciler struct {
	client.Client
	NodeName           string
	ConfigMapNamespace string
	ConfigMapName      string
	Exec               exec.API
	NetlinkLib         libnetlink.NetlinkLib
}

func (r *HostConfigReconciler) Reconcile(ctx context.Context, conf *corev1.ConfigMap) (ctrl.Result, error) {
	logr := log.FromContext(ctx)

	logr.Info("reconcile", "namespace", conf.Namespace, "name", conf.Name)

	cfg, err := config.ParseConfig(conf.Data["config"])
	if err != nil {
		logr.Error(err, "failed to parse config")
		// will reconcile again if config map is updated
		return reconcile.Result{}, nil
	}

	var host *config.Host
	for _, h := range cfg.Hosts {
		if h.HostID == r.NodeName {
			host = &h
			break
		}
	}

	if host == nil {
		logr.Info("host not found", "node", r.NodeName)
		// will reconcile again if config map is updated
		return reconcile.Result{}, nil
	}

	for _, rail := range host.Rails {
		bridge, err := getBridgeToRail(&rail, cfg, r.Exec)
		if err != nil {
			logr.Error(err, "failed to get bridge to rail", "rail", rail)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		// delete normal action flows - creating a secured bridge is not supported with sriov-network-operator
		// the error is ignored because in non secured bridge there are flows that cannot be deleted, specfically
		// for cookie=0, the first time will work but second reconcile will fail.
		// those are the default flows:
		// 	cookie=0x0, duration=1297.663s, table=254, n_packets=0, n_bytes=0, priority=0,reg0=0x1 actions=controller(reason=)
		// 	cookie=0x0, duration=1297.663s, table=254, n_packets=0, n_bytes=0, priority=2,recirc_id=0 actions=drop
		// 	cookie=0x0, duration=1297.663s, table=254, n_packets=0, n_bytes=0, priority=0,reg0=0x3 actions=drop
		// 	cookie=0x0, duration=1297.663s, table=254, n_packets=0, n_bytes=0, priority=0,reg0=0x2 actions=drop
		// once the bridge is created as secured this code will be removed
		_, _ = r.Exec.Execute(fmt.Sprintf("ovs-ofctl del-flows %s cookie=0x0/-1", bridge))

		pf, err := getRailDevice(rail.Name, cfg)
		if err != nil {
			logr.Error(err, "failed to get rail device", "rail", rail)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if err := r.addFlows(bridge, pf, rail); err != nil {
			logr.Error(err, "failed to add arp flows", "rail", rail)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *HostConfigReconciler) addFlows(bridge string, pf string, rail config.HostRail) error {
	link, err := r.NetlinkLib.LinkByName(bridge)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", bridge, err)
	}
	addrs, err := r.NetlinkLib.IPv4Addresses(link)
	if err != nil {
		return fmt.Errorf("failed to get addresses for interface %s: %w", bridge, err)
	}
	for _, addr := range addrs {
		flow := fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,arp,arp_tpa=%s,actions=output:local"`,
			bridge, defaultPriority, hostConfigCookie, addr.IP)
		if _, err := r.Exec.Execute(flow); err != nil {
			return fmt.Errorf("failed to exec [%s]: %s", flow, err)
		}

		flow = fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,ip,nw_dst=%s,actions=output:local"`,
			bridge, defaultPriority, hostConfigCookie, addr.IP)
		if _, err := r.Exec.Execute(flow); err != nil {
			return fmt.Errorf("failed to exec [%s]: %s", flow, err)
		}
	}

	// TOR is the gateway for the outer ip
	flow := fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,arp,arp_tpa=%s,actions=output:%s"`,
		bridge, defaultPriority, hostConfigCookie, rail.PeerLeafPortIP, pf)
	if _, err := r.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to exec [%s]: %s", flow, err)
	}

	src, err := r.NetlinkLib.GetRouteSrc(rail.PeerLeafPortIP)
	if err != nil {
		return err
	}

	for _, addr := range addrs {
		if addr.IP.String() != src {
			continue
		}

		flow := fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,`+
			`ip,in_port=local,nw_dst=%s,actions=output:%s"`,
			bridge, defaultPriority, hostConfigCookie, addr.IPNet.String(), pf)
		if _, err := r.Exec.Execute(flow); err != nil {
			return fmt.Errorf("failed to exec [%s]: %s", flow, err)
		}
	}

	return nil
}

func (r *HostConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}).
		WithEventFilter(predicate.NewPredicateFuncs(func(object client.Object) bool {
			cm, ok := object.(*corev1.ConfigMap)
			if !ok {
				return true
			}
			return cm.Name == r.ConfigMapName &&
				cm.Namespace == r.ConfigMapNamespace
		})).
		Complete(reconcile.AsReconciler(r.Client, r))
}
