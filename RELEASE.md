# Release Process

Before changing `manifest.json`, creating a tag, building release artifacts, or publishing a GitHub release, state the proposed next version and justify it.

The version rationale must include:

- The current latest release and tag.
- The scope of the changes being released.
- Why the proposed `MAJOR.MINOR.PATCH` level fits semantic versioning expectations.
- The exact manifest `Version` value to use, including the fourth Elgato build component.
- The exact Git tag to create.

Wait for explicit approval of that version rationale before making any release-version changes or running release commands.

When a hardware test has been approved and the related change is committed and pushed, handle the corresponding GitHub issue as part of the same publish flow. Close it when the release/publish completes, or update it with the exact remaining blocker if it cannot be closed yet.

Release notes must follow the same structure as recent releases:

- Use a user-facing feature or "What's changed" heading.
- Group entries under sections such as "New features", "Improvements", or "Bug fixes" when useful.
- Always include a "Downloads" section listing both release artifacts.
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

### Finding the current Marketplace version

Do not ask which version is live on the Marketplace; look it up. The plugin's public listing is:

the **HWiNFO** product on the Elgato Marketplace (still on the exension maker account until the transfer completes); find it via `https://marketplace.elgato.com/search?q=hwinfo`

Fetch that page and read the **Version** field (it also shows the "Last Updated" date). That value is the currently published Marketplace version. Write the Marketplace changelog as the Windows-relevant delta between it and the version now being published. For example, if the listing shows `1.9.1` and you are publishing `2.0.0`, the changelog covers only what changed for Windows between 1.9.1 and 2.0.0.

## Steps

1. Confirm the release scope and current latest release.
2. Propose and justify the next version.
3. Wait for explicit approval of the version rationale.
4. Update `Version` in `com.exension.hwinfo.sdPlugin/manifest.json` to `MAJOR.MINOR.PATCH.0`.
5. Commit and push the release changes.
6. Handle the GitHub issue or issues included in the committed scope.
7. Run `make release`.
8. Create the GitHub release with tag `vMAJOR.MINOR.PATCH`, English release notes, and the artifact attached:
   - `com.exension.hwinfo.streamDeckPlugin`
9. Confirm that the issue state matches the published release scope.
