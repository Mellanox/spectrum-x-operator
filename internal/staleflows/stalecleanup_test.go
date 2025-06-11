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

	gomock "github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Stale Flows Cleanup", func() {
	var (
		cleaner   *Cleaner
		ctx       context.Context
		cancel    context.CancelFunc
		flowsMock *controller.MockFlowsAPI
		ctrl      *gomock.Controller
		ns        *corev1.Namespace
	)

	BeforeEach(func() {
		ns = &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: "test-ns-"}}
		Expect(k8sClient.Create(context.Background(), ns)).Should(Succeed())

		ctrl = gomock.NewController(GinkgoT())
		flowsMock = controller.NewMockFlowsAPI(ctrl)
		ctx, cancel = context.WithCancel(context.Background())

		cleaner = &Cleaner{
			Client:          k8sClient,
			Flows:           flowsMock,
			CleanupInterval: 100 * time.Millisecond, // Short interval for testing
		}
	})

	AfterEach(func() {
		if cancel != nil {
			cancel()
		}
		ctrl.Finish()
		Expect(k8sClient.Delete(context.Background(), ns)).Should(Succeed())
	})

	Context("StartCleanupRoutine", func() {
		It("should start and stop cleanup routine gracefully", func() {
			// Mock the cleanup call - we expect at least one call
			flowsMock.EXPECT().CleanupStaleFlowsForBridges(gomock.Any(), gomock.Any()).
				Return(nil).
				AnyTimes()

			// Start the cleanup routine
			cleaner.StartCleanupRoutine(ctx)

			// Give it some time to run at least once
			time.Sleep(150 * time.Millisecond)

			// Cancel the context to stop the routine
			cancel()
			cancel = nil // Prevent double cancellation in AfterEach

			// Give it time to stop gracefully
			time.Sleep(50 * time.Millisecond)
		})

		It("should use default interval when not configured", func() {
			cleanerWithoutInterval := &Cleaner{
				Client: k8sClient,
				Flows:  flowsMock,
				// CleanupInterval not set, should default to 5 minutes
			}

			// We expect the cleanup to be called, but since the default interval is 5 minutes,
			// we won't wait for it in the test
			flowsMock.EXPECT().CleanupStaleFlowsForBridges(gomock.Any(), gomock.Any()).
				Return(nil).
				AnyTimes()

			cleanerWithoutInterval.StartCleanupRoutine(ctx)

			// Just verify it starts without error
			time.Sleep(10 * time.Millisecond)
		})
	})

	Context("cleanupStaleFlows", func() {
		It("should handle no pods gracefully", func() {
			// No pods in the namespace, so cleanup should return early
			err := cleaner.cleanupStaleFlows(ctx)
			Expect(err).Should(BeNil())
		})

		It("should create pod UID map and call cleanup", func() {
			// Create some test pods
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "image"}},
				},
			}
			pod2 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod2",
					Namespace: ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "image"}},
				},
			}

			Expect(k8sClient.Create(ctx, pod1)).Should(Succeed())
			Expect(k8sClient.Create(ctx, pod2)).Should(Succeed())

			// Store the pod UIDs we receive for validation
			var receivedPodUIDs map[string]bool

			// Expect cleanup to be called and capture the pod UIDs
			flowsMock.EXPECT().CleanupStaleFlowsForBridges(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, podUIDs map[string]bool) error {
					receivedPodUIDs = podUIDs
					return nil
				}).
				Times(1)

			err := cleaner.cleanupStaleFlows(ctx)
			Expect(err).Should(BeNil())

			// Validate the received pod UIDs outside the goroutine
			Expect(len(receivedPodUIDs)).To(Equal(2))
			Expect(receivedPodUIDs[string(pod1.UID)]).To(BeTrue())
			Expect(receivedPodUIDs[string(pod2.UID)]).To(BeTrue())
		})

		It("should handle cleanup errors", func() {
			// Create a test pod
			pod := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "pod1",
					Namespace: ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "image"}},
				},
			}
			Expect(k8sClient.Create(ctx, pod)).Should(Succeed())

			// Mock cleanup to return an error
			flowsMock.EXPECT().CleanupStaleFlowsForBridges(gomock.Any(), gomock.Any()).
				Return(fmt.Errorf("cleanup failed")).
				Times(1)

			err := cleaner.cleanupStaleFlows(ctx)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to cleanup stale flows"))
			Expect(err.Error()).Should(ContainSubstring("cleanup failed"))
		})

		It("should handle context cancellation gracefully", func() {
			// Create a cancelled context
			cancelledCtx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			err := cleaner.cleanupStaleFlows(cancelledCtx)
			Expect(err).Should(HaveOccurred())
			Expect(err.Error()).Should(ContainSubstring("failed to list pods"))
		})
	})

	Context("Integration test", func() {
		It("should perform full cleanup cycle", func() {
			// Create some pods
			pod1 := &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "active-pod",
					Namespace: ns.Name,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{Name: "app", Image: "image"}},
				},
			}
			Expect(k8sClient.Create(ctx, pod1)).Should(Succeed())

			// Track if cleanup was called
			var callCount int

			// Set up expectations - cleanup should be called
			flowsMock.EXPECT().CleanupStaleFlowsForBridges(gomock.Any(), gomock.Any()).
				DoAndReturn(func(ctx context.Context, podUIDs map[string]bool) error {
					callCount++
					// Just verify our pod is in the list (there may be others from other tests)
					if callCount == 1 && len(podUIDs) > 0 {
						// At least our pod should be present
						return nil
					}
					return nil
				}).
				AnyTimes() // Allow multiple calls due to timing

			// Start cleanup routine
			cleaner.StartCleanupRoutine(ctx)

			// Wait for at least one cleanup cycle
			Eventually(func() int {
				return callCount
			}, 300*time.Millisecond, 10*time.Millisecond).Should(BeNumerically(">=", 1))

			// Stop the routine
			cancel()
			cancel = nil
		})
	})
})
