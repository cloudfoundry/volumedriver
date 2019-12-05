package invoker

import (
	"bytes"
	"code.cloudfoundry.org/dockerdriver"
	"os/exec"
)

//go:generate counterfeiter -o ../dockerdriverfakes/fake_invoker.go . Invoker

type InvokeResult struct{
	Cmd          *exec.Cmd
	OutputBuffer *bytes.Buffer
	ErrorBuffer *bytes.Buffer
}

func (i InvokeResult) StdError() string {
	return i.ErrorBuffer.String()
}

func (i InvokeResult) StdOutput() string {
	return i.OutputBuffer.String()
}

func (i InvokeResult) Wait() error {
	return i.Cmd.Wait()
}

type Invoker interface {
	Invoke(env dockerdriver.Env, executable string, args []string) (InvokeResult, error)
}
