# Proposal

## Why

Many servers are only reachable through a proxy: a bastion that the user tunnels through with `ssh -D`, or a corporate SOCKS5 proxy. SSHClipboard connects to servers directly, so it can't upload to them.

## What Changes

- A profile can connect through a **SOCKS5 proxy**: a proxy type (none or SOCKS5), the proxy's host and port (default 1080), and an optional username. The proxy password is a secret like the others: kept in the OS credential store, asked for when the store can't provide it, copied by Duplicate, and removed with the profile.
- The proxy resolves the server's host name, so names that only resolve behind it work. Host keys are still verified for the server's host and port, not the proxy's.
- A failure at the proxy is reported as its own connection step ("connecting through the proxy"), naming the proxy. The proxy's network failures are retried like the server's; a refused proxy sign-in or a missing proxy password is not.
- The proxy columns go into the v1 schema (`0001_init.sql`). The app is unreleased, so there is no migration or upgrade path.

SOCKS5 is the only proxy type for now. HTTP CONNECT and SSH jump hosts (`ProxyJump`) are out of scope; the proxy type leaves room for them.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `ssh-profiles`: profiles gain a proxy; connecting, connection tests, and secret storage cover it.

## Impact

- **Code**: `internal/store` (types, profiles repo, the v1 schema), `internal/secret` (the `proxy-password` kind), `internal/sshconn` (dialing through the proxy, `StepProxy`, `ErrProxyAuth`, a test SOCKS5 proxy in `sshtest`), `internal/uploader` (retry classification), `internal/core` (validation, saving and copying the proxy password, the prompt), `internal/view` (Servers form fields, prompt title, step text), and `locales/en.json`.
- **Dependencies**: `golang.org/x/net/proxy`; `golang.org/x/net` was already an indirect dependency and becomes a direct one.
