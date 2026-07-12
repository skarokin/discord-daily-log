#!/usr/bin/env python3
"""Register development-guild Discord slash commands."""

from __future__ import annotations

import argparse
import json
import os
import urllib.error
import urllib.request
from pathlib import Path


COMMANDS = [
    {
        "name": "ask",
        "description": "Analyze the current daily-log thread",
        "type": 1,
        "options": [
            {
                "name": "prompt",
                "description": "Question about today's log",
                "type": 3,
                "required": False,
                "max_length": 1500,
            }
        ],
    },
    {
        "name": "goal",
        "description": "Show or replace the goal prepended to every request",
        "type": 1,
        "options": [
            {
                "name": "description",
                "description": "Complete replacement goal; omit to show current goal",
                "type": 3,
                "required": False,
                "max_length": 1800,
            }
        ],
    },
]

ROOT = Path(__file__).resolve().parent.parent


def load_local_env() -> None:
    path = ROOT / ".env"
    if not path.exists():
        return
    for raw_line in path.read_text(encoding="utf-8").splitlines():
        line = raw_line.strip()
        if not line or line.startswith("#") or "=" not in line:
            continue
        name, value = line.split("=", 1)
        os.environ.setdefault(name.strip(), value.strip())


def main() -> None:
    load_local_env()
    parser = argparse.ArgumentParser()
    parser.add_argument("--application-id", required=True)
    parser.add_argument("--guild-id", required=True)
    parser.add_argument(
        "--bot-token",
        default=os.environ.get("DISCORD_BOT_TOKEN"),
        help="Defaults to DISCORD_BOT_TOKEN; prefer the environment to shell history.",
    )
    args = parser.parse_args()
    if not args.bot_token:
        parser.error("--bot-token or DISCORD_BOT_TOKEN is required")

    endpoint = (
        "https://discord.com/api/v10/applications/"
        f"{args.application_id}/guilds/{args.guild_id}/commands"
    )
    for command in COMMANDS:
        request = urllib.request.Request(
            endpoint,
            data=json.dumps(command).encode(),
            method="POST",
            headers={
                "Authorization": f"Bot {args.bot_token}",
                "Content-Type": "application/json",
                "User-Agent": "discord-daily-log/0.1",
            },
        )
        try:
            with urllib.request.urlopen(request, timeout=30) as response:
                result = json.load(response)
        except urllib.error.HTTPError as error:
            detail = error.read().decode(errors="replace")
            raise SystemExit(
                f"Discord returned HTTP {error.code} for /{command['name']}: {detail}"
            ) from error
        print(f"Registered /{result['name']} ({result['id']})")


if __name__ == "__main__":
    main()
