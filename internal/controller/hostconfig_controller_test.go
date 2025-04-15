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
	"fmt"

	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var _ = Describe("HostConfig Controller", func() {
	var (
		hostConfigController *HostConfigReconciler
		nodeName             = "host-1"
		ctx                  = context.Background()
		ns                   *corev1.Namespace
		execMock             *exec.MockAPI
		flowsMock            *MockFlowsAPI
		ctrl                 *gomock.Controller
		cm                   *corev1.ConfigMap
	)

	BeforeEach(func() {
		cm = &corev1.ConfigMap{}
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

		ctrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(ctrl)
		flowsMock = NewMockFlowsAPI(ctrl)

		hostConfigController = &HostConfigReconciler{
			Client:             k8sClient,
			NodeName:           nodeName,
			ConfigMapNamespace: ns.Name,
			ConfigMapName:      config.ConfigMapKey,
			Exec:               execMock,
			Flows:              flowsMock,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
		Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
	})

	It("invalid configmap", func() {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      config.ConfigMapKey,
				Namespace: ns.Name,
			},
			Data: map[string]string{
				"foo": `bar`,
			},
		}
		Expect(k8sClient.Create(ctx, cm)).Should(Succeed())

		res, err := hostConfigController.Reconcile(ctx, cm)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res.RequeueAfter).Should(BeZero())
	})

	It("host not found", func() {
		updateConfigMap(ctx, ns.Name, validConfig())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: ns.Name}, cm)).Should(Succeed())
		hostConfigController.NodeName = "not-existing-host"
		res, err := hostConfigController.Reconcile(ctx, cm)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res.RequeueAfter).Should(BeZero())
	})

	Context("single rail error", func() {
		BeforeEach(func() {
			updateConfigMap(ctx, ns.Name, validConfig())
			Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: ns.Name}, cm)).Should(Succeed())
			// rail2
			execMock.EXPECT().Execute("ovs-vsctl port-to-br eth1").Return("br-rel2", nil)
			flowsMock.EXPECT().DeleteBridgeDefaultFlows("br-rel2").Return(nil)
			flowsMock.EXPECT().AddHostRailFlows("br-rel2", "eth1", gomock.Any(), gomock.Any()).Return(nil)

		})

		It("port to bridge error", func() {
			// rail1
			execMock.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("", fmt.Errorf("error"))

			res, err := hostConfigController.Reconcile(ctx, cm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(res.RequeueAfter).ShouldNot(BeZero())
		})

		It("failed to delete bridge default flows", func() {
			// rail1
			execMock.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("br-rel1", nil)
			flowsMock.EXPECT().DeleteBridgeDefaultFlows("br-rel1").Return(fmt.Errorf("error"))

			res, err := hostConfigController.Reconcile(ctx, cm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(res.RequeueAfter).ShouldNot(BeZero())
		})

		It("failed to add host rail flows", func() {
			// rail1
			execMock.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("br-rel1", nil)
			flowsMock.EXPECT().DeleteBridgeDefaultFlows("br-rel1").Return(nil)
			flowsMock.EXPECT().AddHostRailFlows("br-rel1", "eth0", gomock.Any(), gomock.Any()).Return(fmt.Errorf("error"))

			res, err := hostConfigController.Reconcile(ctx, cm)
			Expect(err).ShouldNot(HaveOccurred())
			Expect(res.RequeueAfter).ShouldNot(BeZero())
		})
	})

	It("success", func() {
		updateConfigMap(ctx, ns.Name, validConfig())
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: cmName, Namespace: ns.Name}, cm)).Should(Succeed())

		// rail1
		execMock.EXPECT().Execute("ovs-vsctl port-to-br eth0").Return("br-rel1", nil)
		flowsMock.EXPECT().DeleteBridgeDefaultFlows("br-rel1").Return(nil)
		flowsMock.EXPECT().AddHostRailFlows("br-rel1", "eth0", gomock.Any(), gomock.Any()).Return(nil)

		// rail2
		execMock.EXPECT().Execute("ovs-vsctl port-to-br eth1").Return("br-rel2", nil)
		flowsMock.EXPECT().DeleteBridgeDefaultFlows("br-rel2").Return(nil)
		flowsMock.EXPECT().AddHostRailFlows("br-rel2", "eth1", gomock.Any(), gomock.Any()).Return(nil)

		res, err := hostConfigController.Reconcile(ctx, cm)
		Expect(err).ShouldNot(HaveOccurred())
		Expect(res.RequeueAfter).Should(BeZero())
	})
})
