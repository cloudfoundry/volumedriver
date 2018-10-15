package procmounts_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestProcmounts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Procmounts Suite")
}
