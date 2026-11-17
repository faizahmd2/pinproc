package deliver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/doc"
	"github.com/faizahmd2/pinproc/internal/inventory"
)

func TestCallbackHTMLAndCooldown(t *testing.T) {
	var hits atomic.Int32
	var ctype atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(b), "<!DOCTYPE html>") {
			t.Errorf("body is not the HTML page")
		}
		ctype.Store(r.Header.Get("Content-Type"))
		hits.Add(1)
	}))
	defer srv.Close()

	cfg := &config.Config{}
	cfg.Callback = config.CallbackConfig{Enabled: true, URL: srv.URL, Method: "POST", Format: "html", Timeout: 2 * time.Second}
	cfg.Alerts.Cooldown = 30 * time.Minute
	cfg.Report.Timezone = "UTC"
	d := &Dispatcher{Cfg: cfg, StateDir: t.TempDir()}
	alert := doc.New(doc.KindAlert, nil, &inventory.Snapshot{Host: inventory.Host{Hostname: "h"}})
	alert.Dimension = contract.DimensionMemory

	r := d.Deliver(context.Background(), alert, Enabled(cfg), false)
	if len(r) != 1 || !r[0].Sent {
		t.Fatalf("first alert = %+v", r)
	}
	if ct, _ := ctype.Load().(string); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("content type = %q", ct)
	}
	r = d.Deliver(context.Background(), alert, Enabled(cfg), false)
	if r[0].Sent || !strings.Contains(r[0].Skipped, "cooldown") {
		t.Fatalf("second alert inside cooldown = %+v", r)
	}
	alert.Dimension = contract.DimensionCPU // a different resource is not held
	if r = d.Deliver(context.Background(), alert, Enabled(cfg), false); !r[0].Sent {
		t.Fatalf("cpu alert = %+v", r)
	}
	alert.Dimension = contract.DimensionMemory
	if r = d.Deliver(context.Background(), alert, Enabled(cfg), true); !r[0].Sent {
		t.Fatalf("forced alert = %+v", r)
	}
	if hits.Load() != 3 {
		t.Errorf("server hits = %d, want 3", hits.Load())
	}
}

func TestEnabledChannels(t *testing.T) {
	cfg := &config.Config{}
	if len(Enabled(cfg)) != 0 {
		t.Fatal("nothing should be enabled by default")
	}
	cfg.Email.Enabled = true
	if ch := Enabled(cfg); len(ch) != 1 || ch[0] != Email {
		t.Fatalf("channels = %v", ch)
	}
}
