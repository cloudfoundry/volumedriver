//go:build !windows

package volumedriver_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/dockerdriver/driverhttp"
	"code.cloudfoundry.org/goshims/filepathshim"
	"code.cloudfoundry.org/goshims/osshim"
	"code.cloudfoundry.org/goshims/timeshim"
	"code.cloudfoundry.org/lager/v3/lagertest"
	"code.cloudfoundry.org/volumedriver"
	"code.cloudfoundry.org/volumedriver/oshelper"
	"code.cloudfoundry.org/volumedriver/volumedriverfakes"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("state file on a real filesystem", func() {
	var (
		root    string
		mounter *volumedriverfakes.FakeMounter
		env     dockerdriver.Env
	)

	checker := &volumedriverfakes.FakeMountChecker{}

	newDriver := func() *volumedriver.VolumeDriver {
		return volumedriver.NewVolumeDriver(lagertest.NewTestLogger("fs"), &osshim.OsShim{}, &filepathshim.FilepathShim{},
			&timeshim.TimeShim{}, checker, root, mounter, oshelper.NewOsHelper())
	}

	BeforeEach(func() {
		root = GinkgoT().TempDir()
		mounter = &volumedriverfakes.FakeMounter{}
		checker.ExistsReturns(true, nil)
		env = driverhttp.NewHttpDriverEnv(lagertest.NewTestLogger("fs"), GinkgoT().Context())
	})

	stateFile := func() string { return filepath.Join(root, "driver-state.json") }

	create := func(d *volumedriver.VolumeDriver, name string) {
		resp := d.Create(env, dockerdriver.CreateRequest{Name: name, Opts: map[string]interface{}{"source": "//1.2.3.4/share"}})
		Expect(resp.Err).To(BeEmpty())
	}

	It("persists 0600 even under umask 0, leaves no tmp file, and round-trips", func() {
		old := syscall.Umask(0)
		defer syscall.Umask(old)

		d := newDriver()
		create(d, "vol")

		info, err := os.Stat(stateFile())
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))
		Expect(stateFile() + ".tmp").NotTo(BeAnExistingFile())

		Expect(newDriver().Get(env, dockerdriver.GetRequest{Name: "vol"}).Err).To(BeEmpty())
	})

	It("does not write through a symlink planted at the state path or tmp path", func() {
		victim := filepath.Join(GinkgoT().TempDir(), "victim")
		Expect(os.WriteFile(victim, []byte("keep"), 0644)).To(Succeed())
		Expect(os.Symlink(victim, stateFile())).To(Succeed())
		Expect(os.Symlink(victim, stateFile()+".tmp")).To(Succeed())

		create(newDriver(), "vol")

		got, err := os.ReadFile(victim)
		Expect(err).NotTo(HaveOccurred())
		Expect(string(got)).To(Equal("keep"))
		info, err := os.Lstat(stateFile())
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().IsRegular()).To(BeTrue())
	})

	It("does not read a symlinked state file", func() {
		target := filepath.Join(GinkgoT().TempDir(), "real.json")
		Expect(os.WriteFile(target, []byte(`{"vol":{"Name":"vol","Mountpoint":"/etc","MountCount":1}}`), 0600)).To(Succeed())
		Expect(os.Symlink(target, stateFile())).To(Succeed())

		Expect(newDriver().List(env).Volumes).To(BeEmpty())
	})

	It("does not block on a FIFO state file", func() {
		Expect(syscall.Mkfifo(stateFile(), 0600)).To(Succeed())

		done := make(chan struct{})
		go func() {
			defer GinkgoRecover()
			Expect(newDriver().List(env).Volumes).To(BeEmpty())
			close(done)
		}()
		Eventually(done, 5*time.Second).Should(BeClosed())
	})

	It("rejects a hardlinked state file and leaves the other link's mode alone", func() {
		data, err := json.Marshal(map[string]volumedriver.NfsVolumeInfo{
			"vol": {VolumeInfo: dockerdriver.VolumeInfo{Name: "vol", Mountpoint: "/etc", MountCount: 1}},
		})
		Expect(err).NotTo(HaveOccurred())
		victim := filepath.Join(GinkgoT().TempDir(), "victim")
		Expect(os.WriteFile(victim, data, 0644)).To(Succeed())
		Expect(os.Link(victim, stateFile())).To(Succeed())

		d := newDriver()
		Expect(d.List(env).Volumes).To(BeEmpty())
		info, err := os.Stat(victim)
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0644)))
	})

	It("tightens a legacy 0777 state file and never unmounts a forged path", func() {
		data, err := json.Marshal(map[string]volumedriver.NfsVolumeInfo{
			"vol":    {VolumeInfo: dockerdriver.VolumeInfo{Name: "vol", Mountpoint: "/etc", MountCount: 1}},
			"../etc": {VolumeInfo: dockerdriver.VolumeInfo{Name: "../etc", Mountpoint: "/etc", MountCount: 1}},
		})
		Expect(err).NotTo(HaveOccurred())
		Expect(os.WriteFile(stateFile(), data, 0600)).To(Succeed())
		Expect(os.Chmod(stateFile(), 0777)).To(Succeed())
		Expect(os.Mkdir(filepath.Join(root, "vol"), 0755)).To(Succeed())

		d := newDriver()
		info, err := os.Stat(stateFile())
		Expect(err).NotTo(HaveOccurred())
		Expect(info.Mode().Perm()).To(Equal(os.FileMode(0600)))

		Expect(d.Drain(env)).To(Succeed())
		Expect(mounter.UnmountCallCount()).To(Equal(1))
		_, target := mounter.UnmountArgsForCall(0)
		Expect(target).To(Equal(filepath.Join(root, "vol")))
	})
})
