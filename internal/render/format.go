// Package render turns a doc.Document into every human format pinproc emits: the
// HTML alert and detailed report (callback), the email, and plain text. Renderers
// are pure functions of the document; they never read the machine.
package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// Options are presentation settings (from the config's report section).
type Options struct {
	Location *time.Location // default UTC
	Zone     string         // label printed next to times, e.g. "UTC" or "IST"
}

func (o Options) loc() (*time.Location, string) {
	if o.Location == nil {
		return time.UTC, "UTC"
	}
	if o.Zone == "" {
		return o.Location, o.Location.String()
	}
	return o.Location, o.Zone
}

// Time formats t as "17 Nov 2026 10:45 UTC" in the configured zone.
func (o Options) Time(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	loc, label := o.loc()
	return t.In(loc).Format("02 Jan 2006 15:04") + " " + label
}

// Clock formats t as "10:45:12 IST".
func (o Options) Clock(t time.Time) string {
	loc, label := o.loc()
	return t.In(loc).Format("15:04:05") + " " + label
}

// ParseZone resolves the config's report.timezone. It accepts "UTC", common
// abbreviations (IST), IANA names ("Asia/Kolkata") and fixed offsets ("+05:30").
// Abbreviations and offsets work even on hosts without tzdata installed.
func ParseZone(name string) (*time.Location, string, error) {
	n := strings.TrimSpace(name)
	switch strings.ToUpper(n) {
	case "", "UTC", "GMT", "Z":
		return time.UTC, "UTC", nil
	case "IST":
		return time.FixedZone("IST", 5*3600+1800), "IST", nil
	}
	if abbr, ok := zoneAbbr[strings.ToUpper(n)]; ok {
		return time.FixedZone(strings.ToUpper(n), abbr), strings.ToUpper(n), nil
	}
	if n[0] == '+' || n[0] == '-' {
		sign := 1
		if n[0] == '-' {
			sign = -1
		}
		h, m, _ := strings.Cut(n[1:], ":")
		hh, err1 := strconv.Atoi(h)
		mm := 0
		var err2 error
		if m != "" {
			mm, err2 = strconv.Atoi(m)
		}
		if err1 != nil || err2 != nil || hh > 14 || mm > 59 {
			return nil, "", fmt.Errorf("timezone %q: use UTC, IST, an IANA name like Asia/Kolkata, or an offset like +05:30", name)
		}
		label := "UTC" + n
		return time.FixedZone(label, sign*(hh*3600+mm*60)), label, nil
	}
	loc, err := time.LoadLocation(n)
	if err != nil {
		return nil, "", fmt.Errorf("timezone %q: %w (use UTC, IST, an IANA name, or an offset like +05:30)", name, err)
	}
	label := time.Now().In(loc).Format("MST")
	if label == "" || label[0] == '+' || label[0] == '-' {
		label = n
	}
	return loc, label, nil
}

var zoneAbbr = map[string]int{
	"PST": -8 * 3600, "PDT": -7 * 3600, "EST": -5 * 3600, "EDT": -4 * 3600,
	"CET": 3600, "CEST": 2 * 3600, "BST": 3600, "JST": 9 * 3600, "SGT": 8 * 3600,
	"AEST": 10 * 3600, "GST": 4 * 3600, "PKT": 5 * 3600, "NPT": 5*3600 + 2700,
}

// Bytes formats a size with binary units: "4.92 GiB", "310 MiB".
func Bytes(v float64) string {
	const k = 1024.0
	switch {
	case v >= k*k*k*k:
		return fmt.Sprintf("%.2f TiB", v/(k*k*k*k))
	case v >= k*k*k:
		return fmt.Sprintf("%.2f GiB", v/(k*k*k))
	case v >= 100*k*k:
		return fmt.Sprintf("%.0f MiB", v/(k*k))
	case v >= k*k:
		return fmt.Sprintf("%.1f MiB", v/(k*k))
	case v >= k:
		return fmt.Sprintf("%.0f KiB", v/k)
	case v <= 0:
		return "0 B"
	}
	return fmt.Sprintf("%.0f B", v)
}

// BytesU is Bytes for unsigned counters.
func BytesU(v uint64) string { return Bytes(float64(v)) }

// GiB formats a size as a plain GiB number ("27.20"), for table columns.
func GiB(v float64) string { return fmt.Sprintf("%.2f", v/(1<<30)) }

// MiB formats a size as a MiB number with thousands separators ("3,212.0").
func MiB(v float64) string { return thousands(v/(1<<20), 1) }

// Rate formats bytes/second the way iostat does ("482.5 MB/s").
func Rate(bps float64) string {
	switch {
	case bps >= 1e9:
		return fmt.Sprintf("%.2f GB/s", bps/1e9)
	case bps >= 1e6:
		return fmt.Sprintf("%.1f MB/s", bps/1e6)
	case bps >= 1e3:
		return fmt.Sprintf("%.0f kB/s", bps/1e3)
	case bps <= 0:
		return "0"
	}
	return fmt.Sprintf("%.0f B/s", bps)
}

// Bits formats bytes/second as a link rate ("210 Mbps").
func Bits(bps float64) string {
	b := bps * 8
	switch {
	case b >= 1e9:
		return fmt.Sprintf("%.2f Gbps", b/1e9)
	case b >= 1e6:
		return fmt.Sprintf("%.0f Mbps", b/1e6)
	case b >= 1e3:
		return fmt.Sprintf("%.0f kbps", b/1e3)
	}
	return fmt.Sprintf("%.0f bps", b)
}

// Count formats a counter compactly ("9.4k").
func Count(v float64) string {
	switch {
	case v >= 1e6:
		return fmt.Sprintf("%.1fM", v/1e6)
	case v >= 1e4:
		return fmt.Sprintf("%.0fk", v/1e3)
	case v >= 1e3:
		return fmt.Sprintf("%.1fk", v/1e3)
	}
	return fmt.Sprintf("%.0f", v)
}

// Cores formats CPU use as cores ("2.15c").
func Cores(v float64) string { return fmt.Sprintf("%.2fc", v) }

// Pct formats a percentage with one decimal.
func Pct(v float64) string { return fmt.Sprintf("%.1f%%", v) }

// Pct0 formats a percentage without decimals.
func Pct0(v float64) string { return fmt.Sprintf("%.0f%%", v) }

// Duration formats an uptime ("42d 3h", "5h 12m", "8m").
func Duration(d time.Duration) string {
	days := int(d.Hours()) / 24
	h := int(d.Hours()) % 24
	m := int(d.Minutes()) % 60
	switch {
	case days > 0:
		return fmt.Sprintf("%dd %dh", days, h)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// Link formats a NIC speed ("10G", "1000M", "unknown").
func Link(mbps int) string {
	switch {
	case mbps <= 0:
		return "link speed n/a"
	case mbps >= 1000 && mbps%1000 == 0:
		return fmt.Sprintf("%dG", mbps/1000)
	}
	return fmt.Sprintf("%dM", mbps)
}

func thousands(v float64, decimals int) string {
	s := strconv.FormatFloat(v, 'f', decimals, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	neg := strings.HasPrefix(intPart, "-")
	intPart = strings.TrimPrefix(intPart, "-")
	var b strings.Builder
	for i, r := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	out := b.String()
	if frac != "" {
		out += "." + frac
	}
	if neg {
		out = "-" + out
	}
	return out
}

func share(part, whole float64) float64 {
	if whole <= 0 {
		return 0
	}
	return math.Max(0, math.Min(100, part/whole*100))
}
