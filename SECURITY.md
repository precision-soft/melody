# Security Policy

## Supported versions

Melody is developed as parallel module lines. **v3 is the actively maintained version**; v1 and v2 are in maintenance mode and receive security fixes and patch-level defect fixes until v4 is released.

**Every supported line receives security fixes**, whether it is actively maintained or feature-frozen; that is what the maintenance mode is for. Which lines those are, when each was released and which event ends its support is one table, in [`README.md`](./README.md#versions--project-status) — it is not repeated here, so the two cannot drift apart.

Security fixes are applied to every line still inside its support window. Other defect fixes are back-ported to v1 and v2 when they fit a patch release; new features land on v3 only (see [`CONTRIBUTING.md`](./CONTRIBUTING.md)).

## Reporting a vulnerability

**Do not open a public issue, pull request, or discussion for security-sensitive reports.**

Please report vulnerabilities privately through GitHub's private vulnerability reporting:

1. Go to the repository's **Security** tab.
2. Select **Report a vulnerability**.
3. Provide the details below.

Include in your report:

- The affected version line (s) and tag/commit.
- Go version and operating system.
- A minimal reproduction (proof of concept where possible).
- The observed impact and your assessment of severity.
- Any relevant logs or stack traces (redact secrets).

## Disclosure process

- We will acknowledge a valid report and begin assessment.
- We aim to confirm the issue, prepare a fix, and coordinate a release across the supported version lines.
- Please give us a reasonable window to ship a fix before any public disclosure.
