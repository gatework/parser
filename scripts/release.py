#!/usr/bin/env python3
"""Publish an immutable source artifact from successful same-commit CI."""

import hashlib
import json
import os
import pathlib
import re
import subprocess
import tarfile
import tempfile
import zipfile


def gh(*args):
    return subprocess.check_output(["gh", *args])


def api(path, body=None):
    if body is None:
        return json.loads(gh("api", path))
    return json.loads(subprocess.check_output(
        ["gh", "api", path, "--method", "POST", "--input", "-"],
        input=json.dumps(body).encode(),
    ))


root = pathlib.Path(__file__).resolve().parents[1]
os.chdir(root)
tag = os.environ["RELEASE_TAG"]
repo = os.environ["GITHUB_REPOSITORY"]
if not re.fullmatch(r"v\d+\.\d+\.\d+", tag) or tag != (root / "VERSION").read_text().strip():
    raise SystemExit("Tag must match VERSION")
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip()
tag_revision = subprocess.check_output(["git", "rev-parse", f"{tag}^{{commit}}"], text=True).strip()
if revision != tag_revision:
    raise SystemExit("Checkout does not match the release tag")
runs = api(f"repos/{repo}/actions/workflows/ci.yml/runs?branch=main&head_sha={revision}&status=success&per_page=100")
run = next((r for r in runs["workflow_runs"] if r["head_sha"] == revision
            and r["head_branch"] == "main" and r["conclusion"] == "success"
            and r["event"] in {"push", "workflow_dispatch"}), None)
if run is None:
    raise SystemExit("No successful main-branch CI run for the exact release commit")
artifacts = api(f"repos/{repo}/actions/runs/{run['id']}/artifacts?per_page=100")["artifacts"]
artifact_name = f"source-{revision}-{run['run_attempt']}"
matches = [a for a in artifacts if a["name"] == artifact_name and not a["expired"]]
if len(matches) != 1:
    raise SystemExit("Expected exactly one non-expired CI source artifact")
artifact = matches[0]
archive_name = f"parser-{tag}.tar.gz"
expected_assets = {archive_name, "REVISION", "SHA256SUMS"}

with tempfile.TemporaryDirectory(prefix="parser-release-") as directory:
    work = pathlib.Path(directory)
    artifact_zip = work / "artifact.zip"
    artifact_zip.write_bytes(gh("api", f"repos/{repo}/actions/artifacts/{artifact['id']}/zip"))
    payload = work / "payload"
    payload.mkdir()
    with zipfile.ZipFile(artifact_zip) as archive:
        if len(archive.namelist()) != len(expected_assets) or set(archive.namelist()) != expected_assets:
            raise SystemExit("Unexpected files in CI artifact")
        archive.extractall(payload)
    if (payload / "REVISION").read_text().strip() != revision:
        raise SystemExit("Artifact revision mismatch")
    checksums = {}
    for line in (payload / "SHA256SUMS").read_text().splitlines():
        digest, filename = line.split("  ", 1)
        if filename in checksums or not re.fullmatch(r"[0-9a-f]{64}", digest):
            raise SystemExit("Invalid checksum manifest")
        checksums[filename] = digest
    if set(checksums) != expected_assets - {"SHA256SUMS"}:
        raise SystemExit("Unexpected checksum manifest")
    for name, digest in checksums.items():
        if hashlib.sha256((payload / name).read_bytes()).hexdigest() != digest:
            raise SystemExit(f"Checksum mismatch: {name}")
    with tarfile.open(payload / archive_name) as archive:
        module = archive.extractfile(f"parser-{tag}/go.mod").read().decode()
        if module.splitlines()[0] != "module github.com/gatework/parser":
            raise SystemExit("Archive has the wrong module path")
        if archive.extractfile(f"parser-{tag}/VERSION").read().decode().strip() != tag:
            raise SystemExit("Archive version mismatch")

    releases = api(f"repos/{repo}/releases?per_page=100")
    release = next((r for r in releases if r["tag_name"] == tag), None)
    if release is None:
        notes = (
            (root / "CHANGELOG.md").read_text().split(f"## {tag}\n", 1)[1].split("\n## ", 1)[0].strip()
            + f"\n\nCommit: `{revision}`\n\nCI: {run['html_url']}\n\nSource artifact ID: `{artifact['id']}`\n"
        )
        release = api(f"repos/{repo}/releases", {
            "tag_name": tag, "target_commitish": revision, "name": tag,
            "body": notes, "draft": True, "prerelease": False,
        })
    if release["draft"]:
        existing_names = {a["name"] for a in release["assets"]}
        if existing_names - expected_assets:
            raise SystemExit("Draft contains unexpected assets; refusing to modify it")
        gh("release", "upload", tag, "--repo", repo, "--clobber", *(str(payload / name) for name in sorted(expected_assets)))
    downloaded = work / "downloaded"
    gh("release", "download", tag, "--repo", repo, "--dir", str(downloaded))
    if {p.name for p in downloaded.iterdir()} != expected_assets:
        raise SystemExit("Release asset list differs from CI artifact")
    for name in expected_assets:
        if (downloaded / name).read_bytes() != (payload / name).read_bytes():
            raise SystemExit(f"Release download differs from CI artifact: {name}")
    if release["draft"]:
        gh("release", "edit", tag, "--repo", repo, "--draft=false", "--latest")
    published = api(f"repos/{repo}/releases/tags/{tag}")
    if published["draft"] or published["prerelease"]:
        raise SystemExit("Release publication was not confirmed")
    print(published["html_url"])
