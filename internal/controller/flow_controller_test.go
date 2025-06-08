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
	"encoding/json"
	"fmt"
	"time"

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"

	gomock "github.com/golang/mock/gomock"
	netdefv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("Pod Controller", func() {
	var (
		flowController *FlowReconciler
		nodeName       = "host1"
		ctx            = context.Background()
		ns             *corev1.Namespace
		execMock       *exec.MockAPI
		flowsMock      *MockFlowsAPI
		ctrl           *gomock.Controller
		pod            *corev1.Pod
	)

	defaultNetStatus := []netdefv1.NetworkStatus{
		{
			Name:      "default/ovs-nic-1",
			Interface: "net1",
			IPs:       []string{"192.0.0.2"},
			Mac:       "82:90:d3:0a:48:88",
			DeviceInfo: &netdefv1.DeviceInfo{
				Type:    "pci",
				Version: "1.1.0",
				Pci: &netdefv1.PciDevice{
					PciAddress: "0000:08:00.2",
					RdmaDevice: "mlx5_2",
				},
			},
		},
		{
			Name:      "default/ovs-nic-1",
			Interface: "net2",
			IPs:       []string{"192.32.0.2"},
			Mac:       "82:90:d3:0a:48:89",
			DeviceInfo: &netdefv1.DeviceInfo{
				Type:    "pci",
				Version: "1.1.0",
				Pci: &netdefv1.PciDevice{
					PciAddress: "0000:08:00.3",
					RdmaDevice: "mlx5_3",
				},
			},
		},
	}

	BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

		ctrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(ctrl)
		flowsMock = NewMockFlowsAPI(ctrl)

		flowController = &FlowReconciler{
			NodeName: "host-1",
			Client:   k8sClient,
			Exec:     execMock,
			Flows:    flowsMock,
		}

		// default pod config
		pod = &corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "pod1",
				Namespace: ns.Name,
			},
			Spec: corev1.PodSpec{
				NodeName: nodeName,
				Containers: []corev1.Container{
					{
						Name:  "app",
						Image: "image",
					},
				},
			},
		}

	})

	AfterEach(func() {
		ctrl.Finish()
		Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
	})

	It("no annotations", func() {
		Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
		result, err := flowController.Reconcile(ctx, pod)
		Expect(err).Should(Succeed())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("no network annotations", func() {
		pod.Annotations = map[string]string{}
		Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
		result, err := flowController.Reconcile(ctx, pod)
		Expect(err).Should(Succeed())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("invalid network annotations", func() {
		pod.Annotations = map[string]string{netdefv1.NetworkStatusAnnot: "this is not a json :)"}
		Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
		result, err := flowController.Reconcile(ctx, pod)
		Expect(err).Should(Succeed())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	It("no relevant network status for the pod", func() {
		netStatus := []netdefv1.NetworkStatus{
			{
				Name:      "cbr0",
				Interface: "eth0",
				IPs:       []string{"10.244.0.14"},
				Mac:       "1a:6b:48:10:db:2f",
				Default:   true,
			},
		}

		netStatusStr, err := json.Marshal(netStatus)
		Expect(err).Should(BeNil())
		pod.Annotations = map[string]string{netdefv1.NetworkStatusAnnot: string(netStatusStr)}
		Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

		result, err := flowController.Reconcile(ctx, pod)
		Expect(err).Should(Succeed())
		Expect(result).To(Equal(reconcile.Result{}))
	})

	Context("pod deleted", func() {
		BeforeEach(func() {
			netStatusStr, err := json.Marshal(defaultNetStatus)
			Expect(err).Should(BeNil())
			pod.Annotations = map[string]string{netdefv1.NetworkStatusAnnot: string(netStatusStr)}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

			pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
		})

		It("delete flows on pod deletion", func() {
			flowsMock.EXPECT().DeletePodRailFlows(gomock.Any(), gomock.Any()).Return(nil).Times(1)
			pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(Succeed())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		It("failed to delete flows on pod deletion - do not fail reconciliation", func() {
			flowsMock.EXPECT().DeletePodRailFlows(gomock.Any(), gomock.Any()).Return(fmt.Errorf("failed to delete flows"))
			pod.DeletionTimestamp = &metav1.Time{Time: time.Now()}
			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(Succeed())
			Expect(result).To(Equal(reconcile.Result{}))
		})
	})

	Context("valid config", func() {
		BeforeEach(func() {
			netStatusStr, err := json.Marshal(defaultNetStatus)
			Expect(err).Should(BeNil())
			pod.Annotations = map[string]string{netdefv1.NetworkStatusAnnot: string(netStatusStr)}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())
		})

		It("success", func() {
			// rail1
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net1 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-1", nil).Times(1)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-1").Return("br-rail1", nil).Times(1)
			flowsMock.EXPECT().AddPodRailFlows(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			// rail2
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net2 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-2", nil)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-2").Return("br-rail2", nil)
			flowsMock.EXPECT().AddPodRailFlows(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(Succeed())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		It("fail to get vf rep", func() {
			// rail1
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net1 external_ids:contPodUid="+string(pod.UID)).
				Return("", fmt.Errorf("failed to get vf rep"))

			// rail2
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net2 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-2", nil)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-2").Return("br-rail2", nil)
			flowsMock.EXPECT().AddPodRailFlows(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		It("failed to get bridge for vf rep", func() {
			// rail1
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net1 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-1", nil)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-1").Return("", fmt.Errorf("failed to get iface to bridge"))

			// rail2
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net2 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-2", nil)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-2").Return("br-rail2", nil)

			flowsMock.EXPECT().AddPodRailFlows(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})

		It("failed to add flows to rail", func() {
			// rail1
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net1 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-1", nil).Times(1)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-1").Return("br-rail1", nil).Times(1)
			flowsMock.EXPECT().AddPodRailFlows(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("failed to add flows to rail"))

			// rail2
			execMock.EXPECT().
				Execute("ovs-vsctl --no-heading --columns=name find Port external_ids:contIface=net2 external_ids:contPodUid="+string(pod.UID)).
				Return("pod-vf-2", nil)
			execMock.EXPECT().Execute("ovs-vsctl iface-to-br pod-vf-2").Return("br-rail2", nil)
			flowsMock.EXPECT().AddPodRailFlows(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)

			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(HaveOccurred())
			Expect(result).To(Equal(reconcile.Result{}))
		})

	})
})
