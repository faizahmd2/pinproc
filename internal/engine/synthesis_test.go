package engine

import (
	"testing"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/rules"
)

func TestSynthesizeCorrelatesIOWaitAndSaturation(t *testing.T) {
	ev := []contract.Evidence{
		{ID:"cpu-1", Entity:contract.Entity{Kind:contract.EntityMachine,ID:"machine"}},
		{ID:"io-1", Entity:contract.Entity{Kind:contract.EntityDevice,ID:"nvme0n1"}},
	}
	signals := []rules.Signal{
		{ID:"cpu.iowait_dominant", Dimension:contract.DimensionCPU, Severity:3, Statement:"iowait dominant", Support:[]string{"cpu-1"}, Force:true},
		{ID:"io.saturated", Dimension:contract.DimensionIO, Severity:4, Statement:"io saturated", Support:[]string{"io-1"}, Force:true},
	}
	got := Synthesize(signals, ev, 5)
	found := false
	for _, h := range got {
		if h.Grade == contract.GradeCorrelated && h.Dimension == contract.DimensionIO { found = true; break }
	}
	if !found { t.Fatalf("expected correlated hypothesis, got %+v", got) }
}

func TestSynthesizeCapsFindings(t *testing.T) {
	var signals []rules.Signal
	for i:=0;i<8;i++ { signals=append(signals,rules.Signal{ID:"s"+string(rune('a'+i)),Dimension:contract.DimensionCPU,Severity:1,Statement:"x"}) }
	got:=Synthesize(signals,nil,3)
	if len(got)!=3 { t.Fatalf("got %d findings",len(got)) }
}
