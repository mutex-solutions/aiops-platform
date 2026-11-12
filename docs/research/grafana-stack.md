# Grafana open-source stack as hosting backend and agent: state as of 2026-10-27

Research for the aiops-platform design (Alloy on customer hosts -> Mimir/Loki/Tempo/Pyroscope, multi-tenant hosted by us or self-hosted by users, AIOps + Slack + on-call layer on top, plugin architecture).

Method: official docs, GitHub repos/releases, GitHub API, Grafana Labs announcements. Every fact carries the date/version it was observed for. Items I could not confirm from a primary source are marked **UNVERIFIED**. Nothing here is legal advice.

---

## 1. Executive summary

1. **Only the agent side is Apache-2.0.** Alloy, Beyla/OBI, Faro, k8s-monitoring chart, grafana-operator, mcp-grafana, grafana-llm-app, plugin SDK, Scenes are Apache-2.0. Grafana, Mimir, Loki, Tempo, Pyroscope are AGPL-3.0-only (each with small Apache-2.0 sub-trees for client/protocol code). An Apache-2.0 platform can run them as separate services behind HTTP/gRPC; it must not link their AGPL Go packages into its own binaries.
2. **All four backends use the same tenancy primitive**: `X-Scope-OrgID` header, no built-in authentication, per-tenant limits via overrides, pipe-separated IDs for cross-tenant queries. The platform must supply the auth gateway that injects/validates the header.
3. **Alloy is the single agent.** v1.21.2 (2026-10-22), ~186 components across 11 families, embeds Beyla (eBPF traces/metrics/network) and the OTel eBPF profiler, runs on Linux/Windows/macOS/FreeBSD, and can be driven centrally via `remotecfg`. The remote-config *protocol* is open (grafana/alloy-remote-config, Apache-2.0); the Grafana server implementation (Fleet Management) is Cloud-only, so we build or adopt a server.
4. **Architecture churn in 2025-2026**: Mimir 3.0 (2025-10-31) made Kafka ingest-storage the default and removed read-write mode; Tempo 3.0 (2026-05-28) removed ingesters and requires Kafka in microservices mode; Pyroscope 2.0 (2026-04-20) removed ingesters (object storage mandatory); Loki will remove Simple Scalable mode in 4.0. Monolithic modes of all four still run without Kafka.
5. **Grafana OSS is a thin tenant UI.** OSS gets orgs/teams, basic roles, OAuth/OIDC/LDAP/auth-proxy/JWT, file/Git provisioning, unified alerting with Prometheus-compatible Alertmanager. Fine-grained RBAC, SAML, SCIM, team sync are Enterprise/Cloud only.
6. **The on-call gap is real.** Grafana OnCall OSS entered maintenance 2025-03-11, repo archived 2026-03-24 (redirects to grafana-cold-storage/oncall), Cloud Connection switched off the same day; Grafana Incident was never open source; Grafana IRM is a paid Cloud add-on. Nothing open source from Grafana Labs covers on-call today.
7. **AI layer is Cloud-gated**: Grafana Assistant on-prem (Grafana >= 13.1) still needs a Grafana Cloud stack as backend; Sift, Asserts, Application Observability, Kubernetes Monitoring are Cloud-only. Open-source pieces we can reuse: mcp-grafana (v2.1.1), grafana-llm-app (stale, last release 2025-06-06), Loki pattern ingester, Tempo TraceQL metrics, Mimir ruler/alertmanager APIs.

---

## 2. Version and license matrix (observed 2026-10-27)

| Component | Latest stable | Date | License | Helm chart (repo) | Chart version |
|---|---|---|---|---|---|
| Grafana Alloy | v1.21.2 | 2026-10-22 | Apache-2.0 | `alloy` (grafana/alloy, published to grafana.github.io/helm-charts) | 1.14.3 (appVersion v1.21.2) |
| Alloy Operator | - | - | Apache-2.0 | `alloy-operator` (grafana/alloy-operator) | 0.9.3 |
| k8s-monitoring | 4.5.5 | 2026 (4.6 planned 2026-09-30 office hours) | Apache-2.0 | `k8s-monitoring` (grafana/k8s-monitoring-helm; also oci://ghcr.io/grafana/helm-charts) | 4.5.5 |
| Grafana Mimir | 3.2.3 (feature: 3.2.0) | 2026-10-22 (3.2.0: 2026-08-19; 3.0.0: 2025-10-31) | AGPL-3.0-only | `mimir-distributed` (grafana/mimir, operations/helm) | main branch shows 6.3.0-weekly.418; GA 6.x line |
| Grafana Loki | v3.7.9 (feature: 3.7.0) | 2026-10-20 (3.7.0: 2026-03-26) | AGPL-3.0-only (+Apache sub-trees) | `loki` (grafana-community/helm-charts) | 20.0.1 (appVersion 3.7.9) |
| Grafana Tempo | v3.1.1 (feature: 3.1.0) | 2026-10-21 (3.1.0: 2026-09-29; 3.0.0: 2026-05-28) | AGPL-3.0-only (+Apache sub-trees) | `tempo` 3.2.1, `tempo-distributed` 3.17.1 (grafana-community/helm-charts) | appVersion 3.1.1 |
| Grafana Pyroscope | v2.4.2 (feature: 2.4.0) | 2026-10-26 (2.4.0: 2026-10-09; 2.0.0: 2026-04-20) | AGPL-3.0-only (+Apache sub-trees) | `pyroscope` (grafana/pyroscope, operations/pyroscope/helm) | 2.4.2 |
| Grafana (OSS) | 13.2.4 (also 13.1.8, 13.0.11, 12.4.13) | 2026-10-13 (13.2.0: 2026-08-18; 13.1.0: 2026-07-01; 13.0: April 2026) | AGPL-3.0-only (+Apache packages) | `grafana` (grafana-community/helm-charts) | 14.1.0 (appVersion 13.2.4) |
| Grafana Beyla | v3.39.1 (v3.40.0 pre-release 2026-10-26) | 2026-10-22 | Apache-2.0 | n/a (embedded in Alloy; standalone image) | - |
| OpenTelemetry eBPF Instrumentation (OBI) | v0.15.2 | 2026-10-16 | Apache-2.0 | n/a | - |
| Faro Web SDK | v2.13.0 | 2026-10-22 | Apache-2.0 | n/a (npm) | - |
| mcp-grafana | v2.1.1 | 2026-10-20 | Apache-2.0 | `grafana-mcp` (grafana/helm-charts and grafana-community) | - |
| grafana-llm-app | 0.21.2 | 2025-06-06 (no release since) | Apache-2.0 | n/a (Grafana plugin) | - |
| grafana-operator | v5.25.0 | 2026-08-24 | Apache-2.0 | `grafana-operator` (oci://ghcr.io/grafana/helm-charts) | 5.25.0 |
| Grafana OnCall OSS | archived | maintenance 2025-03-11, archived 2026-03-24 | AGPL-3.0 | (chart no longer maintained) | - |
| grafana-plugin-sdk-go, @grafana/scenes | - | - | Apache-2.0 | - | - |

Sources: GitHub releases pages and `releases/latest` API for each repo; Chart.yaml files on main branches (fetched raw 2026-10-27); grafana/helm-charts `charts/` listing. Note the Helm chart migration: as of 2026-03-16 the Loki chart is maintained by Grafana Community Champions in `grafana-community/helm-charts` (which also holds `grafana`, `tempo`, `tempo-distributed`, `grafana-mcp`); `mimir-distributed` is sourced from grafana/mimir and mirrored in grafana/helm-charts; the legacy `loki-distributed`, `loki-simple-scalable`, `loki-stack`, `promtail`, `grafana-agent` charts remain only in grafana/helm-charts.

---

## 3. Grafana Alloy (the single agent)

### 3.1 What it is
Alloy describes itself as an "open source OpenTelemetry Collector distribution with built-in Prometheus pipelines" covering metrics, logs, traces and profiles (README, 2026-10). Release cadence: a minor roughly every three weeks, patches every one to two weeks. v1.21.0 (2026-10-19) upgraded to OTel Collector v0.162.0 and Beyla 3.38.0; v1.20.0 (2026-09-25) added a distroless image and GA for `loki.secretfilter`.
Sources: https://github.com/grafana/alloy ; https://github.com/grafana/alloy/releases

Supported platforms (install docs): Linux amd64/arm64, Windows amd64, macOS amd64/arm64, FreeBSD amd64. Install methods: Linux packages, macOS, Windows, Docker, Podman, Kubernetes (Helm), standalone binary, Ansible, Chef, Puppet, OpenShift. Sends anonymous usage data unless `--disable-reporting`.
Source: https://grafana.com/docs/alloy/latest/set-up/install/

### 3.2 Two engines
- **Default engine**: Alloy configuration syntax + Alloy components; has the UI, live debugging, clustering, support bundles, backward-compat guarantees.
- **OpenTelemetry Engine** (`alloy otel --config=collector.yaml`): the upstream Collector runtime embedded in Alloy; runs plain Collector YAML without translation. Marked experimental but not behind the experimental flag. Bundled extensions include `opamp`, `alloyengine` (run an Alloy-syntax pipeline alongside), basicauth, bearertokenauth, headerssetter, healthcheck, k8sleaderelector, oauth2clientauth, sigv4auth, filestorage, zpages. An `otel-supervisor` command receives config via OpAMP from Fleet Management. GrafanaCON 2026 positioned Alloy as a fully compliant OTel Collector distribution with native YAML support.
Sources: https://grafana.com/docs/alloy/latest/set-up/otel_engine/ ; https://grafana.com/docs/alloy/latest/introduction/otel_alloy/ ; https://grafana.com/docs/alloy/latest/set-up/otel_engine/engine-extension/

### 3.3 Stability levels and flags
`--stability.level` = `experimental` | `public-preview` | `generally-available` (default). Lowering it unlocks experimental components/blocks. `--feature.community-components.enabled` (default false) turns on community-maintained components (no stability level, no commercial support). `--config.format` = `alloy` | `otelcol` | `prometheus` | `promtail` | `static` converts other formats at startup (public preview). `--cluster.enabled` for clustering (viewer -> participant -> terminating states). UI + `/-/reload` + self-metrics on `--server.http.listen-addr` (default 127.0.0.1:12345). `--storage.path` default `data-alloy/`.
Sources: https://grafana.com/docs/alloy/latest/reference/cli/run/ ; https://grafana.com/docs/alloy/latest/get-started/community_components/

### 3.4 Component inventory (docs index pages, 2026-10-27)

| Family | Count | Components |
|---|---|---|
| `prometheus.*` | 38 | echo, enrich, receive_http, relabel, remote_write, scrape; operator.{podmonitors, probes, scrapeconfigs, servicemonitors}; **exporter.**{apache, azure, blackbox, cadvisor, cloudwatch, consul, databricks, dnsmasq, elasticsearch, gcp, github, kafka, memcached, mongodb, mssql, mysql, oracledb, postgres, process, redis, self, snmp, snowflake, squid, static, statsd, unix, windows} |
| `loki.*` | 23 | echo, enrich, process, relabel, rules.kubernetes, secretfilter (GA in 1.20), write; **source.**{api, awsfirehose, azure_event_hubs, cloudflare, docker, file, gcplog, gelf, heroku, journal, kafka, kubernetes, kubernetes_events, podlogs, syslog, windowsevent} |
| `otelcol.*` | 72 | auth.{basic, bearer, google, headers, oauth2, sigv4}; connector.{count, host_info, servicegraph, signaltometrics, spanlogs, spanmetrics}; encoding.{jsonlog, text}; exporter.{awss3, datadog, debug, faro, file, googlecloud, googlecloudpubsub, kafka, loadbalancing, loki, otlp, otlphttp, prometheus, splunkhec, syslog}; extension.jaeger_remote_sampling; processor.{attributes, batch, cumulativetodelta, deltatocumulative, discovery, filter, groupbyattrs, interval, k8sattributes, memory_limiter, metric_start_time, probabilistic_sampler, redaction, resourcedetection, span, tail_sampling, transform}; receiver.{awscloudwatch, awsecscontainermetrics, awss3, cloudflare, datadog, faro, file_stats, filelog, fluentforward, googlecloudpubsub, influxdb, jaeger, kafka, loki, nginx, opencensus, otlp, prometheus, solace, splunkhec, syslog, tcplog, vcenter, zipkin}; storage.file |
| `discovery.*` | 32 | aws, azure, consul, consulagent, digitalocean, dns, docker, dockerswarm, ec2, eureka, file, gce, hetzner, http, ionos, kubelet, kubernetes, kuma, lightsail, linode, marathon, nerve, nomad, openstack, ovhcloud, process, puppetdb, relabel, scaleway, serverset, triton, uyuni |
| `pyroscope.*` | 7 | ebpf, enrich, java, receive_http, relabel, scrape, write |
| `beyla.*` | 1 | beyla.ebpf |
| `database_observability.*` | 3 | mysql, postgres, sql_server (no `mssql` name) |
| `faro.*` | 1 | faro.receiver |
| `local.*` | 2 | local.file, local.file_match |
| `remote.*` | 5 | remote.http, remote.kubernetes.configmap, remote.kubernetes.secret, remote.s3, remote.vault |
| `mimir.*` | 2 | mimir.alerts.kubernetes, mimir.rules.kubernetes |

Notes on the list requested in the task: there is **no** `prometheus.exporter.kubernetes` (Kubernetes metrics come from `discovery.kubernetes` + `prometheus.scrape` of kubelet/cAdvisor/kube-state-metrics, or `prometheus.exporter.cadvisor`), and `prometheus.exporter.catchpoint` was removed in 1.21.0. Windows coverage: `prometheus.exporter.windows`, `loki.source.windowsevent`. Index pages do not show stability labels; those are on each component page (not fetched for all 186).
Sources: https://grafana.com/docs/alloy/latest/reference/components/{prometheus,loki,otelcol,discovery,pyroscope,beyla,database_observability,faro,local,remote,mimir}/

### 3.5 `remotecfg` and Fleet Management
- The `remotecfg` block (one per config) polls `url` every `poll_frequency` (default 1m, minimum 10s) sending `id` (random UUID persisted in `alloy_seed.json` if unset), `name`, and `attributes` (reserved `collector.os`, `collector.version` auto-populated). Auth: one of `authorization`, `basic_auth`, `bearer_token(_file)`, `oauth2`; `tls_config`, proxy, `http_headers`. The server "dynamically decides which configuration to serve based on the collector's `id` and `attributes`". A static HTTP server does **not** work (404/415 symptoms); use `import.http` for static files. The docs page states no stability level.
- The protocol is the **alloy-remote-config API**: protobuf + Connect-RPC (`collector.v1.CollectorService`) in github.com/grafana/alloy-remote-config, Apache-2.0, generated with buf/connect-go. The repo contains only API definitions and generation tooling, **no server**: "This code can be used to implement a remote configuration server for Grafana Alloy."
- **Grafana Fleet Management** (the server) is a Grafana Cloud product (inventory, remote configuration pipelines, health dashboards, API over HTTP/JSON + gRPC). It supports Alloy via Alloy Remote Config and any OTel Collector with the OpAMP extension (and the Alloy OTel engine via `otel-supervisor`). Docs state: "Both protocols are open source and feature alternative community-driven server implementations." It can be used from self-managed/on-prem Alloy fleets, but the control plane is hosted by Grafana.
- Community open-source servers that implement the protocol (none affiliated with Grafana Labs): `procoduck/shepherd` (Kubernetes-first, validation gates, GitOps, multi-tenant RBAC, needs Postgres), `danielfree19/alloy-fleet-manager` (Apache-2.0, vendor-neutral replacement), `opsplane-services/alloy-remote-config-server` (template-based, Redis). Maturity of each is **UNVERIFIED** (small star counts reported by third parties).
Sources: https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/ ; https://github.com/grafana/alloy-remote-config ; https://grafana.com/docs/grafana-cloud/send-data/fleet-management/introduction/ ; https://github.com/procoduck/shepherd ; https://github.com/danielfree19/alloy-fleet-manager ; https://github.com/opsplane-services/alloy-remote-config-server

### 3.6 `foreach`, `livedebugging`, clustering
- `foreach` (experimental; needs `--stability.level=experimental`): runs a separate pipeline per list element (`collection`, `var`, `template` block, optional `id`, `hash_string_id`, `enable_metrics`). Canonical use: one `prometheus.exporter.redis` per discovered pod. Components inside may reference outside exports, not vice versa.
- `livedebugging { enabled = true }` (default false) streams real-time component data to the Alloy UI; docs recommend TLS on the `http` block.
- Clustering (`--cluster.*`) distributes scrape targets across replicas; used by the k8s-monitoring `clustered` preset.
Sources: https://grafana.com/docs/alloy/latest/reference/config-blocks/foreach/ ; https://grafana.com/docs/alloy/latest/reference/config-blocks/livedebugging/

### 3.7 eBPF inside Alloy
**`beyla.ebpf`** (Alloy 1.21 embeds Beyla v3.38.0): zero-code traces + RED metrics for HTTP/S, gRPC, SQL, Redis, Kafka, Mongo, Memcached, GenAI, GPU; network flow metrics (`socket_filter` or `tc`), service graph and span metrics, process/host metrics, runtime metrics. Alloy runs Beyla as a child process. Linux only. Capabilities: `BPF`, `NET_ADMIN`, `NET_RAW`, `PERFMON`, `DAC_READ_SEARCH`, `SYS_PTRACE`, `CHECKPOINT_RESTORE`, `SYS_RESOURCE` (kernels < 5.11), `SYS_ADMIN` only for library-level instrumentation; `CAP_DAC_OVERRIDE` added to allowed set in 1.21.0. Kubernetes: `hostPID: true`, unconfined AppArmor, prefer explicit capability adds over `privileged: true`. Metrics exported as `targets` for `prometheus.scrape` (set `honor_labels = true`); traces via the required `output { traces = [...] }` block to an `otelcol.*` consumer. Kernel baseline comes from Beyla/OBI: **Linux 5.8+ with BTF** (`/sys/kernel/btf/vmlinux`), or RHEL-family 4.18 (build 348+) with backports; BTF on by default in most distros from 5.14.
**`pyroscope.ebpf`**: embeds Grafana's maintained variant of the OpenTelemetry eBPF profiler; CPU (`process_cpu`) and optional off-CPU sampling, probes; native (C/C++/Rust/Zig/Go) without host debug symbols plus HotSpot JVM, Python, Ruby, PHP, Node.js/V8, Perl, .NET. Must run as root in the host PID namespace, or with capabilities `BPF`, `PERFMON`, `SYS_PTRACE`, `CHECKPOINT_RESTORE` (kernel 5.9+, else `SYS_ADMIN`), `SYS_RESOURCE`, `DAC_READ_SEARCH`, `SYSLOG` plus read-only `/sys/kernel/tracing`; a writable symbol-cache directory; `no_kernel_version_check` for older kernels with backports; `obi_process_context_enabled` correlates profiles with Beyla trace/span IDs.
Sources: https://grafana.com/docs/alloy/latest/reference/components/beyla/beyla.ebpf/ ; https://grafana.com/docs/alloy/latest/reference/components/pyroscope/pyroscope.ebpf/ ; https://github.com/grafana/beyla ; OBI SUPPORT_MATRIX.md (raw, 2026-10-27)

### 3.8 Kubernetes layout: k8s-monitoring Helm chart (v4.5.5)
Collection-only chart (no storage, Grafana or Alertmanager). Collectors are user-defined entries in a `collectors:` map, each shaped by presets: `clustered`, `statefulset`, `daemonset`, `deployment`, `singleton`, `filesystem-log-reader`, `privileged` (root + host PID, needed for eBPF/Java profilers), plus sizing presets `small|medium|large|xlarge` (~50/250/1000/>1000 nodes). Instances are managed by the bundled `alloy-operator` subchart (0.9.3). The reference layout:

| Collector | Presets | Controller | Purpose |
|---|---|---|---|
| metrics-collector | clustered, statefulset | StatefulSet | cluster/host/cost metrics, annotation discovery, ServiceMonitor/PodMonitor |
| logs-collector | filesystem-log-reader, daemonset | DaemonSet | pod + node logs from /var/log |
| events-collector | singleton | 1 replica | cluster events, anything that must not be duplicated |
| receiver | daemonset | DaemonSet | OTLP 4317/4318 from apps (applicationObservability) |
| profiles-collector | privileged, daemonset | DaemonSet | eBPF, Java, pprof profiling |

So yes: Alloy runs as DaemonSet + clustered StatefulSet + singleton in one release. Features (all off by default except `selfReporting`): clusterMetrics, clusterEvents, costMetrics, hostMetrics, podLogsViaLoki / podLogsViaOpenTelemetry / podLogsViaKubernetesApi / podLogsObjects, nodeLogs, windowsEventLogs, kubernetesManifests (experimental), applicationObservability, autoInstrumentation (Beyla), profiling, profilesReceiver, integrations, prometheusOperatorObjects, annotationAutodiscovery, lokiLogsReceiver, prometheusMetricsReceiver. Destinations: `prometheus` (remote write), `loki`, `otlp` (metrics/logs/traces), `pyroscope`; multiple at once; per-feature `destinations` and `dataProcessors`. Optional telemetry services: kube-state-metrics, node-exporter, windows-exporter, opencost, kepler, beyla k8s cache, k8s-manifest-tail, sdkInjector. Install from `https://grafana.github.io/helm-charts` or `oci://ghcr.io/grafana/helm-charts/k8s-monitoring`.
Sources: https://github.com/grafana/k8s-monitoring-helm ; charts/k8s-monitoring/README.md ; https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/configuration/helm-chart-config/helm-chart/collector-reference/

### 3.9 Standalone Alloy Helm chart
`grafana/alloy` chart 1.14.3 (appVersion v1.21.2) from grafana.github.io/helm-charts; the Kubernetes install page only shows default install. Controller type options (DaemonSet default / Deployment / StatefulSet) are **UNVERIFIED** from the fetched page (they exist in values.yaml `controller.type` per prior knowledge; confirm before relying).
Source: https://grafana.com/docs/alloy/latest/set-up/install/kubernetes/

---

## 4. Grafana Mimir (metrics)

| Aspect | State (Mimir 3.2.x, 2026-10) |
|---|---|
| License | AGPL-3.0-only; LICENSING.md lists only `vendor/` and `operations/` under upstream licenses (no Apache-2.0 Go packages) |
| Deployment modes | `-target=all` monolithic (horizontally scalable, "not recommended for large-scale"); microservices (`-target=ingester` etc., recommended for production). **Read-write mode removed in 3.0.0** ("Remove support for the experimental read-write deployment mode" #12584) |
| Architectures | classic, and **ingest storage** (Kafka buffer between distributor and ingesters/queriers, decouples read/write paths; 3.0 made it the headline; classic still supported). Helm chart 6.0.0 reportedly defaults to ingest storage (**UNVERIFIED**, third-party summary) |
| Query engine | Mimir Query Engine default since 3.0 (`-querier.query-engine=prometheus` to revert); 3.2 adds `histogram_quantiles`, remote execution and query sharding on by default |
| Multi-tenancy | `-auth.multitenancy-enabled` (default **true**): requests must carry `X-Scope-OrgID`; when false, `-auth.no-auth-tenant` (default `anonymous`). Mimir does no authentication: "must add a layer of protection such as a reverse proxy" |
| Tenant federation | `-tenant-federation.enabled` -> `X-Scope-OrgID: a|b|c` for queries; `-tenant-federation.max-tenants` (0 = unlimited), `-tenant-federation.max-concurrent` (experimental, default 16); `-ruler.tenant-federation.enabled` for federated rule groups (`source_tenants`); series get `__tenant_id__` |
| Tenant IDs | <= 150 bytes; `0-9 a-z A-Z ! - _ . * ' ( )`; `.`/`..` and `__mimir_cluster` forbidden |
| Per-tenant limits | `limits` block defaults; runtime config file(s) (`-runtime-config.file`, reload every 10s) with `overrides: <tenant>: {ingestion_rate: ...}`; any limit overridable; `/runtime_config?mode=diff` |
| Ingestion | `POST /api/v1/push` (remote write, protobuf+snappy, `X-Prometheus-Remote-Write-Version: 0.1.0`); `POST /otlp/v1/metrics` (OTLP/HTTP protobuf, gzip ok); `POST /api/v1/push/influx/write`. Remote-Write 2.0 experimental (3.0) |
| OTLP details | Native OTLP/HTTP recommended over remote write; `service.namespace/service.name` -> `job`, `service.instance.id` -> `instance`; other resource attrs -> `target_info` (query with `info()`); experimental flags: `-distributor.otel-promote-resource-attributes`, `-distributor.otel-native-delta-ingestion`, `-distributor.otel-convert-histograms-to-nhcb`, `-distributor.otel-translation-strategy`; exponential histograms need native-histogram ingestion enabled |
| Ruler / Alertmanager | Prometheus-compatible ruler with per-tenant rule groups (`/prometheus/config/v1/rules/...`, `-ruler.enable-api`); multi-tenant Alertmanager with per-tenant config (`/api/v1/alerts`, `-alertmanager.enable-api`), both storing config in object storage |
| Storage | `s3` (+MinIO/S3-compatible), `gcs`, `azure` (hierarchical namespace must be off), `swift`, `filesystem`; `local` read-only for ruler/AM; blocks and ruler/AM stores cannot share bucket+prefix; Mimir does not create buckets |
| Helm | `mimir-distributed` (microservices on Kubernetes); monolithic via same chart is **UNVERIFIED** |

Sources: https://github.com/grafana/mimir/releases ; https://grafana.com/docs/mimir/latest/release-notes/v3.0/ ; https://grafana.com/docs/mimir/latest/references/architecture/deployment-modes/ ; https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/ ; https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/ ; https://grafana.com/docs/mimir/latest/configure/about-runtime-configuration/ ; https://grafana.com/docs/mimir/latest/references/http-api/ ; https://grafana.com/docs/mimir/latest/configure/configure-otel-collector/ ; https://grafana.com/docs/mimir/latest/configure/configure-object-storage-backend/ ; cmd/mimir/help-all.txt.tmpl (raw main, 2026-10-27); https://github.com/grafana/mimir/issues/4832

---

## 5. Grafana Loki (logs)

| Aspect | State (Loki 3.7.x, 2026-10) |
|---|---|
| License | AGPL-3.0-only; **Apache-2.0**: `clients/`, `pkg/framedstdcopy/`, `pkg/ingester/wal`, `pkg/logproto/`, `pkg/loghttp/`, `pkg/logqlmodel/`, `pkg/push`, `pkg/util`, `production/`, `tools/` |
| Deployment modes | Monolithic `-target=all` (~20 GB/day, HA with memberlist + RF3); **Simple Scalable** (`read`/`write`/`backend`, up to ~1 TB/day) is the Helm default but **deprecated, removed in Loki 4.0** (announced 2026-08-24; also removes multi-store backends, table manager; Thanos object store becomes default); new **HA monolithic** mode is the 4.0 replacement; microservices (distributor, ingester, querier, query-frontend, query-scheduler, index-gateway, compactor, ruler, pattern ingester, bloom planner/builder/gateway). No v4 tags exist yet (GitHub tags, 2026-10-27) |
| Multi-tenancy | `auth_enabled: true` by default -> `X-Scope-OrgID` required (401 "no org id" otherwise); `false` -> single tenant `fake`. Authorization "outside Loki's scope" (front with NGINX etc.) |
| Cross-tenant | `querier.multi_tenant_queries_enabled: true` (default false) -> `X-Scope-OrgID: A|B`; query endpoints only (push/tail/otlp reject multiple); `__tenant_id__` injected into results |
| Tenant IDs | <= 150 bytes (~20 suggested); same charset as Mimir; avoid colons |
| Per-tenant limits | `limits_config` + runtime overrides (`overrides: <tenant>:`), including `otlp_config` and bloom flags |
| Ingestion | `POST /loki/api/v1/push` (snappy protobuf or JSON, structured metadata object after the line); `POST /otlp/v1/logs` (OTLP/HTTP only, configure exporter endpoint as `/otlp`); needs `allow_structured_metadata: true` (default in 3.0+) |
| OTLP mapping | 17 default resource attributes become index labels (`service.name`, `service.namespace`, `service.instance.id`, `k8s.*`, `cloud.*`, `container.name`, `deployment.environment.name`); everything else -> structured metadata; dots -> `_`; per-tenant `otlp_config` rules (`index_label`, `structured_metadata`, `drop`); limits 64 KB / 128 entries per line |
| Pattern ingester | Optional component, **disabled by default** (`pattern_ingester.enabled: true`); Drain algorithm; patterns ephemeral (last ~3h, not persisted across restarts); exposes `GET /loki/api/v1/patterns`; powers Logs Drilldown; also `detected_fields` and `detected_field/{name}/values` APIs; runs in `all` and `write` targets (SSD support since 3.3.0) |
| Bloom filters | **Experimental**, no SLA; accelerates structured-metadata "needle in haystack" filters for > ~75 TB/month; components bloom-planner (single instance), bloom-builder (stateless), bloom-gateway (needs local SSD cache); not in single-binary; `bounded` sharding strategy |
| Storage | Chunks: S3, GCS, Azure Blob, IBM COS, Baidu BOS, Alibaba OSS; filesystem and S3-compatible (MinIO) flagged not production-ready; index: TSDB (recommended since 2.8), boltdb-shipper (legacy); DynamoDB/Bigtable/Cassandra deprecated. Helm chart's bundled MinIO dependency removal planned 2026-10-31 |
| Helm | `loki` chart 20.0.1 (grafana-community/helm-charts, "supporting monolithic, simple scalable, and microservices modes"); legacy `loki-distributed` in grafana/helm-charts; SSD <-> Distributed zero-downtime migration mode exists |

Sources: https://github.com/grafana/loki/releases ; LICENSING.md (raw main); https://grafana.com/docs/loki/latest/get-started/deployment-modes/ ; https://grafana.com/whats-next/2026-08-24-loki-ssd-deployment-no-longer-available/ ; https://grafana.com/docs/loki/latest/operations/multi-tenancy/ ; https://grafana.com/docs/loki/latest/reference/loki-http-api/ ; https://grafana.com/docs/loki/latest/send-data/otel/ ; https://grafana.com/docs/loki/latest/get-started/components/ ; https://grafana.com/docs/loki/latest/operations/bloom-filters/ ; https://grafana.com/docs/loki/latest/operations/storage/ ; https://grafana.com/docs/loki/latest/setup/install/helm/ ; https://grafana.com/docs/loki/latest/setup/migrate/ssd-to-distributed/

---

## 6. Grafana Tempo (traces)

| Aspect | State (Tempo 3.1.x, 2026-10) |
|---|---|
| License | AGPL-3.0-only; **Apache-2.0**: `operations/`, `pkg/tempopb` |
| 3.0 architecture (2026-05-28) | Ingester **removed entirely**; distributor -> Kafka-compatible queue (Kafka, WarpStream) -> live-store (recent traces, multi-AZ), block-builder (Parquet blocks, RF1), metrics-generator; backend-scheduler + worker replace the compactor; `scalable-single-binary` target removed. **Microservices mode requires Kafka; monolithic (`-target=all`) runs all components in-process without Kafka** |
| 3.1 (2026-09-29) | vParquet5 default (vParquet3 writes rejected), TraceQL metrics arithmetic + `with(extrapolate=true)`, redaction by TraceQL query, trace diff/summary via API/CLI/MCP (experimental), Kafka SASL/TLS/rack-aware |
| Multi-tenancy | `multitenancy_enabled: true` forces `X-Scope-OrgID` on all components (OTLP gRPC/HTTP via collector/Alloy headers); Grafana datasource via `httpHeaderName1/httpHeaderValue1`. Default single-tenant ID name **UNVERIFIED** (not on fetched page) |
| Cross-tenant | `query_frontend.multi_tenant_queries_enabled` (default **true**, no-op unless multitenancy on): `X-Scope-OrgID: foo|bar` for search, tags, trace-by-ID and TraceQL metrics; IDs normalized (sorted, deduped) |
| Per-tenant overrides | `overrides.per_tenant_override_config` file, hot-reloaded; keys for retention, ingestion limits, `max_bytes_per_trace`, metrics-generator processors; caveat: unset fields become zero values, not defaults; legacy flat overrides rejected unless `enable_legacy_overrides: true` |
| Ingestion | OTLP gRPC + HTTP (recommended), Jaeger (Thrift compact/binary/HTTP, gRPC), Zipkin HTTP; OpenCensus receiver removed in 3.0; Kafka is internal WAL, not an ingest protocol; GET (enterprise) supports OTLP only |
| TraceQL metrics | **GA in 3.0** (alerting on them experimental); functions `rate, count_over_time, sum/max/min/avg_over_time, quantile_over_time, histogram_over_time, compare, topk/bottomk`, comparison and arithmetic operators, exemplars; served by `/api/metrics/query` and `/api/metrics/query_range`; `query_frontend.metrics` (`max_duration` 24h, `query_backend_after` 15m -> live-store vs object store); Grafana 10.4+ |
| Query APIs | `/api/traces/{id}`, `/api/v2/traces/{id}` (span pruning, `application/vnd.grafana.llm`), `/api/search` (TraceQL `q`), `/api/(v2/)search/tags`, `/api/(v2/)search/tag/{tag}/values`, `POST /api/v2/traces/diff` (experimental), gRPC `StreamingQuerier` (`stream_over_http_enabled`) |
| Storage | S3 (+MinIO), GCS, Azure Blob, local filesystem (dev only); Parquet blocks |
| Helm | `tempo` 3.2.1 (single binary), `tempo-distributed` 3.17.1 (microservices) in grafana-community/helm-charts, appVersion 3.1.1 |

Sources: https://github.com/grafana/tempo/releases ; LICENSING.md (raw main); https://grafana.com/docs/tempo/latest/release-notes/v3-0/ ; https://grafana.com/docs/tempo/latest/operations/architecture/ ; https://grafana.com/docs/tempo/latest/operations/manage-advanced-systems/multitenancy/ ; https://grafana.com/docs/tempo/latest/operations/manage-advanced-systems/cross_tenant_query/ ; https://grafana.com/docs/tempo/latest/traceql/metrics-queries/ ; https://grafana.com/docs/tempo/latest/operations/traceql-metrics/ ; https://grafana.com/docs/tempo/latest/api_docs/ ; https://grafana.com/docs/tempo/latest/setup/helm-chart/

---

## 7. Grafana Pyroscope (profiles)

| Aspect | State (Pyroscope 2.4.x, 2026-10) |
|---|---|
| License | AGPL-3.0-only; **Apache-2.0**: `lidia`, `og/pkg/agent/profiler`, `og/pkg/agent/ebpfspy`, `og/packages/pyroscope-flamegraph`, `og/packages/pyroscope-models` |
| 2.0 architecture (2026-04-20) | v2 storage is the default: distributor -> **segment-writer** writes directly to object storage; **metastore** (Raft, per README "no ingesters or local disks"); **compaction-worker**; query-frontend -> **query-backend**. `-architecture.storage` = `v1` / `v1-v2-dual` (default) / `v2`; startup fails for v2 without an object-storage backend; v1 ingesters still available via `-write-path=ingester`; Go module path `github.com/grafana/pyroscope/v2`; v1 "will be formally deprecated" |
| 2.4 (2026-10-09) | experimental deterministic profile IDs, distributor inflight-bytes limit, async queries, OTLP language detection via `telemetry.sdk.language`, symdb compaction optimizations |
| Multi-tenancy | `multitenancy_enabled` / `-auth.multitenancy-enabled` (default **false** -> everything in tenant `anonymous`); when true `X-Scope-OrgID` required; "Pyroscope doesn't include an authentication layer" -> reverse proxy. Tenant federation: **UNVERIFIED / not found** |
| Ingestion | `POST /ingest` (legacy; `name`, `from`, `until`, formats folded/lines/pprof/jfr, multipart for sample_type_config/labels); `/push.v1.PusherService/Push` (Connect, HTTP/1.1 or HTTP/2, JSON or protobuf, base64 pprof); OTLP profiles accepted "from OpenTelemetry-compatible sources such as the OpenTelemetry eBPF profiler" (README) but the OTLP endpoint path is **UNVERIFIED** (not in the API reference page) |
| Query | Connect `querier.v1.QuerierService/{SelectMergeStacktraces, SelectSeries, Series, LabelNames, LabelValues, ProfileTypes, Diff, SelectHeatmap (v2 only)}`; legacy `GET /pyroscope/render` |
| Storage | S3, GCS, Azure, Swift, filesystem (single node) per v1 architecture doc; v2 requires object storage |
| Helm | `pyroscope` 2.4.2 (source grafana/pyroscope `operations/pyroscope/helm/pyroscope`, published to grafana.github.io/helm-charts); default install = single binary; `values-micro-services.yaml` for microservices (ships an in-cluster MinIO); chart 2.0.0 replaced `all.enable-v1-*` with `architecture.storage` |
| SDKs | push SDKs for Go, Java, Python, Ruby, Node.js, .NET, Rust; Alloy `pyroscope.scrape`/`pyroscope.ebpf`/`pyroscope.java`/`pyroscope.receive_http` |

Sources: https://github.com/grafana/pyroscope ; https://github.com/grafana/pyroscope/releases ; LICENSING.md (raw main); https://grafana.com/docs/pyroscope/latest/release-notes/v2-0/ ; https://grafana.com/docs/pyroscope/latest/reference-pyroscope-architecture/about-grafana-pyroscope-architecture/ ; https://grafana.com/docs/pyroscope/latest/configure-server/about-tenant-ids/ ; https://grafana.com/docs/pyroscope/latest/reference-server-api/ ; https://grafana.com/docs/pyroscope/latest/deploy-kubernetes/helm/

---

## 8. Beyla and OpenTelemetry eBPF Instrumentation (OBI)

- Beyla "has been donated to the CNCF OpenTelemetry Project" and now lives upstream as OBI (`open-telemetry/opentelemetry-ebpf-instrumentation`; `open-telemetry/obi` is not a repo). grafana/beyla is a downstream distribution that vendors OBI via a git submodule; all Beyla maintainers work full time upstream; non-doc PRs go to OBI. Beyla stays active (v3.39.1, 2026-10-22; Apache-2.0).
- OBI: Apache-2.0, v0.15.2 (2026-10-16), "currently in Development", stays in `v0` with breaking changes between minors; pin tags. Runtime: Linux 5.8+ with BTF (RHEL-family 4.18+ with backports: RHEL/CentOS/Rocky/Alma 8), amd64/arm64, root or per-feature capabilities (`OTEL_EBPF_ENFORCE_SYS_CAPS`); verifier CI covers 5.10 - 7.2 kernels, RHEL 8.9/8.10/9.6, AL2023. Can run in host PID namespace or as a sidecar sharing the pod PID namespace.
- Implication: Alloy's `beyla.ebpf` gives us OBI capability without taking an OBI dependency ourselves; version lag is set by Alloy's embedded Beyla (3.38.0 in Alloy 1.21).
Sources: https://github.com/grafana/beyla ; https://github.com/grafana/beyla/releases ; https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation ; SUPPORT_MATRIX.md (raw main, 2026-10-27)

## 9. Faro (frontend RUM)

`@grafana/faro-web-sdk` v2.13.0 (2026-10-22), Apache-2.0. Collects Web Vitals (v5 library, FID dropped), console/errors, logs, sessions, traces (`@grafana/faro-web-tracing`), session replay, user actions (rage/dead/error clicks in 2.13). Packages: faro-core, faro-web-sdk, faro-web-tracing, faro-react. Ships to Alloy `faro.receiver` (or `otelcol.receiver.faro`), which routes to Loki/Tempo. Browser and client side of SSR frameworks only; React Native, Node.js, workers explicitly unsupported. Grafana Cloud "Frontend Observability" mobile SDKs: **UNVERIFIED** (not found in this repo).
Sources: https://github.com/grafana/faro-web-sdk ; https://github.com/grafana/faro-web-sdk/releases

---

## 10. Grafana OSS (13.x)

### 10.1 Release state and license
13.2.4 is latest (2026-10-13); branches 13.2 / 13.1 / 13.0 / 12.4 get coordinated security batches roughly every two weeks. 13.0 landed around GrafanaCON 2026 (what's-new entries dated 2026-04-20/21; 13.1.0 2026-07-01; 13.2.0 2026-08-18). License AGPL-3.0-only; Apache-2.0 exceptions: `packages/grafana-data`, `grafana-e2e-selectors`, `grafana-runtime`, `grafana-ui`, `packaging/`, `kinds/`, `pkg/kinds/`, `pkg/kindsys/`, `pkg/registry/schemas/`, `grafana-mixin/`, TraceView components, icon sets. Separate Apache-2.0 repos: `grafana-plugin-sdk-go`, `scenes`.
13.0 headline: dynamic dashboards (v2 schema) GA and migration-on-open; Git Sync GA (OSS + Cloud; 13.0.0 had a data-loss migration bug fixed in 13.0.1); Grafana Advisor GA; Assistant to OSS/Enterprise (public preview); Scenes toggle removed (pre-Scenes dashboards gone); `/api` deprecated in favour of `/apis`; Image Renderer plugin support removed; `grafana-cli`/`grafana-server` replaced by `grafana cli` / `grafana server`; React 19; legacy single-tenant Alertmanager config endpoints removed.
Sources: https://github.com/grafana/grafana/releases ; LICENSING.md (raw main); https://grafana.com/docs/grafana/latest/whatsnew/whats-new-in-v13-0/ ; GitHub API releases list

### 10.2 Orgs vs teams for multi-tenancy
An organization isolates folders, dashboards, data sources, alerts, notification channels, annotations, reports, service accounts and teams; auth providers, config and licenses are shared; a user may belong to several orgs; teams exist inside an org. Resources "cannot be easily shared with another organization"; deleting an org deletes its teams and dashboards; only Server Admins manage orgs. The docs frame orgs as "easier and cheaper to manage than multiple instances". The fetched page makes no recommendation of teams+RBAC over orgs (**UNVERIFIED** claim often repeated; Grafana Cloud itself uses one org per stack). OSS roles: Grafana Admin, Org Admin, Editor, Viewer, None; fixed/custom RBAC roles are "Available in Grafana Enterprise and Grafana Cloud".
Sources: https://grafana.com/docs/grafana/latest/administration/organization-management/ ; https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/access-control/

### 10.3 Authentication
OSS: anonymous, basic, **auth proxy** (header-based), generic OAuth (OIDC with refresh tokens), Entra ID / GitHub / GitLab / Google / Okta / Grafana.com OAuth, LDAP (login + sync on login), **JWT**; one provider per type. Enterprise/Cloud only: SAML, Team Sync, active LDAP sync, protected roles, **SCIM** (Grafana 12; Entra ID and Okta; "Grafana Enterprise or a paid Grafana Cloud account"; config UI public preview 12.2, 2025-09-10). No built-in MFA (use the IdP). The free unlicensed Enterprise binary does not unlock licensed features.
Sources: https://grafana.com/docs/grafana/latest/setup-grafana/configure-security/configure-authentication/ ; https://grafana.com/docs/grafana/latest/setup-grafana/configure-access/configure-scim-provisioning/ ; https://grafana.com/whats-new/2025-09-10-scim-configuration-ui/

### 10.4 Provisioning
File provisioning (`provisioning/{datasources,dashboards,plugins,alerting}`) with `$ENV`/`${ENV}` interpolation (values only, not inside dashboard JSON), `prune: true` for data sources, dashboards as classic JSON or Kubernetes-style resources (required for v2), folder structure mirroring, `disableDeletion`/`allowUiUpdates`. Alerting provisioning covers rules, contact points, policies, templates, mute timings (separate guide). Git Sync GA in 13.0. HTTP API (`/apis`) for everything above. **grafana-operator** v5.25.0 (Apache-2.0) manages Grafana instances and resources via CRs (`grafana.integreatly.org/v1beta1`: `Grafana`, `GrafanaDashboard` confirmed on README; datasource/folder/alerting CRDs **UNVERIFIED** from this fetch), supports external Grafana instances, requires Grafana >= 11 since 5.25.
Sources: https://grafana.com/docs/grafana/latest/administration/provisioning/ ; https://github.com/grafana/grafana-operator ; https://github.com/grafana/grafana-operator/releases

### 10.5 Alerting
- Two rule types: **Grafana-managed** (evaluated by Grafana; multi-datasource, expressions, images, no-data/error handling) and **data source-managed** (Mimir/Loki ruler; details on separate comparison page, not fetched). Recording rules supported.
- Built-in Grafana Alertmanager "extends the Prometheus Alertmanager"; handles only Grafana-managed alerts; config snapshots (max 100 per org).
- External Alertmanager as a data source: implementations Mimir, Cortex, Prometheus; "Receive Grafana Alerts" + enable in Settings forwards **all** Grafana-managed alerts to it; for a subset use the Alertmanager contact point. Prometheus Alertmanager: silences editable from Grafana, contact points/policies/templates read-only (no update API). AWS AMP Alertmanager not supported.
- **State history** `[unified_alerting.state_history]`: `backend = loki | prometheus | multiple` (`primary`/`secondaries`), `loki_remote_url`, `prometheus_target_datasource_uid`, metric `GRAFANA_ALERTS{alertname, alertstate, grafana_alertstate, grafana_rule_uid,...}` via remote write; Loki enables the History views (dedicated Loki instance recommended); `annotations` backend and `loki_tenant_id`/`loki_basic_auth_*` keys **UNVERIFIED** from the fetched page.
- 13.0: `grafana.com/provenance` enforced on Kubernetes-style notification APIs; Alertmanager status endpoint needs `alert.notifications.system-status:read`.
Sources: https://grafana.com/docs/grafana/latest/alerting/fundamentals/alert-rules/ ; https://grafana.com/docs/grafana/latest/alerting/set-up/configure-alertmanager/ ; https://grafana.com/docs/grafana/latest/alerting/set-up/configure-alert-state-history/

### 10.6 Plugin model
- Types: panel, data source, app (apps bundle data sources/panels, pages, Scenes, UI extensions; enabled per org; provisionable).
- Backend plugins (since v7.0): Go, `grafana-plugin-sdk-go`, each backend launched as a subprocess and spoken to over **gRPC via HashiCorp go-plugin**; capabilities: query data (data source backends), resource handlers (arbitrary HTTP), health checks, metrics, streaming; enable Alerting/recorded queries/caching; instance management caches per-datasource/app instances. Apps can have backends.
- Scenes (`@grafana/scenes`, Apache-2.0): `EmbeddedScene`, `SceneFlexLayout`, `VizPanel`, `PanelBuilders`, variables, URL sync, time range, `@grafana/scenes-ml`; since 13.0 the only dashboard architecture.
- Signing: public plugins reviewed and signed Community/Commercial; **private** plugins signed with `--rootUrls` bound to specific instance URLs; both need a Grafana Cloud Access Policy token with `plugins:write`. Unsigned plugins load only via `[plugins] allow_loading_unsigned_plugins = <ids>` (defaults.ini: "Plugins with modified signatures are never loaded") or `app_mode = development`.
Sources: https://grafana.com/developers/plugin-tools/key-concepts/plugin-types-usage ; https://grafana.com/developers/plugin-tools/key-concepts/backend-plugins/ ; https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin ; https://grafana.com/developers/scenes/ ; conf/defaults.ini (raw main)

### 10.7 AI features: what is OSS, what is Cloud
| Feature | Availability (2026-10) |
|---|---|
| Grafana Assistant | Cloud public preview 2025-08-14; extended to OSS/Enterprise 2026-04-21. Self-managed needs **Grafana >= 13.1.0**, the `grafana-assistant-app` plugin, and a **Grafana Cloud stack** as backend/billing; prompts + query context go to the Cloud backend (region of the stack); data sources stay on-prem. Not on-prem: Investigations, Slack integration, infrastructure memory, Cloud MCP connections, CLI tokens, SQL table discovery, Automations. July 2026 "AI Week" GA'd Investigations, Workspace, Automations, Cloud MCP server, gcx, Agent Observability (Cloud) |
| Sift | Cloud-only diagnostic assistant; no public repo (github.com/grafana/sift -> 404) |
| Asserts, Application Observability, Kubernetes Monitoring | Cloud-only curated apps ("Kubernetes Monitoring is available only in Grafana Cloud"); collection side (Alloy, k8s-monitoring chart, Beyla) is OSS |
| mcp-grafana | Apache-2.0, v2.1.1 (2026-10-20); tools for dashboards, datasources, Prometheus/Loki/Tempo/Pyroscope queries, alerting (2.1.0 can target Mimir/Loki rulers with `managed_by: datasource`), Incident, OnCall, Sift, Asserts, annotations, deeplinks, raw API; `stdio`, `sse`, `streamable-http` (recommended for per-caller credentials / multi-tenant); only `agento11y` tools are Cloud-only; each tool declares required RBAC actions |
| grafana-llm-app | Apache-2.0, last release 0.21.2 on **2025-06-06**: LLM proxy app plugin (OpenAI, Anthropic via OpenAI-compatible API, customisable OpenAI path; Azure **UNVERIFIED**), embeds mcp-grafana v0.4.2 (old), MCP enabled by default; treat as stale |
Sources: https://grafana.com/whats-new/2026-04-21-grafana-assistant-becomes-available-on-prem/ ; https://grafana.com/docs/grafana-cloud/machine-learning/assistant/on-premise/ ; https://grafana.com/press/2026/07/27/grafana-labs-ships-six-tools-that-power-agentic-operations-from-planning-to-production/ ; https://grafana.com/docs/grafana-cloud/machine-learning/sift/ ; https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/configuration/ ; https://github.com/grafana/mcp-grafana ; https://github.com/grafana/mcp-grafana/releases ; https://github.com/grafana/grafana-llm-app/releases

---

## 11. On-call and incident: OnCall OSS, Grafana IRM, Grafana Incident

| Date | Event |
|---|---|
| 2025-03-11 | Grafana OnCall (OSS) enters maintenance mode (critical bugs + CVEs with CVSS >= 7.0 only); Grafana Cloud IRM announced (OnCall Cloud + Incident merged) |
| 2025-04-21 | Separate OnCall (Cloud) and Incident apps sunset in favour of IRM |
| 2026-03-24 | OnCall OSS repo archived (README: "archived on 2026-03-24"; last push 2026-03-24T11:14Z); Cloud Connection, Grafana APIs and mobile app stop working with OSS (SMS/phone/push gone; use Twilio/Exotel/Zvonok or webhooks to Pushover/Gotify/ntfy) |
| 2026-06-05 | GitHub repo-archive banner date; repo now lives at `grafana-cold-storage/oncall` (github.com/grafana/oncall redirects) |

OnCall OSS remains AGPL-3.0; its maintainers explicitly encourage community-maintained editions. **Grafana Incident** is "a closed source, cloud-only incident management tool"; never open source. **Grafana IRM** = "the on-call management and incident response product in Grafana Cloud": schedules/rotations/overrides, escalation chains, alert groups, incident declaration/roles/post-incident review, Slack / Microsoft Teams / mobile; "a paid Grafana Cloud add-on, billed based on monthly active IRM users". Free tier up to 3 active IRM users and a self-managed IRM option for Enterprise contracts are **UNVERIFIED** (third-party comparison only; the official IRM page mentions neither).
Sources: https://github.com/grafana/oncall (-> grafana-cold-storage/oncall) ; GitHub API `repos/grafana/oncall` (`archived: true`); https://grafana.com/blog/grafana-oncall-maintenance-mode/ ; https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/ ; https://grafana.com/blog/2025/03/11/oncall-management-incident-response-grafana-cloud-irm/

---

## 12. Cross-backend comparison

### 12.1 Multi-tenancy
| | Mimir 3.2 | Loki 3.7 | Tempo 3.1 | Pyroscope 2.4 |
|---|---|---|---|---|
| Enable flag | `-auth.multitenancy-enabled` (default true) | `auth_enabled` (default true) | `multitenancy_enabled` (default false) | `multitenancy_enabled` (default false) |
| Tenant header | `X-Scope-OrgID` | `X-Scope-OrgID` | `X-Scope-OrgID` | `X-Scope-OrgID` |
| Default tenant when off | `anonymous` (`-auth.no-auth-tenant`) | `fake` | single-tenant (name UNVERIFIED) | `anonymous` |
| Cross-tenant query | `-tenant-federation.enabled`, `a|b`, `max-tenants` | `multi_tenant_queries_enabled` (default false), `a|b` | `multi_tenant_queries_enabled` (default true), `a|b` | UNVERIFIED |
| Per-tenant limits | runtime `overrides:` | runtime `overrides:` | `per_tenant_override_config` file | `overrides` (reference config) |
| Built-in auth | none | none | none | none |
| Tenant ID rules | <=150 bytes, restricted charset | same | not fetched | same family (about-tenant-ids page) |

### 12.2 Ingestion endpoints
| Signal | Endpoint | Protocol |
|---|---|---|
| Metrics | Mimir `POST /api/v1/push` | Prometheus remote write 1.0 (2.0 experimental) |
| Metrics | Mimir `POST /otlp/v1/metrics` | OTLP/HTTP protobuf (gzip) |
| Metrics | Mimir `POST /api/v1/push/influx/write` | Influx line protocol |
| Logs | Loki `POST /loki/api/v1/push` | snappy protobuf or JSON |
| Logs | Loki `POST /otlp/v1/logs` | OTLP/HTTP only |
| Traces | Tempo distributor | OTLP gRPC 4317 / HTTP 4318 (ports per Collector convention; not restated on API page), Jaeger, Zipkin |
| Profiles | Pyroscope `POST /ingest`, `/push.v1.PusherService/Push` | legacy HTTP, Connect; OTLP profiles path UNVERIFIED |
| Frontend | Alloy `faro.receiver` | Faro JSON -> Loki/Tempo |

### 12.3 Deployment modes and Kafka
| | Monolithic | Scalable without Kafka | Microservices |
|---|---|---|---|
| Mimir 3.x | `-target=all`, horizontally scalable | classic architecture (ingesters) still supported | ingest-storage (Kafka) or classic |
| Loki 3.7 | `-target=all` (~20 GB/day) | SSD (deprecated, gone in 4.0) -> HA monolithic in 4.0 | `loki` chart distributed mode; no Kafka requirement |
| Tempo 3.x | `-target=all`, no Kafka | none (scalable-single-binary removed) | **Kafka required** |
| Pyroscope 2.x | single binary (object storage required for v2) | - | distributor / segment-writer / metastore / compaction-worker / query-frontend / query-backend |

---

## 13. Licensing and trademark implications for an Apache-2.0 platform

### 13.1 AGPL services behind a service boundary
- Grafana Labs' own position (CEO Q&A, 2021-04-20): "Unmodified distributions are not affected"; shipping Grafana inside a product/service is permitted if license terms are followed; "Plugins, agents, and certain libraries will remain Apache-licensed"; only those who **modify** AGPL code and offer it over a network without publishing changes need a commercial license.
- FSF GPL FAQ: aggregation vs. combination depends on the communication mechanism and semantics; "pipes, sockets and command-line arguments are communication mechanisms normally used between two separate programs"; linking in a shared address space "almost surely means combining them into one program"; containers do not change the analysis (`AggregateContainers`). AGPL section 13 obligations attach to **modified** versions offered over a network (`UnreleasedModsAGPL`); merely running a program without conveying it requires "Nothing" (`NoDistributionRequirements`). The FAQ has no entry stating that a separate program talking to an AGPL program over a network API becomes AGPL; the general separate-programs test applies.
- Practical rules for aiops-platform (engineering interpretation, not legal advice):
  1. Run Grafana/Mimir/Loki/Tempo/Pyroscope as **separate processes/containers** talking HTTP/gRPC; our Apache-2.0 services never import their AGPL Go packages (e.g. `github.com/grafana/mimir/pkg/...`, `github.com/grafana/loki/pkg/logql`, `github.com/grafana/tempo/pkg/traceql`).
  2. Importing the **Apache-2.0 sub-trees** is fine: Loki `pkg/push`, `pkg/logproto`, `pkg/loghttp`, `pkg/logqlmodel`, `pkg/util`, `clients/`; Tempo `pkg/tempopb`; Grafana `packages/grafana-{data,ui,runtime}`, `grafana-plugin-sdk-go`, `@grafana/scenes`; Mimir has **no** Apache sub-tree (use HTTP/remote-write protobuf from Prometheus instead).
     - Correction, 2026-10-28: Pyroscope has no usable Apache sub-tree either. Its `LICENSING.md` on `main` lists only `lidia`, `og/pkg/agent/profiler`, `og/pkg/agent/ebpfspy`, `og/packages/pyroscope-flamegraph` and `og/packages/pyroscope-models` as Apache-2.0; everything else, including `api/` (the Connect push and query protos and their generated Go types), is AGPL-3.0-only. Source: https://github.com/grafana/pyroscope/blob/main/LICENSING.md
     - Addition, 2026-10-28: on npm the license is per package, not per directory. `@grafana/prometheus` 13.2.2, `@grafana/o11y-ds-frontend` 13.3.0 and `@grafana/sql` 13.3.0 are published as AGPL-3.0-only. `@grafana/data`, `@grafana/ui`, `@grafana/runtime`, `@grafana/schema`, `@grafana/i18n` and `@grafana/e2e-selectors` (13.3.0) and `@grafana/scenes` (9.1.0) are Apache-2.0. Source: npm registry metadata, `https://registry.npmjs.org/<package>/latest`, read 2026-10-28.
  3. If we patch an AGPL service (e.g. a modified Loki build), we must publish the modified source for that service; because our platform is open source anyway this is a packaging obligation, not a blocker, but keep patches in a clearly separate repo with its own AGPL license.
  4. Helm charts and `operations/` directories are Apache-2.0 (Loki, Tempo, Mimir, Pyroscope) and can be vendored.
  5. Distributing a bundle (our chart + AGPL images) is "mere aggregation"; include each component's license text.
Sources: https://grafana.com/blog/2021/04/20/qa-with-our-ceo-on-relicensing/ ; https://grafana.com/licensing/ ; https://www.gnu.org/licenses/gpl-faq.html (MereAggregation, GPLPlugins, AggregateContainers, UnreleasedModsAGPL, AGPLv3InteractingRemotely, NoDistributionRequirements); LICENSING.md files cited above

### 13.2 Trademarks
- Registered: Grafana®, Loki®, Tempo®, Grafana logo®, k6 logo®. Unregistered (TM): Mimir™, Pyroscope™, Faro™, Grafana OnCall™, Pyroscope logo™. Alloy and Beyla are **not on the list** (list is non-exhaustive).
- Policy: marks may be used for open-source discussion/development/support; use as adjective + generic noun, visually distinguished, with ®/TM; **prohibited**: using marks in your product/service names, company logos, domain names, social handles, metatags, ad keywords; altering logos; more prominent than your own name; use with "derivative software incorporating modified Grafana Labs software product code"; implying endorsement. Required attribution, "prominently in all materials using the Grafana Labs Marks, including but not limited to websites, publications, splash screens, screenshots or in documentation": "The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates." (second sentence recorded 2026-10-28; earlier versions of this note had only the first). The policy also says: "Do not use the Grafana Labs Marks in any commercial or marketing context other than as expressly permitted", and any other use "requires a separate trademark license agreement". Violations are treated as a material breach of the open-source licenses. Permission/questions: hello@grafana.com. Phrasings like "for Grafana" / "works with Grafana" are not explicitly addressed (**UNVERIFIED** whether allowed without written permission).
- Implications: do not name the product/repos/domains with "Grafana", "Loki", "Tempo", "Mimir"; describe compatibility in prose ("stores metrics in Grafana Mimir®"); if we ship a modified Grafana build, strip Grafana branding; keep the attribution statement in docs/UI where marks appear.
Sources: https://grafana.com/trademark-policy/ (re-read 2026-10-28) ; https://grafana.com/trademark-policy/trademark-list/

### 13.3 Object storage and OSS editions (added 2026-10-28)
- **MinIO**: `github.com/minio/minio` declares AGPL-3.0 and is archived (last push 2026-04-24; the SeaweedFS README below dates the end of development to 2026-04-25). Its README opens with "THIS REPOSITORY IS NO LONGER MAINTAINED.", says "The MinIO community edition is now distributed as source code only. We will no longer provide pre-compiled binary releases for the community version.", and points to the AIStor Free and AIStor Enterprise products. No maintained community images or security fixes can be assumed. The Loki chart and the Pyroscope microservices values bundle a MinIO subchart. Source: https://api.github.com/repos/minio/minio (read 2026-10-28); https://github.com/minio/minio
- **SeaweedFS**: `github.com/seaweedfs/seaweedfs` is Apache-2.0 (LICENSE file and README "License" section), actively maintained (push 2026-10-28), with an S3 API. A separate commercial "SeaweedFS Enterprise Edition" adds data recovery, self-healing storage, custom erasure coding and EC repair. Source: https://github.com/seaweedfs/seaweedfs (README, LICENSE, read 2026-10-28)
- **Enterprise editions** (reported by the 2026-10-28 licensing review, not re-checked here, so UNVERIFIED): Grafana publishes `grafana/grafana-enterprise` next to `grafana/grafana` under a proprietary license, and the upstream `mimir-distributed`, `loki` and `tempo-distributed` charts carry `enterprise` toggles for GEM, GEL and GET.

### 13.4 Plugin signature levels (added 2026-10-28)
- From the Grafana plugin policy: **Private** signatures are free; private plugins "are for use on your own Grafana instance. They may not be shared to the Grafana community or to your customers". **Community** signatures are free for plugins whose "dependent technologies [...] are open source and/or not for profit" and that are "Not commercial in nature and not affiliated with any commercial endeavor". **Commercial** plugins "are offered in connection with a commercial offering, commercial dependencies or offered by a for-profit business" and need a Commercial Plugin Subscription. Source: https://grafana.com/legal/plugins/ (read 2026-10-28)

---

## 14. What this means for aiops-platform (architect notes)

1. **Agent = unmodified Alloy** (Apache-2.0) with a generated `config.alloy`; our "fleet" plugin implements `collector.v1.CollectorService` (alloy-remote-config) so one `remotecfg {}` block per host is the entire customer-side config. Evaluate shepherd / alloy-fleet-manager before writing our own; OpAMP via the OTel engine is the second path for pure OTel collectors.
2. **Tenant gateway** is mandatory: none of the four backends authenticate. One auth proxy issuing `X-Scope-OrgID` from the customer's token covers remote_write, OTLP, Loki push, Tempo OTLP, Pyroscope push, and Grafana data sources (`httpHeaderName1`).
3. **Self-hosted profile** should default to monolithic/single-binary for all four (no Kafka): Mimir `-target=all`, Loki monolithic (prepare for 4.0 HA-monolithic, avoid SSD), Tempo `-target=all`, Pyroscope single binary with object storage. Our hosted profile uses microservices with Kafka (Mimir ingest-storage, Tempo 3 requirement).
4. **Grafana OSS per tenant**: one org per tenant (or one Grafana per tenant for hard isolation), auth-proxy or OIDC from our IdP, provisioning via `/apis` or grafana-operator; fine-grained RBAC/SCIM/SAML must come from our side or Enterprise.
5. **Alerting**: Grafana-managed rules + Mimir/Loki rulers; our on-call plugin receives alerts via Alertmanager webhook contact point (Grafana AM or Mimir AM), which keeps the platform decoupled from OnCall OSS's archived code. OnCall OSS (AGPL) can be mined for schedule/escalation data models but should not be linked.
6. **AI layer**: mcp-grafana is the reusable, current, Apache-2.0 bridge (streamable-http, per-tenant credentials); grafana-llm-app is stale; Assistant/Sift/Asserts are not reusable on-prem.
7. **UI**: a Grafana app plugin (Scenes + Go backend over gRPC) for in-Grafana views, privately signed per instance (`rootUrls`) or allow-listed as unsigned for self-hosters.

---

## 15. Unverified items and open questions

**UNVERIFIED (not confirmed from a primary source in this pass)**
- Alloy Helm chart `controller.type` options (DaemonSet default / Deployment / StatefulSet).
- Maturity/activity of community remotecfg servers (shepherd, alloy-fleet-manager, opsplane).
- Mimir Helm chart 6.0 defaulting to ingest-storage; whether `mimir-distributed` supports monolithic mode.
- Tempo's default single-tenant ID name when multitenancy is disabled.
- Pyroscope OTLP profiles endpoint path and cross-tenant query support.
- Grafana alert state history `annotations` backend and `loki_tenant_id` / `loki_basic_auth_*` keys.
- grafana-operator CRD list beyond `Grafana` and `GrafanaDashboard`.
- grafana-llm-app Azure OpenAI support.
- Grafana IRM free tier (3 users) and self-managed IRM for Enterprise.
- Faro mobile SDKs.
- Trademark policy stance on "works with Grafana" phrasing.
- Exact Grafana 13.0.0 release timestamp (tag exists; release object not returned by API).

**Open questions for the design**
- Build vs adopt the remotecfg server; whether to also speak OpAMP.
- How to scope Mimir/Loki/Tempo per-tenant overrides from our control plane (runtime-config ConfigMap vs Tempo overrides file vs Pyroscope reference config) in one abstraction.
- Loki 4.0 timing and the HA-monolithic mode: do we wait for 4.0 before shipping the self-hosted profile?
- Whether to run one Grafana per tenant or one org per tenant in the hosted profile (orgs share auth config; Assistant and plugins are per-org).
- Kafka as a hard dependency in the hosted profile (Mimir ingest-storage + Tempo 3) vs WarpStream/Redpanda.

---

## 16. Sources (all accessed 2026-10-27)

Alloy: https://github.com/grafana/alloy , https://github.com/grafana/alloy/releases , https://grafana.com/docs/alloy/latest/set-up/install/ , https://grafana.com/docs/alloy/latest/set-up/install/kubernetes/ , https://grafana.com/docs/alloy/latest/reference/cli/run/ , https://grafana.com/docs/alloy/latest/get-started/community_components/ , https://grafana.com/docs/alloy/latest/reference/components/ (and family sub-pages prometheus, loki, otelcol, discovery, pyroscope, beyla, database_observability, faro, local, remote, mimir), https://grafana.com/docs/alloy/latest/reference/components/beyla/beyla.ebpf/ , https://grafana.com/docs/alloy/latest/reference/components/pyroscope/pyroscope.ebpf/ , https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/ , https://grafana.com/docs/alloy/latest/reference/config-blocks/foreach/ , https://grafana.com/docs/alloy/latest/reference/config-blocks/livedebugging/ , https://grafana.com/docs/alloy/latest/set-up/otel_engine/ , https://grafana.com/docs/alloy/latest/introduction/otel_alloy/ , https://github.com/grafana/alloy-remote-config , https://grafana.com/docs/grafana-cloud/send-data/fleet-management/introduction/ , https://github.com/procoduck/shepherd , https://github.com/danielfree19/alloy-fleet-manager , https://github.com/opsplane-services/alloy-remote-config-server , https://github.com/grafana/k8s-monitoring-helm , https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/configuration/helm-chart-config/helm-chart/collector-reference/ , https://github.com/grafana/alloy-operator
Mimir: https://github.com/grafana/mimir , https://github.com/grafana/mimir/releases , https://grafana.com/docs/mimir/latest/release-notes/v3.0/ , https://grafana.com/docs/mimir/latest/references/architecture/deployment-modes/ , https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/ , https://grafana.com/docs/mimir/latest/configure/about-tenant-ids/ , https://grafana.com/docs/mimir/latest/configure/about-runtime-configuration/ , https://grafana.com/docs/mimir/latest/references/http-api/ , https://grafana.com/docs/mimir/latest/configure/configure-otel-collector/ , https://grafana.com/docs/mimir/latest/configure/configure-object-storage-backend/ , https://github.com/grafana/mimir/issues/4832
Loki: https://github.com/grafana/loki , https://github.com/grafana/loki/releases , https://grafana.com/docs/loki/latest/get-started/deployment-modes/ , https://grafana.com/docs/loki/latest/get-started/components/ , https://grafana.com/docs/loki/latest/operations/multi-tenancy/ , https://grafana.com/docs/loki/latest/reference/loki-http-api/ , https://grafana.com/docs/loki/latest/send-data/otel/ , https://grafana.com/docs/loki/latest/operations/bloom-filters/ , https://grafana.com/docs/loki/latest/operations/storage/ , https://grafana.com/docs/loki/latest/setup/install/helm/ , https://grafana.com/whats-next/2026-08-24-loki-ssd-deployment-no-longer-available/ , https://grafana.com/docs/loki/latest/setup/migrate/ssd-to-distributed/ , https://grafana.com/docs/grafana/latest/explore/simplified-exploration/logs/patterns/
Tempo: https://github.com/grafana/tempo , https://github.com/grafana/tempo/releases , https://grafana.com/docs/tempo/latest/release-notes/v3-0/ , https://grafana.com/docs/tempo/latest/operations/architecture/ , https://grafana.com/docs/tempo/latest/operations/manage-advanced-systems/multitenancy/ , https://grafana.com/docs/tempo/latest/operations/manage-advanced-systems/cross_tenant_query/ , https://grafana.com/docs/tempo/latest/traceql/metrics-queries/ , https://grafana.com/docs/tempo/latest/operations/traceql-metrics/ , https://grafana.com/docs/tempo/latest/api_docs/ , https://grafana.com/docs/tempo/latest/setup/helm-chart/
Pyroscope: https://github.com/grafana/pyroscope , https://github.com/grafana/pyroscope/releases , https://grafana.com/docs/pyroscope/latest/release-notes/v2-0/ , https://grafana.com/docs/pyroscope/latest/reference-pyroscope-architecture/about-grafana-pyroscope-architecture/ , https://grafana.com/docs/pyroscope/latest/configure-server/about-tenant-ids/ , https://grafana.com/docs/pyroscope/latest/reference-server-api/ , https://grafana.com/docs/pyroscope/latest/deploy-kubernetes/helm/
Beyla/OBI/Faro: https://github.com/grafana/beyla , https://github.com/grafana/beyla/releases , https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation , https://github.com/grafana/faro-web-sdk , https://github.com/grafana/faro-web-sdk/releases
Grafana: https://github.com/grafana/grafana , https://github.com/grafana/grafana/releases , https://grafana.com/docs/grafana/latest/whatsnew/whats-new-in-v13-0/ , https://grafana.com/docs/grafana/latest/administration/organization-management/ , https://grafana.com/docs/grafana/latest/administration/roles-and-permissions/access-control/ , https://grafana.com/docs/grafana/latest/setup-grafana/configure-security/configure-authentication/ , https://grafana.com/docs/grafana/latest/setup-grafana/configure-access/configure-scim-provisioning/ , https://grafana.com/docs/grafana/latest/administration/provisioning/ , https://github.com/grafana/grafana-operator , https://grafana.com/docs/grafana/latest/alerting/fundamentals/alert-rules/ , https://grafana.com/docs/grafana/latest/alerting/set-up/configure-alertmanager/ , https://grafana.com/docs/grafana/latest/alerting/set-up/configure-alert-state-history/ , https://grafana.com/developers/plugin-tools/key-concepts/plugin-types-usage , https://grafana.com/developers/plugin-tools/key-concepts/backend-plugins/ , https://grafana.com/developers/plugin-tools/publish-a-plugin/sign-a-plugin , https://grafana.com/developers/scenes/
AI / IRM: https://grafana.com/whats-new/2026-04-21-grafana-assistant-becomes-available-on-prem/ , https://grafana.com/docs/grafana-cloud/machine-learning/assistant/on-premise/ , https://grafana.com/press/2026/07/27/grafana-labs-ships-six-tools-that-power-agentic-operations-from-planning-to-production/ , https://grafana.com/docs/grafana-cloud/machine-learning/sift/ , https://github.com/grafana/mcp-grafana , https://github.com/grafana/grafana-llm-app , https://github.com/grafana/oncall , https://grafana.com/blog/grafana-oncall-maintenance-mode/ , https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/ , https://grafana.com/docs/grafana-cloud/monitor-infrastructure/kubernetes-monitoring/configuration/
Licensing/trademark: https://grafana.com/licensing/ , https://grafana.com/blog/2021/04/20/qa-with-our-ceo-on-relicensing/ , https://www.gnu.org/licenses/gpl-faq.html , https://grafana.com/trademark-policy/ , https://grafana.com/trademark-policy/trademark-list/ , LICENSING.md on main of grafana/grafana, grafana/loki, grafana/mimir, grafana/tempo, grafana/pyroscope
Helm: https://github.com/grafana/helm-charts/tree/main/charts , https://github.com/grafana-community/helm-charts , Chart.yaml files on main for alloy, alloy-operator, k8s-monitoring, mimir-distributed, loki, tempo, tempo-distributed, pyroscope, grafana
