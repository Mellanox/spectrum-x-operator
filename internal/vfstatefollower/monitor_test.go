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

package vfstatefollower

import (
	"context"
	"fmt"

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	spectrumxv1alpha2 "github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	mocksnetlink "github.com/Mellanox/spectrum-x-operator/pkg/lib/netlink/mocks"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	testNodeName = "test-node"
	testPF0      = "eth0"
	testPF1      = "eth1"
	testRep0     = "eth0_0"
	testRep1     = "eth0_1"
)

var _ = Describe("Monitor", func() {
	var (
		ctrl        *gomock.Controller
		netlinkMock *mocksnetlink.MockNetlinkLib
		monitor     *Monitor
		mockLink    *mocksnetlink.MockLink
	)

	BeforeEach(func() {
		ctrl = gomock.NewController(GinkgoT())
		netlinkMock = mocksnetlink.NewMockNetlinkLib(ctrl)
		mockLink = mocksnetlink.NewMockLink(ctrl)

		monitor = &Monitor{
			Client:   k8sClient,
			NodeName: testNodeName,
			Netlink:  netlinkMock,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Describe("allPFsNoCarrier", func() {
		It("returns true when single PF has NO-CARRIER", func() {
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(true)

			result, err := monitor.allPFsNoCarrier([]string{testPF0})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("returns false when single PF is up", func() {
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(false)

			result, err := monitor.allPFsNoCarrier([]string{testPF0})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("returns true when all PFs have NO-CARRIER", func() {
			mockLink1 := mocksnetlink.NewMockLink(ctrl)
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(true)
			netlinkMock.EXPECT().LinkByName(testPF1).Return(mockLink1, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink1).Return(true)

			result, err := monitor.allPFsNoCarrier([]string{testPF0, testPF1})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeTrue())
		})

		It("returns false when one PF is up in multi-PF topology", func() {
			mockLink1 := mocksnetlink.NewMockLink(ctrl)
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(true)
			netlinkMock.EXPECT().LinkByName(testPF1).Return(mockLink1, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink1).Return(false)

			result, err := monitor.allPFsNoCarrier([]string{testPF0, testPF1})
			Expect(err).NotTo(HaveOccurred())
			Expect(result).To(BeFalse())
		})

		It("returns error when PF link lookup fails", func() {
			netlinkMock.EXPECT().LinkByName(testPF0).Return(nil, fmt.Errorf("no such device"))

			_, err := monitor.allPFsNoCarrier([]string{testPF0})
			Expect(err).To(HaveOccurred())
		})
	})

	Describe("reconcileTopology", func() {
		var rt spectrumxv1alpha2.RailTopology

		BeforeEach(func() {
			rt = spectrumxv1alpha2.RailTopology{
				Name: "rail0",
				NicSelector: spectrumxv1alpha2.NicSelector{
					PfNames: []string{testPF0},
				},
			}
			monitor.FindActiveReps = func(pfName string) ([]string, error) {
				return []string{testRep0, testRep1}, nil
			}
		})

		Context("all PFs are NO-CARRIER", func() {
			BeforeEach(func() {
				netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
				netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(true)
			})

			It("sets reps down when they are admin-UP", func() {
				repLink0 := mocksnetlink.NewMockLink(ctrl)
				repLink1 := mocksnetlink.NewMockLink(ctrl)

				netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink0, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink0).Return(true)
				netlinkMock.EXPECT().LinkSetDown(repLink0).Return(nil)

				netlinkMock.EXPECT().LinkByName(testRep1).Return(repLink1, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink1).Return(true)
				netlinkMock.EXPECT().LinkSetDown(repLink1).Return(nil)

				Expect(monitor.reconcileTopology(context.Background(), rt)).To(Succeed())
			})

			It("does not call LinkSetDown when rep is already admin-DOWN (idempotent)", func() {
				repLink0 := mocksnetlink.NewMockLink(ctrl)
				netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink0, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink0).Return(false)

				repLink1 := mocksnetlink.NewMockLink(ctrl)
				netlinkMock.EXPECT().LinkByName(testRep1).Return(repLink1, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink1).Return(false)

				Expect(monitor.reconcileTopology(context.Background(), rt)).To(Succeed())
			})
		})

		Context("at least one PF is UP", func() {
			BeforeEach(func() {
				netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
				netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(false)
			})

			It("restores reps to UP when they are admin-DOWN", func() {
				repLink0 := mocksnetlink.NewMockLink(ctrl)
				repLink1 := mocksnetlink.NewMockLink(ctrl)

				netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink0, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink0).Return(false)
				netlinkMock.EXPECT().LinkSetUp(repLink0).Return(nil)

				netlinkMock.EXPECT().LinkByName(testRep1).Return(repLink1, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink1).Return(false)
				netlinkMock.EXPECT().LinkSetUp(repLink1).Return(nil)

				Expect(monitor.reconcileTopology(context.Background(), rt)).To(Succeed())
			})

			It("does not call LinkSetUp when rep is already admin-UP (idempotent)", func() {
				repLink0 := mocksnetlink.NewMockLink(ctrl)
				netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink0, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink0).Return(true)

				repLink1 := mocksnetlink.NewMockLink(ctrl)
				netlinkMock.EXPECT().LinkByName(testRep1).Return(repLink1, nil)
				netlinkMock.EXPECT().IsLinkAdminStateUp(repLink1).Return(true)

				Expect(monitor.reconcileTopology(context.Background(), rt)).To(Succeed())
			})
		})

		It("returns error when PF carrier check fails", func() {
			netlinkMock.EXPECT().LinkByName(testPF0).Return(nil, fmt.Errorf("no such device"))

			err := monitor.reconcileTopology(context.Background(), rt)
			Expect(err).To(HaveOccurred())
		})

		It("continues processing other reps when one rep link lookup fails", func() {
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(true)

			repLink1 := mocksnetlink.NewMockLink(ctrl)
			netlinkMock.EXPECT().LinkByName(testRep0).Return(nil, fmt.Errorf("link not found"))
			netlinkMock.EXPECT().LinkByName(testRep1).Return(repLink1, nil)
			netlinkMock.EXPECT().IsLinkAdminStateUp(repLink1).Return(true)
			netlinkMock.EXPECT().LinkSetDown(repLink1).Return(nil)

			Expect(monitor.reconcileTopology(context.Background(), rt)).To(Succeed())
		})
	})

	Describe("tick", func() {
		const testNamespace = "default"

		boolPtr := func(b bool) *bool { return &b }

		newRPC := func(name string, nodeSelector map[string]string, pfNames []string, vfStateFollow *bool) *spectrumxv1alpha2.SpectrumXRailPoolConfig {
			v := intstr.FromInt(1)
			return &spectrumxv1alpha2.SpectrumXRailPoolConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      name,
					Namespace: testNamespace,
				},
				Spec: spectrumxv1alpha2.SpectrumXRailPoolConfigSpec{
					NodeSelector:   nodeSelector,
					NumVfs:         4,
					MaxUnavailable: &v,
					VFStateFollow:  vfStateFollow,
					RailTopology: []spectrumxv1alpha2.RailTopology{{
						Name:        "rail0",
						NicSelector: spectrumxv1alpha2.NicSelector{PfNames: pfNames},
						MTU:         9000,
					}},
				},
			}
		}

		BeforeEach(func() {
			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   testNodeName,
					Labels: map[string]string{"node-role": "spectrum-x"},
				},
			}
			Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
			})

			rpc := newRPC("test-rpc", map[string]string{"node-role": "spectrum-x"}, []string{testPF0}, nil) // nil = default true
			Expect(k8sClient.Create(context.Background(), rpc)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(context.Background(), rpc)).To(Succeed())
			})

			monitor.FindActiveReps = func(pfName string) ([]string, error) {
				return []string{testRep0}, nil
			}
		})

		It("reconciles topologies for a matching node", func() {
			repLink := mocksnetlink.NewMockLink(ctrl)
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(true)
			netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink, nil)
			netlinkMock.EXPECT().IsLinkAdminStateUp(repLink).Return(true)
			netlinkMock.EXPECT().LinkSetDown(repLink).Return(nil)

			monitor.tick(context.Background())
		})

		It("skips CRs whose nodeSelector does not match the node", func() {
			nonMatchingRPC := newRPC("no-match-rpc", map[string]string{"node-role": "other"}, []string{"eth99"}, nil)
			Expect(k8sClient.Create(context.Background(), nonMatchingRPC)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(context.Background(), nonMatchingRPC)).To(Succeed())
			})

			// Only the matching RPC's PF should be checked — eth99 must not appear
			repLink := mocksnetlink.NewMockLink(ctrl)
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(false)
			netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink, nil)
			netlinkMock.EXPECT().IsLinkAdminStateUp(repLink).Return(true)

			monitor.tick(context.Background())
		})

		It("skips topology when VFStateFollow is explicitly false", func() {
			disabledRPC := newRPC("disabled-rpc", map[string]string{"node-role": "spectrum-x"}, []string{"eth99"}, boolPtr(false))
			Expect(k8sClient.Create(context.Background(), disabledRPC)).To(Succeed())
			DeferCleanup(func() {
				Expect(k8sClient.Delete(context.Background(), disabledRPC)).To(Succeed())
			})

			// Only the enabled RPC's PF (testPF0) should be checked; eth99 must not appear
			repLink := mocksnetlink.NewMockLink(ctrl)
			netlinkMock.EXPECT().LinkByName(testPF0).Return(mockLink, nil)
			netlinkMock.EXPECT().IsLinkNoCarrier(mockLink).Return(false)
			netlinkMock.EXPECT().LinkByName(testRep0).Return(repLink, nil)
			netlinkMock.EXPECT().IsLinkAdminStateUp(repLink).Return(true)

			monitor.tick(context.Background())
		})

		It("does not panic when node is not found", func() {
			monitor.NodeName = "nonexistent-node"
			DeferCleanup(func() { monitor.NodeName = testNodeName })
			// tick logs the error and returns; no expectations on netlinkMock
			monitor.tick(context.Background())
		})
	})
})
