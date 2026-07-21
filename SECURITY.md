# Security Policy

## Reporting a vulnerability

Report authentication bypasses, unsafe share-page rendering, stored or reflected injection, sensitive-data exposure, or predictable share identifiers through [GitHub Security Advisories](https://github.com/starcat-app/starcat-sharing-api/security/advisories/new). Do not publish API keys, private notes, share payloads, database contents, or production logs in an issue.

Security fixes target the current default branch and latest deployed version. Runtime secrets must be injected through environment variables or Fly.io secrets and must never be committed.
