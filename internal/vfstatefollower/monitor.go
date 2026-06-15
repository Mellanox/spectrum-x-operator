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

package vfstatefollower

import (
	"context"
	"time"

	sriovnet "github.com/k8snetworkplumbingwg/sriovnet"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	netlinklib "github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink"
)

const defaultPollInterval = time.Second

// Monitor watches PF carrier state and mirrors it to VF representors so that
// pods receive a link-down notification when all upstream switch ports for a
// topology go down.
type Monitor struct {
	Client       client.Client
	NodeName     string
	Netlink      netlinklib.NetlinkLib
	PollInterval time.Duration
	// FindActiveReps is injectable for testing; defaults to the sriovnet-based impl.
	FindActiveReps func(pfName string) ([]string, error)
}

// Start runs the carrier-follow loop until ctx is canceled.
func (m *Monitor) Start(ctx context.Context) {
	interval := m.PollInterval
	if interval == 0 {
		interval = defaultPollInterval
	}

	logr := log.FromContext(ctx).WithName("vf-state-follower")
	logr.Info("starting", "interval", interval)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			m.tick(ctx)
		case <-ctx.Done():
			return
		}
	}
}

// tick reads all SpectrumXRailPoolConfig CRs from the cache, filters those
// that select this node, and reconciles each topology.
func (m *Monitor) tick(ctx context.Context) {
	logr := log.FromContext(ctx).WithName("vf-state-follower")

	node := &corev1.Node{}
	if err := m.Client.Get(ctx, types.NamespacedName{Name: m.NodeName}, node); err != nil {
		logr.Error(err, "failed to get node")
		return
	}

	rpcList := &v1alpha2.SpectrumXRailPoolConfigList{}
	if err := m.Client.List(ctx, rpcList); err != nil {
		logr.Error(err, "failed to list SpectrumXRailPoolConfig")
		return
	}

	for i := range rpcList.Items {
		rpc := &rpcList.Items[i]
		if rpc.Spec.VFStateFollow != nil && !*rpc.Spec.VFStateFollow {
			continue
		}
		sel := labels.Set(rpc.Spec.NodeSelector).AsSelector()
		if !sel.Matches(labels.Set(node.Labels)) {
			continue
		}
		for _, rt := range rpc.Spec.RailTopology {
			if err := m.reconcileTopology(ctx, rt); err != nil {
				logr.Error(err, "failed to reconcile topology", "topology", rt.Name)
			}
		}
	}
}

// reconcileTopology checks whether all PFs for the topology are NO-CARRIER and
// mirrors that state to the VF representors whose VFs are inside pods.
func (m *Monitor) reconcileTopology(ctx context.Context, rt v1alpha2.RailTopology) error {
	logr := log.FromContext(ctx).WithName("vf-state-follower")

	allDown, err := m.allPFsNoCarrier(rt.NicSelector.PfNames)
	if err != nil {
		return err
	}

	for _, pfName := range rt.NicSelector.PfNames {
		reps, err := m.activeRepsForPF(pfName)
		if err != nil {
			logr.Error(err, "failed to find active reps", "pf", pfName)
			continue
		}

		for _, repName := range reps {
			rep, err := m.Netlink.LinkByName(repName)
			if err != nil {
				logr.Error(err, "failed to get rep link", "rep", repName)
				continue
			}

			isUp := m.Netlink.IsLinkAdminStateUp(rep)
			if allDown && isUp {
				logr.Info("setting rep down (PF NO-CARRIER)", "rep", repName, "topology", rt.Name)
				if err := m.Netlink.LinkSetDown(rep); err != nil {
					logr.Error(err, "failed to set rep down", "rep", repName)
				}
			} else if !allDown && !isUp {
				logr.Info("restoring rep up (PF carrier recovered)", "rep", repName, "topology", rt.Name)
				if err := m.Netlink.LinkSetUp(rep); err != nil {
					logr.Error(err, "failed to set rep up", "rep", repName)
				}
			}
		}
	}

	return nil
}

// allPFsNoCarrier returns true when every PF in pfNames reports NO-CARRIER.
func (m *Monitor) allPFsNoCarrier(pfNames []string) (bool, error) {
	for _, pfName := range pfNames {
		link, err := m.Netlink.LinkByName(pfName)
		if err != nil {
			return false, err
		}
		if !m.Netlink.IsLinkNoCarrier(link) {
			return false, nil
		}
	}
	return true, nil
}

// activeRepsForPF returns the names of VF representors for pfName whose
// corresponding VF is not present in the host network namespace (i.e. the VF
// is inside a pod).
func activeRepsForPF(pfName string) ([]string, error) {
	handle, err := sriovnet.GetPfNetdevHandle(pfName)
	if err != nil {
		return nil, err
	}

	reps := make([]string, 0, len(handle.List))
	for _, vf := range handle.List {
		if sriovnet.GetVfNetdevName(handle, vf) != "" {
			continue
		}
		rep, err := sriovnet.GetVfRepresentor(pfName, vf.Index)
		if err != nil {
			continue
		}
		reps = append(reps, rep)
	}
	return reps, nil
}

// activeRepsForPF calls FindActiveReps if set, otherwise the default sriovnet impl.
func (m *Monitor) activeRepsForPF(pfName string) ([]string, error) {
	if m.FindActiveReps != nil {
		return m.FindActiveReps(pfName)
	}
	return activeRepsForPF(pfName)
}
