# Standalone release

The fork starts its independent stable release series at `v1.0.0`. Publish from `asharca/sub2api` on `release/standalone`, not from `feat/conversation-storage` or either upstream repository. The release tag must be a bare `vMAJOR.MINOR.PATCH` with major ≥ 1, and must match `backend/cmd/server/VERSION` (without the `v`) at the tagged commit. The workflow checks that the commit is in `origin/release/standalone`; it never commits or pushes `VERSION` back to any branch.

The standalone profile (`.goreleaser.standalone.yaml`) preserves the fork's release artifacts:

- A stable GitHub Release with `sub2api_1.0.0_linux_amd64.tar.gz`, `checksums.txt`, and portable re-login runtime archives for Linux amd64 and arm64.
- The Linux amd64 image at `ghcr.io/asharca/sub2api:1.0.0` and `ghcr.io/asharca/sub2api:1.0.0-amd64`. It does not publish to Docker Hub or update moving image tags such as `latest`.

The workflow builds the frontend once, embeds it in the Go binary, and verifies the archive's source commit, target, version and SHA256 before reusing it for the release and Docker image. All build jobs use the source commit resolved by `prepare`; GoReleaser OSS publishes the existing archive and checksum without compiling again. The optional Telegram announcement requires an explicit opt-in on manual dispatch.

For a real release, commit the version change on `release/standalone`, push that branch to `origin` first, then create and push an annotated `v1.0.0` tag on the intended commit. Tag pushes trigger the release automatically. If dispatching manually, select `release/standalone` as the workflow ref and enter the existing release tag.

## Validate without publication

After `release/standalone` is available in the fork (and GitHub Actions recognizes `release.yml` as a dispatchable workflow):

```bash
gh workflow run release.yml --repo asharca/sub2api --ref release/standalone \
  -f tag=release/standalone -f dry_run=true
```

A dry run reads `VERSION` from the selected source without requiring a new tag. It builds and verifies the archive, portable runtimes and GHCR image, exports the image locally as an OCI archive instead of pushing it, and skips registry login, GitHub Release publication and Telegram notifications. See `release-dry-run-report` for the checksum and metadata.

Helper checks:

```bash
python -m pip install -r .github/release-tools/requirements-release.txt
python -m unittest discover -s .github/release-tools -p 'test_release_matrix.py'
bash -n .github/release-tools/release-images.sh
```
