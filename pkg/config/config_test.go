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

package config_test

import (
	"encoding/json"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Test Config via API", func() {

	Context("Parse Config valid input", func() {
		It("Should return Config", func() {
			cfg, err := config.ParseConfig(validConfig())
			Expect(err).ToNot(HaveOccurred())
			Expect(cfg).To(Not(BeNil()))
			marshaledConfig, err := json.MarshalIndent(cfg, "", "  ")
			Expect(err).ToNot(HaveOccurred())
			Expect(validConfig()).To(BeEquivalentTo(string(marshaledConfig)))
		})
	})
	Context("Parse Config invalid input", func() {
		It("Should not return Config", func() {
			cfg, err := config.ParseConfig("")
			Expect(err).To(HaveOccurred())
			Expect(cfg).To(BeNil())
		})
	})
})

func validConfig() string {
	jsonData := `{
  "spectrum-x-networks": {
    "cross_rail_subnet": "192.0.0.0/8",
    "mtu": 9000,
    "rails": [
      {
        "name": "rail_1",
        "subnet": "192.0.0.0/11"
      },
      {
        "name": "rail_2",
        "subnet": "192.32.0.0/11"
      }
    ]
  },
  "rail_device_mapping": [
    {
      "rail_name": "rail_1",
      "dev_name": "eth0"
    },
    {
      "rail_name": "rail_2",
      "dev_name": "eth1"
    }
  ],
  "hosts": [
    {
      "host_id": "host_1",
      "rails": [
        {
          "name": "rail_1",
          "network": "192.0.0.0/31",
          "peer_leaf_port_ip": "172.0.0.0"
        },
        {
          "name": "rail_2",
          "network": "192.32.0.0/31",
          "peer_leaf_port_ip": "172.32.0.0"
        }
      ]
    },
    {
      "host_id": "host_2",
      "rails": [
        {
          "name": "rail_1",
          "network": "192.0.0.2/31",
          "peer_leaf_port_ip": "172.0.0.2"
        },
        {
          "name": "rail_2",
          "network": "192.32.0.2/31",
          "peer_leaf_port_ip": "172.32.0.2"
        }
      ]
    }
  ]
}`
	return jsonData
}
