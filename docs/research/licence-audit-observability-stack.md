# Licence audit: the observability stack

Read on 2026-11-11. Scope: every upstream of the default observability stack that `00-decisions.md` §3, the plugin record, the community record and the architecture set name. This is engineering analysis, not legal advice; section 8 lists what a lawyer must answer.

Method: each licence was read at the primary source (the `LICENSE` and `LICENSING.md` files at the release tag the plan pins and at the default branch, the GitHub licence API, npm registry metadata, and the vendor's own policy pages). Source archives were downloaded for the collector, the eBPF instrumentation and the runtime sensor to count licence identifiers in their BPF trees and to list imports. Earlier research notes were used as leads only. Section 10 gives every URL with its tag or commit. "The vendor" below means Grafana Labs, the company behind the default stack.

The five uses checked for each upstream:

| Code | Use |
|---|---|
| U1 | Community edition: an Apache-2.0 repository and release artifacts (binary, images, Helm chart, compose file) that bundle or pull the upstream |
| U2 | Our hosted multi-tenant service running the upstream unmodified |
| U3 | A later commercial enterprise edition on the same core, with closed plugins in the same binary or as separate processes |
| U4 | Third parties selling plugins that wrap or call the upstream |
| U5 | The upstream's name in a plugin id or description |

Owner decisions A and B of this round (the agent harness and the on-call provider are each one plugin) do not touch this group. Nothing in the observability stack depends on either.

---

## 1. Result in short

1. **No server of the stack has to be rebuilt.** The five AGPL-3.0-only servers (dashboard server, metrics, logs, traces and profiles stores) are usable in all five uses as long as they stay unmodified, run as separate processes and are reached over HTTP or gRPC, which is what principle 7 and 08 §5.1 L2 to L5 already require.
2. **Two things are blocked and need a different choice**, both small:
   - The upstream Helm chart of the profiles store is AGPL-3.0-only (it is not in that project's Apache-2.0 list and carries no licence file of its own). It cannot be vendored into the Apache-2.0 repository. We write our own templates for the single-binary profiles store, as 08 §4.1 already plans for the metrics store fallback.
   - The vendor's plugin policy forbids "a separate public plugin catalog involving or relating to Grafana Labs or its offerings". The marketplace and the project index must never list dashboard-server plugins (app, data source or panel plugins). Third-party user interface reaches the dashboard server only as provisioned dashboard content or through contribution points that the one first-party app renders.
3. **One component is optional only:** `aiops-app`, the dashboard app plugin. The licence allows it, the vendor's signing terms do not give it a clean distribution path (section 5.3). It must never be required by the core, a default bundle or a conformance suite. The plan already has it optional in self-host; this audit makes that a rule.
4. **Six facts differ from the plan or the earlier notes** (section 7): the profiles store chart licence; the charts of the dashboard server, the logs store and the traces store moved to a community organisation in 2026 and the metrics store chart is deprecated; the collector binary links two AGPL-3.0-only packages of the logs store; three headers in the sensor's BPF tree are GPL-2.0 only; the trademark policy has two clauses the checklist does not carry; the plugin policy now says a distributed plugin outside the catalog "still require[s] a signing subscription".
5. **The largest risk is not the licence text. It is the single vendor:** one company owns the copyright through a contributor agreement that allows sublicensing, has relicensed once, controls the marks and the plugin signing key, and since 2026-10-20 operates its own paid plugin marketplace.

---

## 2. Verdict table

Component names are the plan's ids or generic nouns. The upstream is in the second column.

| Component | Upstream, version checked | Role in the plan | Use mode | Licence verified | Verdict |
|---|---|---|---|---|---|
| `dashboard-ui` | Grafana OSS v13.3.1 | Dashboards, explore, alerting UI, host of `aiops-app` | Unmodified image as a separate process; we render provisioning files | AGPL-3.0-only; Apache-2.0 sub-trees (section 3.2) | ok-with-conditions |
| `metrics-store` | Mimir 3.2.4 | Metrics, rulers, multi-tenant alert routing | Unmodified image, separate process, HTTP | AGPL-3.0-only; `operations/` and `vendor/` under their own licences (`operations/LICENSE_APACHE2`) | ok-with-conditions |
| `logs-store` | Loki v3.7.9 | Logs, ruler, alert state history, sensor events | Unmodified image, separate process, HTTP | AGPL-3.0-only; Apache-2.0 sub-trees (section 3.2) | ok-with-conditions |
| `traces-store` | Tempo v3.1.1 | Traces, service graphs | Unmodified image, separate process, HTTP and gRPC | AGPL-3.0-only; `operations/` and `pkg/tempopb` Apache-2.0 | ok-with-conditions |
| `profiles-store` | Pyroscope v2.4.3 | Continuous profiles | Unmodified image, separate process, HTTP (Connect JSON with our own types) | AGPL-3.0-only; Apache-2.0 only for `lidia` and four `og/` directories | ok-with-conditions |
| Profiles store chart | `operations/pyroscope/helm/pyroscope` 2.4.3 | A chart the `aiops` chart would pin | Content copied or packaged into our chart | AGPL-3.0-only by the project's `LICENSING.md` default; no licence file in the directory | replace-or-rebuild |
| Server charts | grafana-community/helm-charts: `grafana` 14.4.1, `loki` 21.2.0, `tempo` 3.2.1, `tempo-distributed` 3.28.0; `mimir-distributed` in the metrics store repository | Charts the `aiops` chart pins | Packaged as sub-charts, unmodified | Apache-2.0 | ok-with-conditions |
| `agent` | Alloy v1.21.3 | Collector on hosts and clusters, `remotecfg` client | Unmodified binary or image fetched from upstream; we render its config | Repository Apache-2.0 (one MIT file). The binary also links two AGPL-3.0-only logs store packages and embeds GPL-2.0 BPF objects | ok-with-conditions |
| Remote config protocol | alloy-remote-config v0.0.12 | Wire types of the `remotecfg` server in the kernel | Linked into our binary | Apache-2.0 | ok |
| eBPF auto-instrumentation | Beyla v3.40.0; OpenTelemetry eBPF Instrumentation v1.0.0 | `beyla.ebpf`, a child process of the agent | Runs inside the unmodified agent | Apache-2.0; BPF sources Apache-2.0 with libbpf headers under `LGPL-2.1 OR BSD-2-Clause`; kernel licence strings "Dual MIT/GPL" and "GPL" | ok-with-conditions |
| eBPF profiler | OpenTelemetry eBPF profiler (the vendor-maintained variant is what the agent links) | `pyroscope.ebpf` inside the agent | Runs inside the unmodified agent | User space Apache-2.0; `support/ebpf` GPL-2.0 | ok-with-conditions |
| Cluster collection chart | k8s-monitoring 4.6.0 | Sub-chart of the `aiops-agent` umbrella chart | Packaged as a sub-chart, unmodified | Apache-2.0; bundled sub-charts Apache-2.0 | ok |
| App plugin backend SDK | grafana-plugin-sdk-go v0.298.0 | Go backend of `aiops-app` | Linked into the app backend binary | Apache-2.0; pulls `hashicorp/go-plugin` and `yamux`, MPL-2.0 | ok |
| App plugin frontend packages | `@grafana/scenes` 9.2.2; `@grafana/data`, `ui`, `runtime`, `schema`, `i18n`, `e2e-selectors` 13.3.1 | Frontend of `aiops-app` | Bundled or resolved at run time from the host | Apache-2.0. `@grafana/prometheus`, `@grafana/o11y-ds-frontend` and `@grafana/sql` are AGPL-3.0-only | ok-with-conditions |
| `aiops-app` | Our own code, subject to the vendor plugin policy (last updated August 2026) | Incident, approval, change and fleet pages inside the dashboard server | A plugin loaded by the AGPL server | Ours, Apache-2.0; distribution governed by signature levels | optional-plugin-only |
| Dashboard-server plugins as marketplace items | Vendor plugin policy | Third parties selling UI plugins through our index or marketplace | A catalogue we or a third party would publish | Policy text, not a licence | replace-or-rebuild |
| `image-renderer` | grafana-image-renderer v5.12.10 | Panel images for reports (P3, optional) | Unmodified image, separate process | Apache-2.0; the image carries Debian Chromium under its own licences | ok |
| Alert router and rule format | Alertmanager v0.35.0; Prometheus v3.16.0 | Customer-run sender in `aiops-only`; the rule file format; the remote-write protobuf | Called over the network; Prometheus Go packages linked | Apache-2.0 | ok |
| Rule and dashboard content | kubernetes-mixin 1.5.6; node-mixin in node_exporter v1.12.1; prometheus-operator runbooks | Starting point of `kubernetes-workload` and `host-basics` | Content copied or adapted into our repository | Apache-2.0, each with a `NOTICE` | ok-with-conditions |
| Cluster and host exporters | kube-state-metrics v2.20.0; node_exporter v1.12.1; windows_exporter | Metric sources through the cluster collection chart and the agent | Unmodified images; libraries inside the agent | Apache-2.0; Apache-2.0; MIT | ok |
| `sensor` | Tetragon v1.7.1 (v1.8.0 is the latest) | Runtime security events (P2) | Unmodified image and package, separate process | User space Apache-2.0; `bpf/` dual `GPL-2.0-only OR BSD-2-Clause` with three GPL-2.0 only headers | ok-with-conditions |
| `gateway` | Envoy v1.40.2 | Tenant gateway | Unmodified image, separate process; we render its config | Apache-2.0 | ok |
| `assistant-mcp` | mcp-grafana v2.3.0 | Read-only MCP endpoint for customers' own assistants | Unmodified image, separate process | Apache-2.0 | ok |
| Cost exporter | OpenCost v1.121.4 | FinOps scrape (second wave) | Unmodified image; a sub-chart of the cluster collection chart | Apache-2.0 | ok |
| Vendor marks | Trademark policy and mark list | Every name and every public text that mentions the default stack | Naming and prose | Policy text, not a licence | ok-with-conditions |

---

## 3. AGPL-3.0: what it allows and requires in each use

### 3.1 The five uses

The clauses quoted are from the `LICENSE` file of the dashboard server at v13.3.1; the other four servers carry the same text.

| Use | What we do | Allowed | Required | Not allowed |
|---|---|---|---|---|
| U1, pull | The chart and the compose file name the upstream OSS image by digest; the user's runtime pulls it from the upstream registry | Yes. We convey nothing. Our chart, compose file and rendered config are our own works under Apache-2.0 | Ship the licence text of each component in `THIRD_PARTY_LICENSES/` (L5); this is courtesy, not a licence duty | Calling the bundle Apache-2.0 as a whole |
| U1, bundle | A mirror registry, an offline or air-gapped bundle, an image of ours that contains an AGPL binary | Yes, under §4 and §6. "You may charge any price or no price for each copy that you convey" | Keep notices and give the licence (§4). Give the Corresponding Source: §6(d) allows a pointer to a network server "operated by you or a third party" with "clear directions next to the object code", and "you remain obligated to ensure that it is available". Keep a source mirror of each tag we bundle. The bundle must stay an aggregate (§5): separate works, not "combined with it such as to form a larger program" | Any term that limits the user's AGPL rights in those components (§10) |
| U1, copy or link | Go imports, npm imports, files copied into the repository | Only the Apache-2.0 sub-trees of section 3.2 | Keep the Apache-2.0 notices | Any other file or package of the five projects |
| U2 | Run the unmodified images for tenants | Yes. "You may make, run and propagate covered works that you do not convey, without conditions". §13 starts with "if you modify the Program" | Nothing, while the images are unmodified. Config, provisioning files and data are not a modification by the §0 definition ("copy from or adapt all or part of the work in a fashion requiring copyright permission") | A patched build without the §13 offer of source to "all users interacting with it remotely" |
| U3 | Enterprise kernel with closed plugins, separate from the AGPL servers | Yes. The kernel and its plugins are separate programs behind a network API and import no AGPL package | The enterprise terms carve the AGPL components out: the customer keeps every AGPL right in them. If the enterprise distribution bundles the images, the U1 bundle row applies | A closed plugin that links an AGPL package; an enterprise licence that restricts the AGPL components |
| U4, call | A third party's plugin calls a store's HTTP API | Yes, with no AGPL duty | Nothing | |
| U4, wrap | A third party's artifact contains an AGPL image or binary | Yes, at any price | That party conveys and has the U1 bundle duties. Its entitlement check must sit in its own code | A licence key, a fee or a use limit on the AGPL part (§10) |
| U5 | The name in an id or a description | The licence grants no mark rights (§7(e)) | The trademark policy decides; see section 6 | Marks in ids, titles, product names |

Two limits of this reading, both for the lawyer: whether an umbrella chart that packages an AGPL chart or images is an aggregate (Q5), and what counts as "modify" for a custom image (Q8).

### 3.2 Apache-2.0 sub-trees, as verified

| Project | Tag read | Apache-2.0 directories | Notes |
|---|---|---|---|
| Dashboard server | v13.3.1 (same on `main`) | `packages/grafana-data/`, `packages/grafana-e2e-selectors/`, `packages/grafana-runtime/`, `packages/grafana-ui/`, `packaging/`, `kinds/`, `pkg/kinds/`, `pkg/kindsys/`, `pkg/registry/schemas/`, `grafana-mixin/`, `public/app/features/explore/TraceView/components`, two icon directories | `packages/grafana-schema`, `grafana-i18n`, `grafana-alerting`, `grafana-api-clients`, `grafana-flamegraph` and `grafana-openapi` are not in `LICENSING.md` but each carries `LICENSE_APACHE2` and declares Apache-2.0 in `package.json`. `docs/` is not listed, so documentation text is AGPL-3.0-only |
| Metrics store | mimir-3.2.4 (same on `main`) | None in Go. `operations/` (the chart and the mixin) is Apache-2.0 by `operations/LICENSE_APACHE2` | Use the Prometheus remote-write protobuf, as L2 says |
| Logs store | v3.7.9 | `clients/`, `pkg/framedstdcopy/`, `pkg/ingester/wal`, `pkg/logproto/`, `pkg/loghttp/`, `pkg/logqlmodel/`, `pkg/push`, `pkg/util`, `production/`, `tools/` | On `main` `pkg/logqlmodel/` is replaced by `pkg/logql/errors/` and `pkg/logql/model/`. The list is per tag |
| Traces store | v3.1.1 (same on `main`) | `operations/`, `pkg/tempopb` | |
| Profiles store | v2.4.3 (same on `main`) | `lidia`, `og/pkg/agent/profiler`, `og/pkg/agent/ebpfspy`, `og/packages/pyroscope-flamegraph`, `og/packages/pyroscope-models` | `operations/` (the chart and the monitoring content) is not listed. `api/` is not listed either, yet `api/LICENSE` is an Apache-2.0 text added on 2023-01-03. The two statements conflict (Q11) |

---

## 4. Conditions per use for the permissive components

For every Apache-2.0, MIT or BSD upstream in the table the answer is the same, so it is given once.

| Use | Condition |
|---|---|
| U1 | Keep the licence text and the upstream `NOTICE` content in `THIRD_PARTY_LICENSES/` and the aggregated notices (Apache-2.0 §4; L7). Mark files we change (§4(b)). MPL-2.0 modules stay unmodified and get a source line (L8) |
| U2 | None |
| U3 | None beyond U1. Closed plugins may link Apache-2.0, MIT and BSD code |
| U4 | None beyond U1 for whoever redistributes |
| U5 | Apache-2.0 §6 grants no mark rights. Prometheus, Alertmanager, Envoy, Kubernetes, OpenCost and Cilium are names of Linux Foundation projects; that foundation's trademark page gives "<your product name> for <mark>" and "compatible with <mark>" as correct and forbids the mark as part of a product name or domain. The marks of the default stack's vendor follow section 6 |

---

## 5. Reasoning for every verdict that is not plain ok

### 5.1 The five AGPL servers: ok-with-conditions

The conditions are the rows of section 3.1. In the plan's terms:

- Unmodified, separate process, OSS image by digest, every `enterprise.*` chart toggle off (L3, L15). Verified today: all five default licences are AGPL-3.0-only at the pinned tags.
- No Go or npm import outside section 3.2 (L2). The allow-list must be pinned to the tag in `go.mod`, because the logs store's list changed between v3.7.9 and `main`.
- A patch to any of them lives in a public AGPL repository (L4), and in hosted it triggers the §13 offer to every remote user. The safer rule is the plan's: no patches.
- A mirror or an offline bundle makes us a conveyor (L5). A pointer to upstream source satisfies §6(d) only while that source stays reachable, so the bundle job mirrors the source of each tag.
- The alert router inside the metrics store is part of the AGPL binary; the standalone Alertmanager is Apache-2.0. The AGPL boundary list of `00-decisions.md` §3.3 stays as it is.
- Risk, all five: one vendor holds the copyright, the proprietary enterprise builds are the vendor's revenue, and the open-source deployment tooling is moving out of the vendor's hands (section 7).

### 5.2 The profiles store chart: replace-or-rebuild

Fact: `LICENSING.md` of the profiles store sets AGPL-3.0-only as the default and lists five Apache-2.0 directories; `operations/` is not one of them, and `operations/pyroscope/helm/pyroscope/` has no licence file and no licence annotation in `Chart.yaml`. The earlier note (grafana-stack §13.1 item 4) recorded this chart as Apache-2.0; that was wrong. The chart also pulls a MinIO sub-chart and two agent sub-charts.

What it blocks:

- U1, copy: the chart cannot be vendored into the Apache-2.0 repository (L6 assumed every vendored chart keeps an Apache-2.0 licence).
- U1 and U3, package: `helm package` puts a pinned sub-chart inside our chart archive. We would convey an AGPL work inside the `aiops` chart, and inside a commercial enterprise chart later. That may be an aggregate, but it is the one place where an AGPL file would sit inside an artifact we publish under our name, and avoiding it costs little.

What we do instead: the `aiops` chart renders its own StatefulSet, Service and config for the single-binary profiles store, written from the documented flags and not from the upstream templates. 08 §4.1 already plans the same fallback for the metrics store. The hosted microservices layout is ours to render anyway. The same rule covers that project's `operations/monitoring` dashboards and rules: do not copy them.

### 5.3 Server charts: ok-with-conditions

Facts:

- The charts of the dashboard server and the traces store moved to `grafana-community/helm-charts` after 2026-01-30. The OSS chart of the logs store moved there on 2026-03-16; the chart left in the logs store repository now describes itself as a chart for the vendor's enterprise logs product. The `mimir-distributed` chart is deprecated: 6.3.0, planned for Q4 2026, is its last release from the vendor, and it moves to the same organisation by the end of 2026.
- `grafana-community/helm-charts` is Apache-2.0. The organisation is described as "maintained by Grafana Community members", is not a verified organisation, and signs charts with a key that changed at the migration.
- The community chart of the logs store no longer depends on a MinIO sub-chart (only `rollout-operator`, Apache-2.0).

Conditions: 08 §4.1 pins the charts from the new location by digest; the trust decision is recorded (who signs, which key); each packaged chart keeps its `LICENSE`; chart icons that are the vendor's logos are not reused by our charts; the release check of L15 still renders the charts and fails on an enterprise image.

Risk: the vendor no longer maintains the open-source deployment path of three of its five servers. The licence is fine; the maintenance is now a community's.

### 5.4 The agent: ok-with-conditions

Facts, from the v1.21.3 source archive:

- `LICENSING.md`: default Apache-2.0, one MIT file.
- `go.mod` requires `github.com/grafana/loki/v3` at commit `5cf8c23a258d`. Two files import `pkg/logql/syntax` and `pkg/logql/log/pattern`. At that commit the logs store's `LICENSING.md` does not list either directory, so both are AGPL-3.0-only.
- It imports `github.com/grafana/pyroscope/api` (the directory with the conflicting licence statements) and `pyroscope/lidia` (Apache-2.0).
- It links the vendor-maintained variant of the OpenTelemetry eBPF profiler, whose `support/ebpf` is GPL-2.0, and embeds a Beyla binary per architecture.
- `github.com/grafana/alloy/syntax` is a separate module with no such dependency; it is safe to link for rendering config.

So the repository is Apache-2.0, but the released binary contains AGPL-3.0-only and GPL-2.0 code. For running it unmodified this changes nothing. For redistribution it does.

Conditions: keep L5 and L11 (installers and charts fetch the agent from upstream by sha256; no image of ours bakes it in). Extend L5: a mirror or an offline bundle of the agent ships AGPL-3.0 §6 source for the two logs store packages as well as the GPL-2.0 §3 offer for the BPF objects. Never import the agent's root module. The enterprise edition does not repackage the agent inside a proprietary installer.

### 5.5 eBPF auto-instrumentation and the eBPF profiler: ok-with-conditions

Facts, OpenTelemetry eBPF Instrumentation v1.0.0, `bpf/`: 342 SPDX identifiers are Apache-2.0; the libbpf headers in `bpf/bpfcore` are `LGPL-2.1 OR BSD-2-Clause` (two are `GPL-2.0-only OR BSD-2-Clause`); the programs declare "Dual MIT/GPL" (ten), "GPL" (two, the network flow programs) or "Dual BSD/GPL" (one) to the kernel. Beyla v3.40.0 is Apache-2.0 and builds on that code. The eBPF profiler states "The eBPF source code is licensed under the GPL 2.0 license".

This closes the open item of L13 for these BPF objects. Conditions: they reach users only inside the unmodified upstream agent (L11); we write no BPF program from this code; a mirror follows 5.4. The gap between the Apache-2.0 source identifiers and the GPL strings in the objects is Q10; it matters only if we ever modify or redistribute.

### 5.6 App plugin frontend packages: ok-with-conditions

Verified on the npm registry today: `@grafana/data`, `ui`, `runtime`, `schema`, `i18n`, `e2e-selectors`, `flamegraph`, `alerting`, `api-clients` 13.3.1, `@grafana/scenes` and `scenes-react` 9.2.2, `@grafana/plugin-ui`, and the build tools `create-plugin`, `sign-plugin`, `plugin-e2e` are Apache-2.0. `@grafana/prometheus` 13.2.2, `@grafana/o11y-ds-frontend` 13.3.1 and `@grafana/sql` 13.3.1 are AGPL-3.0-only.

Conditions: the L2 allow-list reads the licence from the installed package metadata on every build, since a package can change licence between versions; the three AGPL packages stay denied. Whether a plugin that runs inside the AGPL frontend is a separate work is Q1. The vendor says "Plugins, agents, and certain libraries will remain Apache-licensed" and keeps the plugin interfaces Apache-2.0, and its own policy accepts proprietary plugins, which supports the separate-work reading for U3 and U4 but is not a licence term.

### 5.7 `aiops-app`: optional-plugin-only

The vendor's plugin policy "governs the offering of plugins for use with Grafana" on open source, third-party managed, enterprise and cloud instances. Our hosted pools are third-party managed instances. The levels, as read today:

| Level | Terms | Fit |
|---|---|---|
| Private | Free. "for use on your own Grafana instance. They may not be shared to the Grafana community or to your customers"; bound to root URLs; not accepted by the vendor's cloud | Our hosted pools only, and whether tenants using the plugin there counts as sharing it with customers is Q2 |
| Community | Free. "Not commercial in nature and not affiliated with any commercial endeavor"; published in the vendor's catalogue; needs a public repository | A project with a planned enterprise edition and a hosted service is unlikely to qualify. UNVERIFIED how the vendor would rule |
| Commercial | "Commercial Plugin Subscription required"; for plugins "offered by a for-profit business" | The clean path for a signed build, at a price and at the vendor's discretion |
| Marketplace | "Marketplace Partner Agreement required"; sold through the vendor's marketplace; may be closed source | Puts our enterprise UI inside the vendor's shop |
| Unsigned | Loads on a self-managed server through `allow_loading_unsigned_plugins`; not supported in the vendor's cloud. The policy adds: "Distributed plugins that are not included in the catalog still require a signing subscription" | Technically works and no licence forbids it; the policy text speaks against distributing it. Q2 |

Per use: U1, shipped unsigned as an optional artifact with the allow-list set by the operator; U2, private signature on our own pools, pending Q2; U3, a Commercial Plugin Subscription or a written agreement before an enterprise build is sold; U4, none, third parties do not ship dashboard-server plugins (5.8); U5, the id carries no mark.

Why optional only: the vendor can decline to sign "for any other reason at the sole discretion of Grafana Labs", holds the only signing key the server trusts, and can change what an unsigned plugin may do in any release. A product whose incident loop needed this plugin would depend on that discretion. The rule: every view the product needs exists as provisioned dashboards (plain content, no plugin, as in P1) and through the kernel API, Slack and `aiopsctl`; the app is an extra. ADR-023 decides the level, with this table as its input.

### 5.8 Dashboard-server plugins as marketplace items: replace-or-rebuild

The policy says: "Grafana Labs manages its plugin catalog. You may not create a separate public plugin catalog involving or relating to Grafana Labs or its offerings." The vendor also sells third-party plugins itself: its marketplace became generally available on 2026-10-20 with more than 30 partner plugins.

What it blocks: U4 for one artifact class. A project index, an enterprise marketplace or a third party's marketplace that lists app, data source or panel plugins for the dashboard server would be such a catalogue. Whether the policy binds a party that signed nothing is Q2; building the enterprise headline feature on the opposite assumption is not sound.

What we choose instead: the index and the marketplace list kernel plugins, remote plugins and connector packs only. A pack may carry dashboards as JSON content, which are not plugins. Third-party pages inside the dashboard server come through contribution points that `aiops-app` renders from kernel data. The index pull-request checks and `aiopsctl plugin check` refuse an artifact that contains a dashboard-server `plugin.json`. A third party that wants to sell a dashboard-server plugin uses the vendor's own catalogue.

### 5.9 Rule and dashboard content: ok-with-conditions

Verified: kubernetes-mixin is Apache-2.0 and now lives at `kubernetes-sigs/kubernetes-mixin`; its `NOTICE` lists contributors under section 7 of the CNCF contributor agreement. node-mixin is `docs/node-mixin` in node_exporter, Apache-2.0 with a `NOTICE`. The prometheus-operator runbooks repository is Apache-2.0 (last push 2024-10-03). The mixins of the metrics, logs and traces stores, of the agent and of the dashboard server sit in Apache-2.0 sub-trees. The monitoring content of the profiles store and all vendor documentation text are AGPL-3.0-only.

Conditions: each rule or dashboard that starts from an upstream file keeps its source annotation (row 17), `NOTICE` carries the upstream `NOTICE` text, and changed files say so (Apache-2.0 §4(b), §4(d)); L7 already checks the first two. Nothing is copied from an AGPL directory or from vendor documentation. Dashboards downloaded from the vendor's public dashboard site are not used: each has its uploader's terms or none. UNVERIFIED: the terms of that site. All five uses are open for content that follows these rules, including a paid third-party pack that adapts the same mixins.

### 5.10 The sensor: ok-with-conditions

Facts, v1.7.1: `LICENSE` is Apache-2.0. `bpf/COPYING` says the code under `bpf/` is, "unless otherwise specified", dual-licensed under GPL-2.0 only or BSD-2-Clause "at your option". Counted by SPDX identifier: 92 `GPL-2.0-only OR BSD-2-Clause`, 5 `LGPL-2.1 OR BSD-2-Clause`, 3 Apache-2.0, and three headers that are GPL-2.0 only (`include/bpf_elf.h`, `include/bpf_ktime.h`, and `include/errno.h` with the Linux syscall note). Four cgroup programs declare "GPL" to the kernel.

Conditions: the election of BSD-2-Clause in L11 does not reach the three GPL-2.0 only headers, so an object built with them is treated as GPL-2.0. That costs nothing while we deploy the upstream image and package unmodified and do not redistribute (L5). A mirror ships the GPL-2.0 §3 offer. The starter TracingPolicy pack is our own YAML; a policy adapted from the upstream `examples/` keeps an Apache-2.0 attribution. U5: Tetragon and Cilium are project names under the Linux Foundation; the vendor behind the project also holds marks. UNVERIFIED which.

### 5.11 Vendor marks: ok-with-conditions

Section 6.

---

## 6. Trademark policy and plugin-signing terms for a hosted service and a marketplace

Read today at `grafana.com/trademark-policy/`. The mark list is "non-exhaustive": Faro, Grafana, Grafana OnCall, Loki, Tempo, Mimir, Pyroscope and three logos.

| Policy text | Consequence |
|---|---|
| The marks "may be used for the purposes of open source discussion, development and support of Grafana Labs' open source projects". "Any other use ... requires a separate trademark license agreement" | Repository docs and technical prose are inside the grant. Hosted pricing pages, sales material and marketplace listings are not (T4 already gates this) |
| "Do not use the Grafana Labs Marks as part of your product or service name", nor in a domain, social handle, metatag or keyword | T1 holds. Applies to plugin ids, titles and index entries, whoever the publisher is |
| "Do not use the Grafana Labs Marks in any areas of your website which are password protected" | New for the checklist. The hosted console and `aiops-app` pages are behind a login. Our own pages there use generic nouns ("dashboards", "metrics store") even where the docs may use the mark |
| Do not use the marks "to reference any other goods or services that Grafana Labs did not create or provide" | New for the checklist. Read literally it covers a description such as "connector for" followed by a mark on a third-party plugin. The plan's T7 form puts the mark after "for"; whether that is permitted nominative use is Q4 |
| Do not use the marks "in conjunction with derivative software incorporating modified Grafana Labs software product code" | T5 holds: no modified build under the marks |
| Attribution statement required "prominently in all materials using" the marks | T3 holds; this file carries it |
| A breach "is considered a material breach of the terms of the applicable open source licenses" and may end them | Whether a trademark policy can end an AGPL-3.0 or Apache-2.0 grant is Q3. As a risk it means a naming mistake is treated by the vendor as a licence matter |
| "Grafana Labs reserves the right to amend this Trademark Usage Policy ... at any time" | Re-read at every release, as section 9 of this file lists |

For the five uses:

- U1: allowed in technical prose with the adjective form, the symbol and the statement. No mark in any name we choose.
- U2: the unmodified dashboard server shows its own branding to tenants; we add none. Everything we write about the service is mark-free unless a written licence is on file (T4). This gate stays part of the P2 exit.
- U3: same as U2 for enterprise sales material.
- U4: a paid third-party plugin may not carry a mark in its id or title. A mark in its description is a commercial context under the policy; the index rule of T7 (rename on a credible objection) stays, and counsel reviews the form before the first such listing.
- U5: id no, title no, description only in the T7 form and pending Q4.

Plugin signing, for the same two contexts: section 5.7 for our own app and section 5.8 for a marketplace. In one line: hosted may use a private signature on its own pools if Q2 allows it, a signed build for customers needs a paid subscription, and no catalogue of ours lists dashboard-server plugins.

---

## 7. Relicensing history and what it implies

| Date | Event | Source |
|---|---|---|
| 2021-04-20 | The dashboard server, the logs store and the traces store move from Apache-2.0 to AGPL-3.0. "Plugins, agents, and certain libraries will remain Apache-licensed". The contributor agreement is replaced by one modelled on the Apache agreement; it grants the vendor a perpetual licence to "sublicense, and distribute" contributions | Announcement and Q&A; commit `8db3eb90ae`; contributor agreement page |
| 2021-08-05 | The metrics store repository, whose earlier history is the Cortex project (Apache-2.0 by the earlier notes, not re-read), has its licence file replaced by the AGPL text ("Apply standard Grafana Labs governance and license"). That is the last change to the file | Commit `174f599c13` |
| 2022-05 to 2023-07 | The profiles store repository starts with the AGPL text (2022-05-31); `api/` gets an Apache-2.0 file (2023-01-03); `LICENSING.md` is clarified (2023-07-18), with the older code base under `og/` and four of its directories kept Apache-2.0 | Commits `b19877d202`, `9d71119f8e`, `8b99b07853` |
| 2021-07 to 2026-06 | The Apache-2.0 list of the dashboard server is edited in at least twelve commits (a data source plugin in and later out, schema directories, icons, removed packages) | History of `LICENSING.md` |
| 2026-01-30, 2026-03-16 | Charts of the dashboard server, the traces store and the logs store move to a community organisation; the vendor keeps the logs store chart for its enterprise product only | Chart READMEs; logs store issue 20705 |
| By 2026-11 | The vendor's open-source on-call project is archived in a cold-storage organisation (last push 2026-03-24). The earlier agent is archived there as well (last push 2026-07-27) | GitHub API |
| 2026-10-20 | The vendor's paid plugin marketplace is generally available | Vendor blog |
| 2026-10-30 | The metrics store chart is deprecated, last release 6.3.0, moving to the community organisation by the end of 2026 | Metrics store issue 17112 |

No licence change after 2021 was found for the five servers, and none for the agent, which stays Apache-2.0.

What it implies:

1. **The vendor can relicense again.** The contributor agreement lets it sublicense all contributions. A move to a source-available licence would not reach the tags we pin, which stay AGPL-3.0, but it would end security fixes under an open licence. The 2021 Q&A itself acknowledges community maintenance of the released code as a possible alternative to the vendor's future editions.
2. **The open-source surface is narrowing at the edges**, not at the core: deployment charts handed to a community, one product archived, enterprise editions as the supported path. Plan for the operational tooling to be ours or a community's.
3. **The Apache-2.0 sub-trees move.** A directory that is Apache-2.0 at one tag can change at the next. The allow-list is per pinned tag.
4. **The vendor is a direct competitor of the enterprise plan** in two places: it sells hosted versions of the same stack, and it now runs a paid plugin marketplace with a rule against other catalogues.
5. **The plan's answer is already in the architecture**: the default stack is a set of bundles behind service definitions, and another backend is a connector pack. This audit adds no new mitigation, only a reason to keep `aiops-only` and the reference pack green in CI (W6), since that is the proof that the product survives a change at the vendor.

---

## 8. Questions for a lawyer

| # | Question | Blocks |
|---|---|---|
| Q1 | Is a dashboard-server app plugin (a frontend bundle loaded by the AGPL frontend at run time, plus a backend that is a separate gRPC process) a separate work, so that it may be Apache-2.0 now and closed source in the enterprise edition? Does the answer differ for the frontend? | U3 for `aiops-app` |
| Q2 | Does the vendor's plugin policy bind a party that has signed nothing with the vendor: the signing subscription for a distributed plugin, and the ban on a separate public plugin catalogue? Does AGPL-3.0 §7 or §10 bear on it? May tenants of our hosted pools use a privately signed plugin there? | ADR-023; U2 and U4 for UI plugins |
| Q3 | May a commercial hosted service show the unmodified dashboard server, with its own branding, to paying tenants without a trademark licence? Can a breach of the trademark policy end rights under AGPL-3.0 or Apache-2.0, given AGPL §7 and §10 and Apache-2.0 §6? | T4; P2 exit |
| Q4 | Is "<own name> connector for <Mark>" in a plugin description or a marketplace listing, free or paid, a permitted nominative use in the jurisdictions we sell in? Is a plugin id a trademark use at all? | U5; T7 |
| Q5 | Is a Helm chart or an offline bundle that packages AGPL images or an AGPL chart next to our Apache-2.0 or commercial code an aggregate under AGPL-3.0 §5? | U1 bundle; U3 |
| Q6 | For a mirror or an air-gapped bundle, is a pointer to upstream source enough under AGPL-3.0 §6(d), or must we host the source? What does GPL-2.0 §3 require for the BPF objects in the agent and the sensor when the distribution is commercial? | L5 |
| Q7 | May a redistributor of the agent binary rely on the repository's Apache-2.0 statement when the binary links two AGPL-3.0-only packages and embeds GPL-2.0 BPF objects? | Any mirror of the agent |
| Q8 | In hosted, what counts as "modify" under AGPL-3.0 §13: a custom image that adds plugins or certificates, a build flag, provisioning? Who are the "users interacting with it remotely" for a store reached only through the gateway? | U2 |
| Q9 | For rules and dashboards adapted from Apache-2.0 mixins: what marking and `NOTICE` content does a commercial edition need? Is an alert expression restated from AGPL documentation a derivative of it? | Row 17 |
| Q10 | A BPF source file says Apache-2.0 and its object declares "GPL" or "Dual MIT/GPL" to the kernel. Which licence governs the object? | Only if we modify or redistribute |
| Q11 | The profiles store's `api/` directory has an Apache-2.0 `LICENSE` file but is absent from the Apache-2.0 list of `LICENSING.md`. Which governs? | Whether the L2 deny on `pyroscope/api` can be lifted |
| Q12 | When a third party's artifact stored in our marketplace embeds AGPL or GPL binaries, are we a conveyor with duties of our own? | Marketplace terms |

---

## 9. Changes this audit asks of the plan

| # | Where | Change |
|---|---|---|
| C1 | 08 §5.1 L6; 08 §4.1; grafana-stack §13.1 item 4 | The profiles store chart is AGPL-3.0-only. Do not vendor or package it; render our own templates |
| C2 | 08 §4.1 | Pin the dashboard server, logs store and traces store charts from `grafana-community/helm-charts`; record the signing key; plan for `mimir-distributed` moving there after 6.3.0 |
| C3 | 08 §5.1 L5, L11 | The agent binary contains AGPL-3.0-only code from the logs store. A mirror of the agent carries the AGPL §6 source as well as the GPL-2.0 offer |
| C4 | 08 §5.1 L11; `00-decisions.md` §3.3 | The BSD-2-Clause election does not cover three GPL-2.0 only headers of the sensor's BPF tree |
| C5 | 08 §5.1 L13 | Closed: Envoy is Apache-2.0 at v1.40.2; the BPF objects of the eBPF auto-instrumentation are as in section 5.5 |
| C6 | 08 §5.1 L2 | Pin the Apache-2.0 sub-tree allow-list to the tag in `go.mod`. Keep the deny on `pyroscope/api` until Q11 is answered |
| C7 | 08 §5.2 T1 to T7 | Add the password-protected clause and the "goods Grafana Labs did not create" clause. Add to T6 the signing-subscription sentence, the separate-catalogue ban and the Marketplace level |
| C8 | Community record §6; plugin record §11 | The index and `aiopsctl plugin check` refuse an artifact that contains a dashboard-server plugin |
| C9 | `00-decisions.md` §3.1 `aiops-app` | State the rule of section 5.7: no row, default bundle or conformance suite may need the app |
| C10 | `tools/upstream-components.yaml` (L3) | Record the licences of section 2 with the tags of section 10; re-read the two policy pages at every release |

---

## 10. Sources

All read on 2026-11-11. "API" is `https://api.github.com/repos/<owner>/<repo>` and its `/license?ref=<tag>` endpoint.

| Subject | URL | Version or commit |
|---|---|---|
| Dashboard server licence | `https://github.com/grafana/grafana/blob/v13.3.1/LICENSING.md`; `LICENSE`; `packages/*/package.json` and `LICENSE_APACHE2` | v13.3.1, `5db6ba8876`; `main` `8dd7488fb9d4` identical |
| Metrics store licence | `https://github.com/grafana/mimir/blob/mimir-3.2.4/LICENSING.md`; `operations/LICENSE_APACHE2` | mimir-3.2.4, `4380c34927`; `main` `414a38bc6288` identical |
| Logs store licence | `https://github.com/grafana/loki/blob/v3.7.9/LICENSING.md` | v3.7.9, `d3b4a2f76b`; `main` `937ee77f41b7` differs as noted; commit `5cf8c23a258d` for the agent's pin |
| Traces store licence | `https://github.com/grafana/tempo/blob/v3.1.1/LICENSING.md` | v3.1.1, `9db7df79d2`; `main` `150dae97feb9` identical |
| Profiles store licence and chart | `https://github.com/grafana/pyroscope/blob/v2.4.3/LICENSING.md`; `api/LICENSE`; `operations/pyroscope/helm/pyroscope/Chart.yaml` | v2.4.3, `860ef8d709`; `main` `caccd8222891` identical |
| Chart moves | `https://github.com/grafana/helm-charts` (`charts/grafana`, `tempo`, `loki`, `mimir-distributed` READMEs); `https://github.com/grafana/loki/issues/20705`; `https://github.com/grafana/mimir/issues/17112`; `https://github.com/grafana/helm-charts/issues/3955` | `main` `89a1e8bb7dcd` |
| Community charts | `https://github.com/grafana-community/helm-charts` (`LICENSE`, `README.md`, `charts/*/Chart.yaml`) | `main` `dd6eb3eba045` |
| Agent | `https://github.com/grafana/alloy/blob/v1.21.3/LICENSING.md`; `go.mod`; source archive of the tag | v1.21.3, `f2108cd783` |
| Remote config protocol | `https://github.com/grafana/alloy-remote-config` (API) | v0.0.12; `main` `39a09f3286` |
| eBPF auto-instrumentation | `https://github.com/grafana/beyla/blob/v3.40.0/LICENSE`; `https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation/tree/v1.0.0/bpf` | v3.40.0, `786c08ea50`; v1.0.0, `d23be0d6b7` |
| eBPF profiler | `https://github.com/open-telemetry/opentelemetry-ebpf-profiler` (`README.md` "Legal", `support/ebpf/LICENSE`) | `main` `d63c9e340f51` |
| Cluster collection chart | `https://github.com/grafana/k8s-monitoring-helm/tree/k8s-monitoring-4.6.0/charts/k8s-monitoring` | 4.6.0, `6c8d01e00b` |
| Plugin SDK | `https://github.com/grafana/grafana-plugin-sdk-go/blob/v0.298.0/LICENSE`; `go.mod` | v0.298.0, `48b9b17b17` |
| Frontend packages | `https://registry.npmjs.org/<package>/latest`; `https://github.com/grafana/scenes/blob/v9.2.2/LICENSE` | Versions in section 5.6; scenes `1807b6f839` |
| Image renderer | `https://github.com/grafana/grafana-image-renderer/blob/v5.12.10/LICENSE`; `Dockerfile` | v5.12.10, `189bb4fecf` |
| Alertmanager, Prometheus | `https://github.com/prometheus/alertmanager/blob/v0.35.0/LICENSE`; `https://github.com/prometheus/prometheus/blob/v3.16.0/LICENSE` | `8cf67a8e75`; `e2e28adeee` |
| Mixins and runbooks | `https://github.com/kubernetes-sigs/kubernetes-mixin` (`LICENSE`, `NOTICE`); `https://github.com/prometheus/node_exporter/tree/v1.12.1/docs/node-mixin`; `https://github.com/prometheus-operator/runbooks` (API) | version-1.5.6, `464d0ae592`; v1.12.1, `6044da7835` |
| Exporters | `https://github.com/kubernetes/kube-state-metrics/blob/v2.20.0/LICENSE`; `https://github.com/prometheus-community/windows_exporter/blob/main/LICENSE` | `4ffeda2ef8`; `main` `4eb9e6dae801` |
| Sensor | `https://github.com/cilium/tetragon/blob/v1.7.1/LICENSE`; `bpf/COPYING`; source archive of the tag | v1.7.1, `99ca93d987` |
| Gateway | `https://github.com/envoyproxy/envoy/blob/v1.40.2/LICENSE`; `NOTICE` | v1.40.2, `b2d89e0ae5` |
| MCP endpoint, cost exporter | `https://github.com/grafana/mcp-grafana/blob/v2.3.0/LICENSE`; `https://github.com/opencost/opencost/blob/v1.121.4/LICENSE` | `c8cafb6785`; `17daaa72c2` |
| Trademark policy and list | `https://grafana.com/trademark-policy/`; `https://grafana.com/trademark-policy/trademark-list/` | Page as served today |
| Plugin policy | `https://grafana.com/legal/plugins/` | "Last updated August 2026" |
| Plugin signing | `https://grafana.com/docs/grafana/latest/administration/plugin-management/plugin-sign/`; `https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin` | Pages as served today |
| Relicensing | `https://grafana.com/licensing/`; `https://grafana.com/blog/2021/04/20/grafana-loki-tempo-relicensing-to-agplv3/`; `https://grafana.com/blog/2021/04/20/qa-with-our-ceo-on-relicensing/` | Dated 2021-04-20 and 2021-04-21 |
| Contributor agreement | `https://grafana.com/docs/grafana/latest/developers/cla/` | Page as served today |
| Vendor marketplace | `https://grafana.com/blog/grafana-marketplace/` | Dated 2026-10-20 |
| Linux Foundation marks | `https://www.linuxfoundation.org/legal/trademark-usage` | Page as served today |

## 11. Unverified

- How the vendor would classify `aiops-app` (Community or Commercial), and the price and terms of a Commercial Plugin Subscription and of the Marketplace Partner Agreement. Neither document is public.
- The licence terms of dashboards published on the vendor's public dashboard site.
- The licences of the third-party code inside the upstream images (Debian Chromium in the image renderer, Envoy's bundled dependencies). We do not redistribute those images.
- The marks held by the company behind the sensor, beyond the Linux Foundation's project marks.
- Whether the agent's two AGPL imports are compiled into every released build variant. The imports are in files with no build tag.
- The exact first versions the 2021 relicensing applied to; the vendor's licensing page loads that answer dynamically and it was not read.
- The licence of the MinIO sub-chart that the profiles store chart depends on. Moot if C1 is taken.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
