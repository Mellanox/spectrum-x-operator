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
	"fmt"
	"os"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	"github.com/Mellanox/spectrum-x-operator/pkg/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ = Describe("xplane DaemonSet toleration updates", func() {
	It("deploys, updates, and removes parent tolerations against the Kubernetes API", func() {
		workingDir, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Chdir("../..")).To(Succeed())
		DeferCleanup(func() { Expect(os.Chdir(workingDir)).To(Succeed()) })
		ns := &v1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "xplane-tolerations-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, ns)).To(Succeed()) })
		rpc := &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: ns.Name, UID: types.UID("test-pool-uid")},
			Spec:       v1alpha2.SpectrumXRailPoolConfigSpec{NodeSelector: map[string]string{"pool": "gpu"}},
		}
		parent := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "renamed-flowcontroller", Namespace: ns.Name},
			Spec: appsv1.DaemonSetSpec{
				Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "flowcontroller"}},
				Template: v1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "flowcontroller"}},
					Spec:       v1.PodSpec{Containers: []v1.Container{{Name: "flowcontroller", Image: "example.com/controller:test"}}},
				},
			},
		}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		controller := true
		pod := &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{Name: "flowcontroller-pod", Namespace: ns.Name,
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: parent.Name, UID: parent.UID, Controller: &controller}},
			},
			Spec: v1.PodSpec{Containers: []v1.Container{{Name: "flowcontroller", Image: "example.com/controller:test"}}},
		}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		r := &SpectrumXRailPoolConfigHostFlowsReconciler{Client: k8sClient, Scheme: scheme.Scheme, ownPod: client.ObjectKeyFromObject(pod)}
		operatorConfig := &config.OperatorConfig{XPlaneRepository: "example.com", XPlaneImage: "xplane", XPlaneVersion: "test", ImagePullSecrets: []string{"registry-secret"}}
		deploy := func() {
			Expect(r.deployXplane(ctx, k8sClient, rpc, scheme.Scheme, ns.Name, operatorConfig)).To(Succeed())
		}
		deploy()
		stored := &appsv1.DaemonSet{}
		key := types.NamespacedName{Name: "xplane-ds", Namespace: ns.Name}
		Expect(k8sClient.Get(ctx, key, stored)).To(Succeed())
		Expect(stored.Spec.Template.Spec.Tolerations).To(BeEmpty())

		seconds := int64(45)
		parent.Spec.Template.Spec.Tolerations = []v1.Toleration{
			{Key: "nvidia.com/gpu", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
			{Key: "dedicated", Operator: v1.TolerationOpEqual, Value: "gpu", Effect: v1.TaintEffectNoExecute, TolerationSeconds: &seconds},
		}
		Expect(k8sClient.Update(ctx, parent)).To(Succeed())
		deploy()
		Expect(k8sClient.Get(ctx, key, stored)).To(Succeed())
		Expect(stored.Spec.Template.Spec.Tolerations).To(Equal(parent.Spec.Template.Spec.Tolerations))
		Expect(stored.Spec.Template.Spec.NodeSelector).To(Equal(rpc.Spec.NodeSelector))
		Expect(stored.Spec.Template.Spec.Containers[0].Image).To(Equal("example.com/xplane:test"))
		Expect(stored.Spec.Template.Spec.ImagePullSecrets).To(Equal([]v1.LocalObjectReference{{Name: "registry-secret"}}))
		Expect(stored.OwnerReferences).To(HaveLen(1))
		Expect(stored.OwnerReferences[0].UID).To(Equal(rpc.UID))

		parent.Spec.Template.Spec.Tolerations = nil
		Expect(k8sClient.Update(ctx, parent)).To(Succeed())
		deploy()
		Expect(k8sClient.Get(ctx, key, stored)).To(Succeed())
		Expect(stored.Spec.Template.Spec.Tolerations).To(BeEmpty())
		version := stored.ResourceVersion
		deploy()
		Expect(k8sClient.Get(ctx, key, stored)).To(Succeed())
		Expect(stored.ResourceVersion).To(Equal(version))
	})
})

var _ = Describe("flowcontroller parent tolerations", func() {
	var (
		r      *SpectrumXRailPoolConfigHostFlowsReconciler
		parent *appsv1.DaemonSet
		pod    *v1.Pod
	)

	BeforeEach(func() {
		parent = &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "renamed-flowcontroller", Namespace: "operator-ns", UID: types.UID("parent-uid")},
			Spec: appsv1.DaemonSetSpec{Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Tolerations: []v1.Toleration{
				{Key: "nvidia.com/gpu", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule},
			}}}},
		}
		controller := true
		pod = &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name: "flowcontroller-pod", Namespace: parent.Namespace,
				OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: parent.Name, UID: parent.UID, Controller: &controller}},
			},
			Spec: v1.PodSpec{Tolerations: []v1.Toleration{{Operator: v1.TolerationOpExists}}},
		}
		r = &SpectrumXRailPoolConfigHostFlowsReconciler{
			Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(parent, pod).Build(),
			Scheme: scheme.Scheme,
			ownPod: client.ObjectKeyFromObject(pod),
		}
	})

	It("copies the actual parent template rather than admission-added pod tolerations", func() {
		seconds := int64(45)
		parent.Spec.Template.Spec.Tolerations = append(parent.Spec.Template.Spec.Tolerations,
			v1.Toleration{Key: "custom", Operator: v1.TolerationOpEqual, Value: "gpu", Effect: v1.TaintEffectNoExecute, TolerationSeconds: &seconds})
		Expect(r.Update(ctx, parent)).To(Succeed())
		tolerations, err := r.flowcontrollerTolerations(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(tolerations).To(Equal(parent.Spec.Template.Spec.Tolerations))
	})

	It("accepts a parent with no tolerations", func() {
		parent.Spec.Template.Spec.Tolerations = nil
		Expect(r.Update(ctx, parent)).To(Succeed())
		tolerations, err := r.flowcontrollerTolerations(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(tolerations).To(BeEmpty())
	})

	It("preserves an existing xplane DaemonSet when reading its source fails", func() {
		existing := &appsv1.DaemonSet{
			ObjectMeta: metav1.ObjectMeta{Name: "xplane-ds", Namespace: "pool-ns"},
			Spec:       appsv1.DaemonSetSpec{Template: v1.PodTemplateSpec{Spec: v1.PodSpec{Tolerations: []v1.Toleration{{Key: "keep", Operator: v1.TolerationOpExists}}}}},
		}
		Expect(r.Create(ctx, existing)).To(Succeed())
		Expect(r.Delete(ctx, parent)).To(Succeed())
		pool := &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: existing.Namespace}}
		Expect(r.deployXplane(ctx, r.Client, pool, scheme.Scheme, pool.Namespace, &config.OperatorConfig{})).NotTo(Succeed())
		stored := &appsv1.DaemonSet{}
		Expect(r.Get(ctx, client.ObjectKeyFromObject(existing), stored)).To(Succeed())
		Expect(stored.Spec).To(Equal(existing.Spec))
	})

	DescribeTable("rejects invalid parent identity",
		func(change func()) {
			change()
			_, err := r.flowcontrollerTolerations(ctx)
			Expect(err).To(HaveOccurred())
		},
		Entry("missing own pod identity", func() { r.ownPod = types.NamespacedName{} }),
		Entry("missing pod", func() { Expect(r.Delete(ctx, pod)).To(Succeed()) }),
		Entry("no controlling owner", func() { pod.OwnerReferences = nil; Expect(r.Update(ctx, pod)).To(Succeed()) }),
		Entry("a different controlling kind", func() { pod.OwnerReferences[0].Kind = "ReplicaSet"; Expect(r.Update(ctx, pod)).To(Succeed()) }),
		Entry("a different owner API", func() { pod.OwnerReferences[0].APIVersion = "example.com/v1"; Expect(r.Update(ctx, pod)).To(Succeed()) }),
		Entry("missing parent", func() { Expect(r.Delete(ctx, parent)).To(Succeed()) }),
		Entry("parent recreated with another UID", func() {
			Expect(r.Delete(ctx, parent)).To(Succeed())
			parent.ResourceVersion = ""
			parent.UID = types.UID("replacement-uid")
			Expect(r.Create(ctx, parent)).To(Succeed())
		}),
	)

	It("maps only its parent to multiplane pools across namespaces", func() {
		for i, entry := range []struct {
			namespace string
			pfs       []string
		}{
			{"operator-ns", []string{"eth0", "eth1"}},
			{"another-ns", []string{"eth2", "eth3"}},
			{"another-ns", []string{"eth4"}},
		} {
			pool := &v1alpha2.SpectrumXRailPoolConfig{
				ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("pool-%d", i), Namespace: entry.namespace},
				Spec:       v1alpha2.SpectrumXRailPoolConfigSpec{RailTopology: []v1alpha2.RailTopology{{NicSelector: v1alpha2.NicSelector{PfNames: entry.pfs}}}},
			}
			Expect(r.Create(ctx, pool)).To(Succeed())
		}
		Expect(r.flowcontrollerToRailConfigs(ctx, parent)).To(ConsistOf(
			reconcile.Request{NamespacedName: types.NamespacedName{Name: "pool-0", Namespace: "operator-ns"}},
			reconcile.Request{NamespacedName: types.NamespacedName{Name: "pool-1", Namespace: "another-ns"}},
		))
		other := parent.DeepCopy()
		other.Name = "unrelated"
		Expect(r.flowcontrollerToRailConfigs(ctx, other)).To(BeEmpty())
		other = parent.DeepCopy()
		other.UID = types.UID("another-uid")
		Expect(r.flowcontrollerToRailConfigs(ctx, other)).To(BeEmpty())
		other = parent.DeepCopy()
		other.Namespace = "another-ns"
		Expect(r.flowcontrollerToRailConfigs(ctx, other)).To(BeEmpty())
	})

	It("enqueues toleration changes and removals while ignoring unrelated updates", func() {
		filter := flowcontrollerTolerationsChanged()
		updated := parent.DeepCopy()
		updated.Status.NumberReady = 4
		Expect(filter.Update(event.UpdateEvent{ObjectOld: parent, ObjectNew: updated})).To(BeFalse())
		updated.Spec.Template.Spec.Containers = []v1.Container{{Name: "controller", Image: "example.com/controller:new"}}
		Expect(filter.Update(event.UpdateEvent{ObjectOld: parent, ObjectNew: updated})).To(BeFalse())
		updated.Spec.Template.Spec.Tolerations = nil
		Expect(filter.Update(event.UpdateEvent{ObjectOld: parent, ObjectNew: updated})).To(BeTrue())
		Expect(filter.Create(event.CreateEvent{Object: parent})).To(BeTrue())
		Expect(filter.Delete(event.DeleteEvent{Object: parent})).To(BeTrue())
	})
})
