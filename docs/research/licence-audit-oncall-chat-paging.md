# Licence audit: on-call, chat and paging

Date read for every source: 2026-11-11. Status: research input for the licence review of this round (owner decisions A, B, C). This is engineering analysis, not legal advice. Section 7 lists the questions a lawyer must answer.

## 1. Scope and method

Group audited: the on-call provider, the chat surface and the paging carriers that the plan names, plus the archived upstream that nobody may copy from, and the constraints on a native first-party on-call plugin.

Owner decision B of this round is taken as given: GoAlert is one on-call provider plugin, not the on-call engine of the product. The audit therefore asks two things of each upstream: do its terms allow the five uses, and can the core and the default bundle live without it.

The five uses, referred to below as U1 to U5:

| Code | Use |
|---|---|
| U1 | Community edition: an Apache-2.0 repository and release artifacts (binary, images, Helm chart, compose file) that bundle or pull the upstream |
| U2 | Our own hosted multi-tenant service running the upstream unmodified |
| U3 | A later commercial enterprise edition on the same core, possibly with closed-source plugins in the same binary or as separate processes |
| U4 | Third parties selling plugins that wrap or call the upstream |
| U5 | The upstream's name in a plugin id or description |

Method. Licence facts were read at the primary source: the licence file on the default branch and the GitHub licence API for repositories, and the vendor's own terms pages for services. The text of the Slack API Terms of Service and of the PagerDuty patent claim was read from the raw page. The other terms pages were read through a fetch tool that returns short excerpts, so only the fragments in quotation marks are verbatim. Anything not read is marked UNVERIFIED. The earlier research notes were used as leads only.

Four modes of use are kept apart throughout, because the obligations differ:

| Mode | Meaning | What governs it |
|---|---|---|
| Network | We or the customer call a service over its API | A contract (terms of service), revocable, not a licence on our code |
| Separate process | We run or reference an unmodified upstream image | The upstream licence on redistribution only if we redistribute the image |
| Linked | Upstream code compiled into our binary | The upstream licence on the binary |
| Copied | Upstream code, schema, text or assets placed in our repository | The upstream licence on our source, and on everything derived from it |

## 2. Components found in the plan

Grep of `docs/architecture/00-decisions.md`, `docs/design/plugin-system-2026-11.md`, `docs/design/community-first-2026-11.md` and `docs/architecture/0*.md`.

| Component | Where the plan names it | In the task scope |
|---|---|---|
| GoAlert 0.35 | 00 §2.1 row 3, §3.2 `oncall-goalert`, bundle `aiops-oncall-goalert`, rows `on-call-provider-goalert`, `on-call-proxy-goalert`, `tool-oncall-who`; profiles `hosted` and `self-host-full` | yes |
| Archived OnCall OSS | 08 ADR context and §5.1: "may be read for data-model ideas, never linked or copied" | yes |
| slack-go v0.31.0 | 00 §2.1 row 4; 03 (the `chat` rows); 06 (`webapi`) | yes |
| Slack platform (Events API, Socket Mode, OAuth v2, Sign in with Slack, Agent Sessions, Marketplace) | 00 §2.1 row 4, `ext-slack`, bundle `aiops-chatops-slack`, rows `chat-surface-slack`, `slack-commands`, `notifier-slack`; `aiopsctl slack manifest` | yes |
| Twilio | 00 §2.1 row 3, `ext-paging-carriers`, one subaccount per hosted tenant, callbacks through the tenant on-call subdomain | yes |
| ntfy | 00 `ext-paging-carriers` "webhook push target (ntfy)"; `notifier-push-ntfy` is dropped | yes |
| PagerDuty | 00 `ext-pagerduty`, `on-call-provider-pagerduty` (triggered item; hosted tenants above the GoAlert ceiling) | yes |
| Opsgenie | 00 §7.3 `on-call-provider-opsgenie` (triggered item) | yes |
| A vendor-hosted incident response service, id suffix `-irm` | 00 §7.3 `on-call-provider-irm` (triggered item) | added |
| Teams, Mattermost | 00 §7.3 `chat-surface-{teams,mattermost}` (triggered item) | added |
| SMTP relay | 00 `ext-paging-carriers`, `notifier-email` | added |
| Alertmanager webhook | 00 §2.1 row 5, dead-man route | added; licence owned by the telemetry-stack group |
| Native on-call plugin `on-call-provider-builtin` | 00 §2.1 row 3 "Native module", §7.3; 06: "would port GoAlert's schedule, override, escalation and on-call semantics" | yes |

Not in the plan, added as prior art for the native plugin: LinkedIn Oncall and Iris.

## 3. Summary table

| Component | Role in the plan | Use mode | Licence or terms, as verified | Verdict |
|---|---|---|---|---|
| GoAlert | On-call provider plugin (decision B): schedules, rotations, escalation, notification | Separate process, unmodified image, called over its GraphQL API; possible later port of semantics (copied) | Apache-2.0 | ok-with-conditions |
| OnCall OSS (archived upstream) | None. Listed so nobody copies from it | Must be none | AGPL-3.0 at the root; a proprietary plugin licence file in `grafana-plugin/` | replace-or-rebuild (never copy; nothing to replace because it was never adopted) |
| slack-go | Slack client library under `chat-surface-slack` | Linked | BSD-2-Clause | ok |
| Slack platform | Chat surface and approvals | Network; our distributed app in hosted; the customer's own app from our manifest in self-host | Contract: API Terms of Service, Developer Policy, Marketplace Agreement | optional-plugin-only |
| Slack marks | Plugin ids, bundle name, descriptions | Name only | Brand terms; no trademark licence | ok-with-conditions |
| Twilio | SMS and voice carrier behind the on-call provider | Network; our account with subaccounts in hosted; the operator's account in self-host | Contract: Terms of Service, Acceptable Use Policy; trademark guidelines | optional-plugin-only |
| ntfy | Push target behind a webhook | Network to a server the operator runs or to ntfy.sh; not shipped by us | Apache-2.0 OR GPL-2.0 (server); ntfy.sh terms for the hosted service | ok-with-conditions |
| PagerDuty | Alternative on-call provider plugin | Network, with the customer's credentials | Contract: Developer Agreement; SDK `go-pagerduty` Apache-2.0 | optional-plugin-only |
| Opsgenie | Alternative on-call provider plugin | Network | Service ends 2027-04-05; SDK Apache-2.0; API terms UNVERIFIED | unverified (drop the item) |
| IRM service (`-irm`) | Alternative on-call provider plugin | Network | Terms not read | unverified |
| Mattermost | Alternative chat surface | Network to the customer's server; client package linked | Server AGPL-3.0 or commercial; compiled builds MIT; `server/public/` Apache-2.0 | ok-with-conditions |
| Teams | Alternative chat surface | Network | Terms not read | unverified |
| SMTP relay | Email notification and paging | Network, open protocol, operator-supplied | No licence involved | ok |
| Alertmanager webhook | Alert source and dead-man sender | Network; we parse its webhook payload | Apache-2.0 | ok |
| LinkedIn Oncall and Iris | Prior art for a native plugin | Reading; possible port (copied) | BSD-2-Clause | ok |
| Native on-call plugin | First-party provider, if built | Our own code | Apache-2.0 (ours) | ok-with-conditions |

Answer to decision C for this group. No component in this group must be rebuilt because of its licence. GoAlert is suitable as a provider plugin and may even be bundled. The one real conflict is contractual, not a code licence: the Slack API Terms of Service restrict commercial distribution, which touches U2, U3 and U4. The archived OnCall OSS must never be a source of code.

## 4. Verified facts per component

### 4.1 GoAlert

| Fact | Value | Source |
|---|---|---|
| Repository, owner | `target/goalert`, owner organisation `target` (Target Brands, Inc.), not archived | https://api.github.com/repos/target/goalert |
| Licence | Apache-2.0; file `LICENSE.md`, blob `dead3101`; copyright line "Copyright (c) 2019-2021 Target Brands, Inc." | https://github.com/target/goalert/blob/master/LICENSE.md at `master` commit `a4708420` (2026-10-28) |
| NOTICE file | None at the repository root on `master` | repository root listing, same commit |
| Release the plan pins | v0.35.0, published 2026-09-21, tag ref object `db982918`; image `goalert/goalert:v0.35.0` on Docker Hub, pushed 2026-09-21 | GitHub releases API; Docker Hub tags API |
| Contribution terms | No CLA in `CONTRIBUTING.md`. The organisation ships a DCO configuration (`target/.github/dco.yml`) and pull request 4603 carries a `Signed-off-by` line and a passing `DCO` check | https://github.com/target/goalert/blob/master/CONTRIBUTING.md; https://github.com/target/.github/blob/main/dco.yml |
| Maintenance | Active: last commit 2026-10-28. Releases v0.33.0 2024-11-27, v0.34.0 2025-08-28, v0.34.1 2025-10-06, v0.35.0 2026-09-21. Three people account for most commits (4221, 2232 and 1091 contributions) | GitHub commits, releases and contributors APIs |
| Image base | `docker.io/library/alpine` plus the single Go binary with the embedded web UI | `devtools/ci/dockerfiles/goalert/Dockerfile` on `master` |
| Open-core split, enterprise edition | None found in the repository. UNVERIFIED beyond the repository |  |
| Trademark registration of the name | UNVERIFIED |  |
| Licences of its Go and npm dependencies | UNVERIFIED (247-line `go.mod`; the web bundle was not scanned) |  |

Conditions per use:

| Use | Result | Condition |
|---|---|---|
| U1 | Allowed | Referencing the upstream image by digest in the chart and the compose file distributes nothing. If we mirror the image, or put it in an air-gapped bundle, we redistribute it: include the Apache-2.0 text, keep notices, and carry an SBOM and the licence texts of what is inside the image (base image packages, Go modules, npm packages) |
| U2 | Allowed | Apache-2.0 has no network clause. Run it unmodified, one instance per tenant, as the plan already says |
| U3 | Allowed | It stays a separate process. Closed plugins do not link it |
| U4 | Allowed | A third-party plugin may call or wrap it. The plugin's own licence is the author's choice |
| U5 | Allowed as a reference | Apache-2.0 section 6 grants no trademark right. Use the name only to say what the plugin talks to (`on-call-provider-goalert`, "on-call provider for GoAlert"). No logo, and not in a product, edition or bundle display name that reads as ours |

Risk. Single corporate owner with no foundation governance; a small core team; version 0.x, so no API stability promise, and roughly one release a year. A permissive licence lets the owner publish later versions under other terms; released versions stay Apache-2.0, and the absence of a CLA makes a relicence of the existing history harder. No sign of either today.

Verdict: ok-with-conditions. Under decision B it is one provider row. Nothing in its licence forces it out of a default bundle, and nothing forces a rebuild.

Porting its semantics into a native plugin is allowed (Apache-2.0 section 4): keep the copyright and licence notices of any file that is translated, state that the files were changed, and add the attribution to `NOTICE`, as 08 §5.1 already records.

### 4.2 OnCall OSS (archived upstream)

| Fact | Value | Source |
|---|---|---|
| Repository | `grafana/oncall` now redirects to `grafana-cold-storage/oncall`; archived; last push 2026-03-24; default branch `dev` at commit `af0fbd40` | https://api.github.com/repos/grafana/oncall (HTTP 301) |
| README | "On 2025-03-11 Grafana OnCall (OSS) entered maintenance mode and was archived on 2026-03-24" | README on `dev` |
| Root licence | AGPL-3.0; file `LICENSE`, blob `19bae358` | GitHub licence API |
| Plugin directory licence | `grafana-plugin/LICENSE` (blob `6b7533ff`) is the text of a "Grafana Enterprise Plugin License Agreement", while `grafana-plugin/package.json` declares `"license": "Apache-2.0"` | files on `dev` |

The three signals disagree. For engineering purposes the whole repository is treated as not reusable.

What its licence would impose if anything were copied:

| Use | Effect of copying AGPL-3.0 code |
|---|---|
| U1 | Blocked. The file or plugin that receives the code becomes a covered work and must be AGPL-3.0. In process, the combined binary must be offered under AGPL-3.0 as a whole, which contradicts an Apache-2.0 release |
| U2 | Section 13 applies: every user who interacts with the modified program over the network must be offered its complete corresponding source |
| U3 | Blocked. Closed-source plugins cannot share a binary with it |
| U4 | Blocked for any paid plugin that is not itself AGPL-3.0 |
| U5 | The product name contains a protected mark; the naming rule of 08 §5.2 already rejects it |

The directory `grafana-plugin/` adds a second trap: its licence file is a proprietary agreement, so the frontend cannot be assumed to be AGPL either.

Running it unmodified as a separate process would be legally comparable to the AGPL backends, but the project is archived and receives no fixes, so it is not a candidate.

Verdict: replace-or-rebuild, in the sense "never a source". It blocks U1, U3 and U4 if copied. What we use instead: GoAlert as a provider plugin, and for a native plugin our own code or a port from GoAlert (Apache-2.0) or from the BSD-2-Clause projects of section 4.10.

Recommended tightening of the sentence in 08 §5.1 ("may be read for data-model ideas"): people who write the native plugin work from the public product documentation and from GoAlert, not from this source tree. No table definitions, migrations, identifiers, message templates, UI text or API descriptions are taken from it. See question 9.

### 4.3 slack-go

| Fact | Value | Source |
|---|---|---|
| Licence | BSD-2-Clause; file `LICENSE`, blob `5145171f`; "Copyright (c) 2015, Norberto Lopes" | https://github.com/slack-go/slack/blob/master/LICENSE at `master` commit `5d5455cb` (2026-11-10) |
| Version the plan pins | v0.31.0, published 2026-10-26 (tag exists). Latest is v0.35.0, 2026-11-05, with v0.33.0 and v0.34.0 in the ten days between | GitHub releases API |
| Owner | Community organisation `slack-go`; not an official Slack SDK | repository metadata |
| CLA | UNVERIFIED |  |

All five uses are allowed. The only obligation is to reproduce the copyright notice and the licence text with binary distributions, which the generated `THIRD_PARTY_LICENSES/` covers. Risk: fast-moving 0.x API, which the thin `slackapi` interface in the plan already absorbs. Verdict: ok.

The library licence says nothing about the Slack service. That is section 4.4.

### 4.4 Slack platform

Terms read:

| Document | Date on the page | URL |
|---|---|---|
| Slack API Terms of Service | "Effective: October 10, 2025" | https://slack.com/terms-of-service/api |
| Changelog entry on the terms update | 2025-05-29 | https://docs.slack.dev/changelog/2025/05/29/tos-updates/ |
| Slack App Developer Policy | "Effective Date: December 10, 2024" | https://docs.slack.dev/developer-policy/ |
| Slack Marketplace Agreement | "Last modified: July 11, 2016" | https://slack.com/terms-of-service/slack-marketplace |
| Marketplace guidelines and requirements | undated, "subject to change" | https://docs.slack.dev/slack-marketplace/slack-marketplace-app-guidelines-and-requirements |
| Marketplace review guide | undated | https://docs.slack.dev/slack-marketplace/slack-marketplace-review-guide |
| Socket Mode documentation | undated | https://docs.slack.dev/apis/events-api/using-socket-mode |

The clauses that matter, verbatim from the API Terms of Service:

1. "You may not Commercially Distribute an Application that integrates with the Slack APIs unless you are authorized to do so under a separate agreement with Slack or our parent company, Salesforce."
2. "By 'Commercially Distribute' we mean any situation where your Application integrates with Slack APIs and users could pay fees for your product, service, or features, whether it's a direct charge to access the App, a 'freemium' model where some features are gated, or a free App that connects to a paid product or service." (The original uses a dash where this quotation has the comma before "whether".)
3. "For most Applications, obtaining a separate agreement that authorizes Commercial Distribution simply means submitting to our Marketplace and agreeing to the Marketplace Agreement. Alternatively, you may enter into a partner agreement with Slack or Salesforce that authorizes you to Commercially Distribute."
4. "The restrictions in this section apply to all Applications that are Commercially Distributed, regardless of whether you publish an off-the-shelf Application or you provide customers with a custom Application or Application template that connects to other products, services, or features. These restrictions do not apply if your Application and any products or services connected to it were created for use only by a single third party."
5. "As the provider of an Application offered for use outside your organization, you may not: (A) use API Data to train a large language model; (B) bulk export Slack message and file data except where expressly allowed by an additional agreement [...]; or (C) use data collected from one organization to directly benefit a different organization or any third party."
6. "You must limit your use, processing, and retention of API Data from others outside your organization to the minimum necessary to develop, test, operate, and support your Application's functionality".
7. You will not "access our APIs in order to replicate or compete with the Services".
8. "We may terminate the Contract with or without cause, and without notice to you."

From the other documents: "Apps using Socket Mode are not currently allowed in the public Slack Marketplace." The Developer Policy prohibits "Sub-licensing, distributing or allowing access to the Slack APIs to anyone else" and "Using Data to train an LLM under any circumstances", and requires deletion of all associated data within 14 business days when an app is discontinued. The Marketplace guidelines say "Slack allows you to charge users for your app", require an app to be installed on 10 or more active workspaces before submission, require disclosure of the model, data retention, tenancy and residency for an app that exposes users to a language model, say "DON'T allow AI make consequential decisions without human review", and for agent or assistant apps say "DON'T store any Slack data you obtain" and "Store metadata instead and pull in data in real time". The Marketplace Agreement as fetched contains no fee or revenue share payable to Slack.

Conditions per use:

| Use | Result | Reasoning |
|---|---|---|
| U1 | Allowed today, with a condition | In self-host the customer creates its own app in its own workspace from `aiopsctl slack manifest` and accepts the terms itself. While nothing is sold, clause 2 is not met. Clause 4 names an "Application template" explicitly, so the moment a paid edition, a paid hosted service or a paid plugin connects to that same template, the community path may be read as a free app that connects to a paid product. Question 1 |
| U2 | Allowed only with authorisation | A paid hosted service with one distributed app is commercial distribution. It needs a Marketplace listing and the Marketplace Agreement, or a partner agreement. The listing needs 10 active workspaces first, so the hosted app starts free. As provider we must hold a user agreement and a privacy policy, obtain explicit authorisation for the use of API data, keep retention to the minimum, never train a model on API data, and never let one tenant's Slack data benefit another tenant |
| U3 | Blocked in its planned form | The enterprise edition is a paid product. Its self-host Slack path is the manifest template in Socket Mode. Clause 4 covers templates, Socket Mode apps cannot be listed, and the single-third-party exception does not describe a product sold to many customers. The routes are a partner agreement with Slack or Salesforce, or a listed Events API app, which needs a public endpoint and therefore breaks on-prem and air-gapped installs |
| U4 | Blocked without a design rule and an agreement | A paid plugin that adds commands or messages to our app makes "features" of that app paid, which is clause 2 again. A plugin that received the bot token or raw API access would also meet the Developer Policy prohibition on allowing access to the APIs to anyone else. A plugin that brings its own Slack app carries its own obligations |
| U5 | See 4.5 |  |

Design consequences that follow from the terms, independent of any lawyer's answer:

1. The chat surface stays a definition with a first-party default that needs no third-party contract (the dashboard and the CLI already carry the incident loop and approvals). No row of the core and no default bundle may need Slack to work. Slack is an optional bundle that the operator turns on.
2. Third-party plugins never receive a Slack token and never call the Slack API. They contribute typed commands and messages through `chat`, and the first-party row renders them. The plan already routes every call through `chat`; this makes it a rule of the gate.
3. In hosted, the incident timeline stores references to Slack messages (channel, timestamp, permalink) and fetches text when needed, not copies of message bodies. Tenant delete removes Slack-derived data within the 14 business day limit.
4. No learning loop, evaluation set or cross-tenant feature may be fed from Slack API data.
5. The Marketplace listing or a partner agreement is a precondition of any price list, not a later task. ADR-010 already holds the listing as open; it should also hold the enterprise self-host question.
6. A self-hostable chat surface whose terms do not restrict commercial distribution (section 4.9) is the hedge for the paid self-host edition.

Verdict: optional-plugin-only. Nothing here is rebuilt; the conflict is contractual. It blocks U3 as planned and U4 without the rule above, and it conditions U2.

### 4.5 Slack marks

Source: Slack brand terms, https://slack.com/terms-of-service/slack-brand (no effective date on the page), and the Marketplace guidelines.

Rules as returned: "Don't register a domain containing the word 'slack' or any variation thereof"; "Do not apply for a trademark that includes the word 'slack'"; "When 'Slack' is used, use as an adjective followed by a description of our services"; "Most uses require a specific written license"; a user of the API "must avoid suggesting your service or application is produced or supported by Slack"; the statement that an application "is integrated with Slack" is tied to a Marketplace listing. The Marketplace guideline gives the naming pattern "Task notifications for Slack" rather than "Slack task notifications". The Developer Policy asks for an icon that does not resemble the Slack icon.

Conditions:

| Item | Rule |
|---|---|
| Plugin and bundle ids | Keep the referential suffix form: `chat-surface-slack`, `notifier-slack`, `aiops-chatops-slack`. Rename `slack-commands` to the same form, for example `chat-commands-slack` |
| Display titles and descriptions | "Chat surface for Slack", never "Slack chat surface" and never a title that reads as a Slack product |
| App name in the manifest | The name the manifest gives the customer's app does not contain the word |
| Logo | Not used in the plugin index, the documentation or a future marketplace page without a written licence |
| Domains, trademarks | None containing the word |
| Third-party listings | The index and a future marketplace apply the same naming rule to publishers and need a takedown path |

Verdict: ok-with-conditions. Whether an identifier counts as nominative use is question 5.

### 4.6 Twilio

| Document | Date on the page | URL |
|---|---|---|
| Terms of Service | "July 16, 2026" | https://www.twilio.com/en-us/legal/tos |
| Acceptable Use Policy | May 12, 2025 | https://www.twilio.com/en-us/legal/aup |
| Trademark guidelines | "Last Updated March 20, 2025" at the top, "September 8, 2025" at the bottom | https://www.twilio.com/en-us/legal/trademark |
| ISV onboarding for A2P 10DLC (documentation, not terms) | undated | https://www.twilio.com/docs/messaging/compliance/a2p-10dlc/onboarding-isv |

Fragments as returned. Terms section 2.2(b): the customer will "not transfer, resell, lease, license, or otherwise make available the Services to third parties (except to make the Services available to your End Users) or offer them on a standalone basis". Section 2.2(d): the customer will "be solely responsible for all acts, omissions, and activities of your End Users". Acceptable Use Policy: "If Customer or any End User violates this AUP, Twilio may suspend Customer's use of the Services." Trademark guidelines: "Don't use Twilio Trademarks as your own product or service name or trade name"; "Don't combine Twilio Trademarks with your or any other party's name or trademark"; a relational phrase such as "for use with" is the accepted form; plain-text reference is allowed.

We write no Twilio code: the client lives inside GoAlert (`notification/twilio`). No Twilio SDK is linked. If one ever is, `twilio/twilio-go` is MIT (GitHub licence API).

| Use | Result | Condition |
|---|---|---|
| U1 | Allowed | The community edition ships no carrier credentials. The operator opens its own account and accepts the terms. Publishing an integration with a documented API needs no agreement from the project (UNVERIFIED that no documentation terms apply) |
| U2 | Allowed with obligations | We are the customer and each tenant is an end user. Paging inside our service is within section 2.2(b); selling messaging on its own is not. We answer for every tenant's conduct, one tenant's violation can suspend the parent account, and per-tenant sender registration is ours to perform. The plan's one subaccount per tenant matches the vendor's guidance |
| U3 | Allowed | Same as U1 |
| U4 | Allowed | A third-party carrier plugin uses the operator's account |
| U5 | Conditional | Identifier in the referential suffix form only; display title "SMS and voice carrier for use with Twilio"; no logo |

Not found in the text read: the emergency-services limitation and any clause on subaccounts. Both UNVERIFIED.

Verdict: optional-plugin-only. A paid proprietary carrier can never be something the core or a default bundle needs. Email and webhook must remain a complete paging path, which the plan already states for self-host.

### 4.7 ntfy

| Fact | Value | Source |
|---|---|---|
| Licence | README: "dual licensed under the Apache License 2.0 and the GPLv2 License"; files `LICENSE` (blob `80877693`, "Copyright 2021 Philipp C. Heckel") and `LICENSE.GPLv2` | https://github.com/binwiederhier/ntfy at `main` commit `a331a5c8` (2026-11-10); latest release v2.37.2, 2026-11-04 |
| Bundled content | The README lists notification sounds under the Mixkit Free License and Creative Commons Attribution | same README |
| Hosted service terms | "Last updated: January 26, 2026", operated by ntfy LLC. Free use "is subject to rate limits"; no uptime guarantee; no clause on commercial use; "we recommend self-hosting your own ntfy server" for demanding needs | https://docs.ntfy.sh/terms/ |
| Marks | "Our trademarks may not be used in connection with any product or service without our prior written consent." | same page |

The plan does not ship ntfy. It is a webhook target the operator runs or subscribes to, and `notifier-push-ntfy` is dropped.

| Use | Result | Condition |
|---|---|---|
| U1 | Allowed | If the server image is ever added to a compose file, elect Apache-2.0 and carry the notices of the bundled sounds |
| U2 | Allowed | Do not point a hosted paging path at the free ntfy.sh tier: rate limits, no uptime commitment. Run the server ourselves |
| U3, U4 | Allowed |  |
| U5 | Conditional | The terms statement on marks is broad. Keep the name out of ids until it is cleared (question 5); a generic `notifier-webhook` already does the job |

Risk: one maintainer and one small company. Verdict: ok-with-conditions.

### 4.8 PagerDuty, Opsgenie, the IRM service

PagerDuty Developer Agreement, "Last Modified: September 30, 2026", https://docs.pagerduty.com/developer/pagerduty-developer-agreement. Fragments as returned: the licence is "limited, revocable, non-exclusive, non-transferable, non-sublicensable" and "solely for Your internal business purposes in developing Your Applications"; section 4(j) forbids to "replicate or attempt to replace the user experience of Our Products"; section 4(k) forbids to "use the PagerDuty Developer Services to develop a product competitive to Our Products"; section 2(c) allows the marks only "Upon Your submission and Our acceptance and publication of Your Applications"; termination "in Our sole discretion at any time and for any reason". Nothing in the text forbids charging for an application. The SDK `PagerDuty/go-pagerduty` is Apache-2.0 (GitHub licence API; latest release v1.8.0 of 2024-01-09, `main` commit `e4de2e38` of 2026-03-24).

| Use | Result for a PagerDuty provider plugin |
|---|---|
| U1, U3 | Conditional. A product that ships its own on-call functions, or bundles GoAlert, and that investigates incidents, may be "a product competitive" under section 4(k). Question 6 |
| U2 | Conditional. The plan's rule "hosted tenants above the GoAlert ceiling use `on-call-provider-pagerduty`" makes a hosted capacity plan depend on a competitor's revocable licence. Under decision B that rule should go |
| U4 | Conditional. The third party is its own party to the agreement |
| U5 | Conditional. The marks clause is narrower than the others in this group |

Verdict: optional-plugin-only, written by us only after question 6 is answered, or left to a contributor.

Opsgenie. Atlassian states that the product "would no longer be available for purchase, effective June 4, 2025" and that on April 5, 2027 "access to Opsgenie will be shut off" (https://www.atlassian.com/licensing/opsgenie). This settles the item that 00 §2.1 carries as UNVERIFIED. The SDK `opsgenie/opsgenie-go-sdk-v2` is Apache-2.0 and was last pushed 2024-07-12. API terms were not read. Verdict: unverified, and moot. Remove `on-call-provider-opsgenie` from the triggered items. The successor product's API terms are UNVERIFIED.

IRM service (`on-call-provider-irm`). A vendor-hosted service of Grafana Labs. Its API terms and the cloud service terms were not read. Verdict: unverified. The id suffix `-irm` passes the naming rule.

### 4.9 Mattermost and Teams

Mattermost, `LICENSE.txt` on `master`, https://github.com/mattermost/mattermost/blob/master/LICENSE.txt: compiled versions produced by Mattermost, Inc. are under an MIT licence; the source is under AGPL-3.0 or a commercial licence; "server/templates/, server/i18n/, server/public/, webapp/ and all subdirectories thereof" are under Apache-2.0; the copyleft terms are not enforced against an application that "does not link to the Mattermost Platform directly". "Your use of the mark Mattermost is subject to Mattermost, Inc's prior written approval".

A `chat-surface-mattermost` plugin talks to the customer's own server over its API and may link only the client and model packages under `server/public/`. That satisfies U1 to U4. U5 needs the approval question answered (question 5). There is no commercial-distribution clause comparable to Slack's in the licence file, which makes it the natural second chat surface for a paid self-host edition. API terms beyond the licence file are UNVERIFIED. Verdict: ok-with-conditions.

Teams: terms not read. Verdict: unverified.

### 4.10 SMTP, Alertmanager webhook, LinkedIn Oncall and Iris

SMTP is an open protocol and the relay is the operator's. Verdict: ok.

Alertmanager: `prometheus/alertmanager` is Apache-2.0 (GitHub licence API, file `LICENSE`, blob `261eeb9e`). We receive and parse its webhook payload, which is use of an interface over the network. The component itself belongs to the telemetry-stack group. Verdict: ok.

LinkedIn Oncall and Iris: `linkedin/oncall` and `linkedin/iris` are BSD-2-Clause (GitHub licence API; last pushes 2025-08-20 and 2025-06-12, `oncall` at commit `9bc2b687`). Low activity, so not candidates to run, but safe to read and to port from with the notice kept. Verdict: ok.

## 5. A native first-party on-call plugin

Scope assumed: schedules, rotations, overrides, escalation policies, contact methods and notification rules, an on-call lookup, and notification delivery through `notifier` contributions.

### 5.1 What may be used

| Source | May be read | May be ported or copied | Obligation |
|---|---|---|---|
| GoAlert | yes | yes | Apache-2.0 section 4: keep notices, mark changed files, attribute in `NOTICE` |
| LinkedIn Oncall, Iris | yes | yes | BSD-2-Clause: keep the copyright notice and licence text |
| Public documentation of commercial products (PagerDuty, Opsgenie, the IRM service) | for behaviour only | no text, no screenshots, no API reference prose, no sample payloads | Their documentation is covered by their terms. The PagerDuty Developer Agreement counts documentation as part of the licensed services and forbids copying it and using it to build a competitive product |
| OnCall OSS (archived) | not by the implementers | never | AGPL-3.0 and a proprietary licence file |
| iCalendar (RFC 5545) for calendar export | yes | the format is an open standard | None. A Go library for it is not chosen; its licence is UNVERIFIED |

### 5.2 What it must not copy

1. Any source file, SQL schema, migration, GraphQL or REST schema file, message template, translated string or UI asset from the archived OnCall OSS.
2. The API surface of a commercial product reproduced field for field so that their clients work against us. Interoperability by imitation of an API is a legal question in itself (question 6), and the PagerDuty agreement forbids replicating the user experience of its products.
3. Product names used as feature names. "Escalation policy", "rotation", "override", "schedule" and "on-call" are generic; vendor feature names and UI wording are not ours to reuse.
4. Logos, icons and notification sounds of any upstream.

### 5.3 Is a schedule algorithm or data model encumbered

Copyright does not protect the idea of a rotation, an override or an escalation step, only a particular expression of it. The data model as an idea is free; a copied schema is not.

Patents are the open point. PagerDuty holds a patent family on schedule management:

| Patent | Title | Dates as shown by Google Patents | Source |
|---|---|---|---|
| US8762190B1 | Realtime schedule management interface | priority 2012-12-21; granted 2014-06-24; status shown "Active"; anticipated expiration 2033-03-15 | https://patents.google.com/patent/US8762190B1/en |
| US8768737B1 | Schedule management interface | same priority date; granted 2014-07-01 | https://patents.google.com/patent/US8768737 |
| US9830578 | Realtime schedule management interface | granted 2017-11-28 | https://patents.justia.com/assignee/pagerduty-inc |

Google Patents states that its status "is an assumption and is not a legal conclusion". Claim 1 of US8762190B1, read from the page, requires all of the following together: a schedule with at least one layer that has a team member, a start time, a precedence value and a shift duration; an edited layer generated from an existing one; "epochs" computed from the period over which the same allocation repeats; a "virtual start time" found by jumping forward by epochs; schedule entries generated from it; a layer preview in a view port; comparison of entries across all layers ordered by precedence; and a final schedule preview in the view port. The claims of the other two patents and of later family members were not read (UNVERIFIED), and the wider PagerDuty portfolio was not surveyed.

Engineering reading, not a legal opinion. The claim is narrow and tied to an editing interface with live previews. A design with layered schedules resolved by precedence and a live preview while editing is the closest to it. GoAlert models a schedule as rules that assign rotations or users to weekday and time ranges, plus temporary schedules and overrides (add, remove, replace), which is a different structure (`schedule/rule/rule.go`, `schedule/temporaryschedule.go` and `override/` on `master`), in a code base whose licence file carries the copyright years 2019 to 2021. Following that model, and computing shifts without the epoch shortcut the claim describes, keeps distance. The Apache-2.0 patent grant of GoAlert covers only patents held by its contributors, not third-party patents. Question 7.

Verdict: ok-with-conditions. The conditions are the lists in 5.1 and 5.2 and an answer to question 7 before a layered schedule editor is designed.

## 6. Consequences for the plan records

These follow from decisions B and C and from the findings above. They are inputs to the next edit of the records, not edits made here.

| Record text today | Finding |
|---|---|
| 00 §3.2 `oncall-goalert` "On-call engine"; "We write no paging code" | Licence allows it. Decision B makes it one provider row; the wording changes, the licence position does not |
| 00 §2.1 row 3 and §7.3: hosted tenants above 40 use `on-call-provider-pagerduty` | Depends on a revocable developer licence with a non-compete clause. Remove the dependency |
| 00 §7.3 `on-call-provider-opsgenie`; 00 §2.1 note "Opsgenie's reported April 2027 sunset is UNVERIFIED" | Verified: end of support 2027-04-05. Drop the item |
| 00 §2.1 row 4: self-host through `aiopsctl slack manifest` in Socket Mode | Fine for a free community edition. Not sufficient for a paid edition without a partner agreement |
| 00 profiles `self-host-full`, `aiops-only`, `hosted` compose `chatops-slack` | Keep it optional: no row outside the bundle may need it, and the first-party surface is the default |
| 08 §5.1: the archived OnCall OSS "may be read for data-model ideas" | Tighten to public documentation only for the people who write the native plugin |
| 08 §5.2 naming rule | Add: third-party marks appear in ids only in the referential suffix form; `slack-commands` is renamed; no upstream logos in the index |
| Plugin gate | Add: a third-party plugin never receives a chat-platform token or raw API access |

## 7. Questions for a lawyer

1. Slack, open core. Customers of the free community edition each create an internal Slack app from our manifest. Once a paid enterprise edition, a paid hosted service or paid marketplace plugins exist, is that template "Commercially Distributed" under the API Terms of Service ("a free App that connects to a paid product or service", "a 'freemium' model", "Application template")? Is each customer-created app "created for use only by a single third party"? Who is the contracting developer of such an app, the customer or us?
2. Slack, paid self-host. Socket Mode apps cannot be listed in the Marketplace. Is a partner agreement with Slack or Salesforce the only route for a paid self-hosted edition, and what does it cost and require?
3. Slack, paid third-party plugins. If a paid plugin contributes commands or messages through our Slack app, does that make our app commercially distributed, and does it breach the Developer Policy rule against "allowing access to the Slack APIs to anyone else" even when the plugin never holds the token? What must a marketplace agreement with publishers say?
4. Slack data. Is storing the text of an incident thread in the incident timeline, and sending it to a tenant-chosen model for inference, within "the minimum necessary"? What authorisation text is required at install? Does the Marketplace "zero-copy" guideline for agent apps bind our hosted app?
5. Marks in identifiers. Are plugin ids, bundle names, image names and chart keys that contain `slack`, `goalert`, `twilio`, `pagerduty`, `ntfy` or `mattermost` nominative use? The ntfy terms require prior written consent for use "in connection with any product or service", Mattermost requires prior written approval, PagerDuty allows its marks only after acceptance and publication of an application, and Twilio forbids combining its marks with another name. Does a marketplace that lists third-party plugins need a naming policy and a takedown procedure, and are we liable for publishers' names?
6. PagerDuty. Given "internal business purposes" in section 2(a) and sections 4(j) and 4(k), may a project that ships on-call and incident investigation functions write and distribute a provider plugin for PagerDuty? May a third party sell one? Does a customer's own subscription cover its use of a community plugin with its own API key? Is implementing a compatible API shape permitted?
7. Patents. Freedom to operate for a native schedule engine against US8762190B1, US8768737B1, US9830578 and later family members: which designs (layers with precedence, live preview while editing, epoch-based computation) are inside the claims, and does a rule and override model in the style of GoAlert stay outside? In which countries does the family exist?
8. Porting from GoAlert. Confirm the notice and attribution duties for code translated from GoAlert into a first-party plugin, and that such a plugin may later sit inside a closed enterprise build.
9. Clean-room line for the archived OnCall OSS. Is reading its source for ideas safe for people who then write our schema, or should access be limited to its public documentation? What follows from the conflicting licence signals in `grafana-plugin/`?
10. Twilio in hosted. Are we a reseller or an ISV under section 2.2(b) and (d) of the Terms of Service? Which sender registrations must we perform per tenant and per country? Which emergency-services disclaimers must reach tenants? How is liability for a tenant's breach of the Acceptable Use Policy limited in our own terms?
11. Image redistribution. If we mirror upstream images or ship an air-gapped bundle, are we the distributor of everything inside each image (base image packages under GPL-2.0, bundled sounds under Creative Commons Attribution), and which source offers follow, compared with referencing upstream images by digest?

## 8. Source log

All read on 2026-11-11.

| Subject | Source | Version, commit or page date |
|---|---|---|
| GoAlert licence | https://github.com/target/goalert/blob/master/LICENSE.md ; https://api.github.com/repos/target/goalert/license | `master` `a47084204362`; blob `dead3101` |
| GoAlert release, image | https://api.github.com/repos/target/goalert/releases ; https://hub.docker.com/r/goalert/goalert/tags | v0.35.0, 2026-09-21 |
| GoAlert contribution terms | https://github.com/target/goalert/blob/master/CONTRIBUTING.md ; https://github.com/target/.github/blob/main/dco.yml ; pull request 4603 | `master` as above |
| OnCall OSS (archived) | https://api.github.com/repos/grafana/oncall (redirects to `grafana-cold-storage/oncall`) ; `LICENSE` ; `grafana-plugin/LICENSE` ; `grafana-plugin/package.json` ; `README.md` | `dev` `af0fbd40558c`; archived 2026-03-24 |
| slack-go | https://github.com/slack-go/slack/blob/master/LICENSE ; releases API | `master` `5d5455cb26de`; v0.31.0 of 2026-10-26; v0.35.0 of 2026-11-05 |
| Slack API terms | https://slack.com/terms-of-service/api | Effective October 10, 2025 |
| Slack terms update | https://docs.slack.dev/changelog/2025/05/29/tos-updates/ | 2025-05-29 |
| Slack Developer Policy | https://docs.slack.dev/developer-policy/ | Effective December 10, 2024 |
| Slack Marketplace Agreement | https://slack.com/terms-of-service/slack-marketplace | Last modified July 11, 2016 |
| Slack Marketplace guidelines, review guide | https://docs.slack.dev/slack-marketplace/slack-marketplace-app-guidelines-and-requirements ; https://docs.slack.dev/slack-marketplace/slack-marketplace-review-guide | undated |
| Socket Mode | https://docs.slack.dev/apis/events-api/using-socket-mode | undated |
| Slack brand terms | https://slack.com/terms-of-service/slack-brand ; https://slack.com/media-kit | undated |
| Twilio terms | https://www.twilio.com/en-us/legal/tos | July 16, 2026 |
| Twilio Acceptable Use Policy | https://www.twilio.com/en-us/legal/aup | May 12, 2025 |
| Twilio trademark guidelines | https://www.twilio.com/en-us/legal/trademark | March 20, 2025 and September 8, 2025 |
| Twilio ISV guidance | https://www.twilio.com/docs/messaging/compliance/a2p-10dlc/onboarding-isv | undated documentation |
| twilio-go | https://api.github.com/repos/twilio/twilio-go | MIT |
| ntfy | https://github.com/binwiederhier/ntfy (README, `LICENSE`, `LICENSE.GPLv2`) | `main` `a331a5c8db86`; v2.37.2 |
| ntfy.sh terms | https://docs.ntfy.sh/terms/ | Last updated January 26, 2026 |
| PagerDuty Developer Agreement | https://docs.pagerduty.com/developer/pagerduty-developer-agreement | Last Modified September 30, 2026 |
| go-pagerduty | https://api.github.com/repos/PagerDuty/go-pagerduty | Apache-2.0; `main` `e4de2e38c6fa` |
| Opsgenie end of life | https://www.atlassian.com/licensing/opsgenie | page as served |
| opsgenie-go-sdk-v2 | https://api.github.com/repos/opsgenie/opsgenie-go-sdk-v2 | Apache-2.0; last push 2024-07-12 |
| Mattermost | https://github.com/mattermost/mattermost/blob/master/LICENSE.txt | `master` as served |
| Alertmanager | https://api.github.com/repos/prometheus/alertmanager/license | Apache-2.0; blob `261eeb9e` |
| LinkedIn Oncall, Iris | https://api.github.com/repos/linkedin/oncall ; https://api.github.com/repos/linkedin/iris | BSD-2-Clause |
| PagerDuty patents | https://patents.google.com/patent/US8762190B1/en ; https://patents.google.com/patent/US8768737 ; https://patents.justia.com/assignee/pagerduty-inc | pages as served |

UNVERIFIED items, collected: licences of GoAlert's Go and npm dependencies and of the contents of its image; trademark registration of the GoAlert name; a CLA for slack-go; Twilio's emergency-services limitation and any clause on subaccounts; whether documentation terms bind a project that only publishes an integration; API terms of Opsgenie's successor, of the IRM service, of Teams and of Mattermost beyond its licence file; the claims of the PagerDuty patents other than claim 1 of US8762190B1; the licence of a Go iCalendar library.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
