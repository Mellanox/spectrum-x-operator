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

package main

import (
	"log"
	"log/slog"
	"os"

	"github.com/Mellanox/spectrum-x-operator/internal/controller"
	"github.com/Mellanox/spectrum-x-operator/internal/railcni"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	"github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/version"
)

func main() {
	//nolint:gosec
	logfile, err := os.OpenFile("/var/log/railcni.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		log.Fatalf("failed to open log file: %v", err)
	}
	defer func() { _ = logfile.Close() }()

	logger := slog.New(slog.NewJSONHandler(logfile, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	railcni := &railcni.RailCNI{
		Log:   logger,
		Exec:  &exec.Exec{},
		Flows: &controller.Flows{Exec: &exec.Exec{}, NetlinkLib: netlink.New()},
	}

	skel.PluginMainFuncs(skel.CNIFuncs{
		Add:   railcni.Add,
		Check: railcni.Check,
		Del:   railcni.Del,
	}, version.All, "rail-cni")
}
