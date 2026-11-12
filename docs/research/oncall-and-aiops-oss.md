# On-call options and AIOps OSS building blocks

Research date: 2026-10-27. All GitHub facts (archive flag, license, last push, latest release) were read live from the GitHub API on that date via `gh api`; web facts cite the page they came from. Anything not verified against a primary source is tagged **UNVERIFIED**. Star counts are rounded.

Context: the new platform sits on the Grafana stack (Alloy on hosts, Mimir/Loki/Tempo/Pyroscope behind), adds an AIOps layer per pillar, Slack chat-ops, and an on-call module. Everything is a Harness-style plugin that can be tested alone before being attached.

---

## Part A: filling the Grafana OnCall gap

### A.1 What actually happened to Grafana OnCall OSS

| Fact | Value | Source |
|---|---|---|
| Maintenance mode | 2025-03-11 (critical bugs and CVSS >= 7.0 only) | [Grafana blog FAQ](https://grafana.com/blog/grafana-oncall-maintenance-mode/) |
| Read-only / archived | 2026-03-24 at v1.16.11 | [README](https://github.com/grafana-cold-storage/oncall), GitHub `archived: true` |
| Repo location | `grafana/oncall` now redirects to `grafana-cold-storage/oncall` (GitHub archive flag set; third-party page reports the flag date as 2026-06-05, **UNVERIFIED**) | GitHub API |
| Last release | v1.16.11, 2026-02-11 | GitHub releases |
| Last push | 2026-03-24 | GitHub API |
| License | AGPL-3.0 | GitHub API |
| Stack | Python/Django engine + Celery + Redis/RabbitMQ + MySQL/Postgres + Grafana frontend plugin | repo layout |
| Successor | Grafana Cloud IRM (OnCall + Incident merged), cloud only, launched 2025-03-11 | [IRM announcement](https://grafana.com/blog/2025/03/11/oncall-management-incident-response-grafana-cloud-irm/) |

What still works on a self-hosted OnCall OSS after 2026-03-24: the engine itself, schedules, escalation chains, Slack, Telegram, email, outgoing webhooks, and phone/SMS through a directly configured provider. The engine settings still carry `TWILIO_*`, `ZVONOK_*` and `EXOTEL_*` variables (read from `engine/settings/base.py`), so SMS/voice never depended on Grafana Cloud. What stopped: the Grafana Cloud Connection (Grafana-provided SMS/phone), the IRM mobile app push, and the cloud heartbeat (`GRAFANA_CLOUD_*` settings). Grafana's migration tooling does not accept OnCall OSS as a source; the documented path is Terraform or the OnCall API ([migration guide](https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/set-up/migrate/oncall-oss/)).

Community maintenance (GitHub API 2026-10-27): `appwrite/grafana-oncall` is an AGPL-3.0 variant of the archived upstream (13 stars, pushed 2026-10-27). Its README says it runs on Grafana 13.2 / React 19 / Django 5.2 LTS, adds Discord integration and MSG91 SMS, and fixes the Insights page on Grafana 13. The survey identified this one active company-maintained edition; that is not evidence of broad community maintenance.

### A.2 Candidate profiles (verified 2026-10-27)

**Keep (`keephq/keep`)**. Python, 12.4k stars, MIT core plus an `ee/` directory under the "Keep Enterprise Edition License" (production use only with a Keep subscription). Latest release v0.54.3 (2026-09-09), last push 2026-10-22. Acquired by Elastic: agreement announced 2025-05-07, completion reported for May 2025 ([Elastic on X](https://x.com/elastic/status/1920102960807190813), [CFOtech](https://cfotech.com.au/story/elastic-completes-acquisition-of-aiops-company-keep)); Elastic said it will remain open source. Feature set from the doc index ([llms.txt](https://docs.keephq.dev/llms.txt)): deduplication and fingerprints, manual correlation rules, AI correlation, topology correlation, incidents, enrichment (extraction/mapping), maintenance windows, YAML workflows with a CLI and GitHub Action, roughly 121 providers including Grafana, Prometheus, Grafana OnCall, PagerDuty, Opsgenie, ilert, Squadcast, Zenduty, Slack, Teams, Twilio. **Keep has no native on-call schedules or escalation policies**; paging is delegated to those providers. A third-party 2026 review reports two maintainers merging all 2026 PRs ([aurorasre.ai](https://www.aurorasre.ai/blog/keep-vs-aurora-open-source-aiops)) — **UNVERIFIED** beyond that source.

**GoAlert (`target/goalert`)**. Go + React, Apache-2.0, 2.9k stars, v0.35.0 (2026-09-21), last push 2026-10-26, single binary on PostgreSQL. From the source tree: integration key types `grafana`, `prometheusAlertmanager`, `site24x7`, `generic`, `email`, `universal`; notification packages `twilio` (SMS + voice), `slack`, `email`, `webhook`; auth providers `basic`, `github`, `oidc`; GraphQL API (`graphql2`) plus API keys; packages for `schedule`, `override`, `escalation`, `oncall`, `heartbeat`, `calsub` (calendar subscriptions). No mobile push app, no status page, no incident timeline or postmortem ([docs](https://goalert.me/docs/)). Single-tenant (no org concept).

**LinkedIn Oncall + Iris (`linkedin/oncall`, `linkedin/iris`)**. Python, BSD-2-Clause, MySQL. Oncall is scheduling only (teams, rosters, calendar, LDAP/Slack user sync, API); paging lives in Iris (escalation plans, SMS/call/email). Oncall last release v2.1.7 (2024-03-12), last push 2025-08-20; Iris last release v1.0.37 (2021-11-16), last push 2025-06-12. Alive but barely.

**Netflix Dispatch (`Netflix/dispatch`)**. Python, Apache-2.0, 6.5k stars. Archived on 2025-09-03 (README said 2025-09-01); last release v20241220. `Netflix/dispatch-docker` archived 2025-10-07. It is incident coordination (Slack channels, GSuite docs, Jira, tasks, reports) with a GenAI incident summary; it never did scheduling or paging. One independent continuation exists, `helldweller/dispatch` (Apache-2.0, 0 stars, pushed 2026-10-26).

**Cabot (`arachnys/cabot`)**. Python/Django, MIT, 5.7k stars. README still says "Maintainers wanted"; last release 0.11.7 (2017-09-14), last push 2023-09-10. Graphite checks, Twilio calls/SMS, schedules from Google Calendar. Dead for our purposes.

**Alertmanager + Karma**. Alertmanager v0.34.1 (2026-09-17), Apache-2.0, Go. Receiver types in `config/config.go`: discord, email, incidentio, jira, mattermost, msteams, msteamsv2, opsgenie, pagerduty, pushover, rocketchat, slack, sns, telegram, victorops, webex, webhook, wechat. Grouping, routing tree, inhibition, silences, time intervals; no schedules, no escalation, no ack. Karma v0.134 (2026-10-22), Apache-2.0: multi-Alertmanager dashboard with dedup, silence management, no paging.

**OpenDuty**. `ustream/openduty` archived (MIT, last push 2019-01-15); `openduty/openduty-archive` last push 2023-01-03. Dead.

**OneUptime (`OneUptime/oneuptime`)**. TypeScript, 7.8k stars, release 14.0.93 (2026-10-27). LICENSE says core is Apache-2.0 and `ee/` (AdminDashboard, Dashboard) is under a separate `ee/LICENSE`, so it is open-core at repo level even though the vendor markets "no feature gating" ([OneUptime comparison](https://oneuptime.com/compare/opsgenie)). It is a monolith: uptime monitoring, OpenTelemetry backend, status pages, on-call with phone/SMS/email/push, escalation, incidents, postmortems. The closest OSS PagerDuty-equivalent, but you cannot take only the on-call piece.

**SigNoz**. MIT + `ee/`, v0.147.0 (2026-10-22). No native on-call or escalation; routes to PagerDuty/Opsgenie/incident.io (incident.io channel added in v0.141.0, 2026-09-09, [changelog](https://signoz.io/changelog/2026-09-09-incidentio-added-as-a-new-alert-channel-p0mdff33y7e5n3qjuq8ac4k0/)).

**Uptime Kuma**. MIT, 93k stars, pushed 2026-10-27. Uptime checks, status pages, 90+ notification providers, no schedules or escalation. Not on-call.

**Datadog On-Call** (reference only). GA January 2025. Team -> routing rules (urgency, support hours) -> escalation policy (steps with ack timeout, notify-all or round-robin, user/team/schedule targets) -> multi-layer schedules. SMS, voice, push, email. $20/seat/month annual ([docs](https://docs.datadoghq.com/incident_response/on-call/), [escalation docs](https://docs.datadoghq.com/incident_response/on-call/escalation_policies/)).

**Commercial parity references** (not candidates; feature bar). incident.io (Slack-native, on-call add-on, status page included on free), Rootly (schedules, overrides, PTO-aware, status pages, Terraform provider `rootlyhq/terraform-provider-rootly` MPL-2.0), FireHydrant Signals (runbooks, service catalog; reported acquired by Freshworks in 2026, **UNVERIFIED**), ilert (EU-hosted, status pages, dedup/grouping), Squadcast (SolarWinds; SLO-aware), Zenduty (reported rebranded Xurrent IMR, **UNVERIFIED**). Opsgenie sunset reported as April 2027 (**UNVERIFIED**, third-party pages). Source for the set: [Hyperping](https://hyperping.com/blog/incident-io-alternatives), [Better Stack](https://betterstack.com/community/comparisons/incident-io-vs-zenduty/).

### A.3 Feature matrix

Legend: Y = yes, N = no, P = partial/through integration, `-` = not applicable. "Last commit" is the GitHub `pushed_at` on 2026-10-27.

| Tool | Schedules / rotations | Escalation policies | Slack | SMS / phone | Push | Email | Alertmanager / Grafana webhook in | Incident timeline / postmortem | Status page | API | License | Language | Last release | Last commit | Status |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| Grafana OnCall OSS | Y (layers, overrides, iCal) | Y | Y (deep) | Y (Twilio/Zvonok/Exotel direct) | N (mobile app cut off 2026-03-24) | Y | Y (native Alertmanager + Grafana integrations) | P (alert-group timeline; postmortem was Grafana Incident) | N | Y (REST + Terraform) | AGPL-3.0 | Python | v1.16.11 2026-02-11 | 2026-03-24 | archived |
| appwrite/grafana-oncall variant | Y | Y | Y + Discord | Y (+MSG91) | N | Y | Y | P | N | Y | AGPL-3.0 | Python | none tagged | 2026-10-27 | company-maintained AGPL variant |
| Keep | N | N | Y | P (Twilio provider) | N | Y | Y (Prometheus, Grafana, ~121 providers) | Y (incidents, AI summary) | N | Y (REST, CLI) | MIT + EE | Python | v0.54.3 2026-09-09 | 2026-10-22 | Elastic-owned, active |
| GoAlert | Y (rotations, overrides, calendar subs) | Y | Y (DM + channel) | Y (Twilio) | N | Y | Y (Alertmanager, Grafana, generic, email, universal) | N | N | Y (GraphQL + API keys) | Apache-2.0 | Go | v0.35.0 2026-09-21 | 2026-10-26 | active |
| LinkedIn Oncall + Iris | Y | Y (Iris plans) | P | Y (Iris/Twilio) | P (iris-mobile) | Y | P (generic API) | N | N | Y | BSD-2 | Python | v2.1.7 2024-03 / v1.0.37 2021-11 | 2025-08 / 2025-06 | low activity |
| Netflix Dispatch | N | N | Y (deep) | P (via PagerDuty/Opsgenie plugin) | N | Y | P (signals) | Y | N | Y | Apache-2.0 | Python | v20241220 | 2025-09-03 | archived |
| Cabot | P (Google Calendar) | P | N (HipChat) | Y (Twilio) | N | Y | N (Graphite/HTTP/Jenkins checks) | N | N | P | MIT | Python | 0.11.7 2017-09 | 2023-09-10 | unmaintained |
| Alertmanager + Karma | N | N (routes, not escalation) | Y | P (via PagerDuty/Opsgenie/SNS/Pushover) | P (Pushover) | Y | - (it is the source) | N | N | Y | Apache-2.0 | Go | v0.34.1 / v0.134 | 2026-10-26 | active |
| OpenDuty | Y | Y | N | Y | N | Y | P | N | N | Y | MIT | Python | none | 2019/2023 | dead |
| OneUptime | Y | Y | Y | Y | Y | Y | Y (webhooks, OTel) | Y | Y | Y | Apache-2.0 + ee/ | TypeScript | 14.0.93 2026-10-27 | 2026-10-27 | active monolith |
| SigNoz | N | N | Y | P | N | Y | - | N | N | Y | MIT + ee/ | TS/Go | v0.147.0 2026-10-22 | 2026-10-27 | active, not on-call |
| Uptime Kuma | N | N | Y | P (Twilio etc.) | P (ntfy/Gotify) | Y | N | N | Y | Y | MIT | JavaScript | - | 2026-10-27 | active, not on-call |
| Datadog On-Call | Y | Y | Y | Y | Y | Y | Y | Y (Incident Mgmt bundle) | Y | Y | commercial | - | GA 2025-01 | - | reference |
| Grafana Cloud IRM | Y | Y | Y | Y | Y | Y | Y | Y | N | Y | commercial | - | 2025-03-11 | - | reference |

Push building blocks if we build our own: `binwiederhier/ntfy` (Apache-2.0, Go, 35k stars, pushed 2026-10-27) and `gotify/server` (MIT-style, Go, 16k stars) are self-hostable push servers with iOS/Android apps.

### A.4 Recommendation: embed GoAlert behind a provider interface now, build the minimal native plugin only when it earns its place

**Decision**: do not build a native on-call engine for the MVP. Define one small `OnCallProvider` plugin interface (who-is-on-call, page, ack/resolve, list schedules) and ship adapters: `oncall-goalert` (embedded, self-hosted default) plus thin `oncall-pagerduty`, `oncall-opsgenie`, `oncall-ilert` webhook adapters. Revisit a native module in phase 2 with a concrete trigger (below).

Reasoning:

1. **The gap is scheduling + escalation + ack, and GoAlert already does exactly that under Apache-2.0.** It is a single Go binary on Postgres, releases monthly, last push yesterday, accepts Alertmanager and Grafana webhooks natively, and has a GraphQL API we can drive from a plugin. Everything else in the OSS field is either archived (OnCall, Dispatch, OpenDuty, Cabot), does not do scheduling (Keep, SigNoz, Alertmanager, Uptime Kuma), or is a monolith that cannot be taken apart (OneUptime).
2. **Do not adopt the archived OnCall implementation.** Its AGPL root and separately licensed plugin directory require their own review, and a Django+Celery+Redis+MySQL deployment adds substantial operational weight. This historical proposal instead integrates a permissively licensed provider through a supported interface; no OnCall source, schema, template or UI text is copied.
3. **Multi-tenancy is the real limit of GoAlert, and it is a hosted-only problem.** The primary audience self-hosts, where one GoAlert per installation is fine. For the hosted tier, run one GoAlert per tenant (it is a single binary; the cost is a Postgres schema and a container). That is cheaper than writing and debugging schedule math.
4. **Notifications are commodity.** Twilio/Vonage for SMS and voice, SMTP, Slack, and ntfy for push are each a few hundred lines in any language. GoAlert covers the first three; push is the one gap (ntfy via GoAlert's webhook contact method covers it well enough).
5. **The plugin interface is what keeps us honest.** If GoAlert is hidden behind `OnCallProvider`, the adapter is testable in isolation (contract tests against the GoAlert demo container `goalert/demo`), and swapping it for a native module later changes one plugin, not the platform.

Trigger for building the native module (phase 2), any one of: hosted tenants exceed what one-GoAlert-per-tenant can operate; Slack-native schedule editing becomes a top request; incident timeline and postmortem need to live in the same data model as schedules. When that happens, port GoAlert's `schedule`, `override`, `escalation` and `oncall` package semantics (Apache-2.0 permits it) into a Go plugin with roughly ten tables, keep Alertmanager as the routing core, and reuse the same adapters for notification providers. Do not port the UI; the plugin renders in Grafana and Slack.

Incident timeline and postmortem are a separate, small plugin regardless of the on-call choice (Dispatch and Keep both show the data model: incident, participants, timeline events, tasks, summary). Status pages are out of scope for MVP; `louislam/uptime-kuma` or OneUptime cover that for people who need it.

---

## Part B: OSS building blocks per pillar

All rows verified against GitHub on 2026-10-27 unless marked.

### B.1 Incident

| Tool | What it gives us | License | Lang | Latest release | Last push | Maturity / owner |
|---|---|---|---|---|---|---|
| HolmesGPT (`HolmesGPT/holmesgpt`, moved from `robusta-dev`) | Agentic RCA over Prometheus/Loki/Tempo/k8s with toolsets and runbooks; CLI and Python library | Apache-2.0 | Python | 0.45.0 2026-10-15 | 2026-10-27 | CNCF Sandbox since 2025-10-08 ([CNCF](https://www.cncf.io/projects/holmesgpt/)); Robusta + Microsoft |
| Robusta (`robusta-dev/robusta`) | Playbooks (YAML triggers -> actions) for Prometheus alerts on k8s; enrichment, auto-remediation, Slack sinks | MIT | Python | 0.53.0 2026-10-15 | 2026-10-26 | Robusta.dev |
| k8sgpt (`k8sgpt-ai/k8sgpt`) | Analyzers for k8s objects with LLM explanation; operator mode | Apache-2.0 | Go | v0.4.41 2026-10-21 | 2026-10-26 | CNCF Sandbox since 2023-12-19 |
| Grafana Sift / Assistant Investigations | ML checks (kube crashes, noisy neighbors, error patterns) and agentic investigations | not OSS, Grafana Cloud only | - | Investigations GA 2026-07-29 ([what's new](https://grafana.com/whats-new/2026-07-29-assistant-investigations-is-now-generally-available-in-grafana-cloud/)) | - | Reference only; "Sift-like correlation" must be built |
| Keep | Dedup, correlation rules, AI correlation, incidents, workflows | MIT + EE | Python | v0.54.3 | 2026-10-22 | Elastic |
| OpenTelemetry Collector (`open-telemetry/opentelemetry-collector`) | Pipeline runtime | Apache-2.0 | Go | - | 2026-10-27 | CNCF Graduated |
| Grafana Alloy (`grafana/alloy`) | OTel Collector distribution with Prometheus/Loki/Pyroscope/eBPF components; the host agent | Apache-2.0 | Go | v1.21.2 2026-10-22 | 2026-10-27 | Grafana Labs |

Notes. HolmesGPT is the only CNCF-governed SRE agent with a Grafana-native toolset; wrap it as the `incident-rca` plugin rather than writing our own agent loop. Robusta's playbook engine duplicates what a workflow plugin would do; borrow its trigger/action schema, not its runtime (it bundles its own Prometheus stack). Keep's correlation and dedup are the closest OSS to Sift; its Elastic ownership and `ee/` split make it a reference, not a dependency.

### B.2 FinOps

| Tool | What it gives us | License | Lang | Latest release | Last push | Notes |
|---|---|---|---|---|---|---|
| OpenCost (`opencost/opencost`) | k8s allocation + cloud cost ingestion (AWS/Azure/GCP billing), Prometheus-backed, API | Apache-2.0 | Go | v1.121.3 2026-09-16 | 2026-10-26 | CNCF Incubating |
| Kubecost (IBM) | Rightsizing, savings, multi-cluster UI | proprietary; helm chart repo `kubecost/kubecost` Apache-2.0 | - | - | 2026-10-26 | Free tier: <= 250 cores, 15-day retention; Kubecost 3.0 adds a $100k/30-day spend gate on free ([Apptio blog](https://www.apptio.com/blog/ibm-kubecost-3-0-faster-smarter-and-built-for-scale/)). IBM acquisition Sep 2024 **UNVERIFIED** date |
| krr (`robusta-dev/krr`) | CPU/memory request recommendations from Prometheus history, CLI, no in-cluster agent | MIT | Python | v1.31.1 2026-10-19 | 2026-10-19 | Reads Mimir directly |
| Goldilocks (`FairwindsOps/goldilocks`) | VPA-based request recommendations with dashboard | Apache-2.0 | Go | v4.16.2 2026-09-15 | 2026-10-18 | needs VPA installed |
| Infracost (`infracost/infracost`) | IaC cost diff (Terraform) in CI | Apache-2.0 | Go | v0.10.47 2026-10-21 | 2026-10-21 | Infracost Cloud is commercial |
| Cloud billing APIs | AWS CUR / Cost Explorer, Azure Cost Management exports, GCP Billing export to BigQuery | vendor | - | - | - | general knowledge, no single source |

### B.3 SecOps

| Tool | What it gives us | License | Lang | Latest release | Last push | Notes |
|---|---|---|---|---|---|---|
| Falco (`falcosecurity/falco`) | eBPF runtime threat detection, rules, Falcosidekick outputs | Apache-2.0 | C++ | 0.45.1 2026-10-19 | 2026-10-26 | CNCF Graduated |
| Trivy Operator (`aquasecurity/trivy-operator`) | In-cluster vuln, misconfig, RBAC, exposed-secret reports as CRDs | Apache-2.0 | Go | v0.35.0 2026-10-05 | 2026-10-26 | Aqua |
| Kubescape (`kubescape/kubescape`) | Posture scanning, frameworks, runtime | Apache-2.0 | Go | v4.0.16 2026-10-14 | 2026-10-26 | CNCF Incubating |
| Prowler (`prowler-cloud/prowler`) | Multi-cloud CSPM checks + compliance packs; Prowler App local server | Apache-2.0 | Python | 5.46.0 2026-10-26 | 2026-10-26 | SaaS is commercial |
| Wazuh (`wazuh/wazuh`) | XDR/SIEM agent, FIM, vuln detection | GPL-2.0 mostly; `src/engine` AGPL-3.0; shared libs dual GPL-2.0/AGPL-3.0 (LICENSE file) | C++ | - | 2026-10-27 | Heavy; copyleft mix |
| ScoutSuite (`nccgroup/ScoutSuite`) | Multi-cloud audit reports | GPL-2.0 | Python | 5.14.0 2024-05-10 | 2025-09-23 | low activity, GPL |
| CloudQuery (`cloudquery/cloudquery`) | Cloud asset inventory ELT | MPL-2.0 framework/SDK | Go | - | 2026-10-26 | Official AWS/GCP/Azure plugins moved to open-core then paid ([announcement](https://www.cloudquery.io/blog/cloudquery-official-free-plugins-moving-to-paid)); avoid for inventory |
| Steampipe (`turbot/steampipe`) | SQL over cloud APIs, zero ETL | AGPL-3.0 core; plugins Apache-2.0 (`steampipe-plugin-aws`) | Go | v2.4.8 2026-10-22 | 2026-10-22 | AGPL core is a network service, fine if not linked |

### B.4 Compliance

| Tool | What it gives us | License | Lang | Latest release | Last push | Frameworks (verified from repo) |
|---|---|---|---|---|---|---|
| Kubescape frameworks (`kubescape/regolibrary`) | Posture frameworks as Rego | Apache-2.0 | Rego | - | - | cis-v1.10.0, cis-v1.12.0, cis-aks 1.2/1.8, cis-eks 1.7/1.8, cis-gke 1.9, nsaframework, mitre, soc2, devopsbest, armobest, ism, security, allcontrols, clusterscan, workloadscan, agent-runtime-hardening |
| Prowler compliance packs | JSON compliance mappings per provider | Apache-2.0 | - | 5.46.0 | 2026-10-26 | AWS: cis 1.4-7.0, iso27001 2013/2022, soc2, pci 3.2.1/4.0, hipaa, gdpr, nis2, nist 800-53 r4/r5, nist 800-171 r2, nist csf 1.1/2.0, fedramp low/moderate, ffiec, mitre_attack, ens, c5, kisa, rbi, secnumcloud, well-architected pillars, threatscore and more; Azure 19 packs, GCP 17, Kubernetes 9, GitHub 3, M365 6 |
| OpenSCAP (`OpenSCAP/openscap`) + ComplianceAsCode (`ComplianceAsCode/content`) | Host-level SCAP scanning and remediation (Ansible/Bash) | LGPL-2.1 / BSD-3-Clause | C / XSLT, Shell | - | 2026-10-23 / 2026-10-26 | CIS, STIG, PCI, HIPAA profiles per OS |
| OPA Gatekeeper (`open-policy-agent/gatekeeper`) | Admission policy as Rego, audit of existing resources | Apache-2.0 | Go | v3.23.1 2026-08-27 | 2026-10-26 | OPA Graduated |
| Kyverno (`kyverno/kyverno`) | YAML policies, admission + background reports (PolicyReport CRD) | Apache-2.0 | Go | v1.19.2 2026-10-20 | 2026-10-27 | listed Graduated on [cncf.io/projects](https://www.cncf.io/projects/) |
| Checkov (`bridgecrewio/checkov`) | IaC/static compliance checks | Apache-2.0 | Python | 3.3.30 2026-10-20 | 2026-10-26 | Palo Alto / Prisma |

Kyverno's PolicyReport and Trivy Operator's report CRDs are the same shape (Kubernetes `wgpolicyk8s.io` PolicyReport), so one `compliance-k8s` collector can read both.

Framework content terms (added 2026-10-28, all **UNVERIFIED**, from the licensing review and not checked against the publishers' sites): framework content carries its own copyright and trademark terms, separate from the scanners' Apache-2.0 code. CIS Benchmarks are reported to be under CC BY-NC-SA 4.0, with commercial use through CIS SecureSuite membership, and "CIS" is a registered mark. MITRE ATT&CK reuse is reported to need a specific attribution notice. These terms must be verified before a hosted, commercial service shows a per-framework score (08-profiles-roadmap-adrs.md §5.1 L17).

### B.5 Change

| Tool | What it gives us | License | Lang | Latest release | Last push | Notes |
|---|---|---|---|---|---|---|
| Argo CD (`argoproj/argo-cd`) | Sync/health events; built-in notifications engine | Apache-2.0 | Go | - | 2026-10-27 | CNCF Graduated. `argoproj/notifications-engine` services (repo tree): alertmanager, awssqs, email, github, googlechat, **grafana** (annotations), mattermost, nats, newrelic, opsgenie, pagerduty, pagerdutyv2, pubsub, pushover, rocketchat, slack, teams, telegram, webex, webhook |
| Flux (`fluxcd/flux2`, `notification-controller`) | Reconciliation events; Alert/Provider CRDs | Apache-2.0 | Go | - | 2026-10-26 / 2026-10-24 | CNCF Graduated. Notifier providers (repo tree): alertmanager, azure_devops, azure_eventhub, bitbucket(server), datadog, discord, gitea, github, gitlab, google_chat, google_pubsub, **grafana**, incidentio, lark, mastodon, matrix, nats, opsgenie, otel, pagerduty, rocket, sentry, slack, teams, telegram, webex, zoom, zulip, plus PR/MR comment notifiers |
| Keptn (`keptn/lifecycle-toolkit`) | Pre/post deployment tasks and SLO evaluation | Apache-2.0 | Go | keptn-v2.5.0 2025-03-19 | 2026-10-26 | **CNCF archived 2025-09-03** ([CNCF project page](https://www.cncf.io/projects/keptn/)); do not depend on it |
| Kargo (`akuity/kargo`) | Multi-stage promotion with freight tracking | Apache-2.0 | Go | v1.12.4 2026-10-12 | 2026-10-27 | Akuity |
| Terraform plan diffs | `terraform show -json` plan; Atlantis (`runatlantis/atlantis`, Apache-2.0) and OpenTofu (`opentofu/opentofu`, MPL-2.0) | Apache-2.0 / MPL-2.0 | Go | - | 2026-10-27 | Both active |
| Renovate (`renovatebot/renovate`) | Dependency update PRs | AGPL-3.0 | TypeScript | - | 2026-10-27 | Mend; consume its PR events, never link |

Both GitOps engines already emit Grafana annotations and Alertmanager alerts. The change pillar's MVP is a collector that turns Argo/Flux/Atlantis/Renovate events into a unified change record stored as Grafana annotations (so every dashboard shows them) and correlated with incidents.

### B.6 Continuous improvement

| Tool | What it gives us | License | Lang | Latest release | Last push | Notes |
|---|---|---|---|---|---|---|
| Pyroscope (`grafana/pyroscope`) | Continuous profiling backend; Alloy collects (eBPF and SDK) | AGPL-3.0 | Go | v2.4.2 2026-10-26 | 2026-10-26 | Network service; talk to it over its API |
| krr | Request right-sizing from history | MIT | Python | v1.31.1 | 2026-10-19 | shared with FinOps |
| Chaos Mesh (`chaos-mesh/chaos-mesh`) | k8s chaos experiments as CRDs | Apache-2.0 | Go | v2.8.6 2026-10-21 | 2026-10-27 | CNCF Incubating |
| LitmusChaos (`litmuschaos/litmus`) | Chaos hub, workflows, resilience score | Apache-2.0 | Go | 3.33.0 2026-10-15 | 2026-10-26 | CNCF Incubating |
| Sloth (`slok/sloth`) | SLO spec -> Prometheus rules + Grafana dashboards | Apache-2.0 | Go | v0.16.0 2026-04-04 | 2026-10-18 | multiwindow burn-rate alerts |
| Pyrra (`pyrra-dev/pyrra`) | SLO CRD + UI + rules | Apache-2.0 | Go | v0.10.2 2026-09-18 | 2026-10-26 | |
| OpenSLO (`OpenSLO/OpenSLO`) | Vendor-neutral SLO YAML spec | Apache-2.0 | spec (Python tooling) | no GitHub releases; current spec version **UNVERIFIED** | 2026-10-20 | Sloth supports OpenSLO input |

### B.7 Executive reporting

Grafana's reporting (scheduled PDF, email, CSV attachments) is Enterprise and Cloud only; the docs' requirements section is written for Grafana Enterprise and requires SMTP plus the image renderer ([create reports docs](https://grafana.com/docs/grafana/latest/dashboards/create-reports/)). OSS Grafana gets panel PNGs only through the renderer.

| Tool | What it gives us | License | Lang | Latest release | Last push |
|---|---|---|---|---|---|
| grafana-image-renderer (`grafana/grafana-image-renderer`) | Panel/dashboard PNG via headless Chromium; also exposed as `get_panel_image` in mcp-grafana | Apache-2.0 | Go | v5.12.9 2026-10-23 | 2026-10-24 |
| Quarto (`quarto-dev/quarto-cli`) | Markdown + code -> PDF/HTML/DOCX reports | MIT (COPYING.md) | TypeScript/Pandoc | - | 2026-10-26 |
| Jupyter (`jupyter/notebook`, `jupyterlab/jupyterlab`) | Notebook execution; nbconvert to PDF/HTML | BSD-3-Clause | Python/TS | - | 2026-10-20 / 2026-10-27 |
| Evidence Core (`evidence-dev/evidence`) | SQL + Markdown -> static BI site | MIT | TypeScript | @evidence-dev/evidence@40.1.8 2026-02-06 | 2026-10-26 | Company independent (YC S21); "Evidence Core" OSS vs "Evidence Studio" cloud split reported Aug 2026 ([evidence.dev/blog/evidence-core](https://evidence.dev/blog/evidence-core)) |

Simplest reporting path: a plugin that queries Mimir/Loki over HTTP, renders charts with the image renderer, fills a Markdown template, and lets Quarto (or plain WeasyPrint) produce the PDF. Jupyter is overkill for scheduled reports; Evidence is better for a living exec portal than for emailed PDFs.

### B.8 LLM layer

| Component | Facts (2026-10-27) | License |
|---|---|---|
| LiteLLM (`BerriAI/litellm`) | v1.106.4 (2026-10-25); repo now describes a Rust core with Python SDK; proxy with cost tracking, guardrails, load balancing across 100+ providers | MIT core; `enterprise/` directory under separate license |
| OpenRouter | Hosted OpenAI-compatible gateway to many providers; commercial, no self-host; details **UNVERIFIED** in this pass | commercial |
| Ollama (`ollama/ollama`) | Local model runner, 185k stars, pushed 2026-10-27 | MIT |
| vLLM (`vllm-project/vllm`) | High-throughput serving, OpenAI-compatible server, pushed 2026-10-27 | Apache-2.0 |
| MCP (`modelcontextprotocol/servers`) | Reference servers; LICENSE says the project is transitioning MIT -> Apache-2.0, new contributions Apache-2.0 | MIT / Apache-2.0 |
| mcp-grafana (`grafana/mcp-grafana`) | v2.1.1 (2026-10-20), Go, 3.6k stars | Apache-2.0 |

mcp-grafana tools ([README](https://github.com/grafana/mcp-grafana), exact names): search (`search_dashboards`, `search_folders`); dashboards (`get_dashboard_by_uid`, `update_dashboard`, `get_dashboard_panel_queries`, `get_dashboard_summary`, ...); datasources (`list_datasources`, `get_datasource`, `check_datasources_health`, `create_datasource`, `update_datasource`); Prometheus (`query_prometheus`, `list_prometheus_metric_names`, `list_prometheus_label_names/values`, `query_prometheus_histogram`); Loki (`query_loki_logs`, `query_loki_stats`, `query_loki_patterns`, `analyze_loki_labels`, label tools); Pyroscope (`query_pyroscope`, profile type and label tools); Tempo (`search_tempo_traces`, `get_tempo_trace`, `diff_tempo_traces`, `query_tempo_metrics`, attribute tools); alerting (`alerting_rules_read/write`, `alerting_manage_routing`, `alerting_routing_write`, `alerting_silences_read/write`); annotations (`get/create/update/delete_annotation`); rendering (`get_panel_image`); incident (`list/create/get/update_incident`, `add_activity_to_incident`, Grafana Incident = Cloud IRM); OnCall (`list_oncall_schedules`, `get_oncall_shift`, `get_current_oncall_users`, `list_oncall_teams`, `list_oncall_users`, `list_alert_groups`, `get_alert_group`, `update_alert_group`, against the OnCall/IRM API); admin, navigation (`generate_deeplink`), snapshots, provisioning, docs, plugins, `grafana_api_request`; off-by-default extras for SQL, CloudWatch, Cloud Logging, Elasticsearch, Quickwit, Graphite, InfluxDB, Agent Observability (Cloud only) and `ask_assistant` (Cloud). **No Sift tools are exposed.** Transports: stdio (default), sse, streamable-http; per-request headers (per-caller token, org id) only work with streamable-http. Auth: `GRAFANA_SERVICE_ACCOUNT_TOKEN` (API key deprecated), optional `--server-auth-token` for callers, `--dynamic-multi-org`. Per-category `--disable-*` flags and `--disable-write` read-only mode. Needs Grafana >= 9.0.

Implication: our `oncall` provider interface should expose the same verbs mcp-grafana already models (schedules, current on-call, alert groups) so the chat-ops agent can use either backend; and because Sift is absent, "Sift-like" checks are ours to build on top of `query_loki_patterns`, `query_prometheus` and `diff_tempo_traces`.

### B.9 Slack SDK

| SDK | Version (2026-10-27) | License | Notes |
|---|---|---|---|
| Bolt for Python (`slackapi/bolt-python`) | v1.30.0, 2026-07-15 | MIT | `Assistant` class with `thread_started` / `user_message`, `say_stream`, `set_status`, `set_suggested_prompts` |
| Bolt for JavaScript (`slackapi/bolt-js`) | v5.1.0, 2026-09-02 | MIT | `Assistant` class with `threadStarted` / `userMessage`, `sayStream`; v5.1.0 adds a 4 MB request body limit in `HTTPReceiver`/`ExpressReceiver` (security fix) |
| slack-go (`slack-go/slack`) | v0.31.0, 2026-10-26 | BSD-2-Clause | Socket Mode supported; `agents.sessions.*` support tracked in [issue #1606](https://github.com/slack-go/slack/issues/1606) (**UNVERIFIED** whether merged) |

Socket Mode vs Events API ([Slack docs](https://docs.slack.dev/apis/events-api/using-socket-mode/)): Socket Mode opens a WebSocket from the app (no public URL, good behind firewalls, no request signing needed, ack by `envelope_id`), limit of 10 concurrent connections, connections refresh every few hours, and **apps using Socket Mode are not allowed in the public Slack Marketplace** (org-wide Enterprise deployment is fine). Events API needs a public HTTPS endpoint and signature verification. For a self-hosted product the default is Socket Mode; offer Events API for anyone who wants to list in the Marketplace.

Agents and AI apps (2025/2026) ([developing agents](https://docs.slack.dev/ai/developing-ai-apps/), [changelog](https://docs.slack.dev/changelog/)): the Assistant messaging experience (`assistant_view`) is reported deprecated for February 2027 and replaced by the Agent Sessions API: `agents.sessions.setStatus` (statuses processing/active/suspended; must be set back to `active` explicitly) and `agents.sessions.rename` replace `assistant.threads.setStatus` / `setTitle` (kept working through a compatibility bridge). New events: `app_context_changed` (entities the user is viewing), `agent_session_stopped` (native stop button), `agent_session_title_changed`. Streaming: `chat.startStream` -> `chat.appendStream` -> `chat.stopStream` with `markdown_text` and `task_update` chunks; a stream lives at most 10 minutes. `assistant.threads.setSuggestedPrompts` remains. Requires the `assistant:write` scope and the agent feature in the app manifest. Some AI features need a paid workspace plan. Changelog date (~2026-08-20) is from a search summary, **UNVERIFIED** exact date.

### B.10 Build vs reuse per pillar, with first MVP

| Pillar | Reuse (as plugins or data sources) | Build ourselves | First MVP (smallest thing that proves the pillar) |
|---|---|---|---|
| Telemetry base | Alloy (Apache-2.0) as the only host agent; Mimir/Loki/Tempo/Pyroscope (AGPL-3.0, run as separate services) | Tenant-aware Alloy config generator; multi-tenant gateway | One Alloy config that ships host metrics, logs, k8s metrics and eBPF profiles to a hosted LGTM stack with `X-Scope-OrgID` |
| On-call | GoAlert (Apache-2.0) embedded; PagerDuty/Opsgenie/ilert webhook adapters; ntfy for push | `OnCallProvider` interface + adapters; incident timeline/postmortem plugin; native schedule engine only in phase 2 | Alertmanager -> GoAlert integration key; Slack `/oncall who` and ack; contract tests against `goalert/demo` |
| Incident | HolmesGPT (Apache-2.0, CNCF) as RCA engine; mcp-grafana tools; Alertmanager for routing | Sift-like checks (log pattern diff via `query_loki_patterns`, deployment correlation from change pillar, k8s crash/noisy-neighbor checks on Mimir); alert enrichment; incident record | Alert webhook -> enrichment (last deploy, error pattern diff, HolmesGPT summary) posted to Slack thread |
| FinOps | OpenCost (Apache-2.0) for allocation + cloud bills; krr (MIT) for rightsizing; Infracost in CI | Tenant cost rollups, budgets/anomaly alerts, savings ledger; Kubecost only as an optional data source | OpenCost export scraped into Mimir + weekly krr run posting top 10 over-provisioned workloads to Slack |
| SecOps | Falco (Apache-2.0) runtime, Trivy Operator CRDs, Kubescape scans, Prowler for cloud | Finding normaliser (one `Finding` schema), dedup, ticket/Slack routing; avoid Wazuh (copyleft mix, weight) and CloudQuery (paid plugins) | Falco events -> Loki; Trivy VulnerabilityReports -> Mimir metrics + daily Slack digest |
| Compliance | Kubescape frameworks, Prowler packs, Kyverno/Gatekeeper PolicyReports, OpenSCAP for hosts | Control-to-evidence mapping, drift history, report | PolicyReport + Kubescape CIS scan -> compliance score panel + PDF via reporting plugin |
| Change | Argo CD / Flux notification events (both have Grafana annotation providers), Atlantis plan JSON, Renovate PRs | Unified change record, change-to-incident correlation, risk scoring | Argo/Flux events -> Grafana annotations + change feed in Slack; incident plugin reads it |
| Continuous improvement | Pyroscope data, krr, Sloth/Pyrra for SLOs (OpenSLO as input format), Chaos Mesh/Litmus as experiment runners | SLO burn trend review, regression detection on profiles, chaos result ingestion | Sloth-generated SLO rules + weekly "error budget and top regressions" Slack report |
| Executive reporting | grafana-image-renderer (Apache-2.0), Quarto (MIT) or WeasyPrint | Report templates per pillar, scheduler, email delivery (Grafana reporting is Enterprise-only) | Monthly PDF: availability, incidents MTTR, cost trend, open critical findings, emailed and posted to Slack |
| LLM layer | LiteLLM (MIT core) proxy; Ollama/vLLM for self-host; mcp-grafana; MCP SDKs | Agent runtime per pillar with tool allow-lists; prompt/eval harness; per-tenant model routing and budgets | LiteLLM in front of one provider + mcp-grafana + HolmesGPT; one Slack agent that answers "what changed before this alert" |
| Chat-ops | Bolt (Python or JS, MIT) with `Assistant` class, Socket Mode default | Per-agent Slack surface, session status, streaming, feedback capture | Single Slack app, one agent session per incident thread, streaming answers via `chat.startStream` |

---

## License notes that affect the architecture

- AGPL-3.0 components (Grafana, Mimir, Loki, Tempo, Pyroscope, k6, Renovate, Steampipe core, Grafana OnCall) are used as separate network services only; nothing of ours links against them. This is the normal Grafana ecosystem posture and keeps our plugins Apache-2.0-compatible.
- Open-core repos (Keep `ee/`, OneUptime `ee/`, SigNoz `ee/`, LiteLLM `enterprise/`) are safe to depend on only through their permissive core paths; vendor the dependency pin and exclude the EE directories from builds.
- CloudQuery's official cloud plugins are no longer free; Steampipe's plugins are Apache-2.0 and are the better inventory source if we need one.
- Copyleft-heavy security tools (Wazuh GPL-2.0/AGPL-3.0 mix, ScoutSuite GPL-2.0) are optional integrations, never bundled.

## Unverified items

- Exact date GitHub set the archive flag on `grafana/oncall` (README says read-only 2026-03-24; a search summary says the flag date is 2026-06-05).
- Elastic acquisition completion date for Keep (2025-05-21 per a search summary citing Elastic's 10-K) and the claim that Keep is maintained by two people.
- FireHydrant acquisition by Freshworks, Zenduty rebrand to Xurrent IMR, Opsgenie April 2027 sunset (all from third-party comparison pages).
- IBM's Kubecost acquisition date (September 2024 per third-party pages).
- OpenSLO current spec version (repo has no GitHub releases).
- OpenRouter capabilities and model counts (not checked against primary docs).
- Slack changelog date for the Agent Sessions API and the February 2027 `assistant_view` deprecation (from a search summary of the changelog).
- Whether `slack-go/slack` has merged `agents.sessions.*` support.
- GoAlert using PostgreSQL was not re-read from the getting-started doc in this pass (it is stated in the repo's docs and is consistent with the `migrate` and `gadb` packages).

## Sources

GitHub API (read 2026-10-27 via `gh api repos/<owner>/<repo>`, `/releases/latest`, `/contents/LICENSE` and repository metadata): grafana-cold-storage/oncall, appwrite/grafana-oncall, keephq/keep, target/goalert, linkedin/oncall, linkedin/iris, Netflix/dispatch, helldweller/dispatch, arachnys/cabot, prometheus/alertmanager, prymitive/karma, ustream/openduty, openduty/openduty-archive, OneUptime/oneuptime, SigNoz/signoz, louislam/uptime-kuma, binwiederhier/ntfy, gotify/server, HolmesGPT/holmesgpt, robusta-dev/robusta, robusta-dev/krr, k8sgpt-ai/k8sgpt, open-telemetry/opentelemetry-collector, grafana/alloy, opencost/opencost, kubecost/kubecost, FairwindsOps/goldilocks, infracost/infracost, falcosecurity/falco, aquasecurity/trivy-operator, kubescape/kubescape, kubescape/regolibrary, prowler-cloud/prowler, wazuh/wazuh, nccgroup/ScoutSuite, cloudquery/cloudquery, turbot/steampipe, turbot/steampipe-plugin-aws, OpenSCAP/openscap, ComplianceAsCode/content, open-policy-agent/gatekeeper, kyverno/kyverno, bridgecrewio/checkov, argoproj/argo-cd, argoproj/notifications-engine, fluxcd/flux2, fluxcd/notification-controller, keptn/lifecycle-toolkit, akuity/kargo, runatlantis/atlantis, opentofu/opentofu, renovatebot/renovate, grafana/pyroscope, grafana/grafana, grafana/mimir, grafana/loki, grafana/tempo, grafana/beyla, grafana/k6, chaos-mesh/chaos-mesh, litmuschaos/litmus, slok/sloth, pyrra-dev/pyrra, OpenSLO/OpenSLO, grafana/grafana-image-renderer, quarto-dev/quarto-cli, jupyter/notebook, jupyterlab/jupyterlab, evidence-dev/evidence, BerriAI/litellm, ollama/ollama, vllm-project/vllm, modelcontextprotocol/servers, grafana/mcp-grafana, slackapi/bolt-python, slackapi/bolt-js, slack-go/slack, rootlyhq/terraform-provider-rootly.

Web pages:
- https://grafana.com/blog/grafana-oncall-maintenance-mode/
- https://grafana.com/blog/2025/03/11/oncall-management-incident-response-grafana-cloud-irm/
- https://grafana.com/docs/grafana-cloud/alerting-and-irm/irm/set-up/migrate/oncall-oss/
- https://github.com/grafana-cold-storage/oncall
- https://x.com/elastic/status/1920102960807190813
- https://cfotech.com.au/story/elastic-completes-acquisition-of-aiops-company-keep
- https://www.aurorasre.ai/blog/keep-vs-aurora-open-source-aiops
- https://docs.keephq.dev/llms.txt
- https://goalert.me/docs/
- https://github.com/netflix/dispatch and https://github.com/helldweller/dispatch
- https://docs.datadoghq.com/incident_response/on-call/ and https://docs.datadoghq.com/incident_response/on-call/escalation_policies/
- https://signoz.io/changelog/2026-09-09-incidentio-added-as-a-new-alert-channel-p0mdff33y7e5n3qjuq8ac4k0/
- https://oneuptime.com/compare/opsgenie
- https://hyperping.com/blog/incident-io-alternatives and https://betterstack.com/community/comparisons/incident-io-vs-zenduty/
- https://www.cncf.io/projects/ and https://www.cncf.io/sandbox-projects/
- https://www.cncf.io/projects/holmesgpt/ and https://www.cncf.io/blog/2026/01/07/holmesgpt-agentic-troubleshooting-built-for-the-cloud-native-era/
- https://www.cncf.io/projects/keptn/
- https://www.apptio.com/blog/ibm-kubecost-3-0-faster-smarter-and-built-for-scale/
- https://www.cloudquery.io/blog/cloudquery-official-free-plugins-moving-to-paid and https://www.cloudquery.io/blog/announcing-open-core-plugins
- https://grafana.com/whats-new/2026-07-29-assistant-investigations-is-now-generally-available-in-grafana-cloud/
- https://grafana.com/docs/grafana/latest/dashboards/create-reports/
- https://evidence.dev/blog/evidence-core and https://evidence.dev/blog/evidence-studio
- https://github.com/grafana/mcp-grafana
- https://docs.slack.dev/apis/events-api/using-socket-mode/
- https://docs.slack.dev/ai/developing-ai-apps/ and https://docs.slack.dev/changelog/
- https://github.com/slack-go/slack/issues/1606

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
