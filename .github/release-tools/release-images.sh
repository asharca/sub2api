#!/usr/bin/env bash
set -euo pipefail
: "${RELEASE_VERSION:?}" "${RELEASE_SHA:?}" "${GITHUB_REPOSITORY:?}" "${RUNNER_TEMP:?}"
owner=${GITHUB_REPOSITORY%%/*}
registry="ghcr.io/${owner,,}/sub2api"
arch=amd64
args=(--platform "linux/$arch" --file ".release-context/$arch/Dockerfile"
  --label "org.opencontainers.image.version=$RELEASE_VERSION"
  --label "org.opencontainers.image.revision=$RELEASE_SHA"
  --label "org.opencontainers.image.source=https://github.com/$GITHUB_REPOSITORY"
  --tag "$registry:$RELEASE_VERSION-$arch"
  --tag "$registry:$RELEASE_VERSION")
if [[ ${DRY_RUN:-false} == true ]]; then
  args+=(--output "type=oci,dest=$RUNNER_TEMP/sub2api-$arch.oci.tar")
else
  args+=(--push)
fi
docker buildx build "${args[@]}" ".release-context/$arch"
