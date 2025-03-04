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

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

type HostConfigReconciler struct {
	client.Client
	NodeName           string
	ConfigMapNamespace string
	ConfigMapName      string
	Exec               exec.API
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

		pf, err := getRailDevice(rail.Name, cfg)
		if err != nil {
			logr.Error(err, "failed to get rail device", "rail", rail)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if err := r.addArpFlows(bridge, pf); err != nil {
			logr.Error(err, "failed to add arp flows", "rail", rail)
			return reconcile.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	return ctrl.Result{}, nil
}

func (r *HostConfigReconciler) addArpFlows(bridge string, pf string) error {
	// TODO: check if there are multiple ip addresses on the port, otherwise the function will fail
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
