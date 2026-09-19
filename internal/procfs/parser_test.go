package procfs

import (
	"bytes"
	"testing"
)

func TestParsers(t *testing.T) {
	tests := []struct {
		name string
		fn   func([]byte) error
		data []byte
	}{
		{"stat", func(b []byte) error { _, e := ParseStat(b); return e }, []byte("cpu 10 0 5 85\ncpu0 5 0 2 43\nctxt 1\nprocesses 2\nprocs_running 1\nprocs_blocked 0\nbtime 1\n")},
		{"meminfo", func(b []byte) error { _, e := ParseMemInfo(b); return e }, []byte("MemTotal: 1024 kB\nMemAvailable: 512 kB\n")},
		{"vmstat", func(b []byte) error { _, e := ParseVMStat(b); return e }, []byte("pgscan_kswapd 1\npgfault 2\n")},
		{"loadavg", func(b []byte) error { _, e := ParseLoadAvg(b); return e }, []byte("1.00 0.50 0.10 2/100 99\n")},
		{"pressure", func(b []byte) error { _, e := ParsePressure(b); return e }, []byte("some avg10=1 avg60=2 avg300=3 total=4\nfull avg10=0 avg60=0 avg300=0 total=0\n")},
		{"pidstat", func(b []byte) error { _, e := ParsePidStat(b); return e }, []byte("12 (my (weird) proc) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51 52\n")},
		{"pidstatus", func(b []byte) error { _, e := ParsePidStatus(b); return e }, []byte("Name: test\nPid: 12\nThreads: 2\n")},
		{"smaps", func(b []byte) error { _, e := ParseSmapsRollup(b); return e }, []byte("Rss: 10 kB\nPss: 8 kB\n")},
		{"io", func(b []byte) error { _, e := ParsePidIO(b); return e }, []byte("rchar: 1\nread_bytes: 2\n")},
		{"schedstat", func(b []byte) error { _, e := ParseSchedStat(b); return e }, []byte("1 2 3\n")},
		{"diskstats", func(b []byte) error { _, e := ParseDiskStats(b); return e }, []byte("8 0 sda 1 2 3 4 5 6 7 8 9 10 11\n")},
		{"netdev", func(b []byte) error { _, e := ParseNetDev(b); return e }, []byte("eth0: 1 2 3 4 5 6 7 8 9 10 11 12\n")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.fn(tc.data); e != nil {
				t.Fatal(e)
			}
		})
	}
}
func TestNoPanicOnGarbage(t *testing.T) {
	inputs := [][]byte{nil, {}, []byte("x"), []byte("cpu"), bytes.Repeat([]byte{0xff}, 64), []byte("1 (broken")}
	fns := []func([]byte) error{func(b []byte) error { _, e := ParseStat(b); return e }, func(b []byte) error { _, e := ParseMemInfo(b); return e }, func(b []byte) error { _, e := ParseVMStat(b); return e }, func(b []byte) error { _, e := ParseLoadAvg(b); return e }, func(b []byte) error { _, e := ParsePressure(b); return e }, func(b []byte) error { _, e := ParsePidStat(b); return e }, func(b []byte) error { _, e := ParsePidStatus(b); return e }, func(b []byte) error { _, e := ParseSmapsRollup(b); return e }, func(b []byte) error { _, e := ParsePidIO(b); return e }, func(b []byte) error { _, e := ParseSchedStat(b); return e }, func(b []byte) error { _, e := ParseDiskStats(b); return e }, func(b []byte) error { _, e := ParseNetDev(b); return e }, func(b []byte) error { _, e := ParseSocketTable(b); return e }}
	for i, b := range inputs {
		for j, fn := range fns {
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("panic input=%d parser=%d: %v", i, j, r)
					}
				}()
				_ = fn(b)
			}()
		}
	}
}
func BenchmarkParsePidStat(b *testing.B) {
	d := []byte("12 (my (weird) proc) S 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51 52\n")
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ParsePidStat(d)
	}
}

func TestPidStatOffsets(t *testing.T) {
	data := []byte("42 (demo) R 7 42 42 0 0 0 11 12 0 13 14 15 16 17 18 19 20 21 22 23 24 25 26 27 28 29 30 31 32 33 34 35 36 37 38 39 40 41 42 43 44 45 46 47 48 49 50 51 52 53 54 55 56 57\n")
	p, err := ParsePidStat(data)
	if err != nil {
		t.Fatal(err)
	}
	if p.PPID != 7 || p.Utime != 14 || p.Stime != 15 || p.NumThreads != 20 || p.Starttime != 22 || p.RSS != 24 {
		t.Fatalf("bad /proc stat offsets: %#v", p)
	}
}
