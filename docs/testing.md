# pinproc testing playbook

How to exercise every aspect on a real Linux VM after installation. Assumes
pinproc is installed and the service is running.

> Load-generation tools used below:
> `sudo apt-get install -y stress-ng fio jq iproute2`

---

## 0. Is it healthy and watching?

```bash
pinproc --version
systemctl status pinproc --no-pager
sudo pinproc doctor                 # every line should be [ok]; if green, captures will not fail
journalctl -u pinproc -n 5 --no-pager | grep self-trigger   # "self-trigger active mode=pull"
```

`doctor` is the one command that tells you everything it needs is present.

---

## 1. Make captures fire quickly (optional, for testing)

Defaults are deliberately conservative (CPU 80/90%, mem 85/95%, io util 80/95%),
and there is a 2-minute cooldown per dimension. For fast iteration, lower them and
shorten cadences. Append a `monitor:` block to the config and restart:

```bash
sudo tee -a /etc/pinproc/config.yaml >/dev/null <<'YAML'

monitor:
  calm_cadence: 3s
  armed_cadence: 1s
  cooldown: 20s
  cpu: { arm_util: 50, cap_util: 70, arm_psi: 10, cap_psi: 20, sustain: 10s }
  mem: { arm_util: 60, cap_util: 75, arm_psi: 5,  cap_psi: 15, sustain: 10s }
  io:  { arm_util: 30, cap_util: 50, arm_psi: 10, cap_psi: 30, sustain: 10s }
  net: { retrans_arm: 20, retrans_cap: 100, conntrack_arm: 50, conntrack_cap: 80,
         time_wait_arm: 2000, time_wait_cap: 5000, orphan_arm: 500, orphan_cap: 1000, sustain: 10s }
YAML
sudo systemctl restart pinproc
pinproc config show                 # confirm the monitor block is active
```

Revert to defaults afterwards:

```bash
sudo sed -i '/^monitor:/,$d' /etc/pinproc/config.yaml && sudo systemctl restart pinproc
```

---

## 2. How to read a report

```bash
pinproc report            # run a fresh investigation now, print it (text)
pinproc report --last     # show the last auto-captured incident (no new run)
pinproc report --dimension memory            # focus a manual run on one resource
pinproc report --hint "checkout latency"     # pass operator context
```

A captured report reads: **verdict → ## Cause (service + components) → ## Incident
timeline → ## Machine → ## Notes**.

---

## 3. CPU

```bash
# peg all cores for 60s
stress-ng --cpu "$(nproc)" --timeout 60s &
sleep 25
pinproc report --last
```

Expect: `## Cause  stress-ng — ~N00% CPU across N processes`, each worker's share,
and the build-up timeline. Verifies: CPU trigger, per-app aggregation, timeline.

---

## 4. Memory (and OOM)

```bash
# grow to ~80% RAM and hold
python3 - <<'PY' &
import time
mb = []
for _ in range(400):            # ~4 GB; stop well under your RAM to avoid OOM
    mb.append(bytearray(10*1024*1024)); time.sleep(0.02)
time.sleep(60)
PY
sleep 20
pinproc report --last           # or: pinproc report --dimension memory
```

Expect the holder named with its RSS. For a **real OOM** (only if you can spare the
box), remove the `time.sleep` cap and let it exceed RAM with swap off — the OOM
kill shows in the machine snapshot / notes.

---

## 5. Disk I/O

```bash
# write to a REAL disk path (not /tmp if /tmp is tmpfs)
fio --name=w --filename="$HOME/pinproc-fio.dat" --size=2G --rw=randwrite \
    --bs=4k --direct=1 --ioengine=libaio --iodepth=32 --runtime=60 --time_based &
sleep 30
pinproc report --last
rm -f "$HOME/pinproc-fio.dat"
```

Expect: `## Cause  fio — N MB/s`, and `## Machine` shows the busy **device**
(`Disk: sdX … util … %`). Note: on fast cloud disks io-PSI stays low, so pinproc
arms on **device util** — that is expected.

---

## 6. Disk space

```bash
df -h /                               # pick a filesystem with room to spare
fallocate -l 2G "$HOME/pinproc-bigfile"   # or: dd if=/dev/zero of=... bs=1M count=2048
pinproc report --dimension filesystem
rm -f "$HOME/pinproc-bigfile"
```

Expect the largest paths called out with an app-oriented "reclaim" action. Also
surfaces inode exhaustion and deleted-but-open files when present.

---

## 7. Network

Connection ownership (works anywhere):

```bash
# hold ~200 connections on a port
python3 - <<'PY' &
import socket,time
s=socket.socket(); s.setsockopt(socket.SOL_SOCKET,socket.SO_REUSEADDR,1)
s.bind(('127.0.0.1',9999)); s.listen(400)
cl=[socket.create_connection(('127.0.0.1',9999)) for _ in range(150)]
sv=[s.accept()[0] for _ in range(150)]
time.sleep(60)
PY
sleep 10
pinproc report --dimension network      # names the service + :9999 + connection count
```

Conntrack / TIME_WAIT (needs the conntrack module + churn):

```bash
sudo modprobe nf_conntrack 2>/dev/null
cat /proc/sys/net/netfilter/nf_conntrack_count /proc/sys/net/netfilter/nf_conntrack_max
# generate many short-lived connections to grow TIME_WAIT / conntrack
for i in $(seq 1 2000); do (exec 3<>/dev/tcp/127.0.0.1/22) 2>/dev/null; done
ss -s        # see TIME_WAIT counts
pinproc report --dimension network
```

Expect conntrack-near-full / ephemeral-port / retransmit findings when thresholds
are crossed, with the owning service and ports.

---

## 8. Priority (CPU/RAM first)

```bash
# run CPU and disk load together; the report should lead with CPU/RAM, not the io/net blip
stress-ng --cpu "$(nproc)" --timeout 40s &
fio --name=w --filename="$HOME/f.dat" --size=1G --rw=randwrite --bs=4k --direct=1 \
    --ioengine=libaio --iodepth=16 --runtime=40 --time_based &
sleep 25
pinproc report --last        # expect the Cause to be CPU (or memory) when comparably loaded
rm -f "$HOME/f.dat"
```

---

## 9. Delivery to your channel (callback)

```bash
# POST each captured report (as plain text) to any HTTP sink
sudo pinproc setup callback --url https://your-endpoint.example/pinproc
sudo pinproc setup callback --disable     # turn it off
```

This POST is the **only** outbound traffic pinproc ever makes. With it disabled the
host is fully offline — no socket, no API, nothing listening.

---

## 10. Footprint / "is it light?"

```bash
systemctl status pinproc --no-pager | grep -E 'Memory|CPU'    # idle RSS and CPU
# per-tick cost is ~tens of microseconds; it sleeps between ticks (adaptive cadence)
cat /sys/fs/cgroup/system.slice/pinproc.service/memory.peak 2>/dev/null
```

Idle it should sit at a few MB and ~0% CPU; it only does a per-process scan while a
dimension is actively armed.

---

## 11. Tuning for your real alerts

Set `monitor:` thresholds (section in §1) to match what *you* consider an incident
on that class of VM, then `sudo systemctl restart pinproc`. `arm_*` starts watching
(collecting the timeline); `cap_*` (or `sustain`) confirms and captures. Raise
`cooldown` to avoid repeat reports during one long incident.
