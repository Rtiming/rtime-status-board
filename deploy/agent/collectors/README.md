# Agent Collector Groups

The deployed agent can remain a single Python script for now, but collector
functions should follow these groups:

- `cpu`: total, per-core, load, interrupts/context switches.
- `memory`: RAM, cache/buffer, swap.
- `storage`: mount usage, disk read/write bytes, IOPS.
- `network`: interface traffic, packets, errors, drops.
- `gpu`: optional NVIDIA/Jetson providers.
- `containers`: Docker/Compose metrics, opt-in.
- `processes`: top CPU/memory processes, opt-in.

Collectors should fail independently and report their status in metrics v2.
