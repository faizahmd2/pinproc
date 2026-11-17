// Package doc defines the single JSON document every pinproc output is rendered
// from. Collection writes one Document; the terminal view, the HTML alert and
// report, the email and the plain-text report are all pure functions of it. This
// keeps the watcher's sampling path free of any presentation or delivery work: a
// capture only has to produce and save a Document.
package doc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/inventory"
	"github.com/faizahmd2/pinproc/internal/journal"
	"github.com/faizahmd2/pinproc/internal/report"
)

// Schema is the document format version.
const Schema = 1

// Kind says what produced a document.
type Kind string

const (
	KindAlert  Kind = "alert"  // a captured (or manually triggered) pressure incident
	KindReport Kind = "report" // an on-demand detailed machine report
)

// Severity grades an alert.
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityWarning  Severity = "warning"
	SeverityInfo     Severity = "info"
)

// Document is the renderer-independent result of one pinproc run.
type Document struct {
	Schema        int                 `json:"schema"`
	Kind          Kind                `json:"kind"`
	ID            string              `json:"id"`
	GeneratedAt   time.Time           `json:"generated_at"`
	Trigger       string              `json:"trigger"` // auto:<dimension> | manual
	Dimension     contract.Dimension  `json:"dimension,omitempty"`
	Severity      Severity            `json:"severity"`
	Headline      string              `json:"headline"`
	TriggerDetail string              `json:"trigger_detail,omitempty"`
	Tag           string              `json:"tag,omitempty"`
	Snapshot      *inventory.Snapshot `json:"snapshot"`
	Incident      *contract.Incident  `json:"incident,omitempty"`
	Findings      []Finding           `json:"findings,omitempty"` // alerts only, and only when they add something
	Logs          *journal.Excerpt    `json:"logs,omitempty"`     // journal + kernel lines from the incident window
	Notes         []string            `json:"notes,omitempty"`
	Text          string              `json:"text,omitempty"` // the classic plain-text report
}

// Finding is one supporting conclusion and what to do about it.
type Finding struct {
	Statement  string             `json:"statement"`
	Action     string             `json:"action,omitempty"`
	Log        string             `json:"log,omitempty"`
	Dimension  contract.Dimension `json:"dimension,omitempty"`
	Confidence float64            `json:"confidence,omitempty"`
}

// maxFindings caps the "also noticed" list: a short list is read, a long one is not.
const maxFindings = 4

// New assembles a document from an engine investigation (cause, findings, text)
// and an inventory snapshot (the machine picture). Either may be nil.
func New(kind Kind, inv *contract.Investigation, snap *inventory.Snapshot) *Document {
	d := &Document{Schema: Schema, Kind: kind, GeneratedAt: time.Now().UTC(), Snapshot: snap, Severity: SeverityInfo, Trigger: "manual"}
	if snap != nil {
		d.GeneratedAt = snap.CollectedAt
		d.Notes = append(d.Notes, snap.Notes...)
	}
	if inv == nil {
		d.Headline = "Machine report"
		return d
	}
	d.ID = inv.ID
	if inv.Trigger != "" {
		d.Trigger = inv.Trigger
	}
	d.Incident = inv.Incident
	d.Headline = report.Verdict(inv)
	if kind == KindAlert {
		d.Text = report.RenderMarkdown(inv) // the classic report.md; a report renders its own text
	}
	caused := contract.Dimension("")
	if inv.Incident != nil {
		d.Dimension = inv.Incident.Dimension
		if inv.Incident.Cause != nil {
			caused = inv.Incident.Cause.Dimension
		}
	}
	if kind == KindAlert {
		d.Findings = findings(inv.Hypotheses, d.Dimension, caused)
	}
	for _, l := range inv.Limitations {
		d.Notes = append(d.Notes, l)
	}
	d.Notes = unique(d.Notes)
	if kind == KindAlert {
		d.Severity = SeverityWarning
		if strings.HasPrefix(d.Trigger, "auto:") {
			d.Severity = SeverityCritical
		}
	}
	return d
}

// findings keeps only what adds to the alert: confident conclusions about the
// incident's own resource (or very confident ones about another), never a
// restatement of the cause already shown. A report has none — it describes the
// machine; it does not diagnose it.
func findings(hyps []contract.Hypothesis, dim, caused contract.Dimension) []Finding {
	var out []Finding
	for _, h := range hyps {
		if caused != "" && h.Dimension == caused && strings.HasPrefix(h.Source, "owner:") {
			continue // already stated as the cause
		}
		strong := h.Confidence >= 0.5 || h.LogContext != nil || h.Grade == contract.GradeCorrelated
		sameResource := dim == "" || h.Dimension == dim || (dim == contract.DimensionIO && h.Dimension == contract.DimensionFilesystem)
		// Another resource's finding only when it is a very confident observation,
		// never an ownership/space breakdown that belongs to that resource's alert.
		onTopic := sameResource || (h.Confidence >= 0.75 && !strings.HasPrefix(h.Source, "owner:"))
		if !strong || !onTopic {
			continue
		}
		f := Finding{Statement: h.Statement, Action: h.Action, Dimension: h.Dimension, Confidence: h.Confidence}
		if h.LogContext != nil {
			f.Log = fmt.Sprintf("%s — %q", h.LogContext.Path, h.LogContext.Line)
		}
		out = append(out, f)
		if len(out) == maxFindings {
			break
		}
	}
	return out
}

// Save writes the document atomically as dir/name.json and returns the path.
func Save(dir, name string, d *Document) (string, error) {
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".json")
	return path, WriteFile(path, b)
}

// Load reads a saved document.
func Load(path string) (*Document, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var d Document
	if err := json.Unmarshal(b, &d); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &d, nil
}

// WriteFile replaces path atomically (temp file + fsync + rename), creating the
// directory when needed.
func WriteFile(path string, b []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0750); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".pinproc-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	_, werr := f.Write(b)
	if werr == nil {
		werr = f.Sync()
	}
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		_ = os.Chmod(tmp, 0640)
		werr = os.Rename(tmp, path)
	}
	if werr != nil {
		_ = os.Remove(tmp)
	}
	return werr
}

func unique(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
