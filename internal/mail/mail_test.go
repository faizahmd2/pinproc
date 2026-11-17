package mail

import (
	"bufio"
	"context"
	"net"
	"strings"
	"testing"
	"time"
)

// fakeSMTP accepts one message and returns what it received.
func fakeSMTP(t *testing.T) (port int, got chan string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	got = make(chan string, 1)
	go func() {
		defer ln.Close()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		r := bufio.NewReader(c)
		say := func(s string) { c.Write([]byte(s + "\r\n")) }
		say("220 fake ESMTP")
		var data strings.Builder
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			cmd := strings.ToUpper(strings.TrimSpace(line))
			switch {
			case strings.HasPrefix(cmd, "EHLO"):
				say("250-fake")
				say("250 8BITMIME")
			case strings.HasPrefix(cmd, "MAIL"), strings.HasPrefix(cmd, "RCPT"):
				data.WriteString(line)
				say("250 ok")
			case cmd == "DATA":
				say("354 go")
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					data.WriteString(l)
				}
				say("250 queued")
			case cmd == "QUIT":
				say("221 bye")
				got <- data.String()
				return
			default:
				say("250 ok")
			}
		}
	}()
	return ln.Addr().(*net.TCPAddr).Port, got
}

func TestSendMultipartWithAttachment(t *testing.T) {
	port, got := fakeSMTP(t)
	err := Send(context.Background(), Server{Host: "127.0.0.1", Port: port, Security: "none", Timeout: 5 * time.Second}, Message{
		From: "pinproc <alerts@example.com>", To: []string{"ops@example.com", "Dev <dev@example.com>"},
		Subject: "[pinproc] host — Memory pressure ✓", Text: "plain body", HTML: "<p>html body</p>",
		Attachments: []Attachment{{Name: "report.html", ContentType: "text/html", Data: []byte("<html>" + strings.Repeat("x", 200) + "</html>")}},
	})
	if err != nil {
		t.Fatal(err)
	}
	msg := <-got
	for _, want := range []string{"MAIL FROM:<alerts@example.com>", "RCPT TO:<ops@example.com>", "RCPT TO:<dev@example.com>",
		"multipart/mixed", "multipart/alternative", "text/plain", "text/html", "filename=\"report.html\"", "=?utf-8?q?", "Auto-Submitted: auto-generated"} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q", want)
		}
	}
}

func TestSendFailsFastOnDeadServer(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	start := time.Now()
	err := Send(context.Background(), Server{Host: "127.0.0.1", Port: port, Security: "none", Timeout: 2 * time.Second},
		Message{From: "a@example.com", To: []string{"b@example.com"}, Subject: "s", Text: "t"})
	if err == nil || time.Since(start) > 3*time.Second {
		t.Fatalf("err=%v after %s", err, time.Since(start))
	}
}
