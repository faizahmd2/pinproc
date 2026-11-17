package mail

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

// scriptedSMTP records every command; it offers AUTH LOGIN only (like Office 365)
// and can reject one recipient.
func scriptedSMTP(t *testing.T, reject string) (int, chan []string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	out := make(chan []string, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { c.Write([]byte(s + "\r\n")) }
		var log []string
		defer func() { out <- log }()
		say("220 fake ESMTP")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.TrimSpace(line)
			up := strings.ToUpper(cmd)
			switch {
			case strings.HasPrefix(up, "EHLO"):
				say("250-fake")
				say("250 AUTH LOGIN XOAUTH2")
				continue
			case up == "AUTH LOGIN":
				say("334 " + base64.StdEncoding.EncodeToString([]byte("Username:")))
				u, _ := r.ReadString('\n')
				say("334 " + base64.StdEncoding.EncodeToString([]byte("Password:")))
				p, _ := r.ReadString('\n')
				ub, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(u))
				pb, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(p))
				log = append(log, "AUTH LOGIN "+string(ub)+":"+string(pb))
				say("235 ok")
				continue
			case strings.HasPrefix(up, "RCPT") && reject != "" && strings.Contains(cmd, reject):
				log = append(log, cmd)
				say("550 5.7.1 relaying denied")
				continue
			case up == "DATA":
				log = append(log, cmd)
				say("554 no data expected in a dry run")
				continue
			case up == "QUIT":
				log = append(log, cmd)
				say("221 bye")
				return
			}
			log = append(log, cmd)
			say("250 ok")
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, out
}

func TestVerifyIsADryRunWithLoginAuth(t *testing.T) {
	port, got := scriptedSMTP(t, "")
	steps, err := Verify(context.Background(), Server{Host: "127.0.0.1", Port: port, Security: "none", Username: "u@example.com", Password: "s3cret", Timeout: 5 * time.Second},
		"pinproc <u@example.com>", []string{"ops@example.com", "dev@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	log := strings.Join(<-got, "\n")
	for _, want := range []string{"AUTH LOGIN u@example.com:s3cret", "MAIL FROM:<u@example.com>", "RCPT TO:<ops@example.com>", "RCPT TO:<dev@example.com>", "RSET", "QUIT"} {
		if !strings.Contains(log, want) {
			t.Errorf("conversation missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, "DATA") {
		t.Fatalf("dry run must never send DATA:\n%s", log)
	}
	names := []string{}
	for _, s := range steps {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "connect,auth,sender,recipient,recipient" {
		t.Fatalf("steps = %v", names)
	}
}

func TestVerifyReportsRejectedRecipientWithHint(t *testing.T) {
	port, got := scriptedSMTP(t, "nobody@")
	steps, err := Verify(context.Background(), Server{Host: "127.0.0.1", Port: port, Security: "none", Username: "u", Password: "p", Timeout: 5 * time.Second},
		"u@example.com", []string{"ok@example.com", "nobody@example.com"})
	<-got
	if err == nil || !strings.Contains(err.Error(), "RCPT TO nobody@example.com") {
		t.Fatalf("err = %v", err)
	}
	if len(steps) != 4 { // connect, auth, sender, first recipient
		t.Fatalf("steps = %+v", steps)
	}
	if h := Hint(err); !strings.Contains(h, "refused a recipient") {
		t.Fatalf("hint = %q", h)
	}
}

func TestHints(t *testing.T) {
	cases := map[string]string{
		"smtp auth (PLAIN) as x: 535 5.7.8 Username and Password not accepted":   "app password",
		"connect smtp.x:587: dial tcp: lookup smtp.x: no such host":              "does not resolve",
		"connect 1.2.3.4:25: dial tcp 1.2.3.4:25: i/o timeout":                   "blocked",
		"connect 1.2.3.4:587: dial tcp 1.2.3.4:587: connect: connection refused": "nothing is listening",
	}
	for msg, want := range cases {
		if h := Hint(errors.New(msg)); !strings.Contains(h, want) {
			t.Errorf("Hint(%q) = %q, want it to mention %q", msg, h, want)
		}
	}
}
