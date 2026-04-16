package lib

import (
	"context"
	"path/filepath"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/log"
)

func FilterNICs(ctx context.Context, pfNames []string) []string {
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
	return result
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
