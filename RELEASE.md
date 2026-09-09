# Release Process

Before changing `manifest.json`, creating a tag, building release artifacts, or publishing a GitHub release, state the proposed next version and justify it.

The version rationale must include:

- The current latest release and tag.
- The scope of the changes being released.
- Why the proposed `MAJOR.MINOR.PATCH` level fits semantic versioning expectations.
- The exact manifest `Version` value to use, including the fourth Elgato build component.
- The exact Git tag to create.

Wait for explicit approval of that version rationale before making any release-version changes or running release commands.

Issues are issues, in every workflow. A GitHub issue is the tracked work item with its own lifecycle, not a note, an internal document, or a passing remark: it is created (text approved first), labeled and assigned, referenced from the branch, commits, PRs, and release notes, updated in the issue itself on progress or blockers, and closed only as part of the publish flow after explicit approval. Every workflow runs through an issue, so work that has no issue gets one first. Internal documents under `docs/` do not replace the issue.

When a hardware test has been approved and the related change is committed and pushed, handle the corresponding GitHub issue as part of the same publish flow. Close it when the release/publish completes, or update it with the exact remaining blocker if it cannot be closed yet.

Release notes must follow the same structure as recent releases:

- Use a user-facing feature or "What's changed" heading.
- Group entries under sections such as "New features", "Improvements", or "Bug fixes" when useful.
- Always include a "Downloads" section listing the release artifact (`com.moeilijk.hwinfo-X.Y.Z.streamDeckPlugin`); there is no Linux artifact for this plugin.
- Do not include a validation section; validation belongs in the final assistant summary, not in public release notes.

Elgato Marketplace release notes are separate from GitHub release notes. The plugin is Windows-only, so do not mention Linux, OpenDeck, release artifacts, downloads, validation, GitHub tags, commits, or hardware-test details there.

Use the existing Marketplace v1.7 style:

```markdown
## What's new

### Feature title
Short user-facing explanation.

### Feature title
Short user-facing explanation.

## Fixes
- Short fix or compatibility note.
```

For Marketplace updates, write only the Windows-relevant delta since the currently published Marketplace version.

### Marketplace status

A moeilijk Marketplace listing "HWiNFO" was submitted through the Maker Console on 26 July 2026 at version 3.0.0 (auto-publish enabled) and is awaiting Elgato review; do not reference it as published until it appears on the Marketplace. The older "HWiNFO" listing on the Marketplace belongs to the exension maker account and is not ours: never write changelogs against it or propose releasing under its identity. Once the moeilijk listing is published, look up its version on its product page (Version and "Last Updated" fields) and write the Marketplace changelog as the Windows-relevant delta since that version.

## Antivirus check

Every packed artifact is scanned before it is published (issue #116). `make release` runs `make av-check` on the artifact it just packed; a detection fails the build, and the release must not be published until the cause is understood. `make av-check` can also be run on its own, and `scripts/av-check.sh <package>` on any package, for example a previously published asset after a definitions update.

The check scans the `.streamDeckPlugin` package and the `hwinfo.exe` and `hwinfo-bridge.exe` inside it:

- Microsoft Defender engine with the current definitions, offline, under wine64 (`scripts/defender-scan.sh`). This always runs and is the hard gate. It covers signatures and local heuristics only; Defender's cloud/ML verdicts (detection names ending in `!ml`) need a real Windows with Defender and cloud protection. It needs `wine64`, `x86_64-w64-mingw32-gcc`, `7z` and `curl`; the first run downloads about 220 MB of definitions into `~/.cache/defender-scan`, refreshed after 24 hours or with `--update`.
- The real Microsoft Defender, cloud protection included, in a disposable Windows 11 VM on QEMU/KVM (`scripts/defender-vm.sh`), run from Linux/WSL without anything on the Windows side. This is the only scan that produces Defender's cloud/ML verdicts. One-time setup: `scripts/defender-vm.sh create --iso <Windows 11 ISO>` installs Windows unattended and an OpenSSH server for host control, `prepare` updates the Defender platform and definitions and validates the cloud connection, `clean` freezes that disk as the baseline. Every `scan` boots a throwaway copy of the baseline, copies the files in (real-time protection judges them on write), updates definitions, runs `MpCmdRun.exe` without remediation, collects detections, versions and the Defender event log, and discards the copy. `make av-check` runs it automatically when the baseline exists (`~/.cache/defender-vm`, or `DEFENDER_VM_DIR`). Needs `qemu-system-x86_64` with `/dev/kvm`, `swtpm` and `xorrisofs`; the VM takes about 25 GB.
- VirusTotal hash lookup (`scripts/virustotal-file.js`) when `VT_API_KEY` is set.
- OPSWAT MetaDefender Cloud and Kaspersky OpenTIP (`scripts/av-online-check.js`) when `METADEFENDER_APIKEY` and `OPENTIP_APIKEY` are set.

Nothing is uploaded unless `AV_CHECK_FLAGS=--upload` is passed; uploading shares the file with the vendors before the release exists. Reports are written to `build/av-reports/<package>-<timestamp>/` with a `summary.md`.

Scan the package, not only the executables: a signature can match the zip container while every file inside is clean (that happened with 3.1.1).

### Reporting a false positive

No vendor offers an API for this; each has a form or an address. Report as the software developer, with the file, its SHA-256, the detection name and the source repository.

- Microsoft Defender: https://www.microsoft.com/en-us/wdsi/filesubmission (choose "Software developer"; track at https://www.microsoft.com/wdsi/submissionhistory)
- Bitdefender: https://www.bitdefender.com/submit/
- Kaspersky: https://opentip.kaspersky.com/ (submit for reanalysis) or newvirus@kaspersky.com
- Trend Micro: https://www.trendmicro.com/en_us/about/legal/detection-reevaluation.html
- Symantec/Broadcom: https://symsubmit.symantec.com/
- Sophos: https://support.sophos.com/support/s/filesubmission
- McAfee/Trellix: datasubmission@trellix.com
- ESET: samples@eset.com
- Avast: https://www.avast.com/report-false-positive; AVG: https://www.avg.com/false-positive-file-form
- Malwarebytes: https://forums.malwarebytes.com/forum/122-false-positives/
- F-Secure: https://www.f-secure.com/en/business/support-and-downloads/submit-a-sample

## Steps

1. Confirm the release scope and current latest release.
2. Propose and justify the next version.
3. Wait for explicit approval of the version rationale.
4. Update `Version` in `com.moeilijk.hwinfo.sdPlugin/manifest.json` to `MAJOR.MINOR.PATCH.0`.
5. Commit and push the release changes.
6. Handle the GitHub issue or issues included in the committed scope.
7. Run `make release`. It packs the artifact and runs the antivirus check on it; a detection stops the release.
8. Create the GitHub release with tag `vMAJOR.MINOR.PATCH`, English release notes, and the artifact attached:
   - `com.moeilijk.hwinfo-MAJOR.MINOR.PATCH.streamDeckPlugin`
9. Confirm that the issue state matches the published release scope.
