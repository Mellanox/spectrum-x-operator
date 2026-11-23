// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0
package controller

import (
	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("DMS plan API admission", func() {
	It("rejects a plan reference until library execution is available", func() {
		pool := &v1alpha2.SpectrumXRailPoolConfig{ObjectMeta: metav1.ObjectMeta{Name: "dms-api-only", Namespace: "default"}, Spec: v1alpha2.SpectrumXRailPoolConfigSpec{DMSPlanParamsRef: "plan", NumVfs: 8, RailTopology: []v1alpha2.RailTopology{{Name: "rail0", NicSelector: v1alpha2.NicSelector{PfNames: []string{"eth1"}}, MTU: 9000}}}}
		Expect(k8sClient.Create(ctx, pool)).To(MatchError(ContainSubstring("dmsPlanParamsRef requires DMS library integration")))
	})
})
