package mountchecker_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestProcmounts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "MountChecker Suite")
}
