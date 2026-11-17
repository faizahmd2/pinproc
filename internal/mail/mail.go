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

// Send delivers m through srv. The whole SMTP conversation is bounded by
// srv.Timeout (and ctx), so a dead server can never hang the caller.
func Send(ctx context.Context, srv Server, m Message) error {
	from, err := mail.ParseAddress(m.From)
	if err != nil {
		return fmt.Errorf("from address: %w", err)
	}
	var rcpts []string
	for _, t := range m.To {
		a, err := mail.ParseAddress(t)
		if err != nil {
			return fmt.Errorf("recipient %q: %w", t, err)
		}
		rcpts = append(rcpts, a.Address)
	}
	if len(rcpts) == 0 {
		return errors.New("no recipients")
	}
	body, err := build(from, m)
	if err != nil {
		return err
	}
	timeout := srv.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	deadline, _ := ctx.Deadline()

	addr := net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port))
	tlsCfg := &tls.Config{ServerName: srv.Host, MinVersion: tls.VersionTLS12}
	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	if srv.Security == "tls" {
		conn, err = (&tls.Dialer{NetDialer: dialer, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect %s: %w", addr, err)
	}
	_ = conn.SetDeadline(deadline)
	c, err := smtp.NewClient(conn, srv.Host)
	if err != nil {
		conn.Close()
		return fmt.Errorf("smtp greeting: %w", err)
	}
	defer c.Close()
	if h, err := os.Hostname(); err == nil && h != "" {
		if err := c.Hello(h); err != nil {
			return fmt.Errorf("smtp hello: %w", err)
		}
	}
	if srv.Security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s does not offer STARTTLS; set email.security to tls (port 465) or none for a local relay", addr)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("starttls: %w", err)
		}
	}
	if srv.Username != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return fmt.Errorf("%s does not offer AUTH", addr)
		}
		if err := c.Auth(smtp.PlainAuth("", srv.Username, srv.Password, srv.Host)); err != nil {
			return fmt.Errorf("smtp auth: %w", err)
		}
	}
	if err := c.Mail(from.Address); err != nil {
		return fmt.Errorf("MAIL FROM: %w", err)
	}
	for _, r := range rcpts {
		if err := c.Rcpt(r); err != nil {
			return fmt.Errorf("RCPT TO %s: %w", r, err)
		}
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
