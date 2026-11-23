// Copyright 2026 NVIDIA CORPORATION & AFFILIATES
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	dmsv1 "github.com/Mellanox/dms-lib/api/v1alpha1"
	"github.com/Mellanox/dms-lib/pkg/device"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

// compileXplaneContent returns the selected service artifact unchanged. The
// authored blueprint determines whether xplane reads topology from config or OVSDB.
func compileXplaneContent(ctx context.Context, spec dmsv1.DMSPlanSpec, data []byte, discover func(context.Context) (device.Inventory, error)) ([]byte, error) {
	compiled, err := buildHostPlan(ctx, spec, data, discover)
	if err != nil {
		return nil, fmt.Errorf("build xplane plan: %w", err)
	}
	config, err := compiled.GetServiceConfig("spcx-doca-xplane-boot")
	if err != nil {
		return nil, fmt.Errorf("get xplane service configuration: %w", err)
	}
	switch config.Schema {
	case "dospcx/template/spcx/ra22/xplane/xplane.toml.j2", "dospcx/template/spcx/ra24/xplane/xplane.toml.j2":
	default:
		return nil, fmt.Errorf("unsupported xplane configuration schema %q", config.Schema)
	}
	if config.MediaType != "application/toml" || len(bytes.TrimSpace(config.Content)) == 0 {
		return nil, fmt.Errorf("xplane configuration requires nonempty TOML")
	}
	return config.Content, nil
}

// prepareXplaneConfig writes this host's selected artifact before reconciling
// the existing DaemonSet. A running xplane consumes configuration at startup;
// changing this file does not claim a live reload or request a pod restart.
func (r *SpectrumXRailPoolConfigHostFlowsReconciler) prepareXplaneConfig(ctx context.Context, pool *v1alpha2.SpectrumXRailPoolConfig) (string, error) {
	if pool.Spec.DMSPlanParamsRef == "" {
		return "", nil
	}
	params := &dmsv1.DMSPlanParams{}
	if err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: pool.Spec.DMSPlanParamsRef}, params); err != nil {
		return "", err
	}
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes, client.MatchingLabels(pool.Spec.NodeSelector)); err != nil {
		return "", err
	}
	local := false
	selector := labels.SelectorFromSet(params.Spec.NodeSelector)
	for _, node := range nodes.Items {
		if !node.DeletionTimestamp.IsZero() {
			continue
		}
		if !selector.Matches(labels.Set(node.Labels)) {
			return "", fmt.Errorf("DMS plan %s does not select pool node %s", params.Name, node.Name)
		}
		local = local || node.Name == r.nodeName
	}
	if !local {
		return "", fmt.Errorf("local node %s is not selected by the rail pool", r.nodeName)
	}
	data, err := readBlueprint(ctx, r.Client, params)
	if err != nil {
		return "", err
	}
	content, err := compileXplaneContent(ctx, params.Spec, data, r.discover)
	if err != nil {
		return "", err
	}
	path := filepath.Join("/var/lib/spectrum-x", "plans", string(pool.UID), "xplane.toml")
	if err := writeXplaneConfig(filepath.Join(r.hostRoot, path), content); err != nil {
		return "", fmt.Errorf("write xplane configuration: %w", err)
	}
	return path, nil
}

func writeXplaneConfig(path string, content []byte) error {
	current, err := os.ReadFile(path) //nolint:gosec // Fixed operator-owned path; hostRoot is only a test prefix.
	if err == nil && bytes.Equal(current, content) {
		return nil
	}
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), ".xplane-*.toml")
	if err != nil {
		return err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	if err = file.Chmod(0o644); err == nil {
		_, err = file.Write(content)
	}
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := os.Rename(file.Name(), path); err != nil {
		return err
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer func() { _ = directory.Close() }()
	return directory.Sync()
}

func (r *SpectrumXRailPoolConfigHostFlowsReconciler) xplanePlanInputToPools(ctx context.Context, object client.Object) []reconcile.Request {
	pools := &v1alpha2.SpectrumXRailPoolConfigList{}
	if err := r.List(ctx, pools, client.InNamespace(object.GetNamespace())); err != nil {
		log.FromContext(ctx).Error(err, "list pools referencing DMS plan")
		return nil
	}
	requests := []reconcile.Request{}
	for _, pool := range pools.Items {
		ref := pool.Spec.DMSPlanParamsRef
		if ref == "" {
			continue
		}
		if node, isNode := object.(*corev1.Node); isNode {
			if labels.SelectorFromSet(pool.Spec.NodeSelector).Matches(labels.Set(node.Labels)) {
				requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pool)})
			}
			continue
		}
		if _, isPlan := object.(*dmsv1.DMSPlanParams); isPlan {
			if ref != object.GetName() {
				continue
			}
		} else {
			params := &dmsv1.DMSPlanParams{}
			if err := r.Get(ctx, types.NamespacedName{Namespace: pool.Namespace, Name: ref}, params); err != nil {
				log.FromContext(ctx).Error(err, "read referenced DMS plan")
				continue
			}
			if params.Spec.BlueprintRef != object.GetName() {
				continue
			}
		}
		requests = append(requests, reconcile.Request{NamespacedName: client.ObjectKeyFromObject(&pool)})
	}
	return requests
}
