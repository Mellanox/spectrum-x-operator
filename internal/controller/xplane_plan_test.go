// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	dmsv1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	"github.com/Mellanox/dms-lib/pkg/device"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	"github.com/Mellanox/spectrum-x-operator/pkg/config"
)

var _ = Describe("plan-sourced xplane configuration", func() {
	var (
		ns             *corev1.Namespace
		nodes          []corev1.Node
		params         *dmsv1.DMSPlanParams
		blueprint      *corev1.ConfigMap
		pool           *v1alpha2.SpectrumXRailPoolConfig
		r              *SpectrumXRailPoolConfigHostFlowsReconciler
		content        string
		operatorConfig *config.OperatorConfig
		roots          []string
	)
	BeforeEach(func() {
		schemaPath, err := filepath.Abs("testdata/dms/yang")
		Expect(err).NotTo(HaveOccurred())
		GinkgoT().Setenv("DMS_SCHEMA_PATH", schemaPath)
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "xplane-plan-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		DeferCleanup(func() { Expect(k8sClient.Delete(ctx, ns)).To(Succeed()) })
		nodes = []corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: ns.Name + "-one", Labels: map[string]string{"xplane-test": ns.Name}}},
			{ObjectMeta: metav1.ObjectMeta{Name: ns.Name + "-two", Labels: map[string]string{"xplane-test": ns.Name}}},
		}
		for i := range nodes {
			Expect(k8sClient.Create(ctx, &nodes[i])).To(Succeed())
			node := nodes[i]
			DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &node))).To(Succeed()) })
		}
		content = "operational_mode = \"spx-ra\"\n[interfaces]\nmtu = 9216\n[interfaces.groups.rail0]\npfs_plane_map = { eth0 = 0 }\nvf_rep = \"patch-xplane-to-br-rail-rail0\"\n"
		data, err := os.ReadFile("testdata/dms/blueprint.yaml")
		Expect(err).NotTo(HaveOccurred())
		var doc map[string]any
		Expect(yaml.Unmarshal(data, &doc)).To(Succeed())
		template := "spcx/ra24/xplane/xplane.toml.j2"
		doc["templates"] = map[string]any{template: content}
		feature := doc["features"].(map[string]any)["family"].(map[string]any)["runtime.yaml"].(map[string]any)
		feature["steps"] = append(feature["steps"].([]any), map[string]any{"type": "service", "service_name": "spcx-doca-xplane-boot", "artifacts": []any{map[string]any{"template": template, "install_to": "/opt/mellanox/doca/services/xplane/xplane.toml"}}})
		data, err = yaml.Marshal(doc)
		Expect(err).NotTo(HaveOccurred())
		blueprint = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "blueprint", Namespace: ns.Name}, Data: map[string]string{blueprintDataKey: string(data)}}
		Expect(k8sClient.Create(ctx, blueprint)).To(Succeed())
		params = &dmsv1.DMSPlanParams{ObjectMeta: metav1.ObjectMeta{Name: "plan", Namespace: ns.Name}, Spec: dmsv1.DMSPlanSpec{BlueprintRef: blueprint.Name, NodeSelector: map[string]string{"xplane-test": ns.Name}}}
		Expect(k8sClient.Create(ctx, params)).To(Succeed())
		pool = &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: ns.Name}, Spec: v1alpha2.SpectrumXRailPoolConfigSpec{NumVfs: 1, RailTopology: []v1alpha2.RailTopology{{Name: "rail0", MTU: 9216, NicSelector: v1alpha2.NicSelector{PfNames: []string{"eth0", "eth1"}}}}, DMSPlanParamsRef: params.Name, NodeSelector: map[string]string{"xplane-test": ns.Name}}}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		roots = []string{GinkgoT().TempDir(), GinkgoT().TempDir()}
		parent := &appsv1.DaemonSet{ObjectMeta: metav1.ObjectMeta{Name: "flowcontroller", Namespace: ns.Name}, Spec: appsv1.DaemonSetSpec{Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "flowcontroller"}}, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: map[string]string{"app": "flowcontroller"}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "flowcontroller", Image: "example.com/controller:test"}}}}}}
		Expect(k8sClient.Create(ctx, parent)).To(Succeed())
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "flowcontroller", Namespace: ns.Name, OwnerReferences: []metav1.OwnerReference{{APIVersion: "apps/v1", Kind: "DaemonSet", Name: parent.Name, UID: parent.UID, Controller: ptr.To(true)}}}, Spec: corev1.PodSpec{Containers: []corev1.Container{{Name: "flowcontroller", Image: "example.com/controller:test"}}}}
		Expect(k8sClient.Create(ctx, pod)).To(Succeed())
		r = &SpectrumXRailPoolConfigHostFlowsReconciler{Client: k8sClient, Scheme: scheme.Scheme, nodeName: nodes[0].Name, hostRoot: roots[0], ownPod: client.ObjectKeyFromObject(pod), discover: func(context.Context) (device.Inventory, error) { return planTestInventory(), nil }}
		operatorConfig = &config.OperatorConfig{XPlaneRepository: "example.com", XPlaneImage: "xplane", XPlaneVersion: "test"}
		workingDir, err := os.Getwd()
		Expect(err).NotTo(HaveOccurred())
		Expect(os.Chdir("../..")).To(Succeed())
		DeferCleanup(func() { Expect(os.Chdir(workingDir)).To(Succeed()) })
	})
	stage := func(index int) error {
		local := *r
		local.nodeName, local.hostRoot = nodes[index].Name, roots[index]
		return local.deployXplane(ctx, k8sClient, pool, scheme.Scheme, ns.Name, operatorConfig)
	}
	fetch := func() *appsv1.DaemonSet {
		ds := &appsv1.DaemonSet{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "xplane-ds", Namespace: ns.Name}, ds)).To(Succeed())
		return ds
	}
	configPath := func(ds *appsv1.DaemonSet) string {
		for _, volume := range ds.Spec.Template.Spec.Volumes {
			if volume.Name == "xplane-config" {
				Expect(volume.HostPath).NotTo(BeNil())
				Expect(*volume.HostPath.Type).To(Equal(corev1.HostPathFile))
				return volume.HostPath.Path
			}
		}
		Fail("xplane config file volume missing")
		return ""
	}
	It("keeps the original deployment for pools without a plan", func() {
		pool.Spec.DMSPlanParamsRef = ""
		Expect(stage(0)).To(Succeed())
		ds := fetch()
		Expect(ds.Spec.Template.Spec.Containers[0].Args).NotTo(ContainElement("--config"))
		for _, volume := range ds.Spec.Template.Spec.Volumes {
			Expect(volume.Name).NotTo(Equal("xplane-config"))
		}
	})
	It("restores the original workload when the plan reference is removed", func() {
		Expect(stage(0)).To(Succeed())
		pool.Spec.DMSPlanParamsRef = ""
		Expect(stage(0)).To(Succeed())
		ds := fetch()
		Expect(ds.Spec.Template.Spec.Containers[0].Args).NotTo(ContainElement("--config"))
		for _, volume := range ds.Spec.Template.Spec.Volumes {
			Expect(volume.Name).NotTo(Equal("xplane-config"))
		}
	})
	It("delivers the selected bytes on the local host without Node writes", func() {
		Expect(stage(0)).To(Succeed())
		ds := fetch()
		path := configPath(ds)
		Expect(path).To(ContainSubstring(string(pool.UID)))
		data, err := os.ReadFile(filepath.Join(roots[0], path))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(content))
		Expect(ds.Spec.Template.Spec.Containers[0].Args).To(Equal([]string{"--config", "/var/lib/spectrum-x/xplane.toml"}))
		// The first host may update the shared DaemonSet before a peer prepares.
		_, err = os.Stat(filepath.Join(roots[1], path))
		Expect(os.IsNotExist(err)).To(BeTrue())
		Expect(stage(1)).To(Succeed())
		peer, err := os.ReadFile(filepath.Join(roots[1], path))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(peer)).To(Equal(content))
		for _, node := range nodes {
			current := &corev1.Node{}
			Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(&node), current)).To(Succeed())
			Expect(current.Annotations).To(BeEmpty())
		}
		Expect(stage(0)).To(Succeed())
		Expect(fetch().ResourceVersion).To(Equal(ds.ResourceVersion))
	})
	It("writes a changed artifact for a new pod mount without changing the workload", func() {
		Expect(stage(0)).To(Succeed())
		before := fetch()
		blueprint.Data[blueprintDataKey] = strings.ReplaceAll(blueprint.Data[blueprintDataKey], "mtu = 9216", "mtu = 9000")
		Expect(k8sClient.Update(ctx, blueprint)).To(Succeed())
		Expect(stage(0)).To(Succeed())
		Expect(fetch().ResourceVersion).To(Equal(before.ResourceVersion))
		data, err := os.ReadFile(filepath.Join(roots[0], configPath(before)))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(strings.ReplaceAll(content, "9216", "9000")))
	})
	It("passes an authored OVSDB topology configuration through unchanged", func() {
		var doc map[string]any
		Expect(yaml.Unmarshal([]byte(blueprint.Data[blueprintDataKey]), &doc)).To(Succeed())
		ovsContent := "operational_mode = \"spx-ra\"\n"
		doc["templates"] = map[string]any{"spcx/ra24/xplane/xplane.toml.j2": ovsContent}
		data, err := yaml.Marshal(doc)
		Expect(err).NotTo(HaveOccurred())
		blueprint.Data[blueprintDataKey] = string(data)
		Expect(k8sClient.Update(ctx, blueprint)).To(Succeed())
		Expect(stage(0)).To(Succeed())
		written, err := os.ReadFile(filepath.Join(roots[0], configPath(fetch())))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(written)).To(Equal(ovsContent))
	})
	It("rejects invalid service input without changing the existing file or workload", func() {
		Expect(stage(0)).To(Succeed())
		before := fetch()
		blueprint.Data[blueprintDataKey] = strings.ReplaceAll(blueprint.Data[blueprintDataKey], "spcx-doca-xplane-boot", "another-service")
		Expect(k8sClient.Update(ctx, blueprint)).To(Succeed())
		Expect(stage(0)).To(MatchError(ContainSubstring("service not selected by plan")))
		Expect(fetch().ResourceVersion).To(Equal(before.ResourceVersion))
		data, err := os.ReadFile(filepath.Join(roots[0], configPath(before)))
		Expect(err).NotTo(HaveOccurred())
		Expect(string(data)).To(Equal(content))
	})
	It("rejects selector mismatch before changing the existing file", func() {
		Expect(stage(0)).To(Succeed())
		before := fetch()
		params.Spec.NodeSelector = map[string]string{"other": "pool"}
		Expect(k8sClient.Update(ctx, params)).To(Succeed())
		Expect(stage(0)).To(MatchError(ContainSubstring("does not select pool node")))
		Expect(fetch().ResourceVersion).To(Equal(before.ResourceVersion))
	})
	It("fails closed on local write failure and maps referenced input events", func() {
		Expect(r.xplanePlanInputToPools(ctx, params)).To(HaveLen(1))
		Expect(r.xplanePlanInputToPools(ctx, blueprint)).To(HaveLen(1))
		Expect(r.xplanePlanInputToPools(ctx, &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: ns.Name}})).To(BeEmpty())
		r.hostRoot = filepath.Join(GinkgoT().TempDir(), "not-a-directory")
		Expect(os.WriteFile(r.hostRoot, []byte("file"), 0o600)).To(Succeed())
		Expect(r.deployXplane(ctx, k8sClient, pool, scheme.Scheme, ns.Name, operatorConfig)).To(MatchError(ContainSubstring("write xplane configuration")))
		ds := &appsv1.DaemonSet{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "xplane-ds", Namespace: ns.Name}, ds)).NotTo(Succeed())
	})
})
