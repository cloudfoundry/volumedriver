package nfsdriver

import (
	"code.cloudfoundry.org/goshims/execshim"
	"context"
)

//go:generate counterfeiter -o nfsdriverfakes/fake_mounter.go . Mounter
type Mounter interface {
	Mount(ctx context.Context, source string, target string, fstype string, flags uintptr, data string) ([]byte, error)
	Unmount(ctx context.Context, target string, flags int) (err error)
}

type nfsMounter struct {
	exec execshim.Exec
}

func NewNfsMounter(exec execshim.Exec) Mounter {
	return &nfsMounter{exec}
}

func (m *nfsMounter) Mount(ctx context.Context, source string, target string, fstype string, flags uintptr, data string) ([]byte, error) {
	cmd := m.exec.CommandContext(ctx, "mount", "-t", fstype, "-o", data, source, target)
	return cmd.CombinedOutput()
}

func (m *nfsMounter) Unmount(ctx context.Context, target string, flags int) (err error) {
	cmd := m.exec.CommandContext(ctx, "umount", target)
	return cmd.Run()
}
