package render

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"strings"
	"sync"

	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/inventory"
)

//go:embed templates/*
var files embed.FS

var (
	once  sync.Once
	tmpl  *template.Template
	perr  error
	style template.CSS
)

func templates() (*template.Template, error) {
	once.Do(func() {
		css, err := files.ReadFile("templates/style.css")
		if err != nil {
			perr = err
			return
		}
		style = template.CSS(css)
		tmpl, perr = template.New("pinproc").Funcs(template.FuncMap{"css": func() template.CSS { return style }}).
			ParseFS(files, "templates/*.tmpl")
	})
	return tmpl, perr
}

// HTML renders the self-contained HTML page for a document: the pressure alert
// layout for alerts, the zero-sum inventory layout for reports. It has no external
// assets, so it can be POSTed, attached or opened from disk as-is.
func HTML(d *doc.Document, o Options) ([]byte, error) {
	t, err := templates()
	if err != nil {
		return nil, err
	}
	name := "report.html.tmpl"
	if d.Kind == doc.KindAlert {
		name = "alert.html.tmpl"
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, newView(d, o)); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Email is one rendered message.
type Email struct {
	Subject string
	HTML    string
	Text    string
}

type emailPillar struct{ Name, Stat, State, Color string }

type emailRow struct {
	Name, PID, Exe, Mem, CPU, IO string
	MemStyle, CPUStyle, IOStyle  template.CSS
	Cause                        bool
}

type emailMem struct{ Label, Value, Pct string }

type emailView struct {
	Subject, BannerText, Color, SoftBg, SoftText string
	Arch, Disk, Uptime, MemTotal, Attachment     string
	Pillars                                      []emailPillar
	Rows                                         []emailRow
	Memory                                       []emailMem
}

// RenderEmail renders the inline-styled email (table layout, no CSS classes or
// scripts, so Gmail/Outlook show it as designed) plus a plain-text alternative.
func RenderEmail(d *doc.Document, o Options, attachment string) (Email, error) {
	t, err := templates()
	if err != nil {
		return Email{}, err
	}
	v := newView(d, o)
	s := v.S
	e := emailView{Attachment: attachment, Uptime: Duration(s.Host.Uptime), MemTotal: GiB(float64(s.Memory.AccountedBytes)) + " GiB"}
	switch d.Severity {
	case doc.SeverityCritical:
		e.Color, e.SoftBg, e.SoftText = "#dc2626", "#fef2f2", "#991b1b"
		e.BannerText = "CRITICAL PRESSURE BREACH"
	case doc.SeverityWarning:
		e.Color, e.SoftBg, e.SoftText = "#d97706", "#fffbeb", "#92400e"
		e.BannerText = "PRESSURE WARNING"
	default:
		e.Color, e.SoftBg, e.SoftText = "#0284c7", "#f0f9ff", "#075985"
		e.BannerText = "DETAILED MACHINE REPORT"
		if d.Kind == doc.KindAlert {
			e.BannerText = "MANUAL PRESSURE CHECK"
		}
	}
	if d.Dimension != "" && d.Kind == doc.KindAlert {
		e.BannerText += " · " + strings.ToUpper(dimName(d.Dimension))
	}
	e.Subject = fmt.Sprintf("[pinproc] %s — %s", s.Host.Hostname, d.Headline)
	if d.Kind == doc.KindReport {
		e.Subject = fmt.Sprintf("[pinproc] %s — detailed report", s.Host.Hostname)
	}
	e.Arch = fmt.Sprintf("%s (%d vCPU / %s)", s.Host.Arch, s.CPU.Cores, gibShort(s.Memory.TotalBytes))
	var disks []string
	for _, dk := range s.Disks {
		var used, avail uint64
		for _, m := range dk.Mounts {
			used += m.UsedBytes
			avail += m.AvailBytes
		}
		disks = append(disks, fmt.Sprintf("%s %s (%s free)", dk.Name, gibShort(dk.SizeBytes), gibShort(avail)))
	}
	e.Disk = strings.Join(disks, " · ")
	if e.Disk == "" {
		e.Disk = "n/a"
	}
	colors := map[string]string{"crit": "#dc2626", "warn": "#d97706", "ok": "#059669"}
	for _, p := range v.Pillars {
		e.Pillars = append(e.Pillars, emailPillar{Name: p.Name, Stat: p.Stat, State: p.State, Color: colors[p.Class]})
	}
	for i, c := range v.Services {
		if i >= 5 {
			break
		}
		sv := findService(s, c)
		plain := template.CSS("color:#334155;")
		r := emailRow{Name: c.Name, Cause: c.Cause, MemStyle: plain, CPUStyle: plain, IOStyle: plain}
		if sv != nil {
			r.PID = fmt.Sprint(sv.MainPID)
			r.Exe = sv.Exe
			r.Mem = BytesU(sv.MemBytes)
			r.CPU = Cores(sv.CPUCores)
			r.IO = Rate(sv.ReadBPS + sv.WriteBPS)
		}
		if c.Class != "" { // highlight the column of the resource that triggered the alert
			hot := template.CSS("color:" + colors[c.Class] + ";font-weight:bold;")
			switch d.Dimension {
			case "cpu", "scheduling":
				r.CPUStyle = hot
			case "io", "filesystem":
				r.IOStyle = hot
			default:
				r.MemStyle = hot
			}
		}
		e.Rows = append(e.Rows, r)
	}
	if d.Kind == doc.KindReport {
		for _, bk := range s.Memory.Buckets {
			e.Memory = append(e.Memory, emailMem{Label: bk.Label, Value: Bytes(bk.Value), Pct: Pct(share(bk.Value, float64(s.Memory.AccountedBytes)))})
		}
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, "email.html.tmpl", struct {
		V *view
		E emailView
	}{v, e}); err != nil {
		return Email{}, err
	}
	text := d.Text
	if text == "" {
		text = Text(d, o)
	}
	return Email{Subject: e.Subject, HTML: buf.String(), Text: text}, nil
}

func findService(s *inventory.Snapshot, c svcCard) *inventory.Service {
	for i := range s.Services {
		sv := &s.Services[i]
		if sv.Name == c.Name || sv.Unit == c.Name {
			return sv
		}
	}
	return nil
}

// FileName is a stable, sortable name for a rendered document.
func FileName(d *doc.Document, ext string) string {
	host := "host"
	if d.Snapshot != nil && d.Snapshot.Host.Hostname != "" {
		host = d.Snapshot.Host.Hostname
	}
	host = strings.Map(func(r rune) rune {
		if r == '-' || r == '.' || r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			return r
		}
		return '-'
	}, host)
	return fmt.Sprintf("pinproc-%s-%s-%s.%s", d.Kind, host, d.GeneratedAt.UTC().Format("20060102-150405"), ext)
}

// Text renders a compact plain-text summary (used when no engine text exists, and
// as the email's text/plain part).
func Text(d *doc.Document, o Options) string {
	v := newView(d, o)
	var b strings.Builder
	fmt.Fprintf(&b, "pinproc · %s", v.S.Host.Hostname)
	if v.HostIP != "" {
		fmt.Fprintf(&b, " (%s)", v.HostIP)
	}
	fmt.Fprintf(&b, " · %s\n\n%s\n", v.Time, d.Headline)
	if d.TriggerDetail != "" {
		fmt.Fprintf(&b, "%s\n", d.TriggerDetail)
	}
	b.WriteString("\n")
	for _, p := range v.Pillars {
		fmt.Fprintf(&b, "%-22s %-24s %s\n", p.Name, p.Stat, p.State)
	}
	b.WriteString("\nMemory\n")
	for _, r := range v.Mem.Rows {
		fmt.Fprintf(&b, "  %-36s %12s MiB  %6s\n", r.Label, r.Value, r.Pct)
	}
	for _, dk := range v.Disks {
		fmt.Fprintf(&b, "\n%s\n", dk.Title)
		for _, r := range dk.Rows {
			fmt.Fprintf(&b, "  %-36s %10s GiB  %6s\n", r.Label, r.Value, r.Pct)
		}
	}
	if len(v.Services) > 0 {
		b.WriteString("\nTop services\n")
		for _, c := range v.Services {
			fmt.Fprintf(&b, "  %d. %s [%s]  %s\n", c.Rank, c.Name, c.Badge, c.Metrics)
		}
	}
	return b.String()
}
