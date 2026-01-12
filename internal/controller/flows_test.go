// Copyright 2025 NVIDIA CORPORATION & AFFILIATES
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"
	"strings"
	"testing/fstest"

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

type containsSubstringMatcher struct {
	substring string
}

func matchSubstring(substring string) gomock.Matcher {
	return &containsSubstringMatcher{substring}
}

func (m *containsSubstringMatcher) Matches(x interface{}) bool {
	s, ok := x.(string)
	if !ok {
		return false
	}
	return strings.Contains(s, m.substring)
}

func (m *containsSubstringMatcher) String() string {
	return "contains substring " + m.substring
}

var _ = Describe("Flows", func() {
	var (
		flows    *Flows
		execMock *exec.MockAPI
		ctrl     *gomock.Controller
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(ctrl)
		flows = &Flows{Exec: execMock}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("AddHardwareMultiplaneFlows", func() {
		BeforeEach(func() {
			sysClassNetFS = fstest.MapFS{
				"test-p0/phys_port_name": &fstest.MapFile{
					Data: []byte("p0"),
				},
				"test-p1/phys_port_name": &fstest.MapFile{
					Data: []byte("p1"),
				},
			}
		})

		It("should add flows for hardware multiplane", func() {
			gomock.InOrder(
				execMock.EXPECT().Execute(`ovs-ofctl add-flow test-br "table=0,cookie=0x1234,priority=16384,arp,actions=output:test-p0,output:test-p1"`).Return("", nil),
				execMock.EXPECT().Execute(`ovs-ofctl add-flow test-br "table=1,cookie=0x1234,nv_mp_pid=0,nv_mp_strict=0,nv_mp_preferred=1,actions=group:0"`).Return("", nil),
				execMock.EXPECT().Execute(`ovs-ofctl add-flow test-br "table=1,cookie=0x1234,nv_mp_pid=0,nv_mp_strict=1,actions=mod_tp_src=10000,output:test-p0"`).Return("", nil),
				execMock.EXPECT().Execute(`ovs-ofctl add-flow test-br "table=1,cookie=0x1234,nv_mp_pid=1,nv_mp_strict=0,nv_mp_preferred=1,actions=group:1"`).Return("", nil),
				execMock.EXPECT().Execute(`ovs-ofctl add-flow test-br "table=1,cookie=0x1234,nv_mp_pid=1,nv_mp_strict=1,actions=mod_tp_src=10001,output:test-p1"`).Return("", nil),
				execMock.EXPECT().Execute(`ovs-ofctl add-flow test-br "table=1,nv_mp_preferred=0,actions=group:100"`).Return("", nil),
			)

			err := flows.AddHardwareMultiplaneFlows("test-br", uint64(0x1234), []string{"test-p0", "test-p1"})
			Expect(err).Should(Succeed())
		})
	})

	Context("AddHardwareMultiplaneGroups", func() {
		It("should add groups for hardware multiplane", func() {
			gomock.InOrder(
				execMock.EXPECT().Execute(
					`ovs-ofctl add-group test-br `+
						`"group_id=0,type=fast_failover,`+
						`bucket=watch_port=test-p0,actions=output:test-p0,`+
						`bucket=watch_group=100,actions=group:100"`,
				).Return("", nil),
				execMock.EXPECT().Execute(
					`ovs-ofctl add-group test-br `+
						`"group_id=1,type=fast_failover,`+
						`bucket=watch_port=test-p1,actions=output:test-p1,`+
						`bucket=watch_group=100,actions=group:100"`,
				).Return("", nil),
				execMock.EXPECT().Execute(
					`ovs-ofctl add-group test-br `+
						`"group_id=100,type=select,selection_method=hash,`+
						`bucket=watch_port=test-p0,actions=output:test-p0,`+
						`bucket=watch_port=test-p1,actions=output:test-p1"`,
				).Return("", nil),
			)

			err := flows.AddHardwareMultiplaneGroups("test-br", []string{"test-p0", "test-p1"})
			Expect(err).Should(Succeed())
		})
	})

	Context("AddPodRailFlows", func() {
		It("should add flows for pod rail", func() {
			// Mock adding flows
			execMock.EXPECT().Execute(gomock.Any()).Return("", nil).Times(3)

			err := flows.AddPodRailFlows(0x5, "vf0", "br-rail1", "10.0.0.1")
			Expect(err).Should(Succeed())
		})

		It("should return error if fails to add arp flow", func() {
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow br-rail1")).Return("", fmt.Errorf("failed to add arp flow"))
			err := flows.AddPodRailFlows(0x5, "vf0", "br-rail1", "10.0.0.1")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to add flows to bridge"))
		})

		It("should return error if fails to add ip flow", func() {
			// First ARP flow succeeds
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow br-rail1")).Return("", nil)
			// Second IP flow fails
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow br-rail1")).Return("", fmt.Errorf("failed to add ip flow"))

			err := flows.AddPodRailFlows(0x5, "vf0", "br-rail1", "10.0.0.1")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to add flows to bridge"))
		})

		It("should return error if fails to add pod flow", func() {
			// First ARP flow succeeds
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow br-rail1")).Return("", nil)
			// Second IP flow succeeds
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow br-rail1")).Return("", nil)
			// Third pod flow fails
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow br-rail1")).Return("", fmt.Errorf("failed to add pod flow"))

			err := flows.AddPodRailFlows(0x5, "vf0", "br-rail1", "10.0.0.1")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to add flows to bridge"))
		})
	})

	Context("AddSoftwareMultiplaneFlows", func() {
		It("should add flows for software multiplane", func() {
			gomock.InOrder(
				execMock.
					EXPECT().
					Execute(`ovs-ofctl add-flow test-br "table=0,cookie=0x1234,priority=16384,arp,actions=output:pf0"`).
					Return("", nil),
				execMock.
					EXPECT().
					Execute(`ovs-ofctl add-flow test-br "table=1,cookie=0x1234,actions=output:pf0"`).
					Return("", nil),
			)
			err := flows.AddSoftwareMultiplaneFlows("test-br", uint64(0x1234), "pf0")
			Expect(err).Should(Succeed())
		})

		It("should return error if fails to add arp flow", func() {
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow test-br")).Return("", fmt.Errorf("failed to add arp flow"))
			err := flows.AddSoftwareMultiplaneFlows("test-br", uint64(0x1234), "pf0")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to add ARP flow"))
		})

		It("should return error if fails to add ip flow", func() {
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow test-br")).Return("", nil)
			execMock.EXPECT().Execute(matchSubstring("ovs-ofctl add-flow test-br")).Return("", fmt.Errorf("failed to add ip flow"))
			err := flows.AddSoftwareMultiplaneFlows("test-br", uint64(0x1234), "pf0")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to add IP flow"))
		})
	})

	Context("DeleteFlowsByCookie", func() {
		It("should delete flows by cookie", func() {
			execMock.EXPECT().Execute("ovs-ofctl del-flows test-br cookie=0x1234/-1").Return("", nil)
			err := flows.DeleteFlowsByCookie("test-br", uint64(0x1234))
			Expect(err).Should(Succeed())
		})

		It("should return error if fails to delete flows", func() {
			execMock.EXPECT().Execute("ovs-ofctl del-flows test-br cookie=0x1234/-1").Return("", fmt.Errorf("failed to delete flows"))
			err := flows.DeleteFlowsByCookie("test-br", uint64(0x1234))
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to delete flows"))
		})
	})

	Context("DeletePodRailFlows", func() {
		It("should delete flows on pod deletion", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1\nbr-rail2", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("rail_pod_id", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail2 test-pod-uid").
				Return("rail_pod_id", nil)
			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail1 cookie=0x5/-1").Return("", nil)
			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail2 cookie=0x5/-1").Return("", nil)
			execMock.EXPECT().Execute("ovs-vsctl remove bridge br-rail1 external_ids test-pod-uid").Return("", nil)
			execMock.EXPECT().Execute("ovs-vsctl remove bridge br-rail2 external_ids test-pod-uid").Return("", nil)
			err := flows.DeletePodRailFlows(0x5, "test-pod-uid")
			Expect(err).Should(Succeed())
		})

		It("should return error if failed to list bridges", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("", fmt.Errorf("failed to list bridges"))
			err := flows.DeletePodRailFlows(0x5, "test-pod-uid")
			Expect(err).Should(HaveOccurred())
		})

		It("should return error if failed to get external id", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)
			execMock.EXPECT().
				Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("", fmt.Errorf("failed to get external id"))

			err := flows.DeletePodRailFlows(0x5, "test-pod-uid")
			Expect(err).Should(HaveOccurred())
		})

		It("should return error if failed to delete flows", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)

			execMock.EXPECT().
				Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("rail_pod_id", nil)

			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail1 cookie=0x5/-1").
				Return("", fmt.Errorf("failed to delete flows"))

			err := flows.DeletePodRailFlows(0x5, "test-pod-uid")
			Expect(err).Should(HaveOccurred())
		})

		It("should return error if failed to clear external id", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)
			execMock.EXPECT().
				Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("rail_pod_id", nil)
			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail1 cookie=0x5/-1").Return("", nil)
			execMock.EXPECT().
				Execute("ovs-vsctl remove bridge br-rail1 external_ids test-pod-uid").
				Return("", fmt.Errorf("failed to clear external id"))

			err := flows.DeletePodRailFlows(0x5, "test-pod-uid")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to clear external id"))
		})

		It("should try to delete all flows before returning error", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1\nbr-rail2", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("rail_pod_id", fmt.Errorf("failed to get external id"))
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail2 test-pod-uid").
				Return("rail_pod_id", nil)
			execMock.EXPECT().Execute("ovs-ofctl del-flows br-rail2 cookie=0x5/-1").Return("", fmt.Errorf("failed to delete flows"))

			err := flows.DeletePodRailFlows(0x5, "test-pod-uid")
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to get external id"))
			Expect(err.Error()).Should(ContainSubstring("failed to delete flows"))
		})
	})

	Context("IsBridgeManagedByRailCNI", func() {
		It("should return true if bridge is managed by rail cni", func() {
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("rail_pod_id", nil)
			isManaged, err := flows.IsBridgeManagedByRailCNI("br-rail1", "test-pod-uid")
			Expect(err).Should(Succeed())
			Expect(isManaged).Should(BeTrue())
		})

		It("should return false if bridge is not managed by rail cni", func() {
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("", nil)
			isManaged, err := flows.IsBridgeManagedByRailCNI("br-rail1", "test-pod-uid")
			Expect(err).Should(Succeed())
			Expect(isManaged).Should(BeFalse())
		})

		It("should return error if failed to get external id", func() {
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1 test-pod-uid").
				Return("", fmt.Errorf("failed to get external id"))
			isManaged, err := flows.IsBridgeManagedByRailCNI("br-rail1", "test-pod-uid")
			Expect(err).Should(HaveOccurred())
			Expect(isManaged).Should(BeFalse())
		})
	})

	Context("CleanupStaleFlowsForBridges", func() {
		It("should cleanup stale flows for bridges", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1\nbr-rail2", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1").
				Return("test-pod-id=rail_pod_id", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail2").
				Return("test-pod-id=rail_pod_id", nil)
			execMock.EXPECT().
				Execute(fmt.Sprintf("ovs-ofctl del-flows br-rail1 cookie=0x%x/-1", GenerateUint64FromString("test-pod-id"))).
				Return("", nil)
			execMock.EXPECT().
				Execute(fmt.Sprintf("ovs-ofctl del-flows br-rail2 cookie=0x%x/-1", GenerateUint64FromString("test-pod-id"))).
				Return("", nil)
			execMock.EXPECT().Execute("ovs-vsctl remove bridge br-rail1 external_ids test-pod-id").Return("", nil)
			execMock.EXPECT().Execute("ovs-vsctl remove bridge br-rail2 external_ids test-pod-id").Return("", nil)
			err := flows.CleanupStaleFlowsForBridges(context.Background(), map[string]bool{})
			Expect(err).Should(Succeed())
		})

		It("should return error if failed to list bridges", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("", fmt.Errorf("failed to list bridges"))
			err := flows.CleanupStaleFlowsForBridges(context.Background(), map[string]bool{})
			Expect(err).Should(HaveOccurred())
		})

		It("should return error if failed to get external id", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1").
				Return("", fmt.Errorf("failed to get external id"))
			err := flows.CleanupStaleFlowsForBridges(context.Background(), map[string]bool{})
			Expect(err).Should(HaveOccurred())
		})

		It("should return error if failed to delete flows", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1").
				Return("test-pod-id=rail_pod_id", nil)
			execMock.EXPECT().
				Execute(fmt.Sprintf("ovs-ofctl del-flows br-rail1 cookie=0x%x/-1", GenerateUint64FromString("test-pod-id"))).
				Return("", fmt.Errorf("failed to delete flows"))

			err := flows.CleanupStaleFlowsForBridges(context.Background(), map[string]bool{})
			Expect(err).Should(HaveOccurred())
		})

		It("should return error if failed to clear external id", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1").
				Return("test-pod-id=rail_pod_id", nil)
			execMock.EXPECT().
				Execute(fmt.Sprintf("ovs-ofctl del-flows br-rail1 cookie=0x%x/-1", GenerateUint64FromString("test-pod-id"))).
				Return("", nil)
			execMock.EXPECT().Execute("ovs-vsctl remove bridge br-rail1 external_ids test-pod-id").
				Return("", fmt.Errorf("failed to clear external id"))
			err := flows.CleanupStaleFlowsForBridges(context.Background(), map[string]bool{})
			Expect(err).Should(HaveOccurred())
		})

		It("should succeed if there are no stale pods", func() {
			execMock.EXPECT().Execute("ovs-vsctl list-br").Return("br-rail1", nil)
			execMock.EXPECT().Execute("ovs-vsctl br-get-external-id br-rail1").
				Return("test-pod-id=rail_pod_id", nil)
			err := flows.CleanupStaleFlowsForBridges(context.Background(), map[string]bool{"test-pod-id": true})
			Expect(err).Should(Succeed())
		})
	})

	Context("GetBridgeNameFromPortName", func() {
		It("should return bridge name for port name", func() {
			execMock.EXPECT().Execute("ovs-vsctl port-to-br test-port").Return("br-rail1", nil)
			bridgeName, err := flows.GetBridgeNameFromPortName("test-port")
			Expect(err).Should(Succeed())
			Expect(bridgeName).Should(Equal("br-rail1"))
		})

		It("should return error if failed to get bridge name", func() {
			execMock.EXPECT().Execute("ovs-vsctl port-to-br test-port").Return("", fmt.Errorf("failed to get bridge name"))
			bridgeName, err := flows.GetBridgeNameFromPortName("test-port")
			Expect(err).Should(HaveOccurred())
			Expect(bridgeName).Should(BeEmpty())
		})
	})
})
