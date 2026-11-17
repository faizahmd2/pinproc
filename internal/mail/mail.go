// Package mail sends one MIME message over SMTP with the standard library only
// (net/smtp). pinproc brings no mail provider: the operator configures their own
// SMTP server (Gmail/Workspace, SES, Postmark, Mailgun, a local relay, ...).
package mail

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"os"
	"strconv"
	"strings"
	"time"
)

// Server is where and how to send.
type Server struct {
	Host     string
	Port     int
	Security string // starttls | tls | none
	Username string
	Password string
	Timeout  time.Duration
}

// Attachment is one file attached to the message.
type Attachment struct {
	Name        string
	ContentType string
	Data        []byte
}

// Message is a multipart/alternative email with optional attachments.
type Message struct {
	From        string
	To          []string
	Subject     string
	Text        string
	HTML        string
	Attachments []Attachment
}

// Step is one stage of an SMTP conversation, reported by Verify.
type Step struct {
	Name   string
	Detail string
}

// Send delivers m through srv. The whole SMTP conversation is bounded by
// srv.Timeout (and ctx), so a dead server can never hang the caller.
func Send(ctx context.Context, srv Server, m Message) error {
	from, rcpts, err := addresses(m.From, m.To)
	if err != nil {
		return err
	}
	body, err := build(from, m)
	if err != nil {
		return err
	}
	ctx, cancel := withTimeout(ctx, srv)
	defer cancel()
	c, err := open(ctx, srv, nil)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := envelope(c, from.Address, rcpts, nil); err != nil {
		return err
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA: %w", err)
	}
	if _, err := w.Write(body); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("server rejected message: %w", err)
	}
	return c.Quit()
}

// Verify runs the conversation up to (not including) DATA — connect, TLS, AUTH,
// MAIL FROM and RCPT TO for every recipient — then resets and quits. It proves
// the settings work without delivering anything.
func Verify(ctx context.Context, srv Server, from string, to []string) ([]Step, error) {
	var steps []Step
	note := func(name, detail string) { steps = append(steps, Step{name, detail}) }
	f, rcpts, err := addresses(from, to)
	if err != nil {
		return steps, err
	}
	ctx, cancel := withTimeout(ctx, srv)
	defer cancel()
	c, err := open(ctx, srv, note)
	if err != nil {
		return steps, err
	}
	defer c.Close()
	if err := envelope(c, f.Address, rcpts, note); err != nil {
		return steps, err
	}
	_ = c.Reset()
	_ = c.Quit()
	return steps, nil
}

func addresses(from string, to []string) (*mail.Address, []string, error) {
	f, err := mail.ParseAddress(from)
	if err != nil {
		return nil, nil, fmt.Errorf("from address %q: %w", from, err)
	}
	var rcpts []string
	for _, t := range to {
		a, err := mail.ParseAddress(t)
		if err != nil {
			return nil, nil, fmt.Errorf("recipient %q: %w", t, err)
		}
		rcpts = append(rcpts, a.Address)
	}
	if len(rcpts) == 0 {
		return nil, nil, errors.New("no recipients")
	}
	return f, rcpts, nil
}

func withTimeout(ctx context.Context, srv Server) (context.Context, context.CancelFunc) {
	t := srv.Timeout
	if t <= 0 {
		t = 15 * time.Second
	}
	return context.WithTimeout(ctx, t)
}

// open connects, greets, upgrades to TLS and authenticates.
func open(ctx context.Context, srv Server, note func(string, string)) (*smtp.Client, error) {
	if note == nil {
		note = func(string, string) {}
	}
	deadline, _ := ctx.Deadline()
	addr := net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port))
	tlsCfg := &tls.Config{ServerName: srv.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: time.Until(deadline)}
	var conn net.Conn
	var err error
	if srv.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return nil, fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	note("connect", addr+" ("+conn.RemoteAddr().String()+")")
	if tc, ok := conn.(*tls.Conn); ok {
		note("tls", tlsVersion(tc.ConnectionState().Version)+" · certificate valid for "+srv.Host)
	}
	c, err := smtp.NewClient(conn, srv.Host)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("smtp greeting from %s: %w", addr, err)
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		if err := c.Hello(h); err != nil {
			c.Close()
			return nil, fmt.Errorf("smtp hello: %w", err)
		}
	}
	if srv.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			c.Close()
			return nil, fmt.Errorf("%s does not offer STARTTLS; use security tls (port 465), or none only for a local relay", addr)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			c.Close()
			return nil, fmt.Errorf("starttls: %w", err)
		}
		if st, ok := c.TLSConnectionState(); ok {
			note("starttls", tlsVersion(st.Version)+" · certificate valid for "+srv.Host)
		}
	}
	if srv.Username != "" {
		ok, mechs := c.Extension("AUTH")
		if !ok {
			c.Close()
			return nil, fmt.Errorf("%s does not offer AUTH (a username is configured; clear it for an open relay)", addr)
		}
		var a smtp.Auth
		mech := ""
		switch {
		case hasMech(mechs, "PLAIN"):
			a, mech = smtp.PlainAuth("", srv.Username, srv.Password, srv.Host), "PLAIN"
		case hasMech(mechs, "LOGIN"):
			a, mech = &loginAuth{user: srv.Username, pass: srv.Password, host: srv.Host}, "LOGIN"
		default:
			c.Close()
			return nil, fmt.Errorf("%s offers AUTH %s; pinproc supports PLAIN and LOGIN", addr, mechs)
		}
		if err := c.Auth(a); err != nil {
			c.Close()
			return nil, fmt.Errorf("smtp auth (%s) as %s: %w", mech, srv.Username, err)
		}
		note("auth", mech+" as "+srv.Username)
	}
	return c, nil
}

func envelope(c *smtp.Client, from string, rcpts []string, note func(string, string)) error {
	if err := c.Mail(from); err != nil {
		return fmt.Errorf("MAIL FROM %s: %w", from, err)
	}
	if note != nil {
		note("sender", from+" accepted")
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
		if note != nil {
			note("recipient", r+" accepted")
		}
	}
	return nil
}

func hasMech(list, m string) bool {
	for _, f := range strings.Fields(strings.ToUpper(list)) {
		if f == m {
			return true
		}
	}
	return false
}

func tlsVersion(v uint16) string {
	switch v {
	case tls.VersionTLS13:
		return "TLS 1.3"
	case tls.VersionTLS12:
		return "TLS 1.2"
	}
	return "TLS"
}

// loginAuth is the AUTH LOGIN mechanism (Office 365 and others offer it without
// PLAIN). Like smtp.PlainAuth it refuses to send credentials without TLS unless
// the server is on this machine.
type loginAuth struct {
	user, pass, host string
	step             int
}

func (a *loginAuth) Start(si *smtp.ServerInfo) (string, []byte, error) {
	if !si.TLS && !isLocal(si.Name) {
		return "", nil, errors.New("refusing AUTH LOGIN over an unencrypted connection")
	}
	if si.Name != a.host {
		return "", nil, errors.New("wrong host name")
	}
	a.step = 0
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(strings.TrimSpace(string(fromServer)))
	a.step++
	switch {
	case strings.HasPrefix(prompt, "user"):
		return []byte(a.user), nil
	case strings.HasPrefix(prompt, "pass"):
		return []byte(a.pass), nil
	case a.step == 1:
		return []byte(a.user), nil
	case a.step == 2:
		return []byte(a.pass), nil
	}
	return nil, fmt.Errorf("unexpected AUTH LOGIN prompt %q", prompt)
}

func isLocal(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// Hint turns a send/verify error into what to check, in plain words.
func Hint(err error) string {
	if err == nil {
		return ""
	}
	e := strings.ToLower(err.Error())
	switch {
	case strings.Contains(e, "no such host"):
		return "the SMTP host name does not resolve — check the spelling (e.g. smtp.gmail.com)"
	case strings.Contains(e, "i/o timeout") || strings.Contains(e, "deadline exceeded"):
		return "no answer from the server — outbound SMTP is often blocked by cloud firewalls; try port 587 (STARTTLS) or 465 (TLS) and check egress rules"
	case strings.Contains(e, "connection refused"):
		return "nothing is listening on that port — check the port and whether the server uses 587 (STARTTLS) or 465 (TLS)"
	case strings.Contains(e, "first record does not look like a tls handshake") || strings.Contains(e, "tls: ") && strings.Contains(e, "record"):
		return "TLS mismatch — port 587 usually needs security starttls, port 465 needs tls"
	case strings.Contains(e, "certificate"):
		return "the server's TLS certificate is not valid for this host name — use the provider's documented SMTP host"
	case strings.Contains(e, "535") || strings.Contains(e, "authentication") || strings.Contains(e, "auth"):
		return "the server rejected the username/password — Gmail, Outlook and Yahoo need an app password (not your login password); SES/SendGrid/Mailgun use SMTP credentials from their console"
	case strings.Contains(e, "mail from"):
		return "the server does not let this account send as that From address — use the account's own address or a verified sender"
	case strings.Contains(e, "rcpt to"):
		return "the server refused a recipient — check the address, or the account may not be allowed to relay to it (sandboxed SES accounts only send to verified addresses)"
	case strings.Contains(e, "starttls"):
		return "the server does not offer STARTTLS on this port — use security tls on port 465"
	}
	return ""
}

func build(from *mail.Address, m Message) ([]byte, error) {
	var buf bytes.Buffer
	hdr := func(k, v string) { fmt.Fprintf(&buf, "%s: %s\r\n", k, v) }
	hdr("From", from.String())
	hdr("To", strings.Join(m.To, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", m.Subject))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+randomID()+"@"+domainOf(from.Address)+">")
	hdr("MIME-Version", "1.0")
	hdr("X-Mailer", "pinproc")
	hdr("Auto-Submitted", "auto-generated")

	mixed := multipart.NewWriter(&buf)
	hdr("Content-Type", "multipart/mixed; boundary="+mixed.Boundary())
	buf.WriteString("\r\n")

	var alt bytes.Buffer
	altW := multipart.NewWriter(&alt)
	if err := qpPart(altW, "text/plain; charset=utf-8", m.Text); err != nil {
		return nil, err
	}
	if m.HTML != "" {
		if err := qpPart(altW, "text/html; charset=utf-8", m.HTML); err != nil {
			return nil, err
		}
	}
	if err := altW.Close(); err != nil {
		return nil, err
	}
	p, err := mixed.CreatePart(textproto.MIMEHeader{"Content-Type": {"multipart/alternative; boundary=" + altW.Boundary()}})
	if err != nil {
		return nil, err
	}
	if _, err := p.Write(alt.Bytes()); err != nil {
		return nil, err
	}
	for _, a := range m.Attachments {
		ct := a.ContentType
		if ct == "" {
			ct = "application/octet-stream"
		}
		p, err := mixed.CreatePart(textproto.MIMEHeader{
			"Content-Type":              {ct + "; name=\"" + a.Name + "\""},
			"Content-Disposition":       {"attachment; filename=\"" + a.Name + "\""},
			"Content-Transfer-Encoding": {"base64"},
		})
		if err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(a.Data)
		for len(enc) > 76 {
			p.Write([]byte(enc[:76] + "\r\n"))
			enc = enc[76:]
		}
		p.Write([]byte(enc + "\r\n"))
	}
	if err := mixed.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func qpPart(w *multipart.Writer, ct, body string) error {
	p, err := w.CreatePart(textproto.MIMEHeader{"Content-Type": {ct}, "Content-Transfer-Encoding": {"quoted-printable"}})
	if err != nil {
		return err
	}
	q := quotedprintable.NewWriter(p)
	if _, err := q.Write([]byte(strings.ReplaceAll(body, "\n", "\r\n"))); err != nil {
		return err
	}
	return q.Close()
}

func randomID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "." + hex.EncodeToString(b)
}

func domainOf(addr string) string {
	if i := strings.LastIndexByte(addr, '@'); i >= 0 {
		return addr[i+1:]
	}
	return "pinproc.local"
}
