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
	"errors"
	"testing"
	"time"
)

func TestExecuteSuccess(t *testing.T) {
	out, err := (&Exec{}).Execute("echo hi")
	if err != nil || out != "hi" {
		t.Fatalf("got %q, %v", out, err)
	}
}

func TestExecuteTimeoutKillsChildren(t *testing.T) {
	old := DefaultTimeout
	DefaultTimeout = 200 * time.Millisecond
	defer func() { DefaultTimeout = old }()

	start := time.Now()
	// sh forks sleep as a child; both must be killed or the pipe stays open.
	_, err := (&Exec{}).Execute("sleep 30; echo done")
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("Execute did not return promptly: %s", elapsed)
	}
}
