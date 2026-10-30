#!/usr/bin/env bash
set -euo pipefail

mode=${1:-build}
if [[ "$mode" != build && "$mode" != publish ]]; then
  echo 'Usage: build.sh [build|publish]' >&2
  exit 1
fi
if [[ "$mode" == publish ]]; then
  if [[ "${BUILDKITE_BRANCH:-}" == klaviyo-main && "${BUILDKITE_PULL_REQUEST:-}" == false ]]; then
    :
  elif [[ "${BUILDKITE_BRANCH:-}" == feat/ssh-proxy-consolidated && ( "${BUILDKITE_PULL_REQUEST:-}" == false || "${BUILDKITE_PULL_REQUEST:-}" == 6 ) ]]; then
    :
  else
    echo 'Publishing requires main or the approved SSH PR branch.' >&2
    exit 1
  fi
fi
if [[ ! "${BUILDKITE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'Building requires a full Git commit SHA.' >&2
  exit 1
fi

repo_dir=$(cd "$(dirname "$0")/.." && pwd)
build_context=$(mktemp -d)
compiler_container=""
trap 'if [[ -n "$compiler_container" ]]; then docker rm -f "$compiler_container" >/dev/null; fi; rm -rf "$build_context"' EXIT

# Match upstream's GoReleaser binary layout, then reuse its release Dockerfile.
compiler_container=$(docker create --user "$(id -u):$(id -g)" \
  -v "$repo_dir:/src:ro" -w /src \
  -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 -e GOFLAGS=-mod=readonly \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -e "IRON_PROXY_BUILD_VERSION=$BUILDKITE_COMMIT" \
  golang:1.27.3-alpine sh -ec \
  'mkdir -p /tmp/output/linux/amd64
   go build -trimpath -ldflags "-s -w -X github.com/ironsh/iron-proxy/internal/version.Version=$IRON_PROXY_BUILD_VERSION" -o /tmp/output/linux/amd64/iron-proxy ./cmd/iron-proxy')
docker start --attach "$compiler_container"
docker cp "$compiler_container:/tmp/output/linux" "$build_context/"
cp "$repo_dir/Dockerfile.release" "$build_context/Dockerfile.release"

if [[ "$mode" == build ]]; then
  docker build --platform linux/amd64 -f "$build_context/Dockerfile.release" \
    -t "klaviyo-iron-proxy:$BUILDKITE_COMMIT" "$build_context"
  exit 0
fi

(
  cd "$build_context"
  kbc docker-utils build-and-publish \
    -f Dockerfile.release -i klaviyo-iron-proxy -t "$BUILDKITE_COMMIT" \
    -b TARGETARCH=amd64 -a eng -a dev
)

# Centaur must consume a digest, never a floating branch/latest tag.
: > iron-proxy-image-refs.txt
for registry in 340433579684 439508887365; do
  digest=$(aws ecr batch-get-image --region us-east-1 --registry-id "$registry" \
    --repository-name klaviyo-iron-proxy --image-ids "imageTag=$BUILDKITE_COMMIT" \
    --query 'images[0].imageId.imageDigest' --output text)
  if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "No valid image digest returned for registry $registry." >&2
    exit 1
  fi
  printf '%s.dkr.ecr.us-east-1.amazonaws.com/klaviyo-iron-proxy@%s\n' "$registry" "$digest" >> iron-proxy-image-refs.txt
done
cat iron-proxy-image-refs.txt
