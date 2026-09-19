package runtime

import (
	"log/slog"
	rt "runtime"
	"runtime/debug"
)

// Local applies bounded-resource settings for local diagnosis.
func Local(log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	debug.SetMemoryLimit(64 << 20)
	debug.SetGCPercent(40)
	n := rt.NumCPU()
	if n > 2 {
		n = 2
	}
	if n < 1 {
		n = 1
	}
	rt.GOMAXPROCS(n)
	log.Debug("diagnos self limits applied", "gomaxprocs", n)
}
