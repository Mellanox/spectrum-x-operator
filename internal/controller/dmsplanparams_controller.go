// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"time"

	dmsv1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	"github.com/Mellanox/dms-lib/pkg/device"
	"github.com/Mellanox/dms-lib/pkg/plan"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	blueprintDataKey    = "blueprint.yaml"
	planResolved        = "Resolved"
	bindingsResolved    = "BindingsResolved"
	planRefreshInterval = time.Minute
)

// DMSPlanParamsReconciler resolves local host bindings; it never configures NICs or services.
type DMSPlanParamsReconciler struct {
	client.Client
	reader   client.Reader
	nodeName string
	discover func(context.Context) (device.Inventory, error)
}

func NewDMSPlanParamsReconciler(c client.Client, reader client.Reader, nodeName string) *DMSPlanParamsReconciler {
	return &DMSPlanParamsReconciler{
		Client: c, reader: reader, nodeName: nodeName,
		discover: func(ctx context.Context) (device.Inventory, error) {
			discoveryContext, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			return device.Discover(discoveryContext, device.Options{Timeout: 30 * time.Second})
		},
	}
}

type hostResolution struct {
	nodeUID   types.UID
	digest    string
	targetMap *dmsv1.TargetMap
	err       error
}

// +kubebuilder:rbac:groups=dms.nvidia.com,resources=dmsplanparams,verbs=get;list;watch
// +kubebuilder:rbac:groups=dms.nvidia.com,resources=dmsplanparams/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch

// Reconcile publishes current local bindings and removes stale inventory memberships.
func (r *DMSPlanParamsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	params := &dmsv1.DMSPlanParams{}
	if err := r.reader.Get(ctx, req.NamespacedName, params); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	node := &corev1.Node{}
	if err := r.reader.Get(ctx, types.NamespacedName{Name: r.nodeName}, node); err != nil && !apierrors.IsNotFound(err) {
		return ctrl.Result{}, err
	}
	var result *hostResolution
	if node.UID != "" && node.DeletionTimestamp.IsZero() && labels.SelectorFromSet(params.Spec.NodeSelector).Matches(labels.Set(node.Labels)) {
		resolved := r.resolveHost(ctx, params, node.UID)
		result = &resolved
		if result.err != nil {
			log.FromContext(ctx).Error(result.err, "Cannot resolve DMS plan", "node", r.nodeName)
		}
	}
	if err := r.publish(ctx, params, result); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: planRefreshInterval}, nil
}

func (r *DMSPlanParamsReconciler) resolveHost(ctx context.Context, params *dmsv1.DMSPlanParams, uid types.UID) hostResolution {
	result := hostResolution{nodeUID: uid}
	data, err := readBlueprint(ctx, r.reader, params)
	if err != nil {
		result.err = err
		return result
	}
	result.digest = blueprintDigest(data)
	result.targetMap, result.err = r.resolve(ctx, params.Spec, data)
	return result
}

func (r *DMSPlanParamsReconciler) resolve(ctx context.Context, spec dmsv1.DMSPlanSpec, data []byte) (*dmsv1.TargetMap, error) {
	blueprint, err := plan.Load(data)
	if err != nil {
		return nil, err
	}
	options, err := planBuildOptions(spec)
	if err != nil {
		return nil, err
	}
	inventory, err := r.discover(ctx)
	if err != nil {
		return nil, fmt.Errorf("discover host: %w", err)
	}
	compiled, err := plan.BuildFromInventory(ctx, blueprint, inventory, options)
	if err != nil {
		return nil, err
	}
	resolved, err := compiled.TargetMap()
	if err != nil {
		return nil, err
	}

	encoded, err := json.Marshal(resolved)
	if err != nil {
		return nil, fmt.Errorf("encode resolved targets: %w", err)
	}
	var targetMap dmsv1.TargetMap
	if err = json.Unmarshal(encoded, &targetMap); err != nil {
		return nil, fmt.Errorf("convert resolved targets: %w", err)
	}
	return &targetMap, nil
}

func planBuildOptions(spec dmsv1.DMSPlanSpec) (plan.BuildOptions, error) {
	parameters := make(map[string]any, len(spec.Parameters)+3)
	for key, value := range spec.Parameters {
		switch key {
		case "planes", "overlay", "SP_XGS":
			return plan.BuildOptions{}, fmt.Errorf("parameter %q must use its explicit field", key)
		}
		// The selected compiler declarations coerce strings; do not guess their types here.
		parameters[key] = value
	}
	if spec.Planes != nil {
		parameters["planes"] = int(*spec.Planes)
	}
	if spec.Overlay != "" {
		parameters["overlay"] = spec.Overlay
	}
	if spec.SPXGS != nil {
		parameters["SP_XGS"] = *spec.SPXGS
	}
	return plan.BuildOptions{Profile: spec.Profile, Environment: "host-k8s", PlatformType: spec.PlatformType, Parameters: parameters}, nil
}

func readBlueprint(ctx context.Context, reader client.Reader, params *dmsv1.DMSPlanParams) ([]byte, error) {
	cm := &corev1.ConfigMap{}
	if err := reader.Get(ctx, types.NamespacedName{Namespace: params.Namespace, Name: params.Spec.BlueprintRef}, cm); err != nil {
		return nil, fmt.Errorf("read blueprint ConfigMap: %w", err)
	}
	data := []byte(cm.Data[blueprintDataKey])
	if len(data) == 0 {
		return nil, fmt.Errorf("ConfigMap %s/%s has no %s", cm.Namespace, cm.Name, blueprintDataKey)
	}
	return data, nil
}

func blueprintDigest(data []byte) string { return fmt.Sprintf("%x", sha256.Sum256(data)) }

func (r *DMSPlanParamsReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&dmsv1.DMSPlanParams{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&corev1.ConfigMap{}, handler.EnqueueRequestsFromMapFunc(r.plansForInput)).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(r.plansForInput), builder.WithPredicates(planNodePredicate())).
		Named("dms-plan-params").Complete(r)
}

// Membership depends on identity, labels and deletion, not heartbeat/status updates.
func planNodePredicate() predicate.Funcs {
	return predicate.Funcs{UpdateFunc: func(update event.UpdateEvent) bool {
		before, after := update.ObjectOld, update.ObjectNew
		if before == nil || after == nil {
			return false
		}
		return before.GetUID() != after.GetUID() || !maps.Equal(before.GetLabels(), after.GetLabels()) ||
			!before.GetDeletionTimestamp().Equal(after.GetDeletionTimestamp())
	}}
}

func (r *DMSPlanParamsReconciler) plansForInput(ctx context.Context, object client.Object) []reconcile.Request {
	list := &dmsv1.DMSPlanParamsList{}
	if err := r.List(ctx, list); err != nil {
		log.FromContext(ctx).Error(err, "List DMS plans for input change")
		return nil
	}
	requests := make([]reconcile.Request, 0, len(list.Items))
	_, isConfigMap := object.(*corev1.ConfigMap)
	for _, params := range list.Items {
		if isConfigMap && (params.Namespace != object.GetNamespace() || params.Spec.BlueprintRef != object.GetName()) {
			continue
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&params)})
	}
	return requests
}
