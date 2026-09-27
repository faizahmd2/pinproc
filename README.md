<img width="1536" height="1024" alt="pinproc" src="https://github.com/user-attachments/assets/83a03e90-113c-44a7-b0ff-f18de480be30" />

# pinproc

pinproc is a small, read-only Linux machine investigator.

It treats the Linux OS as the system boundary. Everything running on it, Node, MySQL, Redis, Nginx, Java, containers, and so on, is an OS resource consumer. pinproc first measures the machine, then follows evidence to the process, thread, cgroup, file descriptor, or socket that best explains the pressure.

It does not require cloud-provider APIs and does not change the machine it investigates. When reporting host identity, pinproc uses the hostname configured by Linux and an IP address actually assigned to the host. If the host owns a public address, it is preferred; otherwise an internal address is reported. pinproc does not try to discover a cloud-provider or NAT address.

## What it looks at

- CPU: utilization, per-core imbalance, load, iowait, PSI, steal, runnable and blocked work.
- Memory: available memory, swap, reclaim, major faults, OOM kills and PSI.
- Block I/O: read/write throughput, IOPS, utilization, await, in-flight I/O and PSI.
- Network: RX/TX, drops, TCP retransmits, listen-backlog overflow and socket pressure.
- Processes: CPU, memory, I/O, threads, process state, limits and process tree.
- Files and sockets: descriptor ownership, socket counts, TCP connection states, listeners and deleted-open files.
- cgroups and filesystem capacity when the OS exposes them.
- Service identity: systemd unit, executable, command line, user, cgroup, container identity and listening ports when provable.

All collection is read-only and bounded.

## Install

Production releases support Linux amd64 and arm64.

Download the latest binary and configuration:

~~~bash
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ASSET="pinproc_linux_amd64" ;;
  aarch64|arm64) ASSET="pinproc_linux_arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

sudo install -d -m 0755 /etc/pinproc
sudo curl -fL "https://github.com/faizahmd2/pinproc/releases/latest/download/$ASSET"   -o /usr/local/bin/pinproc
sudo chmod 0755 /usr/local/bin/pinproc

sudo curl -fL "https://github.com/faizahmd2/pinproc/releases/latest/download/app.yaml"   -o /etc/pinproc/app.yaml
~~~

Put the JEV key in <code>/etc/pinproc/app.yaml</code>.

The installer creates the <code>pinproc</code> system user and group by default, creates <code>/var/lib/pinproc</code>, fixes its ownership, installs the systemd unit and starts the service.

~~~bash
sudo /usr/local/bin/pinproc --config /etc/pinproc/app.yaml service install
~~~

Check the service:

~~~bash
sudo systemctl is-enabled pinproc
sudo systemctl is-active pinproc
sudo journalctl -u pinproc -n 50 --no-pager
~~~

## The API

There are only three HTTP endpoints.

### Investigate

~~~http
GET /investigate
~~~

No request body.

Optional query parameters:

~~~text
hint
dimension
~~~

Examples:

~~~bash
curl http://127.0.0.1:8080/investigate
curl "http://127.0.0.1:8080/investigate?hint=API%20latency%20increased"
curl "http://127.0.0.1:8080/investigate?dimension=cpu"
~~~

pinproc waits up to 30 seconds for the investigation to finish.

When it finishes in that window, the response is the complete investigation report.

If another investigation is already running:

~~~json
{
  "status": "processing",
  "message": "An investigation is already in progress."
}
~~~

If the investigation is still running after 30 seconds:

~~~json
{
  "status": "processing",
  "message": "Investigation is still running. Use GET /report for the result."
}
~~~

There are no public investigation IDs and no public status lookup.

### Report

~~~http
GET /report
~~~

Returns the latest completed report.

While a new investigation is running, <code>/report</code> reports that the current result is still processing instead of silently returning an older report.

After two minutes of an unchanged pending state, <code>/report</code> may return the previous completed report as a clearly marked <code>stale</code> response. The returned report contains its own <code>incident_checked_at</code> timestamp so an older investigation cannot be mistaken for the current incident.

### Health

~~~http
GET /health
~~~

Returns a small health response for process supervision and monitoring.

Unknown paths and unsupported HTTP methods behave like missing endpoints.

## Authentication

The service listens on loopback by default.

When <code>server.api_key</code> is configured and the request is not loopback, send:

~~~http
Authorization: Bearer <api-key>
~~~

The API only uses <code>hint</code> and <code>dimension</code> as investigation query parameters.

## Callback

A callback is optional. When enabled, pinproc sends the completed investigation report once to the configured HTTP endpoint.

~~~yaml
callback:
  enabled: true
  url: https://example.example/pinproc
  timeout: 5s
~~~

Callback delivery is best-effort and does not change the diagnostic result. A callback failure is logged separately.

This makes pinproc easy to connect to Grafana alerts, Google Cloud Monitoring alerts, Prometheus-based systems, PagerDuty, or a company-specific incident endpoint without putting vendor logic into the core investigator.

## Reports and safety

pinproc keeps one report and one small state file:

~~~text
/var/lib/pinproc/
├── report.json
└── state.json
~~~

Reports are replaced atomically. pinproc does not retain a growing investigation history.

The investigation itself is bounded. Reads are size-limited, risky filesystem reads have timeouts, deep inspection has explicit limits, and report output is bounded by the engine's evidence and finding limits.

The service performs no installation, configuration change, process control, database write, filesystem cleanup, or other mutation on the machine it investigates.

## How diagnosis works

The core flow is:

~~~text
Linux kernel state
      ↓
bounded native reads
      ↓
typed evidence
      ↓
deterministic rules
      ↓
AI reasoning over bounded evidence
      ↓
developer-readable report
~~~

AI does not replace machine evidence.

A report should describe what was observed, which OS owner was associated with it, and how strong that connection is. It should not claim application-internal facts that pinproc cannot observe.

For example, pinproc may establish:

~~~text
Block I/O pressure observed
        ↓
mysqld has the highest observed write throughput
        ↓
mysql.service owns the process
        ↓
mysqld owns a large number of open sockets / file descriptors
~~~

That is an OS-level diagnosis.

It does not need to know which SQL query is slow.

## Development

Clone the repository and use a local configuration:

~~~bash
git clone https://github.com/faizahmd2/pinproc.git
cd pinproc
cp app.yaml app.local.yaml
~~~

Set:

~~~yaml
service:
  user: ""
  group: ""
  data_directory: ./data
~~~

Then:

~~~bash
go run ./cmd/diagnos service run --config ./app.local.yaml
~~~

Run the test suite:

~~~bash
go test ./...
go vet ./...
~~~

The production collection layer is Linux-only because it reads Linux /proc, /sys, cgroups and kernel state directly.
