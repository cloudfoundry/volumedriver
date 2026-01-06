package volumedriver_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/onsi/gomega/gbytes"

	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/dockerdriver/driverhttp"
	"code.cloudfoundry.org/goshims/filepathshim/filepath_fake"
	"code.cloudfoundry.org/goshims/osshim/os_fake"
	"code.cloudfoundry.org/goshims/timeshim/time_fake"
	"code.cloudfoundry.org/lager/v3/lagertest"
	"code.cloudfoundry.org/volumedriver"
	"code.cloudfoundry.org/volumedriver/oshelper"
	"code.cloudfoundry.org/volumedriver/volumedriverfakes"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Nfs Driver", func() {
	var logger *lagertest.TestLogger
	var ctx context.Context
	var env dockerdriver.Env
	var fakeOs *os_fake.FakeOs
	var fakeFilepath *filepath_fake.FakeFilepath
	var fakeTime *time_fake.FakeTime
	var fakeMounter *volumedriverfakes.FakeMounter
	var fakeMountChecker *volumedriverfakes.FakeMountChecker
	var volumeDriver *volumedriver.VolumeDriver
	var mountDir string

	const volumeName = "test-volume-id"

	var ip string

	BeforeEach(func() {
		logger = lagertest.NewTestLogger("volumedriver-local")
		ctx = context.TODO()
		env = driverhttp.NewHttpDriverEnv(logger, ctx)

		mountDir = "/path/to/mount"

		ip = "1.1.1.1"

		fakeOs = &os_fake.FakeOs{}
		fakeFilepath = &filepath_fake.FakeFilepath{}
		fakeTime = &time_fake.FakeTime{}
		fakeMounter = &volumedriverfakes.FakeMounter{}
		fakeMountChecker = &volumedriverfakes.FakeMountChecker{}
		fakeMountChecker.ExistsReturns(true, nil)
	})

	Context("created", func() {
		BeforeEach(func() {
			volumeDriver = volumedriver.NewVolumeDriver(logger, fakeOs, fakeFilepath, fakeTime, fakeMountChecker, mountDir, fakeMounter, oshelper.NewOsHelper())
		})

		Describe("#Activate", func() {
			It("returns Implements: VolumeDriver", func() {
				activateResponse := volumeDriver.Activate(env)
				Expect(len(activateResponse.Implements)).To(BeNumerically(">", 0))
				Expect(activateResponse.Implements[0]).To(Equal("VolumeDriver"))
			})
		})

		Describe("Mount", func() {

			Context("when the volume has been created", func() {

				var mountResponse dockerdriver.MountResponse

				BeforeEach(func() {
					setupVolume(env, volumeDriver, volumeName, ip)
					fakeFilepath.AbsReturns("/path/to/mount/", nil)
				})

				JustBeforeEach(func() {
					mountResponse = volumeDriver.Mount(env, dockerdriver.MountRequest{Name: volumeName})
				})

				It("should mount the volume", func() {
					Expect(mountResponse.Err).To(Equal(""))
					Expect(strings.Replace(mountResponse.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))

					Expect(fakeFilepath.AbsCallCount() > 0).To(BeTrue())

					Expect(fakeMounter.MountCallCount()).To(Equal(1))
					_, from, to, _ := fakeMounter.MountArgsForCall(0)
					Expect(from).To(Equal(ip))
					Expect(strings.Replace(to, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
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
					Expect(fakeOs.WriteFileCallCount()).To(Equal(2))
				})

				Context("when the file system cant be written to", func() {
					BeforeEach(func() {
						fakeOs.WriteFileReturns(errors.New("badness"))
					})

					It("returns an error in the response", func() {
						Expect(mountResponse.Err).To(Equal("persist state failed when mounting: badness"))
					})
				})

				It("returns the mount point on a /VolumeDriver.Get response", func() {
					getResponse := ExpectVolumeExists(env, volumeDriver, volumeName)
					Expect(strings.Replace(getResponse.Volume.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
				})

				Context("when mounter returns an error", func() {
					BeforeEach(func() {
						fakeMounter.MountReturns(errors.New("unsafe-error"))
					})

					It("should return a mount response with the error", func() {
						Expect(mountResponse.Err).To(Equal("unsafe-error"))
						Expect(mountResponse.Mountpoint).To(Equal(""))
					})
				})

				Context("when mounter returns an safe error", func() {
					BeforeEach(func() {
						fakeMounter.MountReturns(dockerdriver.SafeError{SafeDescription: "safe-error"})
					})

					It("should return a mount response with the error", func() {
						Expect(mountResponse.Err).To(Equal(`{"SafeDescription":"safe-error"}`))
						Expect(mountResponse.Mountpoint).To(Equal(""))
					})

				})

				Context("when the mount operation takes more than 8 seconds", func() {
					BeforeEach(func() {
						startTime := time.Now()
						fakeTime.NowReturnsOnCall(0, startTime)
						fakeTime.NowReturnsOnCall(1, startTime.Add(time.Second*9))
					})
					It("logs a warning", func() {
						Expect(logger.TestSink.Buffer()).Should(gbytes.Say("mount-duration-too-high"))
					})
				})

				Context("when we mount the volume again", func() {
					JustBeforeEach(func() {
						mountResponse = volumeDriver.Mount(env, dockerdriver.MountRequest{Name: volumeName})
					})

					It("doesn't return an error", func() {
						Expect(mountResponse.Err).To(Equal(""))
						Expect(strings.Replace(mountResponse.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
					})

					Context("when the volume is no longer mounted", func() {
						BeforeEach(func() {
							fakeMounter.CheckReturns(false)
						})
						It("remounts the volume", func() {
							Expect(fakeMounter.CheckCallCount()).NotTo(BeZero())
							Expect(fakeMounter.MountCallCount()).To(Equal(2))
						})
						It("doesn't return an error", func() {
							Expect(mountResponse.Err).To(Equal(""))
							Expect(strings.Replace(mountResponse.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
						})
					})
				})

				Context("when the driver is drained while there are still mounts", func() {
					var drainResponse error
					JustBeforeEach(func() {
						drainResponse = volumeDriver.Drain(env)
					})

					It("unmounts the volume", func() {
						Expect(drainResponse).NotTo(HaveOccurred())
						Expect(fakeMounter.UnmountCallCount()).NotTo(BeZero())
						_, name := fakeMounter.UnmountArgsForCall(0)
						Expect(strings.Replace(name, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
					})
					It("purges the directory", func() {
						Expect(drainResponse).NotTo(HaveOccurred())
						Expect(fakeMounter.PurgeCallCount()).NotTo(BeZero())
						_, path := fakeMounter.PurgeArgsForCall(0)
						Expect(path).To(Equal("/path/to/mount"))
					})
				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					mountResponse := volumeDriver.Mount(env, dockerdriver.MountRequest{Name: "bla"})
					Expect(mountResponse.Err).To(Equal("Volume 'bla' must be created before being mounted"))
				})
			})
			Context("when two volumes have been created", func() {

				var mountResponse dockerdriver.MountResponse

				BeforeEach(func() {
					setupVolume(env, volumeDriver, volumeName, ip)
					setupVolume(env, volumeDriver, volumeName+"2", ip)
					fakeFilepath.AbsReturns("/path/to/mount/", nil)

					fakeMounter.MountStub = func(env dockerdriver.Env, source string, target string, opts map[string]interface{}) error {
						time.Sleep(time.Millisecond * 100)
						return nil
					}
				})

				It("should mount both in parallel", func() {
					var wg sync.WaitGroup
					wg.Add(1)
					startTime := time.Now()
					go func() {
						mountResponse2 := volumeDriver.Mount(env, dockerdriver.MountRequest{Name: volumeName + "2"})
						Expect(mountResponse2.Err).To(Equal(""))
						wg.Done()
					}()
					mountResponse = volumeDriver.Mount(env, dockerdriver.MountRequest{Name: volumeName})
					Expect(mountResponse.Err).To(Equal(""))

					wg.Wait()
					elapsed := time.Since(startTime)
					Expect(elapsed).To(BeNumerically("<", time.Millisecond*150))
				})

			})
		})

		Describe("Unmount", func() {
			Context("when a volume has been created", func() {
				BeforeEach(func() {
					setupVolume(env, volumeDriver, volumeName, ip)
				})

				Context("when a volume has been mounted", func() {
					var unmountResponse dockerdriver.ErrorResponse

					BeforeEach(func() {
						setupMount(env, volumeDriver, volumeName, fakeFilepath)
					})

					JustBeforeEach(func() {
						unmountResponse = volumeDriver.Unmount(env, dockerdriver.UnmountRequest{
							Name: volumeName,
						})
					})

					It("doesn't return an error", func() {
						Expect(unmountResponse.Err).To(Equal(""))
					})

					It("After unmounting /VolumeDriver.Get returns no volume", func() {
						getResponse := volumeDriver.Get(env, dockerdriver.GetRequest{
							Name: volumeName,
						})

						Expect(getResponse.Err).To(Equal("volume not found"))
					})

					It("/VolumeDriver.Unmount unmounts", func() {
						Expect(fakeMounter.UnmountCallCount()).To(Equal(1))
						_, removed := fakeMounter.UnmountArgsForCall(0)
						Expect(strings.Replace(removed, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
					})

					It("writes the driver state to disk", func() {
						// 1 - create
						// 2 - mount
						// 3 - unmount
						Expect(fakeOs.WriteFileCallCount()).To(Equal(3))
					})

					Context("when it fails to write the driver state to disk", func() {
						BeforeEach(func() {
							fakeOs.WriteFileReturns(errors.New("badness"))
						})

						It("returns an error response", func() {
							Expect(unmountResponse.Err).To(Equal("failed to persist state when unmounting: badness"))
						})
					})

					Context("when the volume is mounted twice", func() {
						BeforeEach(func() {
							setupMount(env, volumeDriver, volumeName, fakeFilepath)
							// JustBefore each does an unmount
						})

						It("returns no error when unmounting", func() {
							Expect(unmountResponse.Err).To(Equal(""))
						})

						It("the volume should remain mounted (due to reference counting)", func() {
							getResponse := ExpectVolumeExists(env, volumeDriver, volumeName)
							Expect(strings.Replace(getResponse.Volume.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
						})

						Context("when unmounting again", func() {
							BeforeEach(func() {
								unmountResponse = volumeDriver.Unmount(env, dockerdriver.UnmountRequest{
									Name: volumeName,
								})
							})

							It("returns no error when unmounting", func() {
								Expect(unmountResponse.Err).To(Equal(""))
							})

							It("deleted the volume", func() {
								getResponse := volumeDriver.Get(env, dockerdriver.GetRequest{
									Name: volumeName,
								})

								Expect(getResponse.Err).To(Equal("volume not found"))
							})
						})
					})

					Context("when the mountpath is not found", func() {
						BeforeEach(func() {
							fakeMountChecker.ExistsReturns(false, nil)
						})

						It("returns an error", func() {
							Expect(strings.Replace(unmountResponse.Err, `\`, "/", -1)).To(Equal("Volume " + volumeName + " does not exist (path: /path/to/mount/" + volumeName + ")"))
						})

						It("decrements the mount count and removes the volume from state", func() {
							// When mount count is 1 and mountpath is not found, volume is removed after decrement
							getResponse := volumeDriver.Get(env, dockerdriver.GetRequest{
								Name: volumeName,
							})
							Expect(getResponse.Err).To(Equal("volume not found"))
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
							getResponse := volumeDriver.Get(env, dockerdriver.GetRequest{
								Name: volumeName,
							})

							Expect(getResponse.Err).To(Equal("volume not found"))
						})
					})

					Context("when the volume ref count is 1 but the mount does not exist", func() {
						BeforeEach(func() {
							fakeMountChecker.ExistsReturns(false, nil)
						})

						It("deletes the mount directory", func() {
							Expect(unmountResponse.Err).ToNot(BeEmpty())
							Expect(fakeOs.RemoveCallCount()).To(Equal(1))
							expectedPathToRemove := fakeOs.RemoveArgsForCall(0)

							Expect(expectedPathToRemove).To(Equal("/path/to/mount/" + volumeName))
						})

						Context("when unable to remove the mount directory", func() {
							BeforeEach(func() {
								fakeOs.RemoveReturns(errors.New("Unable to remove"))
							})

							It("returns an error", func() {
								Expect(unmountResponse.Err).To(ContainSubstring("Volume test-volume-id does not exist (path: /path/to/mount/test-volume-id) and unable to remove mount directory"))
							})
						})

						Context("when os.Remove returns os.ErrNotExist", func() {
							BeforeEach(func() {
								fakeOs.RemoveReturns(os.ErrNotExist)
							})

							It("still decrements the mount count and removes the volume from state", func() {
								// Unmount returns an error but still decrements mount count
								Expect(unmountResponse.Err).To(ContainSubstring("Volume " + volumeName + " does not exist"))
								Expect(fakeOs.RemoveCallCount()).To(Equal(1))

								// Verify the volume is removed from state (mount count reached 0)
								getResponse := volumeDriver.Get(env, dockerdriver.GetRequest{
									Name: volumeName,
								})
								Expect(getResponse.Err).To(Equal("volume not found"))
							})

							It("writes the driver state to disk", func() {
								// 3 - unmount (when os.Remove returns os.ErrNotExist)
								Expect(fakeOs.WriteFileCallCount()).To(Equal(3))
							})
						})
					})

					Context("when unmount succeeds but os.Remove returns os.ErrNotExist", func() {
						BeforeEach(func() {
							fakeMounter.UnmountReturns(nil)
							fakeOs.RemoveReturns(os.ErrNotExist)
						})

						It("still decrements the mount count and removes the volume from state", func() {
							// Unmount returns an error but still decrements mount count
							Expect(unmountResponse.Err).To(ContainSubstring("Volume " + volumeName + " does not exist"))
							Expect(fakeMounter.UnmountCallCount()).To(Equal(1))
							Expect(fakeOs.RemoveCallCount()).To(Equal(1))

							// Verify the volume is removed from state (mount count reached 0)
							getResponse := volumeDriver.Get(env, dockerdriver.GetRequest{
								Name: volumeName,
							})
							Expect(getResponse.Err).To(Equal("volume not found"))
						})
					})
				})

				Context("when the volume has not been mounted", func() {
					It("returns an error", func() {
						unmountResponse := volumeDriver.Unmount(env, dockerdriver.UnmountRequest{
							Name: volumeName,
						})

						Expect(unmountResponse.Err).To(Equal("volume not previously mounted"))
					})
				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					unmountResponse := volumeDriver.Unmount(env, dockerdriver.UnmountRequest{
						Name: volumeName,
					})

					Expect(unmountResponse.Err).To(Equal(fmt.Sprintf("Volume '%s' not found", volumeName)))
				})
			})
		})

		Describe("Create", func() {
			Context("when create is called with a volume ID", func() {

				var createResponse dockerdriver.ErrorResponse

				JustBeforeEach(func() {
					opts := map[string]interface{}{"source": ip}
					createResponse = volumeDriver.Create(env, dockerdriver.CreateRequest{
						Name: volumeName,
						Opts: opts,
					})
				})

				It("should write state, but omit Opts for security", func() {
					Expect(fakeOs.WriteFileCallCount()).To(Equal(1))

					_, data, _ := fakeOs.WriteFileArgsForCall(0)
					Expect(data).To(ContainSubstring("\"Name\":\"" + volumeName + "\""))
					Expect(data).NotTo(ContainSubstring("\"Opts\""))
				})

				Context("when the file system cant be written to", func() {
					BeforeEach(func() {
						fakeOs.WriteFileReturns(errors.New("badness"))
					})

					It("returns an error in the response", func() {
						Expect(createResponse.Err).To(Equal("persist state failed when creating: badness"))
					})
				})
			})

			Context("when a second create is called with the same volume ID", func() {
				BeforeEach(func() {
					setupVolume(env, volumeDriver, "volume", ip)
				})

				Context("with the same opts", func() {
					It("does nothing", func() {
						setupVolume(env, volumeDriver, "volume", ip)
					})
				})
			})
		})

		Describe("Get", func() {
			Context("when the volume has been created", func() {
				It("returns the volume name", func() {
					volumeName := "test-volume"
					setupVolume(env, volumeDriver, volumeName, ip)
					ExpectVolumeExists(env, volumeDriver, volumeName)
				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					volumeName := "test-volume"
					ExpectVolumeDoesNotExist(env, volumeDriver, volumeName)
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
					setupVolume(env, volumeDriver, volumeName, ip)
					setupMount(env, volumeDriver, volumeName, fakeFilepath)
				})

				It("returns the mount point on a /VolumeDriver.Path", func() {
					pathResponse := volumeDriver.Path(env, dockerdriver.PathRequest{
						Name: volumeName,
					})
					Expect(pathResponse.Err).To(Equal(""))
					Expect(strings.Replace(pathResponse.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
				})
			})

			Context("when a volume is not created", func() {
				It("returns an error on /VolumeDriver.Path", func() {
					pathResponse := volumeDriver.Path(env, dockerdriver.PathRequest{
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
					setupVolume(env, volumeDriver, volumeName, ip)
				})

				It("returns an error on /VolumeDriver.Path", func() {
					pathResponse := volumeDriver.Path(env, dockerdriver.PathRequest{
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
					setupVolume(env, volumeDriver, volumeName, ip)
				})

				It("returns the list of volumes", func() {
					listResponse := volumeDriver.List(env)

					Expect(listResponse.Err).To(Equal(""))
					Expect(listResponse.Volumes[0].Name).To(Equal(volumeName))

				})
			})

			Context("when the volume has not been created", func() {
				It("returns an error", func() {
					volumeName := "test-volume"
					ExpectVolumeDoesNotExist(env, volumeDriver, volumeName)
				})
			})
		})

		Describe("Remove", func() {

			var removeResponse dockerdriver.ErrorResponse

			JustBeforeEach(func() {
				removeResponse = volumeDriver.Remove(env, dockerdriver.RemoveRequest{
					Name: volumeName,
				})
			})

			It("fails if no volume name provided", func() {
				removeResponse := volumeDriver.Remove(env, dockerdriver.RemoveRequest{
					Name: "",
				})
				Expect(removeResponse.Err).To(Equal("Missing mandatory 'volume_name'"))
			})

			It("returns no error if the volume is not found", func() {
				Expect(removeResponse.Err).To(BeEmpty())
			})

			Context("when the volume has been created", func() {
				BeforeEach(func() {
					setupVolume(env, volumeDriver, volumeName, ip)
				})

				It("Remove succeeds", func() {
					Expect(removeResponse.Err).To(Equal(""))
					ExpectVolumeDoesNotExist(env, volumeDriver, volumeName)
				})

				It("doesn't unmount since there are not mounts", func() {
					Expect(fakeMounter.UnmountCallCount()).To(Equal(0))
				})

				It("should write state to disk", func() {
					// 1 create
					// 2 remove
					Expect(fakeOs.WriteFileCallCount()).To(Equal(2))
				})

				Context("when writing state to disk fails", func() {
					BeforeEach(func() {
						fakeOs.WriteFileReturns(errors.New("badness"))
					})

					It("should return an error response", func() {
						Expect(removeResponse.Err).NotTo(BeEmpty())
					})
				})

				Context("when volume has been mounted", func() {
					BeforeEach(func() {
						setupMount(env, volumeDriver, volumeName, fakeFilepath)
						fakeMounter.UnmountReturns(nil)
					})

					It("/VolumePlugin.Remove unmounts volume", func() {
						Expect(removeResponse.Err).To(Equal(""))
						Expect(fakeMounter.UnmountCallCount()).To(Equal(1))

						ExpectVolumeDoesNotExist(env, volumeDriver, volumeName)
					})
				})
			})

			Context("when the volume has not been created", func() {
				It("doesn't return an error", func() {
					removeResponse := volumeDriver.Remove(env, dockerdriver.RemoveRequest{
						Name: volumeName,
					})
					Expect(removeResponse.Err).To(BeEmpty())
				})
			})
		})

		Describe("Restoring Internal State", func() {
			JustBeforeEach(func() {
				volumeDriver = volumedriver.NewVolumeDriver(logger, fakeOs, fakeFilepath, fakeTime, fakeMountChecker, mountDir, fakeMounter, oshelper.NewOsHelper())
			})

			Context("no state is persisted", func() {
				BeforeEach(func() {
					fakeOs.ReadFileReturns(nil, errors.New("file not found"))
				})

				It("returns an empty list when fetching the list of volumes", func() {
					Expect(volumeDriver.List(env)).To(Equal(dockerdriver.ListResponse{
						Volumes: []dockerdriver.VolumeInfo{},
					}))
				})
			})

			Context("when state is persisted", func() {
				BeforeEach(func() {
					data, err := json.Marshal(map[string]volumedriver.NfsVolumeInfo{
						"some-volume-name": {
							Opts: map[string]interface{}{"source": "123.456.789"},
							VolumeInfo: dockerdriver.VolumeInfo{
								Name:       "some-volume-name",
								Mountpoint: "/some/mount/point",
								MountCount: 1,
							},
						},
					})

					Expect(err).ToNot(HaveOccurred())
					fakeOs.ReadFileReturns(data, nil)
				})

				It("returns the persisted volumes when listing", func() {
					Expect(volumeDriver.List(env)).To(Equal(dockerdriver.ListResponse{
						Volumes: []dockerdriver.VolumeInfo{
							{Name: "some-volume-name", Mountpoint: "/some/mount/point", MountCount: 1},
						},
					}))
				})

				Context("when the mounts are not present", func() {
					It("only returns the volumes that are present on disk", func() {
						removeResult := volumeDriver.Remove(env, dockerdriver.RemoveRequest{Name: "some-volume-name"})
						Expect(removeResult.Err).To(BeEmpty())

						Expect(volumeDriver.List(env)).To(Equal(dockerdriver.ListResponse{
							Volumes: []dockerdriver.VolumeInfo{},
						}))
					})
				})

				Context("when the state is corrupted", func() {
					BeforeEach(func() {
						fakeOs.ReadFileReturns([]byte("I have eleven toes."), nil)
					})
					It("will return no volumes", func() {
						Expect(volumeDriver.List(env)).To(Equal(dockerdriver.ListResponse{
							Volumes: []dockerdriver.VolumeInfo{},
						}))
					})
				})
			})
		})
	})
})

func ExpectVolumeDoesNotExist(env dockerdriver.Env, efsDriver dockerdriver.Driver, volumeName string) {
	getResponse := efsDriver.Get(env, dockerdriver.GetRequest{
		Name: volumeName,
	})

	Expect(getResponse.Err).To(Equal("volume not found"))
	Expect(getResponse.Volume.Name).To(Equal(""))
}

func ExpectVolumeExists(env dockerdriver.Env, efsDriver dockerdriver.Driver, volumeName string) dockerdriver.GetResponse {
	getResponse := efsDriver.Get(env, dockerdriver.GetRequest{
		Name: volumeName,
	})

	Expect(getResponse.Err).To(Equal(""))
	Expect(getResponse.Volume.Name).To(Equal(volumeName))
	return getResponse
}

func setupVolume(env dockerdriver.Env, volumeDriver dockerdriver.Driver, volumeName string, source string) {
	opts := map[string]interface{}{"source": source}
	createResponse := volumeDriver.Create(env, dockerdriver.CreateRequest{
		Name: volumeName,
		Opts: opts,
	})
	Expect(createResponse.Err).To(Equal(""))
}

func setupMount(env dockerdriver.Env, volumeDriver dockerdriver.Driver, volumeName string, fakeFilepath *filepath_fake.FakeFilepath) {
	fakeFilepath.AbsReturns("/path/to/mount/", nil)
	mountResponse := volumeDriver.Mount(env, dockerdriver.MountRequest{Name: volumeName})
	Expect(mountResponse.Err).To(Equal(""))
	Expect(strings.Replace(mountResponse.Mountpoint, `\`, "/", -1)).To(Equal("/path/to/mount/" + volumeName))
}
