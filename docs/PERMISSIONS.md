# Diagnos V2 Permissions

Most evidence is available without privilege: machine procfs counters, PSI, cgroups, disk/network counters and basic process metadata.

Cross-user process attribution can require access to /proc/<pid>/io, smaps, smaps_rollup, wchan and fd links. Run Diagnos with the minimum read capability needed by the deployment.

Diagnos does not execute remote commands, use SSH transport, or require eBPF. Jev decision calls require the configured TypeSafe API credential; if Jev is unavailable, deterministic rules keep the investigation running.
