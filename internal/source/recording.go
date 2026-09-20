package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
)

type recordingRaw struct {
	Key string
	Path string
	Data []byte
	Err string
}
type recordingSnapshot struct {
	At time.Time
	Reads map[string][]recordingRaw
	Bytes int64
}
type recordingEvent struct {
	Kind string
	Snapshot *recordingSnapshot
	Sample *struct{T0, T1 recordingSnapshot; Window time.Duration}
	Facts *contract.Facts
	Err string
}
type Recording struct {
	Events []recordingEvent
}

type RecordingSource struct {
	Inner Source
	Rec *Recording
}

func NewRecording(inner Source) *RecordingSource { return &RecordingSource{Inner:inner,Rec:&Recording{}} }

func (s *RecordingSource) Name() string { return "recording("+s.Inner.Name()+")" }
func (s *RecordingSource) Facts(ctx context.Context)(contract.Facts,error){
	f,err:=s.Inner.Facts(ctx)
	s.Rec.Events=append(s.Rec.Events,recordingEvent{Kind:"facts",Facts:&f,Err:errString(err)})
	return f,err
}
func (s *RecordingSource) Snapshot(ctx context.Context, reads []Read)(Snapshot,error){
	x,err:=s.Inner.Snapshot(ctx,reads)
	s.Rec.Events=append(s.Rec.Events,recordingEvent{Kind:"snapshot",Snapshot:packSnapshot(x),Err:errString(err)})
	return x,err
}
func (s *RecordingSource) Sample(ctx context.Context, reads []Read, w time.Duration)(Sample,error){
	x,err:=s.Inner.Sample(ctx,reads,w)
	y:=&struct{T0,T1 recordingSnapshot;Window time.Duration}{T0:*packSnapshot(x.T0),T1:*packSnapshot(x.T1),Window:x.Window}
	s.Rec.Events=append(s.Rec.Events,recordingEvent{Kind:"sample",Sample:y,Err:errString(err)})
	return x,err
}
func (s *RecordingSource) Close()error{return s.Inner.Close()}
func (s *RecordingSource) Save(path string)error{
	if err:=os.MkdirAll(filepathDir(path),0755);err!=nil{return err}
	b,err:=json.MarshalIndent(s.Rec,"","  ");if err!=nil{return err}
	return os.WriteFile(path,append(b,'\n'),0644)
}
func LoadRecording(path string)(*Recording,error){
	b,err:=os.ReadFile(path);if err!=nil{return nil,err};var r Recording
	if err:=json.Unmarshal(b,&r);err!=nil{return nil,err};return &r,nil
}
func errString(err error)string{if err==nil{return ""};return err.Error()}
func packSnapshot(s Snapshot)*recordingSnapshot{r:=&recordingSnapshot{At:s.At,Reads:map[string][]recordingRaw{},Bytes:s.Bytes};for k,rows:=range s.Reads{for _,x:=range rows{r.Reads[k]=append(r.Reads[k],recordingRaw{Key:x.Key,Path:x.Path,Data:x.Data,Err:errString(x.Err)})}};return r}
func filepathDir(path string)string{for i:=len(path)-1;i>=0;i--{if path[i]=='/'{if i==0{return "/"};return path[:i]}};return "."}
var _ = errors.New
