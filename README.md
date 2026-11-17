# pinproc

<p align="center">
  <strong>Read-only Linux machine investigation for backend systems.</strong><br/>
  Measure the host, follow the evidence, and produce a developer-readable explanation.
</p>

pinproc treats the Linux host as the system boundary. It watches CPU, memory, disk I/O and network by reading kernel pressure/utilization on a cheap adaptive timer, and when a resource crosses a threshold it captures the incident and attributes it to the **service** responsible — with the component processes, a build-up timeline, and an application-level next step.

Collection is native and read-only. There is **no socket, no API and no agent to call**: pinproc triggers itself. By default nothing leaves the machine; the only optional egress is the alert/report you choose to send to your callback URL or your own SMTP server.

## Install

### Ubuntu / Debian

Install from the pinproc APT repository:

```bash
curl -fsSL https://faizahmd2.github.io/pinproc-apt/install.sh | sudo bash
```

Verify the installation:

```bash
pinproc --version
```

The package installs:

| Path / Service             | Purpose                        |
| -------------------------- | ------------------------------ |
| `/usr/bin/pinproc`         | CLI                            |
| `pinproc.service`          | systemd service                |
| `/etc/pinproc/config.yaml` | Configuration                  |
| `/var/lib/pinproc`         | Runtime data                   |
| `pinproc` user/group       | Dedicated unprivileged account |

Check the service:

```bash
systemctl status pinproc --no-pager
```

## Usage

```bash
pinproc                       # live, top-like view of the machine (q to quit)
pinproc --once                # print one snapshot and exit (also used when piped)
sudo pinproc report           # detailed report → report.html + report.json, sent to your callback/email
sudo pinproc alert callback   # send a pressure alert now (the service does this by itself)
sudo pinproc alert email
sudo pinproc doctor           # read-only self-check — if green, captures won't fail
sudo pinproc --upgrade        # upgrade to the latest release (APT installs)
```

Every output is rendered from one JSON document (`alert.json` / `report.json` in
`/var/lib/pinproc`), so the terminal view, the HTML pages, the email and the plain
text always agree. `pinproc render <file.json> --as html|email|text` re-renders a
saved document, for example in another timezone.

**Alerts** (captured by the service): a severity banner naming the exact kernel
signal and threshold, CPU / memory / disk / network pillars, the cause service and
how the incident built up, block-device and NIC queues, and the top services with
their binary, command line, cgroup, cgroup PSI, OOM score and disk I/O.

**Detailed report** (`pinproc report`): hardware baseline, then zero-sum accounting
— RAM split into applications (per service), shared memory, file cache, kernel and
free so the rows add up to the machine's physical RAM; CPU time split into
applications, kernel, waiting and idle across all cores; and one card per disk
splitting its full size into used-per-mount (with the largest directories), free,
reserved/metadata, swap and unpartitioned space.

## Configuration

```bash
sudo pinproc config show                  # effective config (secrets masked)
sudo pinproc config callback --url https://example.com/pinproc [--format html|json|text] [--test]
sudo pinproc config email                 # interactive SMTP setup; --test sends a test mail
sudo pinproc config timezone IST          # default UTC
sudo pinproc config cooldown 30m          # min gap between automatic alerts per resource
sudo pinproc config tag production        # label in report headers
```

Delivery is the last step and never blocks the watcher: the service saves the
alert, then sends it in the background with bounded timeouts. The only outbound
traffic pinproc ever makes is to the callback URL and SMTP server you configure;
with neither set, nothing leaves the machine. Email uses your own SMTP account
(Gmail/Workspace app password, SES, Postmark, Mailgun, a local relay…); pinproc
ships no mail provider.

Tune when an incident is captured by editing the optional `monitor:` section of
`/etc/pinproc/config.yaml` (see `docs/testing.md`), then restart the service.

## Testing

See [docs/testing.md](docs/testing.md) for commands to exercise every resource
(CPU, memory, disk I/O, disk space, network) on a real VM.

## Remove

```bash
sudo apt remove pinproc
```

To also remove configuration files:

```bash
sudo apt purge pinproc
```

## Development

Requirements:

* Go
* Linux for the native collection layer

Run tests and static checks:

```bash
go test ./...
go vet ./...
```

Build:

```bash
make build
```

The Linux collection layer reads `/proc`, `/sys`, cgroups, and kernel state directly and is therefore Linux-specific.

## Links

* [Pinproc](https://github.com/faizahmd2/pinproc)
* [APT repository](https://faizahmd2.github.io/pinproc-apt/)
* [Architecture](docs/trigger-architecture.md)
