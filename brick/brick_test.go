package brick

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	full := filepath.Join(Root, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAnEnvelopeIsReadFromItsPortAndNumbersKeepTheirDigits(t *testing.T) {
	in(t, "r", "s", 1, "out")
	write(t, "agk/in/orders/envelope.json", `{"meta":{"run_id":"r","step":"up","port":"ok","attempt":1,"count":1,"produced_at":"2026-01-31T09:00:00Z"},"items":[{"id":"a","data":{"cents":12345678901234567890},"files":[]}]}`)
	items, err := Items("orders")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Data["cents"] != json.Number("12345678901234567890") {
		t.Errorf("read %+v", items)
	}
}

func TestAPortNobodyFedIsAnEmptyBatch(t *testing.T) {
	in(t, "r", "s", 1, "out")
	Stdin = strings.NewReader("")
	t.Cleanup(func() { Stdin = os.Stdin })
	for _, port := range []string{"in", "reference"} {
		items, err := Items(port)
		if err != nil || items == nil || len(items) != 0 {
			t.Errorf("port %s read as %v, %v", port, items, err)
		}
	}
}

func TestThePortInIsReadFromStandardInputWhenNoFileIsMounted(t *testing.T) {
	in(t, "r", "s", 1, "out")
	Stdin = strings.NewReader(`{"meta":{"run_id":"r","step":"up","port":"out","attempt":1,"count":1,"produced_at":"2026-01-31T09:00:00Z"},"items":[{"id":"a","data":{},"files":[]}]}`)
	t.Cleanup(func() { Stdin = os.Stdin })
	items, err := Items("in")
	if err != nil || len(items) != 1 || items[0].ID != "a" {
		t.Errorf("read %+v, %v", items, err)
	}
}

func TestEmitWritesTheMetadataTheContainerWasGiven(t *testing.T) {
	in(t, "01M2AAZ9G62NQXFAFCXKRPJEH5", "charge", 3, "ok,rejected")
	if err := Emit("ok", []Item{{ID: "a"}}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(filepath.Join(Root, OutPorts, "ok.json"))
	want := `{"meta":{"run_id":"01M2AAZ9G62NQXFAFCXKRPJEH5","step":"charge","port":"ok","attempt":3,"count":1,"produced_at":"1970-01-01T00:00:00Z"},"items":[{"id":"a","data":{},"files":[]}]}` + "\n"
	if string(b) != want {
		t.Errorf("wrote %s\nwant  %s", b, want)
	}
}

func TestEmitRefusesWhatTheRunnerWouldRefuse(t *testing.T) {
	in(t, "r", "s", 1, "ok")
	if err := Emit("other", nil); err == nil || !strings.Contains(err.Error(), "not one this step declares") {
		t.Errorf("an undeclared port was answered %v", err)
	}
	if err := Emit("../ok", nil); err == nil {
		t.Error("a port reaching outside /agk/out/ports was accepted")
	}
	os.Unsetenv("AGK_RUN_ID")
	if err := Emit("ok", nil); err == nil || !strings.Contains(err.Error(), "AGK_RUN_ID") {
		t.Errorf("a missing AGK_RUN_ID was answered %v", err)
	}
}

func TestTwoPortsNeverSpillToOneName(t *testing.T) {
	in(t, "r", "s", 1, "ok,invalid")
	Limits.InlineMax = 4
	for _, port := range []string{"ok", "invalid"} {
		if err := Emit(port, []Item{{ID: "a", Data: map[string]any{"note": port + "-heavy"}}}); err != nil {
			t.Fatal(err)
		}
	}
	var ok, invalid Envelope
	for port, e := range map[string]*Envelope{"ok": &ok, "invalid": &invalid} {
		b, _ := os.ReadFile(filepath.Join(Root, OutPorts, port+".json"))
		json.Unmarshal(b, e)
	}
	if ok.Items[0].Files[0].Name != "a-note.json" || invalid.Items[0].Files[0].Name != "a-note-2.json" {
		t.Errorf("spilled to %s and %s", ok.Items[0].Files[0].Name, invalid.Items[0].Files[0].Name)
	}
}

func TestAttachCarriesItsOwnDigest(t *testing.T) {
	in(t, "r", "render", 1, "out")
	f, err := Attach("out", "invoice.pdf", "application/pdf", []byte("%PDF"))
	if err != nil {
		t.Fatal(err)
	}
	if f.URI != "agk://run/r/render/out/invoice.pdf" || f.Size != 4 || f.SHA256 != "315d429b7714cedb6ad04ac31240145257692630457f3c88253c5beceac76027" {
		t.Errorf("attached %+v", f)
	}
	if _, err := Attach("out", "../x", "text/plain", nil); err == nil {
		t.Error("a name reaching outside /agk/out/files was accepted")
	}
}

func TestAParameterAbsentIsNotAParameterZero(t *testing.T) {
	in(t, "r", "s", 1, "out")
	write(t, "agk/params.json", `{"retries":0}`)
	var n int
	if ok, err := Param("retries", &n); !ok || err != nil || n != 0 {
		t.Errorf("retries: %v %v %d", ok, err, n)
	}
	if ok, err := Param("timeout", &n); ok || err != nil {
		t.Errorf("an absent parameter was answered %v, %v", ok, err)
	}
}

func TestTheRunContextJoinsRunJSONAndTheEnvironment(t *testing.T) {
	in(t, "01JMZ8V1P9C4XQ7K2N4D6F8H0A", "charge", 2, "ok,rejected")
	write(t, "agk/run.json", `{"id":"01JMZ8V1P9C4XQ7K2N4D6F8H0A","workflow":"monthly-invoicing","namespace":"finance","commit":"a3f9c1e","trigger_kind":"schedule","triggered_by":"finance/agentiik","state":"running","started_at":"2026-10-01T04:03:13Z"}`)
	t.Setenv("AGK_WORKFLOW", "monthly-invoicing@a3f9c1e")
	t.Setenv("AGK_SHARD", "3/8")
	t.Setenv("AGK_DEADLINE", "2026-10-01T04:13:13Z")
	t.Setenv("AGK_REPO", "/agk/repo")
	r, err := Context()
	if err != nil {
		t.Fatal(err)
	}
	if r.Workflow != "monthly-invoicing" || r.Version != "a3f9c1e" || r.TriggerKind != "schedule" || r.Attempt != 2 ||
		r.Shard == nil || *r.Shard != (Shard{3, 8}) || !r.Deadline.Equal(time.Date(2026, 10, 1, 4, 13, 13, 0, time.UTC)) ||
		strings.Join(r.OutPorts, ",") != "ok,rejected" || r.Repo != "/agk/repo" {
		t.Errorf("context %+v", r)
	}
}

func TestASecretIsReadFromItsFileAndAMissingOneIsSaidSo(t *testing.T) {
	in(t, "r", "s", 1, "out")
	write(t, "agk/secrets/bearer", "s3cret")
	t.Setenv("BEARER", "from the environment")
	if b, ok, err := Secret("bearer"); !ok || err != nil || string(b) != "s3cret" {
		t.Errorf("bearer: %q %v %v", b, ok, err)
	}
	if _, ok, err := Secret("smtp"); ok || err != nil {
		t.Errorf("an absent secret was answered %v, %v", ok, err)
	}
	if _, _, err := Secret("../params.json"); err == nil {
		t.Error("a secret outside /agk/secrets was read")
	}
}

func TestExitLeavesByTheBandsAndSaysWhy(t *testing.T) {
	var left []int
	var log strings.Builder
	exit, stderr = func(code int) { left = append(left, code) }, &log
	t.Cleanup(func() { exit, stderr = os.Exit, os.Stderr })
	Fail("a")
	Retry("b")
	Refuse("c")
	Exit(125, "d")
	if !slices.Equal(left, []int{1, 100, 120, 1}) {
		t.Errorf("left with %v", left)
	}
	if !strings.HasPrefix(log.String(), "a\nb\nc\nd\nexit code 125 is not a brick's to leave with") {
		t.Errorf("the log says %q", log.String())
	}
}
