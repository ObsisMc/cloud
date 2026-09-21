# cmd/devkeys: development key generator

[中文](README.md) | [English](README.en.md)

`cmd/devkeys` exists only for local development: it lets `task dev` start the real Gateway and Cloud on a laptop without anyone running `openssl` by hand. Production keys come from the deployment infrastructure; this command plays no part there.

## Responsibilities

- Generates two purpose-separated Ed25519 key pairs under `.local/gateway/` (`-dir` overrides): `gateway-service.key`/`.pem` and `user-identity.key`/`.pem`. The PKCS#8 private keys go to the Gateway (`configs/gateway.yaml`); the PKIX public keys go to Cloud (`auth.keys` in `configs/config.yaml`).
- Generates a random 32-byte `gateway-pkce.key`.
- Points out a missing `github-client-secret` file; the developer writes the GitHub OAuth App client secret there and provides the client id through `GATEWAY_GITHUB_CLIENT_ID`.

## Boundaries and invariants

- **Idempotent, never overwrites**: existing files are kept; a pair's private and public key must exist together, otherwise the command refuses and asks for both to be removed, so the Gateway and Cloud never trust drifting keys.
- **Never prints private material**: only "generated"/"kept" file names.
- **Writes only under `.local/`**: the directory is ignored by Git; nothing generated reaches the repository, logs or the database.
- **Not for deployment**: files are `0600`, but there is no rotation, custody or distribution.

See the [cmd overview](../README.en.md) and the [Gateway documentation](../../docs/gateway.md).
