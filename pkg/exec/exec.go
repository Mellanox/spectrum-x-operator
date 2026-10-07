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
	"context"
	"errors"
	"fmt"
	osexec "os/exec"
	"strings"
	"syscall"
	"time"
)

// DefaultTimeout bounds a single command execution. ovs-vsctl/ovs-ofctl can hang
// indefinitely when ovsdb-server or ovs-vswitchd is unhealthy.
var DefaultTimeout = 30 * time.Second

// waitDelay is how long to wait for I/O pipes to close after the process was killed.
const waitDelay = 5 * time.Second

// ErrTimeout is returned (wrapped) when a command exceeds its timeout.
var ErrTimeout = errors.New("command timed out")

//go:generate ../../bin/mockgen -package exec -destination mock_exec.go . API

type API interface {
	Execute(command string) (string, error)
	ExecutePrivileged(command string) (string, error)
}

type Exec struct{}

var _ API = (*Exec)(nil)

func (e *Exec) Execute(command string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), DefaultTimeout)
	defer cancel()

	// Commands are built by the operator itself, never from user-supplied input.
	cmd := osexec.CommandContext(ctx, "sh", "-c", command) //nolint:gosec
	// Run in its own process group so a timeout kills the whole tree (sh and the
	// ovs-* child), otherwise the child keeps the output pipe open.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = waitDelay

	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return "", fmt.Errorf("%w after %s: %s", ErrTimeout, DefaultTimeout, command)
	}
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
