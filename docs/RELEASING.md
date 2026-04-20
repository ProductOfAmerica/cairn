# Releasing cairn

## Cutting a release

Releases are driven by tags. Push a semver tag and `.github/workflows/release.yml` runs goreleaser.

```bash
git tag -a v0.4.0 -m "v0.4.0"
git push origin v0.4.0
```

Goreleaser produces (per `.goreleaser.yaml`):

- Archives for `linux/amd64`, `linux/arm64`, `darwin/amd64`, `darwin/arm64`, `windows/amd64`.
- `checksums.txt` (SHA-256).
- A GitHub Release with auto-generated changelog (excludes `docs:`, `test:`, `chore:`, `ci:` commits).

Archive name template: `cairn_<version>_<os>_<arch>.{tar.gz|zip}`. Version strings in the archive name drop the leading `v` (goreleaser default).

## Updating the Scoop manifest

Per-release, two fields in `packaging/scoop/cairn.json` move:

- `version`
- `architecture.64bit.url`
- `architecture.64bit.hash` (copy the sha256 of `cairn_<ver>_windows_amd64.zip` from the release's `checksums.txt`)

`autoupdate` + `checkver` handle the rest when bumping via `scoop` tooling.

### Publishing to a bucket

Two paths:

1. **Own bucket (recommended):** create `ProductOfAmerica/scoop-cairn` on GitHub, commit `cairn.json` to repo root. Users install via `scoop bucket add cairn https://github.com/ProductOfAmerica/scoop-cairn && scoop install cairn`.
2. **ScoopInstaller/Extras:** fork `ScoopInstaller/Extras`, add `bucket/cairn.json`, open PR. Gatekept; can be rejected.

Start with (1). Migrate to (2) after a couple of releases if there's demand.

## Submitting to winget

winget manifests are PRs to `microsoft/winget-pkgs`. Do not hand-write YAML — use `wingetcreate`:

```powershell
winget install Microsoft.WingetCreate
wingetcreate new https://github.com/ProductOfAmerica/cairn/releases/download/v0.4.0/cairn_0.4.0_windows_amd64.zip
```

It scrapes the `.exe` metadata, prompts for Publisher/PackageName/etc., produces YAML under `manifests/p/ProductOfAmerica/Cairn/0.4.0/`, and opens the PR when you run `wingetcreate submit`. Subsequent releases: `wingetcreate update ProductOfAmerica.Cairn -v 0.4.1 -u <url>`.

Package identifier convention: `ProductOfAmerica.Cairn`.

## SignPath Foundation (Windows code signing)

SmartScreen warnings on the downloaded `.exe` go away with a real Authenticode signature. SignPath Foundation issues free code-signing for qualifying OSS projects.

### Application

1. Apply: <https://signpath.org/foundation/apply>.
2. Eligibility cairn already meets: public repo, MIT license, no closed-source payloads, no binary blobs.
3. Application asks for project URL, release cadence, release artifact list, maintainer identity.

Approval takes weeks, not minutes. Do step 1 in parallel with the first unsigned release — users get unsigned binaries via winget/scoop in the meantime.

### Integration once approved

SignPath issues an organization + project config and expects the release workflow to request a signing job over their API. The recipe:

1. Add GitHub Secrets: `SIGNPATH_ORG_ID`, `SIGNPATH_PROJECT_SLUG`, `SIGNPATH_SIGNING_POLICY`, `SIGNPATH_API_TOKEN`.
2. In `release.yml`, after goreleaser produces unsigned archives, extract the Windows `.exe`, submit it to SignPath via their official GitHub Action (`SignPath/GitHubActionSubmitSigningRequest@v1`), download the signed artifact, repack.
3. Re-generate `checksums.txt` so the signed `.exe` hashes are authoritative. **Important:** downstream manifests (scoop, winget) reference these hashes — regenerate after signing, not before.
4. Upload the signed archives as release assets (goreleaser's `release --skip=publish` then `gh release upload`, or post-process hook).

The SignPath action docs live at <https://about.signpath.io/documentation/github-actions>.

## Rollout order

1. Tag `v0.4.0`, push, verify goreleaser workflow succeeds, sanity-check an archive on each platform.
2. Start own scoop bucket. Test `scoop install cairn` end-to-end.
3. Submit first winget PR via `wingetcreate`. Expect reviewer feedback on manifest fields.
4. File SignPath Foundation application.
5. On SignPath approval (separate PR): wire signing into `release.yml`, re-cut the next release signed.
