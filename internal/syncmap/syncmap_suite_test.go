package syncmap_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSyncmap(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Syncmap Suite")
}
