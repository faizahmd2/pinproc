package source

import (
	"context"
	"time"

	"github.com/faizahmd2/vm-native-diagnos/internal/contract"
)

// ReadKind describes how a source resolves a read request.
type ReadKind int

const (
	ReadFile ReadKind = iota
	ReadGlob
	ReadDirNames
	ReadLink
	ReadGlobLinks
)

// Read describes a bounded source read.
type Read struct {
	Key      string
	Path     string
	Kind     ReadKind
	MaxBytes int
	Optional bool
}

// Raw is one concrete read result.
type Raw struct {
	Key  string
	Path string
	Data []byte
	Err  error
}

// Snapshot is one timestamped set of raw reads.
type Snapshot struct {
	At    time.Time
	Reads map[string][]Raw
	Bytes int64
}

// Sample contains two snapshots separated by Window.
type Sample struct {
	T0, T1 Snapshot
	Window time.Duration
}

// Source is the sole evidence collection abstraction.
type Source interface {
	Name() string
	Facts(context.Context) (contract.Facts, error)
	Snapshot(context.Context, []Read) (Snapshot, error)
	Sample(context.Context, []Read, time.Duration) (Sample, error)
	Close() error
}
