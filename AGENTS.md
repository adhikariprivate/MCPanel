# Repository instructions

## MCPN identity and compatibility

- MCPN means **My Control Panel**. Use MCPN in user-visible product branding and docs.
- Keep the `mpn` executable, command, service, unit and asset identifiers unless a migration explicitly preserves compatibility for existing installations.
- English is the only supported language for the root README and changelog. Do not add translated README or changelog copies.
- GitHub-owned release sources are `adhikariprivate/MCPanel`, `adhikariprivate/mcpn-mail`, and `adhikariprivate/mcpn-packages`.
- Keep version pins and checksums consistent with released assets. Do not point installers at an unpublished release or change a checksum without verifying the matching asset.

## Task workflow

For each substantive change, coordinate three separate agents:

1. **Plan agent:** inspect the request, repository instructions and affected code; return scope, risks and an implementation plan without editing.
2. **Build agent:** implement the approved scope, update documentation and changelog, and report changed files and unresolved dependencies.
3. **Validation agent:** independently inspect the diff, run the relevant formatting, build and test commands, and report failures without silently changing implementation.

The coordinating agent reviews the plan, implementation and validation report, resolves findings, and reports any checks that could not run. Keep each agent focused on its named stage; do not combine all stages in one agent.

## Documentation and changelog upkeep

- Documentation is authored in English Markdown under `docs/` and built with Material for MkDocs using the root `mkdocs.yml`.
- When product behavior, setup, configuration, or supported workflows change, update the relevant documentation page in the same change.
- Add a concise entry under the next version heading in `docs/changelog.md` for every user-facing release change. Keep release history in that file; do not recreate a root changelog.
- Keep `mkdocs.yml` navigation in sync with new or renamed documentation pages.
- For release notes, `.github/workflows/release.yml` extracts the matching section from `docs/changelog.md`.
