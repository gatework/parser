#!/usr/bin/env python3
"""Archive the checked-out, verified commit without rebuilding its contents."""

import hashlib
import pathlib
import re
import subprocess


root = pathlib.Path(__file__).resolve().parents[1]
version = (root / "VERSION").read_text().strip()
if not re.fullmatch(r"v\d+\.\d+\.\d+", version):
    raise SystemExit("VERSION must be vMAJOR.MINOR.PATCH")
subprocess.run(["git", "diff", "--exit-code", "HEAD", "--"], cwd=root, check=True)
revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=root, text=True).strip()
dist = root / "dist"
dist.mkdir(exist_ok=True)
archive = dist / f"parser-{version}.tar.gz"
subprocess.run(
    ["git", "archive", "--format=tar.gz", f"--prefix=parser-{version}/", f"--output={archive}", revision],
    cwd=root,
    check=True,
)
(dist / "REVISION").write_text(revision + "\n")
assets = [archive, dist / "REVISION"]
(dist / "SHA256SUMS").write_text("".join(
    f"{hashlib.sha256(asset.read_bytes()).hexdigest()}  {asset.name}\n" for asset in assets
))
print(f"Packaged {version} from {revision}")
