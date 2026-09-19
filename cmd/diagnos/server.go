package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sync"

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
	Hint      string             `json:"hint,omitempty"`
	Dimension contract.Dimension `json:"dimension,omitempty"`
	Budget    string             `json:"budget,omitempty"`
	Trigger   string             `json:"trigger,omitempty"`
	NoAI      bool               `json:"no_ai,omitempty"`
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
		Short: "run the local trigger/report HTTP agent",
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
			dir, err := config.ResolveOutputDirectory(cfg.Agent.ReportDir)
			if err != nil {
				return err
			}
			s := &nativeServer{cfg: cfg, report: dir}
			mux := http.NewServeMux()
			mux.HandleFunc("/trigger", s.handleTrigger)
			mux.HandleFunc("/report", s.handleReport)
			logger.Info("vm-native-diagnos listening", "addr", listen, "report_dir", dir)
			return http.ListenAndServe(listen, mux)
		},
	}
}

func (s *nativeServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req triggerRequest
	if r.Body != nil {
		defer r.Body.Close()
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req)
	}
	if req.Budget == "" {
		req.Budget = s.cfg.Engine.Budget
	}
	if req.Budget == "" {
		req.Budget = "normal"
	}
	b, err := budget(req.Budget)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Trigger == "" {
		req.Trigger = "http"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	src := source.NewLocal("/proc", "/sys", 8<<20)
	defer src.Close()

	reg, err := capability.BuildBuiltin()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	dec, err := makeDecisionProvider(s.cfg, req.NoAI)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	eng := engine.New(engine.Options{
		Source: src, Registry: reg, Rules: rules.Default(), Decision: dec,
		Identity: identity.New(src), Budget: b, ParallelWidth: s.cfg.Engine.ParallelWidth, Logger: logger,
	})
	inv, err := eng.Run(r.Context(), engine.Request{
		Host: "localhost", Trigger: req.Trigger, Hint: req.Hint, Dimension: req.Dimension,
	})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if s.cfg.Narrator.Enabled {
		if text, ne := narrator.NewRules().Narrate(context.Background(), inv); ne == nil && narrator.Validate(inv, text) == nil {
			inv.Narrative = text
		}
	}
	if err := report.Write(inv, filepath.Clean(s.report)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"id": inv.ID, "status": "completed", "stop_reason": inv.StopReason,
		"report": "/report",
	})
}

func (s *nativeServer) handleReport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data, err := os.ReadFile(filepath.Join(s.report, "investigation.json"))
	if err != nil {
		if os.IsNotExist(err) {
			http.Error(w, "no report available; POST /trigger first", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(data)
}
