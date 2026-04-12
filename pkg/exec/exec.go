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

package exec

import (
	"fmt"
	osexec "os/exec"
	"strings"
)

//go:generate ../../bin/mockgen -package exec -destination mock_exec.go . API

type API interface {
	Execute(command string) (string, error)
	ExecutePrivileged(command string) (string, error)
}

type Exec struct{}

var _ API = (*Exec)(nil)

func (e *Exec) Execute(command string) (string, error) {
	out, err := osexec.Command("sh", "-c", command).CombinedOutput() //nolint:noctx
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

func (e *Exec) ExecutePrivileged(command string) (string, error) {
	return e.Execute(buildPrivilegedCommand(command))
}

func buildPrivilegedCommand(command string) string {
	// nsenter is used here to launch processes inside the container in a way that makes said processes feel
	// and behave as if they're running on the host directly rather than inside the container
	return fmt.Sprintf("nsenter --target 1 --net -- %s", command)
}
