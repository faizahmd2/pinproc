# pinproc

<p align="center">
  <strong>Read-only Linux machine investigation for backend systems.</strong><br/>
  Measure the host, follow the evidence, and produce a developer-readable explanation.
</p>

~~~mermaid
flowchart TD
    A[Linux host] --> B[Native bounded inspection]
    B --> C[Typed evidence]
    C --> D[Deterministic rules]
    D --> E[Investigation report]
    D -. optional .-> F[AI provider]
    F --> E
~~~

pinproc treats the Linux host as the system boundary. CPU, memory, disk, network, sockets, processes, threads, file descriptors, and cgroups are measured first. The investigation then follows the evidence toward the resource or process that best explains the pressure.

Collection is native and read-only. pinproc does not require a cloud-provider API or a mandatory central service.

## Install

### Ubuntu / Debian

The recommended installation uses the Pinproc APT repository:

~~~bash
curl -fsSL https://faizahmd2.github.io/pinproc-apt/install.sh | sudo bash
~~~

This detects amd64 or arm64, configures the signed APT repository, and installs the latest Pinproc package.

Verify:

~~~bash
pinproc --version
systemctl status pinproc --no-pager
~~~

The package installs:

- /usr/bin/pinproc
- systemd service: pinproc.service
- configuration: /etc/pinproc/config.yaml
- runtime data: /var/lib/pinproc
- dedicated unprivileged pinproc system user and group

### Direct package

Release .deb packages are also available for Linux amd64 and arm64.

~~~bash
sudo apt install ./pinproc_<version>_<arch>.deb
~~~

## Quick start

Check the service:

~~~bash
systemctl status pinproc --no-pager
~~~

Check the local API:

~~~bash
curl http://127.0.0.1:8080/health
~~~

Run an investigation:

~~~bash
curl http://127.0.0.1:8080/investigate
~~~

Read the latest report:

~~~bash
curl http://127.0.0.1:8080/report
~~~

The API is local by default and listens on 127.0.0.1:8080.

## What pinproc investigates

pinproc collects bounded evidence across the host:

- CPU utilisation and pressure
- memory and process memory
- disk I/O and filesystem state
- network I/O
- sockets and TCP state
- processes and threads
- file descriptors and socket attribution
- cgroups and process resource pressure
- process/thread capacity
- retransmission signals
- host identity

The goal is not to dump every Linux metric. It follows a bounded investigation path and produces evidence that can be understood by a backend developer.

## CLI

### AI providers

List installed providers:

~~~bash
pinproc ai list
~~~

Show the active provider and configuration state:

~~~bash
pinproc ai status
~~~

Disable AI reasoning:

~~~bash
sudo pinproc ai remove
~~~

AI is optional. Without a configured provider, pinproc continues with deterministic rules.

### Configuration

Show the managed configuration:

~~~bash
sudo pinproc config show
sudo pinproc config show --json
~~~

Validate configuration:

~~~bash
sudo pinproc config validate
~~~

Secrets are redacted from configuration output.

### Setup

Configure an AI provider:

~~~bash
sudo pinproc setup ai
~~~

Select a provider explicitly:

~~~bash
sudo pinproc setup ai --provider <provider-id>
~~~

Configure a report callback:

~~~bash
sudo pinproc setup callback --url https://example.com/pinproc
~~~

Disable the callback:

~~~bash
sudo pinproc setup callback --disable
~~~

Configure the local API listener:

~~~bash
sudo pinproc setup server --listen 127.0.0.1:8080
~~~

Configure or update the remote API key interactively:

~~~bash
sudo pinproc setup server
~~~

Clear the remote API key:

~~~bash
sudo pinproc setup server --clear-api-key
~~~

Configuration changes restart the pinproc systemd service.

## AI providers

The core binary is provider-neutral. Providers are separate packages that communicate with pinproc through a versioned local JSON protocol.

~~~text
pinproc
  │
  └── provider protocol v1
        ├── JEV
        ├── company-internal provider
        └── local-model provider
~~~

### JEV provider

Install the provider package:

~~~bash
sudo apt install pinproc-provider-jev
~~~

Configure it:

~~~bash
sudo pinproc setup ai --provider jev
~~~

The setup command prompts for provider settings and secrets instead of placing them in command-line arguments.

Provider manifests are installed under:

~~~text
/usr/share/pinproc/providers/
~~~

Provider executables are installed under:

~~~text
/usr/libexec/pinproc/providers/
~~~

See [docs/provider-protocol.md](docs/provider-protocol.md) for the provider protocol.

## API

| Endpoint | Purpose |
|---|---|
| GET /health | Service health |
| GET /investigate | Start an investigation |
| GET /report | Read the current/last report |

/investigate accepts optional hint and dimension query parameters. Only one investigation runs at a time.

Reports are stored as:

~~~text
/var/lib/pinproc/
├── report.json
└── state.json
~~~

Reports are replaced atomically and pinproc does not retain an unbounded report history.

## Architecture

~~~text
Linux kernel state
      ↓
bounded native reads
      ↓
typed evidence
      ↓
deterministic rules
      ↓
optional AI provider
      ↓
developer-readable report
~~~

The AI provider is downstream of the bounded OS evidence. Removing all AI providers leaves pinproc operational with deterministic rules.

## Security model

The Debian package:

- runs the service as the dedicated pinproc user
- does not require the service to run as root
- grants only the Linux capabilities needed for host inspection
- keeps writable runtime state under /var/lib/pinproc
- passes provider configuration and secrets over local stdin JSON rather than process arguments

## Package lifecycle

~~~bash
sudo apt remove pinproc
~~~

removes the package while preserving operator configuration/data.

~~~bash
sudo apt purge pinproc
~~~

removes package-owned configuration and runtime data. Package-created service accounts are removed only when the package created them.

## Development

Requirements:

- Go
- Linux for the native collection layer

Run tests and static checks:

~~~bash
go test ./...
go vet ./...
~~~

Build the core:

~~~bash
make build
~~~

Build the JEV provider:

~~~bash
make build-provider
~~~

The Linux collection layer reads /proc, /sys, cgroups, and kernel state directly and is therefore Linux-specific.

## Repository

- [Pinproc](https://github.com/faizahmd2/pinproc)
- [APT repository](https://faizahmd2.github.io/pinproc-apt/)
- [Provider protocol](docs/provider-protocol.md)
