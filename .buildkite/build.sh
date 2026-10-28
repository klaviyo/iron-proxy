#!/usr/bin/env bash
set -euo pipefail

mode=${1:-build}
if [[ "$mode" != build && "$mode" != publish ]]; then
  echo 'Usage: build.sh [build|publish]' >&2
  exit 1
fi
if [[ "$mode" == publish ]]; then
  if [[ "${BUILDKITE_BRANCH:-}" != klaviyo-main || "${BUILDKITE_PULL_REQUEST:-}" != false ]]; then
    echo 'Publishing requires a non-PR build of klaviyo-main.' >&2
    exit 1
  fi
fi
if [[ ! "${BUILDKITE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'Building requires a full Git commit SHA.' >&2
  exit 1
fi

repo_dir=$(cd "$(dirname "$0")/.." && pwd)
build_context=$(mktemp -d)
trap 'rm -rf "$build_context"' EXIT
mkdir -p "$build_context/linux/amd64"

# Match upstream's GoReleaser binary layout, then reuse its release Dockerfile.
docker run --rm --user "$(id -u):$(id -g)" \
  -v "$repo_dir:/src:ro" -v "$build_context:/out" -w /src \
  -e CGO_ENABLED=0 -e GOOS=linux -e GOARCH=amd64 -e GOFLAGS=-mod=readonly \
  -e GOCACHE=/tmp/go-build -e GOMODCACHE=/tmp/go-mod \
  -e "IRON_PROXY_BUILD_VERSION=$BUILDKITE_COMMIT" \
  golang:1.27.3-alpine sh -ec \
  'go build -trimpath -ldflags "-s -w -X github.com/ironsh/iron-proxy/internal/version.Version=$IRON_PROXY_BUILD_VERSION" -o /out/linux/amd64/iron-proxy ./cmd/iron-proxy
   cd ssh
   go build -trimpath -ldflags "-s -w" -o /out/linux/amd64/iron-ssh ./cmd/iron-ssh'
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
  digest=$(aws ecr describe-images --region us-east-1 --registry-id "$registry" \
    --repository-name klaviyo-iron-proxy --image-ids "imageTag=$BUILDKITE_COMMIT" \
    --query 'imageDetails[0].imageDigest' --output text)
  if [[ ! "$digest" =~ ^sha256:[0-9a-f]{64}$ ]]; then
    echo "No valid image digest returned for registry $registry." >&2
    exit 1
  fi
  printf '%s.dkr.ecr.us-east-1.amazonaws.com/klaviyo-iron-proxy@%s\n' "$registry" "$digest" >> iron-proxy-image-refs.txt
done
cat iron-proxy-image-refs.txt
