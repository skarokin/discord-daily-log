#!/usr/bin/env python3
"""Run the bot locally and expose it through a Cloudflare quick tunnel."""

from __future__ import annotations

import argparse
import os
import shutil
import subprocess
import tempfile
import time
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent


def load_env(path: Path) -> None:
    if not path.exists():
        raise SystemExit(f"Create {path} from .env.example first")
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        os.environ[name.strip()] = value.strip()


def require(command: str) -> None:
    if shutil.which(command) is None:
        raise SystemExit(f"{command} is required but was not found on PATH")


def source_snapshot(env_path: Path) -> dict[Path, tuple[int, int]]:
    paths = list(ROOT.rglob("*.go")) + [ROOT / "go.mod", ROOT / "go.sum", env_path]
    return {
        path: (path.stat().st_mtime_ns, path.stat().st_size)
        for path in paths
        if path.exists()
    }


def build_server(output: Path) -> bool:
    result = subprocess.run(
        ["go", "build", "-o", str(output), "./cmd/server"],
        cwd=ROOT,
    )
    if result.returncode != 0:
        print("Build failed; the previous server remains active.")
        return False
    return True


def stop(process: subprocess.Popen[bytes] | None) -> None:
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=5)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait()


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--env-file", default=".env")
    args = parser.parse_args()

    require("go")
    require("cloudflared")
    env_path = ROOT / args.env_file
    load_env(env_path)
    os.environ["DEV_MODE"] = "true"
    port = os.environ.get("PORT", "8080")

    server: subprocess.Popen[bytes] | None = None
    tunnel: subprocess.Popen[bytes] | None = None
    with tempfile.TemporaryDirectory(prefix="discord-daily-log-") as temp_dir:
        build_number = 1
        binary_suffix = ".exe" if os.name == "nt" else ""
        binary = Path(temp_dir) / f"server-{build_number}{binary_suffix}"
        if not build_server(binary):
            raise SystemExit("Initial Go build failed")
        server = subprocess.Popen([str(binary)], cwd=ROOT)
        time.sleep(1)
        if server.poll() is not None:
            raise SystemExit(f"Go server exited with status {server.returncode}")

        print("\nDiscord local interaction testing:")
        print("1. Copy the trycloudflare.com URL printed below.")
        print("2. Set Discord's Interactions Endpoint URL to <URL>/interactions.")
        print("3. Go changes rebuild automatically without changing the tunnel URL.\n")
        tunnel = subprocess.Popen(
            ["cloudflared", "tunnel", "--url", f"http://localhost:{port}"],
        )
        try:
            snapshot = source_snapshot(env_path)
            while tunnel.poll() is None:
                time.sleep(0.75)
                updated = source_snapshot(env_path)
                if updated == snapshot:
                    continue
                snapshot = updated
                build_number += 1
                candidate = Path(temp_dir) / f"server-{build_number}{binary_suffix}"
                print("\nChange detected; rebuilding Go server...")
                if not build_server(candidate):
                    continue
                load_env(env_path)
                os.environ["DEV_MODE"] = "true"
                stop(server)
                server = subprocess.Popen([str(candidate)], cwd=ROOT)
                print("Go server restarted; Cloudflare tunnel is unchanged.")
            raise SystemExit(f"cloudflared exited with status {tunnel.returncode}")
        except KeyboardInterrupt:
            pass
        finally:
            stop(server)
            stop(tunnel)


if __name__ == "__main__":
    main()
