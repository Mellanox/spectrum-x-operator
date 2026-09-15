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

package controller

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	sriovv1 "github.com/k8snetworkplumbingwg/sriov-network-operator/api/v1"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var planeIDRe = regexp.MustCompile(`external_ids:plane_id=(\d+)`)

var _ = Describe("createXPlaneBridges plane_id", func() {
	var (
		execMock   *exec.MockAPI
		mockCtrl   *gomock.Controller
		reconciler *SpectrumXRailPoolConfigHostFlowsReconciler
		commands   []string
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(mockCtrl)
		commands = nil
		execMock.EXPECT().Execute(gomock.Any()).DoAndReturn(func(cmd string) (string, error) {
			commands = append(commands, cmd)
			return "", nil
		}).AnyTimes()
		reconciler = NewSpectrumXRailPoolConfigHostFlowsReconciler(nil, nil, nil, execMock, nil, "test-node")
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	// uplinkPlaneIDs returns the plane_id values from the per-PF uplink add-port
	// commands, in the order the ports were created.
	uplinkPlaneIDs := func() []int {
		var ids []int
		for _, cmd := range commands {
			if !strings.Contains(cmd, "external_ids:xplane-uplink=true") {
				continue
			}
			match := planeIDRe.FindStringSubmatch(cmd)
			Expect(match).NotTo(BeNil(), "expected plane_id external_id in command: %s", cmd)
			id, err := strconv.Atoi(match[1])
			Expect(err).NotTo(HaveOccurred())
			ids = append(ids, id)
		}
		return ids
	}

	DescribeTable("computes plane_id as len(pfNames)*swPlane + pfName index",
		func(swPlane int, pfNames []string, wantPlaneIDs []int) {
			rt := &v1alpha2.RailTopology{
				Name: "rail-x",
				MTU:  9000,
				NicSelector: v1alpha2.NicSelector{
					PfNames: pfNames,
				},
				SwPlane: swPlane,
			}
			nodeState := &sriovv1.SriovNetworkNodeState{}

			Expect(reconciler.createXPlaneBridges(ctx, rt, nodeState)).To(Succeed())
			Expect(uplinkPlaneIDs()).To(Equal(wantPlaneIDs))
		},
		Entry("swPlane 0", 0, []string{"eth_rail1p1", "eth_rail1p2", "eth_rail1p3", "eth_rail1p4"}, []int{0, 1, 2, 3}),
		Entry("swPlane 1", 1, []string{"eth_rail1p5", "eth_rail1p6", "eth_rail1p7", "eth_rail1p8"}, []int{4, 5, 6, 7}),
		Entry("swPlane 3, two PFs", 3, []string{"eth_rail1p1", "eth_rail1p2"}, []int{6, 7}),
		Entry("unset swPlane defaults to 0", 0, []string{"eth_rail1p1"}, []int{0}),
	)
})
