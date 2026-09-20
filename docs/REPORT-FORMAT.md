# pinproc Investigation Report

investigation.json is the stable machine-readable contract. report.md is derived only from that contract.

Evidence records include capability, entity, dimension, level, observations, source paths and verification commands. Unavailable collection is explicit.

Findings are generated from deterministic rule signals. Reports render attribution, verification, not-investigated work and limitations so the operator can work backwards from every claim.

The human-readable report starts with a compact machine snapshot: hostname, primary IP when available, OS, kernel, architecture, CPU count, current CPU/load, memory and root filesystem usage. Detailed traversal paths and verification reads are suppressed for successful investigations and retained for failed or partially collected investigations so routine reports stay focused on the finding.
