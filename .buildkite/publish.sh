#!/usr/bin/env bash
set -euo pipefail

# Fail closed even if this script is accidentally invoked by a PR/manual job.
if [[ "${BUILDKITE_BRANCH:-}" != klaviyo-main || "${BUILDKITE_PULL_REQUEST:-}" != false ]]; then
  echo 'Publishing requires a non-PR build of klaviyo-main.' >&2
  exit 1
fi
if [[ ! "${BUILDKITE_COMMIT:-}" =~ ^[0-9a-f]{40}$ ]]; then
  echo 'Publishing requires a full Git commit SHA.' >&2
  exit 1
fi

kbc docker-utils build-and-publish \
  -f ssh/Dockerfile -i klaviyo-iron-proxy -t "$BUILDKITE_COMMIT" -a eng -a dev

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
