package invoker_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestInvoker(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Invoker Suite")
}