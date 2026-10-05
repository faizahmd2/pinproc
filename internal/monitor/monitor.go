// Package monitor is the read-only, self-triggering state machine: it samples the
// cheap aggregate pressure on an adaptive timer, arms a dimension when it crosses
// a warn threshold, records lightweight "dots" while watching, and fires a capture
// only when the incident is confirmed (crosses the capture threshold or stays
// elevated) — otherwise it discards the dots and leaves no trace. There is no
// socket and no egress; a capture is handed to a CaptureSink.
package monitor

import (
	"context"
	"time"

	"github.com/faizahmd2/pinproc/internal/contract"
	"github.com/faizahmd2/pinproc/internal/dots"
	"github.com/faizahmd2/pinproc/internal/pressure"
)

// DimThreshold configures arming/capture for one dimension. A 0 utilization bound
// disables the utilization check for that dimension (e.g. io is PSI-only).
type DimThreshold struct {
	ArmUtil float64       `yaml:"arm_util"`
	ArmPSI  float64       `yaml:"arm_psi"`
	CapUtil float64       `yaml:"cap_util"`
	CapPSI  float64       `yaml:"cap_psi"`
	Sustain time.Duration `yaml:"sustain"`
}

// NetThreshold configures network arming/capture. Network has several independent
// failure modes (none a single utilization %), so each is its own bound; a 0 bound
// disables that check. Bandwidth is deliberately not a trigger (no reliable NIC
// capacity on cloud hosts) — it is context only.
type NetThreshold struct {
	RetransArm   float64       `yaml:"retrans_arm"` // TCP retransmits/s
	RetransCap   float64       `yaml:"retrans_cap"`
	ConntrackArm float64       `yaml:"conntrack_arm"` // % of nf_conntrack_max
	ConntrackCap float64       `yaml:"conntrack_cap"`
	TimeWaitArm  uint64        `yaml:"time_wait_arm"` // TIME_WAIT sockets
	TimeWaitCap  uint64        `yaml:"time_wait_cap"`
	OrphanArm    uint64        `yaml:"orphan_arm"`
	OrphanCap    uint64        `yaml:"orphan_cap"`
	Sustain      time.Duration `yaml:"sustain"`
}

// Config is the tunable policy. Zero values fall back to DefaultConfig via Normalize.
type Config struct {
	CPU          DimThreshold  `yaml:"cpu"`
	Mem          DimThreshold  `yaml:"mem"`
	IO           DimThreshold  `yaml:"io"`
	Net          NetThreshold  `yaml:"net"`
	CalmCadence  time.Duration `yaml:"calm_cadence"`
	ArmedCadence time.Duration `yaml:"armed_cadence"`
	Cooldown     time.Duration `yaml:"cooldown"`
	WatchWindow  time.Duration `yaml:"watch_window"`
	RingCapacity int           `yaml:"ring_capacity"`
	TopN         int           `yaml:"top_n"`
	// Priority biases which dimension is chosen when several are elevated at once.
	// CPU and memory are the resources that matter most in practice, so they are
	// weighted higher — a real CPU/RAM problem wins over a transient io/net spike,
	// while io/net can still win when they clearly dominate and CPU/RAM are fine.
	Priority DimWeights `yaml:"priority"`
}

// DimWeights weights the arm-selection score per dimension.
type DimWeights struct {
	CPU float64 `yaml:"cpu"`
	Mem float64 `yaml:"mem"`
	IO  float64 `yaml:"io"`
	Net float64 `yaml:"net"`
}

// DefaultConfig returns the conservative defaults (the agreed table).
func DefaultConfig() Config {
	return Config{
		CPU:          DimThreshold{ArmUtil: 80, ArmPSI: 20, CapUtil: 90, CapPSI: 40, Sustain: 60 * time.Second},
		Mem:          DimThreshold{ArmUtil: 85, ArmPSI: 10, CapUtil: 95, CapPSI: 30, Sustain: 60 * time.Second},
		IO:           DimThreshold{ArmUtil: 80, ArmPSI: 30, CapUtil: 95, CapPSI: 60, Sustain: 30 * time.Second},
		Net:          NetThreshold{RetransArm: 50, RetransCap: 200, ConntrackArm: 80, ConntrackCap: 95, TimeWaitArm: 20000, TimeWaitCap: 40000, OrphanArm: 1000, OrphanCap: 2000, Sustain: 30 * time.Second},
		CalmCadence:  10 * time.Second,
		ArmedCadence: 2 * time.Second,
		Cooldown:     2 * time.Minute,
		WatchWindow:  10 * time.Minute,
		TopN:         3,
		Priority:     DimWeights{CPU: 1.0, Mem: 1.0, IO: 0.5, Net: 0.5},
	}
}

// Normalize fills any unset fields from the defaults and derives ring capacity.
func (c *Config) Normalize() {
	d := DefaultConfig()
	if c.CalmCadence <= 0 {
		c.CalmCadence = d.CalmCadence
	}
	if c.ArmedCadence <= 0 {
		c.ArmedCadence = d.ArmedCadence
	}
	if c.Cooldown <= 0 {
		c.Cooldown = d.Cooldown
	}
	if c.WatchWindow <= 0 {
		c.WatchWindow = d.WatchWindow
	}
	if c.TopN <= 0 {
		c.TopN = d.TopN
	}
	if (c.CPU == DimThreshold{}) {
		c.CPU = d.CPU
	}
	if (c.Mem == DimThreshold{}) {
		c.Mem = d.Mem
	}
	if (c.IO == DimThreshold{}) {
		c.IO = d.IO
	}
	if (c.Net == NetThreshold{}) {
		c.Net = d.Net
	}
	if (c.Priority == DimWeights{}) {
		c.Priority = d.Priority
	}
	if c.RingCapacity <= 0 {
		c.RingCapacity = int(c.WatchWindow/c.ArmedCadence) + 4
	}
}

func (c Config) threshold(dim contract.Dimension) DimThreshold {
	switch dim {
	case contract.DimensionMemory:
		return c.Mem
	case contract.DimensionIO:
		return c.IO
	default:
		return c.CPU
	}
}

// Capture is a confirmed incident handed to the sink.
type Capture struct {
	Dimension contract.Dimension
	StartedAt time.Time
	FiredAt   time.Time
	Reason    string
	Dots      []dots.Dot
}

// CaptureSink receives confirmed incidents (writes the report, optional push).
type CaptureSink interface {
	Capture(context.Context, Capture)
}

// LevelSource yields the cheap aggregate levels (pressure.Reader implements it).
type LevelSource interface{ Calm() pressure.Levels }

// ContenderSource yields top contenders for a dimension (contenders.Sampler).
type ContenderSource interface {
	Top(contract.Dimension, int, time.Time) []dots.Contender
	Reset()
}

type phase int

const (
	idle phase = iota
	watching
)

type action int

const (
	actCalm action = iota // stay idle
	actArm
	actWatch
	actCapture
	actDiscard
)

// Monitor runs the state machine. Construct with New, then Run.
type Monitor struct {
	cfg     Config
	levels  LevelSource
	cont    ContenderSource
	ring    *dots.Ring
	sink    CaptureSink
	clock   func() time.Time
	onEvent func(action, contract.Dimension, string) // optional hook for tests/logs

	phase       phase
	armedDim    contract.Dimension
	armedAt     time.Time
	lastCapture map[contract.Dimension]time.Time
}

// New builds a Monitor. cfg is normalized; clock defaults to time.Now.
func New(cfg Config, levels LevelSource, cont ContenderSource, sink CaptureSink) *Monitor {
	cfg.Normalize()
	return &Monitor{
		cfg:         cfg,
		levels:      levels,
		cont:        cont,
		ring:        dots.NewRing(cfg.RingCapacity),
		sink:        sink,
		clock:       time.Now,
		lastCapture: map[contract.Dimension]time.Time{},
	}
}

// Run drives the loop until ctx is cancelled, sleeping calm-cadence while idle and
// armed-cadence while watching.
func (m *Monitor) Run(ctx context.Context) error {
	for {
		m.tick(ctx)
		cadence := m.cfg.CalmCadence
		if m.phase == watching {
			cadence = m.cfg.ArmedCadence
		}
		t := time.NewTimer(cadence)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
	}
}

// tick performs one evaluation and its side effects.
func (m *Monitor) tick(ctx context.Context) {
	now := m.clock()
	l := m.levels.Calm()

	if m.phase == watching {
		// Always record this moment on the timeline before deciding.
		m.addDot(now, l)
	}

	act, dim, reason := m.decide(now, l)
	if m.onEvent != nil {
		m.onEvent(act, dim, reason)
	}
	switch act {
	case actArm:
		m.ring.Arm(dim)
		m.cont.Reset()
		m.addDot(now, l) // first (baseline) dot
	case actCapture:
		cap := Capture{Dimension: dim, StartedAt: m.armedAt, FiredAt: now, Reason: reason, Dots: m.ring.Snapshot()}
		if m.sink != nil {
			m.sink.Capture(ctx, cap)
		}
		m.ring.Reset()
		m.cont.Reset()
	case actDiscard:
		m.ring.Reset()
		m.cont.Reset()
	}
}

// decide is the pure state transition: it reads current levels and updates phase,
// returning the action to take. Separated from side effects so it is unit-testable.
func (m *Monitor) decide(now time.Time, l pressure.Levels) (action, contract.Dimension, string) {
	if m.phase == idle {
		dim, ok := m.pickArm(now, l)
		if !ok {
			return actCalm, "", ""
		}
		m.phase = watching
		m.armedDim = dim
		m.armedAt = now
		return actArm, dim, "armed"
	}
	dim := m.armedDim
	if m.captured(dim, l) {
		m.phase = idle
		m.lastCapture[dim] = now
		return actCapture, dim, "crossed capture threshold"
	}
	if s := m.sustainOf(dim); s > 0 && now.Sub(m.armedAt) >= s && m.armed(dim, l) {
		m.phase = idle
		m.lastCapture[dim] = now
		return actCapture, dim, "sustained elevation"
	}
	if !m.armed(dim, l) {
		m.phase = idle
		return actDiscard, dim, "recovered"
	}
	if now.Sub(m.armedAt) > m.cfg.WatchWindow {
		m.phase = idle
		return actDiscard, dim, "watch window elapsed"
	}
	return actWatch, dim, ""
}

// armed/captured/sustainOf/score dispatch per dimension so network (which has no
// single utilization metric) can use its own signals while cpu/mem/io use (util,psi).
func (m *Monitor) armed(dim contract.Dimension, l pressure.Levels) bool {
	if dim == contract.DimensionNetwork {
		return netArmed(m.cfg.Net, l)
	}
	th := m.cfg.threshold(dim)
	lvl, psi := dimValues(l, dim)
	return crossed(lvl, th.ArmUtil) || crossed(psi, th.ArmPSI)
}

func (m *Monitor) captured(dim contract.Dimension, l pressure.Levels) bool {
	if dim == contract.DimensionNetwork {
		return netCaptured(m.cfg.Net, l)
	}
	th := m.cfg.threshold(dim)
	lvl, psi := dimValues(l, dim)
	return crossed(lvl, th.CapUtil) || crossed(psi, th.CapPSI)
}

func (m *Monitor) sustainOf(dim contract.Dimension) time.Duration {
	if dim == contract.DimensionNetwork {
		return m.cfg.Net.Sustain
	}
	return m.cfg.threshold(dim).Sustain
}

func (m *Monitor) score(dim contract.Dimension, l pressure.Levels) float64 {
	var raw float64
	if dim == contract.DimensionNetwork {
		raw = netScore(m.cfg.Net, l)
	} else {
		th := m.cfg.threshold(dim)
		lvl, psi := dimValues(l, dim)
		raw = exceedance(lvl, th.ArmUtil) + exceedance(psi, th.ArmPSI)
	}
	return raw * m.weight(dim)
}

// weight biases selection toward the resources that matter most in practice.
func (m *Monitor) weight(dim contract.Dimension) float64 {
	var w float64
	switch dim {
	case contract.DimensionMemory:
		w = m.cfg.Priority.Mem
	case contract.DimensionIO:
		w = m.cfg.Priority.IO
	case contract.DimensionNetwork:
		w = m.cfg.Priority.Net
	default:
		w = m.cfg.Priority.CPU
	}
	if w <= 0 {
		w = 1
	}
	return w
}

func netArmed(th NetThreshold, l pressure.Levels) bool {
	return crossed(l.TCPRetransPerSec, th.RetransArm) ||
		crossed(l.ConntrackPct, th.ConntrackArm) ||
		crossedU(l.TCPTimeWait, th.TimeWaitArm) ||
		crossedU(l.TCPOrphan, th.OrphanArm)
}

func netCaptured(th NetThreshold, l pressure.Levels) bool {
	return crossed(l.TCPRetransPerSec, th.RetransCap) ||
		crossed(l.ConntrackPct, th.ConntrackCap) ||
		crossedU(l.TCPTimeWait, th.TimeWaitCap) ||
		crossedU(l.TCPOrphan, th.OrphanCap)
}

func netScore(th NetThreshold, l pressure.Levels) float64 {
	return exceedance(l.TCPRetransPerSec, th.RetransArm) +
		exceedance(l.ConntrackPct, th.ConntrackArm) +
		exceedance(float64(l.TCPTimeWait), float64(th.TimeWaitArm)) +
		exceedance(float64(l.TCPOrphan), float64(th.OrphanArm))
}

// pickArm returns the dimension that most exceeds its warn threshold, honouring
// per-dimension cooldown.
func (m *Monitor) pickArm(now time.Time, l pressure.Levels) (contract.Dimension, bool) {
	best := contract.Dimension("")
	bestScore := 0.0
	for _, dim := range []contract.Dimension{contract.DimensionCPU, contract.DimensionMemory, contract.DimensionIO, contract.DimensionNetwork} {
		if last, ok := m.lastCapture[dim]; ok && now.Sub(last) < m.cfg.Cooldown {
			continue
		}
		if !m.armed(dim, l) {
			continue
		}
		if s := m.score(dim, l); s > bestScore {
			bestScore = s
			best = dim
		}
	}
	return best, best != ""
}

func (m *Monitor) addDot(now time.Time, l pressure.Levels) {
	dim := m.armedDim
	lvl, psi := dimValues(l, dim)
	m.ring.Add(dots.Dot{
		At:    now,
		Level: lvl,
		PSI:   psi,
		Top:   m.cont.Top(dim, m.cfg.TopN, now),
		Ctx: dots.Context{
			CPUUtil: l.CPUUtilPct, CPUPSI: l.CPUStallPct,
			MemUsed: l.MemUsedPct, MemPSI: l.MemStallPct,
			IOPSI:    l.IOStallPct,
			NetRxBps: l.NetRxBps, NetTxBps: l.NetTxBps,
			Retrans: l.TCPRetransPerSec, Conntrack: l.ConntrackPct, TimeWait: l.TCPTimeWait,
		},
	})
}

// dimValues returns the headline (level, pressure) pair for a dimension, used for
// the dot's armed-dimension display. Network's detail lives in the dot context.
func dimValues(l pressure.Levels, dim contract.Dimension) (lvl, psi float64) {
	switch dim {
	case contract.DimensionMemory:
		return l.MemUsedPct, l.MemStallPct
	case contract.DimensionIO:
		return l.IOUtilPct, l.IOStallPct
	case contract.DimensionNetwork:
		return l.ConntrackPct, l.TCPRetransPerSec
	default:
		return l.CPUUtilPct, l.CPUStallPct
	}
}

// crossed reports value >= bound when bound is enabled (>0).
func crossed(value, bound float64) bool { return bound > 0 && value >= bound }

// crossedU is crossed for unsigned counts.
func crossedU(value, bound uint64) bool { return bound > 0 && value >= bound }

func exceedance(value, bound float64) float64 {
	if bound <= 0 || value < bound {
		return 0
	}
	return (value - bound) / bound
}
