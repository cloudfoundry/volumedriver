package invoker_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"testing"
)

func TestInvoker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Invoker Suite")
}
