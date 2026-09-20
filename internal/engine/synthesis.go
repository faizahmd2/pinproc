package engine

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
	"github.com/faizahmd2/vm-native-diagnos/internal/rules"
)

// Synthesize builds a bounded set of explicitly graded hypotheses.
func Synthesize(signals []rules.Signal, ev []contract.Evidence, limit ...int) []contract.Hypothesis {
	maxFindings := 5
	if len(limit) > 0 && limit[0] > 0 { maxFindings = limit[0] }

	var out []contract.Hypothesis
	for _, s := range signals {
		entity := contract.Entity{Kind: contract.EntityMachine, ID: "machine"}
		for _, e := range ev {
			for _, id := range s.Support {
				if e.ID == id { entity = e.Entity }
			}
		}
		out = append(out, contract.Hypothesis{
			ID: "hy-" + s.ID, Statement: s.Statement, Dimension: s.Dimension, Entity: entity,
			Grade: contract.GradeObserved, Support: append([]string{}, s.Support...),
			Confidence: float64(s.Severity) / 4, Source: "rule:" + s.ID,
		})
	}

	// Cross-dimension coincidence: high iowait is often the symptom while I/O saturation is the cause.
	hasIOWait := hasSignal(signals, "cpu.iowait_dominant")
	hasIOSat := hasSignal(signals, "io.saturated")
	if hasIOWait && hasIOSat {
		support := append(signalSupport(signals, "cpu.iowait_dominant"), signalSupport(signals, "io.saturated")...)
		out = append(out, contract.Hypothesis{
			ID: "hy-correlated-io-wait", Statement: "CPU busy time is primarily I/O wait, not compute, while block I/O is saturated",
			Dimension: contract.DimensionIO, Entity: entityForSupport(ev, support), Grade: contract.GradeCorrelated,
			Support: uniqueStrings(support), Confidence: 0.88, Source: "pattern:io_wait_plus_io_saturation",
		})
	}

	// Thread concentration: only synthesize when the top two threads account for most process CPU.
	out = append(out, threadConcentrationHypotheses(ev)...)

	// A saturated machine CPU finding is contradicted when no process has materially elevated user CPU.
	if hasSignal(signals, "cpu.saturated") {
		maxUser := 0.0
		var processEvidence []string
		for _, e := range ev {
			if e.Capability != "process.cpu" { continue }
			processEvidence = append(processEvidence, e.ID)
			if v, ok := observation(e, "proc.user_pct"); ok && v > maxUser { maxUser = v }
		}
		if len(processEvidence) > 0 && maxUser <= 20 {
			for i := range out {
				if out[i].Source == "rule:cpu.saturated" {
					out[i].Contradicts = append(out[i].Contradicts, processEvidence...)
				}
			}
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Confidence > out[j].Confidence })
	if len(out) > maxFindings { out = out[:maxFindings] }
	return out
}

func threadConcentrationHypotheses(ev []contract.Evidence) []contract.Hypothesis {
	type group struct {
		entity contract.Entity
		total float64
		threads []struct{ id string; cpu float64; ev string }
	}
	groups := map[string]*group{}
	for _, e := range ev {
		if e.Capability != "process.cpu" { continue }
		total, ok := observation(e, "proc.cpu_pct"); if !ok || total <= 0 { continue }
		g:=&group{entity:e.Entity,total:total}
		key:=e.Entity.ID; groups[key]=g
	}
	for _, e := range ev {
		if e.Capability != "thread.cpu" { continue }
		cpu, ok := observation(e, "thread.cpu_pct"); if !ok { continue }
		pid:=e.Entity.ParentID
		if g:=groups[pid]; g!=nil { g.threads=append(g.threads, struct{id string;cpu float64;ev string}{e.Entity.ID,cpu,e.ID}) }
	}
	var out []contract.Hypothesis
	for _, g := range groups {
		sort.Slice(g.threads, func(i,j int)bool{return g.threads[i].cpu>g.threads[j].cpu})
		if len(g.threads)<2 { continue }
		share:=(g.threads[0].cpu+g.threads[1].cpu)/g.total
		if share <= 0.70 { continue }
		name:=g.entity.Display; if name=="" { name=g.entity.ID }
		out=append(out,contract.Hypothesis{
			ID:"hy-thread-concentration-"+g.entity.ID,
			Statement:fmt.Sprintf("CPU is concentrated in %d threads of %s",2,name),
			Dimension:contract.DimensionCPU,Entity:g.entity,Grade:contract.GradeInferred,
			Support:[]string{g.threads[0].ev,g.threads[1].ev},Confidence:share,Source:"pattern:thread_concentration",
		})
	}
	return out
}

func hasSignal(ss []rules.Signal, id string) bool { for _, s := range ss { if s.ID==id { return true } }; return false }
func signalSupport(ss []rules.Signal,id string)[]string{for _,s:=range ss{if s.ID==id{return s.Support}};return nil}
func entityForSupport(ev []contract.Evidence, ids []string) contract.Entity{for _,e:=range ev{for _,id:=range ids{if e.ID==id{return e.Entity}}};return contract.Entity{Kind:contract.EntityMachine,ID:"machine"}}
func observation(e contract.Evidence,key string)(float64,bool){for _,o:=range e.Observations{if o.Key==key{return o.Value,true}};return 0,false}
func uniqueStrings(in []string)[]string{m:=map[string]bool{};out:=[]string{};for _,x:=range in{if x!=""&&!m[x]{m[x]=true;out=append(out,x)}};return out}
var _ = json.Marshal
var _ = strings.TrimSpace
