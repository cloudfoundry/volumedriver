package invoker

import (
	"bytes"
	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/lager"
	"context"
	"os/exec"
	"syscall"
)

type pgroupInvoker struct {
}

func NewProcessGroupInvoker() Invoker {
	return &pgroupInvoker{}
}

func (r *pgroupInvoker) Invoke(env dockerdriver.Env, executable string, cmdArgs []string) (InvokeResult, error) {
	logger := env.Logger().Session("invoking-command-pgroup", lager.Data{"executable": executable, "args": cmdArgs})
	logger.Info("start")
	defer logger.Info("end")

	// We do not pass in the docker context to let the exec.Command handle timeout/cancel, because we want to kill the entire process group. (Mount spawns child processes, which we also want to kill)
	cmdHandle := exec.CommandContext(context.Background(), executable, cmdArgs...)
	cmdHandle.SysProcAttr = &syscall.SysProcAttr{}
	cmdHandle.SysProcAttr.Setpgid = true

	var stdOutBuffer, stdErrBuffer bytes.Buffer
	cmdHandle.Stdout = &stdOutBuffer
	cmdHandle.Stderr = &stdErrBuffer
	err := cmdHandle.Start()

	if err != nil {
		logger.Error("command-start-failed", err, lager.Data{"exe": executable, "output": stdOutBuffer.Bytes()})
		return InvokeResult{}, err
	}

	go func() {
		select {
		case <-env.Context().Done():
			logger.Info("command-sigkill", lager.Data{"exe": executable, "pid": -cmdHandle.Process.Pid})
			err := syscall.Kill(-cmdHandle.Process.Pid, syscall.SIGKILL)
			if err != nil {
				logger.Info("command-sigkill-error", lager.Data{"desc": err.Error()})
			}
			err = cmdHandle.Wait()
			if err != nil {
				logger.Info("command-sigkill-wait-error", lager.Data{"desc": err.Error()})
			}
		}
	}()

	return InvokeResult{Cmd: cmdHandle, OutputBuffer: &stdOutBuffer, ErrorBuffer: &stdErrBuffer}, nil
}
