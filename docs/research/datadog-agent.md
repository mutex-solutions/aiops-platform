# Datadog Agent: what it collects, how it is built, and what the Grafana/OTel equivalent is

Research note for the aiops-platform agent design. Goal: understand the Datadog "install one agent, get everything" experience well enough to design a single Alloy-based agent with the same feel.

- Research date: 2026-10-27. All docs.datadoghq.com pages were read on that day; the docs are unversioned, so "as of 2026-10-27" applies unless a version is given.
- Latest Datadog Agent release at research time: **7.84.6** (released 2026-10-23). The 7.84.0 feature release is dated 2026-09-30. Helm chart `datadog/datadog` on `main` pins image tag 7.84.6.
- Anything marked **UNVERIFIED** was not confirmed against a primary source in this session.

---

## 1. Process architecture

### 1.1 Repository shape

`github.com/DataDog/datadog-agent` (Apache-2.0 for user-space; the eBPF C code under `pkg/ebpf/c` is GPL-2.0). Primary language Go (go.work multi-module; Go 1.26.7 as of 7.84.0), plus Rust (`rust/`, the `agent-data-plane` lives in a separate repo `DataDog/saluki`), C/C++ (`rtloader/` embeds CPython for Python checks), Python (`tasks/` invoke tooling). Builds via Bazel and Omnibus; packaging for deb/rpm/MSI/Chocolatey/AIX/Docker. Separate changelogs for the Agent, the Cluster Agent (`CHANGELOG-DCA.rst`), AIX and the install script.

`cmd/` on `main` contains these entry points (22 binaries):

| Binary | Role |
|---|---|
| `agent` | Core Agent: collector (checks), forwarder, DogStatsD server, logs agent, OTLP ingest, autodiscovery, tagger, workloadmeta |
| `trace-agent` | APM receiver (8126/TCP + UDS), trace sampling/stats, profiling intake proxy, EVP proxy |
| `process-agent` | Live Processes / Live Containers / Orchestrator pod collection. Since 7.65.0 these checks run inside the core Agent by default on Linux (`process_config.run_in_core_agent`); the process-agent binary is still required for Cloud Network Monitoring |
| `system-probe` | Privileged sidecar: eBPF/kernel-driver modules (NPM, USM, CWS event monitoring, OOM kill, TCP queue, traceroute, ping, service discovery, GPU, dynamic instrumentation, language detection) |
| `security-agent` | User-space half of security: CSPM compliance checks (`compliance_config`), CWS rule evaluation/events (`runtime_security_config`) |
| `dogstatsd` | Standalone DogStatsD server binary (for hosts that only need StatsD) |
| `cluster-agent` | Datadog Cluster Agent (DCA): API-server proxy, cluster checks dispatch, KSM core, orchestrator explorer, admission controller, external metrics, k8s events |
| `cluster-agent-cloudfoundry` | DCA variant for Cloud Foundry |
| `otel-agent` | DDOT: Datadog Distribution of OpenTelemetry Collector, embedded in the Agent package |
| `host-profiler` | Full-Host eBPF profiler built on the OpenTelemetry Collector framework and OTel eBPF profiler (Preview) |
| `installer` | Datadog Installer used by Fleet Automation remote upgrades (deleted from the Docker image at build time) |
| `cws-instrumentation` | ptrace-based tracer for Workload Protection on AWS Fargate (where eBPF is unavailable) |
| `serverless-init` | Wrapper/sidecar mini-agent for Cloud Run, Azure Container Apps etc. |
| `iot-agent` | Reduced agent for IoT devices |
| `privateactionrunner` | Private Action Runner (Workflow Automation on private networks) |
| `sbomgen` | SBOM generation helper |
| `secrethelper`, `secret-generic-connector` | Secret backend helpers |
| `config-stream-client` | Client for the "config streaming" mechanism (7.85 notes say env overrides on sub-containers stop working when config streaming is on) |
| `loader`, `systray`, `ai_prompt_logger` | Loader, Windows tray app, prompt logger |

Sources: [repo root](https://github.com/DataDog/datadog-agent), [cmd/ listing](https://github.com/DataDog/datadog-agent/tree/main/cmd), [releases](https://github.com/DataDog/datadog-agent/releases), [7.84.0 notes](https://github.com/DataDog/datadog-agent/releases/tag/7.84.0), [Live Processes](https://docs.datadoghq.com/infrastructure/process/).

### 1.2 Core Agent internals

Per the [architecture page](https://docs.datadoghq.com/agent/architecture/): the main process has a **Collector** that runs checks every 15 s (Agent 7 embeds a Python 3 interpreter via rtloader for Python integrations; Go "core" checks exist too) and a **Forwarder** that sends payloads over HTTPS, buffering in memory and dropping oldest on overflow. Logs go on a separate pipeline (HTTPS default since 6.19/7.19). **DogStatsD** is a Go implementation of StatsD receiving over UDP 8125 or a UNIX socket (`/var/run/datadog/dsd.socket`), with container "origin detection" when using UDS and host PID.

Default ports (datadog.yaml example + architecture page):

| Port | Use |
|---|---|
| 5000 | `expvar_port` (Agent runtime metrics; DCA uses 5000 for /metrics) |
| 5001 | `cmd_port` IPC API for CLI/GUI |
| 5002 | GUI (Windows/macOS only; `GUI_port` is -1 on Linux) |
| 5005 | Cluster Agent API port (node agents -> DCA, auth token) |
| 5009 | `agent_ipc.port` (added when DDOT is installed on Linux hosts) |
| 5555 / 5556 / 5557 | health ports: node agent / cluster agent / cluster-check runner |
| 8125/udp | DogStatsD |
| 8126/tcp | APM trace receiver (+ UDS `/var/run/datadog/apm.socket`) |
| 4317 / 4318 | OTLP gRPC/HTTP (core-agent OTLP ingest or DDOT) |
| 8000 | DCA admission webhook |
| 9162/udp, 2055/4739/6343/udp | SNMP traps, NetFlow/IPFIX/sFlow listeners (configurable) |
| 5100/5101/5102 | Agent Data Plane unprivileged/privileged/telemetry API |

### 1.3 Optional processes and how they are wired

- **trace-agent**: enabled by default (`apm_config.enabled: true`). In Kubernetes it is its own container (`trace-agent -config=/etc/datadog-agent/datadog.yaml`) with hostPort 8126 and the APM UDS mounted. OTLP env vars must be set on both the core `agent` and `trace-agent` containers because OTLP traces are handled by the trace-agent.
- **process-agent**: Live Containers on by default when a container runtime socket is mounted; Live Processes opt-in (`process_config.process_collection.enabled`). Talks to system-probe over `/var/run/sysprobe/sysprobe.sock` for I/O stats and NPM connections. Since 7.65 process/container checks run in the core agent on Linux; the Helm chart still renders a `process-agent` container (needed for CNM) with `DD_PROCESS_CONFIG_RUN_IN_CORE_AGENT_ENABLED=true`.
- **system-probe**: separate privileged container/service; modules are enabled individually in `system-probe.yaml`. Other agents reach it over a UDS plus the shared auth token (`DD_AUTH_TOKEN_FILE_PATH`). On Windows it runs as LocalSystem and, since a 2026 change, is supervised by `dd-procmgr` instead of the legacy `datadog-system-probe` SCM service.
- **security-agent**: `security-agent start -c datadog.yaml`; reads CWS events from system-probe over `/var/run/sysprobe/runtime-security.sock`; needs the host root mounted (`HOST_ROOT=/host/root`) plus `/etc/passwd` and `/etc/group` for compliance checks and user resolution; the rendered Helm manifest adds `AUDIT_CONTROL` and `AUDIT_READ` capabilities.
- **otel-agent (DDOT)**: separate container (`ddot-collector` image since Operator 1.22.0; requires Agent >= 7.67.0) or a process supervised by `dd-procmgrd` on Linux hosts (7.84.0 removed the `datadog-agent-ddot` systemd unit in favour of `processes.d`).
- **agent-data-plane**: Rust (saluki) process that takes over the DogStatsD pipeline when `data_plane.dogstatsd.enabled` is set; the core Agent's DogStatsD server goes dormant. Preview; ADP 1.6.x bundled with 7.84.
- **dd-procmgrd**: since 7.84.0 the Datadog process manager is the default Linux "service manager" when its binary ships with the Agent; each service manager (systemd/upstart/sysvinit/procmgrd) installs its own full unit set.

Sources: [architecture](https://docs.datadoghq.com/agent/architecture/), [rendered all-features manifest, chart 3.96.0 / agent 7.63.0](https://docs.datadoghq.com/resources/yaml/datadog-agent-all-features.yaml), [CSM Agent variables](https://docs.datadoghq.com/security/cloud_security_management/setup/agent_variables/), [OTLP ingest](https://docs.datadoghq.com/opentelemetry/setup/otlp_ingest_in_the_agent/), [DDOT on Kubernetes](https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/install/kubernetes/), [7.84.0 notes](https://github.com/DataDog/datadog-agent/releases/tag/7.84.0), [ADP / saluki](https://github.com/DataDog/saluki), [DogStatsD high throughput](https://docs.datadoghq.com/developers/dogstatsd/high_throughput/), [PR #56977 (procmgr on Windows)](https://github.com/DataDog/datadog-agent/pull/56977).

### 1.4 system-probe modules and eBPF loading strategy

Modules (from `system-probe.yaml` keys and docs): `network_config` (NPM/CNM, DNS stats, conntrack), `service_monitoring_config` (USM), `runtime_security_config` + `event_monitoring_config` (CWS), `system_probe_config.enable_oom_kill`, `system_probe_config.enable_tcp_queue_length`, `system_probe_config.process_config` (process I/O stats), `traceroute`, `ping`, `discovery` (service discovery), `gpu_monitoring`, `dynamic_instrumentation`, `system_probe_config.language_detection`, `windows_crash_detection` (Windows), `compliance_config.run_in_system_probe` (Helm option). 7.84.0 removed the `module-restart` CLI.

eBPF program loading has three strategies, chosen per program and controlled under `system_probe_config` (keys from `pkg/ebpf/config.go`):

| Strategy | Keys | Notes |
|---|---|---|
| CO-RE (preferred) | `enable_co_re`, `btf_path`, `btf_output_dir`, `remote_config_btf_enabled` | If the kernel lacks BTF, the Agent can download a BTF file for the running kernel from `https://install.datadoghq.com` (hard-coded host, 30 s timeout) |
| Runtime compilation | `enable_runtime_compiler`, `enable_kernel_header_download`, `kernel_header_dirs`, `runtime_compiler_output_dir`, `apt_config_dir`/`yum_repos_dir`/`zypper_repos_dir` | Ships flattened C + embedded clang; needs kernel headers (can download them using the host's package repo config; download dir is the constant `/var/tmp/datadog-agent/system-probe/kernel-headers`). Output dir must be root-owned and not world-writable or compilation is skipped |
| Prebuilt | `allow_prebuilt_fallback`, `allow_runtime_compiled_fallback` | Hard-coded struct offsets per kernel version family; last resort. `excluded_linux_versions`, `attach_kprobes_with_kprobe_events_abi`, `enable_tracepoints` tune attachment |

The Datadog engineering blog on CWS describes the order as "CO-RE first, then runtime offset guessing, then hard-coded offsets". The OOM-kill and TCP-queue checks are documented as runtime-compiled (they need `linux-headers-$(uname -r)` / `kernel-devel`).

Sources: [system-probe.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/system-probe_linux.yaml.example), [pkg/ebpf/config.go](https://github.com/DataDog/datadog-agent/blob/main/pkg/ebpf/config.go), [Hardening eBPF for runtime security (blog)](https://www.datadoghq.com/blog/engineering/ebpf-workload-protection-lessons/), [OOM Kill](https://docs.datadoghq.com/integrations/oom_kill/), [TCP Queue Length](https://docs.datadoghq.com/integrations/tcp_queue_length/).

### 1.5 Windows: kernel drivers instead of eBPF

- `ddnpm`: Windows Filtering Platform network driver used by NPM/CNM and USM (`pkg/network/driver`, source in `DataDog/windows-drivers`, version pinned by `WINDOWS_DDNPM_VERSION` in `release.json`). Agent 7.27.1+ for NPM; for Agent <= 7.44 the MSI needed `ADDLOCAL="MainApplication,NPM"`.
- `ddprocmon`: Datadog Process Monitor driver (status included in flares since 7.63) used for process/CWS telemetry on Windows; Workload Protection on Windows requires Server 2019+.
- USM on Windows uses the `ddnpm` driver for HTTP and ETW (`Microsoft-Windows-HttpService`) for HTTPS, so HTTPS is only visible for IIS.
- system-probe's named pipe `\\.\pipe\dd_system_probe` is restricted to LocalSystem, Administrators and `ddagentuser` (7.63).

Sources: [PR #58770](https://github.com/DataDog/datadog-agent/pull/58770), [7.63.0 notes](https://github.com/DataDog/datadog-agent/releases/tag/7.63.0), [CNM setup](https://docs.datadoghq.com/network_monitoring/performance/setup/), [USM setup](https://docs.datadoghq.com/universal_service_monitoring/setup/), [Workload Protection setup](https://docs.datadoghq.com/security/workload_protection/setup/).

### 1.6 Cluster Agent (DCA)

Enabled by default with Helm chart >= 2.7.0 and Operator >= 1.0.0. One Deployment (replica 1 by default, leader election via ConfigMap/Lease) that:

- proxies the kube-apiserver for node agents (reduces node-agent RBAC to kubelet-only reads) and ships cluster metadata tags to them (port 5005, shared auth token >= 32 chars);
- runs **cluster checks** (exactly one instance of a check across the cluster; node agents poll the DCA every 10 s; checks are reassigned when an agent stops reporting; optional dedicated **cluster check runner** Deployment, replicas 2) and **endpoint checks** (`kube_endpoints` provider/listener);
- runs **kubernetes_state_core** (built-in reimplementation of kube-state-metrics v2+, DCA >= 1.12; no standalone KSM needed) and **kubernetes_apiserver** (events; `collectKubernetesEvents` default true with a curated `collectedEventTypes` list);
- runs the **Orchestrator Explorer** collection of full resource manifests (Agent 7.27+/DCA 1.11+; DCA 7.40+ on K8s 1.25+; YAML history 7.44+; CRDs/custom resources, incl. KubeRay and NVIDIA Dynamo CRs by default since 7.84; container scrubbing on by default);
- runs the **admission controller** (`MutatingAdmissionWebhook`, DCA >= 7.40; port 8000; `failurePolicy: Ignore` in the rendered chart): injects `DD_AGENT_HOST`/`DD_TRACE_AGENT_URL`/`DD_ENTITY_ID`/`DD_EXTERNAL_ENV`, `env/service/version` from pod or owner labels, DogStatsD/APM socket volumes (modes `hostip`, `service`, `socket`, `csi` (7.67+)), tracer libraries for Single Step Instrumentation (DCA >= 7.39), Agent sidecar injection (Fargate), and CWS instrumentation (`cwsInstrumentation`, enriches CWS events with k8s user identities);
- optionally serves **external metrics** for HPA (`externalMetricsServer`), **control plane monitoring** checks (Operator default true), `kubernetesActions` (DCA 7.84+), `kueue` (7.82+), workload/cluster autoscaling (`DatadogPodAutoscaler`), and compliance checks for the cluster (`DD_COMPLIANCE_CONFIG_ENABLED` on the DCA).

Node-agent ClusterRole (`Dockerfiles/manifests/rbac/clusterrole.yaml`): get/list/watch on services, events, endpoints, pods, nodes, componentstatuses; get/update on the `datadogtoken` and `datadog-leader-election` ConfigMaps; create ConfigMaps; get on `/version`, `/healthz`, `/metrics`; get on `nodes/metrics`, `nodes/spec`, `nodes/proxy`, `nodes/stats` (kubelet connectivity). The DCA has its own ClusterRole plus a `datadog-ksm-core` ClusterRole (can list/watch Secrets cluster-wide) and a Role for Secrets (admission TLS).

Sources: [Cluster Agent](https://docs.datadoghq.com/containers/cluster_agent/), [Cluster checks](https://docs.datadoghq.com/containers/cluster_agent/clusterchecks/), [Admission controller](https://docs.datadoghq.com/containers/cluster_agent/admission_controller/), [kubernetes_state_core](https://docs.datadoghq.com/integrations/kubernetes_state_core/), [Kubernetes Explorer](https://docs.datadoghq.com/infrastructure/containers/orchestrator_explorer/), [Operator CRD v2alpha1](https://github.com/DataDog/datadog-operator/blob/main/docs/configuration.v2alpha1.md), [clusterrole.yaml](https://github.com/DataDog/datadog-agent/blob/main/Dockerfiles/manifests/rbac/clusterrole.yaml), [rendered manifest](https://docs.datadoghq.com/resources/yaml/datadog-agent-all-features.yaml).

### 1.7 DDOT (embedded OpenTelemetry Collector)

DDOT bundles a curated OTel Collector into the Agent package and keeps the Agent's own collection stack next to it. Bundled collector versions per Agent release (DDOT page): 7.82.x = contrib v0.155.0 / core v1.61.0; 7.69.x = v0.129.0 / v1.35.0 (so DDOT predates 7.69; several components are tagged "since 7.56.0").

Default component set (`comp/otelcol/collector-contrib/impl/manifest.yaml`): receivers dockerstats, filelog, fluentforward, hostmetrics, jaeger, k8sobjects, kubeletstats, otlp, podman, prometheus, receivercreator, zipkin, nop; processors attributes, batch, cumulativetodelta, filter, groupbyattrs, k8sattributes, memorylimiter, probabilisticsampler, resourcedetection, resource, tailsampling, transform; exporters datadog, debug, loadbalancing, otlp, otlphttp, sapm, nop; connectors datadog, routing, spanmetrics; extensions datadogextension (7.72+), healthcheck, observer, pprof, filestorage, zpages. Datadog-specific additions: `infraattributes` processor (applies Agent tagger tags to OTLP data by `container.id`), a config **Converter** (adds/validates components, exposes original + enhanced config), `ddflare` extension (flare from collector + agent; its endpoint is no longer unauthenticated since 7.84). 7.84.0: zstd for all signals; series via v3 intake.

Enable: Helm `datadog.otelCollector.enabled: true` (+ ports 4317/4318 with hostPort); Operator `features.otelCollector.enabled: true`; K8s >= 1.29 and Helm 3 required for the documented path; custom config via `--set-file datadog.otelCollector.config=otel-config.yaml` or `features.otelCollector.conf.configMap` (key must be `otel-config.yaml`). Linux hosts: install script with `DD_OTELCOLLECTOR_ENABLED=true`, or `sudo datadog-agent otel install` on an existing Agent >= 7.78; config at `/etc/datadog-agent/otel-config.yaml`; `datadog.yaml` gets `otelcollector.enabled: true`, `agent_ipc.port: 5009`, `config_refresh_interval: 60`. Fleet Automation shows DDOT config; Remote Configuration for DDOT is Preview. BYOC ("custom components") is supported via a separate build guide.

The lighter alternative is the core Agent's **OTLP ingest** (`otlp_config.receiver.protocols.grpc|http.endpoint`, traces/metrics since 7.32, logs since 7.48, `otlp_config.logs.enabled`), which has no pipeline features and must be local to the apps.

Sources: [DDOT overview](https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/), [DDOT Kubernetes](https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/install/kubernetes/), [DDOT Linux](https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/install/linux/), [OTLP ingest](https://docs.datadoghq.com/opentelemetry/setup/otlp_ingest_in_the_agent/).

---

## 2. Signal catalogue: what one Agent install collects

### 2.1 Infrastructure metrics (host, container, orchestrator)

- **Host system checks** (Go core checks, on by default): CPU, Disk, File Handle, IO, Load, Memory, Network, NTP, Uptime, plus Container and Docker checks when a runtime socket is mounted. Windows has no `system.load.*` (uses `system.proc.queue.length`). macOS gained a `thermal` check in 7.84.0. `infrastructure_mode` (`full`, `basic`, `end_user_device`, `cloud_cost_only`, `none`) selects which integrations/metadata run.
- **Containers**: `container` check from the runtime (Docker socket, containerd/CRI socket `cri_socket_path`, Podman DB `podman_db_path`), cgroup v1/v2 via `/host/sys/fs/cgroup`, kubelet (`kubelet` core check, `DD_KUBELET_CORE_CHECK_ENABLED`, kubelet host from `status.hostIP`, TLS verify default true, ports 10250/10255). Container lifecycle events and image metadata (`container_lifecycle`, `container_image`) are on by default in Helm.
- **Kubernetes**: `kubernetes_state_core` gauges for apiservice, configmap, container, crd, cronjob, daemonset, deployment, endpoint, hpa, ingress, job, limitrange, namespace, node, pdb, pv/pvc, pod, replicaset, resourcequota, secret, service, statefulset, vpa; k8s events; Orchestrator Explorer manifests; Helm check (`helmCheck`), Kueue, control-plane checks (etcd/apiserver/controller-manager/scheduler via DCA).
- **ECS**: `ecs_fargate` check reads the ECS task metadata endpoint (Docker stats JSON for all containers in the task); on EC2 the Agent task runs as a daemon service; `ecs_task_collection_enabled` default true; `ecs_deployment_mode` auto/daemon/sidecar.
- **GPU**: `gpu.enabled` (core) + `gpu_monitoring.enabled` (system-probe, Linux, privileged); NVLink tags added in 7.84; deprecated eBPF probes need kernel 5.8+.
- **Cloud tags/metadata**: EC2/GCE/Azure instance metadata, `collect_ec2_tags`, `collect_gce_tags`, host aliases; hostname resolution order: configured `hostname` (rejected if `ip-`/`domu`), OS `hostname -f` unless it is an EC2 default, EC2 instance-id, then DNS name; `hostname_fqdn`, `hostname_file`, `hostname_trust_uts_namespace` options.

Sources: [Docker Agent](https://docs.datadoghq.com/containers/docker/), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example), [Helm values](https://github.com/DataDog/helm-charts/blob/main/charts/datadog/values.yaml), [kubernetes_state_core](https://docs.datadoghq.com/integrations/kubernetes_state_core/), [ECS Fargate](https://docs.datadoghq.com/integrations/ecs_fargate/), [hostname FAQ](https://docs.datadoghq.com/agent/faq/how-datadog-agent-determines-the-hostname/).

### 2.2 Integrations and Autodiscovery

- Datadog states "1,000+ integrations" (DDOT page). Checks are Python (integrations-core, run by the embedded interpreter; `check_runners` default 4) or Go core checks. Config per integration in `conf.d/<integration>.d/conf.yaml` (`init_config` + `instances`), or `auto_conf.yaml` templates matched to containers by `ad_identifiers` (container short image name, e.g. `redis` matches `foo/redis:latest`).
- **Autodiscovery** sources (`config_providers` / `listeners`): `kubelet`, `docker`, `clusterchecks`, `endpointschecks`, `kube_services`, `kube_endpoints`, `etcd`, `consul`, `zookeeper` (key path `/datadog/check_configs/<image>/`), local files, ConfigMaps, Helm `datadog.confd`, Operator `extraConfd`, and the `DatadogInstrumentation` CRD (v1alpha1, targets a Deployment/Service/StrimziPodSet without editing pod specs). Pod annotations: v2 (`ad.datadoghq.com/<container>.checks` JSON keyed by integration, Agent 7.36+) and legacy v1 (`check_names`/`init_configs`/`instances`), plus `.logs`, `.check_tag_cardinality`, `logs_exclude`. Template variables `%%host%%`, `%%env_X%%` documented on the page; `%%port%%`, `%%kube_namespace%%` live in a separate guide (not fetched). Precedence: annotations > DatadogInstrumentation > static config. Docker label equivalents `com.datadoghq.ad.*` are referenced by the ECS/Docker docs but the exact format was not fetched (**UNVERIFIED** here; well known in practice).
- **Prometheus/OpenMetrics**: `prometheus_scrape.enabled` + `service_endpoints` autodiscovers `prometheus.io/*`-annotated pods/services and schedules the `openmetrics` check (v2).
- **Integration security** (7.78+): `integration_ignore_untrusted_file_params`, `integration_file_paths_allowlist`, `integration_trusted_providers` (default `file`, `remote-config`).
- **JMX**: JMXFetch (Java >= 1.7 required; `-jmx` image adds `openjdk-11-jre-headless`) connects to MBean servers and emits through DogStatsD; 350 metrics/instance cap; `conf.d/jmx.d` with `host/port`, `jmx_url`, attach-API via `process_name_regex`+`tools_jar_path`; bean include/exclude filters; JVM defaults (heap, GC, threads, FDs). JMX-based integrations include ActiveMQ, Cassandra, Kafka, Solr, Tomcat.
- **Database Monitoring**: Postgres, MySQL, MariaDB, Oracle, SQL Server, MongoDB, Amazon DocumentDB, ClickHouse (self-hosted and RDS/Aurora/Cloud SQL/AlloyDB/Azure/Atlas variants). Collects normalized query metrics, query samples, explain plans, recommendations, host metrics, `custom_queries`. Enabled on the per-engine integration (the `dbm: true` key and required DB grants come from per-engine pages that were not fetched; **UNVERIFIED** here). 7.84 adds TLS options for "Data security" PostgreSQL scans and Data Observability for SQL Server.
- **Windows-specific checks**: Windows Event Log (see 2.4), plus the `wmi_check`, `windows_performance_counters`, `windows_service`, `iis`, `active_directory`, `dotnetclr`, `sqlserver` integrations (names from integrations-core; not individually fetched). 7.84 adds logon-duration events with Group Policy details and a `wlan` check.

Sources: [Kubernetes integrations/Autodiscovery](https://docs.datadoghq.com/containers/kubernetes/integrations/), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example), [Java/JMX](https://docs.datadoghq.com/integrations/java/), [Dockerfile](https://github.com/DataDog/datadog-agent/blob/main/Dockerfiles/agent/Dockerfile), [DBM](https://docs.datadoghq.com/database_monitoring/), [DDOT overview](https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/).

### 2.3 Logs

- Off by default (`logs_enabled: false`, `DD_LOGS_ENABLED`). Transport HTTPS by default since 6.19/7.19 (`force_use_http`/`force_use_tcp`; `logs_dd_url` for proxies). 7.84.0 switched to fixed send concurrency `logs_config.pipelines x 10` (40 by default).
- Sources (`type` in a `logs:` block): `file` (wildcards, `exclude_paths`, `start_position`, `encoding`, fingerprinting to survive rotation), `tcp`/`udp` (raw, JSON, Syslog; `use_sourcehost_tag`), `journald` (`include_units`/`exclude_units`), `windows_event` (`channel_path`, XPath `query`), `docker`. Per-source `service`, `source`, `sourcecategory`, `tags`, and `log_processing_rules` (`exclude_at_match`, `include_at_match`, `mask_sequences`, `multi_line`). Global `logs_config.processing_rules`, `auto_multi_line_detection` (default true), `open_files_limit` 500 (200 on macOS).
- Containers: `container_collect_all` (default false; otherwise only containers with an AD log config). Kubernetes tails `/var/log/pods` (recommended; `k8s_container_use_file`), or the Docker socket (needed for non-root agents on Docker because `/var/lib/docker/containers` symlink targets are root-only), or an in-container file path shared via hostPath; a kubelet-API mode exists in Helm (`containerCollectUsingFiles`) but its version gate was not fetched. Offsets persist in the pointer dir (`/opt/datadog-agent/run` in docs; `/var/lib/datadog-agent/logs` hostPath in the rendered chart). Default `source`/`service` = short image name or `tags.datadoghq.com/service` label; `DD_CONTAINER_EXCLUDE_LOGS`, `ad.datadoghq.com/logs_exclude`.
- Serverless: Lambda extension collects logs via the Lambda Telemetry API; the Datadog Forwarder Lambda handles CloudWatch log groups and S3; Firehose destination for high volume; FireLens/Fluent Bit `datadog` output or `awslogs`+Forwarder on Fargate.
- Fleet Automation can push custom-log configs to many Agents ("Configure Custom Logs").

Sources: [Host Agent log collection](https://docs.datadoghq.com/agent/logs/), [Kubernetes log collection](https://docs.datadoghq.com/containers/kubernetes/log/), [DaemonSet guide](https://docs.datadoghq.com/containers/guide/kubernetes_daemonset/), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example), [Lambda](https://docs.datadoghq.com/serverless/aws_lambda/), [AWS integration](https://docs.datadoghq.com/integrations/amazon_web_services/), [ECS Fargate](https://docs.datadoghq.com/integrations/ecs_fargate/), [7.84.0 notes](https://github.com/DataDog/datadog-agent/releases/tag/7.84.0).

### 2.4 Windows event logs, WMI, performance counters

- `win32_event_log` (integration 5.7.1, Agent >= 6.0): (a) as **logs** with `type: windows_event`, `channel_path`, `source: windows.events`, XPath/XML `query` (recommended, cheaper than regex `log_processing_rules`); (b) as **Datadog events** via the Event Log API (`legacy_mode: false`, filters on path/type/source/id, or `query`); legacy WMI `Win32_NTLogEvent` mode deprecated since 7.20. Security channel needs the Agent user in "Event Log Readers". `dd_security_events: low|high` (7.54+) ships curated Security events for Cloud SIEM.
- Performance counters and WMI: separate `windows_performance_counters` and `wmi_check` integrations (not fetched in this session).

Source: [Windows Event Log](https://docs.datadoghq.com/integrations/win32_event_log/).

### 2.5 APM traces, profiling, OTLP

- **Traces**: trace-agent on 8126/TCP and UDS; `apm_config.apm_non_local_traffic` to accept remote tracers; obfuscation, `filter_tags`, `replace_tags`, `ignore_resources`, probabilistic sampler, remote adaptive sampling via Remote Config; `additional_endpoints`. Datadog SDKs for Java, Python, Go, Ruby, Node.js, .NET, PHP, C++, Rust. Single Step Instrumentation injects tracers via the DCA admission controller (k8s) or the installer (hosts); 7.84 handles AMD64 hosts running 32-bit binaries.
- **Continuous Profiler**: in-process profilers shipped inside the SDKs (`DD_PROFILING_ENABLED=true`), profiles sent through the trace-agent (Agent v6+; v7 recommended; some .NET features need 7.51+). Profile types per language (CPU, wall, allocations, live heap, exceptions, I/O, lock contention, timeline, heap snapshots). `ddprof` (native C/C++/Rust) is in maintenance mode; Datadog is moving to a host-based eBPF profiler.
- **Full-Host Profiling (Preview)**: eBPF, system-wide, "built on OpenTelemetry" (Datadog's maintained variant `DataDog/opentelemetry-ebpf-profiler`); docs state Linux 5.4+ amd64 / 5.5+ arm64, but PR #58270 (2026-10-16) raised the documented minimum to **5.10+** because the upstream profiler dropped older kernels; distributed as a standalone executable (`dd-otel-host-profiler`) and as the `host-profiler` container in the Helm chart with capabilities BPF, PERFMON, SYS_PTRACE, SYS_RESOURCE, DAC_READ_SEARCH, SYSLOG, CHECKPOINT_RESTORE, IPC_LOCK. Needs unstripped binaries or `datadog-ci elf-symbols upload`.
- **OTLP**: see 1.7.

Sources: [Enabling the Profiler](https://docs.datadoghq.com/profiler/enabling/), [Full Host](https://docs.datadoghq.com/profiler/enabling/full_host/), [PR #58270](https://github.com/DataDog/datadog-agent/pull/58270), [DataDog/opentelemetry-ebpf-profiler](https://github.com/DataDog/opentelemetry-ebpf-profiler), [Helm values](https://github.com/DataDog/helm-charts/blob/main/charts/datadog/values.yaml), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example).

### 2.6 RUM, Synthetics (not Agent-based)

- **RUM / Browser / Mobile**: browser SDK and mobile SDKs send directly to Datadog intake; no Agent involvement (not fetched; stated for completeness).
- **Synthetics private locations**: a separate worker (`datadog/synthetics-private-location-worker` image, Helm chart `datadog/synthetics-private-location`, Windows MSI 1.74.0) that polls Datadog every 2 s over outbound HTTPS/443 only, pulls test configs and pushes results; runs API, multistep, browser (bundled Chrome/Edge/Firefox), ICMP tests (needs `NET_RAW` under Podman; `NET_ADMIN` to block reserved IPs). Config file holds the location secrets; the Datadog Agent is not involved.

Source: [Private locations](https://docs.datadoghq.com/synthetics/private_locations/).

### 2.7 Network: CNM/NPM, DNS, Network Path, USM, NDM

**Cloud Network Monitoring (CNM, formerly NPM)**
- Linux eBPF (kernel 4.4+ or backports; RHEL/CentOS 7.6+), Windows via `ddnpm` driver (7.27.1+, Server 2012 R2+, hosts only), macOS unsupported. Agent 6.14+. Collects TCP/UDP flows with bytes/packets/retransmits/RTT, failed connections (resets/refusals/timeouts, default on since 7.59), DNS server metrics (`dns_monitoring_ports`, default 53; 7.76+ for custom ports), conntrack NAT resolution, service mesh (Istio 1.6.4+), Cilium (1.6+, kernel 5.1.16+), cloud entity resolution for AWS/Azure load balancers via the cloud integrations.
- Enable: `network_config.enabled: true` in `system-probe.yaml` (`DD_SYSTEM_PROBE_NETWORK_ENABLED`), plus the process-agent; Helm `datadog.networkMonitoring.enabled`; Operator `features.npm.enabled`; Fargate preview (7.58+) with `DD_NETWORK_CONFIG_ENABLE_EBPFLESS=true` and `SYS_PTRACE`.
- Privileges (Docker/K8s): `--cgroupns host --pid host --security-opt apparmor:unconfined`, capabilities SYS_ADMIN, SYS_RESOURCE, SYS_PTRACE, NET_ADMIN, NET_BROADCAST, NET_RAW, IPC_LOCK, CHOWN; mounts `/proc`, `/sys/fs/cgroup`, `/sys/kernel/debug`, (chart also mounts `/sys/fs/bpf`); SELinux policy for the system-probe binary on RHEL; Bottlerocket needs `super_t`/`spc_t` seLinuxOptions; GKE Autopilot has a dedicated flag.

**Network Path** (traceroute): scheduled tests 7.59+ (Linux), 7.72+ (Windows), 7.75+ (macOS); dynamic tests from CNM connections 7.73+; "basic" dynamic tests (5 top paths/hour) 7.84+; NetFlow-driven tests 7.81+ (experimental). TCP (`syn`, `sack`, `syn_socket` on Windows) and UDP; `traceroute.enabled` in system-probe (auto-enabled on 7.84+ when CNM dynamic tests are on), `network_path.*` in datadog.yaml, `conf.d/network_path.d/conf.yaml`.

**Universal Service Monitoring (USM)**
- Agent 6.40/7.40+, kernel 4.14+ (RHEL 8+), Windows Server 2012 R2+ (IIS only for HTTPS). Core (GA): HTTP and HTTPS via OpenSSL for containerized workloads; non-containerized Linux via process inference (7.42+). Preview: Go TLS (7.51+), Node.js TLS (7.54+), Istio mTLS (7.50+), HTTP/2 + gRPC (7.53+, kernel 5.2+), Kafka (7.53+, kernel 5.2+, plaintext only), "additional protocols and traffic encryption methods" and Cloud Service Discovery (S3 + ~20 third-party SaaS APIs by outbound request). Postgres/Redis/MySQL config keys exist in the config tree (**UNVERIFIED** names in this session). `http_replace_rules` for path normalization.
- Reads `DD_SERVICE/DD_ENV/DD_VERSION` from `/proc/PID/environ` (not runtime-set values). `env` tag is mandatory.
- Privileges: same capability set as CNM; extra mounts `/lib/modules`, `/usr/src`, the runtime-compiler build and kernel-header dirs, package-repo dirs (`/etc/apt`, `/etc/yum.repos.d`, ...); HTTPS needs `/` mounted at `/host/root` + `HOST_ROOT`. GKE Autopilot unsupported.

**Network Device Monitoring (NDM)**
- SNMP polling (v2c/v3 confirmed; v1 for the `snmp` check not confirmed on fetched pages): per-device `snmp.d/conf.yaml` or `network_devices.autodiscovery.configs` (subnets, auth, `loader: core`, `discovery_interval` 3600 s, `collect_topology` true, `collect_vpn`, ping), vendor profiles (metrics, metadata, interfaces, LLDP/CDP topology), device tags, HA active/standby pair (failover within 90 s), `device_tags_source` (7.84). Runs in the core Agent (no system-probe) and can run as cluster checks.
- **SNMP traps**: Agent 7.37+ (multiple v3 users 7.51+), `network_devices.snmp_traps.enabled`, port 9162 (use `setcap CAP_NET_BIND_SERVICE` for 162), `community_strings`, v3 `users` (MD5/SHA*/DES/AES*), OID-to-name resolution via a bundled TrapsDB (11,000+ MIBs), emitted as logs `source:snmp-traps`.
- **NetFlow**: Agent 7.45+, `network_devices.netflow.listeners` with `flow_type` netflow5/netflow9/ipfix/sflow5 and ports (2055/2056/4739/6343 in examples), `workers`, 5-minute aggregation, SNMP enrichment by exporter IP + ifIndex, cloud/IANA/custom enrichment, optional reverse DNS (`reverse_dns_enrichment`), `aggregator_max_flows_per_flush_interval` (7.75.1+).
- **Network Configuration Management (NCM)**: present in 7.84 notes (local config store, check failure metric); details not fetched.

Sources: [CNM setup](https://docs.datadoghq.com/network_monitoring/performance/setup/), [Network Path setup](https://docs.datadoghq.com/network_monitoring/network_path/setup/), [USM setup](https://docs.datadoghq.com/universal_service_monitoring/setup/), [USM additional protocols/cloud service discovery](https://docs.datadoghq.com/universal_service_monitoring/additional_protocols/), [NDM setup](https://docs.datadoghq.com/network_monitoring/devices/setup/), [SNMP traps](https://docs.datadoghq.com/network_monitoring/devices/snmp_traps/), [NetFlow](https://docs.datadoghq.com/network_monitoring/devices/netflow/), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example).

### 2.8 Processes, containers, service discovery

- **Live Processes**: all processes with command line, PID, user (`/etc/passwd` mount in containers), CPU/RSS, container/pod tags; 10 s resolution, 2 s streaming while the page is open (30 min max). I/O and open-file stats come from system-probe (`system_probe_config.process_config.enabled`). Argument scrubbing on by default (`scrub_args`, `custom_sensitive_words`, `strip_proc_arguments`). Windows supported. Fargate: `pidMode: task`, Agent 7.50+ for container association.
- **Live Containers**: on by default when a runtime socket is mounted (`container_collection.enabled: true`).
- **Process discovery**: on by default, 4 h interval, catalogues running integrations/services ("Detected" badges).
- **Service Discovery** (`discovery.enabled`, default on since 7.78.0 on Linux; Operator `serviceDiscovery` default true for images >= 7.78): detects processes listening on ports, language, APM instrumentation status; split between the core Agent's workloadmeta process collector and a system-probe module (cluster-check runners log errors when the socket is missing; set `DD_DISCOVERY_ENABLED=false` there).
- Language detection (`DD_LANGUAGE_DETECTION_ENABLED`, binary analysis in system-probe) feeds SSI.

Sources: [Live Processes](https://docs.datadoghq.com/infrastructure/process/), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example), [Operator CRD](https://github.com/DataDog/datadog-operator/blob/main/docs/configuration.v2alpha1.md), [helm-charts issue #2943](https://github.com/DataDog/helm-charts/issues/2943).

### 2.9 Security: CWS, CSPM, vulnerabilities/SBOM, SCA

**Workload Protection (CWS)**
- Linux eBPF (kernel 4.14+; RHEL 7 kernel 3.10 runs degraded without network events), Windows Server 2019+ via driver, AWS Fargate via `cws-instrumentation` (ptrace). Docker install needs Agent 7.46+. Enabled with `runtime_security_config.enabled` on both system-probe and security-agent (`DD_RUNTIME_SECURITY_CONFIG_ENABLED`), Helm `datadog.securityAgent.runtime.enabled`, Operator `features.cws.enabled`. Rules are delivered via Remote Configuration (`DD_RUNTIME_SECURITY_CONFIG_REMOTE_CONFIGURATION_ENABLED`); local policies in `/etc/datadog-agent/runtime-security.d`; Security Agent policies v0.84.0 ship with 7.84.
- Event types supported by the SECL rule language on Linux (42, with the Agent version that introduced each): exec 7.27, exit 7.38, open/chmod/chown/link/unlink/rename/rmdir/mkdir/utimes/setxattr/removexattr 7.27, setuid/setgid/capset 7.27, selinux 7.30, bpf 7.33, load_module/unload_module/mmap/mprotect/ptrace/signal 7.35, dns/splice 7.36, bind 7.37, mount 7.42 (exp.), chdir 7.52 (exp.), imds 7.55, connect/packet 7.60, accept/network_flow_monitor 7.63, sysctl 7.65, cgroup_write/setrlimit/setsockopt 7.68, capabilities 7.70 (exp.), prctl 7.71, socket 7.81, unshare 7.84. Windows has its own file/registry event set (not fetched).
- Extras: File Integrity Monitoring (`fim_enabled`, default false), activity dumps -> security profiles with anomaly detection and auto-suppression (default on), `enforcement` (kill/response actions, Helm default true), `network.enabled` (DNS/flow events, default true), `syscallMonitor` (off), argument/env scrubbing (`event_monitoring_config.custom_sensitive_words`, `envs_with_value`).
- Privileges: same capability set as CNM plus mounts `/etc/passwd`, `/etc/group`, `/` -> `/host/root` (ro), `/sys/kernel/debug` (rw), `/etc/os-release`; AppArmor unconfined; the Helm chart writes a custom seccomp profile for system-probe into the node's `/var/lib/kubelet/seccomp` via an init container.

**Misconfigurations (CSPM) / host benchmarks**
- Agent side runs in the security-agent (`compliance_config.enabled`, `DD_COMPLIANCE_CONFIG_ENABLED`; `check_interval` 20 m; rules dir `/etc/datadog-agent/compliance.d`; `host_benchmarks`; XCCDF enabled in the rendered chart; optional `runInSystemProbe`); the DCA runs cluster-level checks; Operator describes the scope as "Docker and Kubernetes benchmarks". Cloud resources are evaluated agentless via the AWS/Azure/GCP integrations (1,000+ rules, 15 min to 4 h cadence, PCI/SOC2 and other frameworks, 15-month history).

**Vulnerabilities / SBOM**
- Agent: `sbom.enabled`, `sbom.container_image.enabled` (`uncompressedLayersSupport` adds `SYS_ADMIN`; `overlayFSDirectScan`; `DD_SBOM_CONTAINER_IMAGE_USE_MOUNT`), `sbom.host.enabled`; container image collection requires Agent 7.46+ and is on by default in Helm >= 3.46 / Operator >= 1.3. The Agent finds OS packages only; agentless scanning (AWS/Azure/GCP snapshots, every 12 h) finds OS + application packages and covers Lambda/Fargate/Container Apps/Cloud Run and registries. Strong evidence the Agent embeds Trivy (Trivy log lines leak into Agent logs, `sbomgen` binary), exact source variant **UNVERIFIED**.
- Software Composition Analysis (SCA) and IAST/Threats are tracer-side features (`asm.sca`, `asm.iast`, `asm.threats` in Helm), not Agent collection.

Sources: [Workload Protection setup](https://docs.datadoghq.com/security/workload_protection/setup/), [Docker setup](https://docs.datadoghq.com/security/workload_protection/setup/agent/docker/), [Kubernetes setup](https://docs.datadoghq.com/security/workload_protection/setup/agent/kubernetes/), [linux_expressions.md](https://github.com/DataDog/datadog-agent/blob/main/docs/cloud-workload-security/linux_expressions.md), [CSM Agent variables](https://docs.datadoghq.com/security/cloud_security_management/setup/agent_variables/), [Cloud Security setup](https://docs.datadoghq.com/security/cloud_security_management/setup/), [Misconfigurations](https://docs.datadoghq.com/security/cloud_security_management/misconfigurations/), [Vulnerabilities](https://docs.datadoghq.com/security/cloud_security_management/vulnerabilities/), [Helm values](https://github.com/DataDog/helm-charts/blob/main/charts/datadog/values.yaml), [issue #28356](https://github.com/DataDog/datadog-agent/issues/28356), [rendered manifest](https://docs.datadoghq.com/resources/yaml/datadog-agent-all-features.yaml).

### 2.10 Serverless

- **AWS Lambda**: Datadog Lambda Extension (layer or `/opt/extensions` in container images) = a small agent inside the execution environment: custom metrics (DogStatsD/Lambda library), traces, enhanced `aws.lambda.enhanced.*` metrics, logs via the Lambda Telemetry API (no CloudWatch needed), request/response payload capture. The "next-generation" low-overhead extension is default from v67 and the only one from v88; v90+ supports Lambda Managed Instances (background flush every 30 s). `DD_SERVERLESS_FLUSH_STRATEGY` tunes flushing. Runtimes: Python, Node.js, Java, Go, .NET, Ruby. Onboarding via Fleet Automation quick start, `@datadog/ai-setup-cli`, or an MCP `serverless_onboarding` tool. Layer ARN format and `DD_*` env var list were not on the fetched pages (**UNVERIFIED**).
- **Cloud Run / Azure Container Apps / App Service**: `serverless-init` either wraps the app container (ENTRYPOINT; direct stdout access) or runs as a sidecar with a shared log volume; collects traces, custom metrics, logs. Cloud Run docs confirm both modes; the Azure pages were not fetched (**UNVERIFIED** that Azure uses the same binary, though `cmd/serverless-init` is generic).

Sources: [Lambda overview](https://docs.datadoghq.com/serverless/aws_lambda/), [Lambda extension](https://docs.datadoghq.com/serverless/libraries_integrations/extension/), [Lambda installation](https://docs.datadoghq.com/serverless/aws_lambda/installation/), [Cloud Run](https://docs.datadoghq.com/serverless/google_cloud_run/), [Cloud Run containers](https://docs.datadoghq.com/serverless/google_cloud_run/containers/).

### 2.11 Agentless cloud integrations

| Cloud | Auth | Metrics | Resources/posture | Logs |
|---|---|---|---|---|
| AWS | IAM role delegation (or access keys for China/GovCloud); CloudFormation/Terraform/Control Tower/Organizations automation | CloudWatch crawler ("metric-by-metric", new metrics every ~10 min on average, 90+ services) or **Metric Streams + Data Firehose** (2-3 min latency) | read-only API calls with the `SecurityAudit` managed policy; CloudTrail refreshes state; drives CSM, Cloud Cost, Cloudcraft | Firehose destination (high volume) or the Forwarder Lambda (S3, CloudWatch log groups) |
| Azure | App Registration (all sites); Azure Native option referenced elsewhere | Azure Monitor polling (interval not stated on fetched page) | resource collection for CSM/Cloud Cost | ARM-template automated log forwarding or manual Event Hub |
| GCP | Service account + Datadog principal impersonation (no key file); roles Monitoring Viewer, Compute Viewer, Cloud Asset Viewer, Browser | Cloud Monitoring API; metrics appear ~15 min after setup; `.delta` companions for cumulative metrics | Cloud Asset Inventory + Pub/Sub asset feed | separate log forwarding guide (Pub/Sub + Dataflow; not fetched); Private Service Connect on US5/EU only |

Sources: [AWS](https://docs.datadoghq.com/integrations/amazon_web_services/), [Azure](https://docs.datadoghq.com/integrations/azure/), [GCP](https://docs.datadoghq.com/integrations/google_cloud_platform/).

### 2.12 Fleet Automation and Remote Configuration

- **Remote Configuration**: components (Agents, SDKs via the Agent, OP Workers, Private Action Runners) poll the Datadog site outbound over HTTPS/443; changes are signed and validated by the receiving component; scoped per component; `remote_configuration.enabled` default true (off on GovCloud); must also be enabled on the org and on the API key. Products: Fleet (flares, upgrades), APM sampling/log injection/header tags, Dynamic Instrumentation, CWS rules, Observability Pipelines, Autoscaling, Private Action Runner, Feature Flags, AAP/WAF, NDM profiles, Serverless auto-instrumentation, Sensitive Data Scanner, Data Observability. system-probe can also fetch BTF via RC. Polling interval and TUF details are not on the fetched page (**UNVERIFIED**).
- **Fleet Automation**: inventory of Agents and OTel Collectors with config history; configure Agents centrally; deploy/update/remove integration config files; custom-log configs; tags (Preview); upgrade Agents/Collectors (requires RC + `remote_updates: true`, Linux/Windows VMs; uses the `installer` binary), upgrade SDKs (Preview); remote flares; public API. Permissions: API Keys Read, Agent Flare Collection, Agent Upgrade, Agent Configuration Management. `inventories_configuration_enabled` (default true) sends the scrubbed config to Datadog.

Sources: [Remote Configuration](https://docs.datadoghq.com/remote_configuration/), [Fleet Automation](https://docs.datadoghq.com/agent/fleet_automation/), [Remote management](https://docs.datadoghq.com/agent/fleet_automation/remote_management/), [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example).

### 2.13 eBPF checks: OOM kill, TCP queue length

- `oom_kill`: one metric `oom_kill.oom_process.count` + an event per kill (victim and trigger process). Kernel 4.9+, runtime-compiled (needs headers), not on Windows or RHEL < 8. `system_probe_config.enable_oom_kill: true` + `conf.d/oom_kill.d/conf.yaml`; Docker `--privileged` or `--cap-add CAP_BPF` on 5.8+, mounts `/sys/kernel/debug`, `/lib/modules`, `/usr/src`; Helm `datadog.systemProbe.enableOOMKill`; Operator `features.oomKill.enabled` (on COS override the `src` volume with emptyDir).
- `tcp_queue_length` (Agent 7.24.1+): `tcp_queue.read_buffer_max_usage_pct` / `write_buffer_max_usage_pct` per container; same mechanism and privileges (`enable_tcp_queue_length`).

Sources: [OOM Kill](https://docs.datadoghq.com/integrations/oom_kill/), [TCP Queue Length](https://docs.datadoghq.com/integrations/tcp_queue_length/).

---

## 3. Install paths

| Target | How | Notes (as of 2026-10-27) |
|---|---|---|
| Linux hosts | `install_script_agent7.sh` from install.datadoghq.com with `DD_API_KEY`, `DD_SITE`; deb/rpm packages; config `/etc/datadog-agent/datadog.yaml`, `conf.d/`, `system-probe.yaml`, `security-agent.yaml`; service manager systemd/upstart/sysvinit or `dd-procmgrd` (default when bundled, 7.84+); `DD_OTELCOLLECTOR_ENABLED=true` adds DDOT; `DD_REMOTE_UPDATES=true` enables Fleet upgrades (**UNVERIFIED** flag name) | Supported: Amazon Linux 2/2023 (>= 7.40), Debian >= 8, Ubuntu >= 14.04, RHEL/CentOS/Oracle >= 7, AlmaLinux/Rocky >= 8 (>= 7.33), SLES >= 12, openSUSE >= 15, Fedora >= 26; x86_64 and arm64 (arm64 >= 7.16). The system-probe binary ships an SELinux policy for RPM systems |
| Windows | MSI `datadog-agent-7-latest.amd64.msi`, properties `APIKEY`, `SITE`, `TAGS`, `HOSTNAME`, `DDAGENTUSER_NAME/PASSWORD`, `LOGS_ENABLED`, `APM_ENABLED`, `PROCESS_ENABLED`, `CMD_PORT`, `PROXY_*`, `DD_LOG_LEVEL` (7.84); `/qn REBOOT=ReallySuppress`; runs as `ddagentuser`; config `C:\ProgramData\Datadog`; services `DatadogAgent`, `datadog-trace-agent`, `datadog-process-agent`, system-probe (via dd-procmgr), security-agent; kernel drivers `ddnpm`, `ddprocmon`; Agent Manager GUI on 5002; Chocolatey package exists (not fetched) | Server 2016/2019/2022/2025, Windows 10/11 (2012 R2 and 8.1 up to 7.46.0). Windows nodes in Kubernetes: Helm `--set targetSystem=windows` |
| macOS | 12+ (x86 >= 7.39; native arm64 >= 7.70); DMG/install script (not fetched); GUI port 5002; `open_files_limit` 200 | 7.84 adds `thermal` check and shutdown-fault events |
| Docker (single container) | `docker run -d --cgroupns host --pid host -v /var/run/docker.sock:/var/run/docker.sock:ro -v /proc/:/host/proc/:ro -v /sys/fs/cgroup/:/host/sys/fs/cgroup:ro -e DD_SITE -e DD_API_KEY gcr.io/datadoghq/agent:7`; add `DD_LOGS_ENABLED`, `DD_APM_NON_LOCAL_TRAFFIC`, `DD_DOGSTATSD_NON_LOCAL_TRAFFIC`, `DD_PROCESS_CONFIG_PROCESS_COLLECTION_ENABLED` + `/etc/passwd:ro`; CNM/USM/CWS add the 8 capabilities, `apparmor:unconfined`, `/sys/kernel/debug`, `/lib/modules`, `/usr/src`, `/etc/os-release`, `/etc/group`, `/:/host/root:ro`; OOM/TCP-queue checks need `--privileged` or `CAP_BPF` | Image: Ubuntu 24.04 base, s6-overlay v2.2.0.3, root by default (`dd-agent` uid 100 for unprivileged), `EXPOSE 8125/udp 8126/tcp`, `CMD /bin/entrypoint.sh`; flavors `-jmx` (adds JRE 11), `-full` (includes otel-agent/DDOT per Operator docs), FIPS variant (`useFIPSAgent`); Windows `-servercore`/`-nanoserver` (**UNVERIFIED** tags); registries registry.datadoghq.com, gcr.io/datadoghq (+eu/asia), public.ecr.aws/datadog, datadoghq.azurecr.io, docker.io/datadog; Podman supported; Docker Swarm cannot set `security_opt` so USM needs AppArmor disabled on the host |
| Kubernetes (Helm `datadog/datadog`) | DaemonSet (hostPID true, hostNetwork false) with init containers `init-volume`, `init-config`, `seccomp-setup` and containers `agent`, `trace-agent`, `process-agent`, `system-probe`, `security-agent` (+ `otel-agent`, `agent-data-plane`, `host-profiler`, `private-action-runner` when enabled); Deployment `cluster-agent`; optional `cluster-checks-runner` Deployment (replicas 2). hostPath volumes: `/proc`, `/sys/fs/cgroup`, `/etc/os-release` (+redhat/fedora/lsb/system-release), `/var/run` (runtime sockets), `/var/run/datadog` (DSD+APM sockets), `/var/lib/kubelet/seccomp`, `/sys/kernel/debug`, `/sys/fs/bpf`, `/etc/passwd`, `/etc/group`, `/` (hostroot, ro), `/var/lib/datadog-agent/logs` (pointer dir), `/var/log/pods`, `/var/log/containers`, `/var/lib/docker/containers`; USM also mounts `/lib/modules`, `/usr/src`, runtime-compiler dirs and package-repo dirs (`providers.flatcar` skips `/usr/src`). system-probe: `privileged: false`, caps SYS_ADMIN, SYS_RESOURCE, SYS_PTRACE, NET_ADMIN, NET_BROADCAST, NET_RAW, IPC_LOCK, CHOWN, DAC_READ_SEARCH, seccomp `localhost/system-probe`, AppArmor `unconfined` (annotation for K8s < 1.30, `appArmorProfile` for >= 1.30). security-agent: AUDIT_CONTROL, AUDIT_READ. Unprivileged mode: `runAsUser: 100` + `supplementalGroups` of the runtime socket | Version matrix: K8s 1.16+ needs Agent 7.19+/DCA 1.9+, 1.21+ 7.36+, 1.22+ 7.37+, 1.25+ 7.40+, 1.33+ 7.67+; Operator chart depends on `datadog-operator`; `registryMigrationMode`; GKE Autopilot/COS/Bottlerocket/OpenShift SCC flags exist; Datadog "discourages" hand-written DaemonSets |
| Kubernetes (Operator) | `DatadogAgent` v2alpha1 with `spec.features.*` toggles (defaults: admissionController, apm, clusterChecks, controlPlaneMonitoring, kubeStateMetricsCore, liveContainerCollection, orchestratorExplorer, processDiscovery, remoteConfiguration, serviceDiscovery (>= 7.78) on; cspm, cws, dataPlane, dynamicInstrumentation, ebpfCheck, externalMetricsServer, gpu, helmCheck, hostProfiler, kubernetesActions, kueue, liveProcessCollection, logCollection, npm, oomKill, otelAgentGateway, otelCollector, prometheusScrape, sbom, tcpQueueLength, usm off); `spec.global.containerStrategy` default `optimized`; `kubelet.tlsVerify` default true with `hostCAPath`/`agentCAPath`; `override.nodeAgent.containers` names: agent, trace-agent, process-agent, system-probe, security-agent, init-config, init-volume, seccomp-setup | Operator >= 1.22.0 uses the `ddot-collector` image for DDOT (Agent >= 7.67) |
| ECS on EC2 | Agent task definition deployed as a DAEMON service (`datadog-agent-ecs.json`); Docker-label Autodiscovery; DogStatsD/APM via host port mapping and `DD_AGENT_HOST` from IMDS; CNM via `datadog-agent-sysprobe-ecs.json` (mount `/sys/kernel/debug`, the 8 capabilities, Linux only); Network Path via `DD_TRACEROUTE_ENABLED` | awsvpc for the Agent discouraged |
| ECS Fargate | Agent as a **sidecar in every task** (`ECS_FARGATE=true`, Agent 6.1.1+, platform 1.4.0+ for net/storage metrics); `ecs_fargate` check on the task metadata endpoint; DogStatsD 8125 and APM 8126 on localhost; logs via FireLens/Fluent Bit `datadog` output or `awslogs`+Forwarder; processes need `pidMode: task` (7.50+); CNM preview via eBPF-less mode + `SYS_PTRACE` (7.58+); CWS via `cws-instrumentation` (ptrace) | No host metrics, no eBPF |
| Serverless | Lambda extension layer/container image; `serverless-init` wrapper or sidecar for Cloud Run/Azure Container Apps/App Service | see 2.10 |

Sources: [Supported platforms](https://docs.datadoghq.com/agent/supported_platforms/), [Windows Agent](https://docs.datadoghq.com/agent/basic_agent_usage/windows/), [Docker Agent](https://docs.datadoghq.com/containers/docker/), [Dockerfile](https://github.com/DataDog/datadog-agent/blob/main/Dockerfiles/agent/Dockerfile), [Kubernetes installation](https://docs.datadoghq.com/containers/kubernetes/installation/), [Helm values](https://github.com/DataDog/helm-charts/blob/main/charts/datadog/values.yaml), [rendered all-features manifest](https://docs.datadoghq.com/resources/yaml/datadog-agent-all-features.yaml), [Operator CRD](https://github.com/DataDog/datadog-operator/blob/main/docs/configuration.v2alpha1.md), [ECS EC2](https://docs.datadoghq.com/containers/amazon_ecs/), [ECS Fargate](https://docs.datadoghq.com/integrations/ecs_fargate/), [DDOT Linux](https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/install/linux/), [CNM setup](https://docs.datadoghq.com/network_monitoring/performance/setup/), [USM setup](https://docs.datadoghq.com/universal_service_monitoring/setup/), [Workload Protection Docker](https://docs.datadoghq.com/security/workload_protection/setup/agent/docker/).

---

## 4. Configuration surface

### 4.1 Files

| File | Scope |
|---|---|
| `datadog.yaml` (`/etc/datadog-agent/`, `C:\ProgramData\Datadog\`) | core Agent + trace/process/security-agent read it (`-config=`/`--cfgpath=`/`-c=`); example file is roughly 4,500-5,500 lines |
| `system-probe.yaml` | system-probe modules (NPM, USM, CWS, OOM, TCP queue, traceroute, ping, GPU, dynamic instrumentation) |
| `security-agent.yaml` | compliance + runtime-security user-space settings (keys mirror `datadog.yaml` sections) |
| `conf.d/<integration>.d/conf.yaml` (+ `auto_conf.yaml`, `metrics.yaml`) | one directory per integration; `init_config`, `instances`, `logs`, `ad_identifiers`, `cluster_check` |
| `otel-config.yaml` | DDOT collector pipeline |
| `compliance.d/`, `runtime-security.d/` | CSPM rules, CWS policies |
| `install_info`, `auth_token`, `processes.d/` | install telemetry, IPC auth token, procmgrd definitions |

### 4.2 Main `datadog.yaml` keys (selection)

Connectivity and identity: `api_key`, `app_key`, `site` (US1/EU/US3/US5/AP1/GovCloud), `dd_url` (metrics only), `infrastructure_mode`, `proxy.{http,https,no_proxy}`, `skip_ssl_validation`, `min_tls_version`, `fips.enabled`, `hostname`, `hostname_file`, `hostname_fqdn`, `host_aliases`, `tags`/`extra_tags`, `env`, `checks_tag_cardinality`/`dogstatsd_tag_cardinality` (low/orchestrator/high), `cloud_provider_metadata`, `collect_ec2_tags`, `collect_gce_tags`, `azure_hostname_style`.

Secrets: `secret_backend_command` (takes precedence) or `secret_backend_type` (aws.secrets, aws.ssm, azure.keyvault, hashicorp.vault, file.json/yaml), `secret_backend_config`, `multi_secret_backends`.

Pipelines: `logs_enabled` + `logs_config.*`; `apm_config.*` (`enabled`, `receiver_port`, `receiver_socket`, `apm_non_local_traffic`, obfuscation, sampling, `additional_endpoints`, `install_id`); `process_config.*` (`process_collection.enabled`, `container_collection.enabled`, `process_discovery.{enabled,interval}`, `run_in_core_agent`, `scrub_args`, `custom_sensitive_words`, `strip_proc_arguments`, queue sizes); `otlp_config.*`; `otelcollector.enabled`; `use_dogstatsd`, `dogstatsd_port`, `dogstatsd_socket`, `dogstatsd_non_local_traffic`, `dogstatsd_origin_detection`, `dogstatsd_mapper_profiles`, `statsd_forward_host`; `data_plane.*`; `forwarder_*` (timeout 20 s, retry queue, `num_workers` 1, disk storage), `metric_lookback.*`.

Containers and Kubernetes: `container_include`/`container_exclude` (+`_metrics`/`_logs`, regex on `name:`/`image:`/`kube_namespace:`), `exclude_pause_container`, `docker_labels_as_tags`, `docker_env_as_tags`, `container_labels_as_tags`, `container_env_as_tags`, `kubernetes_pod_labels_as_tags`, `kubernetes_pod_annotations_as_tags`, `kubernetes_namespace_labels_as_tags`, `kubernetes_node_labels_as_tags`, `kubernetes_kubelet_host`, `kubelet_tls_verify`, `kubernetes_http(s)_kubelet_port`, `kubelet_client_*`, `cri_socket_path`, `containerd_namespaces`, `kubernetes_kubeconfig_path`, `kubernetes_apiserver_tls_verify`, `kubernetes_use_endpoint_slices`, `collect_kubernetes_events`, `leader_election`, `cluster_name` (<= 40 chars per yaml; <= 80 per k8s doc), `cluster_agent.{enabled,url,cmd_port,auth_token,kubernetes_service_name}`, `orchestrator_explorer.*`, `prometheus_scrape.*`, `ecs_*`, `podman_db_path`, `container_cgroup_root`, `container_proc_root`.

Autodiscovery: `config_providers`, `extra_config_providers`, `listeners`, `extra_listeners`, `autoconf_template_dir`, `autoconf_config_files_poll`, `ac_include`/`ac_exclude` (deprecated), `ad_allowed_env_vars`, `ignore_autoconf`, `autoconfig_exclude_features`.

Security: `compliance_config.*`, `runtime_security_config.*`, `sbom.*`, `container_image.enabled`.

Network devices: `network_devices.{namespace,autodiscovery,snmp_traps,netflow}`, `reverse_dns_enrichment.*`, `network_path.*`, `traceroute` (system-probe).

Agent internals: `remote_configuration.enabled`, `remote_updates`, `expvar_port`, `cmd_port`, `GUI_port`, `health_port`, `check_runners`, `log_level`/`log_file`/`log_to_syslog`, `inventories_configuration_enabled`, `python_version`-related (`disable_py3_validation`), `jmx_*` (`jmx_use_container_support`, `jmx_max_ram_percentage`, `jmx_check_period`), `agent_ipc.*`, `telemetry.*`, `integration_*` security keys.

### 4.3 Environment variables

Every key maps to `DD_<UPPERCASED_PATH_WITH_UNDERSCORES>` (e.g. `logs_config.container_collect_all` -> `DD_LOGS_CONFIG_CONTAINER_COLLECT_ALL`; `system_probe_config` keys use `DD_SYSTEM_PROBE_*`; CWS accepts both `DD_EVENT_MONITORING_CONFIG_*` and legacy `DD_RUNTIME_SECURITY_CONFIG_*`). Env overrides file settings. Frequently used in containers: `DD_API_KEY`, `DD_SITE`, `DD_HOSTNAME`, `DD_ENV`, `DD_TAGS`, `DD_LOGS_ENABLED`, `DD_LOGS_CONFIG_CONTAINER_COLLECT_ALL`, `DD_LOGS_CONFIG_K8S_CONTAINER_USE_FILE`, `DD_APM_ENABLED`, `DD_APM_NON_LOCAL_TRAFFIC`, `DD_APM_RECEIVER_SOCKET`, `DD_DOGSTATSD_NON_LOCAL_TRAFFIC`, `DD_DOGSTATSD_SOCKET`, `DD_DOGSTATSD_ORIGIN_DETECTION`, `DD_PROCESS_CONFIG_PROCESS_COLLECTION_ENABLED`, `DD_PROCESS_AGENT_ENABLED` (legacy), `DD_SYSTEM_PROBE_ENABLED`, `DD_SYSTEM_PROBE_NETWORK_ENABLED`, `DD_SYSTEM_PROBE_SERVICE_MONITORING_ENABLED`, `DD_SYSTEM_PROBE_EXTERNAL`, `DD_SYSPROBE_SOCKET`, `DD_AUTH_TOKEN_FILE_PATH`, `DD_RUNTIME_SECURITY_CONFIG_ENABLED`, `DD_COMPLIANCE_CONFIG_ENABLED`, `DD_SBOM_*`, `DD_KUBERNETES_KUBELET_HOST` (from `status.hostIP`), `DD_KUBELET_TLS_VERIFY`, `DD_CRI_SOCKET_PATH`, `DD_CLUSTER_AGENT_ENABLED`, `DD_CLUSTER_AGENT_AUTH_TOKEN`, `DD_CLUSTER_NAME`, `DD_CONTAINER_INCLUDE/EXCLUDE[_METRICS|_LOGS]`, `DD_CONTAINER_LABELS_AS_TAGS`, `DD_KUBERNETES_POD_LABELS_AS_TAGS`, `DD_EXTRA_CONFIG_PROVIDERS`, `DD_EXTRA_LISTENERS`, `DD_HEALTH_PORT`, `DD_PROXY_HTTP(S)`, `DD_OTLP_CONFIG_RECEIVER_PROTOCOLS_GRPC_ENDPOINT`, `DD_REMOTE_CONFIGURATION_ENABLED`, `DD_DISCOVERY_ENABLED`, `DD_ORCHESTRATOR_EXPLORER_ENABLED`, `HOST_ROOT`, `HOST_PROC`, `DOCKER_DD_AGENT`.

Breaking note for 7.85: with config streaming enabled, env overrides set only on the `process-agent`, `trace-agent`, `system-probe`, `security-agent` Helm containers stop working; they must be set on the core `agent` container or `datadog.env`.

Sources: [datadog.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/datadog-agent_linux.yaml.example), [system-probe.yaml example](https://raw.githubusercontent.com/DataDog/datadog-agent/main/pkg/config/example/system-probe_linux.yaml.example), [DaemonSet guide](https://docs.datadoghq.com/containers/guide/kubernetes_daemonset/), [Docker Agent](https://docs.datadoghq.com/containers/docker/), [Helm values](https://github.com/DataDog/helm-charts/blob/main/charts/datadog/values.yaml), [CSM Agent variables](https://docs.datadoghq.com/security/cloud_security_management/setup/agent_variables/).

---

## 5. Privilege summary

| Component | Linux privilege | Host access | Kernel floor |
|---|---|---|---|
| Core agent, trace-agent, process-agent (metrics, logs, APM, processes) | root by default; can run as uid 100 with the runtime-socket group | `/proc` (ro), `/sys/fs/cgroup` (ro), runtime socket, `/var/log/pods` or docker containers dir, `/etc/passwd` (ro, usernames), `/etc/os-release`, kubelet 10250 with TLS verify, hostPID (for DogStatsD origin detection and USM/CWS) | none |
| system-probe NPM/USM/CWS | `privileged: false` + CAP_SYS_ADMIN, SYS_RESOURCE, SYS_PTRACE, NET_ADMIN, NET_BROADCAST, NET_RAW, IPC_LOCK, CHOWN, DAC_READ_SEARCH; AppArmor unconfined; custom seccomp allow-list (bpf, perf_event_open, setns, ...) | `/sys/kernel/debug` (rw), `/sys/fs/bpf`, `/lib/modules`, `/usr/src` (runtime compilation), `/host/root` (USM HTTPS, CWS), `/etc/passwd`, `/etc/group`; conntrack via NET_ADMIN | NPM 4.4+; USM 4.14+ (HTTP/2, gRPC, Kafka 5.2+); CWS 4.14+ (degraded on 3.10); OOM kill 4.9+; CO-RE needs BTF or Datadog-hosted BTF download |
| security-agent (CSPM, CWS user space) | CAP_AUDIT_CONTROL, CAP_AUDIT_READ (chart) | `/host/root` (ro), `/etc/passwd`, `/etc/group`, runtime socket | none |
| OOM kill / TCP queue checks | `--privileged` or CAP_BPF (>= 5.8) under Docker | kernel headers present or downloadable | 4.9+ |
| host-profiler | CAP_BPF, PERFMON, SYS_PTRACE, SYS_RESOURCE, DAC_READ_SEARCH, SYSLOG, CHECKPOINT_RESTORE, IPC_LOCK; seLinux `spc_t`; drop ALL otherwise | `/sys/kernel/tracing` or debugfs (via profiler) | 5.10+ (per Oct-2026 PR) |
| SBOM (`uncompressedLayersSupport`) | adds CAP_SYS_ADMIN | container image layers via runtime | none |
| SNMP traps on port 162 | `setcap CAP_NET_BIND_SERVICE` on the agent binary | none | none |
| Private Action Runner | CAP_NET_RAW; rshell helper needs seccomp + landlock (7.84) | none | none |
| Windows | LocalSystem for system-probe; `ddagentuser` for the Agent; kernel drivers `ddnpm`/`ddprocmon`; ETW for IIS HTTPS | Event Log Readers group for Security channel | Server 2016+ (CWS 2019+) |

---

## 6. Feature matrix: Datadog capability -> Grafana/OTel equivalent -> gap

Grafana/OTel facts verified on 2026-10-27: Alloy v1.21.2 (2026-10-22; Beyla v3.38.0 embedded; OTel Collector v0.162.0 base); OBI (OpenTelemetry eBPF Instrumentation, the upstream of Beyla) v0.12.x, "in Development/v0", kernel 5.8+ (4.18+ on RHEL 8 derivatives) with BTF required; OTel eBPF profiler kernel 5.10+ for recent commits (last 5.4-compatible commit `8047150e`); Grafana k8s-monitoring Helm chart 4.5.5; Grafana OnCall OSS entered maintenance 2025-03-11, archived 2026-03-24 (GitHub archive banner 2026-06-05), replacement is Grafana Cloud IRM; Grafana Synthetic Monitoring agent is OSS but its API backend is Grafana Cloud only; Falco modern eBPF needs BPF ring buffer + BTF (effectively 5.8+), kmod 3.10+; Tetragon kernel 4.19+ with BTF mandatory. Items marked "(not fetched)" come from general knowledge of the project and were not re-verified in this session.

| Datadog capability | What it collects | Privilege | Candidate Grafana / OTel equivalent | Gap |
|---|---|---|---|---|
| Host system checks (cpu, mem, disk, io, net, load, ntp, uptime, fd) | host metrics every 15 s | none | `prometheus.exporter.unix` (node_exporter), `otelcol.receiver.hostmetrics` (in Alloy via OTel), `prometheus.exporter.windows` | none |
| Container metrics (Docker/containerd/CRI/Podman, cgroups) | per-container CPU/mem/io/net | runtime socket, `/sys/fs/cgroup` | `prometheus.exporter.cadvisor`, `otelcol.receiver.kubeletstats` (contrib), `dockerstatsreceiver`, `podmanreceiver` (contrib; Alloy exposes cadvisor + kubeletstats via the k8s chart) | none for k8s/docker; Podman only via contrib collector |
| Kubernetes state (KSM core) | `kubernetes_state.*` for ~30 resource kinds | DCA RBAC | `kube-state-metrics` (bundled by k8s-monitoring chart) + `k8sclusterreceiver` | none; KSM runs as a separate Deployment rather than inside the agent |
| Kubernetes events | curated event types | DCA RBAC | `loki.source.kubernetes_events`, `k8seventsreceiver` | none |
| Orchestrator Explorer (full manifests, YAML history, CRDs) | resource manifests as objects | DCA RBAC list/watch | `k8sobjectsreceiver` (DDOT itself uses it) -> Loki; chart feature `kubernetesManifests` (experimental) | UI/history diffing must be built; no scrubbing of secrets out of the box |
| 1,000+ integrations via Autodiscovery (annotations/labels/CRD/KV stores) | service-specific metrics/logs/service checks | varies | `prometheus.exporter.*` (28 in Alloy: postgres, mysql, mssql, oracledb, redis, mongodb, elasticsearch, kafka, memcached, consul, apache, squid, dnsmasq, snmp, blackbox, statsd, process, cloudwatch, gcp, azure, ...), `otelcol.receiver.*` (24 in Alloy; ~110 in contrib), `prometheus.operator.*` (ServiceMonitor/PodMonitor/Probe/ScrapeConfig), `annotationAutodiscovery` in k8s-monitoring chart, `otelcol.receiver.prometheus` + `receivercreator`/`discovery.*` | Breadth and the "drop a conf.yaml per integration" ergonomics; no auto_conf.yaml-style defaults keyed by image name; no service-check concept; JMX requires `jmxreceiver` (contrib, needs JRE) or JMX exporter sidecar |
| DogStatsD (UDP/UDS, origin detection, mapper) | custom metrics/events/service checks | hostPID for UDS origin detection | `prometheus.exporter.statsd`, `statsdreceiver` (contrib), `otelcol.receiver.datadog` (accepts Datadog agent payloads) | no UDS origin detection / container tagging; events and service checks have no first-class home (map to Loki/annotations) |
| Logs: files, journald, syslog TCP/UDP, Windows events, container stdout, k8s pods, multiline, masking | logs with source/service/tags | file access, pointer dir | `loki.source.file/journal/syslog/windowsevent/docker/kubernetes/podlogs/api/kafka/gelf/awsfirehose/gcplog/azure_event_hubs`, `otelcol.receiver.filelog/syslog/tcplog`, `loki.process` (multiline, replace, drop), `loki.secretfilter` (GA 1.20) | automatic multi-line detection and per-source `source`-based parsing pipelines are manual; `windows_event` XPath filtering must be expressed via `loki.source.windowsevent` filters |
| APM traces (8126, UDS), tracer SDKs, SSI injection | spans, trace metrics | none | OTel SDKs + `otelcol.receiver.otlp/jaeger/zipkin`, `otelcol.connector.spanmetrics/servicegraph`, OTel Operator `Instrumentation` CRD for auto-injection (not fetched), `otelcol.receiver.datadog` for Datadog-tracer compatibility | SSI parity depends on OTel Operator; Datadog's adaptive remote sampling has no OSS counterpart beyond `tail_sampling` |
| Continuous profiler (in-process SDK) | CPU/wall/alloc/heap/lock/exception profiles | none | Pyroscope SDKs (Go, Java, Python, Ruby, .NET, Node.js, Rust, PHP), `pyroscope.java` (async-profiler attach), `pyroscope.receive_http`, `pyroscope.scrape` | parity for mainstream languages; PHP/timeline features vary |
| Full-Host eBPF profiler (Preview) | whole-host CPU profiles, native + JVM/Python/Ruby/PHP/Node/.NET/Perl | BPF, PERFMON, SYS_PTRACE, CHECKPOINT_RESTORE, SYS_RESOURCE, DAC_READ_SEARCH, SYSLOG; kernel 5.10+ | `pyroscope.ebpf` (embeds Grafana's maintained variant of open-telemetry/opentelemetry-ebpf-profiler; same capability set; root + hostPID) | none in principle; both derive from the same upstream |
| OTLP ingest / DDOT embedded collector | OTLP metrics/traces/logs, pipelines | none | Alloy `otelcol.*` (it *is* an OTel Collector distribution) | none |
| Cloud Network Monitoring (eBPF flows, DNS, failed connections, conntrack, cloud entity mapping) | L4 flows between tagged endpoints | SYS_ADMIN + 7 caps, debugfs, hostPID | `beyla.ebpf` network metrics (socket_filter or TC; flows by src/dst, direction, protocol, k8s metadata; `network_inter_zone`), OBI TCP statistical metrics (RTT, failed/successful connections, retransmits) | Linux only (Windows ddnpm has no OSS equivalent); DNS server metrics and conntrack NAT resolution are partial; cloud LB entity mapping must be built from cloud inventory |
| Network Path (traceroute, dynamic from flows) | hop-by-hop latency/loss | NET_RAW via system-probe | none in Alloy; `blackbox` has no traceroute; Grafana SM has a traceroute check but backend is Cloud-only (not fetched) | gap: needs a small traceroute collector (e.g. wrap `mtr`/`traceroute` into metrics) |
| Universal Service Monitoring (HTTP/HTTPS, HTTP/2, gRPC, Kafka, TLS via OpenSSL/Go/Node/Istio, cloud service discovery) | RED metrics + dependency map without SDKs | same as CNM + `/host/root`, `/lib/modules`, `/usr/src` | `beyla.ebpf` / OBI: HTTP/1.x, HTTP/2, gRPC, MySQL/PostgreSQL/MSSQL, Redis, MongoDB, Kafka, Couchbase, Memcached, SunRPC, DNS, MQTT/NATS/AMQP, GraphQL/JSON-RPC/Elasticsearch/AWS S3-SQS-SNS/GenAI over HTTP; TLS parsed for most; context propagation for Go, Java, Node, Ruby, Python; GPU (CUDA) | OBI protocol coverage exceeds Datadog USM GA; gaps: Windows (IIS ETW) has no OSS equivalent; OBI is v0 "Development"; kernel 5.8+ with BTF vs Datadog 4.14+ (CO-RE/runtime-compile/prebuilt fallbacks) |
| Network Device Monitoring: SNMP polling, profiles, autodiscovery, topology, HA | device/interface metrics, metadata, LLDP/CDP | none (CAP_NET_BIND_SERVICE for port 162) | `prometheus.exporter.snmp` (snmp_exporter with generator-based modules), `snmpreceiver` (contrib), `discovery.*` for subnets is not built in | gap: no vendor profile library with metadata/topology; subnet autodiscovery, HA pairs and device inventory must be built |
| SNMP traps | traps as logs with OID resolution | UDP 9162 | none in Alloy; `snmptrapd` -> `loki.source.syslog` (manual); OTel has no trap receiver | gap: MIB/OID resolution DB (Datadog ships 11k MIBs) |
| NetFlow/IPFIX/sFlow | flow records with SNMP/geo/cloud enrichment | UDP listeners | `netflowreceiver` (contrib, goflow2-based) -> logs; not wrapped in Alloy; goflow2/pmacct standalone (not fetched) | partial: enrichment (SNMP ifIndex names, cloud IP ranges, ASN) must be built; UI for flow analytics |
| Live Processes / Live Containers / process discovery | per-process cmdline, user, CPU/RSS, I/O; 2 s live mode | `/etc/passwd`, hostPID; I/O via system-probe | `prometheus.exporter.process` (aggregated by group), `hostmetrics` process scraper (per-process metrics, contrib), OBI `survey` mode / process metrics, `osqueryreceiver` (contrib) | gap: no "live ps" stream with command lines and scrubbing; must be built as a plugin (periodic `/proc` snapshot -> Loki or a custom API) |
| Service Discovery (listening ports -> services, language, APM status) | service inventory | system-probe | OBI discovery (`open_ports`, `exe_path`, k8s selectors) + `survey_info`; `discovery.process` in Alloy | partial; "is it instrumented?" status needs a plugin |
| Workload Protection (CWS): 42 Linux event types, FIM, DNS/IMDS/network, activity dumps -> security profiles, enforcement, remote rules | runtime security events | SYS_ADMIN + caps, debugfs, `/host/root`, hostPID | Falco (CNCF, modern eBPF CO-RE, rules YAML, falcosidekick outputs) or Tetragon (process_exec/exit by default, kprobe/tracepoint/uprobe/LSM via `TracingPolicy`, enforcement via sigkill/override) -> `loki.source.*`/OTLP logs | Linux only (Windows CWS no OSS equivalent); security-profile learning/anomaly detection must be built; Fargate ptrace mode has no equivalent |
| CSPM host/container/k8s benchmarks (security-agent + DCA, 20 min) | compliance findings | `/host/root`, AUDIT caps | kube-bench, Trivy (`trivy k8s`/operator misconfig), OpenSCAP/XCCDF (not fetched) -> findings as logs/metrics | gap: a findings data model and scheduler; cloud-account CSPM needs Prowler/CloudQuery-style scanners (not fetched) |
| Container-image + host SBOM / vulnerabilities (Trivy-based) | SBOM, CVEs with runtime context | SYS_ADMIN for uncompressed layers | Trivy Operator (k8s, VulnerabilityReport CRDs), Trivy/Syft on hosts, `trivy` filesystem scans scheduled by Alloy? (no) | gap: vulnerability DB hosting, dedup across hosts, runtime prioritisation; agentless snapshot scanning has no OSS equivalent |
| Agentless cloud scanning (AWS/Azure/GCP snapshots, every 12 h) | host/container/Lambda vulns + misconfigs without agents | cloud IAM | none packaged (Prowler for posture; Trivy on mounted snapshots DIY) | gap |
| OOM kill / TCP queue eBPF checks | OOM events per container; socket buffer saturation | privileged or CAP_BPF | cAdvisor `container_oom_events_total`, kernel log via `loki.source.journal`, k8s events (OOMKilled); OBI TCP stats / node_exporter `sockstat` | partial |
| GPU monitoring (NVIDIA, NVLink) | GPU utilisation per process/pod | privileged system-probe | NVIDIA DCGM exporter (not fetched), OBI GPU/CUDA instrumentation, Kepler (energy; k8s chart) | none for device metrics; per-process attribution partial |
| Windows: perf counters, WMI, services, IIS, AD, .NET CLR, Event Log (XPath, Security channel), logon duration, crash detection | Windows metrics/logs/events | ddagentuser, Event Log Readers | `prometheus.exporter.windows` (windows_exporter collectors incl. iis, ad, dotnet, service, textfile), `loki.source.windowsevent`, `windowsperfcountersreceiver`, `windowseventlogreceiver`, `iisreceiver`, `activedirectorydsreceiver` (contrib) | arbitrary WMI queries have no receiver; Windows network/service/security kernel-driver features have no OSS equivalent |
| Database Monitoring (query metrics, samples, explain plans; Postgres/MySQL/MariaDB/SQL Server/Oracle/MongoDB/DocumentDB/ClickHouse) | DB-internal telemetry | DB user grants | `database_observability.mysql`, `.postgres` (stat_statements on by default since 1.21), `.sql_server` (health check, clustering, multi-db since 1.21); `prometheus.exporter.postgres/mysql/mssql/oracledb/mongodb`; `sqlqueryreceiver` | Oracle, MongoDB, DocumentDB, ClickHouse DBM-level depth missing; Alloy DBO stability labels not stated on the reference pages |
| Cluster Agent: cluster checks (single instance), endpoint checks, external metrics for HPA, admission controller (env/tag/socket injection, SSI, sidecar injection), control-plane checks, Kueue/Ray CR collection | cluster-level singletons | DCA RBAC, webhook TLS | k8s-monitoring chart `alloy-singleton` (clustered Alloy with leader-style singleton), `prometheus.operator.*`, Prometheus Adapter / KEDA for HPA (not fetched), OTel Operator webhook for injection (not fetched) | admission-time tag/env injection and the "run a check exactly once across the cluster with failover" dispatcher need a plugin; HPA external metrics via Prometheus Adapter |
| Remote Configuration + Fleet Automation (inventory, config history, remote integration/log config, upgrades, flares) | agent fleet control plane | outbound 443 | Alloy `remotecfg` block (poll `alloy-remote-config` API, Apache-2.0 proto; id/attributes; 1 m default poll; basic/oauth2/bearer auth), OpAMP supervisor for OTel Collector (not fetched) | gap: no OSS server for the remotecfg API (Grafana Fleet Management is Cloud-only); remote upgrade and flare collection must be built; this is the single biggest piece to own |
| Synthetics private locations | API/browser/ICMP tests from inside the network | NET_RAW/NET_ADMIN optional | `prometheus.exporter.blackbox` (HTTP/TCP/ICMP/DNS/gRPC), k6 (scripted/browser; not fetched), Grafana SM agent (OSS) | gap: SM backend is Cloud-only; a scheduler + results store for private probes must be built |
| RUM (not agent) | browser/mobile sessions | n/a | Grafana Faro web SDK + `faro.receiver` / `otelcol.receiver.faro`, `otelcol.exporter.faro` | none for web; mobile SDK maturity (not fetched) |
| Serverless: Lambda extension; `serverless-init` wrapper/sidecar | metrics/traces/logs from functions and PaaS containers | n/a | OpenTelemetry Lambda layers / collector extension (not fetched), Alloy or otelcol as Cloud Run/Container Apps sidecar | partial: enhanced Lambda metrics and Telemetry-API log capture are extension-specific; a small OTel-based "serverless-init" equivalent would be needed |
| Agentless cloud metrics/logs/resources (CloudWatch crawler/Metric Streams, Azure Monitor, GCP Monitoring; Firehose/Event Hub/Pub/Sub logs) | cloud service metrics and logs | cloud IAM | `prometheus.exporter.cloudwatch` (YACE), `.azure`, `.gcp`; `otelcol.receiver.awscloudwatch`, `azuremonitorreceiver`, `googlecloudmonitoringreceiver` (contrib); `loki.source.awsfirehose`, `.azure_event_hubs`, `.gcplog`; `otelcol.receiver.googlecloudpubsub` | resource inventory/relationship collection (for CSM/cost/entity mapping) must be built; no Metric Streams equivalent packaged |
| Secrets backends (command, AWS/Azure/Vault/file) | secret resolution | none | Alloy `remote.vault`, `remote.kubernetes.secret`, `remote.http`, `local.file`, `remote.s3` | none |
| Agent self-telemetry, health endpoints, flare | `/live`, `/ready`, expvar, flare | none | Alloy `/-/ready`, `/metrics`, `prometheus.exporter.self`, support bundle (`alloy` UI "Support bundle"; not fetched) | remote flare needs the fleet server |
| On-call (adjacent; Datadog On-Call is SaaS) | schedules, escalations, paging | n/a | Grafana OnCall OSS archived (maintenance 2025-03-11, archived 2026-03-24); Grafana Alerting has contact points but no schedules/escalations | gap: must be built or integrated (the aiops-platform on-call module) |

---

## 7. Takeaways for a single Alloy-based agent

1. Datadog's "one agent" is really **one package, many processes**: a core collector, a privileged eBPF sidecar (system-probe) reachable over a UDS + shared auth token, a user-space security agent, a trace receiver, an embedded OTel collector, and a cluster-level singleton. Alloy already covers the core collector and the OTel collector in one binary; the privileged pieces (OBI via `beyla.ebpf`, `pyroscope.ebpf`, and a Falco/Tetragon-class runtime-security sensor) should be isolated the same way Datadog isolates system-probe: separate container, explicit capability list (BPF, PERFMON, NET_ADMIN, NET_RAW, SYS_PTRACE, DAC_READ_SEARCH, CHECKPOINT_RESTORE, SYS_RESOURCE; SYS_ADMIN only for Go library-level propagation / debugfs on old kernels), `hostPID`, AppArmor unconfined, seccomp allow-list, and read-only `/host/root` only where needed.
2. The customer-facing config surface is tiny: an API key, a site, tags, and a handful of feature flags (`logs`, `apm`, `networkMonitoring`, `serviceMonitoring`, `securityAgent.runtime`, `sbom`, `orchestratorExplorer`). Everything else is derived. The k8s-monitoring Helm chart already works this way (`features.*` -> generated Alloy config); the same pattern should be used for hosts (one YAML with `features:` -> generated `config.alloy`).
3. Autodiscovery ergonomics (annotation/label -> check config, image-name defaults, cluster checks with exactly-once dispatch) are the main thing Alloy lacks as a product; `prometheus.operator.*`, `discovery.kubernetes` + relabel, and `otelcol.receiver.prometheus` are the building blocks, but the "integration catalogue with defaults" layer must be built as plugins.
4. Fleet control plane is the biggest missing piece: the `remotecfg` API is open (Apache-2.0 proto) but Grafana ships no OSS server, and there is nothing for remote upgrade, remote flare, or config history. This is where the platform should invest first because it is what makes "install once, manage centrally" real.
5. Windows parity is structurally hard: Datadog's network, USM and CWS features on Windows rely on proprietary kernel drivers and ETW. Plan for metrics/logs parity via `windows_exporter` + `loki.source.windowsevent`, and treat Windows network/security as a later, driver-free (packet capture or ETW-based) plugin.
6. Kernel floors differ: Datadog keeps 4.4/4.14 support with CO-RE -> runtime compilation -> prebuilt fallbacks (and downloads BTF/kernel headers from its own mirror). OBI (5.8+, BTF) and the OTel eBPF profiler (5.10+) are stricter; the platform should document that floor and consider shipping a BTF mirror (btfhub-style) for older distros rather than runtime compilation.
7. Several Datadog features are not agent features at all (RUM, agentless cloud, synthetics, Lambda extension, remote-config backend). The agent design should not try to absorb them; they are separate plugins on the server side.

---

## Sources

Datadog (docs read 2026-10-27; GitHub `main` at research time):

- https://docs.datadoghq.com/agent/architecture/
- https://github.com/DataDog/datadog-agent and https://github.com/DataDog/datadog-agent/tree/main/cmd
- https://github.com/DataDog/datadog-agent/releases and https://github.com/DataDog/datadog-agent/releases/tag/7.84.0
- https://github.com/DataDog/datadog-agent/blob/main/pkg/config/example/datadog-agent_linux.yaml.example
- https://github.com/DataDog/datadog-agent/blob/main/pkg/config/example/system-probe_linux.yaml.example
- https://github.com/DataDog/datadog-agent/blob/main/pkg/ebpf/config.go
- https://github.com/DataDog/datadog-agent/blob/main/Dockerfiles/agent/Dockerfile
- https://github.com/DataDog/datadog-agent/blob/main/Dockerfiles/manifests/rbac/clusterrole.yaml
- https://github.com/DataDog/datadog-agent/blob/main/docs/cloud-workload-security/linux_expressions.md
- https://github.com/DataDog/datadog-agent/pull/58270, https://github.com/DataDog/datadog-agent/pull/58770, https://github.com/DataDog/datadog-agent/pull/56977, https://github.com/DataDog/datadog-agent/issues/28356
- https://github.com/DataDog/helm-charts/blob/main/charts/datadog/values.yaml and https://github.com/DataDog/helm-charts/issues/2943
- https://docs.datadoghq.com/resources/yaml/datadog-agent-all-features.yaml (rendered chart 3.96.0, agent 7.63.0)
- https://github.com/DataDog/datadog-operator/blob/main/docs/configuration.v2alpha1.md
- https://docs.datadoghq.com/containers/kubernetes/installation/, /containers/kubernetes/integrations/, /containers/kubernetes/log/, /containers/guide/kubernetes_daemonset/, /containers/docker/, /containers/amazon_ecs/, /integrations/ecs_fargate/
- https://docs.datadoghq.com/containers/cluster_agent/, /containers/cluster_agent/clusterchecks/, /containers/cluster_agent/admission_controller/, /integrations/kubernetes_state_core/, /infrastructure/containers/orchestrator_explorer/
- https://docs.datadoghq.com/agent/logs/, /agent/supported_platforms/, /agent/basic_agent_usage/windows/, /agent/faq/how-datadog-agent-determines-the-hostname/, /agent/fleet_automation/, /agent/fleet_automation/remote_management/, /remote_configuration/
- https://docs.datadoghq.com/network_monitoring/performance/setup/, /network_monitoring/network_path/setup/, /network_monitoring/devices/setup/, /network_monitoring/devices/snmp_traps/, /network_monitoring/devices/netflow/
- https://docs.datadoghq.com/universal_service_monitoring/setup/, /universal_service_monitoring/additional_protocols/
- https://docs.datadoghq.com/security/workload_protection/setup/, /security/workload_protection/setup/agent/docker/, /security/workload_protection/setup/agent/kubernetes/, /security/cloud_security_management/setup/, /security/cloud_security_management/setup/agent_variables/, /security/cloud_security_management/misconfigurations/, /security/cloud_security_management/vulnerabilities/
- https://www.datadoghq.com/blog/engineering/ebpf-workload-protection-lessons/
- https://docs.datadoghq.com/infrastructure/process/, /integrations/oom_kill/, /integrations/tcp_queue_length/, /integrations/java/, /integrations/win32_event_log/, /database_monitoring/
- https://docs.datadoghq.com/profiler/enabling/, /profiler/enabling/full_host/, https://github.com/DataDog/opentelemetry-ebpf-profiler
- https://docs.datadoghq.com/opentelemetry/setup/ddot_collector/, /opentelemetry/setup/ddot_collector/install/kubernetes/, /opentelemetry/setup/ddot_collector/install/linux/, /opentelemetry/setup/otlp_ingest_in_the_agent/
- https://docs.datadoghq.com/serverless/aws_lambda/, /serverless/aws_lambda/installation/, /serverless/libraries_integrations/extension/, /serverless/google_cloud_run/, /serverless/google_cloud_run/containers/
- https://docs.datadoghq.com/synthetics/private_locations/
- https://docs.datadoghq.com/integrations/amazon_web_services/, /integrations/azure/, /integrations/google_cloud_platform/
- https://docs.datadoghq.com/developers/dogstatsd/high_throughput/, https://github.com/DataDog/saluki

Grafana / OpenTelemetry / CNCF (read 2026-10-27):

- https://grafana.com/docs/alloy/latest/reference/components/ (+ /loki/, /otelcol/, /prometheus/, /beyla/beyla.ebpf/, /pyroscope/pyroscope.ebpf/, /database_observability/), https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/, https://github.com/grafana/alloy/releases
- https://github.com/open-telemetry/opentelemetry-ebpf-instrumentation, its SUPPORT_MATRIX.md, https://opentelemetry.io/docs/zero-code/obi/security/
- https://github.com/open-telemetry/opentelemetry-ebpf-profiler
- https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/receiver (listing truncated at `syslogreceiver` in the fetched view)
- https://github.com/grafana/k8s-monitoring-helm and its charts/k8s-monitoring/README.md
- https://github.com/grafana/oncall (archived)
- https://grafana.com/docs/grafana-cloud/testing/synthetic-monitoring/set-up/set-up-private-probes/, https://github.com/grafana/synthetic-monitoring-agent
- https://github.com/falcosecurity/falco, https://falco.org/docs/concepts/event-sources/kernel/
- https://github.com/cilium/tetragon, https://tetragon.io/docs/installation/faq/

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
