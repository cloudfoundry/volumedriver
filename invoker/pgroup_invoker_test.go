package invoker_test

import (
	"code.cloudfoundry.org/dockerdriver"
	"code.cloudfoundry.org/dockerdriver/driverhttp"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/volumedriver/invoker"
	"context"
	"fmt"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"math/rand"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

var _ = Describe("ProcessGroupInvoker", func() {
	var (
		pgroupInvoker      invoker.Invoker
		dockerDriverEnv    dockerdriver.Env
		execToInvoke       string
		argsToExecToInvoke []string
		result             invoker.InvokeResult
		err                error
		testlogger         *lagertest.TestLogger
	)

	BeforeEach(func() {
		testlogger = lagertest.NewTestLogger("test-pgInvoker")
		dockerDriverEnv = driverhttp.NewHttpDriverEnv(testlogger, context.TODO())

		pgroupInvoker = invoker.NewProcessGroupInvoker()
	})

	JustBeforeEach(func() {
		result, err = pgroupInvoker.Invoke(
			dockerDriverEnv,
			execToInvoke,
			argsToExecToInvoke)

	})

	Context("wait", func() {
		Context("command returns success", func() {
			var expectedOutput string

			BeforeEach(func() {
				source := rand.NewSource(GinkgoRandomSeed())

				execToInvoke = "echo"
				expectedOutput = randomNumberAsString(source)
				argsToExecToInvoke = []string{expectedOutput}
			})

			It("calls a real command", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Wait()).To(Succeed())
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
			})
		})

		Context("command has stderr output", func() {
			var expectedOutput string

			BeforeEach(func() {
				source := rand.NewSource(GinkgoRandomSeed())

				execToInvoke = "bash"
				expectedOutput = randomNumberAsString(source)
				argsToExecToInvoke = []string{"-c", fmt.Sprintf("echo %s >&2", expectedOutput)}
			})

			It("outputs the stderr output", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Wait()).To(Succeed())
				Expect(result.StdError()).To(Equal(expectedOutput + "\n"))
				Expect(result.StdError()).To(Equal(expectedOutput + "\n"))
			})
		})

		Context("command returns an error code", func() {
			BeforeEach(func() {
				execToInvoke = "bash"
				argsToExecToInvoke = []string{"-c", "exit 1"}
			})

			It("returns an error", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.Wait()).To(BeAssignableToTypeOf(&exec.ExitError{}))
			})
		})

		Context("invoked command spawns child process", func() {
			BeforeEach(func() {
				execToInvoke = "bash"
				argsToExecToInvoke = []string{"-c", `echo $$; sleep 5555 &
sleep 7777`}
			})

			AfterEach(func() {
				cmd := exec.Command("pkill", "sleep")
				err := cmd.Start()
				Expect(err).NotTo(HaveOccurred())
			})

			It("runs all the child processes in the same process group", func() {
				var pid int
				By("determining the pid of the invoked process", func() {
					Expect(err).NotTo(HaveOccurred())
					Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
					pid, err = strconv.Atoi(strings.ReplaceAll(result.StdOutput(), "\n", ""))
					Expect(err).NotTo(HaveOccurred())
				})

				By("killing the process group of the invoked process", func() {
					Expect(syscall.Kill(-pid, syscall.SIGKILL)).To(Succeed())
					Expect(result.Wait().(*exec.ExitError).Sys().(syscall.WaitStatus).Signal().String()).To(Equal("killed"))
				})

				Eventually(func() error {
					cmd := exec.Command("ps", "-p", fmt.Sprintf("%v", pid))
					_, err := cmd.Output()
					return err
				}, 30*time.Second).Should(BeAssignableToTypeOf(&exec.ExitError{}))

				Eventually(func() error {
					cmd := exec.Command("pgrep", "-l", "-f", "sleep 5555")
					_, err := cmd.Output()
					return err
				}, 3*time.Second).Should(BeAssignableToTypeOf(&exec.ExitError{}))
			})
		})

		Context("unable to start a command", func() {
			BeforeEach(func() {
				execToInvoke = "/non-existent-command-that-def-doesnt-exist"
				argsToExecToInvoke = []string{}
			})

			It("returns an error", func() {
				Expect(err).To(HaveOccurred())
			})
		})

		Context("Running a command that take a long time", func() {
			Context("cancelling the docker context", func() {
				var cancelfunc context.CancelFunc

				BeforeEach(func() {
					var deadline context.Context
					deadline, cancelfunc = context.WithDeadline(context.Background(), time.Now().Add(1*time.Hour))
					dockerDriverEnv = driverhttp.NewHttpDriverEnv(testlogger, deadline)
					execToInvoke = "bash"
					argsToExecToInvoke = []string{"-c", "echo $$"}
				})

				It("should not kill the process group or error due to not being able to kill the absent process", func() {
					Expect(result.Wait()).To(Succeed())
					cancelfunc()

					Eventually(testlogger.Buffer(), 5*time.Second).Should(gbytes.Say(`command-sigkill-error.*"desc":"no such process"`))
					Eventually(testlogger.Buffer(), 5*time.Second).Should(gbytes.Say(`command-sigkill-wait-error.*"desc":"exec: Wait was already called"`))
				})
			})

			Context("timing out on the docker context", func() {
				BeforeEach(func() {
					deadline, _ := context.WithDeadline(context.Background(), time.Now().Add(1*time.Second))
					dockerDriverEnv = driverhttp.NewHttpDriverEnv(lagertest.NewTestLogger("test-pgInvoker"), deadline)
					execToInvoke = "bash"
					argsToExecToInvoke = []string{"-c", "echo $$; sleep 10"}
				})

				It("should kill the process group", func() {
					var pid int
					By("determining the pid of the invoked process", func() {
						Expect(err).NotTo(HaveOccurred())
						Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
						pid, err = strconv.Atoi(strings.ReplaceAll(result.StdOutput(), "\n", ""))
						Expect(err).NotTo(HaveOccurred())
					})

					Eventually(func() error {
						cmd := exec.Command("ps", "-p", fmt.Sprintf("%v", pid))
						_, err := cmd.Output()
						return err
					}, 3*time.Second).Should(BeAssignableToTypeOf(&exec.ExitError{}))
				})
			})
		})
	})

	Context("waitFor", func() {
		Context("command returns success", func() {
			var expectedOutput string

			BeforeEach(func() {
				source := rand.NewSource(GinkgoRandomSeed())

				execToInvoke = "echo"
				expectedOutput = randomNumberAsString(source)
				argsToExecToInvoke = []string{expectedOutput}
			})

			It("calls a real command", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.WaitFor(expectedOutput, 1*time.Second)).To(Succeed())
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
			})

			It("should error if output doesn't contain string we are waiting for", func(done chan<- interface{}) {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.WaitFor("string-that-will-never-print", 60*time.Second)).NotTo(Succeed())

				close(done)
			}, 5)
		})

		Context("invoked command spawns child process", func() {
			BeforeEach(func() {
				execToInvoke = "bash"
				argsToExecToInvoke = []string{"-c", `echo $$; sleep 5555 &
sleep 7777`}
			})

			AfterEach(func() {
				cmd := exec.Command("pkill", "sleep")
				err := cmd.Start()
				Expect(err).NotTo(HaveOccurred())
			})

			It("runs all the child processes in the same process group", func() {
				var pid int
				By("determining the pid of the invoked process", func() {
					Expect(err).NotTo(HaveOccurred())
					Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
					pid, err = strconv.Atoi(strings.ReplaceAll(result.StdOutput(), "\n", ""))
					Expect(err).NotTo(HaveOccurred())
				})

				Expect(result.WaitFor("nonexistent", 1*time.Second)).NotTo(Succeed())

				Eventually(func() error {
					cmd := exec.Command("ps", "-p", fmt.Sprintf("%v", pid))
					_, err := cmd.Output()
					return err
				}, 30*time.Second).Should(BeAssignableToTypeOf(&exec.ExitError{}))

				Eventually(func() error {
					cmd := exec.Command("pgrep", "-l", "-f", "sleep 5555")
					_, err := cmd.Output()
					return err
				}, 3*time.Second).Should(BeAssignableToTypeOf(&exec.ExitError{}))
			})
		})

		Context("command takes a long time to run", func() {
			var expectedOutput string

			BeforeEach(func() {
				source := rand.NewSource(GinkgoRandomSeed())

				execToInvoke = "bash"
				expectedOutput = randomNumberAsString(source)
				argsToExecToInvoke = []string{"-c", fmt.Sprintf("echo $$; sleep 30 && echo %s", expectedOutput)}
			})

			It("should timeout if timeout elapses before output contains desired string", func(done chan<- interface{}) {
				Expect(err).NotTo(HaveOccurred())

				err := result.WaitFor(expectedOutput, 1*time.Second)
				Expect(err).To(HaveOccurred())
				Expect(err).To(MatchError("command timed out"))

				var pid int
				Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
				pid, err = strconv.Atoi(strings.ReplaceAll(result.StdOutput(), "\n", ""))
				Expect(err).NotTo(HaveOccurred())

				Eventually(func() error {
					cmd := exec.Command("ps", "-p", fmt.Sprintf("%v", pid))
					_, err := cmd.Output()
					return err
				}, 4*time.Second).Should(BeAssignableToTypeOf(&exec.ExitError{}))

				close(done)
			}, 10)
		})

		Context("command returns an error code", func() {
			BeforeEach(func() {
				execToInvoke = "bash"
				argsToExecToInvoke = []string{"-c", "exit 1"}
			})

			It("returns an error", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.WaitFor("sometext", 10*time.Second)).To(BeAssignableToTypeOf(&exec.ExitError{}))
			})
		})

		Context("command contains stdout but exits with a non-zero code", func() {
			BeforeEach(func() {
				execToInvoke = "bash"
				argsToExecToInvoke = []string{"-c", "exit 1"}
			})

			It("returns an error", func() {
				Expect(err).NotTo(HaveOccurred())
				Expect(result.WaitFor("", 10*time.Second)).To(Succeed())
			})
		})

	})
})

func randomNumberAsString(source rand.Source) string {
	randomGenerator := rand.New(source)
	return strconv.Itoa(randomGenerator.Int())
}
