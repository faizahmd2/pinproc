# Diagnos V2 Architecture

Diagnos V2 is a bounded, realtime Linux investigation engine. It is host-native: the investigated machine's /proc, /sys, cgroups and other read-only kernel interfaces are the source of truth. It has no Prometheus, node_exporter or TSDB dependency. The default build uses no eBPF.

Local collection uses direct /proc and /sys reads. Remote collection uses native SSH with one bounded batch per snapshot/sample; remote mode still reads the target machine's OS directly. Short local sampling windows are used only when a kernel counter delta is required for a realtime rate, not for historical telemetry or range queries. The engine never converts model output into an arbitrary path or shell command.

The investigation descends from machine signals to resource owners, execution units and deeper mechanisms. Every capability is registered with a dimension, level, cost, requirements and legal next edges. Model choices are validated against this graph and against entities already observed.

Hard constraints include bounded reads, bounded output, bounded wall time/bytes/steps/decision calls, low local GOMAXPROCS, and atomic report writes. Missing processes during a live scan are normal and are recorded rather than treated as fatal.

eBPF is intentionally outside the default core. A future build-tagged module can add event observation without changing Source, contract or engine boundaries.
