# S0 reference and tool pins

Date: 2026-11-12. Evidence class: source/artifact metadata inspection, **not a
spike, compatibility or test-pass result**. The [criteria](s0-spike-criteria.md)
define acceptance. Module prefix: `github.com/hiento09/aiops-platform`.

## Oracle

Reference versions are `@deepseek-ai/cordis` 4.1.0 and
`@deepseek-ai/cordis-plugin-loader` 1.1.0, with cosmokit 1.9.0, schemastery 3.19.0
and Standard Schema 1.1.0. The [harness commit](https://github.com/deepseek-ai/deepseek-harness/commit/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159)
is `fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159`.

Raw SHA-256 equality against that commit was checked across all nine Cordis,
nine loader, seven cosmokit and one schemastery TypeScript files. The installed
packages also match every regular file of their exact npm archive. Source
manifests differ from published manifests in workspace dependency expansion,
bin normalization and formatting; these are not erased to claim whole-directory
equality. No signed build linkage or unique historical publication commit is
claimed: the commit is unsigned and scoped package metadata has no gitHead.

| Exact npm archive | SHA-256 |
|---|---|
| [cordis 4.1.0](https://registry.npmjs.org/@deepseek-ai/cordis/-/cordis-4.1.0.tgz) | `3b0099a6aa9b0b52c3e199042acf6f1ae195c5690f7de11aa61171bca7752eca` |
| [loader 1.1.0](https://registry.npmjs.org/@deepseek-ai/cordis-plugin-loader/-/cordis-plugin-loader-1.1.0.tgz) | `afb467fc85361e8ab99a88cdcbcf90ef3ffc270dfbe8c2e03f503bb4c899939f` |
| [cosmokit 1.9.0](https://registry.npmjs.org/@deepseek-ai/cosmokit/-/cosmokit-1.9.0.tgz) | `6225043dbc7f6ff6c7cb104bc381b74c972577967ad827af26180719e17ff71d` |
| [schemastery 3.19.0](https://registry.npmjs.org/@deepseek-ai/schemastery/-/schemastery-3.19.0.tgz) | `f702ce152aba393497c5d8d1258c6e602c7d85a22c3071b5fee6ccf0503e39e0` |
| [Standard Schema 1.1.0](https://registry.npmjs.org/@standard-schema/spec/-/spec-1.1.0.tgz) | `a7cb7268be280ab518d450f8c3b07c86f23417225c0ab53691ed59b3067cceaf` |

Preserve source and package-byte hash manifests beside the test-only oracle.
Execute built-JavaScript packages; do not silently transpile different semantics
or acquire optional native loader packages. Supported loader tree APIs may be
used without native resolution internals. Resolution/internal-module behavior
and Go loader functionality are not implemented in S0.

### Notices and reference tests

The four scoped packages carry MIT, Copyright (c) 2021-present Shigma. The
[harness licence](https://raw.githubusercontent.com/deepseek-ai/deepseek-harness/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/LICENSE)
is MIT, Copyright (c) 2026 DeepSeek. Both full notices must accompany source,
port and oracle. Standard Schema's MIT notice is Copyright (c) 2024 Colin
McDonnell. Oracle code never enters product artifacts.

The package/vendor directories have no complete original upstream test suite.
The same pinned harness contains regression tests:

- [Core lifecycle](https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/scripts/cordis-core-lifecycle.spec.ts),
  SHA-256 `6d432032070d2eba46f39b6f9b0a9b0c921810a2e39ac94aeeafd0d335980995`.
- [Entry commit](https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/scripts/cordis-entry-commit.spec.ts),
  SHA-256 `2bc07db5fe89afa015bf839bc55d1607a0cff54b439c915d41bbd4fb3b59e61d`.
- [Lifecycle ownership](https://github.com/deepseek-ai/deepseek-harness/blob/fcc0ad462172522aaf5c93a7b2e01f6b9f0b0159/packages/extensions/tool-cordis/tests/cordis-lifecycle.spec.ts),
  SHA-256 `c98e874987a5d2219fc0f6ce3709d9a514739a0f738a66692f12b4d2875033df`.

These were read/hashed, not executed. Re-express their kernel-relevant operations
in the S0 deterministic script corpus with attribution; record that adaptation
explicitly rather than claiming the original Vitest harness ran. Loader file
persistence, native resolver and tool-dispatcher-specific tests remain outside
S0. Awaited in-Apply self-disposal is not proven by external disposal tests.

## Isolated JavaScript runtime

Use [Node v24.22.1 Linux x64](https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-x64.tar.xz),
SHA-256 `6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6`.
The [official checksum manifest](https://nodejs.org/dist/v24.22.1/SHASUMS256.txt)
has SHA-256 `fdfbffc969c0f604beff0124d409b33a43b53f5c681d6220f80023e0d6df9dc4`.
Source commit is `ce7aaf1cee78b8cf0ce17364260535c418d3ed26`.
The [complete runtime licence](https://raw.githubusercontent.com/nodejs/node/ce7aaf1cee78b8cf0ce17364260535c418d3ed26/LICENSE)
has SHA-256 `5888dbb9a1d2b18f2c3e6c5f6af1b39de658372b402a0577b002777f14c62ace`.
Retain that complete notice locally, not a single MIT summary: bundled components
have separate terms. Only the runtime is executed; npm/npx/Corepack is not needed.
This is an unmodified internal development tool, not linked or redistributed in
a product. Re-review any redistribution or additional tooling scope.

The available host Node 22.22.1 is not this pin and does not meet the harness's
Node >=24.9.0 declaration. A cached mutable Node image is not substituted. Verify
archive bytes before execution. Published checksums are not independent signature
verification, and metadata inspection is not a compatibility result.

## PostgreSQL test artifact

Selected linux/amd64 Docker Official Image:

```text
docker.io/library/postgres:16.15-trixie@sha256:43f9275df91e012c3da3d86bea5d7bb5e67a473296cc906c5f9e80b8176b1a37
```

Observed registry index:
`sha256:f9a7b9ebf6f56201f949db745e47bc423c0f0f8294bafe991cc10bf74b029657`;
selected config:
`sha256:ba07bdb15f0f2ac1ba7a88891da731cae65ac36d663fdb7de8f698ea47e0e381`.
Config records PG_VERSION `16.15-1.pgdg13+2`.
[Packaging source](https://github.com/docker-library/postgres/blob/9d15534160ade17f2b6c455a39ee967c49b1937d/16/trixie/Dockerfile)
is pinned to `9d15534160ade17f2b6c455a39ee967c49b1937d`.
Core [COPYRIGHT](https://raw.githubusercontent.com/postgres/postgres/7d3e000c5961a544302072058a1184e9a588837b/COPYRIGHT)
SHA-256 is `3d6af92ff8a4c2cdf69afb1cf44edea727922f5cd0cf8b5f72b11cdecac8fdfd`;
[packaging MIT licence](https://raw.githubusercontent.com/docker-library/postgres/9d15534160ade17f2b6c455a39ee967c49b1937d/LICENSE)
SHA-256 is `87ffd2c45e3f90cfa3407b5c40ef8333e87c3e875e4895f8b64df758198deafc`.

Whole image/base-layer software is not PostgreSQL/MIT-only. This image is pulled
for internal tests only, not mirrored or redistributed. No complete extracted
image package audit, signed-image verification or security certification is
claimed. Do not substitute the older cached mutable tag. Initialization's
superuser is not a measured runtime role; the probe creates separate nonowner,
NOSUPERUSER/NOBYPASSRLS runtime credentials and verifies live durability settings.

## Go infrastructure probe pins and licences

| Module | Pin | Licence |
|---|---|---|
| github.com/riverqueue/river | v0.51.2 | MPL-2.0 |
| github.com/riverqueue/river/riverdriver | v0.51.2 | MPL-2.0 |
| github.com/riverqueue/river/riverdriver/riverpgxv5 | v0.51.2 | MPL-2.0 |
| github.com/riverqueue/river/rivershared | v0.51.2 | MPL-2.0 |
| github.com/riverqueue/river/rivertype | v0.51.2 | MPL-2.0 |
| github.com/jackc/pgx/v5 | v5.11.0 | MIT |
| github.com/jackc/pgerrcode | v0.0.0-20240316143900-6e2875d9b438 | MIT and PostgreSQL data notice |
| github.com/jackc/pgpassfile | v1.0.0 | MIT |
| github.com/jackc/pgservicefile | v0.0.0-20240606120523-5a60cdf6a761 | MIT |
| github.com/jackc/puddle/v2 | v2.2.3 | MIT |
| github.com/robfig/cron/v3 | v3.0.1 | MIT |
| github.com/tidwall/gjson | v1.20.0 | MIT |
| github.com/tidwall/sjson | v1.2.5 | MIT |
| github.com/tidwall/match | v1.2.0 | MIT |
| github.com/tidwall/pretty | v1.2.1 | MIT |
| golang.org/x/sync | v0.24.0 | BSD-3-Clause |
| golang.org/x/text | v0.43.0 | BSD-3-Clause |

[River source](https://github.com/riverqueue/river/tree/v0.51.2) resolves to
`8468ff25e2cc7afd790d7037703ae72d8b2138cb`;
[pgx](https://github.com/jackc/pgx/tree/v5.11.0) resolves to
`5e583fa7aabfa88b796292f849fc9d7d75ac159d`. Each of the five River module archives
was inspected for its own MPL licence, not inferred only from the root module.
Archive source routes use `https://proxy.golang.org/<module>/@v/<version>.zip`.

Use River unmodified; run its own migrations, never copy their SQL. Preserve a
source/version notice per MPL module and exact source archives for any test
artifact that is distributed. Separate original code is Apache-2.0; no change
to covered MPL files is hidden. The inspected core-provider inventory is not a
final package SBOM. Before running the compiled probe, enumerate actual linked
packages and confirm every additional module/directory licence; fail closed on
unknown terms. Upstream-only development/test modules are recorded separately
and not automatically used by the probe. Kernel remains standard-library-only
and imports none of these modules.

## Source-grounded questions the probe must answer

InsertTx wraps the caller's pgx.Tx and does not commit or switch roles. Explicit
River Schema must be consistent across caller and background connections;
transaction-local role/settings do not propagate to a separate listener pool.
Stock River has no tenant column, tenant-aware RLS policy or worker context.
Provider-owned stamping/lookup must be proved atomic without payload identity.
An extra column alone is not returned in the generated River job scan.

Sequential Stop/Start on the same River instance is supported by source; a stop
timeout is not a completed stop. Recreating the instance is a distinct case and
subscriptions must be re-established. Worker registration has no public removal;
use a revocable handler table and snooze unowned work. These are inspected API
facts, not successful S0 integration measurements.
