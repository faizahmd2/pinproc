# pinproc

<p align="center">
  <strong>Read-only Linux machine investigation for backend systems.</strong><br/>
  Measure the host, follow the evidence, and produce a developer-readable explanation.
</p>

pinproc treats the Linux host as the system boundary. It watches CPU, memory, disk I/O and network by reading kernel pressure/utilization on a cheap adaptive timer, and when a resource crosses a threshold it captures the incident and attributes it to the **service** responsible — with the component processes, a build-up timeline, and an application-level next step.

Collection is native and read-only. There is **no socket, no API and no agent to call**: pinproc triggers itself. By default nothing leaves the machine; the only optional egress is a single plain-text report POSTed to a URL you configure.

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

pinproc runs as a systemd service that watches the host and captures incidents on
its own. To read reports:

```bash
pinproc report          # run an investigation now and print it
pinproc report --last   # print the service's last captured incident
sudo pinproc doctor     # read-only self-check — if green, captures won't fail
```

Reports read top-down: a one-line verdict, the **Cause** (service + components +
action), how the incident **built up**, and the current machine context.

## Configuration

```bash
sudo pinproc config show        # show the managed config
sudo pinproc config validate    # validate it
```

Send each captured report (as plain text) to one URL — the only traffic pinproc
ever sends:

```bash
sudo pinproc setup callback --url https://example.com/pinproc
sudo pinproc setup callback --disable
```

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
