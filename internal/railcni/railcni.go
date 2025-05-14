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

// parseNetConf parses the CNI configuration
func parseNetConf(data []byte) (*NetConf, error) {
	conf := &NetConf{}
	if err := json.Unmarshal(data, conf); err != nil {
		return nil, fmt.Errorf("failed to parse config: %v", err)
	}
	return conf, nil
}

type RailCNI struct {
	Log *slog.Logger
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

	// Return the previous result unchanged
	return types.PrintResult(prevResult, conf.CNIVersion)
}

func (r *RailCNI) Del(args *skel.CmdArgs) error {
	return nil
}

func (r *RailCNI) Check(args *skel.CmdArgs) error {
	return nil
}
