/*
Copyright 2026 NVIDIA CORPORATION & AFFILIATES

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
	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	"github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/render"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("xplane GPU toleration", func() {
	It("creates and upgrades the DaemonSet without repeated updates", func() {
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "xplane-tolerations-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, ns)).To(Succeed()) })
		rpc := &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "xplane-tolerations", Namespace: ns.Name, UID: "xplane-tolerations"},
			Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
				NodeSelector: map[string]string{"xplane-test": "true"},
			},
		}
		data := render.MakeRenderData()
		data.Data["Namespace"] = ns.Name
		data.Data["Image"] = "example.com/xplane:test"
		data.Data["ImagePullSecrets"] = []string{}
		objects, err := render.RenderDir("../../manifests/state-xplane", &data)
		Expect(err).NotTo(HaveOccurred())

		for _, obj := range objects {
			if obj.GetKind() != DaemonSet {
				continue
			}
			By("creating a DaemonSet that tolerates GPU NoSchedule taints")
			Expect(syncDsObject(ctx, k8sClient, scheme.Scheme, rpc, obj)).To(Succeed())
			ds := &appsv1.DaemonSet{}
			key := client.ObjectKey{Namespace: ns.Name, Name: "xplane-ds"}
			Expect(k8sClient.Get(ctx, key, ds)).To(Succeed())
			expected := []v1.Toleration{{Key: "nvidia.com/gpu", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule}}
			Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(expected))
			Expect(ds.Spec.Template.Spec.NodeSelector).To(Equal(rpc.Spec.NodeSelector))

			By("upgrading an existing DaemonSet without the toleration")
			ds.Spec.Template.Spec.Tolerations = nil
			Expect(k8sClient.Update(ctx, ds)).To(Succeed())
			Expect(syncDsObject(ctx, k8sClient, scheme.Scheme, rpc, obj)).To(Succeed())
			Expect(k8sClient.Get(ctx, key, ds)).To(Succeed())
			Expect(ds.Spec.Template.Spec.Tolerations).To(Equal(expected))

			By("leaving the unchanged DaemonSet alone")
			version := ds.ResourceVersion
			Expect(syncDsObject(ctx, k8sClient, scheme.Scheme, rpc, obj)).To(Succeed())
			Expect(k8sClient.Get(ctx, key, ds)).To(Succeed())
			Expect(ds.ResourceVersion).To(Equal(version))
			return
		}
		Fail("xplane manifests did not contain a DaemonSet")
	})
})
