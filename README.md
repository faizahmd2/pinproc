# pinproc

pinproc is a small, read-only Linux machine investigator.

It treats the Linux OS as the system boundary. Everything running on it, Node, MySQL, Redis, Nginx, Java, containers, and so on, is an OS resource consumer. pinproc measures the machine, follows evidence to the resource owner, and produces a bounded incident report.

The core package has no required AI service. It can run entirely offline with deterministic rules. AI is an optional provider adapter selected by the operator. Providers are external executables that speak a small versioned stdin/stdout protocol, so the pinproc core does not contain provider-specific HTTP calls.

## Install

The production package is a Debian/Ubuntu .deb. Releases support Linux amd64 and arm64.

Once a release exists:

~~~bash
curl -fsSL https://github.com/faizahmd2/pinproc/releases/latest/download/install.sh | sudo bash
~~~

For a local package file:

~~~bash
sudo apt install ./pinproc_<version>_<arch>.deb
~~~

The package owns the daemon, systemd unit, configuration directory, and dedicated service account.

The daemon runs as the dedicated non-login pinproc system user. Operators do not choose the service account. The service writes runtime state only under /var/lib/pinproc.

## Configuration

Production configuration lives in exactly one operator file:

~~~text
/etc/pinproc/config.yaml
~~~

Do not edit it manually. Use:

~~~bash
sudo pinproc setup ai
sudo pinproc setup callback
sudo pinproc setup server
~~~

Each successful setup validates the resulting configuration and restarts the service. Secrets are entered interactively without echo.

Package upgrades preserve the configuration.

## AI providers

AI is optional.

Without AI, pinproc continues with deterministic rules and records:

~~~text
AI is not configured; using deterministic rules.
Run 'sudo pinproc setup ai' for better adaptive results.
~~~

Providers are installed separately and discovered from:

~~~text
/usr/share/pinproc/providers/
~~~

Provider binaries are installed under:

~~~text
/usr/libexec/pinproc/providers/
~~~

Inspect installed providers:

~~~bash
pinproc provider list
pinproc provider show <provider>
sudo pinproc ai-status
~~~

Configure one:

~~~bash
sudo pinproc setup ai
~~~

Disable AI:

~~~bash
sudo pinproc setup ai --disable
~~~

The provider protocol is the stable boundary. A provider can use any AI vendor, a company-internal endpoint, a local model, or no network at all. The pinproc core does not know which vendor is behind the adapter.

See PROVIDERS.md for the provider package contract.

## API

The daemon exposes:

~~~text
GET /health
GET /investigate
GET /report
~~~

/investigate optionally accepts hint and dimension. It waits up to 30 seconds for a result, then returns processing while the investigation continues.

The service listens on 127.0.0.1:8080 by default. A non-loopback listener requires an API key:

~~~bash
sudo pinproc setup server
~~~

Callbacks are optional and best-effort:

~~~bash
sudo pinproc setup callback
~~~

## Reports and safety

Runtime state is kept under:

~~~text
/var/lib/pinproc/
├── report.json
└── state.json
~~~

Reports are replaced atomically and are not retained as a growing history.

The investigation is bounded. Reads have limits and timeouts, model state is capped, evidence is bounded, and decision calls are budgeted.

AI never replaces local machine evidence. The report keeps evidence and hypothesis grading separate from model reasoning.

## What pinproc looks at

- CPU: utilization, per-core balance, load, iowait, PSI, runnable and blocked work.
- Memory: available memory, swap, reclaim, major faults, OOM kills and PSI.
- Block I/O: throughput, IOPS, utilization, await, in-flight I/O and PSI.
- Network: RX/TX, drops, TCP retransmits, listen-backlog overflow and socket pressure.
- Processes: CPU, memory, I/O, threads, process state, limits and process tree.
- Files and sockets: descriptor ownership, socket counts, TCP states, listeners and deleted-open files.
- cgroups and filesystem capacity when the OS exposes them.
- Service identity: systemd unit, executable, command line, user, cgroup, container identity and listening ports when provable.

All collection is read-only and bounded.

## Investigation budgets

The default decision budget allows up to 8 AI provider calls per investigation. The exact number used is recorded in spent.decision_calls.

A healthy machine with no hint or forced dimension may require zero AI calls. An anomalous investigation starts with one broad assessment call and may make additional adaptive calls only while deeper evidence is warranted.

If a provider is unavailable, pinproc falls back to deterministic rules rather than failing the whole investigation.

## Package lifecycle

~~~text
apt install pinproc
      ↓
pinproc starts as a systemd service
      ↓
sudo pinproc setup ...
      ↓
configuration is validated and service restarted
      ↓
apt upgrade
      ↓
configuration is preserved
      ↓
apt remove pinproc
      ↓
service and binaries removed; config retained
      ↓
apt purge pinproc
      ↓
package-owned config/state and the dedicated pinproc account removed
~~~

The core package does not remove separately installed provider packages.

## Development

The production service reads /etc/pinproc/config.yaml. Hidden development commands may use --config.

Run:

~~~bash
go test ./...
go vet ./...
make build
~~~

Build a Debian package:

~~~bash
VERSION=0.3.0 GOARCH=arm64 make package
~~~

## License

MIT.
