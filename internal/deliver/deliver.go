// Package deliver is the last, fire-and-forget stage: it renders a saved Document
// and hands it to the configured channels (HTTP callback, SMTP email). It runs off
// the watcher's sampling path, bounds every network call with a timeout, and
// remembers when each automatic alert was sent so a long incident is reported
// once per cooldown instead of on every capture.
package deliver

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faizahmd2/pinproc/internal/callback"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/mail"
	"github.com/faizahmd2/pinproc/internal/render"
)

// Channel is a delivery target.
type Channel string

const (
	Callback Channel = "callback"
	Email    Channel = "email"
)

// Result is the outcome for one channel.
type Result struct {
	Channel Channel
	Sent    bool
	Skipped string // why it was not attempted (disabled, cooldown, ...)
	Err     error
}

func (r Result) String() string {
	switch {
	case r.Err != nil:
		return fmt.Sprintf("%s: failed — %v", r.Channel, r.Err)
	case r.Skipped != "":
		return fmt.Sprintf("%s: skipped — %s", r.Channel, r.Skipped)
	}
	return fmt.Sprintf("%s: delivered", r.Channel)
}

// Dispatcher sends documents. Safe for concurrent use.
type Dispatcher struct {
	Cfg      *config.Config
	StateDir string // where the cooldown ledger lives; "" disables the ledger
	Log      *slog.Logger
	mu       sync.Mutex
}

// Options returns the render options for the configured timezone.
func Options(cfg *config.Config) render.Options {
	loc, zone, err := render.ParseZone(cfg.Report.Timezone)
	if err != nil {
		return render.Options{}
	}
	return render.Options{Location: loc, Zone: zone}
}

// Enabled lists the channels that are configured and switched on.
func Enabled(cfg *config.Config) []Channel {
	var out []Channel
	if cfg.Callback.Enabled && strings.TrimSpace(cfg.Callback.URL) != "" {
		out = append(out, Callback)
	}
	if cfg.Email.Enabled {
		out = append(out, Email)
	}
	return out
}

// Fire delivers an automatic alert in the background and returns immediately.
// The cooldown applies. Results are logged.
func (d *Dispatcher) Fire(dc *doc.Document) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
		defer cancel()
		for _, r := range d.Deliver(ctx, dc, Enabled(d.Cfg), false) {
			if d.Log == nil {
				continue
			}
			switch {
			case r.Err != nil:
				d.Log.Warn("alert delivery failed", "channel", r.Channel, "error", r.Err)
			case r.Skipped != "":
				d.Log.Info("alert delivery skipped", "channel", r.Channel, "reason", r.Skipped)
			default:
				d.Log.Info("alert delivered", "channel", r.Channel, "dimension", dc.Dimension)
			}
		}
	}()
}

// Deliver sends dc to each channel and waits. force skips the cooldown (explicit
// operator requests); automatic alerts pass force=false.
func (d *Dispatcher) Deliver(ctx context.Context, dc *doc.Document, channels []Channel, force bool) []Result {
	opt := Options(d.Cfg)
	var page []byte
	var pageErr error
	rendered := false
	pageOnce := func() ([]byte, error) {
		if !rendered {
			page, pageErr = render.HTML(dc, opt)
			rendered = true
		}
		return page, pageErr
	}
	var out []Result
	for _, ch := range channels {
		r := Result{Channel: ch}
		key := string(ch) + ":" + string(dc.Dimension)
		if !force && dc.Kind == doc.KindAlert {
			if until, held := d.held(key); held {
				r.Skipped = "cooldown until " + opt.Time(until) + " (alerts.cooldown " + d.Cfg.Alerts.Cooldown.String() + ")"
				out = append(out, r)
				continue
			}
		}
		switch ch {
		case Callback:
			r.Err = d.sendCallback(ctx, dc, pageOnce)
		case Email:
			r.Err = d.sendEmail(ctx, dc, opt, pageOnce)
		default:
			r.Err = fmt.Errorf("unknown channel")
		}
		r.Sent = r.Err == nil
		if r.Sent && !force && dc.Kind == doc.KindAlert {
			d.record(key)
		}
		out = append(out, r)
	}
	return out
}

func (d *Dispatcher) sendCallback(ctx context.Context, dc *doc.Document, page func() ([]byte, error)) error {
	c := d.Cfg.Callback
	if strings.TrimSpace(c.URL) == "" {
		return fmt.Errorf("no callback.url configured (pinproc config callback --url ...)")
	}
	var body []byte
	ct := "text/html; charset=utf-8"
	switch c.Format {
	case "json":
		b, err := json.Marshal(dc)
		if err != nil {
			return err
		}
		body, ct = b, "application/json"
	case "text":
		body, ct = []byte(textOf(dc, Options(d.Cfg))), "text/plain; charset=utf-8"
	default:
		b, err := page()
		if err != nil {
			return err
		}
		body = b
	}
	return callback.SendBody(ctx, c.Method, c.URL, c.Timeout, ct, body)
}

func textOf(dc *doc.Document, o render.Options) string {
	if dc.Text != "" {
		return dc.Text
	}
	return render.Text(dc, o)
}

func (d *Dispatcher) sendEmail(ctx context.Context, dc *doc.Document, opt render.Options, page func() ([]byte, error)) error {
	e := d.Cfg.Email
	if strings.TrimSpace(e.Host) == "" || len(e.To) == 0 {
		return fmt.Errorf("email is not configured (pinproc config email)")
	}
	pass, err := e.EmailPassword()
	if err != nil {
		return err
	}
	name := ""
	var atts []mail.Attachment
	if e.AttachReport {
		b, err := page()
		if err != nil {
			return err
		}
		name = render.FileName(dc, "html")
		atts = append(atts, mail.Attachment{Name: name, ContentType: "text/html; charset=utf-8", Data: b})
	}
	msg, err := render.RenderEmail(dc, opt, name)
	if err != nil {
		return err
	}
	return mail.Send(ctx, mail.Server{Host: e.Host, Port: e.Port, Security: e.Security, Username: e.Username, Password: pass, Timeout: e.Timeout},
		mail.Message{From: e.From, To: e.To, Subject: msg.Subject, Text: msg.Text, HTML: msg.HTML, Attachments: atts})
}

// --- cooldown ledger -------------------------------------------------------

const ledgerFile = "delivery-state.json"

func (d *Dispatcher) load() map[string]time.Time {
	m := map[string]time.Time{}
	if d.StateDir == "" {
		return m
	}
	if b, err := os.ReadFile(filepath.Join(d.StateDir, ledgerFile)); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}

// held reports whether key is inside its cooldown, and until when.
func (d *Dispatcher) held(key string) (time.Time, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	last, ok := d.load()[key]
	if !ok {
		return time.Time{}, false
	}
	until := last.Add(d.Cfg.Alerts.Cooldown)
	return until, time.Now().Before(until)
}

func (d *Dispatcher) record(key string) {
	if d.StateDir == "" {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	m := d.load()
	now := time.Now().UTC()
	m[key] = now
	for k, t := range m { // forget entries long past any cooldown
		if now.Sub(t) > 7*24*time.Hour {
			delete(m, k)
		}
	}
	if b, err := json.Marshal(m); err == nil {
		_ = doc.WriteFile(filepath.Join(d.StateDir, ledgerFile), b)
	}
}
