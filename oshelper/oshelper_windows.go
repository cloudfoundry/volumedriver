// +build windows

package oshelper

import "code.cloudfoundry.org/nfsdriver"

type osHelper struct {
}

func NewOsHelper() nfsdriver.OsHelper {
	return &osHelper{}
}

func (o *osHelper) Umask(mask int) (oldmask int) {
	return 0
}
