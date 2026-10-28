// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/Mellanox/dms-lib/pkg/device"
	"github.com/Mellanox/dms-lib/pkg/plan"
	"sigs.k8s.io/yaml"
)

var planPCIAddress = regexp.MustCompile(`^[0-9a-f]{4}:[0-9a-f]{2}:[01][0-9a-f]\.[0-7]$`)

// A failed device outside an explicit selection need not block that selection.
// Fingerprint matching or alternative maps can become ambiguous when a candidate
// is omitted from discovery, so those cases retain their previous unresolved map.
// Both stages matter: a failed post-breakout ASIC can otherwise make discovery
// incorrectly look like the pre-breakout stage.
func checkPlanDiscoveryIssues(data []byte, platform string, resolved plan.TargetMap, inventory device.Inventory) error {
	if len(inventory.Issues) == 0 {
		return nil
	}
	var authored struct {
		TargetMaps map[string]plan.TargetMap `json:"targetMaps"`
	}
	if err := yaml.Unmarshal(data, &authored); err != nil {
		return err
	}
	candidates := 0
	var authoredMap plan.TargetMap
	for _, candidate := range authored.TargetMaps {
		if platform == "" || candidate.PlatformType == platform {
			candidates++
			authoredMap = candidate
		}
	}
	if candidates != 1 {
		return fmt.Errorf("discovery issues may affect selection among authored target maps")
	}
	selected := map[string]bool{}
	for _, stage := range []*plan.TargetMapStage{authoredMap.PreBreakout, authoredMap.PostBreakout, resolved.PreBreakout, resolved.PostBreakout} {
		if stage == nil {
			continue
		}
		addresses, err := explicitStageASICs(stage)
		if err != nil {
			return err
		}
		for address := range addresses {
			selected[address] = true
		}
	}
	if len(selected) == 0 {
		return fmt.Errorf("cannot scope discovery issues without explicit endpoints")
	}
	for _, issue := range inventory.Issues {
		if !issueOutsideSelection(issue, inventory.Devices, selected) {
			return fmt.Errorf("discovery issue may affect selected device %q PCI %q: %v", issue.DeviceID, issue.PCI, issue.Err)
		}
	}
	return nil
}

func explicitStageASICs(stage *plan.TargetMapStage) (map[string]bool, error) {
	if stage == nil {
		return nil, fmt.Errorf("cannot scope discovery issues without an observed target-map stage")
	}
	addresses := make([]string, 0, len(stage.Targets)+len(stage.Devices))
	if len(stage.Targets) != 0 {
		for _, target := range stage.Targets {
			addresses = append(addresses, target.BDF)
		}
	} else {
		for _, target := range stage.Devices {
			selectors := target
			if match, ok := target["match"].(map[string]any); ok {
				selectors = match
			}
			bdf, _ := selectors["bdf"].(string)
			addresses = append(addresses, bdf)
		}
	}
	if len(addresses) == 0 {
		return nil, fmt.Errorf("cannot scope discovery issues without explicit endpoints")
	}
	selected := make(map[string]bool, len(addresses))
	for _, bdf := range addresses {
		if !planPCIAddress.MatchString(bdf) {
			return nil, fmt.Errorf("discovery issues require explicit BDF selection, not inferred fingerprints")
		}
		selected[strings.Split(bdf, ".")[0]] = true
	}
	return selected, nil
}

func issueOutsideSelection(issue device.DiscoveryIssue, devices []device.Device, selected map[string]bool) bool {
	correlated := false
	if issue.PCI != "" {
		if !planPCIAddress.MatchString(issue.PCI) || selected[strings.Split(issue.PCI, ".")[0]] {
			return false
		}
		correlated = true
	}
	for _, observed := range devices {
		if issue.DeviceID == "" || observed.ID != issue.DeviceID {
			continue
		}
		if len(observed.Ports) == 0 {
			return false
		}
		for _, port := range observed.Ports {
			if !planPCIAddress.MatchString(port.PCI) || selected[strings.Split(port.PCI, ".")[0]] {
				return false
			}
		}
		correlated = true
	}
	return correlated
}
