package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
)

func Write(inv *contract.Investigation, root string) error {
	if inv == nil { return fmt.Errorf("investigation is nil") }
	if inv.ID == "" { return fmt.Errorf("investigation id is empty") }
	if err := os.MkdirAll(root, 0755); err != nil { return err }
	dir := filepath.Join(root, inv.ID)
	if err := os.MkdirAll(dir, 0755); err != nil { return err }
	b, err := json.MarshalIndent(inv, "", "  ")
	if err != nil { return err }
	if err := atomic(filepath.Join(dir, "investigation.json"), append(b, '\n')); err != nil { return err }
	if err := atomic(filepath.Join(dir, "report.md"), []byte(RenderMarkdown(inv))); err != nil { return err }
	if err := retain(root, 3); err != nil { return err }
	return atomic(filepath.Join(root, "latest.txt"), []byte(inv.ID+"\n"))
}

func EnsureWritable(root string) error {
	if err := os.MkdirAll(root, 0755); err != nil { return err }
	f, err := os.CreateTemp(root, ".diagnos-startup-*")
	if err != nil { return err }
	name := f.Name()
	if _, err := f.Write([]byte("ok")); err != nil { _ = f.Close(); _ = os.Remove(name); return err }
	if err := f.Sync(); err != nil { _ = f.Close(); _ = os.Remove(name); return err }
	if err := f.Close(); err != nil { _ = os.Remove(name); return err }
	return os.Remove(name)
}

func LatestDir(root string) (string, error) {
	b, err := os.ReadFile(filepath.Join(root, "latest.txt"))
	if err != nil { return "", err }
	id := strings.TrimSpace(string(b))
	if id == "" { return "", fmt.Errorf("latest report pointer is empty") }
	return filepath.Join(root, id), nil
}

func retain(root string, keep int) error {
	entries, err := os.ReadDir(root)
	if err != nil { return err }
	var ids []string
	for _, e := range entries {
		if e.IsDir() && strings.HasPrefix(e.Name(), "inv-") { ids = append(ids, e.Name()) }
	}
	sort.Strings(ids)
	if len(ids) <= keep { return nil }
	for _, id := range ids[:len(ids)-keep] { if err := os.RemoveAll(filepath.Join(root,id)); err != nil { return err } }
	return nil
}

func atomic(path string, b []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".diagnos-*")
	if err != nil { return err }
	tmp := f.Name()
	if _, err = f.Write(b); err != nil { _ = f.Close(); _ = os.Remove(tmp); return err }
	if err = f.Sync(); err != nil { _ = f.Close(); _ = os.Remove(tmp); return err }
	if err = f.Close(); err != nil { _ = os.Remove(tmp); return err }
	return os.Rename(tmp, path)
}

func RenderMarkdown(inv *contract.Investigation) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# vm-native-diagnos — %s\n\n", inv.Host)
	if len(inv.Hypotheses) > 0 { b.WriteString(inv.Hypotheses[0].Statement) } else { b.WriteString("No material anomaly was established.") }
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s · %d levels · budget: %s\n", formatDuration(inv.Duration), inv.Spent.Depth, budgetName(inv))
	for i, h := range inv.Hypotheses {
		fmt.Fprintf(&b, "\n%d. %s [%s] %.2f\n", i+1, h.Statement, h.Grade, h.Confidence)
		if detail := findingDetail(inv,h); detail != "" { fmt.Fprintf(&b, "   %s\n", detail) }
		if h.LogContext != nil {
			line := strings.ReplaceAll(h.LogContext.Line, "\"", "'")
			extra := ""; if h.LogContext.Count > 1 { extra = fmt.Sprintf(" (x %d)", h.LogContext.Count) }
			fmt.Fprintf(&b, "   log: %s — %q%s\n", h.LogContext.Path, line, extra)
		}
	}
	if len(inv.Hypotheses)>0 {
		b.WriteString("\n")
		renderChain(&b,inv,inv.Hypotheses[0].Entity)
		if cmds:=verifyForTopFinding(inv,inv.Hypotheses[0].Entity);len(cmds)>0 {
			b.WriteString("Verify\n");for _,cmd:=range cmds{fmt.Fprintf(&b,"   → %s\n",cmd)}
		}
	}
	if len(inv.NotInvestigated)>0 { b.WriteString("\nNot investigated\n");for _,c:=range inv.NotInvestigated{fmt.Fprintf(&b,"   %s(%s) — %s\n",c.Capability,c.Scope.ID,c.Reason)} }
	if len(inv.Limitations)>0 { b.WriteString("\nLimitations\n");for _,x:=range unique(inv.Limitations){fmt.Fprintf(&b,"   - %s\n",x)} }
	if len(inv.Notices)>0 { b.WriteString("\n## Notices\n\n");for _,n:=range inv.Notices{fmt.Fprintf(&b,"⚠ %s — %s",n.Capability,n.Message);if n.Count>1{fmt.Fprintf(&b," (x%d)",n.Count)};b.WriteString("\n")} }
	return b.String()
}

func findingDetail(inv *contract.Investigation,h contract.Hypothesis)string{
	var parts []string
	if h.Entity.Display!="" { parts=append(parts,h.Entity.Display) }
	for _,id:=range h.Support{
		for _,e:=range inv.Evidence{
			if e.ID!=id{continue}
			for _,o:=range e.Observations{
				if len(parts)>=3{break}
				switch o.Unit{
				case "percent":parts=append(parts,fmt.Sprintf("%.0f%% %s",o.Value,shortKey(o.Key)))
				case "ms":parts=append(parts,fmt.Sprintf("%.0fms %s",o.Value,shortKey(o.Key)))
				case "bytes_per_sec":parts=append(parts,fmt.Sprintf("%.1fMB/s %s",o.Value/1024/1024,shortKey(o.Key)))
				case "count","count_per_sec":parts=append(parts,fmt.Sprintf("%.0f %s",o.Value,shortKey(o.Key)))
				}
			}
		}
	}
	if len(parts)==0&&len(h.Support)>0{return "evidence: "+strings.Join(h.Support,", ")}
	return strings.Join(parts," · ")
}
func shortKey(k string)string{if i:=strings.LastIndex(k,":");i>=0{k=k[:i]};if i:=strings.LastIndex(k,".");i>=0{k=k[i+1:]};return strings.ReplaceAll(k,"_"," ")}
func renderChain(b *strings.Builder,inv *contract.Investigation,top contract.Entity){
	b.WriteString("machine");e:=top;seen:=map[string]bool{}
	for {
		key:=string(e.Kind)+"|"+e.ID;if seen[key]{break};seen[key]=true
		label:=e.Display;if label==""{label=e.ID}
		if e.Kind==contract.EntityProcess&&e.Service!=nil{label=e.Service.Name+" ("+e.ID+")";if e.Service.Container!=nil&&e.Service.Container.Name!=""{b.WriteString(" → container "+e.Service.Container.Name)}}
		b.WriteString(" → "+strings.ToLower(string(e.Kind))+" "+label)
		found:=false
		for _,x:=range inv.ObservedEntities{if x.ID==e.ParentID&&x.Kind!=contract.EntityMachine{e=x;found=true;break}}
		if !found{break}
	}
	b.WriteString("\n")
}
func verifyForTopFinding(inv *contract.Investigation,top contract.Entity)[]string{
	seen:=map[string]bool{};var out []string
	for _,e:=range inv.Evidence{if e.Entity.ID==top.ID||e.Entity.ID==top.ParentID{for _,v:=range e.Verify{if v!=""&&!seen[v]{seen[v]=true;out=append(out,v)}}}}
	sort.Strings(out);return out
}
func sameEntity(a,b contract.Entity)bool{return a.Kind==b.Kind&&a.ID==b.ID&&a.ParentID==b.ParentID}
func unique(in []string)[]string{m:=map[string]bool{};out:=[]string{};for _,x:=range in{if x!=""&&!m[x]{m[x]=true;out=append(out,x)}};return out}
func budgetName(inv *contract.Investigation)string{if inv.Budget.MaxDepth<=2{return "fast"};if inv.Budget.MaxDepth>=5{return "deep"};return "normal"}
func formatDuration(d time.Duration)string{if d<=0{return "0.0s"};return fmt.Sprintf("%.1fs",d.Seconds())}
_ = sameEntity
