package nfsdriver

import (
	"context"
	"fmt"
	"time"

	"code.cloudfoundry.org/voldriver"
	"code.cloudfoundry.org/voldriver/driverhttp"
	"code.cloudfoundry.org/voldriver/invoker"
)

//go:generate counterfeiter -o nfsdriverfakes/fake_mounter.go . Mounter
type Mounter interface {
	Mount(env voldriver.Env, source string, target string, opts map[string]interface{}) error
	Unmount(env voldriver.Env, target string) error
	Check(env voldriver.Env, name, mountPoint string) bool
	Purge(env voldriver.Env, path string)
}

type nfsMounter struct {
	invoker     invoker.Invoker
	fstype      string
	defaultOpts string
}

func NewNfsMounter(invoker invoker.Invoker, fstype, defaultOpts string) Mounter {
	return &nfsMounter{invoker, fstype, defaultOpts}
}

func (m *nfsMounter) Mount(env voldriver.Env, source string, target string, opts map[string]interface{}) error {
	_, err := m.invoker.Invoke(env, "mount", []string{"-t", m.fstype, "-o", m.defaultOpts, source, target})
	return err
}

func (m *nfsMounter) Unmount(env voldriver.Env, target string) error {
	_, err := m.invoker.Invoke(env, "umount", []string{target})
	return err
}

func (m *nfsMounter) Check(env voldriver.Env, name, mountPoint string) bool {
	ctx, _ := context.WithDeadline(context.TODO(), time.Now().Add(time.Second*5))
	env = driverhttp.EnvWithContext(ctx, env)
	_, err := m.invoker.Invoke(env, "mountpoint", []string{"-q", mountPoint})

	if err != nil {
		env.Logger().Info(fmt.Sprintf("unable to verify volume %s (%s)", name, err.Error()))
		return false
	}
	return true
}

func (m *nfsMounter) Purge(_ voldriver.Env, _ string) {
	// this is a no-op for now
}
