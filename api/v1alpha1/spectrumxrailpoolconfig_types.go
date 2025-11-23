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

// SpectrumXRailPoolConfigSpec defines the desired state of SpectrumXRailPoolConfig.
type SpectrumXRailPoolConfigSpec struct {
	// Type of the pool config
	// +kubebuilder:validation:Enum=none;swplb;hwplb;uniplane
	// +kubebuilder:validation:Required
	MultiplaneMode string `json:"multiplaneMode"`

	// Reference to a SriovNetworkNodePolicy resource
	// +kubebuilder:validation:Required
	SriovNetworkNodePolicyRef string `json:"sriovNetworkNodePolicyRef"`

	// Reference to a CIDR Pool resource
	// +kubebuilder:validation:Required
	CidrPoolRef string `json:"cidrPoolRef"`
}

// SpectrumXRailPoolConfigStatus defines the observed state of SpectrumXRailPoolConfig.
type SpectrumXRailPoolConfigStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
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
