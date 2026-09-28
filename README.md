# pinproc

<p align="center">
  <strong>Read-only Linux machine investigation for backend systems.</strong><br/>
  Measure the host, follow the evidence, and produce a developer-readable explanation.
</p>

pinproc treats the Linux host as the system boundary. CPU, memory, disk, network, sockets, processes, threads, file descriptors, and cgroups are measured first. The investigation then follows the evidence toward the resource or process that best explains the pressure.

Collection is native and read-only. pinproc does not require a cloud-provider API or a mandatory central service.

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

## AI Provider

AI is optional. Without a configured provider, pinproc continues with deterministic rules.

Currently supported provider:

```bash
sudo apt install pinproc-provider-jev
```

Configure it:

```bash
sudo pinproc setup ai --provider jev
```

The setup command prompts for the API key.

Manage providers:

```bash
pinproc ai list
pinproc ai status
sudo pinproc ai remove
```

## API

By default, pinproc listens on `127.0.0.1:8080`.

| Endpoint       | Purpose                                 |
| -------------- | --------------------------------------- |
| `/health`      | Service health                          |
| `/investigate` | Start an investigation and get a report |
| `/report`      | Read the current / last report          |

You can also guide an investigation with a hint or dimension:

```text
http://127.0.0.1:8080/investigate?hint=memory%2090
```

```text
http://127.0.0.1:8080/investigate?dimension=cpu
```

## Configuration

Show the managed configuration:

```bash
sudo pinproc config show
sudo pinproc config show --json
```

Validate configuration:

```bash
sudo pinproc config validate
```

Configure a report callback:

~~~bash
sudo pinproc setup callback --url https://example.com/pinproc
~~~

Disable the report callback:

~~~bash
sudo pinproc setup callback --disable
~~~

Configure the API listening port:

~~~bash
sudo pinproc setup server --listen 127.0.0.1:8080
~~~

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

Build the core:

```bash
make build
```

Build the JEV provider:

```bash
make build-provider
```

The Linux collection layer reads `/proc`, `/sys`, cgroups, and kernel state directly and is therefore Linux-specific.

## Links

* [Pinproc](https://github.com/faizahmd2/pinproc)
* [APT repository](https://faizahmd2.github.io/pinproc-apt/)
* [Provider protocol](docs/provider-protocol.md)
