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

package staleflows

import (
	"context"
	"fmt"
	"time"

	"github.com/Mellanox/spectrum-x-operator/internal/controller"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type Cleaner struct {
	client.Client
	CleanupInterval time.Duration
	Flows           controller.FlowsAPI
}

// StartCleanupRoutine starts a background goroutine that periodically cleans up stale flows
func (c *Cleaner) StartCleanupRoutine(ctx context.Context) {
	go func() {
		// Use configured interval or default to 5 minutes
		interval := c.CleanupInterval
		if interval == 0 {
			interval = 5 * time.Minute
		}

		logr := log.FromContext(ctx).WithName("flow-cleanup")
		logr.Info("Starting flow cleanup routine", "interval", interval)

		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logr.Info("Stopping flow cleanup routine")
				return
			case <-ticker.C:
				if err := c.cleanupStaleFlows(ctx); err != nil {
					logr.Error(err, "Failed to cleanup stale flows")
				}
			}
		}
	}()
}

// cleanupStaleFlows removes flows for pods that no longer exist
func (c *Cleaner) cleanupStaleFlows(ctx context.Context) error {
	pods := &corev1.PodList{}
	// clinet cache is set to watch pods on the same node as the reconciler
	if err := c.List(ctx, pods); err != nil {
		return fmt.Errorf("failed to list pods: %w", err)
	}

	if len(pods.Items) == 0 {
		return nil
	}

	existingPods := make(map[string]bool, len(pods.Items))
	for _, pod := range pods.Items {
		existingPods[string(pod.UID)] = true
	}

	if err := c.Flows.CleanupStaleFlowsForBridges(ctx, existingPods); err != nil {
		return fmt.Errorf("failed to cleanup stale flows: %w", err)
	}

	return nil
}
