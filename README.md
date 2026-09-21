# pinproc

pinproc is a small, read-only Linux machine inspection service.

It runs continuously as a system service, survives reboots, and performs an inspection only when POST /trigger is called. At most one inspection can run at a time. Every inspection has a durable lifecycle state:

    idle -> running -> done

or:

    running -> failed / interrupted

The service reads local Linux interfaces such as /proc, /sys, cgroups, process/thread state, sockets, limits and related kernel state.

## Supported release targets

Production releases are Linux only:

- x86_64 / amd64
- arm64 / aarch64

## Install from the latest GitHub release

The normal deployment path is a Linux VM with systemd.

### 1. Download the binary and default config

Copy and paste:

    set -e

    ARCH="$(uname -m)"
    case "$ARCH" in
      x86_64) ASSET="pinproc_linux_amd64" ;;
      aarch64|arm64) ASSET="pinproc_linux_arm64" ;;
      *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
    esac

    sudo install -d -m 0755 /etc/pinproc
    sudo curl -fL       "https://github.com/faizahmd2/pinproc/releases/latest/download/$ASSET"       -o /usr/local/bin/pinproc
    sudo chmod 0755 /usr/local/bin/pinproc

    sudo curl -fL       "https://github.com/faizahmd2/pinproc/releases/latest/download/app.yaml"       -o /etc/pinproc/app.yaml

### 2. Put the AI key in app.yaml

Edit:

    sudo vi /etc/pinproc/app.yaml

Set:

    decision:
      provider: jev
      api_key: "<replace-with-ai-key>"
      base_url: https://api.typesafe.ai
      model: jev-latest
      timeout: 10s

Replace only the placeholder with the real key.

No environment variable is required. Environment variables remain optional overrides.

The default service settings are:

    service:
      user: ""
      group: ""
      data_directory: /var/lib/pinproc

    output:
      directory: /var/lib/pinproc/reports

When service.user is empty, installation uses the user who invoked sudo, for example ubuntu. When service.user is set to pinproc, the installer creates that dedicated service account when it does not already exist.

### 3. Install and start the service

Run:

    sudo /usr/local/bin/pinproc --config /etc/pinproc/app.yaml service install

This command:

- creates the configured service user if necessary
- creates the service data/report directory
- installs the systemd unit
- enables it for boot
- starts it immediately

Systemd brings enabled services back during normal boot through the configured boot target.

Check it:

    sudo systemctl status pinproc

Follow logs:

    sudo journalctl -u pinproc -f

Check service health:

    curl -sS http://127.0.0.1:8080/healthz | jq .

## Linux service security

The service is designed to run as an unprivileged account instead of running the inspection binary as root.

The generated systemd unit restricts the service to these capabilities:

    CAP_DAC_READ_SEARCH
    CAP_SYS_PTRACE
    CAP_SYSLOG

The unit also uses systemd filesystem and namespace restrictions and writes persistent state only below /var/lib/pinproc.

### Use the current user

For the simplest setup, keep:

    service:
      user: ""
      group: ""

Then install with:

    sudo /usr/local/bin/pinproc --config /etc/pinproc/app.yaml service install

The service runs as the invoking account rather than as root.

### Use a dedicated pinproc account

For a dedicated account, set:

    service:
      user: pinproc
      group: pinproc

Then run the same install command.

The installer creates the Linux system account if it is missing and assigns it a non-login shell.

This is the recommended deployment shape when you want the inspection isolated from the normal login account.

### Manually create the dedicated account (optional)

The installer can create this account automatically. When you prefer to create it yourself, run:

    sudo groupadd --system pinproc
    sudo useradd --system --gid pinproc --home-dir /var/lib/pinproc --no-create-home --shell /usr/sbin/nologin pinproc

Then keep service.user and service.group set to pinproc and run the service install command. Existing accounts are reused; the installer does not delete them.

## Trigger an inspection

There is one production inspection entry point:

    POST /trigger

Example:

    curl -sS -X POST http://127.0.0.1:8080/trigger       -H 'Content-Type: application/json'       -d '{
        "hint": "request latency increased",
        "dimension": "cpu",
        "budget": "normal",
        "trigger": "incident"
      }' | jq .

AI is used by default when the configured provider has a real key.

For a deterministic test run, explicitly add this field to the request:

    "no_ai": true

### One inspection at a time

If an inspection is already running, additional trigger requests do not start another one.

They receive HTTP 409 Conflict with the active inspection ID:

    {
      "status": "running",
      "id": "inv-...",
      "message": "inspection already happening; wait for it to finish"
    }

Ten simultaneous callers therefore result in one active inspection and nine rejected trigger attempts.

When the active inspection reaches a terminal state, the next trigger can start a new inspection.

## Inspect progress

Use:

    curl -i -sS http://127.0.0.1:8080/status | jq .

Typical running state:

    {
      "status": "running",
      "id": "inv-...",
      "stage": "deep_investigation"
    }

Typical completed state:

    {
      "status": "done",
      "id": "inv-...",
      "stage": "done"
    }

The state file is persistent:

    /var/lib/pinproc/reports/state.json

If the machine or service is restarted while an inspection is running, the next service startup marks that inspection as interrupted instead of leaving an indefinitely running state behind.

## Get the completed report

While an inspection is running:

    curl -i http://127.0.0.1:8080/report?format=json

returns HTTP 202 Accepted and the current state.

After completion:

    curl -sS http://127.0.0.1:8080/report?format=json | jq .

For the human-readable report:

    curl -sS http://127.0.0.1:8080/report

Reports are retained in the configured report directory, with the latest completed investigation referenced by:

    /var/lib/pinproc/reports/latest.txt

## Failure behavior

The service is intentionally fail-visible.

If an inspection fails completely, the service writes an investigation report and marks its durable state as failed.

A failure is never represented by "nothing happened".

The journal also contains lifecycle messages including:

    inspection accepted
    inspection progress
    inspection done
    inspection failed
    inspection panic

Follow them with:

    sudo journalctl -u pinproc -f

## Reboot test

After installation:

    sudo systemctl is-enabled pinproc
    sudo systemctl is-active pinproc

Then reboot:

    sudo reboot

After reconnecting:

    sudo systemctl is-active pinproc
    curl -sS http://127.0.0.1:8080/healthz | jq .

The service should be running again without manually launching the binary.

## Update to a new release

Download the new release binary over the installed path, then restart:

    ARCH="$(uname -m)"
    case "$ARCH" in
      x86_64) ASSET="pinproc_linux_amd64" ;;
      aarch64|arm64) ASSET="pinproc_linux_arm64" ;;
      *) echo "Unsupported architecture: $ARCH"; exit 1 ;;
    esac

    sudo curl -fL       "https://github.com/faizahmd2/pinproc/releases/latest/download/$ASSET"       -o /usr/local/bin/pinproc
    sudo chmod 0755 /usr/local/bin/pinproc
    sudo systemctl restart pinproc

Your existing /etc/pinproc/app.yaml and report data remain in place.

## Remove the service

    sudo /usr/local/bin/pinproc service uninstall

This removes the systemd service but deliberately preserves the configuration and reports.

## Developer commands

The deployed service uses service run and starts inspections only through POST /trigger.

The repository also contains developer-oriented one-shot commands:

    ./pinproc investigate localhost --no-ai --budget fast
    ./pinproc capture localhost --no-ai --budget fast --out ./captures
    ./pinproc replay ./captures/<inspection> --budget fast --out ./replay-output

These are useful for local debugging and fixture creation; they are not the production service integration path.

## Build and test

    make test
    make vet
    make build
    make release

make release produces only:

    dist/pinproc_linux_amd64
    dist/pinproc_linux_arm64
    dist/app.yaml
    dist/checksums.txt

The service is intentionally Linux-only because its collection layer uses Linux /proc, /sys and cgroup interfaces.
