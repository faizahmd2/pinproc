# Diagnos V2 capability graph

| Capability | Level | Dimension | Scope | Cost | Next |
|---|---:|---|---|---|---|
| machine.cpu | 1 | cpu | machine | low | machine.processes, machine.io |
| machine.memory | 1 | memory | machine | low | machine.processes |
| machine.io | 1 | io | machine | low | machine.processes |
| machine.network | 1 | network | machine | low | machine.processes |
| machine.limits | 1 | limits | machine | low | machine.processes |
| machine.processes | 2 | attribution | machine | medium | process.cpu, process.memory, process.io |
| process.cpu | 2 | cpu | observed PID | low | thread.cpu, cgroup.cpu |
| process.memory | 2 | memory | observed PID | low | process.memory_maps, cgroup.memory |
| process.io | 2 | io | observed PID | low | process.files, cgroup.io |
| process.files | 3 | filesystem | observed PID | medium | process.sockets |
| process.memory_maps | 4 | memory | observed PID | high | — |
| process.sockets | 4 | network | observed PID | medium | — |
| thread.cpu | 3 | cpu | observed TID | medium | — |
| cgroup.cpu | 2 | cpu | observed cgroup | low | — |
| cgroup.memory | 2 | memory | observed cgroup | low | — |
| cgroup.io | 2 | io | observed cgroup | low | — |

## Traversal rule

1. L1 machine evidence is collected first.
2. Deterministic rules identify materially abnormal dimensions.
3. The decision provider may choose only from registered capabilities.
4. A PID, TID, or cgroup scope is legal only when a previous capability observed it.
5. Noul decisions can stop traversal or suppress deeper inspection.
6. Every investigation is bounded by depth, steps, wall time, bytes, and model-call budgets.
