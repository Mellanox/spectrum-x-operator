// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"fmt"

	dmsv1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func (r *DMSPlanParamsReconciler) publish(ctx context.Context, observed *dmsv1.DMSPlanParams, result *hostResolution) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		latest := &dmsv1.DMSPlanParams{}
		if err := r.reader.Get(ctx, client.ObjectKeyFromObject(observed), latest); err != nil {
			return err
		}
		if latest.UID != observed.UID || latest.Generation != observed.Generation {
			return planConflict(latest, "plan changed during discovery")
		}
		digest := ""
		data, blueprintErr := readBlueprint(ctx, r.reader, latest)
		if blueprintErr == nil {
			digest = blueprintDigest(data)
		}
		if result != nil && digest != result.digest {
			return planConflict(latest, "blueprint changed during discovery")
		}
		nodes := &corev1.NodeList{}
		if err := r.reader.List(ctx, nodes); err != nil {
			return err
		}
		selected := selectedPlanNodes(latest, nodes.Items)
		updated := latest.DeepCopy()
		if updated.Status.TargetMapByHost == nil {
			updated.Status.TargetMapByHost = map[string]dmsv1.HostTargetMap{}
		}
		// Every daemon cleans global membership, including while its own node is deselected.
		// Live reads on each retry prevent deleting a newer node incarnation's publication.
		for name, entry := range updated.Status.TargetMapByHost {
			node, exists := selected[name]
			if !exists || node.UID != entry.NodeUID {
				delete(updated.Status.TargetMapByHost, name)
			}
		}
		if result != nil {
			node, exists := selected[r.nodeName]
			if !exists || node.UID != result.nodeUID {
				return planConflict(latest, "local node selection or UID changed during discovery")
			}
			updatePlanHost(updated, r.nodeName, result)
		}
		updatePlanSummary(updated, selected, digest, blueprintErr)
		if equality.Semantic.DeepEqual(latest.Status, updated.Status) {
			return nil
		}
		// ResourceVersion provides optimistic concurrency while a full status
		// update preserves literal null renderer values. Merge patches interpret
		// those values as deletion requests.
		return r.Status().Update(ctx, updated)
	})
}

func selectedPlanNodes(params *dmsv1.DMSPlanParams, nodes []corev1.Node) map[string]corev1.Node {
	selected := make(map[string]corev1.Node)
	selector := labels.SelectorFromSet(params.Spec.NodeSelector)
	for _, node := range nodes {
		if node.DeletionTimestamp.IsZero() && selector.Matches(labels.Set(node.Labels)) {
			selected[node.Name] = node
		}
	}
	return selected
}

func updatePlanHost(params *dmsv1.DMSPlanParams, name string, result *hostResolution) {
	entry := params.Status.TargetMapByHost[name]
	if entry.NodeUID != result.nodeUID {
		entry = dmsv1.HostTargetMap{NodeUID: result.nodeUID}
	}
	condition := metav1.Condition{
		Type: planResolved, ObservedGeneration: params.Generation, Status: metav1.ConditionFalse,
		Reason: "ResolutionFailed", Message: "host binding resolution failed",
	}
	if result.err == nil && result.targetMap != nil {
		entry.TargetMap = *result.targetMap
		entry.ObservedGeneration = params.Generation
		entry.BlueprintDigest = result.digest
		condition.Status, condition.Reason, condition.Message = metav1.ConditionTrue, bindingsResolved, "Host bindings are current"
	} else if result.err != nil {
		condition.Message = result.err.Error()
		if len(condition.Message) > 4096 {
			condition.Message = condition.Message[:4096]
		}
	}
	meta.SetStatusCondition(&entry.Conditions, condition)
	params.Status.TargetMapByHost[name] = entry
}

func hostBindingCurrent(params *dmsv1.DMSPlanParams, entry dmsv1.HostTargetMap, node corev1.Node, digest string) bool {
	condition := meta.FindStatusCondition(entry.Conditions, planResolved)
	return digest != "" && entry.NodeUID == node.UID && entry.ObservedGeneration == params.Generation &&
		entry.BlueprintDigest == digest && entry.InventoryStage != "" && condition != nil &&
		condition.Status == metav1.ConditionTrue && condition.ObservedGeneration == params.Generation
}

func updatePlanSummary(params *dmsv1.DMSPlanParams, nodes map[string]corev1.Node, digest string, inputErr error) {
	params.Status.ObservedGeneration, params.Status.BlueprintDigest = params.Generation, digest
	condition := metav1.Condition{
		Type: planResolved, ObservedGeneration: params.Generation, Status: metav1.ConditionFalse,
		Reason: "PendingBindings", Message: "Waiting for current bindings for every selected node",
	}
	allCurrent := len(nodes) > 0 && inputErr == nil
	for name, node := range nodes {
		if !hostBindingCurrent(params, params.Status.TargetMapByHost[name], node, digest) {
			allCurrent = false
		}
	}
	switch {
	case allCurrent:
		condition.Status, condition.Reason, condition.Message = metav1.ConditionTrue, bindingsResolved, "All selected host bindings are current"
	case inputErr != nil:
		condition.Reason, condition.Message = "InvalidInput", "Blueprint or status cannot be published; inspect host conditions and controller logs"
	case len(nodes) == 0:
		condition.Reason, condition.Message = "NoMatchingNodes", "No nodes match the plan selector"
	}
	meta.SetStatusCondition(&params.Status.Conditions, condition)
}

func planConflict(params *dmsv1.DMSPlanParams, message string) error {
	return apierrors.NewConflict(schema.GroupResource{Group: "dms.nvidia.com", Resource: "dmsplanparams"}, params.Name, fmt.Errorf("%s", message))
}
