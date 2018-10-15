package procmounts_test

import (
	"errors"
	"io"

	"code.cloudfoundry.org/goshims/bufioshim/bufio_fake"
	"code.cloudfoundry.org/goshims/osshim/os_fake"
	"code.cloudfoundry.org/nfsdriver/procmounts"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Procmounts", func() {
	var (
		fakeOs               *os_fake.FakeOs
		fakeBufio            *bufio_fake.FakeBufio
		fakeProcMountsFile   *os_fake.FakeFile
		fakeProcMountsReader *bufio_fake.FakeReader
		procMountChecker     procmounts.Checker
	)

	BeforeEach(func() {
		fakeProcMountsFile = &os_fake.FakeFile{}

		fakeOs = &os_fake.FakeOs{}
		fakeOs.OpenReturns(fakeProcMountsFile, nil)

		fakeProcMountsReader = &bufio_fake.FakeReader{}
		fakeProcMountsReader.ReadStringReturnsOnCall(0, "nfsserver:/export/dir /mount/path nfs options 0 0\n", nil)
		fakeProcMountsReader.ReadStringReturnsOnCall(1, "", io.EOF)

		fakeBufio = &bufio_fake.FakeBufio{}
		fakeBufio.NewReaderReturns(fakeProcMountsReader)
	})

	JustBeforeEach(func() {
		procMountChecker = procmounts.NewChecker(fakeBufio, fakeOs)
	})

	Describe("Exists", func() {
		Context("when a mount path exists", func() {
			It("returns true", func() {
				exists, err := procMountChecker.Exists("/mount/path")
				Expect(err).NotTo(HaveOccurred())
				Expect(exists).To(BeTrue())

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(1))
			})
		})

		Context("when a mount path does not exist", func() {
			It("returns false", func() {
				exists, err := procMountChecker.Exists("/other/path")
				Expect(err).NotTo(HaveOccurred())
				Expect(exists).To(BeFalse())
			})
		})

		Context("when /proc/mounts cannot be opened", func() {
			BeforeEach(func() {
				fakeOs.OpenReturns(nil, errors.New("open failed"))
			})

			It("returns an error", func() {
				_, err := procMountChecker.Exists("/mount/path")
				Expect(err).To(MatchError("open failed"))

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(0))
			})
		})

		Context("when reading /proc/mounts fails", func() {
			BeforeEach(func() {
				fakeProcMountsReader.ReadStringReturnsOnCall(0, "", errors.New("read failed"))
			})

			It("returns an error", func() {
				_, err := procMountChecker.Exists("/mount/path")
				Expect(err).To(MatchError("read failed"))

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(1))
			})
		})

		Context("when closing /proc/mounts fails", func() {
			BeforeEach(func() {
				fakeProcMountsFile.CloseReturns(errors.New("close failed"))
			})

			It("returns an error", func() {
				_, err := procMountChecker.Exists("/mount/path")
				Expect(err).To(MatchError("close failed"))

				Expect(fakeProcMountsFile.CloseCallCount()).To(Equal(1))
			})
		})
	})
})
