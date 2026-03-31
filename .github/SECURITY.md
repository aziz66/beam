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

## Disclosure Policy

Once a fix is ready and released, we will publish a security advisory crediting the reporter (unless they prefer to remain anonymous).
