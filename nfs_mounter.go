package nfsdriver

import (
	"code.cloudfoundry.org/goshims/execshim"
	"context"
	"code.cloudfoundry.org/lager"
	"fmt"
)

//go:generate counterfeiter -o nfsdriverfakes/fake_mounter.go . Mounter
type Mounter interface {
	Mount(logger lager.Logger, ctx context.Context, source string, target string, opts map[string]interface{}) error
	Unmount(logger lager.Logger, ctx context.Context, target string) error
}

type nfsMounter struct {
	exec execshim.Exec
	fstype string
	defaultOpts string
}

func NewNfsMounter(exec execshim.Exec, fstype, defaultOpts string) Mounter {
	return &nfsMounter{exec, fstype, defaultOpts}
}

func (m *nfsMounter) Mount(logger lager.Logger, ctx context.Context, source string, target string, opts map[string]interface{}) error {
	cmd := m.exec.CommandContext(ctx, "mount", "-t", m.fstype, "-o", m.defaultOpts, source, target)
	output, err := cmd.CombinedOutput()
	if err != nil {
		logger.Error("nfs-mount-failed", err, lager.Data{"err": output})
		err = fmt.Errorf("%s:(%s)", output, err.Error())
	}
	return err
}

func (m *nfsMounter) Unmount(logger lager.Logger, ctx context.Context, target string) (err error) {
	cmd := m.exec.CommandContext(ctx, "umount", target)
	return cmd.Run()
}
