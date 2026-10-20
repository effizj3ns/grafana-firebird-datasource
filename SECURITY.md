# Security Policy

## Supported Versions

| Version | Supported |
|---------|-----------|
| 0.1.x   | yes       |

## Reporting a Vulnerability

**Do NOT open a public issue for security vulnerabilities.**

Contact the maintainers privately (see the repository's SUPPORT contact or
the author profile on GitHub).

Please include:

- Affected version (from `src/plugin.json`)
- Description of the vulnerability and impact
- Steps to reproduce or a proof of concept
- Any suggested fix (optional)

We aim to acknowledge reports within 5 business days and will keep the
reporter informed about the fix timeline.

## Security Model Notes

- The plugin executes **read-only SELECT/WITH statements only** against the
  configured Firebird database. This is comfort-level enforcement; the real
  guarantee MUST come from a read-only DB user (defense in depth).
  Note: selectable stored procedures can execute DML server-side even when
  called via SELECT — restrict grants accordingly.
- The DB password is stored in Grafana's encrypted secureJsonData and is
  never returned to the frontend after save.
- The plugin is distributed unsigned; loading it requires
  `GF_PLUGINS_ALLOW_LOADING_UNSIGNED_PLUGINS` on the Grafana instance.
