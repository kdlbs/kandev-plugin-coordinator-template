# Coordinator template guidance

Keep plugin source in this repository. Follow the parent Kandev
[create-kandev-plugin workflow](https://github.com/kdlbs/kandev/blob/main/.agents/skills/create-kandev-plugin/SKILL.md)
and [public authoring guide](https://github.com/kdlbs/kandev/blob/main/docs/public/plugins-authoring.md).

- Pin public `github.com/kdlbs/kandev/pluginsdk` and `@kdlbs/plugin-sdk` exactly
  to the same verified SDK version. Do not add SDK `replace`, `file:`, or `link:`
  substitutions. Commit generated Go sums and npm locks.
- Use the highest installed SDK-wide host floor for `min_kandev_version` and
  keep `api_version` supported by both SDKs. SDK, host, and plugin versions are
  independent. Preserve installed-plugin behavior when migrating imports.
- Install with `npm ci --ignore-scripts`, then run `make sdk-check`, `make test`,
  `make vet`, `make build`, and `make audit-recipes`. The frontend scripts are
  `typecheck:recipes` and `test:recipes`; no `test:ui` script exists.
- Validate in a standalone checkout. Include every Go/frontend recipe when
  updating imports. Do not use a sibling SDK checkout to make checks pass.
- Packaging uses backend-owned `plugin-pack`. `.kandev-tooling-ref` pins its
  source independently. Include `apps/backend` and `pluginsdk` in the tooling
  checkout and pass its backend directory as `KANDEV_TOOLING`. Do not add the
  backend module to the plugin dependency graph.
- Run host and all-platform packaging verification, inspect archive contents
  and checksums, then smoke-test the package in a disposable instance of the
  stated minimum host. Record exact commands and untested surfaces.
- Keep the opt-in source-control recipe outside shipped archives. Preserve
  identity, declared capabilities, API versions, and plugin release versions
  unless a task explicitly changes them. Publishing, merging, catalog edits,
  and contacting authors require separate authorization.
