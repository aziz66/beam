# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| Latest  | ✅        |

## Reporting a Vulnerability

**Please do not report security vulnerabilities through public GitHub issues.**

Report security issues privately via [GitHub Security Advisories](https://github.com/aziz66/beam/security/advisories/new). This allows us to assess and patch the issue before public disclosure.

Please include:
- A description of the vulnerability and its potential impact
- Steps to reproduce
- Any proof-of-concept or exploit code (if applicable)

We aim to respond within **72 hours** and will keep you updated throughout the process.

## Scope

Areas of particular interest:
- E2E encryption implementation (`web/js/crypto.js`, `cli/main.go`)
- SSRF protection in the link preview endpoint (`internal/preview/`)
- WebSocket authentication and rate limiting (`internal/hub/`)
- Passphrase handling for pinned rooms (`internal/room/`)
- Content Security Policy and security headers (`main.go`)

## Known Limitations

**Trust in the Host:** Even with end-to-end encryption, you are trusting the server to deliver the correct frontend code. A compromised server could theoretically serve a malicious JavaScript payload designed to steal the keys. This is a fundamental limitation of web-based E2E encryption, not a flaw unique to Beam.

Mitigations in place:
- `script-src 'self'` CSP blocks all external script injection
- `connect-src 'self'` CSP prevents exfiltration of keys to external domains, even if malicious JS were executed
- The frontend is embedded directly in the server binary (`go:embed`) — an attacker must replace the binary itself, not just a file on disk
- The server is fully open source — anyone can audit the code and verify what the binary serves

For the highest level of trust, self-host Beam by building from source and auditing the code yourself.

## Disclosure Policy

Once a fix is ready and released, we will publish a security advisory crediting the reporter (unless they prefer to remain anonymous).
