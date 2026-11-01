package brick

import (
	"fmt"
	"io"
	"os"
)

// The bands of the exit code table a brick may leave by. The runner reads the band and nothing
// else; 121 to 124 are its own and 125 and above the runtime's.
const (
	// Failed is an application failure, retried only where the step's retry.on says so.
	Failed = 1
	// Transient is a far side that may answer next time, retried by the step's policy.
	Transient = 100
	// Invalid is input that will be as unusable on every attempt, never retried.
	Invalid = 120
)

// ExitCode is the code Exit leaves with for the code it is given: the code where a brick may
// leave by it, 1 to 120, and Failed for anything else, since a brick leaving with the runner's
// or the runtime's code would be read as a broken contract or a broken machine.
func ExitCode(code int) int {
	if code < Failed || code > Invalid {
		return Failed
	}
	return code
}

// exit and stderr are os.Exit and os.Stderr, replaced by a test.
var (
	exit             = os.Exit
	stderr io.Writer = os.Stderr
)

// Exit writes one line on standard error, the task's log, and leaves with code, mapped by
// ExitCode. A brick's diagnostics go to standard error and never standard output, which no log
// keeps. A code outside the bands is named in the log before it is replaced.
func Exit(code int, format string, args ...any) {
	fmt.Fprintf(stderr, format+"\n", args...)
	if c := ExitCode(code); c != code {
		fmt.Fprintf(stderr, "exit code %d is not a brick's to leave with: 1 to 99 is a failure, 100 to 119 transient, 120 invalid input; leaving with %d\n", code, c)
	}
	exit(ExitCode(code))
}

// Fail leaves with Failed: the step failed, and is retried only where it says so.
func Fail(format string, args ...any) { Exit(Failed, format, args...) }

// Retry leaves with Transient: the far side was briefly unavailable, and the step's policy
// tries again. Never for a payload that will be as wrong next time.
func Retry(format string, args ...any) { Exit(Transient, format, args...) }

// Refuse leaves with Invalid: what the step or the batch says is unusable and will be on every
// attempt, so the runner never retries it.
func Refuse(format string, args ...any) { Exit(Invalid, format, args...) }
