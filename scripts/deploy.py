#!/usr/bin/env python3
"""Apply infrastructure, build the image, and deploy Cloud Run."""

from __future__ import annotations

import argparse
import datetime
import shutil
import subprocess
from pathlib import Path


ROOT = Path(__file__).resolve().parent.parent
FOUNDATION = ROOT / "infra" / "foundation"
APP = ROOT / "infra" / "app"


def run(*args: str, cwd: Path | None = None, capture: bool = False) -> str:
    result = subprocess.run(
        args,
        cwd=cwd,
        check=True,
        text=True,
        stdout=subprocess.PIPE if capture else None,
    )
    return result.stdout.strip() if capture else ""


def require(command: str) -> None:
    if shutil.which(command) is None:
        raise SystemExit(f"{command} is required but was not found on PATH")


def main() -> None:
    parser = argparse.ArgumentParser()
    parser.add_argument("--project-id", required=True)
    parser.add_argument("--region", default="us-central1")
    parser.add_argument("--foundation-var-file", default="terraform.tfvars")
    parser.add_argument("--app-var-file", default="terraform.tfvars")
    args = parser.parse_args()

    require("gcloud")
    require("tofu")
    run("gcloud", "config", "set", "project", args.project_id)

    run("tofu", f"-chdir={FOUNDATION}", "init")
    run(
        "tofu",
        f"-chdir={FOUNDATION}",
        "apply",
        f"-var-file={args.foundation_var_file}",
        f"-var=project_id={args.project_id}",
        f"-var=region={args.region}",
    )

    missing = []
    for secret in ("discord-bot-token", "usda-api-key"):
        versions = run(
            "gcloud",
            "secrets",
            "versions",
            "list",
            secret,
            "--filter=state=ENABLED",
            "--format=value(name)",
            capture=True,
        )
        if not versions:
            missing.append(secret)
    if missing:
        names = ", ".join(missing)
        raise SystemExit(
            "Add an enabled version manually to these Secret Manager stores, "
            f"then rerun: {names}. This script never writes secret values."
        )

    tag = datetime.datetime.now().strftime("%Y%m%d-%H%M%S")
    image = (
        f"{args.region}-docker.pkg.dev/{args.project_id}/"
        f"discord-daily-log/app:{tag}"
    )
    run("gcloud", "builds", "submit", str(ROOT), "--tag", image)

    app_service_account = (
        f"discord-daily-log@{args.project_id}.iam.gserviceaccount.com"
    )
    task_service_account = (
        f"discord-task-invoker@{args.project_id}.iam.gserviceaccount.com"
    )
    run("tofu", f"-chdir={APP}", "init")
    run(
        "tofu",
        f"-chdir={APP}",
        "apply",
        f"-var-file={args.app_var_file}",
        f"-var=project_id={args.project_id}",
        f"-var=region={args.region}",
        f"-var=image={image}",
        f"-var=app_service_account_email={app_service_account}",
        f"-var=task_service_account_email={task_service_account}",
    )

    endpoint = run(
        "tofu",
        f"-chdir={APP}",
        "output",
        "-raw",
        "interactions_endpoint",
        capture=True,
    )
    print("\nDeployment complete.")
    print("Set this Discord Interactions Endpoint URL:")
    print(endpoint)


if __name__ == "__main__":
    main()
