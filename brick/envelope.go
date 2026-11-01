package brick

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Envelope is what travels on a port: the batch and its items. It is closed, and the runner
// refuses one carrying a member it does not know, so it has these two and nothing else.
type Envelope struct {
	Meta  Meta   `json:"meta"`
	Items []Item `json:"items"`
}

// Meta describes the batch. run_id, step, port and attempt are what the container was given,
// and Emit takes them from the environment rather than from the caller for that reason.
// produced_at is stamped by the runner when the port is published, which is after the brick is
// gone, so Emit writes the zero time.
type Meta struct {
	RunID      string `json:"run_id"`
	Step       string `json:"step"`
	Port       string `json:"port"`
	Attempt    int    `json:"attempt"`
	Count      int    `json:"count"`
	ProducedAt string `json:"produced_at"`
}

// Item is one unit of work: an identity, a payload and the artifacts it refers to.
//
// Data is decoded with json.Number for numbers, so that an amount or an identifier passes
// through a brick with the digits it arrived with rather than as the nearest float64.
//
// The identity is the brick's to choose. One derived from the payload it came from makes two
// runs over one batch comparable and lets a replay rejoin what it rejoined before.
type Item struct {
	ID    string         `json:"id"`
	Data  map[string]any `json:"data"`
	Files []File         `json:"files"`
}

// File is an artifact an item refers to rather than carries.
type File struct {
	Name      string `json:"name"`
	URI       string `json:"uri"`
	MediaType string `json:"media_type"`
	Size      int64  `json:"size"`
	SHA256    string `json:"sha256"`
}

// Stdin is where Read takes the envelope of the port in when no file was mounted for it, which
// is a brick run by hand with an envelope piped in. A test replaces it.
var Stdin io.Reader = os.Stdin

// Read is the envelope that arrived on an input port, from /agk/in/<port>/envelope.json.
//
// For the port in alone, a brick run by hand with no file mounted reads it from standard input,
// the single envelope the contract also hands a brick there. A port nobody fed is an empty
// batch rather than an error: an edge above that published nothing is the ordinary case, and a
// brick refusing it would fail a run for the absence of work.
func Read(port string) (Envelope, error) {
	if err := checkName(port); err != nil {
		return Envelope{}, fmt.Errorf("port %w", err)
	}
	path := at(filepath.Join(InDir, port, "envelope.json"))
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) && port == "in" {
		b, err = piped()
	}
	if errors.Is(err, fs.ErrNotExist) || err == nil && len(bytes.TrimSpace(b)) == 0 {
		return Envelope{Meta: Meta{Port: port}, Items: []Item{}}, nil
	}
	if err != nil {
		return Envelope{}, fmt.Errorf("the envelope on port %s could not be read: %w", port, err)
	}
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var e Envelope
	if err := d.Decode(&e); err != nil {
		return Envelope{}, fmt.Errorf("the envelope on port %s is not an envelope: %w", port, err)
	}
	if e.Items == nil {
		e.Items = []Item{}
	}
	for i := range e.Items {
		if e.Items[i].Data == nil {
			e.Items[i].Data = map[string]any{}
		}
		if e.Items[i].Files == nil {
			e.Items[i].Files = []File{}
		}
	}
	return e, nil
}

// piped reads standard input when something was piped into it, and answers fs.ErrNotExist
// for a terminal, which nobody means as an envelope and which would otherwise wait for ever.
func piped() ([]byte, error) {
	if f, ok := Stdin.(*os.File); ok {
		info, err := f.Stat()
		if err != nil || info.Mode()&os.ModeCharDevice != 0 {
			return nil, fs.ErrNotExist
		}
	}
	return io.ReadAll(Stdin)
}

// Items is the items of an input port, which is what most bricks want of Read.
func Items(port string) ([]Item, error) {
	e, err := Read(port)
	return e.Items, err
}

// Emit writes the envelope of one output port at /agk/out/ports/<port>.json.
//
// The metadata comes from the environment and count from the items, since the runner refuses
// an envelope that disagrees with either. The envelope is held to the size rules first: a value
// of data above inline_max_bytes is spilled to an artifact, and one still above
// envelope_max_bytes, or holding more than max_items, is refused with ErrTooLarge rather than
// left for the runner to refuse with 121.
//
// A port a brick has nothing to say on need not be emitted at all: a port nobody writes
// publishes an empty envelope.
func Emit(port string, items []Item) error {
	if err := checkName(port); err != nil {
		return fmt.Errorf("port %w", err)
	}
	t, err := taskOf()
	if err != nil {
		return err
	}
	if !t.declared(port) {
		return fmt.Errorf("port %s is not one this step declares, which are %v: the runner refuses an envelope on any other", port, t.ports)
	}
	if items == nil {
		items = []Item{}
	}
	l := Limits.orDefault()
	if len(items) > l.MaxItems {
		return fmt.Errorf("%w: port %s holds %d items and max_items is %d", ErrTooLarge, port, len(items), l.MaxItems)
	}
	for i := range items {
		if items[i].Data == nil {
			items[i].Data = map[string]any{}
		}
		if items[i].Files == nil {
			items[i].Files = []File{}
		}
	}
	if err := spill(t, port, items, l.InlineMax); err != nil {
		return err
	}
	e := Envelope{
		Meta:  Meta{RunID: t.runID, Step: t.step, Port: port, Attempt: t.attempt, Count: len(items), ProducedAt: "1970-01-01T00:00:00Z"},
		Items: items,
	}
	b, err := encode(e)
	if err != nil {
		return fmt.Errorf("the envelope on port %s cannot be encoded: %w", port, err)
	}
	if len(b) > l.EnvelopeMax {
		return fmt.Errorf("%w: the envelope on port %s weighs %d bytes with its heavy values spilled, and envelope_max_bytes is %d", ErrTooLarge, port, len(b), l.EnvelopeMax)
	}
	dir := at(OutPorts)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, port+".json"), append(b, '\n'), 0o644)
}

// Attach writes an artifact under /agk/out/files/ and answers the File an item refers to it by,
// its size and digest computed here because the item states them and the runner holds it to
// them. /agk/out/files is one directory for every port, so a name is one artifact of the task.
func Attach(port, name, mediaType string, content []byte) (File, error) {
	if err := checkName(name); err != nil {
		return File{}, fmt.Errorf("artifact %w", err)
	}
	t, err := taskOf()
	if err != nil {
		return File{}, err
	}
	return attach(t, port, name, mediaType, content)
}

func attach(t task, port, name, mediaType string, content []byte) (File, error) {
	dir := at(OutFiles)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return File{}, err
	}
	if err := os.WriteFile(filepath.Join(dir, name), content, 0o644); err != nil {
		return File{}, fmt.Errorf("the artifact %s could not be written: %w", name, err)
	}
	sum := sha256.Sum256(content)
	return File{
		Name:      name,
		URI:       "agk://run/" + t.runID + "/" + t.step + "/" + port + "/" + name,
		MediaType: mediaType,
		Size:      int64(len(content)),
		SHA256:    hex.EncodeToString(sum[:]),
	}, nil
}

// encode is the JSON of a value as encoding/json writes it, the engine's own encoding: keys
// sorted, no space, <, > and & escaped. It is what a value is measured and spilled as.
func encode(v any) ([]byte, error) {
	return json.Marshal(v)
}
