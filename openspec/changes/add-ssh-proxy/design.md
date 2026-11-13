# Design

## Context

`sshconn.Config.Dial` opened a TCP connection with `net.Dialer` and ran the SSH handshake over it. Profiles had no proxy setting.

## Decisions

### D1. Per-profile proxy, stored in the profile

The proxy belongs to the profile, as OpenSSH's `ProxyCommand`/`ProxyJump` belong to a `Host`, because different servers sit behind different proxies. `store.Profile.Proxy` holds the type, host, port, and username. The v1 schema (`0001_init.sql`) has `proxy_type` (`'none'` or `'socks5'`, default `'none'`), `proxy_host`, `proxy_port` (default 1080), and `proxy_username` with CHECK constraints, next to `key_path`. The app is unreleased, so the columns are part of the first schema rather than a migration. A disabled proxy keeps its fields, so turning it back on restores them.

### D2. The proxy password is a secret kind

`secret.ProxyPassword` (`profile/<id>/proxy-password`) joins `password` and `passphrase`, so it inherits the credential store, the session-only fallback, the prompt (`SecretNeededError`), Duplicate, and Delete. A username without a stored password asks for it, as RFC 1929 needs a password.

### D3. `golang.org/x/net/proxy` for SOCKS5

`proxy.SOCKS5` with the profile's `net.Dialer` as the forward dialer. It sends host names to the proxy unresolved (remote DNS, like `socks5h`). The dial runs under `context.WithTimeout(DialTimeout)`, because the dialer's own timeout only covers the TCP connect to the proxy, not its handshake. The SSH handshake and host-key check use the server's `host:port`, so trust never attaches to the proxy.

### D4. A proxy step, and which proxy failures are permanent

Failures through the proxy are `DialError{Step: StepProxy}` so the connection test says where it failed. The library's `socks connect tcp a->b:` prefix is replaced with `SOCKS5 proxy <host:port>:`. The uploader retries `StepProxy` failures like `StepConnect` ones (the proxy or the server behind it may be restarting), except sign-in refusals, marked with `sshconn.ErrProxyAuth`, and a missing proxy password. The SOCKS library reports sign-in refusals as plain errors, so they are recognized by their text; a change of wording would only make them retried.

## Risks / Trade-offs

- [Recognizing sign-in refusals by text] → Contained in `signInRefused`; the failure mode is a few extra retries, not a wrong result.
- [SOCKS5 only] → The proxy type is an enum with a CHECK constraint; HTTP CONNECT or jump hosts are a new value and dialer.
