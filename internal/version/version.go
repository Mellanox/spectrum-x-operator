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

package version

import "fmt"

var (
	// values below are set via ldflags passed to go build command
	version       = "master@git"
	commit        = "unknown commit"
	date          = "unknown date"
	gitTreeState  = ""
	releaseStatus = ""
)

// GetVersionString returns version string for Spectrum-X Operator
func GetVersionString() string {
	return fmt.Sprintf("%s(%s%s), commit:%s, date:%s", version, gitTreeState, releaseStatus, commit, date)
}
