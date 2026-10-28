# Iron SSH companion

`iron-ssh` uses sshpiper's SSH transport library in an isolated Go module.
The library replacement does not affect the HTTP proxy's crypto dependency.
Run it beside the HTTP proxy with the same per-sandbox network boundary.

Build from this directory: `go build ./cmd/iron-ssh`. Build an image from the
repository root: `docker build -f ssh/Dockerfile -t iron-ssh:local .`.

The `-config` YAML/JSON file contains `listen`, `host_key` (an existing Iron
secret source), `client_public_key` (registered by the trusted control plane),
`authorize_url`, and `token_env`. The environment variable named by `token_env`
is a proxy-only bearer token. Configuration and host keys must never be mounted
in the sandbox. `upstream_deny_cidrs` uses Iron's existing dial-time IP guard;
omission blocks loopback and cloud metadata. Explicit overrides are intended
for trusted operator configuration, including loopback-only integration tests.

The authorizer receives `{route, public_key}`. It must derive the principal from
the proxy token and return a grant containing `id`, `principal`, `generation`,
`destination` (`tcp://host:port` or a full `wss://host/path`), `username`,
`host_key` (authorized-key format), `private_key` (Iron secret source), and
`expires_at` (RFC3339, at most 15 minutes away). It must deny ambiguous or
unauthorized routes. Responses are limited to 1 MiB; errors never expose their
bodies or credential values. Redirects are not followed.

The paired public key is checked before authorization or any upstream dial.
Exec and SFTP channels are supported; other capabilities terminate the
connection. Both SSH host identities and WSS TLS are verified. WSS implements
the wstunnel v11 default unmasked framing and routing handshake with a fixed remote target `127.0.0.1:22`.
The JWT is protocol metadata, not an authentication credential.

The lease is renewed every five seconds. Expiry, authorization failure, or a
change to any grant field except expiry closes the active connection. Authorizer
failures fail closed. Reconnection never retries a remote command automatically.
A terminated channel does not guarantee detached remote descendants have exited.

For standalone local provisioning, replace `authorize_url`/`token_env` with
`grants_file`. That file is a YAML list of `{route, public_key, grant}` records,
with the same grant schema. It is reread for every check; removing a record
revokes active connections. Treat the grants file as trusted management input.

Run `go test -race ./...` in this module. No SSH functionality is enabled merely
by installing this binary; the embedding platform must supply the paired key,
authenticated authorizer, destination grants, and lifecycle wiring.

## Klaviyo builds

The `iron-proxy` Buildkite pipeline uses `.buildkite/pipeline.yml` on the fork.
It tests the HTTP and SSH modules with the race detector and vet, then builds
an AMD64 image. PRs and other branches build without publishing. Non-PR builds
of `klaviyo-main` use `kbc docker-utils build-and-publish` to publish
`klaviyo-iron-proxy:<full-commit-sha>` to eng and dev ECR. CI owns repository
creation and policies; no separate Terraform ECR resource is needed.

The `iron-proxy-image-refs.txt` build artifact records both registry references
by digest. Use the eng reference as `IRON_PROXY_IMAGE` when building Centaur's
`services/iron-proxy/Dockerfile`, then publish and pin that wrapper image in
Centaur's deployment. Publishing this base does not deploy or automatically
advance Centaur. Both the base's HTTP binary and `iron-ssh` come from this
checkout. The runtime image retains the pinned upstream base.

Pipeline registration is Terraform-managed in infrastructure-deployment's
`infrastructure/live/eng/buildkite/iron_proxy.tf`. Merge the fork pipeline
before applying that registration. The shared pipeline router loads the
repository's `.buildkite/pipeline.yml`; no custom bootstrap or credentials
belong in this repository. The registration uses `klaviyo-main`, builds
same-repository PRs, and refuses fork PRs.
