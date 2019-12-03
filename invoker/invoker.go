package invoker

import "code.cloudfoundry.org/dockerdriver"

//go:generate counterfeiter -o ../dockerdriverfakes/fake_invoker.go . Invoker

type Invoker interface {
	Invoke(env dockerdriver.Env, executable string, args []string) ([]byte, error)
}
