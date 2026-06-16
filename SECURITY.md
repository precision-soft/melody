# Security Policy

## Supported versions

Melody is developed as three parallel module lines. **v3 is the actively maintained version**; v1 and v2 are
in maintenance mode but still receive security fixes.

| Version line                                 | Status                   | Security fixes |
|----------------------------------------------|--------------------------|----------------|
| v3.x (`github.com/precision-soft/melody/v3`) | Actively maintained      | Yes            |
| v2.x (`github.com/precision-soft/melody/v2`) | Maintenance (fixes only) | Yes            |
| v1.x (`github.com/precision-soft/melody`)    | Maintenance (fixes only) | Yes            |

Security fixes are applied to all three lines. Other bug fixes and all new features land on v3 only
(see [`CONTRIBUTING.md`](./CONTRIBUTING.md)).

## Reporting a vulnerability

**Do not open a public issue, pull request, or discussion for security-sensitive reports.**

Please report vulnerabilities privately through GitHub's private vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Select **Report a vulnerability**.
3. Provide the details below.

Include in your report:

- The affected version line(s) and tag/commit.
- Go version and operating system.
- A minimal reproduction (proof of concept where possible).
- The observed impact and your assessment of severity.
- Any relevant logs or stack traces (redact secrets).

## Disclosure process

- We will acknowledge a valid report and begin assessment.
- We aim to confirm the issue, prepare a fix, and coordinate a release across the supported version lines.
- Please give us a reasonable window to ship a fix before any public disclosure.
