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
	"fmt"
	"strings"

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	libnetlink "github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink"

	"go.uber.org/multierr"
)

const (
	railPeerIP      = "rail_peer_ip"
	railUplink      = "rail_uplink"
	RailPodID       = "rail_pod_id"
	defaultPriority = 32768
)

//go:generate ../../bin/mockgen -destination mock_flows.go -source flows.go -package controller

type FlowsAPI interface {
	AddPodRailFlows(cookie uint64, vf, bridge, podIP, podMAC string) error
	DeletePodRailFlows(cookie uint64, podID string) error
	IsBridgeManagedByRailCNI(bridge, podID string) (bool, error)
}

var _ FlowsAPI = &Flows{}

type Flows struct {
	Exec       exec.API
	NetlinkLib libnetlink.NetlinkLib
}

func (f *Flows) AddPodRailFlows(cookie uint64, vf, bridge, podIP, podMAC string) error {
	// ovs-ofctl add-flow -OOpenFlow13 $RAIL_BR "table=0, arp,arp_tpa=${CONTAINER_IP} actions=output:${REP_PORT}"
	flow := fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,arp,arp_tpa=%s,actions=output:%s"`,
		bridge, defaultPriority, cookie, podIP, vf)
	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add flows to bridge [%s]: %v", bridge, err)
	}

	link, err := f.NetlinkLib.LinkByName(bridge)
	if err != nil {
		return fmt.Errorf("failed to get interface %s: %w", bridge, err)
	}

	bridgeMAC := link.Attrs().HardwareAddr

	torIP, err := f.Exec.Execute(fmt.Sprintf("ovs-vsctl br-get-external-id %s %s", bridge, railPeerIP))
	if err != nil {
		return fmt.Errorf("failed to get tor ip for bridge [%s]: %v", bridge, err)
	}

	if torIP == "" {
		return fmt.Errorf("tor ip is empty for bridge [%s], set [%s] external_id on the bridge", bridge, railPeerIP)
	}

	torMAC, err := f.getTorMac(torIP)
	if err != nil {
		return fmt.Errorf("failed to get tor mac for bridge [%s]: %v", bridge, err)
	}

	uplink, err := f.Exec.Execute(fmt.Sprintf("ovs-vsctl br-get-external-id %s %s", bridge, railUplink))
	if err != nil {
		return fmt.Errorf("failed to get rail uplink for bridge [%s]: %v", bridge, err)
	}

	if uplink == "" {
		return fmt.Errorf("uplink is empty for bridge [%s], set [%s] external_id on the bridge", bridge, railUplink)
	}

	// ovs-ofctl add-flow -OOpenFlow13 $RAIL_BR "table=0,ip,in_port=${REP_PORT},
	// actions=mod_dl_src=${ROUTER_MAC}, mod_dl_dst=${ROUTER_NH_MAC},dec_ttl, output=${PF_PORT}"
	// setting the priority to avoid conflicts with a more specific flows
	flow = fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,ip,in_port=%s,`+
		`actions=mod_dl_src=%s,mod_dl_dst=%s,dec_ttl,output:%s"`,
		bridge, defaultPriority/2, cookie, vf, bridgeMAC, torMAC, uplink)
	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add flows to bridge [%s] flow [%s] %v", bridge, flow, err)
	}

	// ovs-ofctl add-flow -OOpenFlow13 $RAIL_BR "table=0,ip,nw_dst=${CONTAINER_IP},
	// actions=mod_dl_src=${ROUTER_MAC},mod_dl_dst=${CONTAINER_MAC},dec_ttl, output=${REP_PORT}"
	flow = fmt.Sprintf(`ovs-ofctl add-flow %s "table=0,priority=%d,cookie=0x%x,ip,nw_dst=%s,`+
		`actions=mod_dl_src=%s,mod_dl_dst=%s,dec_ttl,output:%s"`,
		bridge, defaultPriority, cookie, podIP, bridgeMAC, podMAC, vf)
	if _, err := f.Exec.Execute(flow); err != nil {
		return fmt.Errorf("failed to add flows to bridge [%s] flow [%s]: %v", bridge, flow, err)
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
			flow := fmt.Sprintf(`ovs-ofctl del-flows %s cookie=0x%x/-1`, bridge, cookie)
			out, err := f.Exec.Execute(flow)
			if err != nil {
				errs = multierr.Append(errs, fmt.Errorf("failed to delete flows for bridge %s: %v, output: %s", bridge, err, out))
				continue
			}
			// clear external id
			_, err = f.Exec.Execute(fmt.Sprintf(`ovs-vsctl br-set-external-id %s %s ""`, bridge, podID))
			if err != nil {
				errs = multierr.Append(errs, fmt.Errorf("failed to clear external id for bridge %s: %v", bridge, err))
				continue
			}
		}
	}
	return errs
}

func (f *Flows) IsBridgeManagedByRailCNI(bridge, podID string) (bool, error) {
	out, err := f.Exec.Execute(fmt.Sprintf("ovs-vsctl br-get-external-id %s %s", bridge, podID))
	if err != nil {
		return false, fmt.Errorf("failed to get external id for bridge %s, output: %s, err: %v", bridge, out, err)
	}

	return out == RailPodID, nil
}

func (f *Flows) getTorMac(torIP string) (string, error) {
	// nsenter --target 1 --net -- arping 2.0.0.3 -c 1
	// nsenter --target 1 --net -- ip neighbor | grep 2.0.0.3 | awk '{print $5}'
	// TODO: check why it always return an error
	reply, _ := f.Exec.ExecutePrivileged(fmt.Sprintf(`arping %s -c 1 | grep "reply from" | awk '{print $5}' | tr -d '[]'`,
		torIP))
	// if err != nil {
	// 	logr.Error(err, fmt.Sprintf("failed to exec: arping %s -c 1", rail.Tor))
	// 	return "", err
	// }

	if reply == "" {
		return "", fmt.Errorf("no reply from arping %s", torIP)
	}

	return reply, nil
}
