package invoker

import (
	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/lager"
	"errors"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

//go:generate counterfeiter -o ../invokerfakes/fake_invoker.go . Invoker
type Invoker interface {
	Invoke(env dockerdriver.Env, executable string, args []string) (InvokeResult, error)
}

type InvokeResult struct {
	cmd          *exec.Cmd
	outputBuffer *Buffer
	errorBuffer  *Buffer
	logger       lager.Logger
}

func (i InvokeResult) StdError() string {
	return i.errorBuffer.String()
}

func (i InvokeResult) StdOutput() string {
	return i.outputBuffer.String()
}

func (i InvokeResult) Wait() error {
	return i.cmd.Wait()
}

func (i InvokeResult) WaitFor(stringToWaitFor string, duration time.Duration) error {
	var errChan = make(chan error, 1)
	go func() {
		err := i.cmd.Wait()
		if err != nil {
			errChan <- err
		}
		close(errChan)
	}()

	timeout := time.After(duration)
	for {
		select {
		case e := <-errChan:
			if e == nil && !i.isExpectedTextContainedInStdOut(stringToWaitFor) {
				return errors.New("command finished without expected Text")
			}
			return e
		case <-timeout:
			err := syscall.Kill(-i.cmd.Process.Pid, syscall.SIGKILL)
			if err != nil {
				i.logger.Info("command-sigkill-error", lager.Data{"desc": err.Error()})
			}
			return errors.New("command timed out")
		default:
			if i.isExpectedTextContainedInStdOut(stringToWaitFor) {
				return nil
			}
		}
	}
}

func (i InvokeResult) isExpectedTextContainedInStdOut(stringToWaitFor string) bool {
	return strings.Contains(i.StdOutput(), stringToWaitFor)
}

