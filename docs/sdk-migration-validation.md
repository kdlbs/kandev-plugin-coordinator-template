# Public SDK migration validation

Validated on 2026-11-05 from default branch `7f45580a5a3d0406b5ad80b169e89544cdd22b61`.
No pre-existing working-tree edits or repository `AGENTS.md` were present.
The parent repository's `create-kandev-plugin` workflow, public authoring guide,
and approved `docs/plans/plugin-sdk-publishing/template-migration.md` were read.

## Release contract

- [PR #4534](https://github.com/kdlbs/kandev/pull/4534) merged on 2026-10-28.
- [Recovery run 42089596215, attempt 3](https://github.com/kdlbs/kandev/actions/runs/42089596215/attempts/3)
  concluded `success`; both public SDK consumers were verified before migration.
- [SDK 0.1.0](https://github.com/kdlbs/kandev/releases/tag/pluginsdk/v0.1.0)
  and [host v0.99.0](https://github.com/kdlbs/kandev/releases/tag/v0.99.0)
  resolve to source `f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`.
- Both installed `sdk-release.json` records say `sdk_version: 0.1.0`,
  `host_release: 0.99.0`, `min_kandev_version: 0.99.0`, `api_versions: [1, 2]`.
- Base manifest retains API v2, identity, capabilities, actions, runtime targets,
  and plugin version `0.2.1`. Base and optional recipe now declare minimum
  Kandev `0.99.0`, using the entire SDK's floor. No release/catalog was changed.

Go and npm SDK versions stay independent of host and plugin release numbers.
The source-only import migration changes no handshake or wire protocol.
There are no direct generated protobuf imports in this repository.

## Standalone dependency resolution and source validation

Environment: Linux amd64, Go `1.26.6`, Node `24.19.0`, npm `11.17.0`.
The implementation was committed and cloned with `--no-local`. The first
preflight used `/tmp/coordinator-sdk-migration-evidence/standalone`; the final
cold-cache run used `/tmp/coordinator-sdk-isolated/plugin`, with no sibling
source checkout of any name. Go/npm resolved public packages into initially
empty, task-specific caches for each run. The packaging checkout was passed
only to package commands and lives outside the final consumer root.

Final isolated commands (starting from the implementation worktree):

```bash
mkdir -p /tmp/coordinator-sdk-isolated
test ! -e /tmp/coordinator-sdk-isolated/kandev
test ! -e /tmp/coordinator-sdk-isolated/kandev-tooling
git clone --no-local . /tmp/coordinator-sdk-isolated/plugin
cd /tmp/coordinator-sdk-isolated/plugin
export GOWORK=off GOMODCACHE=/tmp/coordinator-sdk-isolated/go-modules
export GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org
go mod tidy
npm ci --ignore-scripts --cache /tmp/coordinator-sdk-isolated/npm-cache
make test vet build audit-recipes
git diff --exit-code -- go.mod go.sum package-lock.json
```

The first preflight also recorded:

```bash
GOWORK=off GOMODCACHE=/tmp/coordinator-sdk-migration-evidence/go-modules go list -m all
GOWORK=off GOMODCACHE=/tmp/coordinator-sdk-migration-evidence/go-modules \
  go mod download -json github.com/kdlbs/kandev/pluginsdk@v0.1.0
npm ls @kdlbs/plugin-sdk
```

All passed. `make test vet build audit-recipes` executes:

```bash
go run github.com/kdlbs/kandev/pluginsdk/cmd/kandev-sdk-check@v0.1.0 \
  --manifest manifest.yaml --frontend-sdk node_modules/@kdlbs/plugin-sdk
go test ./server/... ./recipes/source-control/server/...
npm run typecheck:recipes
npm run test:recipes
go vet ./server/... ./recipes/source-control/server/...
go build -o bin/kandev-plugin-coordinator-template ./server/...
npm audit --audit-level=high
```

Results: both SDK checks report compatible with minimum Kandev `0.99.0`;
Go server and recipe packages pass; recipe typecheck passes; two UI recipe
files / 14 tests pass; vet and build pass; npm audit reports zero vulnerabilities.
There is no `test:ui` script: existing UI coverage is `test:recipes`, and the
packaged coordinator was additionally exercised in a real browser below.

The graph selects `github.com/kdlbs/kandev/pluginsdk v0.1.0`, with no replacement
and no `github.com/kandev/kandev` backend module. Its public Go checksum is
`h1:Pov5zE2gGf53FslhpcoGgx6E3nu6kjizT69x3MlEEEs=`; Go's download metadata
identifies the SDK tag/source above. npm resolves exact `@kdlbs/plugin-sdk@0.1.0`
from `https://registry.npmjs.org/@kdlbs/plugin-sdk/-/plugin-sdk-0.1.0.tgz`.
No local SDK links or substitutions remain in the generated lockfile.
The SDK requires gRPC `1.83.2`, reflected by `go mod tidy`.

The existing audit initially failed on Vitest `4.1.11` and its Vite/source-map-js
transitives. A Vitest patch to `4.1.12` and npm-generated transitive lock updates
restore the existing audit gate. No UI behavior or test scripts were changed.

Additional checks (all passed):

```bash
go mod verify
node --check ui/bundle.js
test -z "$(gofmt -l server recipes)"
git diff --check
go run github.com/rhysd/actionlint/cmd/actionlint@v1.7.7 \
  -shellcheck= -pyflakes= .github/workflows/build.yml \
  .github/workflows/ci.yml .github/workflows/release.yml
```

## Packaging

`plugin-pack` remains backend-owned. SDK compilation/typechecking is standalone;
packaging still requires an explicit source checkout. `.kandev-tooling-ref` pins
`f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`, independently of dependency resolution.
The tooling checkout includes `apps/backend` and root `pluginsdk` for the tool's
own local replacement. It was materialized separately using:

```bash
git clone --filter=blob:none --no-checkout https://github.com/kdlbs/kandev.git \
  /tmp/coordinator-sdk-migration-evidence/kandev-tooling
git -C /tmp/coordinator-sdk-migration-evidence/kandev-tooling \
  sparse-checkout set apps/backend pluginsdk
git -C /tmp/coordinator-sdk-migration-evidence/kandev-tooling \
  checkout f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb
make verify-package-host \
  KANDEV_TOOLING=/tmp/coordinator-sdk-migration-evidence/kandev-tooling/apps/backend
CGO_ENABLED=0 make verify-package \
  KANDEV_TOOLING=/tmp/coordinator-sdk-migration-evidence/kandev-tooling/apps/backend
```

Packaging commands above ran from the first standalone checkout.
Host packaging passed: manifest, Linux amd64 executable, UI bundle, generated
checksums; every internal checksum passed. The initial all-platform invocation
without `CGO_ENABLED=0` failed cross-compiling Linux arm64 because this Nix
executor inherits `CGO_ENABLED=1` and has no arm64 C cross-assembler. The explicit
pure-Go run uses the existing modernc SQLite implementation and no C cross-toolchain.

All-platform packaging passed. A separate archive inventory assertion found
exactly eight files: `manifest.yaml`, `ui/bundle.js`, generated `checksums.txt`,
and the five declared executables (Linux amd64/arm64, macOS amd64/arm64,
Windows amd64). No recipes, package manifests, or development sources leaked.
Every internal checksum passed. Archive SHA-256:
`358903e9e036a77c2c2670035e0afdb795cfff81cf224fd33f65607285ecc289`.
The archive was rebuilt after an interrupted pack operation, then inspected;
no interrupted artifact was used as validation evidence.

## Released minimum-host smoke

The unmodified Linux amd64 host binary was downloaded from release `v0.99.0`;
its published archive SHA-256 passed. `kandev __backend -version` reports
`v0.99.0`, commit `f0e2e774cfe2df8a0d15e4c21dea7b48580e38fb`.
The disposable host uses its own home/database/plugins, binds loopback, and has
no integration credentials configured. No model turn or external issue write
was dispatched. Host startup exposed the shipped embedded frontend.

```bash
gh release download v0.99.0 --repo kdlbs/kandev \
  --pattern kandev-linux-x64.tar.gz --pattern kandev-linux-x64.tar.gz.sha256 \
  --dir /tmp/coordinator-sdk-migration-evidence
# From the evidence directory:
sha256sum -c kandev-linux-x64.tar.gz.sha256
tar -xzf kandev-linux-x64.tar.gz
./kandev/bin/kandev __backend -version
KANDEV_HOME_DIR=/tmp/coordinator-sdk-migration-evidence/host-home \
  KANDEV_SERVER_HOST=127.0.0.1 KANDEV_FEATURES_AUTH=false \
  KANDEV_AGENT_STANDALONE_PORT=41937 \
  ./kandev/bin/kandev __backend -port 41837
curl -F package=@standalone/kandev-plugin-coordinator-template-0.2.1.tar.gz \
  http://127.0.0.1:41837/api/plugins/install
python3 /tmp/coordinator-sdk-migration-evidence/smoke-api.py
python3 /tmp/coordinator-sdk-migration-evidence/smoke-lifecycle.py
LD_LIBRARY_PATH="$(cat /tmp/coordinator-sdk-migration-evidence/browser/library-path.txt)" \
  node /tmp/coordinator-sdk-migration-evidence/browser/smoke-ui.mjs
```

The task-local API harness records HTTP requests/responses in `smoke-api.json`.
The initial host-platform archive was used for the detailed smoke below. After
full-package construction, the plugin was uninstalled from this disposable
instance, the all-platform archive was installed, and active status/API v2 plus
a real Host-backed `instance.catalog` response were verified again. The
all-platform archive digest is recorded in `full-package-smoke.json`.
Results against `/api/plugins/kandev-plugin-coordinator-template`:

- Install/activation: HTTP 200, status `active`; confirms subprocess handshake.
- `instance.catalog` and `instance.list`: 200 with real Host profile data.
- Empty `instance.save`: 400; undeclared action and webhook: 404.
- GET/PUT/DELETE workspace capability approvals: 200. A valid `instance.save`
  with revoked grants returns 403; restoring grants allows creation (200).
- `conversation.status`: 200 with retained conversation/session and revision;
  `task.status`, `proposal.list`, `watch.list`, `outcome.list`, `routine.list`,
  and `issue.writes.list`: 200.
- Repeating instance creation without its current revision: 409.
- Disable/re-enable: 200, status `disabled` then `active`; instance settings
  survive the subprocess restart unchanged.

Browser smoke passed using Playwright `1.64.1` with cached Chromium 1228:
1440×1000 desktop and 390×844 touch/mobile contexts both render the coordinator
home and settings routes. The desktop also completes two disable/re-enable
cycles with one coordinator navigation entry after each cycle. No page errors
occurred. Screenshots and `ui-smoke.json` are retained in the task-local evidence
directory. On this Nix machine, browser libraries were supplied explicitly via
`LD_LIBRARY_PATH`; no source or product runtime changes were needed.
The first phone probe incorrectly expected the collapsed task panel to be
visible; the corrected probe checks the phone route and settings controls.

Smoke limits: real managed-agent execution/MCP tool invocation, SSH execution,
provider-backed Jira/Linear writes, every optional administration/interaction
mutation, and the inactive source-control adapter recipe were not exercised.
No real provider credentials were used. The recipe remains excluded from the
package and is covered by its actual Go/TypeScript tests. This evidence is a
migration smoke check, not certification of every optional integration.
