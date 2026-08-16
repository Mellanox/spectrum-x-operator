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

package ovssafestart

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	// HostScriptPath is the path of the safe-start script on the node.
	HostScriptPath = "/var/lib/spectrum-x/xplane-ovs-safe-start.sh"
	// HostDropInPath is the systemd drop-in path on the node.
	HostDropInPath = "/etc/systemd/system/ovs-vswitchd.service.d/20-xplane-safe-start.conf"
)

//go:embed xplane-ovs-safe-start.sh
var scriptContent []byte

//go:embed 20-xplane-safe-start.conf
var dropInContent []byte

// EnsureInstalled writes the OVS safe-start script and systemd drop-in.
// hostRoot is empty in production (paths are bind-mounted at their absolute
// locations). Tests pass a temp dir prefix.
func EnsureInstalled(hostRoot string) error {
	if err := writeFileIfChanged(resolveHostPath(hostRoot, HostScriptPath), scriptContent, 0o755); err != nil {
		return fmt.Errorf("failed to install safe-start script: %w", err)
	}
	if err := writeFileIfChanged(resolveHostPath(hostRoot, HostDropInPath), dropInContent, 0o644); err != nil {
		return fmt.Errorf("failed to install safe-start drop-in: %w", err)
	}
	return nil
}

func resolveHostPath(hostRoot, absPath string) string {
	if hostRoot == "" {
		return absPath
	}
	return filepath.Join(hostRoot, strings.TrimPrefix(absPath, "/"))
}

func writeFileIfChanged(path string, content []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	// path is always under fixed Host* constants (optionally prefixed for tests).
	existing, err := os.ReadFile(path) //nolint:gosec // G304
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
