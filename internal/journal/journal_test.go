package journal

import (
	"strings"
	"testing"
	"time"
)

func TestParseKmsg(t *testing.T) {
	r, ok := parseKmsg([]byte("3,812,5123456,-;Out of memory: Killed process 4242 (java) total-vm:9GB\n SUBSYSTEM=mem\n"))
	if !ok || r.level != 3 || r.usec != 5123456 || !strings.HasPrefix(r.msg, "Out of memory: Killed process 4242") || strings.Contains(r.msg, "SUBSYSTEM") {
		t.Fatalf("got %+v ok=%v", r, ok)
	}
	if _, ok := parseKmsg([]byte("garbage")); ok {
		t.Fatal("garbage parsed")
	}
}

func TestParseJSONHandlesByteArrayMessages(t *testing.T) {
	in := `{"__REALTIME_TIMESTAMP":"1763380000000000","PRIORITY":"3","MESSAGE":"db timeout after 30s","_SYSTEMD_UNIT":"api.service","_PID":"77","SYSLOG_IDENTIFIER":"api"}
{"__REALTIME_TIMESTAMP":"1763380001000000","PRIORITY":"6","MESSAGE":[104,105,255],"UNIT":"api.service"}
not json`
	lines := parseJSON([]byte(in))
	if len(lines) != 2 {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].Priority != 3 || lines[0].PID != 77 || lines[0].Unit != "api.service" || lines[0].Ident != "api" {
		t.Fatalf("first = %+v", lines[0])
	}
	if lines[1].Message != "hi?" || lines[1].Unit != "api.service" {
		t.Fatalf("second = %+v", lines[1])
	}
}

func TestCollapseKeepsNewestGroupsWithCounts(t *testing.T) {
	now := time.Now()
	var in []Line
	for i := 0; i < 5; i++ {
		in = append(in, Line{At: now, Message: "connection to 10.0.0.5:5432 refused (attempt " + string(rune('1'+i)) + ")"})
	}
	in = append(in, Line{At: now, Message: "worker 12 exited"}, Line{At: now, Message: "worker 13 exited"}, Line{At: now, Message: "listening"})
	out := collapse(in, 2)
	if len(out) != 2 || out[0].Message != "worker 13 exited" || out[0].Count != 2 || out[1].Message != "listening" {
		t.Fatalf("out = %+v", out)
	}
}

func TestInteresting(t *testing.T) {
	if !interesting(Line{Priority: 6, Message: "upstream timed out while reading"}) {
		t.Fatal("timeout should be interesting")
	}
	if interesting(Line{Priority: 6, Message: "GET /health 200"}) {
		t.Fatal("health check is not interesting")
	}
	if !interesting(Line{Priority: 4, Message: "anything"}) {
		t.Fatal("warning priority is interesting")
	}
}
