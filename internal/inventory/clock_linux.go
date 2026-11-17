//go:build linux

package inventory

import (
	"fmt"

	"golang.org/x/sys/unix"
)

// checkClock asks the kernel whether NTP has the clock synchronised — the same
// adjtimex(2) status timedatectl reports as "System clock synchronized".
func checkClock() Check {
	ch := Check{ID: "clock", Area: "time", Name: "Clock synchronised (NTP)", Cmd: "timedatectl"}
	var tx unix.Timex
	state, err := unix.Adjtimex(&tx)
	if err != nil {
		return Check{}
	}
	if state == unix.TIME_ERROR || tx.Status&unix.STA_UNSYNC != 0 {
		ch.State, ch.Value = "warn", "not synchronised"
		ch.Hint = "timestamps in logs and TLS validity drift; enable systemd-timesyncd or chrony"
		return ch
	}
	ch.State = "ok"
	ch.Value = fmt.Sprintf("yes · max error %.1f ms", float64(tx.Maxerror)/1000)
	return ch
}
