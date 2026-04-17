/*
 Copyright 2026, NVIDIA CORPORATION & AFFILIATES

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

package lib

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func FilterNICs(ctx context.Context, pfNames []string) ([]string, error) {
	log := log.FromContext(ctx)

	seen := make(map[string]string) // pciSlot -> iface

	for _, iface := range pfNames {
		pciSlot, err := getPCISlot(iface)
		if err != nil {
			log.V(0).Info("FilterNICs(): can't get PCI slot for interface, skipping", "interface", iface, "error", err)
			continue
		}

		if _, exists := seen[pciSlot]; !exists {
			seen[pciSlot] = iface
		}
	}

	result := make([]string, 0, len(seen))
	for _, iface := range seen {
		result = append(result, iface)
	}

	if len(result) == 0 {
		return result, fmt.Errorf("expect at least NIC found but got 0")
	}

	sort.Strings(result)
	return result, nil
}

// getPCISlot returns "0000:d8:00" (without function)
func getPCISlot(iface string) (string, error) {
	link := filepath.Join("/sys/class/net", iface, "device")

	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return "", err
	}

	// Extract last part: 0000:d8:00.0
	parts := strings.Split(resolved, "/")
	pciAddr := parts[len(parts)-1]

	// Remove function (.0 / .1)
	slot := strings.Split(pciAddr, ".")[0]

	return slot, nil
}
