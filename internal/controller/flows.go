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
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	"go.uber.org/multierr"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	GroupIdAllPlanes = 100
	RailPodID        = "rail_pod_id"
)

//go:generate ../../bin/mockgen -destination mock_flows.go -source flows.go -package controller

type FlowsAPI interface {
	AddHardwareMultiplaneFlows(bridgeName string, cookie uint64, pfNames []string) error
	AddHardwareMultiplaneGroups(bridgeName string, pfNames []string) error
	AddPodRailFlows(cookie uint64, vf, bridge, podIP string) error
	AddSoftwareMultiplaneFlows(bridgeName string, cookie uint64, pfName string) error
	DeleteFlowsByCookie(bridgeName string, cookie uint64) error
	DeletePodRailFlows(cookie uint64, podID string) error
	IsBridgeManagedByRailCNI(bridge, podID string) (bool, error)
	CleanupStaleFlowsForBridges(ctx context.Context, existingPodUIDs map[string]bool) error
	GetBridgeNameFromPortName(portName string) (string, error)
}

var (
	_             FlowsAPI = &Flows{}
	sysClassNetFS          = os.DirFS("/sys/class/net")
)

type Flows struct {
	Exec exec.API
}

func getPlaneIDFromPfName(pfName string) (int, error) {
	fd, err := sysClassNetFS.Open(fmt.Sprintf("%s/phys_port_name", pfName))
	if err != nil {
		return 0, fmt.Errorf("failed to open %s: %v", pfName, err)
	}
	defer fd.Close()

	var pid int

	if _, err = fmt.Fscanf(fd, "p%d", &pid); err != nil {
		return 0, fmt.Errorf("failed to read %s: %v", pfName, err)
	}

	return pid, nil
}

func (f *Flows) AddHardwareMultiplaneFlows(bridgeName string, cookie uint64, pfNames []string) error {
	if len(pfNames) == 0 {
		return errors.New("no pf names provided")
	}

	// ARP.2
	flow := fmt.Sprintf("table=0,cookie=0x%x,priority=16384,arp,actions=output:%s", cookie, pfNames[0])

	for _, pfName := range pfNames[1:] {
		flow += ",output:" + pfName
	}

	if _, err := f.Exec.Execute(fmt.Sprintf("ovs-ofctl add-flow %s \"%s\"", bridgeName, flow)); err != nil {
		return fmt.Errorf("failed to add arp flow for %s to bridge %s: %v", pfNames[0], bridgeName, err)
	}

	for _, pfName := range pfNames {
		planeID, err := getPlaneIDFromPfName(pfName)
		if err != nil {
			return fmt.Errorf("failed to get plane id for %s: %v", pfName, err)
		}

		// IP.1
		flow = fmt.Sprintf("table=1,cookie=0x%x,nv_mp_pid=%d,nv_mp_strict=0,nv_mp_preferred=1,actions=group:%d", cookie, planeID, planeID)
		if _, err := f.Exec.Execute(fmt.Sprintf("ovs-ofctl add-flow %s \"%s\"", bridgeName, flow)); err != nil {
			return fmt.Errorf("failed to add IP flow for %s to bridge %s: %v", pfName, bridgeName, err)
		}

		// RTT rules (RTT CC workaround flows)
		tp_src_base := 10000
		flow = fmt.Sprintf("table=1,cookie=0x%x,nv_mp_pid=%d,nv_mp_strict=1,actions=mod_tp_src=%d,output:%s", cookie, planeID, tp_src_base+planeID, pfName)
		if _, err := f.Exec.Execute(fmt.Sprintf("ovs-ofctl add-flow %s \"%s\"", bridgeName, flow)); err != nil {
			return fmt.Errorf("failed to add flow for %s to bridge %s: %v", pfName, bridgeName, err)
		}
	}

	// IP.1b
	flow = fmt.Sprintf("table=1,nv_mp_preferred=0,actions=group:%d", GroupIdAllPlanes)

	if _, err := f.Exec.Execute(fmt.Sprintf("ovs-ofctl add-flow %s \"%s\"", bridgeName, flow)); err != nil {
		return fmt.Errorf("failed to add non-RoCE flow to bridge %s: %v", bridgeName, err)
	}

	return nil
}

func (f *Flows) AddHardwareMultiplaneGroups(bridgeName string, pfNames []string) error {
	for _, pfName := range pfNames {
		planeID, err := getPlaneIDFromPfName(pfName)
		if err != nil {
			return fmt.Errorf("could not get plane ID for %s: %v", pfName, err)
		}

		group := fmt.Sprintf("group_id=%d,type=fast_failover,", planeID)
		group += fmt.Sprintf("bucket=watch_port=%s,actions=output:%s,", pfName, pfName)
		group += fmt.Sprintf("bucket=watch_group=%d,actions=group:%d", GroupIdAllPlanes, GroupIdAllPlanes)

		if _, err := f.Exec.Execute(fmt.Sprintf("ovs-ofctl add-group %s \"%s\"", bridgeName, group)); err != nil {
			return fmt.Errorf("failed to add group for %s to bridge %s: %v", pfName, bridgeName, err)
		}
	}

	// All planes
	group := fmt.Sprintf("group_id=%d,type=select,selection_method=hash", GroupIdAllPlanes)
	for _, pfName := range pfNames {
		group += fmt.Sprintf(",bucket=watch_port=%s,actions=output:%s", pfName, pfName)
	}

	if _, err := f.Exec.Execute(fmt.Sprintf("ovs-ofctl add-group %s \"%s\"", bridgeName, group)); err != nil {
		return fmt.Errorf("failed to add group %d to bridge %s: %v", GroupIdAllPlanes, bridgeName, err)
	}

	return nil
}

func (f *Flows) AddPodRailFlows(cookie uint64, vf, bridge, podIP string) error {
	// ovs-ofctl add-flow -OOpenFlow13 $RAIL_BR "table=0, arp,arp_tpa=${CONTAINER_IP} actions=output:${REP_PORT}"
	flow := fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,cookie=0x%x,arp,arp_tpa=%s,actions=output:%s"`,
		bridge, cookie, podIP, vf)
	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add flows to bridge [%s]: %v", bridge, err)
	}

	flow = fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,cookie=0x%x,ip,nw_dst=%s,actions=output:%s"`,
		bridge, cookie, podIP, vf)
	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add flows to bridge [%s]: %v", bridge, err)
	}

	flow = fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,ip,in_port=%s,actions=resubmit(,1)"`,
		bridge, 16384, cookie, vf)
	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add flows to bridge [%s]: %v", bridge, err)
	}

	return nil
}

func (f *Flows) AddSoftwareMultiplaneFlows(bridgeName string, cookie uint64, pfName string) error {
	// ARP.2
	flow := fmt.Sprintf("ovs-ofctl add-flow %s \"table=0,cookie=0x%x,priority=16384,arp,actions=output:%s\"", bridgeName, cookie, pfName)

	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add ARP flow to bridge %s: %v", bridgeName, err)
	}

	// IP.1
	flow = fmt.Sprintf("ovs-ofctl add-flow %s \"table=1,cookie=0x%x,actions=output:%s\"", bridgeName, cookie, pfName)

	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add IP flow to bridge %s: %v", bridgeName, err)
	}

	return nil
}

func (f *Flows) DeleteFlowsByCookie(bridgeName string, cookie uint64) error {
	flow := fmt.Sprintf("ovs-ofctl del-flows %s cookie=0x%x/-1", bridgeName, cookie)

	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to delete flows for bridge %s: %v", bridgeName, err)
	}

	return nil
}

func (f *Flows) DeletePodRailFlows(cookie uint64, podID string) error {
	out, err := f.Exec.Execute("ovs-vsctl list-br")
	if err != nil {
		return fmt.Errorf("failed to list bridges: %v", err)
	}

	bridges := strings.Split(out, "\n")

	var errs error

	for _, bridge := range bridges {
		val, err := f.Exec.Execute(fmt.Sprintf("ovs-vsctl br-get-external-id %s %s", bridge, podID))
		if err != nil {
			errs = multierr.Append(errs, fmt.Errorf("failed to get external id for bridge %s: %v", bridge, err))
			continue
		}

		if val == RailPodID {
			if err := f.cleanBridgeFlows(bridge, podID, cookie); err != nil {
				errs = multierr.Append(errs, err)
				continue
			}
		}
	}
	return errs
}

func (f *Flows) cleanBridgeFlows(bridge, podID string, cookie uint64) error {
	flow := fmt.Sprintf("ovs-ofctl del-flows %s cookie=0x%x/-1", bridge, cookie)
	out, err := f.Exec.Execute(flow)
	if err != nil {
		return fmt.Errorf("failed to delete flows for bridge %s: %v, output: %s", bridge, err, out)
	}
	// clear external id
	_, err = f.Exec.Execute(fmt.Sprintf("ovs-vsctl remove bridge %s external_ids %s", bridge, podID))
	if err != nil {
		return fmt.Errorf("failed to clear external id for bridge %s: %v", bridge, err)
	}
	return nil
}

func (f *Flows) IsBridgeManagedByRailCNI(bridge, podID string) (bool, error) {
	out, err := f.Exec.Execute(fmt.Sprintf("ovs-vsctl br-get-external-id %s %s", bridge, podID))
	if err != nil {
		return false, fmt.Errorf("failed to get external id for bridge %s, output: %s, err: %v", bridge, out, err)
	}

	return out == RailPodID, nil
}

func (f *Flows) CleanupStaleFlowsForBridges(ctx context.Context, existingPodUIDs map[string]bool) error {
	logr := log.FromContext(ctx)

	// List all bridges once
	out, err := f.Exec.Execute("ovs-vsctl list-br")
	if err != nil {
		return fmt.Errorf("failed to list bridges: out: %s, err: %v", out, err)
	}

	bridges := strings.Split(out, "\n")
	var errs error

	for _, bridge := range bridges {
		// Get all external IDs for this bridge once
		externalIDsOut, err := f.Exec.Execute(fmt.Sprintf("ovs-vsctl br-get-external-id %s", bridge))
		if err != nil {
			errs = multierr.Append(errs,
				fmt.Errorf("failed to get external ids for bridge %s: out: %s, err: %v", bridge, externalIDsOut, err))
			continue
		}

		// Collect all stale pods for this bridge
		var stalePods []struct {
			podID  string
			cookie uint64
		}

		// Parse external IDs to find pod IDs managed by rail CNI
		lines := strings.Split(externalIDsOut, "\n")
		for _, line := range lines {
			if strings.Contains(line, "="+RailPodID) {
				// Extract the key part (pod ID) before the =
				parts := strings.Split(line, "=")
				if len(parts) >= 2 {
					podID := parts[0]

					// Check if this pod still exists
					if !existingPodUIDs[podID] {
						// Pod doesn't exist, add to cleanup list
						cookie := GenerateUint64FromString(podID)
						stalePods = append(stalePods, struct {
							podID  string
							cookie uint64
						}{podID, cookie})
					}
				}
			}
		}

		if len(stalePods) == 0 {
			continue
		}

		for _, stalePod := range stalePods {
			logr.Info("Cleaning up stale flows for bridge", "bridge", bridge, "podID", stalePod.podID, "cookie", stalePod.cookie)
			if err := f.cleanBridgeFlows(bridge, stalePod.podID, stalePod.cookie); err != nil {
				errs = multierr.Append(errs, err)
				continue
			}
		}
	}

	return errs
}

func (f *Flows) GetBridgeNameFromPortName(portName string) (string, error) {
	out, err := f.Exec.Execute("ovs-vsctl port-to-br " + portName)
	if err != nil {
		return "", fmt.Errorf("failed to get bridge name for port %s: %v", portName, err)
	}
	return out, nil
}
