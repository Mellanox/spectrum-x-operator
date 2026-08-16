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
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureInstalledWritesFiles(t *testing.T) {
	root := t.TempDir()

	if err := EnsureInstalled(root); err != nil {
		t.Fatalf("EnsureInstalled: %v", err)
	}

	script, err := os.ReadFile(resolveHostPath(root, HostScriptPath))
	if err != nil {
		t.Fatalf("read script: %v", err)
	}
	if !bytes.Equal(script, scriptContent) {
		t.Fatalf("script content mismatch")
	}

	dropIn, err := os.ReadFile(resolveHostPath(root, HostDropInPath))
	if err != nil {
		t.Fatalf("read drop-in: %v", err)
	}
	if !bytes.Equal(dropIn, dropInContent) {
		t.Fatalf("drop-in content mismatch")
	}

	if err := EnsureInstalled(root); err != nil {
		t.Fatalf("EnsureInstalled second call: %v", err)
	}
}

func TestResolveHostPath(t *testing.T) {
	if got := resolveHostPath("", HostScriptPath); got != HostScriptPath {
		t.Fatalf("empty root: got %q", got)
	}
	root := "/tmp/root"
	want := filepath.Join(root, "var/lib/spectrum-x/xplane-ovs-safe-start.sh")
	if got := resolveHostPath(root, HostScriptPath); got != want {
		t.Fatalf("prefixed: got %q want %q", got, want)
	}
}
