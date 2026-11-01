package brick

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
	"time"
)

// Params is /agk/params.json: what the step chose, as the manifest declared it. A step that
// supplied nothing leaves the file absent, which is an empty map and not an error: whether a
// parameter is required was settled when the workflow was validated.
func Params() (map[string]json.RawMessage, error) {
	b, err := os.ReadFile(at(ParamsFile))
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]json.RawMessage{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("the parameters could not be read: %w", err)
	}
	var p map[string]json.RawMessage
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, fmt.Errorf("the parameters at %s are not a JSON object: %w", ParamsFile, err)
	}
	if p == nil {
		p = map[string]json.RawMessage{}
	}
	return p, nil
}

// Param decodes one parameter into v and answers whether the step supplied it. The two are kept
// apart because a default applied over an explicit zero is the bug a reader checks the manifest
// to rule out, and a brick that cannot tell "absent" from "zero" cannot implement one.
func Param(name string, v any) (bool, error) {
	p, err := Params()
	if err != nil {
		return false, err
	}
	raw, ok := p[name]
	if !ok {
		return false, nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(v); err != nil {
		return true, fmt.Errorf("the parameter %s is not the shape this brick reads: %w", name, err)
	}
	return true, nil
}

// Run is the run context: /agk/run.json, and the AGK_* variables the runner sets.
type Run struct {
	// ID is the run's ULID, AGK_RUN_ID.
	ID string `json:"id"`
	// Workflow is the workflow's name and Version the version it runs at, AGK_WORKFLOW split at
	// its @.
	Workflow string `json:"workflow"`
	Version  string `json:"version,omitempty"`
	// Namespace owns the workflow.
	Namespace string `json:"namespace"`
	// Commit is the commit the run started from, absent for agk run --local.
	Commit string `json:"commit,omitempty"`
	// TriggerKind, TriggeredBy and StartedAt are what started the run, whom it is attributed to
	// and when it was let in. A runner older than v0.7.3 is not told them, and they are empty.
	TriggerKind string `json:"trigger_kind,omitempty"`
	TriggeredBy string `json:"triggered_by,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	// Step, Attempt and Shard name this container's task. Shard is nil without a fan-out.
	Step    string `json:"step"`
	Attempt int    `json:"attempt"`
	Shard   *Shard `json:"shard,omitempty"`
	// Deadline is when the container will be stopped, AGK_DEADLINE.
	Deadline time.Time `json:"deadline"`
	// Repo is where the workflow repository is mounted, AGK_REPO.
	Repo string `json:"repo"`
	// OutPorts are the output ports the step declares, AGK_OUT_PORTS.
	OutPorts []string `json:"out_ports"`
	// TraceParent is the W3C Trace Context of the task's span, TRACEPARENT.
	TraceParent string `json:"traceparent,omitempty"`
}

// Shard is one shard of a fan-out: AGK_SHARD, 3/8 read as index 3 of count 8.
type Shard struct {
	Index int `json:"index"`
	Count int `json:"count"`
}

// Context reads the run context. /agk/run.json says what the run is and the environment says
// what this container is; a field the runner did not give is left empty rather than invented,
// since an empty one is honest and an invented one reads as a fact.
func Context() (Run, error) {
	var r Run
	b, err := os.ReadFile(at(RunFile))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return Run{}, fmt.Errorf("the run context could not be read: %w", err)
	}
	if err == nil {
		if err := json.Unmarshal(b, &r); err != nil {
			return Run{}, fmt.Errorf("%s is not the run context: %w", RunFile, err)
		}
	}
	if v := os.Getenv("AGK_RUN_ID"); v != "" {
		r.ID = v
	}
	if v := os.Getenv("AGK_WORKFLOW"); v != "" {
		r.Workflow, r.Version, _ = strings.Cut(v, "@")
	}
	if v := os.Getenv("AGK_NAMESPACE"); v != "" {
		r.Namespace = v
	}
	if v := os.Getenv("AGK_COMMIT"); v != "" {
		r.Commit = v
	}
	r.Step = os.Getenv("AGK_STEP")
	if v := os.Getenv("AGK_ATTEMPT"); v != "" {
		if _, err := fmt.Sscanf(v, "%d", &r.Attempt); err != nil {
			return Run{}, fmt.Errorf("AGK_ATTEMPT is %q, and an attempt is a whole number", v)
		}
	}
	if v := os.Getenv("AGK_SHARD"); v != "" {
		var s Shard
		if n, err := fmt.Sscanf(v, "%d/%d", &s.Index, &s.Count); err != nil || n != 2 {
			return Run{}, fmt.Errorf("AGK_SHARD is %q, and a shard is written index/count", v)
		}
		r.Shard = &s
	}
	if v := os.Getenv("AGK_DEADLINE"); v != "" {
		d, err := time.Parse(time.RFC3339, v)
		if err != nil {
			return Run{}, fmt.Errorf("AGK_DEADLINE is %q, and a deadline is RFC 3339", v)
		}
		r.Deadline = d
	}
	r.Repo = os.Getenv("AGK_REPO")
	r.OutPorts = split(os.Getenv("AGK_OUT_PORTS"))
	r.TraceParent = os.Getenv("TRACEPARENT")
	return r, nil
}

// Secret reads the secret mounted at /agk/secrets/<name>, and answers false where none is: a
// secret the manifest declares optional may be absent, and a brick that degrades without it
// asks. It never reads the environment, where a secret would be inherited by every process the
// brick starts and printed by the first one that dumps its environment.
//
// A manifest that mounts a secret elsewhere under /agk/secrets names that file here.
func Secret(name string) ([]byte, bool, error) {
	if err := checkName(name); err != nil {
		return nil, false, fmt.Errorf("secret %w", err)
	}
	b, err := os.ReadFile(at(SecretsDir + "/" + name))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("the secret %s could not be read: %w", name, err)
	}
	return b, true, nil
}
