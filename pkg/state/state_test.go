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

package state

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/Mellanox/spectrum-x-operator/api/v1alpha2"
)

func TestState(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "State Suite")
}

var _ = Describe("GetNodeState", func() {
	It("returns the matching node state when present", func() {
		states := []v1alpha2.NodeState{
			{Name: "node-a", State: v1alpha2.SyncStatusSucceeded},
			{Name: "node-b", State: v1alpha2.SyncStatusInProgress, Message: "pending"},
			{Name: "node-c", State: v1alpha2.SyncStatusFailed},
		}

		got, err := GetNodeState(states, "node-b")
		Expect(err).ToNot(HaveOccurred())
		Expect(got).ToNot(BeNil())
		Expect(got.Name).To(Equal("node-b"))
		Expect(got.State).To(Equal(v1alpha2.State(v1alpha2.SyncStatusInProgress)))
		Expect(got.Message).To(Equal("pending"))
	})

	It("returns an error when node is not found", func() {
		states := []v1alpha2.NodeState{
			{Name: "node-a", State: v1alpha2.SyncStatusSucceeded},
		}

		got, err := GetNodeState(states, "missing")
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("missing"))
		Expect(got).To(BeNil())
	})

	It("returns an error for an empty slice", func() {
		got, err := GetNodeState(nil, "node-a")
		Expect(err).To(HaveOccurred())
		Expect(got).To(BeNil())
	})
})
