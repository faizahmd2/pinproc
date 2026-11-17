package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/faizahmd2/pinproc/internal/inventory"
)

func snap() *inventory.Snapshot {
	return &inventory.Snapshot{
		CollectedAt: time.Now(),
		Host:        inventory.Host{Hostname: "node-1", PrimaryIP: "10.0.0.5", OS: "Ubuntu 24.04 LTS", Kernel: "6.8.0", Arch: "x86_64"},
		CPU:         inventory.CPU{Cores: 4, BusyPct: 40, UserPct: 30, SystemPct: 10},
		Memory: inventory.Memory{TotalBytes: 8 << 30, AccountedBytes: 8 << 30, UsedBytes: 4 << 30, UsedPct: 50,
			Buckets: []inventory.Bucket{{Key: "apps", Label: "Applications", Value: 3 << 30}, {Key: "free", Label: "Free", Value: 5 << 30}}},
		Disks:     []inventory.Disk{{Name: "sda", SizeBytes: 100 << 30, Mounts: []inventory.MountUse{{Path: "/", UsedBytes: 40 << 30, AvailBytes: 60 << 30}}}},
		Services:  []inventory.Service{{Name: "nginx", Unit: "nginx.service", Kind: "systemd", Procs: 3, MemBytes: 200 << 20, CPUCores: 0.4}},
		Processes: []inventory.Process{{PID: 10, User: "www", Comm: "nginx", Exe: "/usr/sbin/nginx", Cmdline: "nginx: worker", CPUCores: 0.4, RSSBytes: 70 << 20}, {PID: 2, Comm: "kthreadd"}},
	}
}

func TestFrameFitsAndShowsEverything(t *testing.T) {
	for _, w := range []int{40, 80, 160} {
		out := Frame(View{Snap: snap(), Width: w, Height: 30, Sort: SortMem})
		for _, want := range []string{"node-1", "CPU", "MEM", "DISK", "nginx.service", "/usr/sbin/nginx", "sort: mem"} {
			if !strings.Contains(out, want) {
				t.Errorf("width %d: missing %q", w, want)
			}
		}
	}
}

func TestKeysQuitAndSort(t *testing.T) {
	m := &model{interval: 2 * time.Second, sort: SortCPU}
	if _, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")}); cmd != nil || m.sort != SortMem {
		t.Fatalf("sort key: sort=%c cmd=%v", m.sort, cmd)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("+")})
	if m.interval != 4*time.Second {
		t.Errorf("interval = %s", m.interval)
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("q")})
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q did not quit")
	}
}
