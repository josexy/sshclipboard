# Tasks

## 1. Storage and secrets

- [x] 1.1 Add `store.Proxy` to profiles and its columns to the v1 schema `0001_init.sql` (type, host, port, username, with CHECK constraints), and verify the constraints and the repo round-trip
- [x] 1.2 Add the `proxy-password` secret kind, removed with the profile, and verify Delete clears it

## 2. Connecting (`internal/sshconn`, `internal/uploader`)

- [x] 2.1 Add an in-process SOCKS5 proxy to `sshtest` (no sign-in or RFC 1929 sign-in, CONNECT to IPv4/IPv6/names, names only it resolves, recorded targets)
- [x] 2.2 Dial through the profile's SOCKS5 proxy with remote name resolution and the dial timeout covering the proxy handshake, report `StepProxy` failures naming the proxy, mark refused sign-ins with `ErrProxyAuth`, and ask for a missing proxy password; verify success, the server's host key, sign-in success/refusal/missing, proxy down, server unreachable behind the proxy, a silent proxy timing out, and Check through the proxy
- [x] 2.3 Retry `StepProxy` network failures, but not refused sign-ins or a missing proxy password, and verify the classification

## 3. Controller and UI

- [x] 3.1 Validate the proxy fields, save, duplicate, and delete the proxy password, flag it as saved, use the typed one in Test connection, and retry entries once it is provided; verify an upload through the proxy end to end including the prompt
- [x] 3.2 Add the proxy fields to the Servers form (shown for SOCKS5 only, never prefilling the password), the proxy password prompt title, and the proxy step text; verify with `ui.Tester`
- [x] 3.3 Document the proxy in the README (setup, defaults, privacy, troubleshooting) and add item 19 to `docs/manual-test.md`

## 4. Verification

- [x] 4.1 Run `CGO_ENABLED=0 go test ./...`, `go vet ./...`, `GOOS=windows go vet ./...`, `GOOS=windows GOARCH=arm64 go build ./...`, and `go tool mygo vet .`, and verify all pass
- [ ] 4.2 Run manual test item 19 on Windows against a server behind an `ssh -D` tunnel
