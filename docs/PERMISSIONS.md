# Diagnos V2 Permissions

Most evidence is available without privilege: machine procfs counters, PSI, cgroups, disk/network counters and basic process metadata.

Cross-user process attribution can require access to /proc/<pid>/io, smaps, smaps_rollup, wchan and fd links. Prefer a dedicated diagnos user with the minimum read capability needed by the deployment. The installer ships a sudoers fallback for environments where file capabilities are unavailable.

Docker metadata is optional. If the Docker socket cannot be read, cgroup container IDs remain usable and the report records the enrichment gap.

Run diagnos doctor before deployment. It reports kernel, cgroup, PSI, effective capabilities and optional Prometheus, decision-provider and Docker reachability.
