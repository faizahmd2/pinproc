# pinproc

pinproc is a read-only Linux machine investigator.

It treats the Linux OS as the system boundary. Everything running on the host, Node, MySQL, Redis, Nginx, Java, containers and so on, is an OS resource consumer. pinproc measures the machine first, then follows evidence toward the process, thread, cgroup, file descriptor or socket that best explains the pressure.

Collection is native and read-only. pinproc does not require a cloud-provider API and does not use a mandatory central service.

## Architecture

```text
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
```

The core binary is provider-neutral. AI providers are separate packages that implement a versioned local JSON protocol. The core does not contain vendor HTTP clients.

```text
pinproc
  └── provider protocol v1
        ├── JEV provider
        ├── company-internal provider
        └── local-model provider
```

Removing all AI providers leaves pinproc operational with deterministic rules.

## Install

Production packages target Linux amd64 and arm64.

Install a release package directly:

```bash
sudo apt install ./pinproc_0.1.0_linux_amd64.deb
```

Or use the optional release installer:

```bash
PINPROC_VERSION=0.1.0 curl -fsSL https://raw.githubusercontent.com/faizahmd2/pinproc/master/scripts/install.sh | bash
```

The package creates a dedicated unprivileged `pinproc` system user and group, installs the systemd service, creates `/var/lib/pinproc`, and creates the managed configuration at `/etc/pinproc/config.yaml`.

The service runs as `pinproc` rather than root. It receives only the Linux capabilities required for read-only host inspection and is writable only to `/var/lib/pinproc`.

## Configuration

Installed configuration is managed through the CLI. End users should not need to edit the configuration file directly.

```bash
sudo pinproc setup ai
sudo pinproc setup callback --url https://example.example/pinproc
sudo pinproc setup callback --disable
sudo pinproc setup server --listen 127.0.0.1:8080
sudo pinproc setup server --clear-api-key
```

Changing configuration restarts the pinproc systemd service so the new configuration becomes active immediately.

The active configuration contains only provider-neutral fields plus provider-specific key/value settings. Vendor-specific configuration belongs to the selected provider package.

Inspect configuration safely:

```bash
sudo pinproc config show
sudo pinproc config show --json
sudo pinproc config validate
```

Secrets are redacted from `config show` output.

## AI

AI is optional.

Without an AI provider, pinproc continues with deterministic rules and records a small notice that AI is not configured. Configure a provider when you want additional contextual reasoning over the bounded evidence.

List providers:

```bash
pinproc ai list
```

Configure one:

```bash
sudo pinproc setup ai
sudo sudo pinproc ai status
```

Disable AI:

```bash
sudo pinproc ai remove
```

Provider packages are independent of the core package. For example, the JEV provider is distributed as `pinproc-provider-jev` and is not part of the core `pinproc` executable.

## Provider protocol

Provider executables are trusted software installed by the machine administrator. They run with the same `pinproc` service identity and communicate over JSON on standard input/output.

Request:

```json
{
  "protocol_version": 1,
  "action": "ask",
  "state": {},
  "questions": {},
  "config": {}
}
```

Response:

```json
{
  "protocol_version": 1,
  "answers": {}
}
```

Provider configuration, including secrets, is passed over stdin JSON and is never placed in process arguments.

See [docs/provider-protocol.md](docs/provider-protocol.md).

## API

```http
GET /health
GET /investigate
GET /report
```

`/investigate` accepts optional `hint` and `dimension` query parameters. One investigation is allowed at a time. The normal budget permits up to eight logical AI decision calls; an individual provider may perform its own transport retries inside one logical call.

## Reports

pinproc keeps one current report and one small state file:

```text
/var/lib/pinproc/
├── report.json
└── state.json
```

Reports are replaced atomically. pinproc does not retain an unbounded history.

## Host identity

pinproc reports the Linux hostname and an IP address actually assigned to an active non-loopback interface. A public address is preferred when the host owns one; otherwise an internal address is used. pinproc does not ask a cloud provider for identity or try to infer a NAT address.

## Package lifecycle

`apt remove pinproc` removes the package and leaves operator configuration/data in place, following normal Debian package behavior.

`apt purge pinproc` removes the package-owned configuration and runtime data. The dedicated `pinproc` user and group are removed only when this package created them, so an administrator-owned account is not deleted accidentally.

After purge, pinproc's package-owned filesystem paths, service unit, runtime data, configuration and package-created service account are removed. System package-manager/journal history is still controlled by the operating system.

## Development

```bash
go test ./...
go vet ./...
make build
make build-provider
```

The Linux collection layer reads `/proc`, `/sys`, cgroups and kernel state directly and is therefore Linux-specific.