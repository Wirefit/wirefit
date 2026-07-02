// Package override implements rule overrides (PRD 3.3/3.4): a team can
// downgrade one specific finding on one interaction, with a recorded
// justification and a mandatory expiry — the escape hatch for coordinated
// rollouts that keeps the gate on for everything else.
//
// Overrides bind to (interaction, path, rule) — never free text — so a
// refactor that moves the field invalidates the override loudly rather than
// silently keeping it alive.
package override

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/wirefit/wirefit/internal/diff"
)

// MaxValidity bounds how far in the future an override may expire.
const MaxValidity = 180 * 24 * time.Hour

type Override struct {
	Interaction   string `yaml:"interaction"`
	Path          string `yaml:"path"`
	Rule          string `yaml:"rule"`
	DowngradeTo   string `yaml:"downgrade-to"` // warning | safe
	Justification string `yaml:"justification"`
	Expires       string `yaml:"expires"` // YYYY-MM-DD

	used bool
}

type File struct {
	Overrides []*Override `yaml:"overrides"`
}

// Load reads and validates an overrides file. A missing file is not an error
// (overrides are optional); it returns an empty File.
func Load(path string, now time.Time) (*File, []error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &File{}, nil
	}
	if err != nil {
		return nil, []error{err}
	}
	var f File
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&f); err != nil {
		return nil, []error{fmt.Errorf("%s: %w", path, err)}
	}
	var errs []error
	for i, o := range f.Overrides {
		at := fmt.Sprintf("%s: overrides[%d]", path, i)
		if o.Interaction == "" || o.Path == "" || o.Rule == "" || o.Justification == "" || o.Expires == "" {
			errs = append(errs, fmt.Errorf("%s: interaction, path, rule, justification and expires are all required", at))
			continue
		}
		if o.DowngradeTo != "warning" && o.DowngradeTo != "safe" {
			errs = append(errs, fmt.Errorf("%s: downgrade-to must be warning or safe, got %q", at, o.DowngradeTo))
		}
		exp, err := time.Parse("2006-01-02", o.Expires)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: expires must be YYYY-MM-DD: %v", at, err))
			continue
		}
		if !exp.After(now) {
			errs = append(errs, fmt.Errorf("%s: expired on %s; remove it or re-justify with a new date (%s)",
				at, o.Expires, o.Justification))
		}
		if exp.Sub(now) > MaxValidity {
			errs = append(errs, fmt.Errorf("%s: expires %s is more than 180 days out; overrides are temporary by design", at, o.Expires))
		}
	}
	return &f, errs
}

func (o *Override) targetClass() diff.Class {
	if o.DowngradeTo == "safe" {
		return diff.Safe
	}
	return diff.Warning
}

// Apply downgrades matching findings for one interaction in place and tags
// them as overridden. Returns the overrides applied.
func (f *File) Apply(interactionID string, r *diff.Result) []*Override {
	var applied []*Override
	for _, o := range f.Overrides {
		if o.Interaction != interactionID {
			continue
		}
		for i := range r.Findings {
			fd := &r.Findings[i]
			if fd.Path != o.Path || fd.Rule != o.Rule || fd.Class <= o.targetClass() {
				continue
			}
			fd.Class = o.targetClass()
			fd.Overridden = true
			fd.Message += fmt.Sprintf("; override accepted: %s (expires %s)", o.Justification, o.Expires)
			o.used = true
			applied = append(applied, o)
		}
	}
	return applied
}

// Stale returns overrides that matched no finding across the whole check —
// the path/rule no longer exists, so the override must be removed (fail-loud
// per PRD 3.3).
func (f *File) Stale() []*Override {
	var out []*Override
	for _, o := range f.Overrides {
		if !o.used {
			out = append(out, o)
		}
	}
	return out
}
