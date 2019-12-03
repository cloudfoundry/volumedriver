package invoker

import (
	"bytes"
	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/goshims/execshim"
	"code.cloudfoundry.org/goshims/syscallshim"
	"code.cloudfoundry.org/lager"
	"context"
	"syscall"
)

type pgroupInvoker struct {
	useExec     execshim.Exec
	syscallShim syscallshim.Syscall
}

func NewProcessGroupInvoker() Invoker {
	return NewProcessGroupInvokerWithExec(&execshim.ExecShim{}, &syscallshim.SyscallShim{})
}

func NewProcessGroupInvokerWithExec(useExec execshim.Exec, syscallShim syscallshim.Syscall) Invoker {
	return &pgroupInvoker{useExec, syscallShim}
}

func (r *pgroupInvoker) Invoke(env dockerdriver.Env, executable string, cmdArgs []string) ([]byte, error) {
	logger := env.Logger().Session("invoking-command-pgroup", lager.Data{"executable": executable, "args": cmdArgs})
	logger.Info("start")
	defer logger.Info("end")

	cmdHandle := r.useExec.CommandContext(context.Background(), executable, cmdArgs...)
	cmdHandle.SysProcAttr().Setpgid = true

	var outb bytes.Buffer
	cmdHandle.SetStdout(&outb)
	cmdHandle.SetStderr(&outb)
	err := cmdHandle.Start()
	if err != nil {
		logger.Error("command-start-failed", err, lager.Data{"exe": executable, "output": outb.Bytes()})
		return nil, err
	}

	complete := make(chan bool)

	go func() {
		select {
		case <-complete:
			// noop
		case <-env.Context().Done():
			logger.Info("command-sigkill", lager.Data{"exe": executable, "pid": -cmdHandle.Pid()})
			r.syscallShim.Kill(-cmdHandle.Pid(), syscall.SIGKILL)
		}
	}()

	err = cmdHandle.Wait()
	if err != nil {
		logger.Error("command-failed", err, lager.Data{"exe": executable, "output": outb.Bytes()})
		return outb.Bytes(), err
	}

	close(complete)

	return outb.Bytes(), nil
}
