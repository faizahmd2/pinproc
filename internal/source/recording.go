package source

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
)

type RecordingSource struct {
	Inner Source
	Dir string
	mu sync.Mutex
	seq int
}

func NewRecording(inner Source, dir string) *RecordingSource {
	return &RecordingSource{Inner:inner,Dir:dir}
}
func (s *RecordingSource) Name() string { return "recording("+s.Inner.Name()+")" }
func (s *RecordingSource) Facts(ctx context.Context) (contract.Facts,error) {
	f,err:=s.Inner.Facts(ctx)
	if err==nil {
		_ = os.MkdirAll(s.Dir,0755)
		if b,e:=json.MarshalIndent(f,"","  ");e==nil{_ = os.WriteFile(filepath.Join(s.Dir,"meta.json"),append(b,'\n'),0644)}
	}
	return f,err
}
func (s *RecordingSource) Snapshot(ctx context.Context, reads []Read) (Snapshot,error) {
	x,err:=s.Inner.Snapshot(ctx,reads)
	if e:=s.saveSnapshot(x);err==nil&&e!=nil{err=e}
	return x,err
}
func (s *RecordingSource) Sample(ctx context.Context, reads []Read, w time.Duration) (Sample,error) {
	x,err:=s.Inner.Sample(ctx,reads,w)
	if e:=s.saveSnapshot(x.T0);err==nil&&e!=nil{err=e}
	if e:=s.saveSnapshot(x.T1);err==nil&&e!=nil{err=e}
	return x,err
}
func (s *RecordingSource) Close() error { return s.Inner.Close() }

func (s *RecordingSource) saveSnapshot(snap Snapshot) error {
	s.mu.Lock(); defer s.mu.Unlock()
	root:=filepath.Join(s.Dir,fmt.Sprintf("snapshot-%03d",s.seq));s.seq++
	if err:=os.MkdirAll(filepath.Join(root,"data"),0755);err!=nil{return err}
	type item struct{Key string `json:"key"`;Path string `json:"path"`;Error string `json:"error,omitempty"`}
	idx:=struct{At time.Time `json:"at"`;Bytes int64 `json:"bytes"`;Reads []item `json:"reads"`}{At:snap.At,Bytes:snap.Bytes}
	for _,rows:=range snap.Reads{
		for _,raw:=range rows{
			idx.Reads=append(idx.Reads,item{Key:raw.Key,Path:raw.Path,Error:errorString(raw.Err)})
			rel:=strings.TrimPrefix(filepath.Clean(raw.Path),string(filepath.Separator))
			path:=filepath.Join(root,"data",rel)
			if raw.Path!=""{if err:=os.MkdirAll(filepath.Dir(path),0755);err==nil{_ = os.WriteFile(path,raw.Data,0644)}}
		}
	}
	b,err:=json.MarshalIndent(idx,"","  ");if err!=nil{return err}
	return os.WriteFile(filepath.Join(root,"index.json"),append(b,'\n'),0644)
}
func errorString(err error)string{if err==nil{return ""};return err.Error()}
