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
	Hint string `json:"hint,omitempty"`
	Dimension contract.Dimension `json:"dimension,omitempty"`
	Budget string `json:"budget,omitempty"`
	Trigger string `json:"trigger,omitempty"`
	NoAI bool `json:"no_ai,omitempty"`
}

type nativeServer struct {
	mu sync.Mutex
	cfg *config.Config
	report string
}

func newServeCmd() *cobra.Command {
	var listen string
	return &cobra.Command{
		Use: "serve", Short: "run the local trigger/report HTTP agent",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load(cfgPath); if err != nil { return err }
			if listen=="" { listen=cfg.Server.Listen }; if listen=="" { listen="127.0.0.1:8080" }
			dir, err := config.ResolveOutputDirectory(cfg.Output.Directory); if err!=nil{return err}
			if err:=report.EnsureWritable(dir);err!=nil{return fmt.Errorf("output directory unavailable: %w",err)}
			probe:=source.NewLocalWithTimeout("/proc","/sys",8<<20,cfg.Source.ReadTimeout)
			defer probe.Close()
			if err:=probe.StartupCheck();err!=nil{return err}
			s:=&nativeServer{cfg:cfg,report:filepath.Clean(dir)}
			mux:=http.NewServeMux();mux.HandleFunc("/trigger",s.handleTrigger);mux.HandleFunc("/report",s.handleReport)
			srv:=&http.Server{Addr:listen,Handler:mux,ReadHeaderTimeout:5*time.Second,ReadTimeout:10*time.Second,WriteTimeout:10*time.Minute+30*time.Second,IdleTimeout:60*time.Second}
			logger.Info("vm-native-diagnos listening","addr",listen,"report_dir",dir)
			return srv.ListenAndServe()
		},
	}
}

func (s *nativeServer) handleTrigger(w http.ResponseWriter, r *http.Request) {
	if !authorized(r,s.cfg.Server.APIKey){http.Error(w,"unauthorized",http.StatusUnauthorized);return}
	if r.Method!=http.MethodGet&&r.Method!=http.MethodPost{http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return}
	req,err:=parseTriggerRequest(w,r);if err!=nil{http.Error(w,err.Error(),http.StatusBadRequest);return}
	if req.Budget==""{req.Budget=s.cfg.Engine.Budget};if req.Budget==""{req.Budget="normal"}
	b,err:=budget(req.Budget);if err!=nil{http.Error(w,err.Error(),http.StatusBadRequest);return}
	if req.Trigger==""{req.Trigger="http"}
	if !s.mu.TryLock(){http.Error(w,"investigation already running",http.StatusConflict);return}
	id:=fmt.Sprintf("inv-%d",time.Now().UnixNano())
	if err := report.WriteRunning(s.report,id,"localhost",req.Trigger); err != nil { s.mu.Unlock(); http.Error(w,"unable to persist running state: "+err.Error(),http.StatusInternalServerError); return }
	logger.Info("investigation accepted","id",id,"trigger",req.Trigger,"budget",req.Budget)
	w.Header().Set("Content-Type","application/json");w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(map[string]any{"id":id,"status":"running","report":"/report"})
	go s.runAsync(id,req,b)
}

func (s *nativeServer) runAsync(id string, req triggerRequest, b contract.Budget) {
	defer s.mu.Unlock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logger.Info("investigation running", "id", id)
	done := make(chan struct{})
	defer close(done)
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				logger.Info("investigation still running", "id", id)
			case <-done:
				return
			}
		}
	}()
	defer func(){if r:=recover();r!=nil{
		inv:=&contract.Investigation{SchemaVersion:contract.SchemaVersion,ID:id,Host:"localhost",Trigger:req.Trigger,Hint:req.Hint,StartedAt:time.Now(),Budget:b,StopReason:contract.StopError}
		inv.Limitations=append(inv.Limitations,fmt.Sprintf("investigation panic: %v",r))
		_ = report.Write(inv,s.report)
		logger.Error("investigation panic","id",id,"panic",r)
	}}()
	src:=source.NewLocalWithTimeout("/proc","/sys",8<<20,s.cfg.Source.ReadTimeout);defer src.Close()
	reg,err:=capability.BuildBuiltin();if err!=nil{_ = s.writeEngineError(id,req,b,err);return}
	dec,err:=makeDecisionProvider(s.cfg,req.NoAI);if err!=nil{_ = s.writeEngineError(id,req,b,err);return}
	eng:=engine.New(engine.Options{Source:src,Registry:reg,Rules:rules.Default(),Decision:dec,Identity:identity.New(src),Budget:b,ParallelWidth:s.cfg.Engine.ParallelWidth,MaxFindings:s.cfg.Report.MaxFindings,DecisionNotice:decisionNotice(s.cfg,req.NoAI),Logger:logger})
	inv,err:=eng.Run(ctx,engine.Request{ID:id,Host:"localhost",Trigger:req.Trigger,Hint:req.Hint,Dimension:req.Dimension})
	if err!=nil{_ = s.writeEngineError(id,req,b,err); logger.Error("investigation failed","id",id,"error",err); return}
	if s.cfg.Narrator.Enabled{if text,ne:=narrator.NewRules().Narrate(context.Background(),inv);ne==nil&&narrator.Validate(inv,text)==nil{inv.Narrative=text}}
	if err:=report.Write(inv,s.report);err!=nil{logger.Error("write report failed","id",id,"error",err)}
}

func (s *nativeServer) writeEngineError(id string,req triggerRequest,b contract.Budget,err error) error {
	inv:=&contract.Investigation{SchemaVersion:contract.SchemaVersion,ID:id,Host:"localhost",Trigger:req.Trigger,Hint:req.Hint,StartedAt:time.Now(),Budget:b,StopReason:contract.StopError,Limitations:[]string{err.Error()}}
	return report.Write(inv,s.report)
}

func parseTriggerRequest(w http.ResponseWriter,r *http.Request)(triggerRequest,error){
	var req triggerRequest
	if r.Method==http.MethodPost&&r.Body!=nil{
		defer r.Body.Close()
		dec:=json.NewDecoder(http.MaxBytesReader(w,r.Body,64<<10))
		if err:=dec.Decode(&req);err!=nil&&!errors.Is(err,io.EOF){return req,fmt.Errorf("invalid JSON body")}
	}
	q:=r.URL.Query()
	if v:=q.Get("hint");v!=""{req.Hint=v};if v:=q.Get("dimension");v!=""{req.Dimension=contract.Dimension(v)}
	if v:=q.Get("budget");v!=""{req.Budget=v};if v:=q.Get("trigger");v!=""{req.Trigger=v}
	if v:=q.Get("no_ai");v!=""{x,err:=strconv.ParseBool(v);if err!=nil&&v!="1"&&v!="0"{return req,fmt.Errorf("invalid no_ai value")};if err==nil{req.NoAI=x}else{req.NoAI=v=="1"}}
	return req,nil
}

func authorized(r *http.Request,key string)bool{
	if key==""{return true};if isLoopback(r){return true};return r.URL.Query().Get("key")==key
}
func isLoopback(r *http.Request)bool{
	host,_,err:=net.SplitHostPort(r.RemoteAddr);if err!=nil{host=r.RemoteAddr};ip:=net.ParseIP(host);return ip!=nil&&ip.IsLoopback()
}

func (s *nativeServer) handleReport(w http.ResponseWriter,r *http.Request){
	if !authorized(r,s.cfg.Server.APIKey){http.Error(w,"unauthorized",http.StatusUnauthorized);return}
	if r.Method!=http.MethodGet{http.Error(w,"method not allowed",http.StatusMethodNotAllowed);return}
	dir,err:=report.LatestDir(s.report);if err!=nil{if os.IsNotExist(err){if running,re:=report.ReadRunning(s.report);re==nil{w.Header().Set("Content-Type","application/json");w.WriteHeader(http.StatusAccepted);_ = json.NewEncoder(w).Encode(map[string]any{"id":running.ID,"status":"running"});return};http.Error(w,"no report available; GET /trigger first",http.StatusNotFound);return};http.Error(w,err.Error(),http.StatusInternalServerError);return}
	format:=strings.ToLower(r.URL.Query().Get("format"))
	if format=="json"{data,err:=os.ReadFile(filepath.Join(dir,"investigation.json"));if err!=nil{http.Error(w,err.Error(),http.StatusInternalServerError);return};w.Header().Set("Content-Type","application/json; charset=utf-8");_,_=w.Write(data);return}
	data,err:=os.ReadFile(filepath.Join(dir,"report.md"));if err!=nil{http.Error(w,err.Error(),http.StatusInternalServerError);return}
	w.Header().Set("Content-Type","text/plain; charset=utf-8");_,_=w.Write(data)
}
