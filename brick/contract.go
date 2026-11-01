package brick

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The paths the contract names. They are constants and not configuration, because a brick
// whose paths came from somewhere would be a brick whose contract depends on how it was run.
const (
	InDir      = "/agk/in"
	OutPorts   = "/agk/out/ports"
	OutFiles   = "/agk/out/files"
	ParamsFile = "/agk/params.json"
	RunFile    = "/agk/run.json"
	SecretsDir = "/agk/secrets"
)

// Root is prefixed to every path of the contract, and it is empty in a container.
//
// It exists for the one place the contract cannot reach: a test on the machine a brick is
// built on, which may not write to /agk and should not need a container to be told where bytes
// go. Nothing in a container sets it.
var Root string

// at is a path of the contract under Root.
func at(path string) string {
	if Root == "" {
		return path
	}
	return filepath.Join(Root, path)
}

// task is what the environment says about this container: the metadata every envelope a brick
// writes has to carry, and which the runner refuses an envelope for disagreeing with.
type task struct {
	runID   string
	step    string
	attempt int
	ports   []string
}

// taskOf reads it, refusing a missing variable by name. An envelope written without them would
// be refused at collection all the same, and the refusal is more use from the code that could
// still have been told.
func taskOf() (task, error) {
	var t task
	var missing []string
	if t.runID = os.Getenv("AGK_RUN_ID"); t.runID == "" {
		missing = append(missing, "AGK_RUN_ID")
	}
	if t.step = os.Getenv("AGK_STEP"); t.step == "" {
		missing = append(missing, "AGK_STEP")
	}
	attempt := os.Getenv("AGK_ATTEMPT")
	if attempt == "" {
		missing = append(missing, "AGK_ATTEMPT")
	}
	if len(missing) > 0 {
		return task{}, fmt.Errorf("%s not set: the runner sets them in every container, and a brick run by hand is given them the same way", strings.Join(missing, ", "))
	}
	n, err := strconv.Atoi(attempt)
	if err != nil || n < 1 {
		return task{}, fmt.Errorf("AGK_ATTEMPT is %q, and an attempt is a whole number from 1", attempt)
	}
	t.attempt = n
	t.ports = split(os.Getenv("AGK_OUT_PORTS"))
	return t, nil
}

// declared says whether the step declares an output port. With AGK_OUT_PORTS unset, which only
// a brick run by hand sees, every port is.
func (t task) declared(port string) bool {
	if t.ports == nil {
		return true
	}
	for _, p := range t.ports {
		if p == port {
			return true
		}
	}
	return false
}

// split reads a comma-separated list, which may be empty.
func split(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// errName is a name that is not one segment of a path: a port, an artifact or a secret is a
// file directly under its directory, and a name carrying a separator would reach outside it.
var errName = errors.New("not a name: one segment of a path, no separator, not . or ..")

func checkName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		return fmt.Errorf("%q is %w", name, errName)
	}
	return nil
}
