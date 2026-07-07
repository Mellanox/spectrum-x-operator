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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	SyncStatusUnknown    State = "Unknown"
	SyncStatusInProgress State = "InProgress"
	SyncStatusFailed     State = "Failed"
	SyncStatusSucceeded  State = "Succeeded"
)

type NicSelector struct {
	// PF selector
	// +kubebuilder:validation:MinItems=1
	PfNames []string `json:"pfNames"`
}

// +kubebuilder:validation:XValidation:rule="!has(self.cidrPoolRef) || !has(self.ipam)",message="Only one of cidrPoolRef or ipam can be specified"
type RailTopology struct {
	// Rail topology name
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
	// PF selector
	NicSelector NicSelector `json:"nicSelector"`
	// Reference to a CIDR Pool resource
	CidrPoolRef string `json:"cidrPoolRef,omitempty"`
	// Advanced IPAM configuration
	IPAM string `json:"ipam,omitempty"`
	// MTU
	// +kubebuilder:validation:Minimum=0
	MTU int `json:"mtu"`
}

// SpectrumXRailPoolConfigSpec defines the desired state of SpectrumXRailPoolConfig.
type SpectrumXRailPoolConfigSpec struct {
	// +kubebuilder:default:=true
	DraEnabled bool `json:"draEnabled,omitempty"`
	// +kubebuilder:validation:Optional
	// NodeSelector specifies a selector for Spectrum-X nodes
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
	// +kubebuilder:default:=1
	// maxUnavailable defines either an integer number or percentage
	// of nodes in the pool that can be configured in a parallel.
	MaxUnavailable *intstr.IntOrString `json:"maxUnavailable,omitempty"`
	// Namespace of the NetworkAttachmentDefinition custom resource
	NetworkNamespace string `json:"networkNamespace,omitempty"`
	// +kubebuilder:validation:Minimum=1
	// Number of VFs for each PF
	NumVfs int `json:"numVfs"`
	// Rails topology list
	// +kubebuilder:validation:MinItems=1
	RailTopology []RailTopology `json:"railTopology"`
}

// State represents reconcile state of the system.
type State string

// NodeState defines a finer-grained view of the observed state of SpectrumXRailPoolConfig.
type NodeState struct {
	// Name of the node this state refers to.
	Name string `json:"name"`
	// The state of the node configuration. ("Unknown", "InProgress", "Failed", "Succeeded")
	// +kubebuilder:validation:Enum={"Unknown", "InProgress", "Failed", "Succeeded"}
	State State `json:"state"`
	// ObservedGeneration is the SpectrumXRailPoolConfig generation this node state was reported for.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Message is a human-readable message indicating details about why
	// the state is in this condition
	Message string `json:"message,omitempty"`
}

// SpectrumXRailPoolConfigStatus defines the observed state of SpectrumXRailPoolConfig.
type SpectrumXRailPoolConfigStatus struct {
	SyncStatus State `json:"syncStatus,omitempty"`
	// NodeStates provide a finer view of the observed state
	NodeStates         []NodeState `json:"nodeStates,omitempty"`
	ObservedGeneration int64       `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.syncStatus`,priority=0
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`,priority=0

// SpectrumXRailPoolConfig is the Schema for the spectrumxrailpoolconfigs API.
type SpectrumXRailPoolConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   SpectrumXRailPoolConfigSpec   `json:"spec,omitempty"`
	Status SpectrumXRailPoolConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// SpectrumXRailPoolConfigList contains a list of SpectrumXRailPoolConfig.
type SpectrumXRailPoolConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []SpectrumXRailPoolConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&SpectrumXRailPoolConfig{}, &SpectrumXRailPoolConfigList{})
}
