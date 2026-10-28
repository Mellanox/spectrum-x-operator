// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"

	dmsv1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	"github.com/Mellanox/dms-lib/pkg/device"
	"github.com/Mellanox/dms-lib/pkg/plan"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

func planTestInventory() device.Inventory {
	return device.Inventory{Complete: true, Issues: []device.DiscoveryIssue{}, Devices: []device.Device{{
		ID: "0000:03:00", Type: "1023", SerialNumber: "shared", PartNumber: "", PSID: "", FirmwareVersion: "",
		ModelName: "ConnectX-8", DPU: false, SuperNIC: true, NetworkBay: nil,
		Ports: []device.Port{{
			PCI: "0000:03:00.0", Netdev: "eth0", RDMA: "mlx5_0", Fwctl: "",
			PhysicalPortIndex: ptr.To(0), PFIndex: ptr.To(0), UplinkRepresentor: "", VFs: []device.VirtualFunction{},
		}},
	}}}
}

var _ = Describe("DMSPlanParams host resolution", func() {
	var (
		ns        *corev1.Namespace
		nodes     []corev1.Node
		params    *dmsv1.DMSPlanParams
		cm        *corev1.ConfigMap
		r         *DMSPlanParamsReconciler
		blueprint []byte
	)
	BeforeEach(func() {
		GinkgoT().Setenv("DMS_SCHEMA_PATH", "testdata/dms/yang")
		var err error
		blueprint, err = os.ReadFile("testdata/dms/blueprint.yaml")
		Expect(err).NotTo(HaveOccurred())
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "dms-plan-"}}
		Expect(k8sClient.Create(ctx, ns)).To(Succeed())
		nodes = []corev1.Node{
			{ObjectMeta: metav1.ObjectMeta{Name: ns.Name + "-one", Labels: map[string]string{"dms-test": ns.Name, "enabled": "true"}}},
			{ObjectMeta: metav1.ObjectMeta{Name: ns.Name + "-two", Labels: map[string]string{"dms-test": ns.Name, "enabled": "true"}}},
		}
		for index := range nodes {
			Expect(k8sClient.Create(ctx, &nodes[index])).To(Succeed())
		}
		cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: "blueprint", Namespace: ns.Name}, Data: map[string]string{blueprintDataKey: string(blueprint)}}
		Expect(k8sClient.Create(ctx, cm)).To(Succeed())
		params = &dmsv1.DMSPlanParams{ObjectMeta: metav1.ObjectMeta{Name: "plan", Namespace: ns.Name}, Spec: dmsv1.DMSPlanSpec{
			NodeSelector: map[string]string{"dms-test": ns.Name, "enabled": "true"}, BlueprintRef: cm.Name,
			Profile: "sample", PlatformType: "test", Planes: nil, Overlay: "", SPXGS: nil, Parameters: map[string]string{},
		}}
		Expect(k8sClient.Create(ctx, params)).To(Succeed())
		r = NewDMSPlanParamsReconciler(k8sClient, k8sClient, nodes[0].Name)
		r.discover = func(context.Context) (device.Inventory, error) { return planTestInventory(), nil }
	})
	AfterEach(func() {
		for index := range nodes {
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, &nodes[index]))).To(Succeed())
		}
		Expect(k8sClient.Delete(ctx, ns)).To(Succeed())
	})
	fetch := func() *dmsv1.DMSPlanParams {
		latest := &dmsv1.DMSPlanParams{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(params), latest)).To(Succeed())
		return latest
	}
	resolved := func(index int) *hostResolution {
		return &hostResolution{nodeUID: nodes[index].UID, digest: blueprintDigest(blueprint), targetMap: &dmsv1.TargetMap{
			SchemaVersion: 1, PlatformType: "test", InventoryStage: "pre_breakout",
			PreBreakout: &dmsv1.TargetMapStage{Targets: []dmsv1.TargetEntry{{ID: "rail0", BDF: "0000:03:00.0", DeviceID: "0x1023", Rail: ptr.To(int32(0))}}},
		}}
	}
	It("reconciles using real Load, ResolveTargetMap and Build then retains a map on partial discovery", func() {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		good := fetch().Status.TargetMapByHost[nodes[0].Name]
		Expect(good.NodeUID).To(Equal(nodes[0].UID))
		Expect(good.BlueprintDigest).To(Equal(blueprintDigest(blueprint)))
		Expect(meta.IsStatusConditionTrue(good.Conditions, planResolved)).To(BeTrue())
		r.discover = func(context.Context) (device.Inventory, error) {
			return device.Inventory{Complete: false, Devices: nil, Issues: []device.DiscoveryIssue{{DeviceID: "0000:03:00", PCI: "0000:03:00.0", Err: errors.New("read failed")}}}, nil
		}
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		failed := fetch().Status.TargetMapByHost[nodes[0].Name]
		Expect(failed.TargetMap).To(Equal(good.TargetMap))
		Expect(meta.IsStatusConditionFalse(failed.Conditions, planResolved)).To(BeTrue())
	})
	It("merges concurrent host publications without losing either map", func() {
		r2 := NewDMSPlanParamsReconciler(k8sClient, k8sClient, nodes[1].Name)
		errorsByHost := make([]error, 2)
		var wg sync.WaitGroup
		wg.Add(2)
		go func() { defer wg.Done(); errorsByHost[0] = r.publish(ctx, params, resolved(0)) }()
		go func() { defer wg.Done(); errorsByHost[1] = r2.publish(ctx, params, resolved(1)) }()
		wg.Wait()
		Expect(errorsByHost).To(Equal([]error{nil, nil}))
		latest := fetch()
		Expect(latest.Status.TargetMapByHost).To(HaveLen(2))
		Expect(meta.IsStatusConditionTrue(latest.Status.Conditions, planResolved)).To(BeTrue())
	})
	It("allows a scoped issue outside the sole explicit target map", func() {
		r.discover = func(context.Context) (device.Inventory, error) {
			inventory := planTestInventory()
			inventory.Issues = []device.DiscoveryIssue{{DeviceID: "unreadable-bf3", PCI: "0000:05:00.0", Err: errors.New("metadata unavailable")}}
			return inventory, nil
		}
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		Expect(meta.IsStatusConditionTrue(fetch().Status.TargetMapByHost[nodes[0].Name].Conditions, planResolved)).To(BeTrue())
	})
	It("preserves a previous map for selected, omitted requested, and uncorrelatable discovery issues", func() {
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		prior := fetch().Status.TargetMapByHost[nodes[0].Name].TargetMap
		for _, kind := range []string{"selected", "omitted", "unknown"} {
			r.discover = func(context.Context) (device.Inventory, error) {
				inventory := planTestInventory()
				issue := device.DiscoveryIssue{DeviceID: "0000:03:00", PCI: "0000:03:00.0", Err: errors.New("read failed")}
				if kind == "omitted" {
					inventory.Devices = nil
				}
				if kind == "unknown" {
					issue.DeviceID, issue.PCI = "", ""
				}
				inventory.Issues = []device.DiscoveryIssue{issue}
				return inventory, nil
			}
			_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
			Expect(err).NotTo(HaveOccurred())
			entry := fetch().Status.TargetMapByHost[nodes[0].Name]
			Expect(entry.TargetMap).To(Equal(prior))
			Expect(meta.IsStatusConditionFalse(entry.Conditions, planResolved)).To(BeTrue(), kind)
		}
	})
	It("does not infer the pre-breakout stage from a failed post-breakout ASIC", func() {
		cm.Data[blueprintDataKey] = strings.Replace(string(blueprint),
			"post_breakout:\n      targets:\n      - {id: rail0, bdf: '0000:03:00.0'",
			"post_breakout:\n      targets:\n      - {id: rail0, bdf: '0000:05:00.0'", 1)
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		good := fetch().Status.TargetMapByHost[nodes[0].Name]
		Expect(meta.IsStatusConditionTrue(good.Conditions, planResolved)).To(BeTrue())
		r.discover = func(context.Context) (device.Inventory, error) {
			inventory := planTestInventory()
			inventory.Issues = []device.DiscoveryIssue{{DeviceID: "0000:05:00", PCI: "0000:05:00.0", Err: errors.New("post-breakout ASIC unreadable")}}
			return inventory, nil
		}
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		failed := fetch().Status.TargetMapByHost[nodes[0].Name]
		Expect(failed.TargetMap).To(Equal(good.TargetMap))
		Expect(meta.IsStatusConditionFalse(failed.Conditions, planResolved)).To(BeTrue())
		Expect(meta.FindStatusCondition(failed.Conditions, planResolved).Message).To(ContainSubstring("discovery issue may affect selected device"))
	})
	It("does not use a missing candidate to choose between alternative authored maps", func() {
		r.discover = func(context.Context) (device.Inventory, error) {
			inventory := planTestInventory()
			inventory.Issues = []device.DiscoveryIssue{{DeviceID: "0000:05:00", PCI: "0000:05:00.0", Err: errors.New("read failed")}}
			return inventory, nil
		}
		alternative := `targetMaps:
  omitted.yaml:
    schema_version: 1
    platform_type: test
    pre_breakout:
      targets:
      - {id: other, bdf: '0000:05:00.0', device_id: '0x1023', role: ew, rail: 0}
`
		data := []byte(strings.Replace(string(blueprint), "targetMaps:\n", alternative, 1))
		_, err := r.resolve(ctx, params.Spec, data)
		Expect(err).To(MatchError(ContainSubstring("selection among authored target maps")))
	})
	It("cleans membership even when its own node is deselected, and rejects old node incarnations", func() {
		Expect(r.publish(ctx, params, resolved(0))).To(Succeed())
		old := resolved(0)
		Expect(k8sClient.Delete(ctx, &nodes[0])).To(Succeed())
		replacement := corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodes[0].Name, Labels: nodes[0].Labels}}
		Expect(k8sClient.Create(ctx, &replacement)).To(Succeed())
		nodes[0] = replacement
		Expect(r.publish(ctx, params, old)).NotTo(Succeed())
		Expect(r.publish(ctx, params, resolved(0))).To(Succeed())
		Expect(fetch().Status.TargetMapByHost[nodes[0].Name].NodeUID).To(Equal(replacement.UID))
		nodes[0].Labels["enabled"] = "false"
		Expect(k8sClient.Update(ctx, &nodes[0])).To(Succeed())
		_, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		Expect(fetch().Status.TargetMapByHost).NotTo(HaveKey(nodes[0].Name))
		nodes[1].Labels["enabled"] = "false"
		Expect(k8sClient.Update(ctx, &nodes[1])).To(Succeed())
		_, err = r.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(params)})
		Expect(err).NotTo(HaveOccurred())
		Expect(fetch().Status.TargetMapByHost).To(BeEmpty())
		Expect(meta.FindStatusCondition(fetch().Status.Conditions, planResolved).Reason).To(Equal("NoMatchingNodes"))
	})
	It("rejects results captured before a spec or ConfigMap payload change", func() {
		Expect(r.publish(ctx, params, resolved(0))).To(Succeed())
		cm.Data[blueprintDataKey] += "\n# edited without a plan generation change\n"
		Expect(k8sClient.Update(ctx, cm)).To(Succeed())
		Expect(r.publish(ctx, params, resolved(0))).NotTo(Succeed())
		pool := &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name}, Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
			PlanParamsRef: params.Name, NodeSelector: map[string]string{"dms-test": ns.Name},
		}}
		rail := NewSpectrumXRailPoolConfigHostFlowsReconciler(k8sClient, scheme.Scheme, nil, nil, nil, nodes[0].Name)
		Expect(rail.validatePlanBindings(ctx, pool)).To(MatchError(ContainSubstring("waiting for current")))
		updated := fetch()
		updated.Spec.Parameters = map[string]string{"value": "0"}
		Expect(k8sClient.Update(ctx, updated)).To(Succeed())
		Expect(r.publish(ctx, params, resolved(0))).NotTo(Succeed())
	})
	It("keeps a previous map unresolved when a replacement would exceed the publication budget", func() {
		Expect(r.publish(ctx, params, resolved(0))).To(Succeed())
		prior := fetch().Status.TargetMapByHost[nodes[0].Name].TargetMap
		oversized := resolved(0)
		oversized.targetMap.Description = strings.Repeat("x", planStatusBudget)
		Expect(r.publish(ctx, params, oversized)).To(Succeed())
		entry := fetch().Status.TargetMapByHost[nodes[0].Name]
		Expect(entry.TargetMap).To(Equal(prior))
		Expect(meta.IsStatusConditionFalse(entry.Conditions, planResolved)).To(BeTrue())
		Expect(meta.FindStatusCondition(entry.Conditions, planResolved).Message).To(ContainSubstring("publication budget"))
	})
	It("passes declaration strings unchanged and lets the real compiler preserve their types", func() {
		params.Spec.Parameters = map[string]string{"value": "0", "large": "9007199254740993", "flag": "false", "text": "001", "mode": "0"}
		params.Spec.SPXGS = ptr.To(false)
		options, err := planBuildOptions(params.Spec)
		Expect(err).NotTo(HaveOccurred())
		Expect(options.Parameters["text"]).To(Equal("001"))
		b, err := plan.Load(blueprint)
		Expect(err).NotTo(HaveOccurred())
		compiled, err := plan.Build(ctx, b, planTestInventory().Devices, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(compiled.Info().Parameters).To(SatisfyAll(HaveKeyWithValue("value", 0), HaveKeyWithValue("large", int(9007199254740993)),
			HaveKeyWithValue("flag", false), HaveKeyWithValue("text", "001"), HaveKeyWithValue("mode", 0), HaveKeyWithValue("SP_XGS", false)))
		options.Parameters["text"] = ""
		compiled, err = plan.Build(ctx, b, planTestInventory().Devices, options)
		Expect(err).NotTo(HaveOccurred())
		Expect(compiled.Info().Parameters["text"]).To(Equal(""))
		options.Parameters["unknown"] = "true"
		_, err = plan.Build(ctx, b, planTestInventory().Devices, options)
		Expect(err).To(HaveOccurred())
		params.Spec.Parameters["SP_XGS"] = "true"
		_, err = planBuildOptions(params.Spec)
		Expect(err).To(MatchError(ContainSubstring("explicit field")))
	})
	It("requires plan coverage and current maps without changing unreferenced pool behavior", func() {
		pool := &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Namespace: ns.Name}, Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
			PlanParamsRef: "", NodeSelector: map[string]string{"dms-test": ns.Name},
		}}
		rail := NewSpectrumXRailPoolConfigHostFlowsReconciler(k8sClient, scheme.Scheme, nil, nil, nil, nodes[0].Name)
		Expect(rail.validatePlanBindings(ctx, pool)).To(Succeed())
		pool.Spec.PlanParamsRef = params.Name
		Expect(r.publish(ctx, params, resolved(0))).To(Succeed())
		r2 := NewDMSPlanParamsReconciler(k8sClient, k8sClient, nodes[1].Name)
		Expect(r2.publish(ctx, params, resolved(1))).To(Succeed())
		Expect(rail.validatePlanBindings(ctx, pool)).To(Succeed())
		nodes[1].Labels["enabled"] = "false"
		Expect(k8sClient.Update(ctx, &nodes[1])).To(Succeed())
		Expect(rail.validatePlanBindings(ctx, pool)).To(MatchError(ContainSubstring("does not select")))
	})
	It("gates the actual rail reconcile before any networking mutation and records InProgress", func() {
		pool := &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: ns.Name, Finalizers: []string{finalizerName}}, Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
			PlanParamsRef: params.Name, NodeSelector: map[string]string{"dms-test": ns.Name}, NumVfs: 1,
			RailTopology: []v1alpha2.RailTopology{{Name: "rail", NicSelector: v1alpha2.NicSelector{PfNames: []string{"eth0"}}, CidrPoolRef: "cidrs"}},
		}}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		rail := NewSpectrumXRailPoolConfigHostFlowsReconciler(k8sClient, scheme.Scheme, nil, nil, nil, nodes[0].Name)
		Expect(rail.doReconcile(ctx, pool)).To(MatchError(ContainSubstring("waiting for current")))
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(pool), pool)).To(Succeed())
		Expect(pool.Status.NodeStates).To(HaveLen(1))
		Expect(pool.Status.NodeStates[0].State).To(Equal(v1alpha2.SyncStatusInProgress))
		Expect(pool.Status.NodeStates[0].Message).To(ContainSubstring("waiting for current"))
	})
	It("enqueues referenced rail pools for blueprint updates and plan deletion events", func() {
		pool := &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Name: "pool", Namespace: ns.Name}, Spec: v1alpha2.SpectrumXRailPoolConfigSpec{
			PlanParamsRef: params.Name, NodeSelector: map[string]string{"dms-test": ns.Name}, NumVfs: 1,
			RailTopology: []v1alpha2.RailTopology{{Name: "rail", NicSelector: v1alpha2.NicSelector{PfNames: []string{"eth0"}}, CidrPoolRef: "cidrs"}},
		}}
		Expect(k8sClient.Create(ctx, pool)).To(Succeed())
		rail := NewSpectrumXRailPoolConfigHostFlowsReconciler(k8sClient, scheme.Scheme, nil, nil, nil, nodes[0].Name)
		Expect(rail.planInputToRailConfigs(ctx, cm)).To(ConsistOf(ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}))
		Expect(k8sClient.Delete(ctx, params)).To(Succeed())
		Expect(rail.planInputToRailConfigs(ctx, params)).To(ConsistOf(ctrl.Request{NamespacedName: client.ObjectKeyFromObject(pool)}))
	})
})
