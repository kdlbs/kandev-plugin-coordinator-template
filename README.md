# Kandev coordinator plugin template

A working source example for authors who want their own Kandev coordinator.
Use this template as inspiration for roles, prompts, task policies, and workspace interfaces.
It shows how a plugin can coordinate work through public Host APIs.

The plugin owns policy, memory, proposals, and reports.
Kandev owns task state, execution, capability authorization, and human approvals.

## Current compatibility

This template uses the public Go SDK `github.com/kdlbs/kandev/pluginsdk v0.1.0`
and the frontend types `@kdlbs/plugin-sdk` pinned exactly to `0.1.0`.
Both SDKs share independent versioning; the plugin keeps its own version.
The [published SDK release](https://github.com/kdlbs/kandev/releases/tag/pluginsdk/v0.1.0)
records Kandev **0.99.0** as both `host_release` and `min_kandev_version`,
with manifest API versions `[1, 2]`. This template retains API v2 and declares
that SDK-wide minimum, including when using the optional source-control recipe.

The new Host APIs do not require a feature flag.
Each workspace must approve the capabilities declared by the installed plugin.
These grants control authorization, not feature availability.

Managed agent execution currently has no supported real provider.
Kandev rejects a managed launch until an adapter enforces the restricted tool policy and passes launch/resume validation.
The template therefore demonstrates the integration and policy structure without claiming that real coordinator chat is available yet.
Other operations remain subject to their own Host capability checks.

## What you can adapt

- Named instances with separate roles, instructions, profiles, task scope, and memory.
- Task delegation, explicit adoption, completion evidence, and outcome reports.
- Proposals that require approval before task creation.
- Durable policy records, an intent outbox, event reconciliation, and recurring routines.
- Optional Jira and Linear issue updates through Host-owned credentials.
- Desktop and phone interfaces built from shared Host components.

This is a template, not a required coordinator policy.
Remove capabilities and tools that your coordinator does not need.
The observer example from the original investigation is not part of this repository.

## Create your coordinator

1. Select **Use this template** on GitHub to create your repository.
2. Clone your repository. SDK compilation, tests, and typechecking need no Kandev source checkout.

```bash
git clone https://github.com/YOUR-OWNER/YOUR-COORDINATOR.git
cd YOUR-COORDINATOR
npm ci --ignore-scripts
```

3. Read the [public authoring guide](https://github.com/kdlbs/kandev/blob/main/docs/public/plugins-authoring.md).

4. Rename the plugin identity in these files:

| File | Change |
| --- | --- |
| `manifest.yaml` | Plugin ID, display name, description, author, and capabilities |
| `go.mod` | Go module path |
| `Makefile` | Binary and package names |
| `server/main.go`, `server/routines.go` | Template identity references |
| `ui/bundle.js` | Plugin ID and routes |
| `package.json`, `package-lock.json` | Private test-package name |
| `.github/workflows/release.yml` | Package archive names |

5. Search for `kandev-plugin-coordinator-template` to find remaining identity references.
6. Adapt the role definitions, policy rules, tools, and interface to your needs.
7. Keep Go and frontend SDK versions pinned exactly; upgrade them deliberately as described below.

The native plugin process and its UI code are trusted installed code.
Managed-agent tool restrictions do not provide an operating-system sandbox for the plugin itself.

## Build and check

```bash
npm ci --ignore-scripts
make test
make vet
make build
make audit-recipes
```

`make test` runs the installed-dependency compatibility checker, Go tests,
recipe typechecking, and the existing recipe UI tests. There is no `test:ui`
script in this repository. Run the checker separately with `make sdk-check`:

```bash
go run github.com/kdlbs/kandev/pluginsdk/cmd/kandev-sdk-check@v0.1.0 \
  --manifest manifest.yaml --frontend-sdk node_modules/@kdlbs/plugin-sdk
```

To upgrade, select a publicly verified SDK release, update the exact Go/npm
pins and checker version, run `go mod tidy` and regenerate `package-lock.json`
with npm, then reinstall with `npm ci --ignore-scripts`. Read both installed
`sdk-release.json` records, set `min_kandev_version` to at least their highest
floor, and retain a manifest API version supported by both. Rerun the checker,
tests, packaging checks, and a smoke test on that minimum host. SDK, host,
manifest API, and plugin versions are separate numbers. Built plugins retain
their existing wire protocol and handshake; source migration does not itself
require reinstalling an already built plugin.

### Packaging tooling

Packaging still uses backend-owned `cmd/plugin-pack`. Its separately tested
source commit is recorded in `.kandev-tooling-ref`; this is a tooling pin,
not an SDK dependency. Packaging verifies its HEAD against that pin before
staging or compiling the package. A packaging checkout must include `pluginsdk/` because
the backend tool's own module has a local SDK replacement.

```bash
git clone --filter=blob:none --no-checkout https://github.com/kdlbs/kandev.git ../kandev-tooling
git -C ../kandev-tooling sparse-checkout set apps/backend pluginsdk
git -C ../kandev-tooling checkout "$(cat .kandev-tooling-ref)"
make verify-package-host KANDEV_TOOLING="$(cd ../kandev-tooling/apps/backend && pwd)"
make verify-package KANDEV_TOOLING="$(cd ../kandev-tooling/apps/backend && pwd)"
```

`make verify-package-host` checks the host-platform archive and its checksums.
`make verify-package` builds all five declared platform binaries. Both run
`sdk-check` first, so install npm dependencies before packaging. An existing
checkout at the pinned commit can be anywhere: pass its backend directory as
`KANDEV_TOOLING=/absolute/path/to/kandev/apps/backend`.
SDK compilation is independent of this checkout; packaging is not checkout-free.

Install the generated archive through **Settings > Plugins** in a compatible Kandev instance.
Approve the required capabilities for the selected workspace.
The package filename is `kandev-plugin-coordinator-template-0.2.1.tar.gz` before you rename the template.

## Code map

| Path | Responsibility |
| --- | --- |
| `manifest.yaml` | Capabilities, actions, runtime targets, and managed tools |
| `server/coordinator.go` | Instance configuration, Host actions, and agent tools |
| `server/policy.go`, `server/policy_store.go` | Proposals, policy limits, SQLite state, and outbox |
| `server/watchers.go`, `server/routines.go` | Task observation and recurring work |
| `server/operations.go`, `server/administration.go` | Optional task, issue, and workflow operations |
| `ui/bundle.js` | Workspace and settings interfaces |
| `recipes/source-control/` | Optional starter material, excluded from plugin packages |

State lives under `KANDEV_PLUGIN_DATA_DIR`.
Memory is scoped to a workspace and instance.
Uncertain external effects retain their receipts for reconciliation instead of automatic replay.

## Origin and license

This example started from [`kdlbs/kandev-plugin-template`](https://github.com/kdlbs/kandev-plugin-template)
at commit `be2f0c51b6fca92cf752c12f4c071961276782be`.
It demonstrates the coordinator extension developed with Kandev PR #3994.
The source is available under the [MIT license](LICENSE).
