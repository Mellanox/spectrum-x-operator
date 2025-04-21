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
	"context"
	"errors"
	"net"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/vishvananda/netlink"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	mock_exec "github.com/Mellanox/spectrum-x-operator/pkg/exec"
	mock_netlink "github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink/mocks"
)

var _ = Describe("OvsIPaddressReconciler", func() {
	var (
		r           *OvsIPaddressReconciler
		mockCtrl    *gomock.Controller
		mockExec    *mock_exec.MockAPI
		mockNetlink *mock_netlink.MockNetlinkLib
		ctx         context.Context
		request     ctrl.Request
		ns          *corev1.Namespace
	)

	BeforeEach(func() {
		ctx = context.Background()
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())
		mockCtrl = gomock.NewController(GinkgoT())
		mockExec = mock_exec.NewMockAPI(mockCtrl)
		mockNetlink = mock_netlink.NewMockNetlinkLib(mockCtrl)

		r = &OvsIPaddressReconciler{
			NodeName:           "host-1",
			Exec:               mockExec,
			ConfigMapNamespace: ns.Name,
			ConfigMapName:      cmName,
			NetlinkLib:         mockNetlink,
			Client:             k8sClient,
		}

		request = ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      cmName,
			},
		}
	})

	AfterEach(func() {
		mockCtrl.Finish()
		Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
	})

	Describe("Reconcile", func() {
		It("should return without error if ConfigMap is not found", func() {
			result, err := r.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})
		It("should fail reconcile", func() {
			By("Reconciling the created config map with invalid json")
			updateConfigMap(ctx, ns.Name, "{{")
			result, err := r.Reconcile(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})
		It("should fail reconcile", func() {
			By("Reconciling the created config map with valid json - non existent node")
			updateConfigMap(ctx, ns.Name, validConfig())
			r.NodeName = "not-in-spec"
			result, err := r.Reconcile(ctx, request)
			Expect(err).To(HaveOccurred())
			Expect(result).To(Equal(ctrl.Result{}))
		})
		It("should succeed reconcile", func() {
			By("Reconciling the created config map with valid json")
			updateConfigMap(ctx, ns.Name, validConfig())
			// Port 1
			internalLink1 := &netlink.Dummy{}
			mockExec.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("br-0000_08_00.1", nil)
			mockNetlink.EXPECT().LinkByName("br-0000_08_00.1").Return(internalLink1, nil).AnyTimes()
			mockNetlink.EXPECT().IsLinkAdminStateUp(internalLink1).Return(false)
			mockNetlink.EXPECT().LinkSetUp(internalLink1).Return(nil)
			mockNetlink.EXPECT().IPv4Addresses(internalLink1).Return([]netlink.Addr{}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink1, "192.0.0.1/31").Return(nil)
			eth0Link := &netlink.Dummy{}
			mockNetlink.EXPECT().LinkByName("eth0").Return(eth0Link, nil).AnyTimes()
			mockNetlink.EXPECT().IPv4Addresses(eth0Link).Return([]netlink.Addr{
				{IPNet: &net.IPNet{IP: net.ParseIP("172.0.0.2"), Mask: net.CIDRMask(24, 32)}},
			}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink1, "172.0.0.2/24").Return(nil)
			mockNetlink.EXPECT().AddrDel(eth0Link, "172.0.0.2/24").Return(nil)
			// Port 2
			internalLink2 := &netlink.Dummy{}
			mockExec.EXPECT().Execute("ovs-vsctl port-to-br eth1").Return("br-0000_08_00.2", nil)
			mockNetlink.EXPECT().LinkByName("br-0000_08_00.2").Return(internalLink2, nil).AnyTimes()
			mockNetlink.EXPECT().IsLinkAdminStateUp(internalLink2).Return(false)
			mockNetlink.EXPECT().LinkSetUp(internalLink2).Return(nil)
			mockNetlink.EXPECT().IPv4Addresses(internalLink2).Return([]netlink.Addr{}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink2, "192.32.0.1/31").Return(nil)
			eth1Link := &netlink.Dummy{}
			mockNetlink.EXPECT().LinkByName("eth1").Return(eth1Link, nil).AnyTimes()
			mockNetlink.EXPECT().IPv4Addresses(eth1Link).Return([]netlink.Addr{
				{IPNet: &net.IPNet{IP: net.ParseIP("172.32.0.2"), Mask: net.CIDRMask(24, 32)}},
			}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink2, "172.32.0.2/24").Return(nil)
			mockNetlink.EXPECT().AddrDel(eth1Link, "172.32.0.2/24").Return(nil)
			result, err := r.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})
		It("should succeed reconcile - ip already exists on the bridge", func() {
			By("Reconciling the created config map with valid json")
			updateConfigMap(ctx, ns.Name, validConfig())
			// Port 1
			eth0Link := &netlink.Dummy{}
			internalLink1 := &netlink.Dummy{}
			mockExec.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("br-0000_08_00.1", nil)
			mockNetlink.EXPECT().LinkByName("br-0000_08_00.1").Return(internalLink1, nil).AnyTimes()
			mockNetlink.EXPECT().IsLinkAdminStateUp(internalLink1).Return(false)
			mockNetlink.EXPECT().LinkSetUp(internalLink1).Return(nil)
			By("ip already exists on the bridge and need to be removed from the physical port")
			eth0Addr := netlink.Addr{IPNet: &net.IPNet{IP: net.ParseIP("172.0.0.2"), Mask: net.CIDRMask(24, 32)}}
			mockNetlink.EXPECT().IPv4Addresses(internalLink1).Return([]netlink.Addr{eth0Addr}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink1, "192.0.0.1/31").Return(nil)
			mockNetlink.EXPECT().LinkByName("eth0").Return(eth0Link, nil).AnyTimes()
			mockNetlink.EXPECT().IPv4Addresses(eth0Link).Return([]netlink.Addr{eth0Addr}, nil)
			mockNetlink.EXPECT().AddrDel(eth0Link, "172.0.0.2/24").Return(nil)
			// Port 2
			internalLink2 := &netlink.Dummy{}
			mockExec.EXPECT().Execute("ovs-vsctl port-to-br eth1").Return("br-0000_08_00.2", nil)
			mockNetlink.EXPECT().LinkByName("br-0000_08_00.2").Return(internalLink2, nil).AnyTimes()
			mockNetlink.EXPECT().IsLinkAdminStateUp(internalLink2).Return(false)
			mockNetlink.EXPECT().LinkSetUp(internalLink2).Return(nil)
			mockNetlink.EXPECT().IPv4Addresses(internalLink2).Return([]netlink.Addr{}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink2, "192.32.0.1/31").Return(nil)
			eth1Link := &netlink.Dummy{}
			mockNetlink.EXPECT().LinkByName("eth1").Return(eth1Link, nil).AnyTimes()
			mockNetlink.EXPECT().IPv4Addresses(eth1Link).Return([]netlink.Addr{
				{IPNet: &net.IPNet{IP: net.ParseIP("172.32.0.2"), Mask: net.CIDRMask(24, 32)}},
			}, nil)
			mockNetlink.EXPECT().AddrAdd(internalLink2, "172.32.0.2/24").Return(nil)
			mockNetlink.EXPECT().AddrDel(eth1Link, "172.32.0.2/24").Return(nil)
			result, err := r.Reconcile(ctx, request)
			Expect(err).To(BeNil())
			Expect(result).To(Equal(ctrl.Result{}))
		})
	})

	Describe("processRailDeviceMapping", func() {
		It("should return error if bridge interface is not found", func() {
			mockExec.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("", errors.New("ovs-vsctl error"))

			err := r.processRailDeviceMapping(ctx, config.RailDeviceMapping{
				RailName: "test-rail",
				DevName:  "eth0",
			}, "192.168.1.1/24")

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get bridge for rail"))
		})
	})

	Describe("getIPsFromInterface", func() {
		It("should return IP addresses for a valid interface", func() {
			mockLink := &netlink.Dummy{}
			mockNetlink.EXPECT().LinkByName("eth0").Return(mockLink, nil)
			mockNetlink.EXPECT().IPv4Addresses(mockLink).Return([]netlink.Addr{
				{IPNet: &net.IPNet{IP: net.ParseIP("192.168.1.10"), Mask: net.CIDRMask(24, 32)}},
			}, nil)

			addrs, err := r.getIPsFromInterface("eth0")
			Expect(err).To(BeNil())
			Expect(addrs).To(HaveLen(1))
			Expect(addrs[0].IP.String()).To(Equal("192.168.1.10"))
		})

		It("should return error if interface is not found", func() {
			mockNetlink.EXPECT().LinkByName("eth0").Return(nil, errors.New("interface not found"))

			addrs, err := r.getIPsFromInterface("eth0")
			Expect(err).To(HaveOccurred())
			Expect(addrs).To(BeNil())
			Expect(err.Error()).To(ContainSubstring("failed to get interface eth0"))
		})
	})

	Describe("getRailGatewayIP", func() {
		It("should return gateway IP for a valid rail", func() {
			railsMap := map[string]config.HostRail{
				"rail1": {Network: "192.168.1.0/24"},
			}

			gwIP, err := r.getRailGatewayIP(railsMap, "rail1")
			Expect(err).To(BeNil())
			Expect(gwIP).To(Equal("192.168.1.1/24"))
		})

		It("should return error for a missing rail", func() {
			railsMap := map[string]config.HostRail{}

			gwIP, err := r.getRailGatewayIP(railsMap, "missing-rail")
			Expect(err).To(HaveOccurred())
			Expect(gwIP).To(BeEmpty())
			Expect(err.Error()).To(ContainSubstring("missing rail missing-rail config"))
		})
	})

	Describe("addIPToInterface", func() {
		It("should successfully add an IP to an interface", func() {
			mockLink := &netlink.Dummy{}
			mockNetlink.EXPECT().LinkByName("br0").Return(mockLink, nil)
			mockNetlink.EXPECT().AddrAdd(mockLink, "192.168.1.2/24").Return(nil)

			err := r.addIPToInterface("br0", "192.168.1.2/24")
			Expect(err).To(BeNil())
		})

		It("should return error if interface is missing", func() {
			mockNetlink.EXPECT().LinkByName("br0").Return(nil, errors.New("interface not found"))

			err := r.addIPToInterface("br0", "192.168.1.2/24")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get interface br0"))
		})
	})

	Describe("removeIPFromInterface", func() {
		It("should successfully remove an IP from an interface", func() {
			mockLink := &netlink.Dummy{}
			mockNetlink.EXPECT().LinkByName("eth0").Return(mockLink, nil)
			mockNetlink.EXPECT().AddrDel(mockLink, "192.168.1.3/24").Return(nil)

			err := r.removeIPFromInterface("eth0", "192.168.1.3/24")
			Expect(err).To(BeNil())
		})

		It("should return error if interface is missing", func() {
			mockNetlink.EXPECT().LinkByName("eth0").Return(nil, errors.New("interface not found"))

			err := r.removeIPFromInterface("eth0", "192.168.1.3/24")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("failed to get interface eth0"))
		})
	})
})
