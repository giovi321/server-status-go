---
title: "Releasing"
description: "Cut a release so hosts can self-update"
---

Releases are what the [self-update](../../control/self-update/) flow pulls from. A release is produced entirely by pushing a version tag.

## Cut a release

```bash
git tag -a v1.2.0 -m "v1.2.0"
git push origin v1.2.0
```

Pushing a tag matching `v*` triggers `.github/workflows/release.yml`, which:

1. Builds two static binaries, `server-status-linux-amd64` and `server-status-linux-arm64`, stamping the version from the tag name (`-ldflags -X .../version.Version=<tag>`)
2. Writes a `sha256sum` sidecar next to each: `server-status-linux-<arch>.sha256`
3. Creates the GitHub release with all four files attached and auto-generated notes

## Why the layout matters

The self-update code looks for an asset named exactly `server-status-linux-<goarch>` and a sibling `<asset>.sha256`, and treats the tag as the version. The release workflow produces exactly that, so a host running an older build sees the new tag, downloads the matching asset, verifies it against the checksum, and swaps atomically. If you build releases by hand, reproduce this naming or self-update will not find the asset.

## Versioning

`internal/version.Version` defaults to `dev` and is overridden at build time. A binary built without the ldflag reports `dev`, which never matches a real release tag, so the update command on a `dev` build will always attempt to move to the latest tagged release.

## After releasing

- Confirm the release shows both binaries and both `.sha256` files
- On a host, press the **Agent** update button in Home Assistant (or send the `update` command) and watch it move to the new version. See [Self-update](../../control/self-update/)
