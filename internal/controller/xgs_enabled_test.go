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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	dmsv1alpha1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

var _ = Describe("isXGSEnabled", func() {
	const namespace = "test-ns"

	newReconciler := func(objs ...*dmsv1alpha1.DMSPlanParams) *SpectrumXRailPoolConfigHostFlowsReconciler {
		s := runtime.NewScheme()
		Expect(dmsv1alpha1.AddToScheme(s)).To(Succeed())
		b := fake.NewClientBuilder().WithScheme(s)
		for _, o := range objs {
			b = b.WithObjects(o)
		}
		return &SpectrumXRailPoolConfigHostFlowsReconciler{Client: b.Build()}
	}

	plan := func(ns, name string, spxgs *bool) *dmsv1alpha1.DMSPlanParams {
		return &dmsv1alpha1.DMSPlanParams{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns},
			Spec:       dmsv1alpha1.DMSPlanSpec{SPXGS: spxgs},
		}
	}

	rpcWithRef := func(ref string) *v1alpha2.SpectrumXRailPoolConfig {
		return &v1alpha2.SpectrumXRailPoolConfig{
			ObjectMeta: metav1.ObjectMeta{Name: "rpc", Namespace: namespace},
			Spec:       v1alpha2.SpectrumXRailPoolConfigSpec{DMSPlanParamsRef: ref},
		}
	}

	yes, no := true, false

	DescribeTable("resolves the DMSPlanParams in the pool config namespace",
		func(ref string, objs []*dmsv1alpha1.DMSPlanParams, expected bool) {
			got, err := newReconciler(objs...).isXGSEnabled(context.Background(), rpcWithRef(ref))
			Expect(err).NotTo(HaveOccurred())
			Expect(got).To(Equal(expected))
		},
		Entry("no reference", "", nil, false),
		Entry("spxgs true", "plan", []*dmsv1alpha1.DMSPlanParams{plan(namespace, "plan", &yes)}, true),
		Entry("spxgs false", "plan", []*dmsv1alpha1.DMSPlanParams{plan(namespace, "plan", &no)}, false),
		Entry("spxgs unset", "plan", []*dmsv1alpha1.DMSPlanParams{plan(namespace, "plan", nil)}, false),
	)

	It("returns an error when the referenced DMSPlanParams does not exist", func() {
		_, err := newReconciler().isXGSEnabled(context.Background(), rpcWithRef("missing"))
		Expect(err).To(HaveOccurred())
	})

	It("ignores a DMSPlanParams with the same name in another namespace", func() {
		r := newReconciler(plan("other-ns", "plan", &yes))
		_, err := r.isXGSEnabled(context.Background(), rpcWithRef("plan"))
		Expect(err).To(HaveOccurred())
	})
})
