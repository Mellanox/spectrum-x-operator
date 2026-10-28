// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	dmsv1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) validatePlanBindings(ctx context.Context, rpc *v1alpha2.SpectrumXRailPoolConfig) error {
	if rpc.Spec.PlanParamsRef == "" {
		return nil
	}
	params := &dmsv1.DMSPlanParams{}
	if err := r.planReader.Get(ctx, types.NamespacedName{Namespace: rpc.Namespace, Name: rpc.Spec.PlanParamsRef}, params); err != nil {
		return fmt.Errorf("read referenced DMSPlanParams: %w", err)
	}
	data, err := readBlueprint(ctx, r.planReader, params)
	if err != nil {
		return err
	}
	digest := blueprintDigest(data)
	nodes := &corev1.NodeList{}
	if err = r.planReader.List(ctx, nodes, client.MatchingLabels(rpc.Spec.NodeSelector)); err != nil {
		return err
	}
	selector := labels.SelectorFromSet(params.Spec.NodeSelector)
	for _, node := range nodes.Items {
		if !selector.Matches(labels.Set(node.Labels)) {
			return fmt.Errorf("DMSPlanParams %s does not select rail pool node %s", params.Name, node.Name)
		}
		if !hostBindingCurrent(params, params.Status.TargetMapByHost[node.Name], node, digest) {
			return fmt.Errorf("waiting for current DMS plan bindings for node %s", node.Name)
		}
	}
	return nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) planInputToRailConfigs(ctx context.Context, object client.Object) []reconcile.Request {
	planNames := map[string]bool{}
	if _, isConfigMap := object.(*corev1.ConfigMap); isConfigMap {
		plans := &dmsv1.DMSPlanParamsList{}
		if err := r.List(ctx, plans, client.InNamespace(object.GetNamespace())); err != nil {
			log.FromContext(ctx).Error(err, "List plans for rail pool input change")
			return nil
		}
		for _, params := range plans.Items {
			if params.Spec.BlueprintRef == object.GetName() {
				planNames[params.Name] = true
			}
		}
	} else {
		// Use the event identity, including deletion events whose object is no longer listed.
		planNames[object.GetName()] = true
	}
	railPools := &v1alpha2.SpectrumXRailPoolConfigList{}
	if err := r.List(ctx, railPools, client.InNamespace(object.GetNamespace())); err != nil {
		log.FromContext(ctx).Error(err, "List rail pools for plan input change")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(railPools.Items))
	for _, pool := range railPools.Items {
		if planNames[pool.Spec.PlanParamsRef] {
			requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pool)})
		}
	}
	return requests
}
