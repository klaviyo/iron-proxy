# Klaviyo builds

The fork's `iron-proxy` Buildkite pipeline loads `.buildkite/pipeline.yml`.
It runs HTTP unit tests with the race detector and vet, then builds an AMD64
image. When the separately developed `ssh/` module is present, the same
pipeline tests and packages `iron-ssh` too; pipeline onboarding does not depend
on that feature being merged first.

PRs and other branches build without publishing. Non-PR builds of
`klaviyo-main` publish `klaviyo-iron-proxy:<full-commit-sha>` to eng and dev ECR
using `kbc docker-utils build-and-publish`. CI owns ECR repository creation and
policies. `iron-proxy-image-refs.txt` records both registry references by digest.
Use the eng digest as `IRON_PROXY_IMAGE` when building Centaur's proxy wrapper.
Publishing this image does not deploy or automatically advance Centaur.

Register using the same CLI workflow and agent cluster as Centaur:

```sh
klaviyocli kcd admin config-pipeline iron-proxy \
  --gh-repo iron-proxy --default-branch klaviyo-main --cluster default
```

Creation starts in pre-release mode, even with `--live`. After PR validation
and merge, run the command again with `--live` to enable default-branch builds
and commit statuses. The CLI configures the shared bootstrap template, GitHub
webhook, and Everyone Build & Read access; no Terraform registration is needed.
It requires `BUILDKITE_API_TOKEN` and GitHub webhook permissions.
