# Spec Delta

## ADDED Requirements

### Requirement: Proxy connection
A profile SHALL connect to its server either directly (the default) or through a SOCKS5 proxy given by host, port (default 1080), and an optional username. The proxy SHALL resolve the server's host name. Host keys SHALL be verified for the server's host and port, not the proxy's. The proxy password SHALL be stored like the profile's other secrets: only in the operating system's credential store, asked for when the store cannot provide it, and removed with the profile.

#### Scenario: Server reachable only through a proxy
- **WHEN** a profile has host "dev-box.internal", which only the proxy can resolve, and a SOCKS5 proxy at 127.0.0.1 port 1080, and the user selects Test
- **THEN** the app connects through the proxy, verifies the host key of "dev-box.internal", and reports success with the resolved remote directory

#### Scenario: Proxy refuses the sign-in
- **WHEN** the proxy rejects the profile's proxy username or password
- **THEN** the test reports a failure at the "connecting through the proxy" step that names the proxy, and uploads to that profile fail without retrying

#### Scenario: Proxy unreachable
- **WHEN** an upload starts while the proxy is not running
- **THEN** the upload is retried like other network failures, and if it still fails its error names the proxy

#### Scenario: Proxy password not stored
- **WHEN** a profile has a proxy username but the credential store has no proxy password for it
- **THEN** the app asks for the proxy password, uses it for this session only, and retries the uploads that failed for want of it

#### Scenario: Invalid proxy
- **WHEN** the user saves a profile with a SOCKS5 proxy whose host is empty or whose port is outside 1–65535
- **THEN** the profile is not saved and the invalid proxy field shows an error message
