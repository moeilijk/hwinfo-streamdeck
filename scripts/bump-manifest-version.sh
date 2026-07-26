#!/usr/bin/env bash
set -euo pipefail

python3 - <<'PY'
import pathlib
import re
import sys

path = pathlib.Path("com.moeilijk.hwinfo.sdPlugin/manifest.json")
state_path = pathlib.Path(".last_patch")
text = path.read_text()
pattern = r'"Version"\s*:\s*"(?P<major>\d+)\.(?P<minor>\d+)\.(?P<patch>\d+)\.(?P<build>\d+)"'
match = re.search(pattern, text)
if not match:
    sys.exit("Version not found or invalid format in manifest.json")

major = int(match.group("major"))
minor = int(match.group("minor"))
patch = int(match.group("patch"))
build = int(match.group("build"))
lineage = (major, minor, patch)

# .last_patch tracks the last committed major.minor.patch lineage.
# If major/minor/patch changed, reset build to 0.
last_lineage = None
if state_path.exists():
    raw = state_path.read_text().strip()
    m = re.fullmatch(r"(\d+)\.(\d+)\.(\d+)", raw)
    if m:
        last_lineage = (int(m.group(1)), int(m.group(2)), int(m.group(3)))

next_build = 0 if lineage != last_lineage else build + 1
new_version = f"{major}.{minor}.{patch}.{next_build}"
new_text = re.sub(pattern, f'"Version": "{new_version}"', text, count=1)
path.write_text(new_text)
state_path.write_text(f"{major}.{minor}.{patch}")
print(new_version)
PY
