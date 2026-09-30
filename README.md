# Kandev coordinator plugin template

A working source example for authors who want their own Kandev coordinator.
Use this template as inspiration for roles, prompts, task policies, and workspace interfaces.
It shows how a plugin can coordinate work through public Host APIs.

The plugin owns policy, memory, proposals, and reports.
Kandev owns task state, execution, capability authorization, and human approvals.

## Current compatibility

This template targets the Host APIs in [Kandev PR #3994](https://github.com/kdlbs/kandev/pull/3994).
CI pins the Host SDK to commit `f24a785cd3012bbf14c06134ed480e82fd7613a3`.
The setup commands below use that same commit.

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
2. Clone Kandev and your repository as sibling directories.
3. Use the merged Host commit below until the APIs reach a stable Kandev release.

```bash
git clone https://github.com/kdlbs/kandev.git
git -C kandev checkout f24a785cd3012bbf14c06134ed480e82fd7613a3
git clone https://github.com/YOUR-OWNER/YOUR-COORDINATOR.git
cd YOUR-COORDINATOR
```

The local `replace` directive in `go.mod` expects this layout:

```text
parent/
  kandev/
  YOUR-COORDINATOR/
```

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
7. Keep the Host SDK revision consistent across setup instructions and GitHub workflows.

The native plugin process and its UI code are trusted installed code.
Managed-agent tool restrictions do not provide an operating-system sandbox for the plugin itself.

## Build and check

```bash
npm ci --ignore-scripts
make test
make vet
make verify-package-host
```

The package uses only manifest-declared public Host APIs.
`make verify-package-host` checks the host-platform archive and its checksums.
`make verify-package` builds all five declared platform binaries.

If you use another Host path, update the `go.mod` replacement and the frontend SDK dependency.
Also pass `KANDEV_SDK=/path/to/kandev/apps/backend` to Make.
Changing `KANDEV_SDK` alone does not change Go or npm dependency resolution.

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
