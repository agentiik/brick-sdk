package brick

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The scenarios every helper is held to, in conformance/scenarios.json, read here exactly as
// the Python and TypeScript tests read them.
type scenarios struct {
	Limits struct {
		InlineMax   int `json:"inline_max_bytes"`
		EnvelopeMax int `json:"envelope_max_bytes"`
		MaxItems    int `json:"max_items"`
	} `json:"limits"`
	Exit    [][2]int            `json:"exit"`
	Segment [][2]string         `json:"segment"`
	Encode  [][]json.RawMessage `json:"encode"`
	Spill   []struct {
		Name string `json:"name"`
		Task struct {
			RunID   string `json:"run_id"`
			Step    string `json:"step"`
			Attempt int    `json:"attempt"`
		} `json:"task"`
		Limits struct {
			InlineMax int `json:"inline_max_bytes"`
		} `json:"limits"`
		Port  string          `json:"port"`
		Items json.RawMessage `json:"items"`
		Want  json.RawMessage `json:"want"`
	} `json:"spill"`
	Refuse []struct {
		Name   string `json:"name"`
		Limits struct {
			InlineMax   int `json:"inline_max_bytes"`
			EnvelopeMax int `json:"envelope_max_bytes"`
			MaxItems    int `json:"max_items"`
		} `json:"limits"`
		Items int    `json:"items"`
		Says  string `json:"says"`
	} `json:"refuse"`
}

func load(t *testing.T) scenarios {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "conformance", "scenarios.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s scenarios
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatal(err)
	}
	return s
}

// in sets up a task under a temporary root, as the runner would set up a container.
func in(t *testing.T, runID, step string, attempt int, ports string) {
	t.Helper()
	Root = t.TempDir()
	t.Cleanup(func() { Root = ""; Limits = DefaultLimits })
	t.Setenv("AGK_RUN_ID", runID)
	t.Setenv("AGK_STEP", step)
	t.Setenv("AGK_ATTEMPT", itoa(attempt))
	t.Setenv("AGK_OUT_PORTS", ports)
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestTheDefaultsAreTheDocumentedOnes(t *testing.T) {
	s := load(t)
	if DefaultLimits != (SizeLimits{s.Limits.InlineMax, s.Limits.EnvelopeMax, s.Limits.MaxItems}) {
		t.Errorf("the defaults are %+v", DefaultLimits)
	}
}

func TestExitCodesStayInTheBricksBands(t *testing.T) {
	for _, c := range load(t).Exit {
		if got := ExitCode(c[0]); got != c[1] {
			t.Errorf("ExitCode(%d) is %d, and the table says %d", c[0], got, c[1])
		}
	}
}

func TestSegmentsAreFiledAsTheEngineFilesThem(t *testing.T) {
	for _, c := range load(t).Segment {
		if got := Segment(c[0]); got != c[1] {
			t.Errorf("Segment(%q) is %q, want %q", c[0], got, c[1])
		}
	}
}

func TestValuesAreEncodedAsTheEngineEncodesThem(t *testing.T) {
	for _, c := range load(t).Encode {
		var v any
		if err := json.Unmarshal(c[0], &v); err != nil {
			t.Fatal(err)
		}
		var want string
		if err := json.Unmarshal(c[1], &want); err != nil {
			t.Fatal(err)
		}
		got, err := encode(v)
		if err != nil || string(got) != want {
			t.Errorf("%s encodes as %s, want %s (%v)", c[0], got, want, err)
		}
	}
}

func TestHeavyValuesAreSpilled(t *testing.T) {
	for _, c := range load(t).Spill {
		t.Run(c.Name, func(t *testing.T) {
			in(t, c.Task.RunID, c.Task.Step, c.Task.Attempt, c.Port)
			Limits.InlineMax = c.Limits.InlineMax
			var items []Item
			if err := json.Unmarshal(c.Items, &items); err != nil {
				t.Fatal(err)
			}
			if err := Emit(c.Port, items); err != nil {
				t.Fatalf("Emit: %s", err)
			}
			b, err := os.ReadFile(filepath.Join(Root, OutPorts, c.Port+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var got struct {
				Meta  Meta `json:"meta"`
				Items any  `json:"items"`
			}
			if err := json.Unmarshal(b, &got); err != nil {
				t.Fatal(err)
			}
			var want any
			if err := json.Unmarshal(c.Want, &want); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Items, want) {
				t.Errorf("published %s\nwant %s", mustJSON(got.Items), c.Want)
			}
			if got.Meta != (Meta{RunID: c.Task.RunID, Step: c.Task.Step, Port: c.Port, Attempt: c.Task.Attempt, Count: 1, ProducedAt: "1970-01-01T00:00:00Z"}) {
				t.Errorf("meta is %+v", got.Meta)
			}
		})
	}
}

func TestAnEnvelopeTheRunnerWouldRefuseIsRefusedFirst(t *testing.T) {
	for _, c := range load(t).Refuse {
		t.Run(c.Name, func(t *testing.T) {
			in(t, "r", "s", 1, "out")
			Limits = SizeLimits{c.Limits.InlineMax, c.Limits.EnvelopeMax, c.Limits.MaxItems}
			var items []Item
			for i := range c.Items {
				items = append(items, Item{ID: strings.Repeat("x", i+1), Data: map[string]any{"v": "0123456789"}})
			}
			err := Emit("out", items)
			if !errors.Is(err, ErrTooLarge) || !strings.Contains(err.Error(), c.Says) {
				t.Fatalf("answered %v, want a refusal naming %s", err, c.Says)
			}
			if _, err := os.Stat(filepath.Join(Root, OutPorts, "out.json")); err == nil {
				t.Error("the refused envelope was written all the same")
			}
		})
	}
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
