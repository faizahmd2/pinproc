# pinproc

<p align="center">
  <strong>Read-only Linux incident watcher.</strong><br/>
  It stays with the kernel while your machine is struggling, pins the process responsible,<br/>
  and hands you the evidence — by email, by callback, or in your terminal.
</p>

pinproc is a small native service for Linux VMs. It reads the kernel's own
pressure signals (`/proc/pressure`, `/proc/stat`, `/proc/meminfo`, cgroups) on a
cheap adaptive timer. The moment CPU, memory, disk I/O or the network crosses a
threshold it captures the incident — while the machine is still under pressure —
and attributes it to the **service** responsible: the exact program, its command
line, its cgroup, how the incident built up, and the log lines written at that
moment.

There is **no agent API, no socket, no cloud**. pinproc triggers itself, needs
no configuration to run, and sends nothing anywhere unless you add an email
account or a callback URL — both are yours, and both are optional.

```bash
curl -fsSL https://faizahmd2.github.io/pinproc-apt/install.sh | sudo bash
```

---

## 1 · Pressure alerts

After install the service is already watching with built-in defaults
(`systemctl status pinproc`). This is the whole loop:

```
 calm ── every 10s: read PSI + utilisation (a few small /proc files) ──────────┐
   │ a resource crosses its warn line                                          │
   ▼                                                                           │
 watching ── every 2s: record a timeline sample + top consumers ── recovers ───┘
   │ crosses the capture line, or stays elevated for the sustain window
   ▼
 capture ── engine + machine snapshot in parallel, 30s hard deadline
   │        cause service · timeline · journal + kernel log lines · workloads
   ▼
 alert.json ──► alert.html ──► email, then callback  (background, bounded, 30-min cooldown)
```

Built to work when the machine is in trouble:

* **It stays alive.** The unit runs with `OOMScoreAdjust=-900`, cgroup memory
  protection (`MemoryLow`) and a high CPU/IO weight so it is scheduled on a
  saturated box, while hard caps (`MemoryMax=384M`, a 64 MiB Go heap limit, two
  OS threads) keep it from ever becoming the problem.
* **It never hangs on the kernel.** Reads that can block behind a stuck process
  (`cmdline`, `smaps_rollup`) are time-boxed; the whole capture has a deadline.
* **It always reports.** If attribution cannot finish, the alert still goes out
  with the timeline and the machine snapshot, and says what was skipped.
* **Delivery is last and out of the way.** The alert is saved first; email and
  callback are sent afterwards in the background with timeouts, at most once per
  resource per cooldown (default 30 minutes) — no alert storms.

An alert contains: the exact kernel signal and threshold that fired, CPU /
memory / disk / network state with the 10s · 1m · 5m stall trend, the **cause
service** and its processes, how the incident built up, **logs from the incident
window** (the cause unit's warnings and errors from the journal, kernel messages
such as OOM kills, hung tasks or I/O errors, and other errors on the host —
repeats collapsed), the workloads under pressure with everything about them, and
the machine breakdown for the resource that fired. Supporting findings appear
only when pinproc has something confident to add.

### Email setup

Bring any SMTP account — Gmail/Workspace or Outlook (with an app password),
Amazon SES, SendGrid, Mailgun, Postmark, Brevo, Zoho, or a relay on the machine.
pinproc speaks SMTP to it directly with Go's standard library; nothing passes
through a third party.

```bash
sudo pinproc config email
```

```
Provider (gmail, outlook, office365, ses, sendgrid, mailgun, postmark, brevo, zoho, local, custom) [custom]: gmail
  note: needs an app password: myaccount.google.com/apppasswords
SMTP host [smtp.gmail.com]:
Port (587 STARTTLS · 465 TLS · 25 local relay) [587]:
Security (starttls · tls · none) [starttls]:
Username (blank for none): alerts@example.com
Password (hidden):
From (e.g. pinproc <alerts@example.com>) [pinproc <alerts@example.com>]:
Send to (comma-separated): ops@example.com, oncall@example.com
Attach the full HTML page to each email? [Y/n]

Alert subject (optional)
  current:      [pinproc] {severity} · {dimension} pressure on {host} — {cause} (default)
  placeholders: {host} {ip} {tag} {severity} {dimension} {cause} {headline}
Custom subject (Enter keeps it, - restores the default):
  alerts will read: [pinproc] CRITICAL · memory pressure on web-1 — java 6.10 GiB

Email configuration saved to /etc/pinproc/config.yaml · pinproc restarted.

Test SMTP connection and authentication now? [Y/n]

Testing smtp.gmail.com:587 (starttls) — dry run, nothing will be sent
  ✓ connect    smtp.gmail.com:587 (142.250.4.108:587)
  ✓ starttls   TLS 1.3 · certificate valid for smtp.gmail.com
  ✓ auth       PLAIN as alerts@example.com
  ✓ sender     alerts@example.com accepted
  ✓ recipient  ops@example.com accepted
  ✓ recipient  oncall@example.com accepted

SMTP settings work. Nothing was sent (dry run).
```

The test walks the SMTP conversation up to — but not including — sending a
message, so it proves the host, port, TLS, login, sender and every recipient
without delivering anything. Run it again any time, or send a real alert:

```bash
sudo pinproc config email --test
sudo pinproc alert email
```

Emails are HTML (inline-styled so Gmail and Outlook render them as designed)
with a plain-text alternative and, by default, the full alert page attached.
The password is never accepted as a command-line flag: it is asked for without
echo, or read with `--password-stdin` / `--password-file`, and stored in
`/etc/pinproc/config.yaml` (mode 0640, root:pinproc). Non-interactive setup:

```bash
echo "$SMTP_PASS" | sudo pinproc config email --password-stdin \
  --host smtp.gmail.com --user alerts@example.com \
  --from "pinproc <alerts@example.com>" --to ops@example.com,oncall@example.com
```

## 2 · The machine, described

```bash
pinproc                 # one structured look at the machine, right now
sudo pinproc report     # the full description → report.html + report.json (+ delivery)
```

pinproc looks at a VM the way a senior engineer does on a box they did not
build: **what is running, how it lives, and whether it will keep running** —
told from the processes' side, with the command that proves every line.

**Workloads.** Every service (systemd unit, container or program) as one card:

| | |
|---|---|
| **Runs** | command line, binary, unit file and drop-ins, restart policy, user, working directory, who launched it (`systemd → containerd-shim → java`), running since, restarts |
| **Uses** | CPU now and averaged since start, hottest threads (`top -H`), memory now (PSS) · peak since start (`memory.peak`) · limit, disk I/O now and since start, threads, `pids.max`, open files against its `nofile` limit |
| **Connects** | the ports it accepts on and how many clients are connected, the endpoints it talks to (a local database is named by its service), the units it depends on |
| **Pressure** | its own cgroup stall time, CPU throttling against its quota, OOM kills, OOM-killer score |
| **Verify** | `systemctl status …`, `journalctl -u …`, `cat /sys/fs/cgroup/…/memory.peak`, `ss -tnp …` — run them yourself |

**Accounting that adds up.** RAM split into applications (per service), shared
memory, file cache, kernel and free so the rows add up to the physical RAM; CPU
time across all cores; every disk's full size split into used-per-mount (with the
largest directories), free, reserved, swap and unpartitioned space.

**Exposed ports.** Every listening socket, its service, its clients, and whether
it is reachable from all interfaces or local only (`ss -tulpn`).

**Host checks.** The things that decide whether workloads keep running: OOM kills
since boot, swap and overcommit, open-file limits (system and per service),
accept-queue overflows, conntrack, clock sync, journal persistence and size,
failed units, restart loops, SSH password/root login, host firewall, automatic
security updates, pending reboot, filesystem fill, network services running as
root. Each with the command that verifies it.

The terminal view is the short version: resources with the stall trend, the
workloads with their ports and clients, the busiest processes with full paths,
and only the checks that need attention. `pinproc --json` prints the same
snapshot as JSON.

## 3 · Callback

POST every alert (and, on request, reports) to your own endpoint — a webhook
receiver, a chat bridge, an incident tool.

```bash
sudo pinproc config callback --url https://hooks.example.com/pinproc
sudo pinproc config callback --format json      # html (default) | json | text
sudo pinproc config callback --test             # small test request
sudo pinproc alert callback                     # a real alert, now
```

The body is the self-contained HTML page by default, or the full JSON document,
or plain text. Alerts go to email first, then the callback; each is bounded by
its own timeout and shares the cooldown.

---

## Settings

pinproc runs with built-in defaults and no configuration at all.

```bash
pinproc config list                          # what is in effect, in plain words
pinproc config show                          # the full YAML (secrets masked)
sudo pinproc config set report.timezone IST  # change any single setting
sudo pinproc config set alerts.cooldown 1h
sudo pinproc config set monitor.mem.cap_util 92
sudo pinproc config timezone IST             # UTC (default), IST, Area/City or +05:30
sudo pinproc config tag production           # label in headers and subjects
```

Every change is validated and the service restarted. Thresholds live under
`monitor:` in `/etc/pinproc/config.yaml` — see [docs/testing.md](docs/testing.md).

## Everyday commands

```bash
pinproc                       # one look at the machine
sudo pinproc report           # full machine description, saved and delivered
sudo pinproc alert email      # pressure alert now (also: callback; --last re-sends the last capture)
sudo pinproc doctor           # read-only self-check — if green, captures won't fail
pinproc render alert.json --as html|email|text   # re-render a saved document
sudo pinproc --upgrade        # upgrade to the latest release
```

Every output is rendered from one JSON document (`alert.json` / `report.json` in
`/var/lib/pinproc`), so the terminal, the HTML page, the email and the text
always agree.

## What it touches

| Path / Service             | Purpose                                           |
| -------------------------- | ------------------------------------------------- |
| `/usr/bin/pinproc`         | the binary                                        |
| `pinproc.service`          | the watcher, as the unprivileged `pinproc` user   |
| `/etc/pinproc/config.yaml` | settings                                          |
| `/var/lib/pinproc`         | saved alerts and reports, delivery cooldown state |

The service holds three read-only capabilities (`CAP_DAC_READ_SEARCH`,
`CAP_SYS_PTRACE`, `CAP_SYSLOG`) to read other users' `/proc` entries, the
journal and the kernel log; it cannot write outside `/var/lib/pinproc`. It runs
`journalctl` and `systemctl show` (read-only queries) with short timeouts; the
only network traffic it ever makes is to the SMTP server and callback URL you
configure.

## Testing

See [docs/testing.md](docs/testing.md) for commands that exercise every resource
(CPU, memory, disk I/O, disk space, network) on a real VM.

## Remove

```bash
sudo apt remove pinproc     # keep settings
sudo apt purge pinproc      # remove settings too
```

## Development

```bash
go test ./...
go vet ./...
make build
```

The collection layer reads `/proc`, `/sys`, cgroups and kernel state directly and
is Linux-specific; everything else builds and tests on any OS.

## Links

* [pinproc](https://github.com/faizahmd2/pinproc)
* [APT repository](https://faizahmd2.github.io/pinproc-apt/)
* [Architecture](docs/trigger-architecture.md)
