package exec

import (
	"fmt"
	osexec "os/exec"
	"strings"
)

//go:generate mockgen -package exec -destination mock_exec.go . API

type API interface {
	Execute(command string) (string, error)
	ExecutePrivileged(command string) (string, error)
}

type Exec struct{}

var _ API = (*Exec)(nil)

func (e *Exec) Execute(command string) (string, error) {
	out, err := osexec.Command("sh", "-c", command).CombinedOutput()
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
