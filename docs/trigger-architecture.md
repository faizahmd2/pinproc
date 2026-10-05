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

## Key finding (VM-verified) — push vs pull, the decision that shapes everything

Proven on Ubuntu 24.04 / kernel 6.8 (arm64):

- `/proc/pressure/*` **triggers are rejected with EINVAL even for root** on this
  kernel. System-wide `/proc` PSI triggers are effectively off on modern Ubuntu.
- **cgroup v2 `*.pressure` triggers work.** Arming the root cgroup's
  `/sys/fs/cgroup/{cpu,memory,io}.pressure` = whole-machine; a leaf = per-service.
- A CPU trigger **fired ~2 s after pressure began, with the process blocked at 0%
  CPU and no socket open** — the push model is real and cheap.
- **But arming a trigger is a *write* to a root-owned cgroupfs file.** It needs
  root or `CAP_DAC_OVERRIDE`, and `/sys/fs/cgroup` must be writable to the service
  (i.e. `ProtectControlGroups` relaxed). pinproc today is read-only with only
  `CAP_DAC_READ_SEARCH` and cannot write it.

So there are two ways to "let the kernel tell us," and this is the fork:

| | **Push (PSI triggers)** | **Pull (read-only sampler)** |
|---|---|---|
| mechanism | arm cgroup trigger, epoll, kernel wakes us | timerfd wakes us every 2–5 s to read PSI/util |
| idle cost | ~0 wakeups | tiny (a few file reads every few seconds) |
| reaction | sub-second | 2–5 s |
| privilege | **needs cgroupfs write** (root / CAP_DAC_OVERRIDE, ProtectControlGroups off) | **pure read** — fits current minimal, read-only posture |
| portability | cgroup v2 only; /proc triggers off on modern Ubuntu; varies by distro | works everywhere PSI/util files are readable (v1/v2, containers, old kernels) |

Both read the *same* kernel pressure numbers; push is notified, pull polls cheaply.
For incidents that build over seconds/minutes, a 2–5 s pull reaction is plenty.

**Recommendation:** make **pull the default** (preserves pinproc's zero-write,
minimal-privilege, maximally-portable identity), and offer **push as an optional
low-latency mode** only where the operator accepts the cgroupfs-write privilege.
Either way: no socket, zero egress, same two-stage dots + report below.

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

## Kernel mechanisms

| mechanism | covers | notes |
|-----------|--------|-------|
| **PSI triggers** (`/proc/pressure/*`, write `some <stall_us> <window_us>`, poll POLLPRI) | cpu, mem, io | kernel ≥5.2 + CONFIG_PSI; 0% idle |
| **per-cgroup PSI** (`/sys/fs/cgroup/<unit>/*.pressure`) | which *service* | cgroup v2; attribution for free |
| **cgroup v2 `memory.events`** (poll) | OOM-kill, per service | instant, fixes 1s-delta OOM miss |
| **epoll + timerfd + signalfd** | the event loop | one sleeping thread |
| **pidfd** (`pidfd_open`, ≥5.3) | correctness | no PID-reuse mis-blame when we react |
| **/proc/net/dev + snmp sample** (timerfd) | network | no PSI for net; link speed often `-1` on VMs |
| **statfs (timer) / fanotify FAN_FS_ERROR** | disk space / ro-remount | space is slow; ro-remount newer kernels |
| ~~eBPF~~ | (per-proc net bytes, runq latency) | **out of core** — root/CAP_BPF, surface; maybe optional later |

## Portability & fallback (because "all Linux")

Detect capabilities at startup:
- PSI triggers present → use them.
- else (old kernel / no CONFIG_PSI) → **timerfd utilization sampler** at a modest
  cadence. Same reports, slightly less instant.
- cgroup v2 → per-cgroup PSI + `memory.events`; cgroup v1 → fall back to host PSI
  / `vmpressure`.
`pinproc doctor` prints which trigger mode is active.

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
2. `internal/psi` — arm/wait/close a real PSI trigger (stdlib syscall epoll, no new
   deps) + availability detection. Proven on the VM: kernel wakes us, no socket.
3. dots ring buffer (`internal/dots`).
4. state machine (arm → watch → capture/discard).
5. wire CAPTURE to the existing engine; timeline in the report.
6. delivery (local + optional push).
7. `doctor` reports trigger mode; packaging drops the listener.

Each step tiny, tested, VM-verified. The report must read as app-level cause +
remediation, understandable by the owner of the affected service.
