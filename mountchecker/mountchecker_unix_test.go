//go:build linux || darwin
// +build linux darwin

package mountchecker_test

import (
	"errors"
	"io"
	"regexp"

	"code.cloudfoundry.org/goshims/bufioshim/bufio_fake"
	"code.cloudfoundry.org/goshims/osshim/os_fake"
	"code.cloudfoundry.org/volumedriver/mountchecker"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Mountchecker", func() {
	var (
		fakeOs               *os_fake.FakeOs
		fakeBufio            *bufio_fake.FakeBufio
		fakeProcMountsFile   *os_fake.FakeFile
		fakeProcMountsReader *bufio_fake.FakeReader
		mountChecker         mountchecker.Checker
	)

	BeforeEach(func() {
		fakeProcMountsFile = &os_fake.FakeFile{}

		fakeOs = &os_fake.FakeOs{}
		fakeOs.OpenReturns(fakeProcMountsFile, nil)

		fakeProcMountsReader = &bufio_fake.FakeReader{}
		fakeProcMountsReader.ReadStringReturnsOnCall(0, "nfsserver:/export/dir /mount/path nfs options 0 0\n", nil)
		fakeProcMountsReader.ReadStringReturnsOnCall(1, "nfsserver:/export/dir /some/path nfs options 0 0\n", nil)
		fakeProcMountsReader.ReadStringReturnsOnCall(2, "", io.EOF)

		fakeBufio = &bufio_fake.FakeBufio{}
		fakeBufio.NewReaderReturns(fakeProcMountsReader)
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

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(1))
			})

		})

		Context("when an intermediate mount exists", func() {
			BeforeEach(func() {
				fakeProcMountsReader.ReadStringReturnsOnCall(0, "nfsserver:/export/dir /mount/path_mapfs nfs options 0 0\n", nil)
				fakeProcMountsReader.ReadStringReturnsOnCall(1, "", io.EOF)
			})

			It("returns false", func() {
				exists, err := mountChecker.Exists("/mount/path")
				Expect(err).NotTo(HaveOccurred())
				Expect(exists).To(BeFalse())
			})

		})

		Context("when a mount path does not exist", func() {
			It("returns false", func() {
				exists, err := mountChecker.Exists("/other/path")
				Expect(err).NotTo(HaveOccurred())
				Expect(exists).To(BeFalse())
			})

			Context("when the path being checked is a regexp", func() {
				It("return false", func() {
					exists, err := mountChecker.Exists("^/other/.*")
					Expect(err).NotTo(HaveOccurred())
					Expect(exists).To(BeFalse())
				})
			})
		})

		Context("when /proc/mounts cannot be opened", func() {
			BeforeEach(func() {
				fakeOs.OpenReturns(nil, errors.New("open failed"))
			})

			It("returns an error", func() {
				_, err := mountChecker.Exists("/mount/path")
				Expect(err).To(MatchError("open failed"))

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(0))
			})
		})

		Context("when reading /proc/mounts fails", func() {
			BeforeEach(func() {
				fakeProcMountsReader.ReadStringReturnsOnCall(0, "", errors.New("read failed"))
			})

			It("returns an error", func() {
				_, err := mountChecker.Exists("/mount/path")
				Expect(err).To(MatchError("read failed"))

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(1))
			})
		})

		Context("when closing /proc/mounts fails", func() {
			BeforeEach(func() {
				fakeProcMountsFile.CloseReturns(errors.New("close failed"))
			})

			It("returns an error", func() {
				_, err := mountChecker.Exists("/mount/path")
				Expect(err).To(MatchError("close failed"))

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(1))
			})
		})

	})

	Describe("List", func() {
		It("returns a list of mount paths matching a regexp", func() {
			pattern, err := regexp.Compile("^/mount/.*")
			Expect(err).NotTo(HaveOccurred())
			mounts, err := mountChecker.List(pattern)
			Expect(err).NotTo(HaveOccurred())
			Expect(mounts).To(ConsistOf([]string{
				"/mount/path",
			}))
		})

		Context("when /proc/mounts cannot be opened", func() {
			BeforeEach(func() {
				fakeOs.OpenReturns(nil, errors.New("open failed"))
			})

			It("returns an error", func() {
				pattern, err := regexp.Compile("/mount/path")
				Expect(err).NotTo(HaveOccurred())
				_, err = mountChecker.List(pattern)
				Expect(err).To(MatchError("open failed"))
			})
		})

	})
})
