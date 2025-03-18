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

package config

import (
	"encoding/json"
)

const (
	// ConfigMapKey is the name of the key in ConfigMap which store
	// configuration
	ConfigMapKey = "config"
)

type Rail struct {
	Name   string `json:"name"`
	Subnet string `json:"subnet"` // /11 subnet
}

type SpectrumXNetworks struct {
	CrossRailSubnet string `json:"cross_rail_subnet"` // /8 subnet
	MTU             uint   `json:"mtu"`
	Rails           []Rail `json:"rails"`
}

type RailDeviceMapping struct {
	RailName string `json:"rail_name"`
	DevName  string `json:"dev_name"`
}

type HostRail struct {
	Name           string `json:"name"`
	Network        string `json:"network"`
	PeerLeafPortIP string `json:"peer_leaf_port_ip"`
}

type Host struct {
	HostID string     `json:"host_id"`
	Rails  []HostRail `json:"rails"`
}

type Config struct {
	SpectrumXNetworks SpectrumXNetworks   `json:"spectrum-x-networks"`
	RailDeviceMapping []RailDeviceMapping `json:"rail_device_mapping"`
	Hosts             []Host              `json:"hosts"`
}

func ParseConfig(jsonStr string) (*Config, error) {
	var config Config
	err := json.Unmarshal([]byte(jsonStr), &config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}
