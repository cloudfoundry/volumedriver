package mountchecker_test

import (
	"errors"
	"os"

	"code.cloudfoundry.org/goshims/bufioshim/bufio_fake"
	"code.cloudfoundry.org/goshims/osshim/os_fake"
	"code.cloudfoundry.org/volumedriver/mountchecker"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Mountchecker", func() {
	var (
		fakeOs       *os_fake.FakeOs
		fakeBufio    *bufio_fake.FakeBufio
		mountChecker mountchecker.Checker
	)

	BeforeEach(func() {
		fakeOs = &os_fake.FakeOs{}
		fakeBufio = &bufio_fake.FakeBufio{}
	})

	JustBeforeEach(func() {
		mountChecker = mountchecker.NewChecker(fakeBufio, fakeOs)
	})

	Describe("Exists", func() {
		Context("when a mount path exists", func() {
			It("returns true", func() {
				exists, err := mountChecker.Exists("/mount/path")
				Expect(err).NotTo(HaveOccurred())
				Expect(exists).To(BeTrue())

				Expect(fakeOs.StatCallCount()).To(Equal(1))

				path := fakeOs.StatArgsForCall(0)
				Expect(path).To(Equal("/mount/path"))
			})
		})

		Context("when a mount path does not exist", func() {
			BeforeEach(func() {
				fakeOs.StatReturns(nil, os.ErrNotExist)
			})

			It("returns false", func() {
				exists, err := mountChecker.Exists("/other/path")
				Expect(err).NotTo(HaveOccurred())
				Expect(exists).To(BeFalse())
			})
		})

		Context("when stat fails", func() {
			BeforeEach(func() {
				fakeOs.StatReturns(nil, errors.New("badness"))
			})

			It("returns the stat error", func() {
				_, err := mountChecker.Exists("/other/path")
				Expect(err).To(MatchError("badness"))
			})
		})
	})

	Describe("List", func() {
		It("returns an empty list", func() {
			mounts, err := mountChecker.List("^/anything/.*")
			Expect(err).NotTo(HaveOccurred())
			Expect(mounts).To(ConsistOf([]string{}))
		})
	})
})
