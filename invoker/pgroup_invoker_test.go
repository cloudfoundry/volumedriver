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
	"os"
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
		testlogger         *lagertest.TestLogger
	)

	BeforeEach(func() {
		testlogger = lagertest.NewTestLogger("test-pgInvoker")
		dockerDriverEnv = driverhttp.NewHttpDriverEnv(testlogger, context.TODO())
		pgroupInvoker = invoker.NewProcessGroupInvoker()
	})

	JustBeforeEach(func() {
		result = pgroupInvoker.Invoke(
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
				Expect(result.Wait()).To(Succeed())
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
			})
		})

		Context("env vars", func() {
			var envVar1, envVar2 string
			var envVars []string
			JustBeforeEach(func() {
				result = pgroupInvoker.Invoke(
					dockerDriverEnv,
					execToInvoke,
					argsToExecToInvoke,
					envVars...)

			})

			BeforeEach(func() {
				source := rand.NewSource(GinkgoRandomSeed())
				envVar1 = randomNumberAsString(source)
				envVar2 = randomNumberAsString(source)
				os.Setenv("FOO", envVar1)
			})

			AfterEach(func() {
				os.Unsetenv("FOO")
			})

			Context("none passed in", func() {
				BeforeEach(func() {
					execToInvoke = "bash"
					argsToExecToInvoke = []string{"-c", "echo $FOO"}
				})

				It("does not override existing env vars", func() {
					Expect(result.Wait()).To(Succeed())
					Expect(result.StdOutput()).To(Equal(envVar1 + "\n"))
					Expect(result.StdOutput()).To(Equal(envVar1 + "\n"))
				})
			})

			Context("passed env vars", func() {
				BeforeEach(func() {
					execToInvoke = "bash"
					envVars = []string{fmt.Sprintf("BAR=%s", envVar2)}
					argsToExecToInvoke = []string{"-c", "echo $FOO && echo $BAR"}
				})

				It("adds the env vars", func() {
					Expect(result.Wait()).To(Succeed())
					Expect(result.StdOutput()).To(Equal(envVar1 + "\n" + envVar2 + "\n"))
					Expect(result.StdOutput()).To(Equal(envVar1 + "\n" + envVar2 + "\n"))
				})
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
					Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))

					var err error
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

			It("Wait returns an error", func() {
				Expect(result.Wait()).To(HaveOccurred())
			})

			It("WaitFor returns an error", func() {
				Expect(result.WaitFor("", 1 * time.Hour)).To(HaveOccurred())
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

					Eventually(testlogger.Buffer(), 5*time.Second).Should(gbytes.Say(`not killing process due to already finished`))
				})

				It("should not skip the killing of the process if the cmd hasn't finished", func() {
					cancelfunc()

					Consistently(testlogger.Buffer(), 5*time.Second).ShouldNot(gbytes.Say(`not killing process due to already finished`))
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
						Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
						var err error
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
				Expect(result.WaitFor(expectedOutput, 1*time.Second)).To(Succeed())
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
				Expect(result.StdOutput()).To(Equal(expectedOutput + "\n"))
			})

			It("should error if output doesn't contain string we are waiting for", func(done chan<- interface{}) {
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
					Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
					var err error
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

			Context("context is cancelled", func() {
				var cancel context.CancelFunc
				BeforeEach(func() {
					var ctx context.Context
					ctx, cancel = context.WithCancel(context.Background())
					dockerDriverEnv = driverhttp.NewHttpDriverEnv(testlogger, ctx)
				})

				It("it should not kill the process", func() {
					Eventually(result.StdOutput, 3*time.Second).Should(MatchRegexp("\\d+"))
					Expect(result.WaitFor(result.StdOutput(), 10*time.Second)).To(Succeed())
					cancel()

					pid, err := strconv.Atoi(strings.ReplaceAll(result.StdOutput(), "\n", ""))
					Expect(err).NotTo(HaveOccurred())

					cmd := exec.Command("ps", "-p", fmt.Sprintf("%v", pid))
					_, err = cmd.Output()
					Expect(err).To(Not(HaveOccurred()))
				})
			})

			It("should timeout if timeout elapses before output contains desired string", func(done chan<- interface{}) {
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
				Expect(result.WaitFor("sometext", 10*time.Second)).To(BeAssignableToTypeOf(&exec.ExitError{}))
			})
		})

		Context("command contains stdout but exits with a non-zero code", func() {
			BeforeEach(func() {
				execToInvoke = "bash"
				argsToExecToInvoke = []string{"-c", "exit 1"}
			})

			It("returns an error", func() {
				Expect(result.WaitFor("", 10*time.Second)).To(Succeed())
			})
		})

	})
})

func randomNumberAsString(source rand.Source) string {
	randomGenerator := rand.New(source)
	return strconv.Itoa(randomGenerator.Int())
}
