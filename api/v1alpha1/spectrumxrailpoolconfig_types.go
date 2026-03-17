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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	SyncStatusUnknown    = "Unknown"
	SyncStatusInProgress = "InProgress"
	SyncStatusFailed     = "Failed"
	SyncStatusSucceeded  = "Succeeded"
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
	// Namespace of the NetworkAttachmentDefinition custom resource
	NetworkNamespace string `json:"networkNamespace,omitempty"`
	// +kubebuilder:validation:Minimum=0
	// Number of VFs for each PF
	NumVfs int `json:"numVfs"`
	// Rails topology list
	// +kubebuilder:validation:MinItems=1
	RailTopology []RailTopology `json:"railTopology"`
}

// SpectrumXRailPoolConfigStatus defines the observed state of SpectrumXRailPoolConfig.
type SpectrumXRailPoolConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	SyncStatus         string `json:"syncStatus,omitempty"`
	ObservedGeneration int64  `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
