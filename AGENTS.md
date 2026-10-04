# Repository instructions

- `README.md` is the canonical description of user-visible CLI commands, output, configuration, and security behavior. Keep it focused on information users need.
- Put package architecture, implementation details, development workflows, and release procedures in `docs/dev.md`.
- Prefer tests that exercise a real boundary or user-visible guarantee. Avoid tests that reproduce library behavior, validate mocks, duplicate another test's coverage, or lock down incidental implementation details.
- Use `just test` to run the default and GUI-tagged suites. Use `just deps-credits` after dependency or target-platform changes.
