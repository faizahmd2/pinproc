# Diagnos

Diagnos V2 is a read-only Linux resource investigation engine.

It does not use eBPF. V2 reads /proc, /sys, cgroup files, and other read-only OS state directly; remote hosts are inspected with bounded native SSH and one batched script per sampling step.

## Investigation model

```text
machine
  ├─ CPU / memory / block I/O / network / limits        (cheap L1)
  │
  └─ resource owner
       ├─ process attribution                            (L2)
       ├─ cgroup accounting                              (L2)
       └─ thread attribution                             (L3)
             ├─ memory maps / descriptor inventory       (L4)
             └─ sockets / deeper mechanisms              (L4+)
```

The AI layer is optional and provider-agnostic. The engine exposes bounded Choice, Noul, and Score decisions through internal/decision.Provider. The default provider is deterministic rules; TypeSafe Jev is an optional adapter.

Every investigation has limits for depth, steps, wall time, bytes, and model calls.

## Install

Build locally:

```sh
git checkout v2-investigation-engine
make build
sudo install -m 0755 diagnos /usr/local/bin/diagnos
sudo /usr/local/bin/diagnos install --user diagnos --interval 10m --dir /var/lib/diagnos/reports
```

## Basic checks

```sh
diagnos doctor
diagnos capabilities
```

## Run a local investigation

```sh
diagnos investigate localhost --dimension cpu --budget fast --out ./report
```

The result contains:

```text
report/
├── investigation.json
└── report.md
```

investigation.json is the stable machine-readable contract. report.md is the developer-facing report with evidence, attribution path, limitations, and verification hints.

## Cron / health-check mode

The cheap health check performs only the machine-level sweep. It exits 0 when no material anomaly is detected and exits 1 when investigation should be triggered.

```sh
*/10 * * * * /usr/local/bin/diagnos check --json > /var/log/diagnos-check.json
```

Grafana/Prometheus alerts can invoke the full investigation command:

```sh
diagnos investigate production-vm \
  --trigger 'grafana: request_rate=1000/s' \
  --hint 'request rate spike' \
  --dimension cpu
```

## Remote targets

Configure an alias in app.yaml:

```yaml
targets:
  production-vm:
    host: 10.0.0.10
    user: ubuntu
    port: 22
```

Then:

```sh
diagnos investigate production-vm --budget normal --out ./report
```

The engine itself does not require Kubernetes. Container and Kubernetes workloads are attributed when Linux cgroup/process identity exposes the relationship.

## Optional Prometheus baseline

Prometheus is not required for direct diagnosis.

When configured, historical Prometheus values are used as additional context to decide whether current observations are unusual. They do not replace local OS evidence.

```yaml
telemetry:
  prometheus:
    url: http://prometheus:9090
    auth:
      type: none
    baseline_queries:
      cpu_utilization: 100 - avg(rate(node_cpu_seconds_total{mode="idle"}[5m])) * 100
```

## Optional TypeSafe decision provider

Keep the engine independent of the provider:

```yaml
decision:
  provider: jev
  base_url: https://api.typesafe.ai
  model: jev-latest
  timeout: 10s
```

Set TYPESAFE_API_KEY in the environment.

Without a provider key, use --no-ai or leave decision.provider: rules.

## Production controls

V2 is deliberately bounded:

- no eBPF
- no mutation of target state
- no arbitrary model-generated shell commands
- model can select only registered capabilities
- selected PID/TID/cgroup scopes must have been observed previously
- remote sampling is batched and output-capped
- reports are written atomically
- temporary fixture replay is supported for repeatable tests

## Development

```sh
make fmt
make fmt-check
make vet
make test
make test-race
make build
```

The V2 tree is self-contained under cmd/diagnos and internal; legacy V1 catalog/planner/CLI artifacts are removed at the V2 milestone boundary.
