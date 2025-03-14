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
		ctrl           *gomock.Controller
		pod            *corev1.Pod
	)

	defaultNetStatus := []netdefv1.NetworkStatus{
		{
			Name:      "default/ovs-nic-1",
			Interface: "net1",
			IPs:       []string{"172.0.0.2"},
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
	}

	BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

		ctrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(ctrl)

		flowController = &FlowReconciler{
			NodeName:           "host1",
			ConfigMapNamespace: ns.Name,
			ConfigMapName:      "config",
			Client:             k8sClient,
			Exec:               execMock,
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

	Context("When reconciling a resource", func() {

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

		It("valid network annotaion, no topolgy mapping", func() {
			netStatusStr, err := json.Marshal(defaultNetStatus)
			Expect(err).Should(BeNil())
			pod.Annotations = map[string]string{netdefv1.NetworkStatusAnnot: string(netStatusStr)}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

			result, err := flowController.Reconcile(ctx, pod)
			Expect(err).Should(Succeed())
			Expect(result).To(Equal(reconcile.Result{RequeueAfter: 5 * time.Second}))
		})

		// TODO: implement
		It("pod deleted", func() {

		})
	})
})
