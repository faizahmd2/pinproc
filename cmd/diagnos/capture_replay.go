package main

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/faizahmd2/pinproc/internal/capability"
	"github.com/faizahmd2/pinproc/internal/config"
	"github.com/faizahmd2/pinproc/internal/contract"
	drules "github.com/faizahmd2/pinproc/internal/decision/rules"
	"github.com/faizahmd2/pinproc/internal/engine"
	"github.com/faizahmd2/pinproc/internal/identity"
	"github.com/faizahmd2/pinproc/internal/narrator"
	"github.com/faizahmd2/pinproc/internal/report"
	"github.com/faizahmd2/pinproc/internal/rules"
	"github.com/faizahmd2/pinproc/internal/source"
	"github.com/spf13/cobra"
)

func newCaptureCmd() *cobra.Command {
	var hint, dim, budgetName, out, trigger string
	var noAI bool
	cmd:=&cobra.Command{Use:"capture [host]",Short:"capture one investigation for offline replay",Args:cobra.MaximumNArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
		host:="localhost";if len(args)>0{host=args[0]}
		cfg,err:=config.Load(cfgPath);if err!=nil{return err}
		base:=out;if base==""{base,_=config.ResolveOutputDirectory(cfg.Output.Directory)};if err:=report.EnsureWritable(base);err!=nil{return err}
		src:=source.NewLocalWithTimeout("/proc","/sys",8<<20,cfg.Source.ReadTimeout);defer src.Close();if err:=src.StartupCheck();err!=nil{return err}
		reg,err:=capability.BuildBuiltin();if err!=nil{return err};b,err:=budget(budgetName);if err!=nil{return err};if budgetName=="normal"{b,_=budget(cfg.Engine.Budget)}
		dec,err:=makeDecisionProvider(cfg,noAI);if err!=nil{return err}
		id:=fmt.Sprintf("inv-%d",time.Now().UnixNano());captureDir:=filepath.Join(base,id);rec:=source.NewRecording(src,captureDir)
		eng:=engine.New(engine.Options{Source:rec,Registry:reg,Rules:rules.Default(),Decision:dec,Identity:identity.New(rec),Budget:b,ParallelWidth:cfg.Engine.ParallelWidth,MaxFindings:cfg.Report.MaxFindings,DecisionNotice:decisionNotice(cfg,noAI),Logger:logger})
		inv,err:=eng.Run(context.Background(),engine.Request{ID:id,Host:host,Trigger:trigger,Hint:hint,Dimension:contract.Dimension(dim)});if err!=nil{return err}
		if cfg.Narrator.Enabled{if text,ne:=narrator.NewRules().Narrate(context.Background(),inv);ne==nil&&narrator.Validate(inv,text)==nil{inv.Narrative=text}}
		if err:=report.Write(inv,base);err!=nil{return err}
		fmt.Fprintln(cmd.OutOrStdout(),"capture:",captureDir)
		return report.Terminal(cmd.OutOrStdout(),inv)
	}}
	cmd.Flags().StringVar(&hint,"hint","","operator context");cmd.Flags().StringVar(&trigger,"trigger","manual","incident trigger/source");cmd.Flags().StringVar(&dim,"dimension","","cpu|memory|io|network|scheduling|filesystem|limits");cmd.Flags().StringVar(&budgetName,"budget","normal","fast|normal|deep");cmd.Flags().StringVar(&out,"out","","report/capture directory");cmd.Flags().BoolVar(&noAI,"no-ai",false,"disable AI decisions")
	return cmd
}

func newReplayCmd() *cobra.Command {
	var budgetName, out string
	cmd:=&cobra.Command{Use:"replay <fixture-dir>",Short:"replay a captured investigation offline",Args:cobra.ExactArgs(1),RunE:func(cmd *cobra.Command,args []string)error{
		cfg,err:=config.Load(cfgPath);if err!=nil{return err}
		dir,err:=filepath.Abs(args[0]);if err!=nil{return err};src:=source.NewReplay(dir);defer src.Close()
		reg,err:=capability.BuildBuiltin();if err!=nil{return err};b,err:=budget(budgetName);if err!=nil{return err};if budgetName=="normal"{b,_=budget(cfg.Engine.Budget)}
		eng:=engine.New(engine.Options{Source:src,Registry:reg,Rules:rules.Default(),Decision:drules.New(),Identity:identity.New(src),Budget:b,ParallelWidth:cfg.Engine.ParallelWidth,MaxFindings:cfg.Report.MaxFindings,Logger:logger})
		inv,err:=eng.Run(context.Background(),engine.Request{Host:"replay",Trigger:"replay"});if err!=nil{return err}
		if cfg.Narrator.Enabled{if text,ne:=narrator.NewRules().Narrate(context.Background(),inv);ne==nil&&narrator.Validate(inv,text)==nil{inv.Narrative=text}}
		base:=out;if base==""{base,_=config.ResolveOutputDirectory(cfg.Output.Directory)};if err:=report.Write(inv,base);err!=nil{return err}
		return report.Terminal(cmd.OutOrStdout(),inv)
	}}
	cmd.Flags().StringVar(&budgetName,"budget","normal","fast|normal|deep");cmd.Flags().StringVar(&out,"out","","report directory")
	return cmd
}
