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

	"github.com/Mellanox/spectrum-x-operator/pkg/exec"
	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("HostConfig Controller", func() {
	var (
		hostConfigController *HostConfigReconciler
		nodeName             = "host1"
		ctx                  = context.Background()
		ns                   *corev1.Namespace
		execMock             *exec.MockAPI
		ctrl                 *gomock.Controller
		cm                   *corev1.ConfigMap
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(k8sClient.Create(ctx, ns)).Should(Succeed())

		ctrl = gomock.NewController(GinkgoT())
		execMock = exec.NewMockAPI(ctrl)

		hostConfigController = &HostConfigReconciler{
			Client:             k8sClient,
			NodeName:           nodeName,
			ConfigMapNamespace: ns.Name,
			ConfigMapName:      "config",
			Exec:               execMock,
		}
	})

	AfterEach(func() {
		ctrl.Finish()
		Expect(k8sClient.Delete(ctx, cm)).Should(Succeed())
		Expect(k8sClient.Delete(ctx, ns)).Should(Succeed())
	})

	It("invalid configmap", func() {
		cm = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "config",
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

})
