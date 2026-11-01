package brick

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// SizeLimits are the size rules an envelope is held to when the runner collects it. A breach
// fails the step with exit code 121, charged to the brick, so Emit keeps to them itself.
type SizeLimits struct {
	// InlineMax is inline_max_bytes: above it, a value of data travels as an artifact.
	InlineMax int
	// EnvelopeMax is envelope_max_bytes, the most a serialised envelope may weigh.
	EnvelopeMax int
	// MaxItems is max_items, the most items one envelope may hold.
	MaxItems int
}

// DefaultLimits are the documentation's defaults. The contract tells a brick no other, so a
// brick on an installation that tightened them takes them as parameters and sets Limits.
var DefaultLimits = SizeLimits{InlineMax: 262144, EnvelopeMax: 4194304, MaxItems: 100000}

// Limits are the rules Emit holds every envelope to. A zero field is its default.
var Limits = DefaultLimits

func (l SizeLimits) orDefault() SizeLimits {
	if l.InlineMax <= 0 {
		l.InlineMax = DefaultLimits.InlineMax
	}
	if l.EnvelopeMax <= 0 {
		l.EnvelopeMax = DefaultLimits.EnvelopeMax
	}
	if l.MaxItems <= 0 {
		l.MaxItems = DefaultLimits.MaxItems
	}
	return l
}

// ErrTooLarge is an envelope that breaks envelope_max_bytes or max_items with its heavy values
// spilled. Nothing more can be spilled, and nothing else may be done with the rest without
// changing what the port means, so it is the brick's to refuse: a step that meets it splits its
// batch upstream.
var ErrTooLarge = errors.New("the envelope breaks the size rules")

// spill moves every top-level value of data heavier than inlineMax into an artifact named
// <item>-<field>.json holding its JSON, referenced in files[]. It is the rule and the name the
// engine spills its own values by, so an item spilled here reads as one the engine spilled.
// The unit is a top-level field: a field under the threshold holds nothing above it, so moving
// the heavy ones leaves nothing inline the runner refuses.
func spill(t task, port string, items []Item, inlineMax int) error {
	for i := range items {
		item := &items[i]
		fields := make([]string, 0, len(item.Data))
		for k := range item.Data {
			fields = append(fields, k)
		}
		sort.Strings(fields)
		for _, field := range fields {
			b, err := encode(item.Data[field])
			if err != nil {
				return fmt.Errorf("item %s: the field %s cannot be encoded: %w", item.ID, field, err)
			}
			if len(b) <= inlineMax {
				continue
			}
			file, err := attach(t, port, spilledName(item.ID, i, field), "application/json", b)
			if err != nil {
				return err
			}
			delete(item.Data, field)
			item.Files = append(item.Files, file)
		}
	}
	return nil
}

// spilledName is <item>-<field>.json, suffixed -2, -3 where an artifact of this task already
// holds the name, since /agk/out/files is one directory for every port.
func spilledName(id string, rank int, field string) string {
	if id == "" {
		id = fmt.Sprintf("item-%d", rank)
	}
	base := Segment(id) + "-" + Segment(field)
	name := base + ".json"
	for n := 2; ; n++ {
		if _, err := os.Stat(filepath.Join(at(OutFiles), name)); errors.Is(err, fs.ErrNotExist) {
			return name
		}
		name = fmt.Sprintf("%s-%d.json", base, n)
	}
}

// Segment files a string down to what one segment of a path and of a URI may hold: letters,
// digits, dot, underscore and hyphen, every other character an underscore, at most 64 of them,
// and field where nothing but dots is left. It is the engine's rule for the name of a value it
// spills.
func Segment(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
		if b.Len() >= 64 {
			break
		}
	}
	out := b.String()
	if strings.Trim(out, ".") == "" {
		return "field"
	}
	return out
}
