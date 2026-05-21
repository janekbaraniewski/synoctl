# Security policy

## Supported versions

synoctl follows semantic versioning. Security fixes land on the latest minor
release line. Patch releases are cut as needed and published to the
[GitHub releases page](https://github.com/janekbaraniewski/synoctl/releases).

| Version line | Supported |
|---|---|
| 0.2.x | active |
| < 0.2 | end of life |

We aim to keep CVE windows short. If a high-severity issue is reported against
an in-support release line, expect a patch within a few days.

## Reporting a vulnerability

Please do not file public GitHub issues for security problems.

Use [GitHub private vulnerability reporting](https://github.com/janekbaraniewski/synoctl/security/advisories/new)
instead. It opens a private advisory channel between you and the maintainers.

If you cannot use that channel, email `security@baraniewski.com` with:

- A clear description of the vulnerability and its impact
- Steps to reproduce, or a proof of concept
- The version of synoctl where you observed it (`synoctl version`)
- The platform and Go version (`go version`)
- Any suggested mitigation, if you have one

You will get an acknowledgement within 3 business days, an initial assessment
within 7 business days, and updates at least weekly until the issue is resolved
or marked out of scope.

## Disclosure

We follow a coordinated disclosure model:

1. The reporter and maintainers privately scope the vulnerability and produce a fix.
2. A patched release is published.
3. A GitHub Security Advisory is published with a CVE when applicable and credit to the reporter.
4. After 30 days the original report is made public, unless extended by mutual agreement.

Researchers acting in good faith are welcome and credited in the advisory unless
they prefer otherwise.

## Scope

In scope:

- The `synoctl` binary, including the TUI, onboarding flow, and CLI subcommands
- The DSM client, including auth, sessions, OTP step-up, credentials, and device tokens
- Keychain integration and config-on-disk layout
- Published Homebrew tap and release artifacts

Out of scope:

- Issues that require local access to a logged-in user's machine to exploit
- Reports against DSM itself or third-party Synology services
- Theoretical issues with no demonstrated impact

## Hardening

This project participates in:

- [GitHub Dependabot](https://github.com/dependabot) for dependency updates and security advisories
- [GitHub CodeQL](https://codeql.github.com/) for static analysis
- [`govulncheck`](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) for Go-specific vulnerability scanning
- [OpenSSF Scorecard](https://scorecard.dev/) for supply-chain hygiene
- [Sigstore cosign](https://www.sigstore.dev/) keyless signing of release binaries

Release checksums are published alongside binaries on the
[releases page](https://github.com/janekbaraniewski/synoctl/releases).
