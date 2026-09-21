# pinproc

pinproc is a small, read-only Linux machine inspection service.

It runs continuously as a system service and performs an inspection when `POST /trigger` is called. It reads local Linux interfaces such as `/proc`, `/sys`, cgroups, process/thread state, sockets, limits and related kernel state.

## Supported release targets

Production releases are Linux only:

- x86_64 / amd64
- arm64 / aarch64

## Install from the latest GitHub release

The normal deployment path is a Linux VM with systemd.

### 1. Download the binary and default config

Copy and paste:

```bash
set -e

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
```

### 2. Put the AI key in app.yaml

Edit:

```bash
sudo vi /etc/pinproc/app.yaml
```

Set:

```yaml
decision:
  provider: jev
  api_key: "<replace-with-ai-key>"
  base_url: https://api.typesafe.ai
  model: jev-latest
  timeout: 10s
```

Replace only the placeholder with the real key.

No environment variable is required. Environment variables remain optional overrides.

The pinproc installer does not create, modify, or delete Linux users or groups, and it does not apply a user/group policy to the systemd service. Any service identity or host-level security policy remains under the machine owner's control.

### 3. Install and start the service

Run:

```bash
sudo /usr/local/bin/pinproc --config /etc/pinproc/app.yaml service install
```

This installs the systemd unit, enables pinproc for boot, creates the required pinproc data/report directories, and starts the service.

### 4. Check the service and logs

Check that systemd enabled and started pinproc:

```bash
sudo systemctl is-enabled pinproc
sudo systemctl is-active pinproc
```

Check recent logs:

```bash
sudo journalctl -u pinproc -n 50 --no-pager
```

Follow logs live:

```bash
sudo journalctl -u pinproc -f
```

Check the local health endpoint:

```bash
curl -sS http://127.0.0.1:8080/healthz | jq .
```

## Trigger an inspection

### With AI

AI is used when the configured provider has a real key:

```bash
curl -sS -X POST http://127.0.0.1:8080/trigger   -H 'Content-Type: application/json'   -d '{
    "hint": "request latency increased",
    "dimension": "cpu",
    "budget": "normal",
    "trigger": "incident"
  }' | jq .
```

### Without AI

For a deterministic local run, add `"no_ai": true`:

```bash
curl -sS -X POST http://127.0.0.1:8080/trigger   -H 'Content-Type: application/json'   -d '{
    "hint": "request latency increased",
    "dimension": "cpu",
    "budget": "normal",
    "trigger": "incident",
    "no_ai": true
  }' | jq .
```

The trigger response returns the inspection ID and links for status and report.

## Inspect progress

Use:

```bash
curl -sS http://127.0.0.1:8080/status | jq .
```

While an inspection is running, the response includes `"status": "running"` and the current stage.

After the inspection is complete, the same endpoint returns `"status": "done"` and the completion time in `finished_at`:

```json
{
  "status": "done",
  "id": "inv-...",
  "stage": "done",
  "started_at": "2026-09-21T10:20:30+05:30",
  "updated_at": "2026-09-21T10:21:02+05:30",
  "finished_at": "2026-09-21T10:21:02+05:30"
}
```

## Get the completed report

For the machine-readable report:

```bash
curl -sS http://127.0.0.1:8080/report?format=json | jq .
```

For the human-readable report:

```bash
curl -sS http://127.0.0.1:8080/report
```

Reports are stored under:

```text
/var/lib/pinproc/reports
```

The latest completed investigation is referenced by:

```text
/var/lib/pinproc/reports/latest.txt
```

## Update to a new release

Download the new release binary over the installed path and restart the service:

```bash
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64) ASSET="pinproc_linux_amd64" ;;
  aarch64|arm64) ASSET="pinproc_linux_arm64" ;;
  *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
esac

sudo curl -fL "https://github.com/faizahmd2/pinproc/releases/latest/download/$ASSET"   -o /usr/local/bin/pinproc
sudo chmod 0755 /usr/local/bin/pinproc
sudo systemctl restart pinproc
```

Your existing `/etc/pinproc/app.yaml` and report data remain in place.

## Complete uninstallation

Stop and disable the service:

```bash
sudo systemctl disable --now pinproc.service
```

Remove the systemd unit, binary, configuration, and all pinproc application data:

```bash
sudo rm -f /etc/systemd/system/pinproc.service
sudo systemctl daemon-reload

sudo rm -f /usr/local/bin/pinproc
sudo rm -rf /etc/pinproc
sudo rm -rf /var/lib/pinproc
```

The installer does not create or manage Linux users or groups, so there is no pinproc account or group for the application to remove.

System-wide journal history is managed by systemd and is not removed by these commands.

## Local development

Clone the repository:

```bash
git clone https://github.com/faizahmd2/pinproc.git
cd pinproc
```

Create a local config so the service writes inside the checkout instead of `/var/lib/pinproc`:

```bash
cp app.yaml app.local.yaml
```

Set these values in `app.local.yaml`:

```yaml
service:
  data_directory: ./data

output:
  directory: ./data/reports
```

For a no-AI local run, the API key can remain as the placeholder.

Start the service directly with Go:

```bash
go run ./cmd/diagnos service run --config ./app.local.yaml
```

From another terminal, trigger an inspection:

```bash
curl -sS -X POST http://127.0.0.1:8080/trigger   -H 'Content-Type: application/json'   -d '{"hint":"local development check","dimension":"cpu","budget":"fast","trigger":"dev","no_ai":true}' | jq .
```

Check the result:

```bash
curl -sS http://127.0.0.1:8080/status | jq .
curl -sS http://127.0.0.1:8080/report
```

Run the basic Go checks from the repository root:

```bash
go test ./...
go vet ./...
```

The production service is Linux-only because the collection layer uses Linux `/proc`, `/sys` and cgroup interfaces.
