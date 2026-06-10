#!/usr/bin/env python3
import json
import os
import re
import shutil
import socket
import subprocess
import sys
import time
import urllib.request


def read_cpu_snapshot():
    cpus = {}
    context_switches = 0
    interrupts = 0
    with open("/proc/stat", "r", encoding="utf-8") as fh:
        for line in fh:
            parts = line.split()
            if not parts:
                continue
            key = parts[0]
            if key.startswith("cpu"):
                values = [int(value) for value in parts[1:]]
                idle = values[3] + values[4]
                total = sum(values)
                cpus[key] = (idle, total)
            elif key == "ctxt" and len(parts) > 1:
                context_switches = int(parts[1])
            elif key == "intr" and len(parts) > 1:
                interrupts = int(parts[1])
    return {"cpus": cpus, "context_switches": context_switches, "interrupts": interrupts}


def cpu_delta_percent(before, after, key):
    idle1, total1 = before["cpus"].get(key, (0, 0))
    idle2, total2 = after["cpus"].get(key, (0, 0))
    total_delta = total2 - total1
    idle_delta = idle2 - idle1
    if total_delta <= 0:
        return 0.0
    return round((total_delta - idle_delta) * 100.0 / total_delta, 2)


def cpu_metrics():
    before = read_cpu_snapshot()
    time.sleep(0.25)
    after = read_cpu_snapshot()
    load1, load5, load15 = os.getloadavg()
    core_keys = sorted(
        [key for key in after["cpus"] if key != "cpu"],
        key=lambda value: int(value[3:]) if value[3:].isdigit() else 0,
    )
    return {
        "percent": cpu_delta_percent(before, after, "cpu"),
        "load1": round(load1, 2),
        "load5": round(load5, 2),
        "load15": round(load15, 2),
        "per_core_percent": [cpu_delta_percent(before, after, key) for key in core_keys],
        "context_switches": after["context_switches"],
        "interrupts": after["interrupts"],
    }


def meminfo():
    data = {}
    with open("/proc/meminfo", "r", encoding="utf-8") as fh:
        for line in fh:
            key, value = line.split(":", 1)
            data[key] = int(value.strip().split()[0]) * 1024
    return data


def memory_metrics(info, total_key, free_key):
    total = info.get(total_key, 0)
    available = info.get(free_key, 0)
    used = max(total - available, 0)
    percent = round((used * 100.0 / total), 2) if total else 0.0
    return {"total_bytes": total, "used_bytes": used, "percent": percent}


def disk_metrics(path="/"):
    stat = os.statvfs(path)
    total = stat.f_blocks * stat.f_frsize
    free = stat.f_bavail * stat.f_frsize
    used = max(total - free, 0)
    percent = round((used * 100.0 / total), 2) if total else 0.0
    return {"mountpoint": path, "total_bytes": total, "used_bytes": used, "percent": percent}


def storage_metrics():
    devices = []
    read_total = 0
    write_total = 0
    ignored_prefixes = ("loop", "ram", "zram", "sr")
    with open("/proc/diskstats", "r", encoding="utf-8") as fh:
        for line in fh:
            parts = line.split()
            if len(parts) < 14:
                continue
            name = parts[2]
            if name.startswith(ignored_prefixes):
                continue
            read_ios = int(parts[3])
            read_bytes = int(parts[5]) * 512
            write_ios = int(parts[7])
            write_bytes = int(parts[9]) * 512
            read_total += read_bytes
            write_total += write_bytes
            devices.append(
                {
                    "name": name,
                    "read_bytes": read_bytes,
                    "write_bytes": write_bytes,
                    "read_ios": read_ios,
                    "write_ios": write_ios,
                }
            )
    return {"read_bytes": read_total, "write_bytes": write_total, "devices": devices}


def network_metrics():
    interfaces = []
    rx_total = 0
    tx_total = 0
    ignored_prefixes = ("lo", "docker", "br-", "veth")
    with open("/proc/net/dev", "r", encoding="utf-8") as fh:
        for line in fh.readlines()[2:]:
            name, rest = line.split(":", 1)
            name = name.strip()
            if name.startswith(ignored_prefixes):
                continue
            values = rest.split()
            rx = int(values[0])
            tx = int(values[8])
            rx_total += rx
            tx_total += tx
            interfaces.append(
                {
                    "name": name,
                    "rx_bytes": rx,
                    "tx_bytes": tx,
                    "rx_packets": int(values[1]),
                    "tx_packets": int(values[9]),
                    "rx_errors": int(values[2]),
                    "tx_errors": int(values[10]),
                    "rx_drops": int(values[3]),
                    "tx_drops": int(values[11]),
                }
            )
    return {"rx_bytes": rx_total, "tx_bytes": tx_total, "interfaces": interfaces}


def parse_float(value):
    value = value.strip()
    if value in ("", "[N/A]", "N/A"):
        return 0.0
    return float(value)


def parse_percent(value):
    value = value.strip().rstrip("%")
    if value in ("", "[N/A]", "N/A"):
        return 0.0
    return round(float(value), 2)


def parse_bytes(value):
    value = value.strip()
    if value in ("", "-", "[N/A]", "N/A"):
        return 0
    match = re.match(r"^([0-9.]+)\s*([A-Za-z]+)?$", value)
    if not match:
        return 0
    number = float(match.group(1))
    unit = (match.group(2) or "B").lower()
    multipliers = {
        "b": 1,
        "kb": 1000,
        "kib": 1024,
        "mb": 1000**2,
        "mib": 1024**2,
        "gb": 1000**3,
        "gib": 1024**3,
        "tb": 1000**4,
        "tib": 1024**4,
    }
    return int(number * multipliers.get(unit, 1))


def parse_byte_pair(value):
    parts = [part.strip() for part in value.split("/", 1)]
    if len(parts) != 2:
        return 0, 0
    return parse_bytes(parts[0]), parse_bytes(parts[1])


def env_bool(name, default=True):
    value = os.environ.get(name)
    if value is None:
        return default
    return value.strip().lower() not in ("0", "false", "no", "off")


def env_int(name, default, minimum=0, maximum=100):
    value = os.environ.get(name)
    if value is None:
        return default
    try:
        parsed = int(value)
    except ValueError:
        return default
    return max(minimum, min(parsed, maximum))


def collector_cache_dir():
    return os.environ.get("STATUS_BOARD_AGENT_CACHE_DIR", f"/tmp/rtime-status-agent-{os.getuid()}")


def collector_cache_path(name):
    return os.path.join(collector_cache_dir(), f"{name}.json")


def load_cached_collector(name):
    path = collector_cache_path(name)
    try:
        with open(path, "r", encoding="utf-8") as fh:
            cached = json.load(fh)
    except (OSError, json.JSONDecodeError):
        return None, 0.0

    captured_at = float(cached.get("captured_at", 0))
    if captured_at <= 0 or "value" not in cached:
        return None, 0.0
    return cached["value"], max(time.time() - captured_at, 0.0)


def save_cached_collector(name, value):
    cache_dir = collector_cache_dir()
    os.makedirs(cache_dir, mode=0o700, exist_ok=True)
    path = collector_cache_path(name)
    tmp_path = f"{path}.tmp"
    with open(tmp_path, "w", encoding="utf-8") as fh:
        json.dump({"captured_at": time.time(), "value": value}, fh, separators=(",", ":"))
    os.replace(tmp_path, path)


def cached_timed_collector(name, collector, fallback, interval_seconds):
    if interval_seconds <= 0:
        return timed_collector(name, collector, fallback)

    start = time.time()
    cached_value, cache_age = load_cached_collector(name)
    if cached_value is not None and cache_age < interval_seconds:
        return cached_value, {
            "name": name,
            "ok": True,
            "cached": True,
            "cache_age_seconds": round(cache_age, 2),
            "elapsed_ms": int((time.time() - start) * 1000),
        }

    try:
        value = collector()
        save_cached_collector(name, value)
        return value, {
            "name": name,
            "ok": True,
            "cached": False,
            "cache_age_seconds": 0,
            "elapsed_ms": int((time.time() - start) * 1000),
        }
    except Exception as exc:
        if cached_value is not None:
            return cached_value, {
                "name": name,
                "ok": False,
                "cached": True,
                "cache_age_seconds": round(cache_age, 2),
                "detail": f"{exc}; reused cache",
                "elapsed_ms": int((time.time() - start) * 1000),
            }
        return fallback, {"name": name, "ok": False, "detail": str(exc), "elapsed_ms": int((time.time() - start) * 1000)}


def adaptive_collector(name, collector, fallback, default_interval, interval_env, collect_env=None):
    if collect_env and not env_bool(collect_env, True):
        return timed_collector(name, collector, fallback)
    interval = env_int(interval_env, default_interval, minimum=0, maximum=86400)
    return cached_timed_collector(name, collector, fallback, interval)


def parse_docker_labels(value):
    labels = {}
    for item in value.split(","):
        if "=" not in item:
            continue
        key, label_value = item.split("=", 1)
        labels[key.strip()] = label_value.strip()
    return labels


def gpu_metrics():
    nvidia_smi = shutil.which("nvidia-smi")
    if nvidia_smi:
        output = subprocess.check_output(
            [
                nvidia_smi,
                "--query-gpu=index,name,utilization.gpu,memory.total,memory.used,temperature.gpu,power.draw",
                "--format=csv,noheader,nounits",
            ],
            text=True,
            timeout=4,
        )
        devices = []
        for line in output.strip().splitlines():
            parts = [part.strip() for part in line.split(",")]
            if len(parts) < 7:
                continue
            memory_total = int(parse_float(parts[3]) * 1024 * 1024)
            memory_used = int(parse_float(parts[4]) * 1024 * 1024)
            devices.append(
                {
                    "index": parts[0],
                    "name": parts[1],
                    "util_percent": parse_float(parts[2]),
                    "memory_total_bytes": memory_total,
                    "memory_used_bytes": memory_used,
                    "memory_percent": round(memory_used * 100.0 / memory_total, 2) if memory_total else 0.0,
                    "temperature_c": parse_float(parts[5]),
                    "power_watts": parse_float(parts[6]),
                }
            )
        return {"available": bool(devices), "provider": "nvidia-smi", "devices": devices}

    tegrastats = shutil.which("tegrastats")
    if tegrastats:
        try:
            output = subprocess.check_output([tegrastats, "--interval", "1000", "--count", "1"], text=True, timeout=4)
            util = 0.0
            marker = "GR3D_FREQ "
            if marker in output:
                tail = output.split(marker, 1)[1]
                util = parse_float(tail.split("%", 1)[0])
            return {
                "available": True,
                "provider": "tegrastats",
                "devices": [{"index": "0", "name": "Jetson GPU", "util_percent": util}],
            }
        except Exception as exc:
            return {"available": False, "provider": "tegrastats", "devices": [], "detail": str(exc)}

    return {"available": False, "provider": "none", "devices": []}


def container_metrics():
    if not env_bool("STATUS_BOARD_COLLECT_CONTAINERS", True):
        return {"available": False, "provider": "disabled", "containers": []}

    docker = shutil.which("docker")
    if not docker:
        return {"available": False, "provider": "none", "containers": []}

    ps_by_id = {}
    ps_by_name = {}
    ps_output = subprocess.check_output(
        [docker, "ps", "--format", "{{json .}}"],
        text=True,
        timeout=4,
    )
    for line in ps_output.strip().splitlines():
        if not line.strip():
            continue
        item = json.loads(line)
        labels = parse_docker_labels(item.get("Labels", ""))
        info = {
            "id": item.get("ID", ""),
            "name": item.get("Names", ""),
            "image": item.get("Image", ""),
            "state": item.get("State", ""),
            "compose_project": labels.get("com.docker.compose.project", ""),
        }
        if info["id"]:
            ps_by_id[info["id"]] = info
        if info["name"]:
            ps_by_name[info["name"]] = info

    stats_output = subprocess.check_output(
        [docker, "stats", "--no-stream", "--format", "{{json .}}"],
        text=True,
        timeout=6,
    )
    containers = []
    for line in stats_output.strip().splitlines():
        if not line.strip():
            continue
        item = json.loads(line)
        container_id = item.get("Container") or item.get("ID", "")
        name = item.get("Name", "")
        info = ps_by_id.get(container_id, ps_by_name.get(name, {}))
        memory_used, memory_limit = parse_byte_pair(item.get("MemUsage", ""))
        network_rx, network_tx = parse_byte_pair(item.get("NetIO", ""))
        block_read, block_write = parse_byte_pair(item.get("BlockIO", ""))
        containers.append(
            {
                "id": container_id,
                "name": name or info.get("name", ""),
                "image": info.get("image", ""),
                "state": info.get("state", ""),
                "compose_project": info.get("compose_project", ""),
                "cpu_percent": parse_percent(item.get("CPUPerc", "")),
                "memory_percent": parse_percent(item.get("MemPerc", "")),
                "memory_usage_bytes": memory_used,
                "memory_limit_bytes": memory_limit,
                "network_rx_bytes": network_rx,
                "network_tx_bytes": network_tx,
                "block_read_bytes": block_read,
                "block_write_bytes": block_write,
            }
        )

    limit = env_int("STATUS_BOARD_CONTAINER_LIMIT", 8, minimum=1, maximum=50)
    containers.sort(key=lambda item: (item["cpu_percent"], item["memory_usage_bytes"]), reverse=True)
    return {"available": True, "provider": "docker", "containers": containers[:limit]}


def process_metrics():
    if not env_bool("STATUS_BOARD_COLLECT_PROCESSES", True):
        return {"process_count": 0, "processes": []}

    args = ["ps", "-eo", "pid,ppid,user,pcpu,pmem,rss,comm", "--sort=-pcpu"]
    try:
        output = subprocess.check_output(args, text=True, timeout=3)
    except subprocess.CalledProcessError:
        output = subprocess.check_output(args[:-1], text=True, timeout=3)

    processes = []
    rows = [line for line in output.splitlines()[1:] if line.strip()]
    for line in rows:
        parts = line.split(None, 6)
        if len(parts) < 7:
            continue
        pid, ppid, user, cpu, memory, rss, command = parts
        processes.append(
            {
                "pid": int(pid),
                "ppid": int(ppid),
                "user": user,
                "command": command,
                "cpu_percent": parse_percent(cpu),
                "memory_percent": parse_percent(memory),
                "rss_bytes": int(rss) * 1024,
            }
        )

    processes.sort(key=lambda item: (item["cpu_percent"], item["memory_percent"]), reverse=True)
    limit = env_int("STATUS_BOARD_PROCESS_LIMIT", 8, minimum=1, maximum=50)
    return {"process_count": len(rows), "processes": processes[:limit]}


def uptime_seconds():
    with open("/proc/uptime", "r", encoding="utf-8") as fh:
        return round(float(fh.read().split()[0]), 2)


def post_json(url, token, payload):
    data = json.dumps(payload, separators=(",", ":")).encode("utf-8")
    req = urllib.request.Request(url, data=data, method="POST")
    req.add_header("Content-Type", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=8) as resp:
        body = resp.read().decode("utf-8")
        if resp.status < 200 or resp.status >= 300:
            raise RuntimeError(f"server returned {resp.status}: {body}")
        return body


def build_payload(node_id):
    info = meminfo()
    cpu, cpu_status = timed_collector("cpu", cpu_metrics, {"percent": 0, "load1": 0, "load5": 0, "load15": 0})
    storage, storage_status = timed_collector("storage", storage_metrics, {"read_bytes": 0, "write_bytes": 0, "devices": []})
    network, network_status = timed_collector("network", network_metrics, {"rx_bytes": 0, "tx_bytes": 0, "interfaces": []})
    gpu, gpu_status = adaptive_collector(
        "gpu",
        gpu_metrics,
        {"available": False, "provider": "none", "devices": []},
        120,
        "STATUS_BOARD_GPU_INTERVAL_SECONDS",
    )
    containers, containers_status = adaptive_collector(
        "containers",
        container_metrics,
        {"available": False, "provider": "unknown", "containers": []},
        300,
        "STATUS_BOARD_CONTAINER_INTERVAL_SECONDS",
        "STATUS_BOARD_COLLECT_CONTAINERS",
    )
    processes, processes_status = adaptive_collector(
        "processes",
        process_metrics,
        {"process_count": 0, "processes": []},
        300,
        "STATUS_BOARD_PROCESS_INTERVAL_SECONDS",
        "STATUS_BOARD_COLLECT_PROCESSES",
    )

    return {
        "schema_version": 2,
        "node_id": node_id,
        "hostname": socket.gethostname(),
        "captured_at": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "resources": {
            "cpu": cpu,
            "memory": memory_metrics(info, "MemTotal", "MemAvailable"),
            "swap": memory_metrics(info, "SwapTotal", "SwapFree"),
            "disk": disk_metrics("/"),
            "storage": storage,
            "network": network,
            "gpu": gpu,
            "containers": containers,
            "processes": processes,
            "uptime": {"seconds": uptime_seconds()},
        },
        "collector_status": [cpu_status, storage_status, network_status, gpu_status, containers_status, processes_status],
        "extra": {"agent": "rtime-status-agent.py", "report_version": "2"},
    }


def agent_check_summary(payload):
    statuses = payload.get("collector_status", [])
    failed = [item for item in statuses if not item.get("ok")]
    required = {"cpu", "storage", "network"}
    required_failed = [item for item in failed if item.get("name") in required]
    resources = payload.get("resources", {})
    storage = resources.get("storage", {})
    network = resources.get("network", {})
    gpu = resources.get("gpu", {})
    containers = resources.get("containers", {})
    processes = resources.get("processes", {})
    return {
        "ok": len(required_failed) == 0,
        "schema_version": payload.get("schema_version"),
        "node_id": payload.get("node_id"),
        "hostname": payload.get("hostname"),
        "collector_ok": len(statuses) - len(failed),
        "collector_failed": len(failed),
        "required_failed": [item.get("name", "") for item in required_failed],
        "optional_failed": [item.get("name", "") for item in failed if item.get("name") not in required],
        "gpu_available": bool(gpu.get("available")),
        "gpu_provider": gpu.get("provider", ""),
        "container_available": bool(containers.get("available")),
        "container_count": len(containers.get("containers", [])),
        "process_count": int(processes.get("process_count", 0) or 0),
        "storage_device_count": len(storage.get("devices", [])),
        "network_interface_count": len(network.get("interfaces", [])),
        "collector_status": statuses,
    }


def timed_collector(name, collector, fallback):
    start = time.time()
    try:
        value = collector()
        return value, {"name": name, "ok": True, "elapsed_ms": int((time.time() - start) * 1000)}
    except Exception as exc:
        return fallback, {"name": name, "ok": False, "detail": str(exc), "elapsed_ms": int((time.time() - start) * 1000)}


def report_url():
    url = os.environ.get("STATUS_BOARD_URL", "http://100.64.10.5:18083/api/v1/metrics/report/v2")
    report_version = os.environ.get("STATUS_BOARD_REPORT_VERSION", "2")
    if report_version == "2" and url.endswith("/api/v1/metrics/report"):
        return url + "/v2"
    return url


def main():
    check_mode = "--check" in sys.argv
    print_mode = "--print" in sys.argv
    if check_mode or print_mode:
        node_id = os.environ.get("STATUS_BOARD_NODE_ID", socket.gethostname())
    else:
        node_id = os.environ["STATUS_BOARD_NODE_ID"]
    token = os.environ.get("STATUS_BOARD_AGENT_TOKEN", "")

    payload = build_payload(node_id)
    if print_mode:
        print(json.dumps(payload, indent=2, sort_keys=True))
        return
    if check_mode:
        summary = agent_check_summary(payload)
        print(json.dumps(summary, indent=2, sort_keys=True))
        if not summary["ok"]:
            raise SystemExit(1)
        return

    post_json(report_url(), token, payload)


if __name__ == "__main__":
    main()
