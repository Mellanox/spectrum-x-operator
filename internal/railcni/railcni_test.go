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
	"io"
	"net"

	"log/slog"

	"github.com/Mellanox/spectrum-x-operator/internal/controller"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	"github.com/containernetworking/cni/pkg/skel"
	current "github.com/containernetworking/cni/pkg/types/100"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("RailCNI", func() {
	var (
		railCNI   *RailCNI
		mockExec  *exec.MockAPI
		mockFlows *controller.MockFlowsAPI
		ctrl      *gomock.Controller
		logger    *slog.Logger
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		mockExec = exec.NewMockAPI(ctrl)
		mockFlows = controller.NewMockFlowsAPI(ctrl)
		logger = slog.New(slog.NewTextHandler(io.Discard, nil))
		railCNI = &RailCNI{
			Log:   logger,
			Exec:  mockExec,
			Flows: mockFlows,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Add", func() {
		var (
			args       *skel.CmdArgs
			prevResult *current.Result
		)

		BeforeEach(func() {
			// Setup common test data
			prevResult = &current.Result{
				CNIVersion: "1.0.0",
				Interfaces: []*current.Interface{
					{
						Name:    "eth0",
						Sandbox: "",
					},
					{
						Name:    "eth1",
						Sandbox: "sandbox",
					},
				},
				IPs: []*current.IPConfig{
					{
						Address: net.IPNet{
							IP:   net.ParseIP("192.168.1.2"),
							Mask: net.CIDRMask(24, 32),
						},
					},
				},
			}

			// Convert prevResult to raw bytes
			prevResultBytes, err := json.Marshal(prevResult)
			Expect(err).NotTo(HaveOccurred())

			// Create a map with the prevResult
			prevResultMap := map[string]interface{}{
				"cniVersion": "1.0.0",
				"prevResult": json.RawMessage(prevResultBytes),
				"ovsBridge":  "br0",
			}

			// Convert the map to JSON
			netConfBytes, err := json.Marshal(prevResultMap)
			Expect(err).NotTo(HaveOccurred())

			args = &skel.CmdArgs{
				ContainerID: "test-container",
				Netns:       "test-netns",
				IfName:      "eth0",
				Args:        "K8S_POD_UID=test-pod-uid",
				StdinData:   netConfBytes,
			}
		})

		It("should successfully add pod rail flows", func() {
			// Setup mock expectations
			mockExec.EXPECT().
				Execute("ovs-vsctl port-to-br eth0").
				Return("br0", nil)

			mockExec.EXPECT().
				Execute("ovs-vsctl set bridge br0 external_id:test-pod-uid=rail_pod_id").
				Return("", nil)

			mockFlows.EXPECT().
				AddPodRailFlows(gomock.Any(), "eth0", "br0", "192.168.1.2").
				Return(nil)

			err := railCNI.Add(args)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should fail on invalid args", func() {
			mockExec.EXPECT().
				Execute("ovs-vsctl port-to-br eth0").
				Return("br0", nil)

			args.Args = "invalid"
			err := railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to load args"))
		})

		It("should return error when bridge lookup fails", func() {
			mockExec.EXPECT().
				Execute("ovs-vsctl port-to-br eth0").
				Return("", fmt.Errorf("bridge lookup failed"))

			err := railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get bridge to vf"))
		})

		It("should return error when failed to set pod external id", func() {
			mockExec.EXPECT().
				Execute("ovs-vsctl port-to-br eth0").
				Return("br0", nil)

			mockExec.EXPECT().
				Execute("ovs-vsctl set bridge br0 external_id:test-pod-uid=rail_pod_id").
				Return("", fmt.Errorf("failed to set pod external id"))

			err := railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to set pod external id"))
		})

		It("should return error when adding pod rail flows fails", func() {
			mockExec.EXPECT().
				Execute("ovs-vsctl port-to-br eth0").
				Return("br0", nil)

			mockExec.EXPECT().
				Execute("ovs-vsctl set bridge br0 external_id:test-pod-uid=rail_pod_id").
				Return("", nil)

			mockFlows.EXPECT().
				AddPodRailFlows(gomock.Any(), "eth0", "br0", "192.168.1.2").
				Return(fmt.Errorf("failed to add flows"))

			err := railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to add pod rail flows"))
		})

		It("should return error when no VF is found", func() {
			// Modify prevResult to have no VF interface
			prevResult.Interfaces = []*current.Interface{
				{
					Name:    "eth1",
					Sandbox: "sandbox",
				},
			}

			// Convert prevResult to raw bytes
			prevResultBytes, err := json.Marshal(prevResult)
			Expect(err).NotTo(HaveOccurred())

			// Create a map with the prevResult
			prevResultMap := map[string]interface{}{
				"cniVersion": "1.0.0",
				"prevResult": json.RawMessage(prevResultBytes),
				"ovsBridge":  "br0",
			}

			// Convert the map to JSON
			netConfBytes, err := json.Marshal(prevResultMap)
			Expect(err).NotTo(HaveOccurred())
			args.StdinData = netConfBytes

			err = railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no vf found"))
		})

		It("should return error when multiple IPs are present", func() {
			// Add multiple IPs to prevResult
			prevResult.IPs = append(prevResult.IPs, &current.IPConfig{
				Address: net.IPNet{
					IP:   net.ParseIP("192.168.1.3"),
					Mask: net.CIDRMask(24, 32),
				},
			})

			// Convert prevResult to raw bytes
			prevResultBytes, err := json.Marshal(prevResult)
			Expect(err).NotTo(HaveOccurred())

			// Create a map with the prevResult
			prevResultMap := map[string]interface{}{
				"cniVersion": "1.0.0",
				"prevResult": json.RawMessage(prevResultBytes),
				"ovsBridge":  "br0",
			}

			// Convert the map to JSON
			netConfBytes, err := json.Marshal(prevResultMap)
			Expect(err).NotTo(HaveOccurred())
			args.StdinData = netConfBytes

			// Setup mock expectations
			mockExec.EXPECT().
				Execute("ovs-vsctl port-to-br eth0").
				Return("br0", nil)

			mockExec.EXPECT().
				Execute("ovs-vsctl set bridge br0 external_id:test-pod-uid=rail_pod_id").
				Return("", nil)

			err = railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("expected single ip"))
		})

		It("should return error when CNI config is invalid", func() {
			args.StdinData = []byte("invalid json")
			err := railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to parse config"))
		})

		It("should return error when not called as chained plugin", func() {
			// Create a map without prevResult
			prevResultMap := map[string]interface{}{
				"cniVersion": "1.0.0",
				"ovsBridge":  "br0",
			}

			// Convert the map to JSON
			netConfBytes, err := json.Marshal(prevResultMap)
			Expect(err).NotTo(HaveOccurred())
			args.StdinData = netConfBytes

			err = railCNI.Add(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("must be called as chained plugin"))
		})
	})
})
