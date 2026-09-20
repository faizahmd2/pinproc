package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/capability"
	"github.com/faizahmd2/vm-native-diagnos/internal/config"
	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/engine"
	"github.com/faizahmd2/vm-native-diagnos/internal/identity"
	"github.com/faizahmd2/vm-native-diagnos/internal/narrator"
	"github.com/faizahmd2/vm-native-diagnos/internal/report"
	"github.com/faizahmd2/vm-native-diagnos/internal/rules"
	"github.com/faizahmd2/vm-native-diagnos/internal/source"
	"github.com/spf13/cobra"
)

type triggerRequest struct {
	Hint      string            `json:"hint,omitempty"`
	Dimension contract.Dimension `json:"dimension,omitempty"`
	Budget    string            `json:"budget,omitempty"`
	Trigger   string            `json:"trigger,omitempty"`
	NoAI      bool              `json:"no_ai,omitempty"`
}

type nativeServer struct {
	mu     sync.Mutex
	cfg    *config.Config
	report string
}

func newServeCmd() *cobra.Command {
	var listen string
	return &cobra.Command{
		Use:   "serve",
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
			dir, err := config.ResolveOutputDirectory(cfg.Output.Directory)
			if err != nil {
				return err
			}
			if err := report.EnsureWritable(dir); err != nil {
				return fmt.Errorf("output directory unavailable: %w", err)
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
			mux.HandleFunc("/healthz", s.handleHealth)
			mux.HandleFunc("/status", s.handleStatus)
			mux.HandleFunc("/trigger", s.handleTrigger)
			mux.HandleFunc("/report", s.handleReport)

			srv := &http.Server{
				Addr:              listen,
				Handler:           mux,
				ReadHeaderTimeout: 5 * time.Second,
				ReadTimeout:       10 * time.Second,
				WriteTimeout:      10 * time.Minute,
				IdleTimeout:       60 * time.Second,
			}
			logger.Info("vm-native-diagnos service started", "addr", listen, "report_dir", dir)
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
	reason := "service restarted while an inspection was running"
	if st.ID != "" {
		reason = fmt.Sprintf("service restarted while inspection %s was running", st.ID)
	}
	_ = report.WriteFailure(dir, st.ID, st.Host, st.Trigger, "", contract.Budget{}, reason, contract.StopError)
	return report.FailState(dir, report.StatusInterrupted, reason)
}

func (s *nativeServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "service": "vm-native-diagnos"})
}

func (s *nativeServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, s.cfg.Server.APIKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "unauthorized"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st, err := report.ReadState(s.report)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "error": err.Error()})
		return
	}
	code := http.StatusOK
	if st.Status == report.StatusRunning {
		code = http.StatusAccepted
	}
	writeJSON(w, code, st)
}

func (s *nativeServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, s.cfg.Server.APIKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "unauthorized"})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"status": "error", "message": "only POST /trigger starts an inspection"})
		return
	}

	req, err := parseTriggerRequest(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	if req.Budget == "" {
		req.Budget = s.cfg.Engine.Budget
	}
	if req.Budget == "" {
		req.Budget = "normal"
	}
	b, err := budget(req.Budget)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	if req.Trigger == "" {
		req.Trigger = "http"
	}

	if !s.mu.TryLock() {
		st, _ := report.ReadState(s.report)
		writeJSON(w, http.StatusConflict, map[string]any{
			"status":  "running",
			"id":      st.ID,
			"message": "inspection already happening; wait for it to finish",
		})
		return
	}

	id := fmt.Sprintf("inv-%d", time.Now().UnixNano())
	if err := report.StartState(s.report, id, "localhost", req.Trigger); err != nil {
		s.mu.Unlock()
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": "unable to persist inspection state: " + err.Error()})
		return
	}

	logger.Info("inspection accepted", "id", id, "trigger", req.Trigger, "budget", req.Budget)
	writeJSON(w, http.StatusAccepted, map[string]any{
		"id": id, "status": "running", "status_url": "/status", "report_url": "/report",
	})
	go s.runAsync(id, req, b)
}

func (s *nativeServer) runAsync(id string, req triggerRequest, b contract.Budget) {
	defer s.mu.Unlock()
	ctx := context.Background()

	progress := func(stage string) {
		if err := report.UpdateState(s.report, stage); err != nil {
			logger.Warn("failed to persist inspection progress", "id", id, "stage", stage, "error", err)
		}
	}
	progress("starting")
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				st, err := report.ReadState(s.report)
				if err == nil {
					logger.Info("inspection progress", "id", id, "status", st.Status, "stage", st.Stage)
				}
			case <-done:
				return
			}
		}
	}()

	defer func() {
		if r := recover(); r != nil {
			reason := fmt.Sprintf("investigation panic: %v", r)
			_ = report.WriteFailure(s.report, id, "localhost", req.Trigger, req.Hint, b, reason, contract.StopError)
			_ = report.FailState(s.report, report.StatusFailed, reason)
			logger.Error("inspection panic", "id", id, "panic", r)
		}
	}()

	src := source.NewLocalWithTimeout("/proc", "/sys", 8<<20, s.cfg.Source.ReadTimeout)
	defer src.Close()

	progress("preparing")
	reg, err := capability.BuildBuiltin()
	if err != nil {
		s.failInspection(id, req, b, err)
		return
	}

	dec, err := makeDecisionProvider(s.cfg, req.NoAI)
	if err != nil {
		s.failInspection(id, req, b, err)
		return
	}

	eng := engine.New(engine.Options{
		Source: src, Registry: reg, Rules: rules.Default(), Decision: dec,
		Identity: identity.New(src), Budget: b, ParallelWidth: s.cfg.Engine.ParallelWidth,
		MaxFindings: s.cfg.Report.MaxFindings, DecisionNotice: decisionNotice(s.cfg, req.NoAI),
		Logger: logger, Progress: progress,
	})
	inv, err := eng.Run(ctx, engine.Request{
		ID: id, Host: "localhost", Trigger: req.Trigger, Hint: req.Hint, Dimension: req.Dimension,
	})
	if err != nil {
		s.failInspection(id, req, b, err)
		return
	}

	progress("narrating")
	if s.cfg.Narrator.Enabled {
		if text, ne := narrator.NewRules().Narrate(ctx, inv); ne == nil && narrator.Validate(inv, text) == nil {
			inv.Narrative = text
		}
	}

	progress("writing_report")
	if err := report.Write(inv, s.report); err != nil {
		_ = report.FailState(s.report, report.StatusFailed, "report write failed: "+err.Error())
		logger.Error("inspection report write failed", "id", id, "error", err)
		return
	}
	if err := report.FinishState(s.report); err != nil {
		logger.Error("failed to mark inspection done", "id", id, "error", err)
		return
	}
	logger.Info("inspection done", "id", id)
}

func (s *nativeServer) failInspection(id string, req triggerRequest, b contract.Budget, err error) {
	reason := err.Error()
	_ = report.WriteFailure(s.report, id, "localhost", req.Trigger, req.Hint, b, reason, contract.StopError)
	_ = report.FailState(s.report, report.StatusFailed, reason)
	logger.Error("inspection failed", "id", id, "error", err)
}

func parseTriggerRequest(w http.ResponseWriter, r *http.Request) (triggerRequest, error) {
	var req triggerRequest
	if r.Body == nil {
		return req, nil
	}
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	if err := dec.Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		return req, fmt.Errorf("invalid JSON body")
	}
	return req, nil
}

func authorized(r *http.Request, key string) bool {
	if key == "" {
		return true
	}
	if isLoopback(r) {
		return true
	}
	return r.URL.Query().Get("key") == key
}

func isLoopback(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (s *nativeServer) handleReport(w http.ResponseWriter, r *http.Request) {
	if !authorized(r, s.cfg.Server.APIKey) {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"status": "unauthorized"})
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	st, _ := report.ReadState(s.report)
	if st.Status == report.StatusRunning {
		writeJSON(w, http.StatusAccepted, st)
		return
	}
	dir, err := report.LatestDir(s.report)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, http.StatusNotFound, map[string]any{"status": "not_found", "message": "no completed inspection report available; POST /trigger first"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	format := strings.ToLower(r.URL.Query().Get("format"))
	if format == "json" {
		data, err := os.ReadFile(filepath.Join(dir, "investigation.json"))
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		return
	}
	data, err := os.ReadFile(filepath.Join(dir, "report.md"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"status": "error", "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
