package volumedriver

import (
	"context"
	"fmt"
	"time"

	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/dockerdriver/driverhttp"
	"code.cloudfoundry.org/dockerdriver/invoker"
)

//go:generate counterfeiter -o volumedriverfakes/fake_mounter.go . Mounter
type Mounter interface {
	Mount(env dockerdriver.Env, source string, target string, opts map[string]interface{}) error
	Unmount(env dockerdriver.Env, target string) error
	Check(env dockerdriver.Env, name, mountPoint string) bool
	Purge(env dockerdriver.Env, path string)
}

type volumeMounter struct {
	invoker     invoker.Invoker
	fstype      string
	defaultOpts string
}

func NewVolumeMounter(invoker invoker.Invoker, fstype, defaultOpts string) Mounter {
	return &volumeMounter{invoker, fstype, defaultOpts}
}

func (m *volumeMounter) Mount(env dockerdriver.Env, source string, target string, opts map[string]interface{}) error {
	_, err := m.invoker.Invoke(env, "mount", []string{"-t", m.fstype, "-o", m.defaultOpts, source, target})
	return err
}

func (m *volumeMounter) Unmount(env dockerdriver.Env, target string) error {
	_, err := m.invoker.Invoke(env, "umount", []string{target})
	return err
}

func (m *volumeMounter) Check(env dockerdriver.Env, name, mountPoint string) bool {
	ctx, cncl := context.WithDeadline(context.TODO(), time.Now().Add(time.Second*5))
	defer cncl()
	env = driverhttp.EnvWithContext(ctx, env)
	_, err := m.invoker.Invoke(env, "mountpoint", []string{"-q", mountPoint})

	if err != nil {
		env.Logger().Info(fmt.Sprintf("unable to verify volume %s (%s)", name, err.Error()))
		return false
	}
	return true
}

func (m *volumeMounter) Purge(_ dockerdriver.Env, _ string) {
	// this is a no-op for now
}
