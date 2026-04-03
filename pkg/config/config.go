/*
Copyright 2026 NVIDIA

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

// Package config contains configuration for the Operator.
package config

import (
	"sync"

	env "github.com/caarlos0/env/v11"
)

var (
	once           sync.Once
	operatorConfig *OperatorConfig
)

// OperatorConfig holds configuration for the Operator.
type OperatorConfig struct {
	XPlaneRepository string   `env:"XPLANE_REPOSITORY"`
	XPlaneImage      string   `env:"XPLANE_IMAGE"`
	XPlaneVersion    string   `env:"XPLANE_VERSION"`
	ImagePullSecrets []string `env:"IMAGE_PULL_SECRETS" envSeparator:","`
}

// FromEnv pulls the operator configuration from the environment.
func FromEnv() *OperatorConfig {
	once.Do(func() {
		operatorConfig = &OperatorConfig{}
		_ = env.Parse(operatorConfig)
	})
	return operatorConfig
}
