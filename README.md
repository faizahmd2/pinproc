# vm-native-diagnos

`vm-native-diagnos` is a tiny, read-only Linux machine investigation agent.

It is the V2 investigation engine moved into its own repository. The agent runs **on the machine being investigated** and reads `/proc`, `/sys`, cgroups, process/thread state, sockets, limits and other read-only OS interfaces directly.

There is no SSH transport, no eBPF, no Prometheus, no node_exporter and no historical telemetry dependency.

## Runtime flow

```text
HTTP POST /trigger
       │
       ▼
local /proc + /sys + cgroups
       │
       ▼
bounded V2 adaptive investigation
       │
       ├── deterministic rules
       ├── registered capabilities
       └── Jev AI decisions when deeper investigation is warranted
       │
       ▼
investigation.json + report.md
       │
       ▼
HTTP GET /report
```

Short sampling windows are only used to calculate realtime counter deltas such as CPU and I/O rates. Nothing is queried from historical telemetry.

## HTTP API

The binary starts the server when invoked without a subcommand.

### Trigger

```http
POST /trigger
Content-Type: application/json

{
  "hint": "request latency increased",
  "dimension": "cpu",
  "budget": "normal",
  "trigger": "incident"
}
```

The investigation runs locally and the latest report is persisted. The response contains the investigation ID and points to `/report`.

### Report

```http
GET /report
```

Returns the latest stable JSON investigation contract.

Default listen address:

```text
127.0.0.1:8080
```

Configure it in `app.yaml`:

```yaml
server:
  listen: :8080
```

The default localhost binding is intentional because `/trigger` is an operational endpoint.

## AI

AI decision-making is enabled by default through the bounded `decision.Provider` interface.

```yaml
decision:
  provider: jev
  base_url: https://api.typesafe.ai
  model: jev-latest
  timeout: 10s
```

Set:

```sh
export TYPESAFE_API_KEY=...
```

If the AI service is unavailable, the investigation falls back to deterministic rule decisions rather than failing the machine analysis.

## Reports

Reports are written to:

```text
~/diagnos/reports/
├── investigation.json
└── report.md
```

The JSON document is the stable machine-readable contract. The Markdown document is the human-readable report.

## Build

```sh
make build
sudo install -m 0755 diagnos /usr/local/bin/vm-native-diagnos
```

Run explicitly:

```sh
vm-native-diagnos serve
```

Or simply:

```sh
vm-native-diagnos
```

The latter starts the server using the default configuration.

## Design constraints

- read-only OS investigation
- local machine only
- no SSH or remote command execution
- no eBPF
- no Prometheus/node_exporter
- no historical telemetry window queries
- bounded capability descent
- model may choose only registered capabilities
- PID/TID/cgroup scopes must be observed before deeper collection
- bounded bytes, depth, steps, wall time and AI calls
- atomic report writes
