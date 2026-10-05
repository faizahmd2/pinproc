# pinproc trigger architecture (kernel-driven, zero-egress)

> Status: design + incremental build. Branch `feat/psi-trigger-daemon`.
> Scope: all Linux hosts (amd64/arm64, older→newer kernels), not just the dev VM.

## Why

Today pinproc runs an always-on HTTP server on `127.0.0.1:8080` and waits to be
called (`GET /investigate`). For a read-only "doctor" that should mostly sleep,
an open port + API key + the temptation of callbacks is more surface and more
"always-waiting daemon" than the job needs.

Goal: **the kernel wakes pinproc when a resource is actually in trouble**, it
captures that incident, and delivers a report the *owning service's developer*
can understand. No listening socket. No API key. **Zero egress by default** — the
only thing that ever leaves the box is a finished report, only to a sink the
operator configured, only on a real incident.

## The model

pinproc is three parts; we are replacing only the first:

1. **Trigger** — kernel push (PSI / cgroup events), not HTTP pull.
2. **Collect + attribute** — the existing `internal/engine` (owner attribution).
3. **Deliver** — local write always; optional single push to a configured sink.

On-demand stays available as `pinproc report`, which runs an investigation
**in-process** (no socket, no network).

## Core concept: utilization ≠ pressure

- **Utilization %** — how full a resource is (CPU%, mem used%, disk used%). A box
  at 80% with nobody waiting is healthy. This is the early-warning/capacity signal.
- **PSI (Pressure Stall Information)** — the time tasks *lost waiting* for a
  resource. Only rises under real contention. This is the "it already hurts" signal.

We arm on whichever crosses first and confirm with the fuller picture. A pure-PSI
trigger would miss an 80% CPU box with no run-queue; a pure-utilization trigger
would cry wolf on healthy busy work.

## Decision: read-only pull (push was evaluated and rejected)

We "let the kernel tell us" by **reading** the kernel's pressure/utilization
numbers on a cheap adaptive timer — not by arming kernel push triggers. Why:

- PSI *push* triggers (`poll()` on a pressure file) were tested on the VM and work
  on cgroup v2 `*.pressure` files, but **arming one is a write to a root-owned
  cgroupfs file** (needs root / `CAP_DAC_OVERRIDE`, with `ProtectControlGroups`
  relaxed). A read-only third-party package must not take write authority over
  cgroupfs — it breaks pinproc's entire reason for being.
- `/proc/pressure/*` push triggers are additionally **rejected with EINVAL even
  for root** on modern Ubuntu (6.8), so push isn't even portable.

Pull reads the *same* kernel numbers and stays **pure read** (only
`CAP_DAC_READ_SEARCH`), works everywhere (cgroup v1/v2, containers, old kernels),
and — measured on the VM — costs **~7µs per calm tick (~63 ms CPU/day at 10 s
cadence)**, far less than a cron that forks a process each tick. For incidents that
build over seconds/minutes, a 2–5 s reaction is plenty. The PSI push code has been
removed from the tree.

## Two-stage trigger (arm → confirm → act)

```
IDLE ──(level ≥ T1)──► WATCHING ──► CAPTURE ──► cooldown ──► IDLE
                          │
                          └──(recovers < T1)──► DISCARD ──► IDLE

T1  = warn level (e.g. CPU util 80% or mem PSI some avg10 > x)
WATCHING: collect cheap "dots" for up to ~10 min
CAPTURE when:  level ≥ T2 (e.g. 90%)   OR
               level sustained ≥ T1 for N minutes (plateau)  OR
               clear upward slope toward T2
DISCARD when:  level falls < T1 and stays (drop the dots, no report)
```

Escalate on **rising OR sustained**, never rising-only — a flat 85% starving box
is still an incident.

## Dots — the pre-incident ring buffer

An in-memory fixed-size circular buffer (~120 slots), no file/DB/dependency. Only
the **armed dimension** is sampled; each dot is a few hundred bytes:

| dim | dot fields |
|-----|------------|
| cpu | ts, util%, load1, runq_len, top3 {pid,comm,cpu%} |
| mem | ts, used%, swap%, psi_some, top3 {pid,comm,rss} |
| io  | ts, util%, await_ms, top3 {pid,comm,io_bps} |
| net | ts, rx, tx, retrans, top_ports {port,conns} |

~10 min @ 5s ≈ tens of KB RAM, continuously overwritten. Nothing hits disk until
CAPTURE. On DISCARD the ring is reset (zero residue). On CAPTURE the ring becomes
the report **timeline** (how it built up + who was climbing) plus one deep
snapshot of the culprit. Cadence is adaptive: slow (~10s) while low, fast (~2–3s)
once armed.

## Signals we read (pull)

| signal | source | covers |
|--------|--------|--------|
| PSI `some avg10` | `/proc/pressure/{cpu,memory,io}` (read) | cpu, mem, io pressure |
| CPU utilization | `/proc/stat` delta between ticks | cpu |
| mem/swap used % | `/proc/meminfo` | memory |
| load 1/5/15 | `/proc/loadavg` | cpu trend |
| per-process contenders | `/proc/[pid]/{stat,statm,io}` — **only when armed** | who owns the dimension |

Read-only throughout (`CAP_DAC_READ_SEARCH`). Deliberately **not** used: kernel
push triggers (write), eBPF (root/CAP_BPF). Network has no PSI → when armed we also
sample `/proc/net/dev`/`snmp`; link capacity is often unknowable on VMs, so we show
rate + retransmits rather than a fake percentage.

## Portability (because "all Linux")

The pull sampler reads whatever is present and degrades per field: PSI missing →
fall back to utilization/load; meminfo/loadavg always present. cgroup v1/v2,
containers and old kernels all work because we only read. `pinproc doctor` reports
which signals are available on the host.

## Edge cases we must handle (not discover)

- **Reaction race** — the culprit may fade/exit by the time we wake → PSI fires on
  *sustained* pressure, we grab **pidfds** immediately, and the pre-incident dots
  preserve "what was climbing."
- **Trigger storms / flapping** → debounce, cooldown, hysteresis; one report per
  incident, quiet until recovery or clear escalation.
- **Don't worsen the incident** → run nice/ionice, cap pinproc in its own cgroup,
  bounded reads (already).
- **Thresholds** → conservative defaults, overridable; `doctor` shows them.
- **Offline / air-gapped** → local-first, bounded rotation, best-effort push.
- **Containers / read-only rootfs / no systemd** → degrade and say so.

## AI, when enabled: classifier → incident analyst

Because the trigger already knows the dimension, contenders, trend and owning
cgroup, the model is called **only on a breach** with a focused prompt ("root
cause: leak vs spike vs fork-storm; safest *application-level* remediation"),
trend-aware across recent captures. Still out-of-process, redacted, opt-in. Egress
stays rare and purposeful.

## Build order

1. `docs/trigger-architecture.md` (this file).
2. `internal/pressure` — read-only calm-path aggregate reader (done, measured).
3. `internal/dots` — in-memory ring of incident samples; effective discard.
4. `internal/contenders` — light per-dimension top-N process sampler (armed only).
5. `internal/monitor` — the state machine (arm → watch → capture/discard) with
   adaptive cadence and cooldown; configurable thresholds.
6. wire CAPTURE to the existing engine; fold the dots in as the report timeline.
7. `service run` runs the monitor (no socket); `pinproc report` runs in-process;
   `doctor` reports available signals.

## Default thresholds (configurable)

| dim | arm (T1) | capture (T2) | sustain | calm | armed | cooldown |
|-----|----------|--------------|---------|------|-------|----------|
| cpu | util 80% or PSI>20 | 90% or PSI>40 | ≥60s | 10s | 2s | 2m |
| mem | used 85% or PSI>10 | 95% or PSI>30 | ≥60s | 10s | 2s | 2m |
| io  | PSI>30 | PSI>60 | ≥30s | 10s | 2s | 2m |

Each step tiny, tested, VM-verified. The report must read as app-level cause +
remediation, understandable by the owner of the affected service.
