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

package railcni

import (
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/Mellanox/spectrum-x-operator/internal/controller"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	"github.com/containernetworking/cni/pkg/skel"
	"github.com/containernetworking/cni/pkg/types"
	current "github.com/containernetworking/cni/pkg/types/100"
	"github.com/containernetworking/cni/pkg/version"
)

// NetConf represents the CNI configuration
type NetConf struct {
	types.NetConf
	OVSBridge string `json:"ovsBridge"`
}

type Args struct {
	types.CommonArgs
	// using args that are added by ovs-cni
	//nolint:stylecheck
	K8S_POD_UID types.UnmarshallableString
}

// parseNetConf parses the CNI configuration
func parseNetConf(data []byte) (*NetConf, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}
	return conf, nil
}

type RailCNI struct {
	Log   *slog.Logger
	Exec  exec.API
	Flows controller.FlowsAPI
}

func (r *RailCNI) Add(args *skel.CmdArgs) error {
	r.Log.Info("railcni add", "args", args)
	r.Log.Info("railcni add", "args.StdinData", string(args.StdinData))

	conf, err := parseNetConf(args.StdinData)
	if err != nil {
		r.Log.Error("railcni add", "error", err)
		return err
	}

	if err := version.ParsePrevResult(&conf.NetConf); err != nil {
		r.Log.Error("railcni add", "error", err)
		return err
	}

	// Get the previous result from the chain
	var prevResult *current.Result
	if conf.PrevResult != nil {
		prevResult, err = current.NewResultFromResult(conf.PrevResult)
		if err != nil {
			r.Log.Error("railcni add", "error", err)
			return fmt.Errorf("failed to parse prevResult: %v", err)
		}
	} else {
		r.Log.Error("railcni add", "error", "must be called as chained plugin")
		return fmt.Errorf("must be called as chained plugin")
	}

	vf, err := r.getVF(prevResult)
	if err != nil {
		r.Log.Error("railcni add, failed to get vf", "error", err)
		return err
	}

	bridge, err := r.Exec.Execute(fmt.Sprintf("ovs-vsctl port-to-br %s", vf))
	if err != nil {
		r.Log.Error("railcni add", "error", err)
		return fmt.Errorf("failed to get bridge to vf %s: %s", vf, err)
	}

	cniArgs := Args{}
	if err := types.LoadArgs(args.Args, &cniArgs); err != nil {
		r.Log.Error("railcni add, failed to load args", "error", err)
		return fmt.Errorf("failed to load args: %v", err)
	}

	cookie := controller.GenerateUint64FromString(string(cniArgs.K8S_POD_UID))

	// get pod interface mac
	podMac, err := r.getPodMac(prevResult)
	if err != nil {
		r.Log.Error("railcni add, failed to get pod mac", "error", err)
		return err
	}

	if len(prevResult.IPs) != 1 {
		r.Log.Error("railcni add, expected single ip", "ips", prevResult.IPs)
		return fmt.Errorf("expected single ip, got %+v", prevResult.IPs)
	}

	if err := r.Flows.AddPodRailFlows(cookie, vf, bridge, prevResult.IPs[0].Address.IP.String(), podMac); err != nil {
		r.Log.Error("railcni add pod flows failed", "error", err)
		return fmt.Errorf("failed to add pod rail flows: %s", err)
	}

	r.Log.Info("railcni add completed", "vf", vf)
	// Return the previous result unchanged
	return types.PrintResult(prevResult, prevResult.CNIVersion)
}

func (r *RailCNI) getPodMac(prevResult *current.Result) (string, error) {
	podMac := ""
	for _, iface := range prevResult.Interfaces {
		if iface.Sandbox != "" {
			podMac = iface.Mac
			break
		}
	}

	if podMac == "" {
		r.Log.Error("railcni add, failed to get pod mac", "error", "no pod mac found")
		return podMac, fmt.Errorf("no pod mac found")
	}

	return podMac, nil
}

func (r *RailCNI) getVF(prevResult *current.Result) (string, error) {
	vf := ""
	// sandbox is the interface inside the pod
	for _, iface := range prevResult.Interfaces {
		r.Log.Info("railcni add", "iface", iface.Name)
		if iface.Sandbox == "" {
			vf = iface.Name
			break
		}
	}

	if vf == "" {
		r.Log.Error("railcni add", "error", "no vf found")
		return "", fmt.Errorf("no vf found")
	}

	return vf, nil
}

func (r *RailCNI) Del(args *skel.CmdArgs) error {
	return nil
}

func (r *RailCNI) Check(args *skel.CmdArgs) error {
	return nil
}
