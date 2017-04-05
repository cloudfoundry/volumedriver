package nfsdriver_test

import (
	"errors"
	"fmt"
	"os"

	"encoding/json"

	"context"

	"code.cloudfoundry.org/goshims/execshim/exec_fake"
	"code.cloudfoundry.org/goshims/filepathshim/filepath_fake"
	"code.cloudfoundry.org/goshims/ioutilshim/ioutil_fake"
	"code.cloudfoundry.org/goshims/osshim/os_fake"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/nfsdriver"
	"code.cloudfoundry.org/nfsdriver/nfsdriverfakes"
	"code.cloudfoundry.org/voldriver"
	"code.cloudfoundry.org/voldriver/driverhttp"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sync"
	"time"
)

var _ = Describe("Nfs Driver", func() {
	var logger lager.Logger
	var ctx context.Context
	var env voldriver.Env
	var fakeOs *os_fake.FakeOs
	var fakeFilepath *filepath_fake.FakeFilepath
	var fakeIoutil *ioutil_fake.FakeIoutil
	var fakeMounter *nfsdriverfakes.FakeMounter
	var fakeCmd *exec_fake.FakeCmd
	var nfsDriver *nfsdriver.NfsDriver
	var mountDir string

	const volumeName = "test-volume-id"

	var ip string

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("nfsdriver-local")
		ctx = context.TODO()
		env = driverhttp.NewHttpDriverEnv(logger, ctx)

		mountDir = "/path/to/mount"

		ip = "1.1.1.1"

		fakeOs = &os_fake.FakeOs{}
		fakeFilepath = &filepath_fake.FakeFilepath{}
		fakeIoutil = &ioutil_fake.FakeIoutil{}
		fakeMounter = &nfsdriverfakes.FakeMounter{}
		fakeCmd = &exec_fake.FakeCmd{}
	})

	Context("when mountpoint verfication hangs", func() {
		It("cancel the mountpoint check", func() {
			var fileContents []byte
			fileContents = []byte("{" +
				"\"4d635e24-1e3e-47a6-8d34-515c1b2419a4\":{" +
				"\"Opts\":{\"source\":\"10.10.5.92\"}," +
				"\"Name\":\"4d635e24-1e3e-47a6-8d34-515c1b2419a4\", " +
				"\"Mountpoint\":\"/tmp/volumes/4d635e24-1e3e-47a6-8d34-515c1b2419a4\"," +
				"\"MountCount\":1" +
				"}}")
			fakeIoutil.ReadFileReturns(fileContents, nil)
			fakeCmd.WaitReturns(context.Canceled)

			nfsDriver = nfsdriver.NewNfsDriver(logger, fakeOs, fakeFilepath, fakeIoutil, mountDir, fakeMounter)
			Expect(fakeMounter.CheckCallCount()).To(Equal(1))
			_, expectedName, expectedMountPoint := fakeMounter.CheckArgsForCall(0)
			Expect(expectedMountPoint).To(Equal("/tmp/volumes/4d635e24-1e3e-47a6-8d34-515c1b2419a4"))
			Expect(expectedName).To(Equal("4d635e24-1e3e-47a6-8d34-515c1b2419a4"))
			Expect(nfsDriver).NotTo(BeNil())
		})
	})

	Context("created", func() {
		BeforeEach(func() {
			nfsDriver = nfsdriver.NewNfsDriver(logger, fakeOs, fakeFilepath, fakeIoutil, mountDir, fakeMounter)
		})

		Describe("#Activate", func() {
			It("returns Implements: VolumeDriver", func() {
				activateResponse := nfsDriver.Activate(env)
				Expect(len(activateResponse.Implements)).To(BeNumerically(">", 0))
				Expect(activateResponse.Implements[0]).To(Equal("VolumeDriver"))
			})
		})

		Describe("Mount", func() {

			Context("when the volume has been created", func() {

				var mountResponse voldriver.MountResponse

				BeforeEach(func() {
					setupVolume(env, nfsDriver, volumeName, ip)
					fakeFilepath.AbsReturns("/path/to/mount/", nil)
				})

				JustBeforeEach(func() {

					mountResponse = nfsDriver.Mount(env, voldriver.MountRequest{Name: volumeName})
				})

				It("should mount the volume on the efs filesystem", func() {
					Expect(mountResponse.Err).To(Equal(""))
					Expect(mountResponse.Mountpoint).To(Equal("/path/to/mount/" + volumeName))

					Expect(fakeFilepath.AbsCallCount() > 0).To(BeTrue())

					Expect(fakeMounter.MountCallCount()).To(Equal(1))
					_, from, to, _ := fakeMounter.MountArgsForCall(0)
					Expect(from).To(Equal(ip))
					Expect(to).To(Equal("/path/to/mount/" + volumeName))
				})

				It("should return 'source' in the mount Opts", func() {
					expected := map[string]interface{}{
						"source": ip,
					}
					Expect(fakeMounter.MountCallCount()).To(Equal(1))
					_, _, _, opts := fakeMounter.MountArgsForCall(0)
					Expect(opts).To(Equal(expected))
				})

				It("should write state", func() {
					// 1 - persist on create
					// 2 - persist on mount
					Expect(fakeIoutil.WriteFileCallCount()).To(Equal(2))
				})

				Context("when the file system cant be written to", func() {
					BeforeEach(func() {
						fakeIoutil.WriteFileReturns(errors.New("badness"))
					})

					It("returns an error in the response", func() {
						Expect(mountResponse.Err).To(Equal("persist state failed when mounting: badness"))
					})
				})

				It("returns the mount point on a /VolumeDriver.Get response", func() {
					getResponse := ExpectVolumeExists(env, nfsDriver, volumeName)
					Expect(getResponse.Volume.Mountpoint).To(Equal("/path/to/mount/" + volumeName))
				})

				Context("when we mount the volume again", func() {
					BeforeEach(func() {
						mountResponse = nfsDriver.Mount(env, voldriver.MountRequest{Name: volumeName})
					})

					It("doesn't return an error", func() {
						Expect(mountResponse.Err).To(Equal(""))
						Expect(mountResponse.Mountpoint).To(Equal("/path/to/mount/" + volumeName))
					})
				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					mountResponse := nfsDriver.Mount(env, voldriver.MountRequest{Name: "bla"})
					Expect(mountResponse.Err).To(Equal("Volume 'bla' must be created before being mounted"))
				})
			})
		})

		Describe("Unmount", func() {
			Context("when a volume has been created", func() {
				BeforeEach(func() {
					setupVolume(env, nfsDriver, volumeName, ip)
				})

				Context("when a volume has been mounted", func() {
					var unmountResponse voldriver.ErrorResponse

					BeforeEach(func() {
						setupMount(env, nfsDriver, volumeName, fakeFilepath, "")
					})

					JustBeforeEach(func() {
						unmountResponse = nfsDriver.Unmount(env, voldriver.UnmountRequest{
							Name: volumeName,
						})
					})

					It("doesn't return an error", func() {
						Expect(unmountResponse.Err).To(Equal(""))
					})

					It("After unmounting /VolumeDriver.Get returns no volume", func() {
						getResponse := nfsDriver.Get(env, voldriver.GetRequest{
							Name: volumeName,
						})

						Expect(getResponse.Err).To(Equal("Volume not found"))
					})

					It("/VolumeDriver.Unmount unmounts", func() {
						Expect(fakeMounter.UnmountCallCount()).To(Equal(1))
						_, removed := fakeMounter.UnmountArgsForCall(0)
						Expect(removed).To(Equal("/path/to/mount/" + volumeName))
					})

					It("writes the driver state to disk", func() {
						// 1 - create
						// 2 - mount
						// 3 - unmount
						Expect(fakeIoutil.WriteFileCallCount()).To(Equal(3))
					})

					Context("when it fails to write the driver state to disk", func() {
						BeforeEach(func() {
							fakeIoutil.WriteFileReturns(errors.New("badness"))
						})

						It("returns an error response", func() {
							Expect(unmountResponse.Err).To(Equal("failed to persist state when unmounting: badness"))
						})
					})

					Context("when the volume is mounted twice", func() {
						BeforeEach(func() {
							setupMount(env, nfsDriver, volumeName, fakeFilepath, "")
							// JustBefore each does an unmount
						})

						It("returns no error when unmounting", func() {
							Expect(unmountResponse.Err).To(Equal(""))
						})

						It("the volume should remain mounted (due to reference counting)", func() {
							getResponse := ExpectVolumeExists(env, nfsDriver, volumeName)
							Expect(getResponse.Volume.Mountpoint).To(Equal("/path/to/mount/" + volumeName))
						})

						Context("when unmounting again", func() {
							BeforeEach(func() {
								unmountResponse = nfsDriver.Unmount(env, voldriver.UnmountRequest{
									Name: volumeName,
								})
							})

							It("returns no error when unmounting", func() {
								Expect(unmountResponse.Err).To(Equal(""))
							})

							It("deleted the volume", func() {
								getResponse := nfsDriver.Get(env, voldriver.GetRequest{
									Name: volumeName,
								})

								Expect(getResponse.Err).To(Equal("Volume not found"))
							})
						})
					})

					Context("when the mountpath is not found on the filesystem", func() {
						BeforeEach(func() {
							fakeOs.StatReturns(nil, os.ErrNotExist)
						})

						It("returns an error", func() {
							Expect(unmountResponse.Err).To(Equal("Volume " + volumeName + " does not exist (path: /path/to/mount/" + volumeName + "), nothing to do!"))
						})

						It("/VolumeDriver.Get still returns the mountpoint", func() {
							getResponse := ExpectVolumeExists(env, nfsDriver, volumeName)
							Expect(getResponse.Volume.Mountpoint).NotTo(Equal(""))
						})
					})

					Context("when the mountpath cannot be accessed", func() {
						BeforeEach(func() {
							fakeOs.StatReturns(nil, errors.New("something weird"))
						})

						It("unmounts anyway", func() {
							Expect(unmountResponse.Err).To(Equal(""))
						})

						It("deleted the volume", func() {
							getResponse := nfsDriver.Get(env, voldriver.GetRequest{
								Name: volumeName,
							})

							Expect(getResponse.Err).To(Equal("Volume not found"))
						})
					})
				})

				Context("when the volume has not been mounted", func() {
					It("returns an error", func() {
						unmountResponse := nfsDriver.Unmount(env, voldriver.UnmountRequest{
							Name: volumeName,
						})

						Expect(unmountResponse.Err).To(Equal("Volume not previously mounted"))
					})
				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					unmountResponse := nfsDriver.Unmount(env, voldriver.UnmountRequest{
						Name: volumeName,
					})

					Expect(unmountResponse.Err).To(Equal(fmt.Sprintf("Volume '%s' not found", volumeName)))
				})
			})
		})

		Describe("Create", func() {
			Context("when create is called with a volume ID", func() {

				var createResponse voldriver.ErrorResponse

				JustBeforeEach(func() {
					opts := map[string]interface{}{"source": ip}
					createResponse = nfsDriver.Create(env, voldriver.CreateRequest{
						Name: volumeName,
						Opts: opts,
					})
				})

				It("should write state, but omit Opts for security", func() {
					Expect(fakeIoutil.WriteFileCallCount()).To(Equal(1))

					_, data, _ := fakeIoutil.WriteFileArgsForCall(0)
					Expect(data).To(ContainSubstring("\"Name\":\"" + volumeName + "\""))
					Expect(data).NotTo(ContainSubstring("\"Opts\""))
				})

				Context("when the file system cant be written to", func() {
					BeforeEach(func() {
						fakeIoutil.WriteFileReturns(errors.New("badness"))
					})

					It("returns an error in the response", func() {
						Expect(createResponse.Err).To(Equal("persist state failed when creating: badness"))
					})
				})
			})

			Context("when a second create is called with the same volume ID", func() {
				BeforeEach(func() {
					setupVolume(env, nfsDriver, "volume", ip)
				})

				Context("with the same opts", func() {
					It("does nothing", func() {
						setupVolume(env, nfsDriver, "volume", ip)
					})
				})
			})
		})

		Describe("Get", func() {
			Context("when the volume has been created", func() {
				It("returns the volume name", func() {
					volumeName := "test-volume"
					setupVolume(env, nfsDriver, volumeName, ip)
					ExpectVolumeExists(env, nfsDriver, volumeName)
				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					volumeName := "test-volume"
					ExpectVolumeDoesNotExist(env, nfsDriver, volumeName)
				})
			})
		})

		Describe("Path", func() {
			Context("when a volume is mounted", func() {
				var (
					volumeName string
				)
				BeforeEach(func() {
					volumeName = "my-volume"
					setupVolume(env, nfsDriver, volumeName, ip)
					setupMount(env, nfsDriver, volumeName, fakeFilepath, "")
				})

				It("returns the mount point on a /VolumeDriver.Path", func() {
					pathResponse := nfsDriver.Path(env, voldriver.PathRequest{
						Name: volumeName,
					})
					Expect(pathResponse.Err).To(Equal(""))
					Expect(pathResponse.Mountpoint).To(Equal("/path/to/mount/" + volumeName))
				})
			})

			Context("when a volume is not created", func() {
				It("returns an error on /VolumeDriver.Path", func() {
					pathResponse := nfsDriver.Path(env, voldriver.PathRequest{
						Name: "volume-that-does-not-exist",
					})
					Expect(pathResponse.Err).NotTo(Equal(""))
					Expect(pathResponse.Mountpoint).To(Equal(""))
				})
			})

			Context("when a volume is created but not mounted", func() {
				var (
					volumeName string
				)
				BeforeEach(func() {
					volumeName = "my-volume"
					setupVolume(env, nfsDriver, volumeName, ip)
				})

				It("returns an error on /VolumeDriver.Path", func() {
					pathResponse := nfsDriver.Path(env, voldriver.PathRequest{
						Name: "volume-that-does-not-exist",
					})
					Expect(pathResponse.Err).NotTo(Equal(""))
					Expect(pathResponse.Mountpoint).To(Equal(""))
				})
			})
		})

		Describe("List", func() {
			Context("when there are volumes", func() {
				var volumeName string
				BeforeEach(func() {
					volumeName = "test-volume-id"
					setupVolume(env, nfsDriver, volumeName, ip)
				})

				It("returns the list of volumes", func() {
					listResponse := nfsDriver.List(env)

					Expect(listResponse.Err).To(Equal(""))
					Expect(listResponse.Volumes[0].Name).To(Equal(volumeName))

				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					volumeName := "test-volume"
					ExpectVolumeDoesNotExist(env, nfsDriver, volumeName)
				})
			})
		})

		Describe("Remove", func() {

			var removeResponse voldriver.ErrorResponse

			JustBeforeEach(func() {
				removeResponse = nfsDriver.Remove(env, voldriver.RemoveRequest{
					Name: volumeName,
				})
			})

			It("fails if no volume name provided", func() {
				removeResponse := nfsDriver.Remove(env, voldriver.RemoveRequest{
					Name: "",
				})
				Expect(removeResponse.Err).To(Equal("Missing mandatory 'volume_name'"))
			})

			It("returns no error if the volume is not found", func() {
				Expect(removeResponse.Err).To(BeEmpty())
			})

			Context("when the volume has been created", func() {
				BeforeEach(func() {
					setupVolume(env, nfsDriver, volumeName, ip)
				})

				It("Remove succeeds", func() {
					Expect(removeResponse.Err).To(Equal(""))
					ExpectVolumeDoesNotExist(env, nfsDriver, volumeName)
				})

				It("doesn't unmount since there are not mounts", func() {
					Expect(fakeMounter.UnmountCallCount()).To(Equal(0))
				})

				It("should write state to disk", func() {
					// 1 create
					// 2 remove
					Expect(fakeIoutil.WriteFileCallCount()).To(Equal(2))
				})

				Context("when writing state to disk fails", func() {
					BeforeEach(func() {
						fakeIoutil.WriteFileReturns(errors.New("badness"))
					})

					It("should return an error response", func() {
						Expect(removeResponse.Err).NotTo(BeEmpty())
					})
				})

				Context("when volume has been mounted", func() {
					BeforeEach(func() {
						setupMount(env, nfsDriver, volumeName, fakeFilepath, "")
						fakeMounter.UnmountReturns(nil)
					})

					It("/VolumePlugin.Remove unmounts volume", func() {
						Expect(removeResponse.Err).To(Equal(""))
						Expect(fakeMounter.UnmountCallCount()).To(Equal(1))

						ExpectVolumeDoesNotExist(env, nfsDriver, volumeName)
					})
				})
			})

			Context("when the volume has not been created", func() {
				It("doesn't return an error", func() {
					removeResponse := nfsDriver.Remove(env, voldriver.RemoveRequest{
						Name: volumeName,
					})
					Expect(removeResponse.Err).To(BeEmpty())
				})
			})
		})

		Describe("Restoring Internal State", func() {
			const (
				PERSISTED_MOUNT_VALID   = true
				PERSISTED_MOUNT_INVALID = false
			)
			JustBeforeEach(func() {
				nfsDriver = nfsdriver.NewNfsDriver(logger, fakeOs, fakeFilepath, fakeIoutil, mountDir, fakeMounter)
			})

			Context("no state is persisted", func() {
				BeforeEach(func() {
					fakeIoutil.ReadFileReturns(nil, errors.New("file not found"))
				})

				It("returns an empty list when fetching the list of volumes", func() {
					Expect(nfsDriver.List(env)).To(Equal(voldriver.ListResponse{
						Volumes: []voldriver.VolumeInfo{},
					}))
				})
			})

			Context("when state is persisted", func() {
				BeforeEach(func() {
					fakeMounter.CheckReturns(PERSISTED_MOUNT_VALID)
					data, err := json.Marshal(map[string]nfsdriver.NfsVolumeInfo{
						"some-volume-name": {
							Opts: map[string]interface{}{"source": "123.456.789"},
							VolumeInfo: voldriver.VolumeInfo{
								Name:       "some-volume-name",
								Mountpoint: "/some/mount/point",
								MountCount: 1,
							},
						},
					})

					Expect(err).ToNot(HaveOccurred())
					fakeIoutil.ReadFileReturns(data, nil)
				})

				It("returns the persisted volumes when listing", func() {
					Expect(nfsDriver.List(env)).To(Equal(voldriver.ListResponse{
						Volumes: []voldriver.VolumeInfo{
							{Name: "some-volume-name", Mountpoint: "/some/mount/point", MountCount: 1},
						},
					}))
				})

				Context("when the mounts are not present", func() {
					BeforeEach(func() {
						fakeMounter.CheckReturns(PERSISTED_MOUNT_INVALID)
					})

					It("only returns the volumes that are present on disk", func() {
						Expect(nfsDriver.List(env)).To(Equal(voldriver.ListResponse{
							Volumes: []voldriver.VolumeInfo{},
						}))
					})
				})

				Context("when the state is corrupted", func() {
					BeforeEach(func() {
						fakeIoutil.ReadFileReturns([]byte("I have eleven toes."), nil)
					})
					It("will return no volumes", func() {
						Expect(nfsDriver.List(env)).To(Equal(voldriver.ListResponse{
							Volumes: []voldriver.VolumeInfo{},
						}))
					})
				})
			})
		})

		Describe("Thread safety", func() {
			BeforeEach(func() {
				fakeMounter.MountStub = func(env voldriver.Env, src string, tgt string, opts map[string]interface{}) error {
					time.Sleep(time.Duration(time.Now().UnixNano() % 200) * time.Millisecond)
					return nil
				}
			})
			It("maintains consistency", func() {
				var wg sync.WaitGroup

				opts := map[string]interface{}{"source": ip}
				createResponse := nfsDriver.Create(env, voldriver.CreateRequest{
					Name: volumeName,
					Opts: opts,
				})
				Expect(createResponse.Err).To(Equal(""))

				smashMount := func() {
					defer GinkgoRecover()
					defer wg.Done()

					mountResponse := nfsDriver.Mount(env, voldriver.MountRequest{Name: volumeName})
					Expect(mountResponse.Err).To(Equal(""))
				}

				smashUnmount := func() {
					defer GinkgoRecover()
					defer wg.Done()

					mountResponse := nfsDriver.Unmount(env, voldriver.UnmountRequest{Name: volumeName})
					Expect(mountResponse.Err).To(Equal(""))
				}

				wg.Add(5)
				for i := 0; i < 5; i++ {
					go smashMount()
				}
				wg.Wait()

				Expect(fakeMounter.MountCallCount()).To(Equal(1))

				wg.Add(5)
				for i := 0; i < 5; i++ {
					go smashUnmount()
				}
				wg.Wait()

				Expect(fakeMounter.UnmountCallCount()).To(Equal(1))

				removeResponse := nfsDriver.Remove(env, voldriver.RemoveRequest{
					Name: volumeName,
				})
				Expect(removeResponse.Err).To(Equal(""))
			})
		})
	})
})

func ExpectVolumeDoesNotExist(env voldriver.Env, efsDriver voldriver.Driver, volumeName string) {
	getResponse := efsDriver.Get(env, voldriver.GetRequest{
		Name: volumeName,
	})

	Expect(getResponse.Err).To(Equal("Volume not found"))
	Expect(getResponse.Volume.Name).To(Equal(""))
}

func ExpectVolumeExists(env voldriver.Env, efsDriver voldriver.Driver, volumeName string) voldriver.GetResponse {
	getResponse := efsDriver.Get(env, voldriver.GetRequest{
		Name: volumeName,
	})

	Expect(getResponse.Err).To(Equal(""))
	Expect(getResponse.Volume.Name).To(Equal(volumeName))
	return getResponse
}

func setupVolume(env voldriver.Env, nfsDriver voldriver.Driver, volumeName string, source string) {
	opts := map[string]interface{}{"source": source}
	createResponse := nfsDriver.Create(env, voldriver.CreateRequest{
		Name: volumeName,
		Opts: opts,
	})
	Expect(createResponse.Err).To(Equal(""))
}

func setupMount(env voldriver.Env, nfsDriver voldriver.Driver, volumeName string, fakeFilepath *filepath_fake.FakeFilepath, passcode string) {
	fakeFilepath.AbsReturns("/path/to/mount/", nil)
	mountResponse := nfsDriver.Mount(env, voldriver.MountRequest{Name: volumeName})
	Expect(mountResponse.Err).To(Equal(""))
	Expect(mountResponse.Mountpoint).To(Equal("/path/to/mount/" + volumeName))
}
