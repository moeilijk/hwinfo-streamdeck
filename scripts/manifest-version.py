#!/usr/bin/env python3
"""Print the release version (MAJOR.MINOR.PATCH) from the plugin manifest.

Single source of truth for artifact filenames (issue #78): the Elgato manifest
carries a 4-component version (MAJOR.MINOR.PATCH.BUILD); release artifacts and
tags use the first three components.

Usage: manifest-version.py [path/to/manifest.json]
"""
import json
import sys

path = sys.argv[1] if len(sys.argv) > 1 else "com.moeilijk.hwinfo.sdPlugin/manifest.json"
version = json.load(open(path))["Version"]
print(".".join(version.split(".")[:3]))
