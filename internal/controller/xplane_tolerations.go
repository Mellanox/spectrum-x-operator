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
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

// Resolve the owner lazily: single-plane configurations do not need xplane or
// depend on the availability of the flowcontroller's Pod and DaemonSet.
func (r *SpectrumXRailPoolConfigHostFlowsReconciler) flowcontrollerOwner(
	ctx context.Context,
) (*metav1.OwnerReference, error) {
	if r.ownPod.Name == "" || r.ownPod.Namespace == "" {
		return nil, fmt.Errorf("POD_NAME and POD_NAMESPACE must identify the flowcontroller Pod")
	}
	pod := &v1.Pod{}
	if err := r.Get(ctx, r.ownPod, pod); err != nil {
		return nil, fmt.Errorf("failed to get flowcontroller Pod %s: %w", r.ownPod, err)
	}
	owner := metav1.GetControllerOf(pod)
	if owner == nil || owner.APIVersion != appsv1.SchemeGroupVersion.String() ||
		owner.Kind != DaemonSet || owner.Name == "" || owner.UID == "" {
		return nil, fmt.Errorf("flowcontroller Pod %s must be controlled by an apps/v1 DaemonSet", r.ownPod)
	}
	return owner, nil
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) flowcontrollerTolerations(
	ctx context.Context,
) ([]v1.Toleration, error) {
	owner, err := r.flowcontrollerOwner(ctx)
	if err != nil {
		return nil, err
	}
	ds := &appsv1.DaemonSet{}
	key := types.NamespacedName{Namespace: r.ownPod.Namespace, Name: owner.Name}
	if err := r.Get(ctx, key, ds); err != nil {
		return nil, fmt.Errorf("failed to get flowcontroller DaemonSet %s: %w", key, err)
	}
	if ds.UID != owner.UID {
		return nil, fmt.Errorf("flowcontroller DaemonSet %s UID does not match Pod controller reference", key)
	}
	return ds.Spec.Template.Spec.DeepCopy().Tolerations, nil
}

func flowcontrollerTolerationsChanged() predicate.Predicate {
	return predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldDS, oldOK := e.ObjectOld.(*appsv1.DaemonSet)
			newDS, newOK := e.ObjectNew.(*appsv1.DaemonSet)
			return oldOK && newOK && !equality.Semantic.DeepEqual(
				oldDS.Spec.Template.Spec.Tolerations, newDS.Spec.Template.Spec.Tolerations,
			)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) flowcontrollerToRailConfigs(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	if obj.GetNamespace() != r.ownPod.Namespace {
		return nil
	}
	owner, err := r.flowcontrollerOwner(ctx)
	if err != nil {
		log.FromContext(ctx).Error(err, "Failed to identify flowcontroller for toleration update")
		return nil
	}
	if obj.GetName() != owner.Name || obj.GetUID() != owner.UID {
		return nil
	}
	pools := &v1alpha2.SpectrumXRailPoolConfigList{}
	if err := r.List(ctx, pools); err != nil {
		log.FromContext(ctx).Error(err, "Failed to list rail pool configs for flowcontroller toleration update")
		return nil
	}
	var requests []reconcile.Request
	for _, pool := range pools.Items {
		for _, topology := range pool.Spec.RailTopology {
			if len(topology.NicSelector.PfNames) > 1 {
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pool)})
				break
			}
		}
	}
	return requests
}
