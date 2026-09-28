package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faizahmd2/pinproc/internal/callback"
	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/narrator"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/faizahmd2/pinproc/internal/source"
	"github.com/spf13/cobra"
)

const (
	investigateWait = 30 * time.Second
	reportPendingMax = 2 * time.Minute
)

type investigateRequest struct {
	Hint string
	Dimension contract.Dimension
}

type investigationResult struct {
	Investigation *contract.Investigation
	Err error
}

type nativeServer struct {
	mu sync.Mutex
	cfg *config.Config
	report string
}

func newServeCmd() *cobra.Command {
	var listen string
	return &cobra.Command{
		Use: "serve",
		Short: "run the local inspection service",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath)
			if err != nil {
				return err
			}
			if listen == "" {
				listen = cfg.Server.Listen
			}
			if listen == "" {
				listen = "127.0.0.1:8080"
			}
			dir, err := config.ResolveOutputDirectory(config.DataDirectory)
			if err != nil {
				return err
			}
			if err := report.MigrateLegacy(dir); err != nil {
				return fmt.Errorf("prepare report storage: %w", err)
			}
			if err := report.EnsureWritable(dir); err != nil {
				return fmt.Errorf("data directory unavailable: %w", err)
			}
			if err := recoverServiceState(dir); err != nil {
				logger.Warn("could not recover previous inspection state", "error", err)
			}

			probe := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, cfg.Source.ReadTimeout)
			defer probe.Close()
			if err := probe.StartupCheck(); err != nil {
				return err
			}

			s := &nativeServer{cfg: cfg, report: filepath.Clean(dir)}
			mux := http.NewServeMux()
			mux.HandleFunc("/health", s.handleHealth)
			mux.HandleFunc("/investigate", s.handleInvestigate)
			mux.HandleFunc("/report", s.handleReport)

			srv := &http.Server{
				Addr: listen,
				Handler: mux,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout: 10 * time.Second,
				WriteTimeout: 35 * time.Second,
				IdleTimeout: 60 * time.Second,
			}
			if notice := decisionNotice(cfg); notice != "" { logger.Warn("ai reasoning unavailable", "message", notice) }
			logger.Info("pinproc service started", "addr", listen, "report_dir", dir)
			return srv.ListenAndServe()
		},
	}
}

func recoverServiceState(dir string) error {
	st, err := report.ReadState(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return report.WriteState(dir, report.InspectionState{Status: report.StatusIdle})
		}
		return err
	}
	if st.Status != report.StatusRunning {
		return nil
	}
	started := time.Now()
	if st.StartedAt != nil {
		started = *st.StartedAt
	}
	reason := "service restarted while an inspection was running"
	inv := report.Failure(started, "", reason, contract.StopError)
	if err := report.Write(inv, dir); err != nil {
		return err
	}
	return report.FailState(dir, report.StatusInterrupted, reason)
}

func (s *nativeServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	writeHTTPJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "pinproc"})
}

func (s *nativeServer) handleInvestigate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if !authorized(r, s.cfg.Server.APIKey) {
		writeHTTPJSON(w, http.StatusUnauthorized, map[string]any{"status": "unauthorized"})
		return
	}

	req, err := parseInvestigateRequest(r)
	if err != nil {
		writeHTTPJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
		return
	}

	jsonResponse := wantsJSON(r)

	if !s.mu.TryLock() {
		writeHTTPJSON(w, http.StatusAccepted, map[string]any{
			"status": "processing",
			"message": "An investigation is already in progress.",
		})
		return
	}

	started := time.Now()
	if err := report.StartState(s.report, started); err != nil {
		s.mu.Unlock()
		writeHTTPJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": "Unable to start investigation: " + err.Error()})
		return
	}

	resultCh := make(chan investigationResult, 1)
	go s.runAsync(started, req, resultCh)

	timer := time.NewTimer(investigateWait)
	defer timer.Stop()
	select {
	case result := <-resultCh:
		if result.Investigation == nil {
			writeHTTPJSON(w, http.StatusInternalServerError, map[string]any{
				"status": "failed",
				"message": "Investigation failed before a report could be generated. Use GET /report for details.",
			})
			return
		}
		if result.Err != nil {
			writeHTTPJSON(w, http.StatusInternalServerError, map[string]any{
				"status": "failed",
				"message": "Investigation failed. The generated error report is included.",
				"report": result.Investigation,
			})
			return
		}
		if jsonResponse {
			writeHTTPJSON(w, http.StatusOK, result.Investigation)
		} else {
			writeHTTPMarkdown(w, http.StatusOK, result.Investigation)
		}
	case <-timer.C:
		writeHTTPJSON(w, http.StatusAccepted, map[string]any{
			"status": "processing",
			"message": "Investigation is still running. Use GET /report for the result.",
		})
	}
}

func (s *nativeServer) runAsync(started time.Time, req investigateRequest, done chan<- investigationResult) {
	defer s.mu.Unlock()

	var result investigationResult
	fail := func(reason string, cause error) {
		inv := report.Failure(started, req.Hint, reason, contract.StopError)
		result.Investigation = inv
		if err := report.Write(inv, s.report); err != nil {
			result.Err = fmt.Errorf("%s; error report write failed: %v", reason, err)
			return
		}
		_ = report.FailState(s.report, report.StatusFailed, reason)
		if cause != nil {
			result.Err = cause
		} else {
			result.Err = fmt.Errorf("%s", reason)
		}
	}

	defer func() {
		if r := recover(); r != nil {
			reason := fmt.Sprintf("investigation panic: %v", r)
			fail(reason, fmt.Errorf("%s", reason))
			logger.Error("inspection panic", "panic", r)
		}
		done <- result
	}()

	ctx := context.Background()
	src := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, s.cfg.Source.ReadTimeout)
	defer src.Close()

	reg, err := capability.BuildBuiltin()
	if err != nil {
		fail("capability graph unavailable: "+err.Error(), err)
		return
	}

	dec, err := makeDecisionProvider(s.cfg)
	if err != nil {
		fail("decision provider unavailable: "+err.Error(), err)
		return
	}

	eng := engine.New(engine.Options{
		Source: src,
		Registry: reg,
		Rules: rules.Default(),
		Decision: dec,
		Identity: identity.New(src),
		Budget: contract.BudgetNormal(),
		ParallelWidth: 3,
		MaxFindings: s.cfg.Report.MaxFindings,
		DecisionNotice: decisionNotice(s.cfg),
		Logger: logger,
	})

	inv, err := eng.Run(ctx, engine.Request{
		ID: fmt.Sprintf("inv-%d", started.UnixNano()),
		Host: localHostName(),
		Trigger: "http",
		Hint: req.Hint,
		Dimension: req.Dimension,
	})
	if err != nil {
		fail("investigation engine failed: "+err.Error(), err)
		return
	}

	if inv.StartedAt.IsZero() {
		inv.StartedAt = started
	}
	inv.IncidentCheckedAt = started

	if s.cfg.Narrator.Enabled {
		if narrative, ne := narrator.NewRules().Narrate(ctx, inv); ne == nil && narrator.Validate(inv, narrative) == nil {
			inv.Narrative = narrative
		}
	}

	if err := report.Write(inv, s.report); err != nil {
		result.Investigation = inv
		result.Err = fmt.Errorf("report write failed: %w", err)
		return
	}
	if err := report.FinishState(s.report); err != nil {
		reason := "could not persist done state: " + err.Error()
		_ = report.FailState(s.report, report.StatusFailed, reason)
		result.Investigation = inv
		result.Err = fmt.Errorf("%s", reason)
		return
	}

	result.Investigation = inv
	if s.cfg.Callback.Enabled && strings.TrimSpace(s.cfg.Callback.URL) != "" {
		payload := *inv
		go func() {
			if err := callback.Post(context.Background(), s.cfg.Callback.URL, s.cfg.Callback.Timeout, &payload); err != nil {
				logger.Warn("report callback failed", "error", err)
			}
		}()
	}
}

func parseInvestigateRequest(r *http.Request) (investigateRequest, error) {
	var out investigateRequest
	query := r.URL.Query()
	out.Hint = strings.TrimSpace(query.Get("hint"))
	if len(out.Hint) > 2048 {
		return out, fmt.Errorf("hint is too long; maximum is 2048 characters")
	}
	if dim := strings.TrimSpace(query.Get("dimension")); dim != "" {
		switch contract.Dimension(dim) {
		case contract.DimensionCPU, contract.DimensionMemory, contract.DimensionIO,
			contract.DimensionNetwork, contract.DimensionScheduling, contract.DimensionFilesystem,
			contract.DimensionLimits:
			out.Dimension = contract.Dimension(dim)
		default:
			return out, fmt.Errorf("unsupported dimension %q", dim)
		}
	}
	return out, nil
}

func (s *nativeServer) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	if !authorized(r, s.cfg.Server.APIKey) {
		writeHTTPJSON(w, http.StatusUnauthorized, map[string]any{"status": "unauthorized"})
		return
	}
	st, err := report.ReadState(s.report)
	if err != nil && !os.IsNotExist(err) {
		writeHTTPJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	if err == nil && st.Status == report.StatusRunning {
		if st.StartedAt != nil && time.Since(*st.StartedAt) >= reportPendingMax {
			inv, reportErr := report.Read(s.report)
			if reportErr == nil {
				writeHTTPJSON(w, http.StatusOK, map[string]any{
					"status": "stale",
					"message": "The current investigation has exceeded the pending window. Returning the last completed report.",
					"report": inv,
				})
				return
			}
			writeHTTPJSON(w, http.StatusServiceUnavailable, map[string]any{
				"status": "processing",
				"message": "The current investigation is still running and no previous report is available.",
			})
			return
		}
		writeHTTPJSON(w, http.StatusAccepted, map[string]any{
			"status": "processing",
			"message": "An investigation is still in progress.",
			"started_at": st.StartedAt,
		})
		return
	}

	inv, err := report.Read(s.report)
	if err != nil {
		if os.IsNotExist(err) {
			writeHTTPJSON(w, http.StatusNotFound, map[string]any{"status": "not_found", "message": "No investigation report exists yet."})
			return
		}
		writeHTTPJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	if wantsJSON(r) {
		writeHTTPJSON(w, http.StatusOK, inv)
		return
	}
	writeHTTPMarkdown(w, http.StatusOK, inv)
}

func wantsJSON(r *http.Request) bool {
	return strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("json")), "true")
}

func writeHTTPMarkdown(w http.ResponseWriter, status int, inv *contract.Investigation) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(report.RenderMarkdown(inv)))
}

func authorized(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	if isLoopback(r) {
		return true
	}
	const prefix = "Bearer "
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	if strings.HasPrefix(value, prefix) {
		return strings.TrimSpace(strings.TrimPrefix(value, prefix)) == key
	}
	return false
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func localHostName() string {
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		return strings.TrimSpace(host)
	}
	return "localhost"
}

func writeHTTPJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
