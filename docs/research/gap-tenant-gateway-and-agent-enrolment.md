# Gap-fill: tenant gateway, agent enrolment and the cross-backend tenant control plane (hosted profile)

Date of research: 2026-10-27. Versions observed: Grafana Alloy v1.21.2 (version pinned by Grafana's own install script), Mimir 3.2.x, Loki 3.7.x, Tempo 3.1.x, Pyroscope 2.4.x, Grafana 13.x, Envoy docs for 1.41.0-dev, OpAMP spec (Beta), OTel `opampsupervisor` (alpha), Elastic Fleet 9.x. All URLs were fetched on 2026-10-27 unless stated. Anything not confirmed from a primary source is marked **UNVERIFIED**.

What this report adds on top of `grafana-stack.md` (§3.5, §12.1, §14.2) and `plugin-architecture.md` (§2.5): the actual enrolment flow and credential model for the one Alloy per customer host, which component terminates auth and injects `X-Scope-OrgID`, how Grafana data sources and admin federation traverse the same gateway, and one control-plane abstraction for per-tenant limits and tenant create/delete across Mimir, Loki, Tempo and Pyroscope. It also resolves the disagreement between the two earlier reports: **the Go kernel is not the proxy** (§3).

---

## 0. Decisions in one page

| # | Decision | Why (evidence in section) |
|---|---|---|
| D1 | **One credential per agent, issued at enrolment: an OAuth2 client (`client_id` = agent id, `client_secret` = per-agent secret).** Every Alloy block that talks to us (`remotecfg`, `prometheus.remote_write`, `loki.write`, `otelcol.auth.oauth2` → `otelcol.exporter.otlphttp`, `pyroscope.write`) has the same `oauth2 { client_id, client_secret_file, token_url, scopes }` block, so Alloy itself fetches and refreshes short-lived JWT access tokens. | §1.1, §1.2 |
| D2 | **The one-line install exchanges a tenant-scoped bootstrap token for that per-agent client**, writes the secret to `/etc/aiops-agent/client_secret` (0600) and a ~15-line `config.alloy` containing only a `remotecfg` block. Everything else is served remotely. This is Grafana Cloud's install model with one change: the token is per-agent and minted by `Enrol`, not a shared per-tenant key pasted by a human. | §1.3 |
| D3 | **Revocation = stop issuing tokens for that client; residual window = access-token TTL (10–15 min).** Rotation of the on-host secret = re-enrol (there is no in-band way for unmodified Alloy to rewrite its own local file). The gateway never needs a revocation list on the hot path. | §1.4 |
| D4 | **Auth terminates in a dedicated stateless gateway, not the kernel.** Hosted profile: Envoy `jwt_authn` with `claim_to_headers` writing `X-Scope-OrgID` from a signed claim, JWKS from a file the control plane publishes; `ext_authz` only for opaque (non-JWT) credentials. Self-hosted single-node profile: an in-process Go reverse proxy plugin with the same contract. Both are implementations of one `gateway` extension point and pass the same conformance suite. | §2, §3 |
| D5 | **Grafana data sources carry `Authorization: Bearer <service JWT>` via `httpHeaderName1/httpHeaderValue1`** (provisioning supports exactly this), not a static `X-Scope-OrgID` header. Admin/cross-tenant data sources get a token whose `orgid` claim is already the pipe-joined list (`a|b|c`) or `*` expanded at mint time; write-scoped tokens never carry a pipe (control-plane invariant). `oauthPassThru` (forward the user's OIDC token) is the phase-2 path for per-user authorization. | §4 |
| D6 | **All four backends consume per-tenant limits as a hot-reloaded YAML file** (`overrides:` keyed by tenant in Mimir/Loki/Pyroscope runtime-config, `per_tenant_override_config` in Tempo; all reload every 10 s by default). The `tenant` plugin renders one `TenantLimits` document into four files/ConfigMaps. Tempo additionally exposes `/api/overrides` (object-store backed, ETag-versioned) for the metrics-generator subset. | §5.1, §5.2 |
| D7 | **Tenant deletion is a multi-step saga, not an API call**: only Mimir has a tenant-delete API; Loki, Tempo and Pyroscope are driven through retention overrides plus object-storage prefix sweeps, and Pyroscope v2 level-0 segments are multi-tenant objects that cannot be erased per tenant until compaction. | §5.3 |

---

## 1. (a) Credential model and the enrolment flow

### 1.1 The agent's credential surface (what unmodified Alloy can do)

All five blocks that talk to our side share the same HTTP-client auth shape (Prometheus `common/config` lineage), verified on each component page:

| Block | Auth options (mutually exclusive) | Client TLS (mTLS) | Extra headers | Behaviour on 4xx |
|---|---|---|---|---|
| `remotecfg` | `basic_auth`, `authorization{type,credentials(_file)}`, `bearer_token(_file)`, `oauth2{client_id, client_secret(_file), token_url, scopes, endpoint_params}` | `tls_config{ca_*, cert_*, key_*, server_name, min_version}`; "for client auth both a cert and a key must be supplied" | `http_headers` | 401/403/404/415 listed as troubleshooting; service keeps polling and keeps the cached config |
| `prometheus.remote_write` → `endpoint` | `basic_auth`, `authorization`, `bearer_token(_file)`, `oauth2`, `sigv4`, `azuread`, `google_iam` | same `tls_config` | `headers` (docs example sets `"X-Scope-OrgID" = "staging"`), `http_headers` | 5xx retried; 429 retried if `retry_on_http_429` (default true, honours `Retry-After`); **"Other HTTP 4xx status code responses are never considered recoverable errors"** → samples dropped (`prometheus_remote_storage_samples_failed_total`); WAL keeps unsent data at most `max_keepalive_time` (8h) |
| `loki.write` → `endpoint` | `basic_auth`, `authorization`, `bearer_token(_file)`, `oauth2` | same | `tenant_id` (sets `X-Scope-OrgID`), `headers`, `http_headers` | same 4xx rule; drops counted in `loki_write_dropped_entries_total`; WAL is experimental |
| `otelcol.exporter.otlp` / `otlphttp` → `client` | `auth = otelcol.auth.<basic|bearer|oauth2|headers|sigv4|google>.<name>.handler` | `tls{ca_*, cert_*, key_*, insecure, server_name, reload_interval}` | `headers` | `retry_on_failure` (5 min `max_elapsed_time`), `sending_queue` (1000 requests default, optional `storage`) |
| `pyroscope.write` → `endpoint` | `basic_auth`, `authorization`, `bearer_token(_file)`, `oauth2` | same `tls_config` | `headers`, `http_headers` | 408 and (optionally) 429 retried; other 4xx not |

Consequences that drive the design:

1. **A credential outage is a data-loss event at the agent, not a retry.** 401/403 are terminal for remote_write, loki.write and pyroscope.write. Therefore credential rotation must overlap (old and new valid at once) and the gateway's availability is the ingestion SLO. This is the strongest argument for keeping the gateway tiny, stateless and independent of the kernel (§3).
2. **Alloy already implements the OAuth2 client-credentials grant** in every block above. Prometheus' `<oauth2>` doc: "OAuth 2.0 authentication using the client credentials or password grant type"; `grant_type` defaults to `client_credentials`; `client_secret_file` is supported. Alloy's `oauth2` block mirrors those fields (`client_id`, `client_secret`, `client_secret_file`, `token_url`, `scopes`, `endpoint_params`, own `tls_config`). The OTel side is `otelcol.auth.oauth2` (listed on Alloy's `otelcol` index page; its field names were not fetched in this pass, **UNVERIFIED** detail).
3. **The served (remote) configuration may reference host files and env vars.** Grafana Cloud's own template references `sys.env("GCLOUD_RW_API_KEY")` inside the write blocks, and the install script comment says remote self-monitoring pipelines depend on `GCLOUD_FM_COLLECTOR_ID` from the same env file. So the remote config can say `client_secret_file = "/etc/aiops-agent/client_secret"` without the secret ever leaving the host or transiting the fleet API. Whether Prometheus re-reads `client_secret_file` at every token refresh (which would make in-place secret rotation possible without restart) is **UNVERIFIED**; `basic_auth.password_file` is documented as re-read on each request.
4. **mTLS is available on every path** (`cert_file`/`key_file`), but nothing in Alloy can enrol a certificate: `remotecfg` has no CSR step, and the OpAMP "Agent-initiated CSR Flow" is `Status: [Development]` in the spec and has no row in the OTel supervisor's status table. mTLS is therefore a hardening profile (issue certs out-of-band with SPIRE or our CA; SPIRE node attestation supports join tokens that "expire immediately after use", AWS IID, Azure MSI, GCE, Kubernetes SA tokens, TPM) and not the default.

Sources: https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/ ; https://grafana.com/docs/alloy/latest/reference/components/prometheus/prometheus.remote_write/ ; https://grafana.com/docs/alloy/latest/reference/components/loki/loki.write/ ; https://grafana.com/docs/alloy/latest/reference/components/otelcol/otelcol.exporter.otlp/ ; https://grafana.com/docs/alloy/latest/reference/components/pyroscope/pyroscope.write/ ; https://prometheus.io/docs/prometheus/latest/configuration/configuration/ (`<oauth2>`) ; https://raw.githubusercontent.com/open-telemetry/opamp-spec/main/specification.md ; https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/cmd/opampsupervisor/README.md ; https://spiffe.io/docs/latest/spire-about/spire-concepts/

### 1.2 Reference models compared

| Model | Bootstrap credential | Per-agent credential | Data-path credential | Revocation granularity | Source |
|---|---|---|---|---|---|
| **Grafana Cloud (Alloy + Fleet Management)** | none: a human copies an access-policy token (`metrics:write`, `logs:write`, `traces:write`, `profiles:write`, `fleet-management:read/write` scopes; optional expiry; `org` or `stack` realm; optional IP ranges) | none: same token on every host | same token, basic auth `username = <hosted-id>`, `password = sys.env("GCLOUD_RW_API_KEY")` for `remotecfg`, `prometheus.remote_write`, `loki.write` (verbatim in `config-fm.alloy`) | whole policy; revoking kills every host using it | install script + template + access-policies doc |
| **Datadog** | `DD_API_KEY` (per org, long-lived) | none | same key | per API key (org-wide) | `datadog-agent.md` §2.12, §3 |
| **Elastic Fleet** | enrollment token = ES API key "tied to an agent policy", reusable "as many times as needed", optional expiry since 9.3 | "After the initial connection request… Fleet Server passes a communication API key to the agent" (check-ins only) | separate **output API key** "with the minimal permissions needed to ingest all the data specified by the agent policy" | per agent; revoking the enrollment token does not affect enrolled agents | Elastic Fleet docs |
| **OpAMP spec** | "Registration On First Use": agents ship with shared initial credentials; server "will immediately generate and offer new connection settings"; "Trust On First Use" for cert-less clients | `ConnectionSettingsOffers.opamp.headers` / `certificate` (Beta); CSR flow (Development) | `own_metrics/own_traces/own_logs` + `other_connections` offers | per agent; "Revoking Access" marks settings revoked and the server "MUST also communicate the revocation fact to the corresponding destinations" | OpAMP spec |
| **GEM / GEL (Grafana Enterprise)** | admin token | n/a (tokens belong to access policies) | token as basic-auth password (`-u "Tenant1:$API_TOKEN"`, or `Tenant1\|Tenant2:$API_TOKEN` for federation), or bearer `tenantID:JWT` for OIDC | per token (`PUT /tokens/{name}` `{"status":"inactive"}` → 401) | GEM admin API, GEM OAuth, Loki HTTP API |

What we take: Elastic's **bootstrap → per-agent exchange** (the only model where one compromised host does not force a fleet-wide rotation), GEM's **tenant/access-policy/token** resource model as the shape of our `tenant` API, and Grafana Cloud's **install mechanics** (package from GitHub Releases with embedded sha256 sums, secret in a `chmod 600` systemd drop-in `/etc/systemd/system/alloy.service.d/env.conf`, config at `/etc/alloy/config.alloy`, `id = $(uname -n)`).

Sources: https://storage.googleapis.com/cloud-onboarding/alloy/scripts/install-linux.sh ; https://storage.googleapis.com/cloud-onboarding/alloy/config/config-fm.alloy ; https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/ ; https://www.elastic.co/guide/en/fleet/current/fleet-enrollment-tokens.html ; https://grafana.com/docs/enterprise-metrics/latest/admin-api/ ; https://grafana.com/docs/enterprise-metrics/latest/manage/oauth/ ; https://grafana.com/docs/loki/latest/reference/loki-http-api/

### 1.3 The enrolment flow (hosted profile)

```
operator (UI/CLI)                 install script on host                  fleet plugin            gateway       LGTM
   |  1. create BootstrapToken       |                                         |                      |            |
   |  {tenant, ttl=24h, max_uses,    |                                         |                      |            |
   |   attrs:{env:prod}}             |                                         |                      |            |
   |-------------------------------->|  2. curl -fsSL .../install.sh | sh -s -- --token <bt>          |            |
   |                                 |  3. POST /v1/enrol {bt, hostname, os, arch, attrs}              |            |
   |                                 |--------------------------------------->|  verify bt, create     |            |
   |                                 |  {agent_id, client_id, client_secret,  |  Agent{tenant,id},     |            |
   |                                 |   token_url, remotecfg_url, pkg_sha}   |  hash secret           |            |
   |                                 |<---------------------------------------|                        |            |
   |                                 |  4. install alloy pkg (sha256 check)   |                        |            |
   |                                 |  5. write /etc/aiops-agent/client_secret (0600)                 |            |
   |                                 |     write /etc/alloy/config.alloy = remotecfg{} only            |            |
   |                                 |  6. systemctl enable --now alloy       |                        |            |
   |                                 |  7. RegisterCollector{id, name, local_attributes} (oauth2 token)|            |
   |                                 |  8. GetConfig{id, local_attributes, hash, remote_config_status, |            |
   |                                 |     effective_config} every poll_frequency ------------------->|            |
   |                                 |     <- {content, hash | not_modified}  |                        |            |
   |                                 |  9. each write block: POST token_url (client_credentials) ----->|            |
   |                                 |     -> JWT{sub=agent, tenant, scope=write, exp=+15m}            |            |
   |                                 | 10. remote_write/loki/otlphttp/pyroscope.write  Authorization: Bearer JWT ->|  X-Scope-OrgID: <tenant> -->
```

The local `config.alloy` written in step 5 is the whole customer-visible surface:

```alloy
remotecfg {
  url            = "https://fleet.<region>.example/api/v1/alloy"
  id             = "<agent_id>"            // issued by Enrol; host-stable, not the hostname
  name           = constants.hostname
  poll_frequency = "1m"
  attributes     = { "env" = "prod" }      // reserved "collector.*" keys are auto-added
  oauth2 {
    client_id          = "<client_id>"
    client_secret_file = "/etc/aiops-agent/client_secret"
    token_url          = "https://auth.<region>.example/oauth2/token"
    scopes             = ["fleet"]
  }
}
```

The served configuration (one file assembled per agent from matching pipelines, cached by Alloy at `<storage.path>/remotecfg/<argsHash>` and reloaded from cache only at startup/Update, never during periodic polls) contains the four write blocks, each with the identical `oauth2` block and `scopes = ["metrics:write"]` etc. Key facts from the Alloy source that shape the fleet server contract:

- RPCs are `collector.v1.CollectorService/{GetConfig, RegisterCollector, UnregisterCollector}` (Connect-RPC, Apache-2.0). `GetConfig` carries `id`, `local_attributes`, `hash`, `remote_config_status {UNSET|APPLIED|APPLYING|FAILED, error_message}` and `effective_config` (only when changed since last send); the response is `{content, hash, not_modified}`. `attributes` (field 2) is deprecated for `local_attributes` (field 4).
- `RegisterCollector` is called at `Run` and again on every `Update` that changes args; failure at startup is fatal unless the URL is empty. `UnregisterCollector` runs at shutdown with a 1 s timeout; failures are logged only.
- A remote config that fails to load is reported as `FAILED` with diagnostics, and Alloy reloads the previous cached config "Otherwise a partial working config will be left in the controller". Grafana's architecture doc confirms: "Alloy finds and falls back to the last known valid effective configuration", and local/remote configs run in isolated controllers ("Local and remote configurations cannot be in conflict").
- The `remotecfg` service is `featuregate.StabilityGenerallyAvailable` in source (the docs page prints no stability label).

Sources: https://raw.githubusercontent.com/grafana/alloy-remote-config/main/api/collector/v1/collector.proto ; https://raw.githubusercontent.com/grafana/alloy/main/internal/service/remotecfg/remotecfg.go ; https://raw.githubusercontent.com/grafana/alloy/main/internal/service/remotecfg/config_manager.go ; https://grafana.com/docs/grafana-cloud/send-data/fleet-management/introduction/architecture/

Kubernetes variant: the same `Enrol` call is made once by a Helm pre-install Job (or by the operator) per cluster, producing a Secret consumed by every Alloy pod; `id` = cluster name + pod name, `attributes.cluster = <name>`. The k8s-monitoring chart's `destinations` already accept `basicAuth`/`bearerToken`/`tls` per destination (`grafana-stack.md` §3.8), so the agent config is generated the same way.

### 1.4 Issuance, rotation, revocation

| Credential | Lifetime | Where stored | Rotation | Revocation | Blast radius |
|---|---|---|---|---|---|
| Bootstrap token | short (hours–days), optional `max_uses`; semantics copied from Elastic (reusable until revoked; revoking "doesn't delete it immediately") | control-plane DB (hash) | create a new one | `DELETE /v1/bootstrap-tokens/{id}`; enrolled agents unaffected | new enrolments only |
| Per-agent OAuth2 client secret | long-lived (no expiry) | host file 0600 + DB hash (Argon2id) | re-enrol (`aiops-agent re-enrol --token <bt>` rewrites the file and restarts Alloy); optional in-place rotation requires the `client_secret_file` re-read behaviour (**UNVERIFIED**) | set `Agent.status = revoked` → `token_url` returns `invalid_client`; existing JWTs die at `exp` | one host |
| Access token (JWT) | 10–15 min; claims `{iss, sub: agent_id, tenant, orgid, scope, aud: "gateway", exp, jti}` | memory in each Alloy component (each `oauth2` block refreshes independently: 5 token requests per agent per TTL ≈ 10 000 agents → ~56 req/s at 15 min, trivial) | automatic | not needed on the hot path; gateway rejects by `exp`; emergency: rotate the signing key (JWKS) | ≤ TTL |
| Service token (Grafana data sources, CI, mimirtool) | long-lived JWT or opaque | Grafana `secureJsonData`, CI secrets | re-mint and re-provision | opaque tokens: `ext_authz` lookup + cache; JWTs: `jti` denylist checked only on query routes (cheap) | one integration |
| Gateway signing key | rotate monthly; JWKS keeps old + new | control plane publishes JWKS as a file/ConfigMap and at `/.well-known/jwks.json` | overlap window ≥ max access-token TTL | remove key from JWKS | fleet-wide (planned) |

Why JWT and not an opaque token the gateway looks up: the gateway must stay up and fast through control-plane deploys (§1.1 point 1). With JWTs the gateway needs only the public keys, which can be delivered as a file; Envoy's `jwt_authn` caches validated tokens (`jwt_cache_config.jwt_cache_size`, default 100, raise to the agent count) so steady-state cost is a hash lookup, and `remote_jwks.cache_duration` defaults to 10 min with `async_fetch` keeping the listener serving. Whether Envoy retains a stale JWKS after a failed refetch was not confirmed (**UNVERIFIED**); using `local_jwks` from a mounted file sidesteps the question.

Sources: https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/jwt_authn/v3/config.proto

---

## 2. (b) Who terminates auth and injects `X-Scope-OrgID`

### 2.1 What the backends and vendor charts actually do

- Mimir: "you must add a layer of protection such as a reverse proxy that authenticates requests and injects the appropriate tenant ID into the `X-Scope-OrgID` header"; Loki: "authorization is not part of the Loki API"; Pyroscope: "doesn't include an authentication layer… The authenticating reverse proxy handles this responsibility"; Tempo `multitenancy_enabled: true` "requires X-Scope-OrgID header on all requests".
- `mimir-distributed` chart ships an NGINX "gateway" (`nginxinc/nginx-unprivileged:1.29-alpine`, `clientMaxBodySize: 540M`, `proxy_read_timeout 300`). It does **not** derive the tenant from credentials: it forwards whatever `X-Scope-OrgID` the client sent, substitutes `mimir.noAuthTenant` when the header is empty, and rejects multiple headers with `400 'Sending multiple X-Scope-OrgID headers is not allowed. Use a single header with | as separator instead.'`. Optional `gateway.nginx.basicAuth` is a single htpasswd user, i.e. one shared password, not tenancy.
- `loki` chart gateway (same image): `locationSnippet: {{ if .Values.loki.tenants }}proxy_set_header X-Scope-OrgID $remote_user;{{ end }}` — tenant = basic-auth username from an htpasswd generated from `loki.tenants[]`. Static, per-tenant password, no per-agent identity; the rendered nginx file sets no `X-Scope-OrgID` otherwise.
- The vendor's real multi-tenant gateway is proprietary: GEM/GEL `auth.type: enterprise` validates tokens against access policies (tenant realms, scopes, label policies) and maps basic-auth user `tenantA|tenantB` or bearer `tenant:JWT` to the header; the OSS `trust` mode is "accept the header as sent" (**the meaning of `trust` is inferred from the GEM config reference naming, UNVERIFIED** as a quoted definition). GEM also keeps a 10 min auth cache (`auth.cache.ttl.duration`).

Sources: https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/ ; https://raw.githubusercontent.com/grafana/mimir/main/operations/helm/charts/mimir-distributed/values.yaml ; https://raw.githubusercontent.com/grafana/loki/main/production/helm/loki/values.yaml ; https://raw.githubusercontent.com/grafana/loki/main/production/helm/loki/templates/_helpers.tpl ; https://grafana.com/docs/enterprise-metrics/latest/config/reference/ ; https://grafana.com/docs/enterprise-metrics/latest/manage/oauth/

### 2.2 Candidate gateways

| Option | Auth mechanism | Header injection | Decision cache | gRPC/OTLP | Hot-path dependency | Verdict |
|---|---|---|---|---|---|---|
| **Envoy `jwt_authn`** | verifies JWT signature/`exp`/`aud`/`iss` in-proxy; `jwt_cache_config` caches valid tokens | `claim_to_headers {header_name: X-Scope-OrgID, claim_name: orgid}` — the header "is removed from incoming requests for every configured provider… any other value will be overwritten" (built-in header hygiene); `forward: false` strips the JWT before upstream | JWT cache (per worker), JWKS cache 10 min | native HTTP/2 | none at request time (keys from file or cached JWKS) | **Hosted default** |
| **Envoy `ext_authz`** | gRPC `CheckRequest` to our authz service; `with_request_body` optional (off) | `OkHttpResponse.headers` ("either add a new header, or override an existing one") and `headers_to_remove`; HTTP mode via `allowed_upstream_headers` | **none built in** (page lists no decision cache); one RPC per request, default timeout 200 ms; `failure_mode_allow` default false → authz outage = 403 = data loss | native | authz service on every request | only for opaque tokens; put a cache in the authz service |
| **NGINX `auth_request`** | subrequest to authz; "If the subrequest returns a 2xx response code, the access is allowed… 401 or 403… denied"; body not sent (`proxy_pass_request_body off`) | `auth_request_set $tenant $upstream_http_x_scope_orgid;` + `proxy_set_header X-Scope-OrgID $tenant;` (and `grpc_set_header` for gRPC) | cacheable since 1.7.3 via `proxy_cache` keyed on the credential | `grpc_pass` requires `http2 on`; `grpc_ssl_verify off` by default | authz service on cache miss; OSS NGINX has no JWT validation (`auth_jwt` is Plus) | viable self-host option for people who already run NGINX; more moving parts |
| **Traefik `forwardAuth`** | "If the service answers with a 2XX code, access is granted"; `forwardBody` default false | `authResponseHeaders: [X-Scope-OrgID]` copies from the auth response, "overriding any existing headers with the same name" | "no caching" documented | yes (h2c) | authz service on **every** request | not for the ingest path |
| **Kernel as proxy (Go `httputil.ReverseProxy` + JWT)** | any | trivial | in-process | needs h2c handling | kernel becomes the ingest SPOF; every kernel deploy/restart = 4xx/5xx window; scales with bandwidth not control-plane load | **single-node self-hosted profile only** |

Throughput and HA consequences, concretely:

- Ingest is many small HTTP/1.1 POSTs (remote-write batches, Loki pushes, OTLP/HTTP, Pyroscope pushes). Choosing **OTLP/HTTP (`otelcol.exporter.otlphttp`) rather than gRPC for traces** makes all four write paths plain HTTP/1.1 POSTs, so any of the proxies above (including NGINX without `grpc_pass`) works and the hosted gateway needs no HTTP/2 upstream handling. Tempo and Mimir both accept OTLP/HTTP (`grafana-stack.md` §12.2).
- Body limits must be aligned end-to-end: Mimir `-distributor.max-recv-msg-size` 100 MB (`104857600`) and `-server.grpc-max-recv-msg-size-bytes` 100 MB; the vendor NGINX uses `client_max_body_size 540M` for Mimir and `4M` for Loki. The gateway must not buffer bodies for auth (`with_request_body` off, `forwardBody` off, `proxy_pass_request_body off`).
- Auth must not require a network round trip per request: JWT verification or a decision cache. With `jwt_authn` the per-request work is a cache lookup; a cold verify is one RSA/ECDSA signature check.
- The gateway is stateless; scale horizontally behind the LB; `failure_mode_allow` stays false (fail closed) because tenancy, not availability, is the invariant.
- Backend `X-Scope-OrgID` hygiene: strip any client-supplied value (Envoy `claim_to_headers` reserves the header; NGINX: explicit `proxy_set_header`), and reject multiple headers (mimir-distributed's `400` rule is the model).

Sources: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter ; https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ext_authz/v3/ext_authz.proto ; https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/auth/v3/external_auth.proto ; https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/jwt_authn/v3/config.proto ; https://nginx.org/en/docs/http/ngx_http_auth_request_module.html ; https://nginx.org/en/docs/http/ngx_http_grpc_module.html ; https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/forwardauth/ ; https://raw.githubusercontent.com/grafana/mimir/main/cmd/mimir/help-all.txt.tmpl

### 2.3 Route table the gateway enforces (both implementations)

| Route (external) | Upstream | Token `scope` required | Pipe (`a\|b`) allowed in `orgid` | Notes |
|---|---|---|---|---|
| `POST /api/v1/push`, `POST /otlp/v1/metrics` | Mimir distributor | `metrics:write` | no | |
| `POST /loki/api/v1/push`, `POST /otlp/v1/logs` | Loki distributor | `logs:write` | no (Loki returns 400 anyway) | |
| `POST /v1/traces` (OTLP/HTTP), optional gRPC 4317 | Tempo distributor | `traces:write` | no | |
| `POST /push.v1.PusherService/Push`, `POST /ingest` | Pyroscope distributor | `profiles:write` | no | |
| `/prometheus/api/v1/*`, `/loki/api/v1/{query,query_range,labels,series,…}`, `/api/search`, `/api/traces/*`, `/querier.v1.QuerierService/*` | query-frontends | `*:read` | yes (admin tokens) | `__tenant_id__` appears in Mimir/Loki results |
| `/prometheus/config/v1/rules*`, `/api/v1/alerts`, `/alertmanager/*` | Mimir ruler / AM | `rules:*`, `alerts:*` | no (ruler federation uses `source_tenants`, not the header) | |
| `/loki/api/v1/delete`, `/compactor/delete_tenant*`, `/api/overrides` | compactors / query-frontend | `admin` | no | internal-only unless `admin` |
| `/api/v1/alloy/collector.v1.CollectorService/*` | fleet plugin | `fleet` | no | Connect-RPC |
| `/oauth2/token`, `/.well-known/jwks.json`, `/v1/enrol` | auth/fleet plugins | none (client auth in body) | – | rate-limited per client_id |

---

## 3. Resolving "is the kernel the proxy?"

`plugin-architecture.md` §2.5 says "The kernel is the authenticating proxy that injects the header"; `grafana-stack.md` §14.2 says a "tenant gateway" is mandatory. Both are right at different profiles, and the C2 boundary should be drawn so that the **gateway is a separately deployable plugin that the kernel configures but does not sit in front of**:

```
            customer host                      our edge                        our data plane
┌───────────────────────────┐   TLS    ┌────────────────────────┐          ┌──────────────────────────┐
│ Alloy (one per host)      │ ───────▶ │ tenant-gateway         │ ───────▶ │ Mimir / Loki / Tempo /    │
│  remotecfg{oauth2}        │  Bearer  │  (Envoy: jwt_authn +   │ X-Scope- │ Pyroscope (multitenancy   │
│  remote_write/loki.write/ │   JWT    │   claim_to_headers)    │  OrgID   │  on, no auth)             │
│  otlphttp/pyroscope.write │          │  stateless, N replicas │          └──────────────────────────┘
└───────────────────────────┘          └───────────┬────────────┘
        ▲ GetConfig/Register                       │ JWKS file / xDS from kernel   ▲ overrides files (ConfigMaps)
        │ (also via gateway, scope=fleet)          ▼                              │
                                        ┌────────────────────────┐                │
                                        │ aiops kernel (Go)      │────────────────┘
                                        │  plugins: fleet, auth, │
                                        │  tenant, gateway.envoy │
                                        │  (renders xDS/JWKS)    │
                                        └────────────────────────┘
```

- Hosted profile: `gateway.envoy` plugin renders Envoy config (routes above, JWKS file, per-route `JwtRequirement`) and publishes it; Envoy pods have no runtime dependency on the kernel. Kernel outages stop enrolment, config changes and token refresh — agents keep running their cached config and keep writing until their access tokens expire (≤ 15 min), which bounds the kernel's availability requirement for ingest: **token refresh (`/oauth2/token`) must be served by a minimal, separately scaled `auth` plugin process**, not by the full kernel. This is the one control-plane endpoint on the ingest SLO.
- Self-hosted single-node profile: `gateway.inproc` is a Go reverse proxy inside the one binary, same route table, same JWT model (the `auth` plugin is in-process). Conformance suite (`plugin-architecture.md` §4.7): `conformance/gateway/` runs the identical black-box tests (reject missing token, reject pipe on write routes, strip client `X-Scope-OrgID`, 413 on oversize body, pass `Retry-After`) against both implementations.

---

## 4. (c) Grafana data sources and cross-tenant admin queries through the same gateway

### 4.1 Mechanisms Grafana supports (Grafana 13 docs)

| Mechanism | Provisioning keys | Works with our gateway? | Notes |
|---|---|---|---|
| Static tenant header | `jsonData.httpHeaderName1: X-Scope-OrgID`, `secureJsonData.httpHeaderValue1: <tenant>` | **No** (the gateway strips it) | only valid when the gateway runs in trust mode, i.e. self-hosted single-tenant |
| Basic auth | `basicAuth: true`, `basicAuthUser`, `secureJsonData.basicAuthPassword` | yes if the gateway parses Basic (GEM style: user = `tenant` or `a\|b`, password = token) | needs an `ext_authz`/lookup path because `jwt_authn` only extracts from `Authorization: Bearer` / query / cookie |
| **Custom `Authorization` header** | `jsonData.httpHeaderName1: Authorization`, `secureJsonData.httpHeaderValue1: 'Bearer <service JWT>'` (the provisioning doc's own example uses exactly `httpHeaderName2: 'Authorization'` / `httpHeaderValue2: 'Bearer XXXXXXXXX'`) | **yes, same `jwt_authn` path as agents** | chosen default (D5) |
| Forward OAuth identity | `jsonData.oauthPassThru: true` (key confirmed in Grafana community thread by a Grafana engineer and in third-party plugin docs; the Prometheus data-source page documents the UI option "Forward OAuth identity… forwards the OAuth access token (and also the OIDC ID token if available)" but not the key — **key name partially UNVERIFIED from first-party docs**) | yes, when the gateway trusts our IdP as a second `jwt_authn` provider and maps `sub`/`groups` → tenant set | per-user authorization; tokens expire with the session; external backend plugins must set `ForwardHTTPHeaders` (OpenTSDB issue #88) |
| TLS client auth | `jsonData.tlsAuth`, `tlsAuthWithCACert`, `secureJsonData.tlsClientCert/tlsClientKey/tlsCACert` | yes (mTLS profile) | one cert per data source |
| `X-Grafana-User` | `[dataproxy] send_user_header = true` adds `X-Grafana-User` (default false) | informational only | `X-Grafana-Org-Id` forwarding to data sources was **not found** in the config reference (**UNVERIFIED**) |
| Team LBAC | per-team label selectors on Loki/Prometheus data sources | Enterprise/Cloud only ("requires GEM/GEL"); "Cross-tenant querying isn't supported" | out of scope; our gateway can add GEM-style `label_policies` later |

Sources: https://grafana.com/docs/grafana/latest/administration/provisioning/ ; https://grafana.com/docs/grafana/latest/datasources/prometheus/configure/ ; https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/ ; https://grafana.com/docs/grafana/latest/administration/data-source-management/teamlbac/ ; https://community.grafana.com/t/provision-datasource-with-forward-oauth-identity/16818 ; https://github.com/grafana/grafana-opentsdb-datasource/issues/88

### 4.2 Design

- One Grafana org per tenant (`plugin-architecture.md` §2.5). The `grafana` plugin provisions, per org, four data sources (`prometheusType: Mimir`, Loki, Tempo, Pyroscope) pointing at the gateway with `Authorization: Bearer <service JWT{tenant, orgid: tenant, scope: [metrics:read, logs:read, traces:read, profiles:read, rules:*, alerts:*]}>`. Grafana-managed alert rules and the Alertmanager data source traverse the same routes.
- **Admin / cross-tenant org**: data sources carry a service JWT whose `orgid` claim is precomputed by the control plane as `t1|t2|…` (or the token's full allowed set when the operator asks for `*`, GEM semantics: "`*` username combined with a policy limited to specific tenants still only reaches those tenants"). The gateway copies the claim verbatim; backend prerequisites are enabled only on the hosted read path:
  - Mimir: `-tenant-federation.enabled=true` on all services (default **off**), optional `-tenant-federation.max-tenants` (0 = unlimited), `-tenant-federation.max-concurrent` 16 (experimental); rulers use `-ruler.tenant-federation.enabled` + `source_tenants` in the rule group, not the header.
  - Loki: `querier.multi_tenant_queries_enabled: true` (default false); "Only query endpoints support multi-tenant calls", push/tail return 400.
  - Tempo: `query_frontend.multi_tenant_queries_enabled` default **true** (no-op unless `multitenancy_enabled`).
  - Pyroscope: **no pipe-separated federation found** in docs or config reference (searched 2026-10-27). Cross-tenant profiling for admins = N per-tenant data sources or a fan-out in our own `datasource.pyroscope` plugin. Treat as absent (**UNVERIFIED as a definitive negative**).
- Invariant enforced at mint time, so the gateway needs no regex: a token with any `*:write` scope has a single-tenant `orgid`; the write routes additionally require the `*:write` scope via per-route `JwtRequirement`.
- Phase 2 (`oauthPassThru`): the gateway adds our IdP as a second JWT provider on query routes only; `orgid` is then resolved from a user→tenants mapping. This needs an `ext_authz` hop (with cache) or a precomputed `tenants` claim issued by our IdP; either way it stays off the write path.

Sources: https://raw.githubusercontent.com/grafana/mimir/main/cmd/mimir/help-all.txt.tmpl ; https://grafana.com/docs/loki/latest/configure/ (querier block) ; https://grafana.com/docs/tempo/latest/configuration/ ; https://grafana.com/docs/pyroscope/latest/configure-server/reference-configuration-parameters/ ; https://grafana.com/docs/enterprise-metrics/latest/manage/tenant-management/tenant-federation/

---

## 5. (d) One control-plane abstraction for limits and tenant lifecycle across four backends

### 5.1 How each backend takes per-tenant limits (verified)

| | Mimir 3.2 | Loki 3.7 | Tempo 3.1 | Pyroscope 2.4 |
|---|---|---|---|---|
| File mechanism | `-runtime-config.file` = "Comma separated list of yaml files or URLs… merged from left to right"; per-file suffixes `;optional-on-startup`, `;optional-keep-last-value-on-failure` | `runtime_config.file` = "Comma separated list of yaml files… merged from left to right"; (`limits_config.per_tenant_override_config` is the deprecated alias) | `overrides.per_tenant_override_config: <path>` (single file; `""` disables) | `runtime_config.file` = "Comma separated list of yaml files or URLs… merged from left to right" (dskit, same as Mimir) |
| Reload period | `-runtime-config.reload-period` 10 s | `runtime_config.period` 10 s | `per_tenant_override_period` 10 s | `runtime_config.period` 10 s |
| YAML shape | `overrides: {<tenant>: {<any limits key>}}` plus `ingester_limits`, `distributor_limits` | `overrides: {<tenant>: {<any limits_config key>}}` (incl. `retention_period`, `retention_stream`, `deletion_mode`, `otlp_config`) | `overrides: {"<tenant>": {ingestion:{…}, global:{…}, compaction:{…}, metrics_generator:{…}}, "*": {…}}` — **not merged field-by-field with `defaults`**: "most fields must be fully specified in the tenant block" | `overrides: {<tenant>: {<limits key>}}` — the Helm chart renders `pyroscope.tenantOverrides` under a top-level `overrides:` key in `<release>-overrides-config/overrides.yaml` (verified); the exact mount/flag in the chart templates is **UNVERIFIED** |
| Per-tenant retention key | `compactor_blocks_retention_period` (flag `-compactor.blocks-retention-period`, "0 to disable") | `retention_period` (`0s` = keep forever; no 24 h minimum, unlike `retention_stream.period`), needs `compactor.retention_enabled: true` + `delete_request_store` | `compaction.block_retention` (0 → compactor default `block_retention` 336 h); `compaction_disabled` switches off compaction *and* retention for the tenant | `limits.retention_period` default 31 d (v2); `compactor_blocks_retention_period` is "[v1 storage only]"; metastore `index.cleanup_grace_period` 6 h ("Blocks are only deleted if all data in the block has passed the retention period") |
| Other per-tenant knobs we must render | `ingestion_rate` (10 000 samples/s default), `ingestion_burst_size` 200 000, `max_global_series_per_user` 150 000, `ingestion_tenant_shard_size` (0 = no shuffle sharding), `max_fetched_series_per_query`, `max_total_query_length` | `ingestion_rate_mb` 4, `ingestion_burst_size_mb` 6, `max_global_streams_per_user` 5000, `otlp_config` (index labels vs structured metadata), `deletion_mode` | `ingestion.rate_limit_bytes` 30 MB, `burst_size_bytes` 30 MB, `max_traces_per_user` 10 000 (per live-store instance), `global.max_bytes_per_trace` 5 MB, `read.max_search_duration`, `metrics_generator.processors` (empty = no span/service-graph metrics) | `ingestion_rate_mb` 4, `ingestion_burst_size_mb` 2, `max_label_names_per_series` 30, `max_profile_size_bytes` 4 MiB, `max_query_lookback` 1 w, `max_query_length` 1 d, `ingestion_tenant_shard_size` |
| Read-back / verification | `GET /runtime_config?mode=diff`; `GET /api/v1/user_limits` (tenant-authenticated, needs runtime config) | overrides-exporter metrics (not fetched, **UNVERIFIED**) | `GET /api/overrides?scope=merged` (query-frontend) | `overrides_exporter` component exists (ring config fetched; metric names **UNVERIFIED**) |
| API-driven per-tenant overrides | none (GEM admin API only) | none (GEL only) | **`/api/overrides`** GET/POST/PATCH/DELETE on the query-frontend, `X-Scope-OrgID` selects the tenant, `Etag`/`If-Match` (412 on mismatch), stored at `/{tenant}/overrides.json` in a dedicated bucket; subset only (`forwarders`, `cost_attribution.dimensions`, `metrics_generator.*`); "The value from the user-configurable overrides takes priority"; `check_for_conflicting_runtime_overrides` guards double definition | none |

Sources: https://grafana.com/docs/mimir/latest/configure/about-runtime-configuration/ ; https://raw.githubusercontent.com/grafana/mimir/main/cmd/mimir/help-all.txt.tmpl ; https://grafana.com/docs/loki/latest/configure/ ; https://grafana.com/docs/loki/latest/operations/storage/retention/ ; https://grafana.com/docs/tempo/latest/configuration/ ; https://grafana.com/docs/tempo/latest/operations/manage-advanced-systems/user-configurable-overrides/ ; https://grafana.com/docs/pyroscope/latest/configure-server/reference-configuration-parameters/ ; https://raw.githubusercontent.com/grafana/pyroscope/main/operations/pyroscope/helm/pyroscope/templates/configmap-overrides.yaml

### 5.2 The abstraction: `TenantLimits` → four rendered files

The `tenant` plugin owns a backend-neutral document and an extension point `limits-renderer` with one implementation per backend. The renderer is pure (document in, YAML out) and therefore unit-testable in isolation, which is the Harness requirement.

```yaml
# TenantLimits (control-plane schema, one per tenant; defaults come from the plan/tier)
tenant: acme
plan: team
retention:            { metrics: 400h, logs: 720h, traces: 336h, profiles: 744h }
ingest:
  metrics:            { samples_per_s: 50000, burst: 500000, max_series: 1000000 }
  logs:               { mb_per_s: 8, burst_mb: 16, max_streams: 20000 }
  traces:             { bytes_per_s: 60000000, max_bytes_per_trace: 5000000 }
  profiles:           { mb_per_s: 4, burst_mb: 8 }
query:                { max_lookback: 30d, max_range: 7d, max_series: 500000 }
features:             { span_metrics: true, service_graphs: true, log_patterns: true, otlp_index_labels: [service.name, k8s.namespace.name] }
status: active        # provisioning | active | suspended | deleting | purged
```

Rendering rules that fall out of §5.1:

1. Every renderer emits a **complete** tenant block, never a partial one (Tempo does not merge with defaults; Mimir/Loki/Pyroscope do, but emitting everything keeps the four files reviewable and diffs honest).
2. The outputs are `mimir-overrides.yaml`, `loki-overrides.yaml`, `tempo-overrides.yaml`, `pyroscope-overrides.yaml`, written as ConfigMaps (Kubernetes) or files (compose). Mimir/Loki/Pyroscope are pointed at a list `base.yaml,tenants.yaml;optional-keep-last-value-on-failure` so a bad render never takes down ingestion; Tempo has a single file, so the renderer validates before writing. All four pick the change up within 10 s; the reconciler then reads `/runtime_config?mode=diff`, `/api/overrides?scope=merged` and the overrides-exporter metrics to confirm, and records drift as an event on the tenant.
3. Tempo's `/api/overrides` is used only for the metrics-generator subset the UI exposes to tenant admins (self-service), with `check_for_conflicting_runtime_overrides: true` so the file and the API cannot both define `processors`.
4. `status: suspended` is enforced by the `auth` plugin refusing token issuance for the tenant's agents and service tokens (effective within one access-token TTL), not by zeroing backend limits; backend limits stay as the secondary guard.
5. Tenant IDs: the stricter `[a-z0-9][a-z0-9-]{0,62}` from `plugin-architecture.md` keeps the ID valid for all four backends (≤150 bytes, no `.`, `..`, colon or `|`), for Kubernetes namespaces and for GEM-style policy names (`[a-z0-9-_]`, 3–64).

### 5.3 Tenant create / delete across the four backends

Create is cheap: none of the four needs anything before the first write (tenants are implicit), so `Create` = write the limits files, provision the Grafana org + data sources, mint the first bootstrap token. Delete is the hard part; the saga below is what the `tenant` plugin's reconciler runs, with each step idempotent and verified.

| Step | Mimir | Loki | Tempo | Pyroscope |
|---|---|---|---|---|
| 1. Stop ingest | revoke all agent clients and service tokens for the tenant (gateway rejects after ≤ TTL) | same | same | same |
| 2. Remove configs | `POST /ruler/delete_tenant_config` ("internal API, not to be exposed"), `POST /multitenant_alertmanager/delete_tenant_config` (both keyed by `X-Scope-OrgID`, 200 even if nothing existed) | none | `DELETE /api/overrides` with `If-Match` (user-configurable overrides live in their own bucket at `/{tenant}/overrides.json`) | none |
| 3. Request data deletion | **`POST /compactor/delete_tenant`** with `X-Scope-OrgID: <tenant>` ("Request deletion of ALL tenant data"); poll **`GET /compactor/delete_tenant_status`** until `blocks_deleted: true`; final marker/debug cleanup after `-compactor.tenant-cleanup-delay` (default 6 h) | **no tenant-delete API.** Set per-tenant `retention_period: 1h` (no 24 h minimum applies to `retention_period`); compactor must be a singleton with `retention_enabled: true` and `delete_request_store`; chunks are removed after `retention_delete_delay`. `POST /loki/api/v1/delete` needs a `query` stream selector; whether a match-all selector is accepted is **UNVERIFIED** | **no tenant-delete API.** Set per-tenant `compaction.block_retention: 1h` (and keep `compaction_disabled: false`); backend-workers run retention (`retention_concurrency` 10 tenants in parallel, `compacted_block_retention` 1 h); then `storage.trace.empty_tenant_deletion_enabled: true` + `empty_tenant_deletion_age` (12 h) deletes "any remaining backend objects found in the tenant path" | **no tenant-delete API found.** Set per-tenant `retention_period: 1h` (per-tenant applicability of the v2 `retention_period` is **UNVERIFIED**); metastore cleanup runs every `index.cleanup_interval` 15 min with `cleanup_grace_period` 6 h |
| 4. Object-storage sweep (erasure) | per-tenant prefix `<tenant>/` (blocks, bucket index, markers) — layout well known but the Mimir page fetched does not print the path (**UNVERIFIED** path, verified that the compactor deletes it) | chunk objects under `<tenant_id>/` ("for example, `fake/` in single-tenant mode, or your org ID per tenant"); index objects under `index/` (path prefix `index.path_prefix`); per-tenant index sub-paths **UNVERIFIED** | `<prefix>/<tenant>/<blockID>/` ("tenant path"; `tempo-cli list blocks <tenant-id>` must come back empty); `tempo-cli` offers `rewrite-blocks drop-traces` and `redact` but nothing that deletes a tenant | **only after compaction**: `blocks/{shard}/{tenant}/{block_id}/block.bin` and `dlq/{shard}/{tenant}/…`; level-0 **`segments/{shard}/anonymous/{block_id}/block.bin` are multi-tenant objects** ("Segments are not yet split by tenant and use an anonymous tenant directory") and are deleted only through the compaction tombstone → delay → hard-delete flow |
| 5. Verify | `delete_tenant_status.blocks_deleted == true`, prefix listing empty | prefix listing empty for chunks; index tables contain no tenant entries | `list blocks` empty, prefix listing empty | prefix listing empty for `blocks/*/<tenant>/` and `dlq/*/<tenant>/`; segments: wait ≥ max segment age + compaction delay (median first compaction ≤ 15 s; hard-delete delay not documented, **UNVERIFIED**) |
| 6. Bucket versioning | if versioning is on (Loki recommends it as a safeguard; Tempo `confirm_versioning` for the overrides bucket), purge non-current versions too | same | same | same |
| 7. Control plane | delete Grafana org (deletes its teams/dashboards), fleet records (`UnregisterCollector` is agent-initiated; mark agents `revoked`), remove tenant from all four overrides files, keep an audit row | | | |

Timing expectations the C3 contract should expose: Mimir deletion completes within one compaction interval (1 h) plus `deletion-delay` (12 h) plus `tenant-cleanup-delay` (6 h) ≈ 19 h; Loki depends on `apply_retention_interval` + `retention_delete_delay` (2 h in the docs example); Tempo ≈ `compaction_cycle` + `compacted_block_retention` (1 h) + `empty_tenant_deletion_age` (12 h); Pyroscope is bounded by compaction progress and is the only backend where a **"data erased within N hours" guarantee cannot be made per tenant** today.

Sources: https://grafana.com/docs/mimir/latest/references/http-api/ ; https://raw.githubusercontent.com/grafana/mimir/main/cmd/mimir/help-all.txt.tmpl ; https://grafana.com/docs/mimir/latest/references/architecture/components/compactor/ ; https://grafana.com/docs/loki/latest/operations/storage/retention/ ; https://grafana.com/docs/loki/latest/operations/storage/logs-deletion/ ; https://grafana.com/docs/loki/latest/reference/loki-http-api/ ; https://grafana.com/docs/tempo/latest/configuration/ ; https://grafana.com/docs/tempo/latest/operations/tempo_cli/ ; https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/block-format/ ; https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/compaction/ ; https://grafana.com/docs/pyroscope/latest/configure-server/reference-configuration-parameters/

---

## 6. C3 contracts this implies

**`fleet` plugin** (implements `collector.v1.CollectorService`; Connect-RPC, served behind the gateway with `scope=fleet`):
- `Enrol(bootstrap_token, hostname, os, arch, attrs) → {agent_id, client_id, client_secret, token_url, remotecfg_url, package: {version, sha256}}` (one-time return of the secret).
- `GetConfig / RegisterCollector / UnregisterCollector` exactly as the proto; the server stores `remote_config_status` and `effective_config` per agent (that is the fleet inventory and "config history" the Datadog report said we must build).
- `Pipelines`: `{name, type: alloy|otel, matchers: {attr: value}, body}`; assembly wraps each pipeline in a `declare` block and merges into one file, hash = fnv over content (Alloy compares by content hash).
- Events: `agent.enrolled`, `agent.config.applied|failed`, `agent.stale` (no poll for 3 × `poll_frequency`).

**`auth` plugin** (minimal, separately scalable):
- `POST /oauth2/token` (client_credentials) → JWT `{iss, sub, tenant, orgid, scope[], aud, exp, jti}`; rejects revoked clients and suspended tenants.
- `GET /.well-known/jwks.json` + publishes the same JWKS to a file/ConfigMap for the gateway.
- `MintServiceToken(tenant | tenants[] | "*", scopes, ttl)`; `Revoke(jti)` (denylist consulted on query routes only).

**`gateway` extension point** (`gateway.envoy`, `gateway.inproc`): `Render(RouteTable, JWKS) → artifact`; conformance: strip/inject `X-Scope-OrgID`, scope-per-route, no pipe on writes, fail closed, body limits, `Retry-After` passthrough.

**`tenant` plugin**: `Create/Get/Update/Suspend/Resume/Delete(tenant)`; `SetLimits(tenant, TenantLimits)`; `limits-renderer` extension point (`mimir`, `loki`, `tempo`, `pyroscope`); reconciler with drift detection; delete saga from §5.3 with per-step status visible on the tenant resource.

**`grafana` plugin**: per-tenant org + four data sources using `httpHeaderName1: Authorization` with a service token from `auth`; admin org with federated tokens.

---

## 7. Unverified items

- Whether Prometheus/Alloy re-read `oauth2.client_secret_file` on every token refresh (decides if in-place secret rotation works without restart).
- `otelcol.auth.oauth2` field names in Alloy (component exists per the index page; page not fetched).
- Whether Envoy keeps serving with a stale JWKS after a failed `remote_jwks` refetch (mitigated by `local_jwks` from file).
- The literal definition of GEM `auth.type: trust` (inferred from the config reference; the GEM pages fetched did not define it).
- `jsonData.oauthPassThru` as the provisioning key (confirmed only via a Grafana engineer's forum answer and third-party plugin docs, not the Grafana provisioning page).
- Whether Grafana forwards `X-Grafana-Org-Id` to data sources (not found in the config reference).
- Pyroscope: absence of pipe-separated tenant federation; per-tenant applicability of v2 `retention_period`; hard-delete delay for compacted segments; the Helm chart's mount path/flag for `overrides.yaml`.
- Loki: whether `POST /loki/api/v1/delete` accepts a match-all selector; the per-tenant layout inside `index/<table>/`.
- Mimir: the exact per-tenant object path (`<tenant>/…`) and the tenant-deletion marker file name (the compactor page fetched does not print them).
- Overrides-exporter metric names for Loki and Pyroscope.
- Maturity of community `remotecfg` servers (shepherd, alloy-fleet-manager, opsplane) — still unverified from `grafana-stack.md`; nothing in this pass changes the recommendation to implement the three-RPC service ourselves.

## 8. Open questions for the architect

1. Access-token TTL: 15 min bounds both the revocation window and the maximum kernel/auth outage before ingest stops; is 15 min acceptable, or do we want a longer TTL plus a `jti` denylist on write routes (costs a cache lookup per request)?
2. OTLP/HTTP as the only trace transport at the edge (simplifies every gateway option) vs also exposing gRPC 4317 (needs Envoy).
3. Should `Enrol` accept cloud instance identity (AWS IID / Azure MSI / GCP identity token, as SPIRE does) in place of a bootstrap token for autoscaled fleets?
4. Pyroscope erasure: accept "best effort until compaction" for profiles in the hosted terms of service, or run profiles single-tenant-per-bucket for regulated tenants?
5. Do we expose Tempo `/api/overrides` to tenant admins directly (self-service metrics-generator) or only through our UI with the renderer as the single writer?
6. Phase-2 `oauthPassThru`: do we want per-user authorization at the data plane (label policies, GEM style) or is per-tenant isolation plus Grafana RBAC enough for v1?

## 9. Sources (all accessed 2026-10-27)

Alloy and remote config: https://grafana.com/docs/alloy/latest/reference/config-blocks/remotecfg/ ; https://github.com/grafana/alloy-remote-config ; https://raw.githubusercontent.com/grafana/alloy-remote-config/main/api/collector/v1/collector.proto ; https://raw.githubusercontent.com/grafana/alloy/main/internal/service/remotecfg/remotecfg.go ; https://raw.githubusercontent.com/grafana/alloy/main/internal/service/remotecfg/config_manager.go ; https://grafana.com/docs/alloy/latest/reference/components/prometheus/prometheus.remote_write/ ; https://grafana.com/docs/alloy/latest/reference/components/loki/loki.write/ ; https://grafana.com/docs/alloy/latest/reference/components/otelcol/otelcol.exporter.otlp/ ; https://grafana.com/docs/alloy/latest/reference/components/pyroscope/pyroscope.write/ ; https://grafana.com/docs/alloy/latest/reference/components/remote/remote.http/ ; https://grafana.com/docs/alloy/latest/configure/linux/ ; https://grafana.com/docs/alloy/latest/reference/cli/otel-supervisor/ (via search summary) ; https://prometheus.io/docs/prometheus/latest/configuration/configuration/

Grafana Cloud reference model: https://storage.googleapis.com/cloud-onboarding/alloy/scripts/install-linux.sh ; https://storage.googleapis.com/cloud-onboarding/alloy/config/config-fm.alloy ; https://grafana.com/docs/grafana-cloud/security-and-account-management/authentication-and-permissions/access-policies/ ; https://grafana.com/docs/grafana-cloud/send-data/fleet-management/introduction/architecture/

Other enrolment models: https://www.elastic.co/guide/en/fleet/current/fleet-enrollment-tokens.html ; https://raw.githubusercontent.com/open-telemetry/opamp-spec/main/specification.md ; https://raw.githubusercontent.com/open-telemetry/opentelemetry-collector-contrib/main/cmd/opampsupervisor/README.md ; https://spiffe.io/docs/latest/spire-about/spire-concepts/

Gateways: https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_filters/ext_authz_filter ; https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/ext_authz/v3/ext_authz.proto ; https://www.envoyproxy.io/docs/envoy/latest/api-v3/service/auth/v3/external_auth.proto ; https://www.envoyproxy.io/docs/envoy/latest/api-v3/extensions/filters/http/jwt_authn/v3/config.proto ; https://nginx.org/en/docs/http/ngx_http_auth_request_module.html ; https://nginx.org/en/docs/http/ngx_http_grpc_module.html ; https://doc.traefik.io/traefik/reference/routing-configuration/http/middlewares/forwardauth/ ; https://raw.githubusercontent.com/grafana/mimir/main/operations/helm/charts/mimir-distributed/values.yaml ; https://raw.githubusercontent.com/grafana/loki/main/production/helm/loki/values.yaml ; https://raw.githubusercontent.com/grafana/loki/main/production/helm/loki/templates/_helpers.tpl

Backends: https://grafana.com/docs/mimir/latest/manage/secure/authentication-and-authorization/ ; https://grafana.com/docs/mimir/latest/configure/about-runtime-configuration/ ; https://grafana.com/docs/mimir/latest/references/http-api/ ; https://grafana.com/docs/mimir/latest/references/architecture/components/compactor/ ; https://raw.githubusercontent.com/grafana/mimir/main/cmd/mimir/help-all.txt.tmpl ; https://grafana.com/docs/loki/latest/configure/ ; https://grafana.com/docs/loki/latest/operations/storage/retention/ ; https://grafana.com/docs/loki/latest/operations/storage/logs-deletion/ ; https://grafana.com/docs/loki/latest/reference/loki-http-api/ ; https://grafana.com/docs/tempo/latest/configuration/ ; https://grafana.com/docs/tempo/latest/operations/manage-advanced-systems/user-configurable-overrides/ ; https://grafana.com/docs/tempo/latest/operations/tempo_cli/ ; https://grafana.com/docs/pyroscope/latest/configure-server/reference-configuration-parameters/ ; https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/about-pyroscope-v2-architecture/ ; https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/block-format/ ; https://grafana.com/docs/pyroscope/latest/reference-pyroscope-v2-architecture/compaction/ ; https://raw.githubusercontent.com/grafana/pyroscope/main/operations/pyroscope/helm/pyroscope/values.yaml ; https://raw.githubusercontent.com/grafana/pyroscope/main/operations/pyroscope/helm/pyroscope/templates/configmap-overrides.yaml

Grafana and GEM: https://grafana.com/docs/grafana/latest/administration/provisioning/ ; https://grafana.com/docs/grafana/latest/datasources/prometheus/configure/ ; https://grafana.com/docs/grafana/latest/setup-grafana/configure-grafana/ ; https://grafana.com/docs/grafana/latest/administration/data-source-management/teamlbac/ ; https://community.grafana.com/t/provision-datasource-with-forward-oauth-identity/16818 ; https://github.com/grafana/grafana-opentsdb-datasource/issues/88 ; https://grafana.com/docs/enterprise-metrics/latest/admin-api/ ; https://grafana.com/docs/enterprise-metrics/latest/config/reference/ ; https://grafana.com/docs/enterprise-metrics/latest/config/ ; https://grafana.com/docs/enterprise-metrics/latest/manage/oauth/ ; https://grafana.com/docs/enterprise-metrics/latest/manage/gateway/ ; https://grafana.com/docs/enterprise-metrics/latest/manage/tenant-management/tenant-federation/

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
