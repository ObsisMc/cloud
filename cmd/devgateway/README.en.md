# devgateway: local development credential bridge

[中文](README.md) | [English](README.en.md)

## Responsibility

A development-only credential bridge for the web frontend that reproduces the production topology "browser → gateway → cloud":

- `GET /devgateway/login?source=&subject=&display=`: signs short-lived dual JWTs (service=gateway plus caller-bound user, 5 minutes) with a local Ed25519 private key and returns `{serviceToken, userToken, expiresAt}`;
- every other path is proxied verbatim to cloud (default `http://127.0.0.1:8080`), forwarding the dual credentials the browser attached.

The private key never enters frontend code. It defaults to `.local/minttoken/keys/private.pem`; a missing key is generated with its `public.pem` written out for the `auth.keys` trust configuration. **Never deploy outside a local machine.**

## Usage

```powershell
go run ./cmd/devgateway -addr 127.0.0.1:8090 -cloud http://127.0.0.1:8080
```

The Vite dev server proxies `/api`, `/internal`, `/healthz` and `/devgateway` to this process. Production uses a real gateway for signing and forwarding; this command is not part of it.
