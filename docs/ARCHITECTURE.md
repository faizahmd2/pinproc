# Diagnos V2 Architecture

Diagnos V2 is a bounded, realtime Linux investigation engine. It is host-native: the investigated machine's /proc, /sys, cgroups and other read-only kernel interfaces are the source of truth. There is no Prometheus, node_exporter, TSDB, SSH transport, or eBPF dependency in the default product.

Collection uses direct local /proc and /sys reads. Short sampling windows are used only when a kernel counter delta is required for a realtime rate, not for historical telemetry or range queries. The engine never converts model output into an arbitrary path or shell command.

The investigation descends from machine signals to resource owners, execution units and deeper mechanisms. Every capability is registered with a dimension, level, cost and legal next edges. Model choices are constrained by this graph and by entities already observed.

Decision-making is separated from narration. Jev is the V2 structured decision provider; deterministic rules are the fallback when Jev is unavailable or explicitly disabled. Narration is generated locally from the stable investigation contract and does not participate in evidence collection or decision selection.

Hard constraints include bounded reads, bounded output, bounded wall time/bytes/steps/decision calls, low local GOMAXPROCS, and atomic report writes. Missing processes during a live scan are normal and are recorded rather than treated as fatal.

eBPF is intentionally outside the core product.
