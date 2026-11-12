# Licence audit: bases of the pillar plugins and their content

Date read: 2026-11-11. Scope: the upstream tools and the third-party content that the pillar plugins (Compliance, SecOps, FinOps, Change, Continuous Improvement, estate) sit on or could sit on.

This is engineering analysis, not legal advice. Section 8 lists the questions a lawyer must answer before the affected item ships.

## 1. Method

- Every licence fact for a GitHub project was read today from the GitHub REST API (`/repos/{repo}`, `/repos/{repo}/license`, `/repos/{repo}/contents/...`) at the default branch. Each row gives the commit and the latest release tag. Where the API reported no SPDX id, the licence file itself was read.
- Content terms were read from the owner's own page where the page could be read. Where it could not (the page returned 403 or rendered only through a script), the row says UNVERIFIED and names what was read instead.
- The earlier research notes were used as leads only.
- The five uses the licences must allow:
  - **U1** community edition: an Apache-2.0 repository and release artifacts (binary, images, Helm chart, compose file) that bundle or pull the upstream.
  - **U2** our hosted multi-tenant service running the upstream unmodified.
  - **U3** a later commercial enterprise edition on the same core, with closed-source plugins in the same binary or as separate processes.
  - **U4** third parties selling plugins that wrap or call the upstream.
  - **U5** the upstream's name in a plugin id or description.
- Four ways of using an upstream, kept apart throughout:
  - **network**: we call a service the customer (or we) run, or the customer's tool pushes its output to us. No upstream code or image is in our artifacts.
  - **image**: we ship or mirror the unmodified upstream image or binary, as a separate process.
  - **link**: upstream code is compiled into our binary or imported by a plugin process.
  - **copy**: upstream content (rules, control text, mappings, data) is copied into our repository or artifacts.

## 2. Answer in short

1. **No software base in this group has to be rebuilt from scratch.** Every scanner, GitOps controller and cost tool the plan names is Apache-2.0 or MIT, and the plan uses all of them at arm's length: the customer runs them and their output reaches us over the network.
2. **One software base must be replaced, not rebuilt:** Terraform (BUSL-1.1). Choose OpenTofu (MPL-2.0) wherever a plugin needs an engine it ships or hosts.
3. **Four tools may exist only as optional plugins the user installs,** never in the core or a default bundle: k6 (AGPL-3.0), Ansible (GPL-3.0), Wazuh (GPL-2.0, moving to AGPL-3.0 for its engine), ScoutSuite (GPL-2.0). They may be called over the network or run as an unmodified separate process. They may never be linked.
4. **The real conflict is content, not code.** CIS Benchmarks, CIS Controls, the SOC 2 criteria and ISO/IEC 27001 cannot be shipped as text in any edition, and CIS also forbids a non-member from claiming a level of compliance with a benchmark. The control catalogue, its wording and its mappings must be written first-party. This is the one thing that must be built from scratch (section 6).
5. **The scanners themselves ship that restricted content.** Kubescape's `cis-*` and `soc2` frameworks and Prowler's `cis_*`, `iso27001_*` and `pci_*` compliance files carry text derived from those sources inside Apache-2.0 repositories. The repository licence cannot grant rights the content owner did not give. Copying those files into our repository, or showing their text in a paid hosted screen, imports the problem. Rule L17 of the licence register ("the scanner's own result text") is therefore too wide and needs the change in section 7.
6. **Two facts changed since the research notes:** the PolicyReport API repository is retired (archived 2025-05-21; the successor is the OpenReports API), and Wazuh's main branch now licences its engine under AGPL-3.0-or-later (commits of 2026-10-14 and 2026-10-15).
7. Nothing in this group depends on the agent harness or on the on-call provider, so owner decisions A and B do not change any verdict here.

## 3. Software bases

All GitHub sources below were read on 2026-11-11. "Head" is the commit of the default branch at reading time.

Shared condition sets, referred to by letter in the table:

- **P (permissive, used over the network).** U1: nothing of the upstream is in our artifacts; a chart or compose file may name the upstream image by tag and digest. If we mirror the image or put it in an offline bundle, the bundle carries the upstream LICENSE and NOTICE (Apache-2.0 section 4, MIT notice). U2: allowed, no network clause. U3: allowed, including linking and closed plugins beside it. U4: allowed. U5: referential use only (section 5).
- **C (copyleft, separate process only).** U1: never in the core or a default bundle; an optional plugin calls it over the network or starts the unmodified binary the user installed. If any artifact of ours contains the binary, that artifact carries the licence text and a source offer for it. U2: allowed unmodified; a modified AGPL service must offer its source to its network users. U3: never in the same binary as closed code; separate process only. U4: allowed on the same terms; the seller carries the same duties. U5: referential use only.

| Component | Role in the plan | Use mode | Licence (verified) | Source | Verdict | Conditions | Risk |
|---|---|---|---|---|---|---|---|
| Kubescape | MVP compliance input; scanner in the customer estate (00 §7.1 step 5, 05 §7) | network: customer runs it, a pusher posts results | Apache-2.0 (`kubescape/kubescape`, `kubescape/regolibrary`, `kubescape/operator`, `kubescape/helm-charts`) | github.com/kubescape/kubescape head 90cbf064de, v4.0.20; regolibrary head 2bfe87670a | ok-with-conditions | Set P. Extra: do not copy `regolibrary/frameworks/cis-*.json` or `soc2.json` into our repository, and do not show the control names and descriptions of those frameworks in any edition (they carry CIS and AICPA derived wording). Kubescape's own control ids (`C-0xxx`) and its own frameworks are Apache-2.0 and may be shown with attribution | Foundation hosted. The framework files mix Apache-2.0 material with third-party derived text; the repository licence does not clear the latter |
| Trivy | P2 SecOps findings; scanner in the customer estate | network | Apache-2.0 (`aquasecurity/trivy`); checks MIT (`aquasecurity/trivy-checks`); DB builder Apache-2.0 (`aquasecurity/trivy-db`); `aquasecurity/vuln-list` has no licence file | trivy head 0e40f8178a, v0.76.2; trivy-checks head e0152428dc, v2.3.0; trivy-db head 6748d23156 | ok-with-conditions | Set P. Extra: never mirror or redistribute the vulnerability database; the customer's scanner pulls it. The DB aggregates feeds with their own terms and its data repository carries no licence | Single vendor (Aqua), no foundation. The Trivy docs mark every language source as usable commercially and show a one-month delay on one source; the OS feeds have no stated terms on that page |
| Trivy Operator | P2 SecOps and compliance input (ADR-018, `signal-ingest-trivy-report`) | network | Apache-2.0 | github.com/aquasecurity/trivy-operator head 689215dd79, v0.36.1 | ok | Set P. Parsing its report kinds needs no code of theirs; if we import its Go API types, that is Apache-2.0 and linkable | Same single vendor |
| Kyverno | Scanner in the customer estate; source of PolicyReports | network | Apache-2.0 (`kyverno/kyverno`, `kyverno/policies`); `kyverno/policy-reporter` MIT | kyverno head ce5feb4095, v1.19.2 | ok | Set P. Sample policies from `kyverno/policies` may be copied with the Apache-2.0 notice | Foundation hosted |
| PolicyReport API (`wgpolicyk8s.io`) | Wire shape of `signal-ingest-policyreport` | link (Go types) or own parser | Apache-2.0, repository retired and archived | github.com/kubernetes-retired/wg-policy-prototypes head ed0ca5a1c7, last push 2025-05-21, archived | ok-with-conditions | Licence is fine for all five uses. Condition is technical: the API has moved to the OpenReports API (`openreports/reports-api`, Apache-2.0, head ea7940de3f, v0.2.1). The ingest row must accept both groups or the plan pins a dead API | Upstream repository is dead; the id `signal-ingest-policyreport` names a format, not a trademark |
| Prowler | Later compliance input through the generic JSON row (05 §7) | network: customer runs the CLI | Apache-2.0; no separate licence file in `ui/`, `api/`, `mcp_server/`, `dashboard/` | github.com/prowler-cloud/prowler head c62c973391, 5.48.0 | ok-with-conditions | Set P. Extra: do not copy `prowler/compliance/**` (it holds `cis_*`, `iso27001_*`, `pci_*` files); map on Prowler check ids only | Single vendor with a paid cloud. Same mixed-content problem as Kubescape |
| OpenCost | FinOps: runs in the customer cluster, the agent scrapes its metrics | network (scrape) | Apache-2.0 | github.com/opencost/opencost head 17daaa72c2, v1.121.4 | ok | Set P | Foundation hosted; main contributor is a commercial vendor |
| krr | FinOps: container `rightsizing-runner`, weekly job (00 §3.2) | image: unmodified, separate process we ship | MIT | github.com/robusta-dev/krr head e70b9fb36f, v1.31.3 | ok | U1: our image carries the MIT notice and the notices of its Python dependencies; the L7 allow-list must run over that image too. U2 to U4: allowed. U5: referential | Single vendor. Python dependency tree not audited here |
| Falco | Held alternative to the chosen runtime sensor (00 §7.3) | image, if ever | Apache-2.0 (`falco`, `rules`, `libs`); `libs/driver/` is MIT OR GPL-2.0 (libs `NOTICES`) | falco head 2be5618cad, 0.45.2; rules head e7b066bfbc; libs head 0815626f9f | ok | Set P. Elect MIT for the driver sources. Rules may be copied with notice | Foundation hosted |
| Tetragon | SecOps sensor (00 §2.1 row 10); listed here because the pillar depends on it | image: pulled from upstream | Apache-2.0; `bpf/` is GPL-2.0-only OR BSD-2-Clause | github.com/cilium/tetragon head d17dc9c47c, v1.8.0 | ok | Set P. Register row L11 already elects BSD-2-Clause for `bpf/` | Foundation hosted |
| Argo CD | Change pillar: sends notification webhooks; we ship templates | network; our own templates | Apache-2.0 (`argo-cd`, `notifications-engine`) | github.com/argoproj/argo-cd head a7ca968804, v3.6.1 | ok | Set P. Templates we write are ours | Foundation hosted |
| Flux | Change pillar: notification-controller webhooks | network | Apache-2.0 (`flux2`, `notification-controller`) | github.com/fluxcd/flux2 head dfcd91066e, v2.10.0 | ok | Set P | Foundation hosted |
| Kargo | Not in the architecture set; a promotion source a change plugin could read | network | Apache-2.0 | github.com/akuity/kargo head 5c53640f90, v1.12.5 | ok | Set P | Single vendor with a paid hosted offer |
| Semaphore UI | Not in the architecture set; a candidate executor controller | network: REST API of a controller the customer runs | MIT; `pro/` in the repository holds open-source stubs, the Pro features are a separate proprietary build | github.com/semaphoreui/semaphore head fc21c63b26, v2.20.23 | ok-with-conditions | Set P. Extra: no row, test or default may need a Pro feature (register L9) | Single vendor, open-core |
| AWX | Not in the architecture set; a candidate executor controller | network: REST API of a controller the customer runs | Apache-2.0 | github.com/ansible/awx head f1bf572171, last release 24.6.1 | ok-with-conditions | U1: API client only; never bundle the AWX image (it contains GPL-3.0 Ansible and the project has paused releases). U2: we do not host it. U3, U4: an API client may be closed. U5: referential; AWX logos have their own trademark guidelines | README: last release 2024-07-02, releases paused during a refactoring. Upstream of a commercial product |
| Ansible (ansible-core) | Not in the architecture set; a candidate executor engine | separate process only: `ansible-playbook` or ansible-runner started as a child, or reached through a controller's API | GPL-3.0 (`COPYING`); `ansible/ansible-runner` Apache-2.0. The BSD-2-Clause status of `module_utils` was not re-read today: UNVERIFIED | github.com/ansible/ansible head b4b2b1e82a, v2.22.0; ansible-runner head 2a425b8976, 2.4.3 | optional-plugin-only | Set C. No plugin of ours imports `ansible.*` Python packages. The plugin starts the user's own install, or an executor image that is separate from every default image and carries the GPL-3.0 text and source offer | GPL-3.0 has no network clause, so hosting is not the problem; linking and bundling are |
| OpenTofu | Not in the architecture set; the IaC engine to choose when a plugin needs one | image (unmodified CLI) or network | MPL-2.0 | github.com/opentofu/opentofu head 0fb8b8f8a9, v1.13.9 | ok-with-conditions | U1: unmodified, with a NOTICE line and source URL (register L8); any changed MPL file is published under MPL-2.0. U2, U3, U4: allowed, also linked next to closed code, because MPL-2.0 is file-level. U5: referential, foundation trademark | Foundation-hosted continuation of the earlier MPL-2.0 Terraform code; MPL-2.0 is already on the L7 allow-list |
| Terraform | Named only as a protocol reference and as a later plan-JSON source | none shipped | BUSL-1.1, licensor IBM, Additional Use Grant excludes hosted or embedded competitive offerings | github.com/hashicorp/terraform head 3de4756fc1, v1.17.1, `LICENSE` | replace-or-rebuild | Blocks U1 (not an open-source licence, fails L7), and puts U2, U3 and U4 at the mercy of the "competitive offering" test for any paid product that embeds or hosts it. Reading plan JSON a customer's own Terraform produced is not use of the Licensed Work. Choose OpenTofu for anything we ship or host | Relicensed from MPL-2.0 in 2023; the licence file now names IBM |
| k6 | Not in the architecture set; a candidate load or synthetic-check tool | separate process only | AGPL-3.0; `k6-operator` Apache-2.0 | github.com/grafana/k6 head 0ef8873124, v2.3.0, `LICENSE.md` | optional-plugin-only | Set C. Never import `go.k6.io/k6` or build an extension into our binary: that makes the binary AGPL and ends U1 and U3. The vendor's trademark policy forbids its marks in a product or service name | Single vendor, AGPL. Scripts a user writes are the user's |
| Sloth | Continuous Improvement: SLO spec rendered to rules (P3) | link (Go library) or build tool, or own renderer of the spec | Apache-2.0 | github.com/slok/sloth head dfab41f326, v0.16.0 | ok | Set P; linking allowed | Single maintainer |
| Pyrra | Same role, the alternative | link, or the customer runs it | Apache-2.0 | github.com/prometheus-community/pyrra head f0beef0cee, v0.10.2 (moved from `pyrra-dev`) | ok | Set P; linking allowed | Now under a community organisation |
| NetBox | Estate: an inventory API read and vocabulary alignment (09 §9) | network: REST or GraphQL API of an instance the customer runs | Apache-2.0 | github.com/netbox-community/netbox head 28755a9d4c, v4.7.5, `LICENSE.txt` | ok | Set P. Naming a NetBox term in our vocabulary export is a reference, not a copy of its code | Single vendor with paid cloud and enterprise features; none is needed |
| osquery | Not in the architecture set; a candidate host inventory reader | image (unmodified daemon) or network | Apache-2.0 OR GPL-2.0-only, at the user's choice | github.com/osquery/osquery head 3c6d178493, 5.24.1, `LICENSE` | ok | Set P; elect Apache-2.0 and record the election | Foundation hosted |
| Wazuh | Named only as an excluded copyleft tool (register L10) | network: API of a manager the customer runs | Release 4.14.10: GPL-2.0. Main branch: engine AGPL-3.0-or-later, shared libraries GPL-2.0-only OR AGPL-3.0-or-later, the rest GPL-2.0. `wazuh-dashboard` and `wazuh-indexer` Apache-2.0 | github.com/wazuh/wazuh head c5fe976ec7, `LICENSE`; LICENSE commits 223cb6bc69 (2026-10-14) and 960ba646d6 (2026-10-15) | optional-plugin-only | Set C. Never copy its rules or decoders into our repository | Licence is changing on the main branch; single vendor |
| ScoutSuite | Named only as an excluded copyleft tool (register L10) | network or the user's own CLI run | GPL-2.0 | github.com/nccgroup/ScoutSuite head 7909f2fc61, 5.14.0, last push 2025-09-23 | optional-plugin-only | Set C | Low activity |
| Chaos Mesh | Lab fault engine (09 §9, ADR-027) | build or test tool only | Apache-2.0 | github.com/chaos-mesh/chaos-mesh head 8e5a702ee4, v2.8.8 | ok | Set P | Foundation hosted |
| KEDA | One option for scale-to-zero (ADR-014) | image, pulled from upstream | Apache-2.0 | github.com/kedacore/keda head 2132ca760b, v2.21.0 | ok | Set P | Foundation hosted |
| Backstage catalog format | Estate: tenant catalog file format and vocabulary alignment | own parser of a documented format | Apache-2.0 | github.com/backstage/backstage head 0a160f3ed4, v1.56.4 | ok | Set P. If the JSON schemas are copied, keep the Apache-2.0 notice | Foundation hosted |
| Atlantis | Later change source (plan JSON) | network | Apache-2.0 | github.com/runatlantis/atlantis head dcbc0e21be, v0.49.0 | ok | Set P | Foundation hosted |
| Renovate | Later change source (pull request events from the forge) | network: we receive forge events only | AGPL-3.0 | github.com/renovatebot/renovate head 83794a8993, 44.227.4 | ok-with-conditions | We never run, ship or link it. If a plugin ever starts it, set C applies and the verdict becomes optional-plugin-only | AGPL, single vendor |
| Apache Jena | CI parse check of the vocabulary export (09, ADR-029) | build or test tool only | Apache-2.0 | github.com/apache/jena head 3dbd5b9775 | ok | None beyond set P | Foundation hosted |
| open-ontologies | Not in the architecture set, where Apache Jena is the working choice | build or test tool only, if used at all | MIT | github.com/fabio-rovai/open-ontologies head 38457467c2, v2.1.0, `LICENSE` | ok-with-conditions | Tool use only. Do not copy the third-party ontology files it carries (they have their own terms, not checked). Nothing in the core may need it | Single maintainer, copyright dated 2026, a hosted commercial platform beside it |

## 4. Content with its own terms

| Content | Role in the plan | Terms (verified) | Source | Verdict | What a plugin may ship |
|---|---|---|---|---|---|
| CIS Benchmarks | Framework behind the compliance score (register L17) | PDF: CC BY-NC-SA 4.0, plus the non-member terms: no sale or exploitation "in any way or for any purpose", no posting on a website, no derivative work "based directly on" a benchmark "or any component thereof", no claim of "a particular level of compliance or consistency". Commercial integration is licensed only through the SecureSuite Product Vendor membership, which requires CIS Benchmarks Certification | cisecurity.org/terms-of-use-for-non-member-cis-products and cisecurity.org/cis-securesuite/pricing-and-categories/product-vendor, read 2026-11-11, no revision date on either page | replace-or-rebuild | Text and recommendation titles: no, in any edition. Mappings: not in first-party plugins until a lawyer or a membership clears them. Identifiers: only as an opaque string a customer's scanner emitted. No score labelled with the CIS name |
| CIS Controls | Not named in the plan; appears in Prowler's files | CC BY-NC-ND 4.0, same non-member terms | Same page, read 2026-11-11 | replace-or-rebuild | Same as above; NoDerivatives also rules out an adapted list |
| MITRE ATT&CK | Technique tags on SecOps detections (register L17) | Royalty-free licence "for research, development, and commercial purposes"; every copy must reproduce MITRE's copyright designation and the licence. ATT&CK is a registered trademark | github.com/mitre-attack/attack-stix-data `LICENSE.txt`, head a8db2f52eb, v20.1; attack.mitre.org terms of use, read 2026-11-11 | ok-with-conditions | Identifiers, names, mappings and description text: yes, with the designation and the licence text in `THIRD_PARTY_LICENSES/` and in any export that carries the text. No endorsement claim |
| NSA and CISA Kubernetes Hardening Guidance | Source of the Kubescape NSA framework | UNVERIFIED: the PDF at media.defense.gov returned 403 today. Secondary excerpts show a disclaimer of endorsement, a trademark section and a copyright section that credits the Kubernetes documentation (Creative Commons) for examples and figures | media.defense.gov/2022/Aug/29/2003066362/-1/-1/0/CTR_KUBERNETES_HARDENING_GUIDANCE_1.2_20220829.PDF (not readable); search excerpts, 2026-11-11 | unverified | Until read: section references and our own wording only. Expected outcome is that the agencies' own text is free to reuse and the borrowed examples and figures are not |
| SOC 2 Trust Services Criteria | Framework mapping (Kubescape `soc2`); a compliance agent would cite it | "© 2024 American Institute of Certified Public Accountants. All rights reserved." The document also carries "©2019, Committee of Sponsoring Organizations of the Treadway Commission (COSO). All rights reserved." SOC 2 is a registered trademark of the AICPA | TSP Section 100, 2017 criteria with revised points of focus 2022, PDF from the AICPA download page, read 2026-11-11 | replace-or-rebuild | Criteria text and points of focus: no. Criterion identifiers (for example `CC6.1`) as references next to our own wording: yes as the working rule, lawyer to confirm. No "SOC 2 compliant" claim |
| ISO/IEC 27001 | Framework mapping (Prowler `iso27001_2022`) | Proprietary, sold per copy. UNVERIFIED at the primary source: iso.org copyright pages returned 403 today. A secondary copy of the standard's notice requires prior written permission for reproduction | iso.org/copyright.html (not readable), 2026-11-11 | replace-or-rebuild | Control text and titles: no. Control numbers (for example `A.8.9`) as references: working rule yes, lawyer to confirm |
| NVD | Vulnerability enrichment | Data: public domain in the United States. API terms ask for the notice "This product uses the NVD API but is not endorsed or certified by the NVD." and forbid implying endorsement. Read through a search index excerpt of the official page, because the page itself did not render for this client: partly UNVERIFIED | nvd.nist.gov/developers/terms-of-use, 2026-11-11 | ok-with-conditions | All fields: yes. Show the notice wherever NVD data is shown. In hosted, one API key and a cache, never a per-tenant fan-out against the public rate limit |
| CVE List | Identifier of every vulnerability finding | Perpetual, royalty-free licence to reproduce, adapt, sublicense and distribute, provided MITRE's copyright designation and the licence are reproduced in any copy | github.com/CVEProject/cve-website `src/views/Legal/TermsOfUse.vue`, head 1f14847571, read 2026-11-11 | ok-with-conditions | Identifiers and record text: yes, with the designation and licence |
| CISA KEV catalog | Priority flag on vulnerability findings | CC0-1.0; no use of the agency logo or seal, no endorsement claim | cisa.gov/sites/default/files/licenses/kev/license.txt, read 2026-11-11 | ok | Everything |
| OSV | Vulnerability feed | Code and schema Apache-2.0. Data keeps the licence of each source: mostly CC-BY-4.0, CC0-1.0, MIT, Apache-2.0; Ubuntu and Alpine CC-BY-SA-4.0; Chainguard and Wolfi CC-BY-NC-ND-4.0 or a separate commercial scanner licence; Debian and the NVD conversion show no licence on the page | google.github.io/osv.dev/data/ and github.com/google/osv.dev head 407cb662ce, read 2026-11-11 | ok-with-conditions | Query the API at run time: yes. A redistributed or cached copy must filter by source licence: drop the NonCommercial source in any paid edition, carry attribution for CC-BY sources, and keep ShareAlike data out of anything we relicense |
| endoflife.date | End-of-life dates for estate components | Site and data MIT (`endoflife-date/endoflife.date`, `endoflife-date/release-data`). Product descriptions are adapted from Wikipedia under CC BY-SA 3.0. No terms or rate limit could be read from the API page (script-rendered): UNVERIFIED | github.com/endoflife-date/endoflife.date head 10eeeff365, `LICENSE` and `README.md`, read 2026-11-11 | ok-with-conditions | Product names, cycles and dates: yes, with the MIT notice. Product descriptions: no |
| NIST SP 800-53 and OSCAL content | Not in the plan; proposed base of the first-party control catalogue | Public domain in the United States and CC0-1.0 worldwide | github.com/usnistgov/oscal-content head f0de6d8d8c, v1.6.0, `LICENSE.md`, read 2026-11-11 | ok | Identifiers, text and mappings |
| ComplianceAsCode content | Not in the plan; a second permissive source of rule wording | BSD-3-Clause | github.com/ComplianceAsCode/content head f0d40648d1, v0.1.82, `LICENSE`, read 2026-11-11 | ok-with-conditions | Its own rule text: yes with the BSD notice. Its profiles that track CIS or other restricted sources: not checked, treat as restricted |

## 5. Naming (use 5)

- The Linux Foundation trademark page (read 2026-11-11, no revision date) covers most projects in section 3. It allows "<your product name> for <mark>", "<your product/company name> plug-in for <mark>" and "compatible with <mark>". It says a mark "should not be used as part of your product name", forbids "<mark> <your product name>", and forbids implying endorsement. Individual projects may add rules.
- Red Hat's guidelines (read 2026-11-11) forbid its marks "as part of your own ... product name, service name" and allow them only after a verb or preposition that states the relationship. This covers Ansible. AWX is not mentioned on that page; the AWX README points to separate logo guidelines.
- The vendor of k6 forbids its marks "as part of your product or service name" and asks for an attribution line (trademark policy read 2026-11-11). Whether k6 is on its mark list was not read: UNVERIFIED.
- Trademark pages of the single-vendor projects (Trivy, Prowler, Kargo, Semaphore UI, NetBox, Wazuh, krr) were not read: UNVERIFIED.
- Working rule for every edition and for marketplace listings:
  1. A plugin's display name leads with our or the seller's name and uses the form "X connector for <upstream>".
  2. A plugin id may contain the upstream's word as a descriptive token (`signal-ingest-trivy-report`), never its logo, and never the name of a vendor whose policy forbids its marks in a product name.
  3. No listing says "certified", "official" or "compliant" about a third-party mark.
  4. The marketplace terms pass the same three rules on to sellers.
- Whether rule 2 is safe is a lawyer question (section 8, Q6).

## 6. Reasoning for every verdict that is not plain ok

**CIS Benchmarks and CIS Controls: replace-or-rebuild.**
- U1 is blocked: NonCommercial ShareAlike text cannot sit in an Apache-2.0 repository, because Apache-2.0 lets every recipient use the work commercially.
- U2, U3 and U4 are blocked: the non-member terms forbid exploiting the product "in any way or for any purpose", forbid a derivative work based on a benchmark or any component of it, and forbid claiming a level of compliance or consistency with it. A per-framework score labelled with the CIS name is such a claim.
- The only licensed route is the Product Vendor membership with certification. That is a business decision with a recurring fee and a certification step, and it would cover us, not another community distribution and not a third-party seller.
- What we build instead: a first-party control catalogue (own ids, own wording) on NIST SP 800-53 and OSCAL content, ATT&CK, and Kubescape's own controls. The score is "controls passed in the <our catalogue name> profile". A CIS-labelled view can come later as an enterprise plugin if the owner buys the membership.

**SOC 2 criteria and ISO/IEC 27001: replace-or-rebuild.**
- Both are all-rights-reserved. Shipping criteria or control text blocks U1 to U4 alike.
- What we build instead: catalogue entries in our own words that cite the criterion or control number. A compliance agent may reason about a control and cite its number; its prompts, skills and fixtures must not embed the source text.

**Terraform: replace-or-rebuild.**
- BUSL-1.1 is outside the L7 allow-list, so U1 fails. For U2 to U4 the grant excludes hosted or embedded use in an offering that competes with the licensor's paid product; an approval-gated change executor with drift detection is close enough to that product that the question should not be left open.
- What we choose instead: OpenTofu as the engine a plugin ships or hosts. A plugin may still read plan JSON, or start a Terraform binary the customer installed and licensed.

**k6, Ansible, Wazuh, ScoutSuite: optional-plugin-only.**
- Linking any of them into the kernel or into a plugin loaded in the kernel process puts the whole binary under GPL or AGPL. That ends the Apache-2.0 release (U1) and the closed plugin in the same binary (U3).
- Shipping one in a default bundle makes every default release carry a source offer for it, and for AGPL a duty to offer source of any modification to network users in hosted (U2).
- They stay usable: as an optional plugin that calls a service the user runs (Wazuh API, an AWX or Semaphore UI controller in front of Ansible) or starts the unmodified binary the user installed (k6, `ansible-playbook`, ScoutSuite). The plugin's own code stays Apache-2.0 or closed.
- Consequence for the core: any default synthetic check, executor or host detection must be first-party or permissive. The plan already has its own executors and a permissive sensor, so nothing needs rebuilding today.

**Kubescape and Prowler: ok-with-conditions.**
- The tools are fine in every use. The condition is on their bundled framework files, as section 2 item 5 explains. A result a customer's scanner produced for a `cis-*` framework carries CIS-derived control names. Storing it for the customer is processing their data. Publishing those names in a paid hosted screen, or copying the framework file into our repository, is redistribution of the derived text.
- Working rule: an allow-list of frameworks whose text may be shown. On it: the scanner's own control ids and names from its own frameworks, the NSA framework wording once the guidance is verified, and ATT&CK with its notice. Off it: `cis-*`, `soc2`, `iso27001_*`, `pci_*`, and `ism` until its terms are read. For frameworks off the list we show our catalogue entry and the scanner's rule id only.

**Trivy: ok-with-conditions.** The scanner is Apache-2.0. The vulnerability database is an aggregate whose data repository has no licence and whose sources keep their own terms, so we never mirror or bundle it; the customer's scanner pulls it from the vendor.

**PolicyReport API: ok-with-conditions.** Licence is clean. The repository is retired, so the ingest row must also accept the OpenReports group. This is a design fix, not a licence one.

**Semaphore UI and AWX: ok-with-conditions.** Both are clean when called over the API of a controller the customer runs. Semaphore UI is open-core, so no row may need a Pro feature. AWX has paused releases and its image contains GPL-3.0 Ansible, so it is never bundled.

**OpenTofu: ok-with-conditions.** MPL-2.0 is file-level and already allowed by L7 and L8: unmodified, with a NOTICE line; changed files are published.

**Renovate: ok-with-conditions.** We only receive forge events about its pull requests. The condition is that this stays true.

**MITRE ATT&CK, NVD, CVE, OSV, endoflife.date, ComplianceAsCode, open-ontologies: ok-with-conditions.** Each allows commercial use and redistribution; each has an attribution or notice duty, or a part that is not covered (the NonCommercial and ShareAlike sources inside OSV, the Wikipedia-derived descriptions in endoflife.date, the third-party files inside open-ontologies). Section 4 gives the exact line for each.

**NSA and CISA guidance: unverified.** The primary document could not be read today. Until it is, treat it like restricted content: references and own wording only.

## 7. What must be built or changed

Build first-party:

1. **Control catalogue and framework mappings** for the Compliance pillar and for any compliance agent: own ids, own wording, based on NIST SP 800-53 and OSCAL content (CC0), ATT&CK (with notice) and Kubescape's own controls (Apache-2.0). No CIS, SOC 2 or ISO text. Cross-references to those sources are numbers only and stay behind the lawyer answers in section 8.
2. **The framework display allow-list** in the control evaluator (section 6, Kubescape and Prowler).
3. **A source-licence filter** on any vulnerability data we cache or redistribute from OSV.

Replace:

4. **Terraform with OpenTofu** wherever a plugin ships or hosts an IaC engine.
5. **The retired PolicyReport API pin** with a parser that accepts `wgpolicyk8s.io` and the OpenReports group.

Changes proposed to the licence register (08 §5.1):

- **L7:** add BUSL-1.1 for Terraform by name to the list of excluded examples (it is already outside the allow-list).
- **L10:** widen from "Wazuh, ScoutSuite" to "k6, Ansible, Wazuh, ScoutSuite and any other GPL or AGPL tool: optional plugin, separate process or network only, never in a default bundle".
- **L17:** replace "the scanner's own result text" with "the scanner's rule id and, only for frameworks on the display allow-list, its result text". Add: no score or view carries the CIS, SOC 2 or ISO name. Record that the CIS terms were read on 2026-11-11 and that ATT&CK needs its copyright designation and licence text shipped.
- **New row:** vulnerability and end-of-life data keep their source licence; `trivy-db` is never mirrored; OSV copies are filtered by source.

## 8. Questions a lawyer must answer

- **Q1. CIS identifiers.** May a product ship a mapping from its own control ids to CIS recommendation numbers (numbers only, no titles), given the non-member terms' ban on derivative works based on "any component" of a benchmark? Does it matter whether we ever accepted those terms by downloading a benchmark?
- **Q2. CIS compliance claims.** Is a pass-rate over controls that a customer's scanner tagged with a CIS framework a "claim of a particular level of compliance or consistency", when the hosted or enterprise product displays it? Is it still one when the label is the scanner's framework id and not the CIS name?
- **Q3. Pass-through of scanner output.** When a customer's Kubescape or Prowler run emits CIS, SOC 2 or ISO derived control names and our hosted service stores and displays them back to that customer only, is that redistribution by us? Does the answer change in the self-hosted community edition, where we host nothing?
- **Q4. CIS membership.** If the owner wants a CIS-labelled view in the enterprise edition: does the Product Vendor membership cover (a) the open-source community edition, (b) third-party sellers on our marketplace, (c) a self-hosted enterprise customer? Which artifacts need CIS Benchmarks Certification?
- **Q5. SOC 2 and ISO numbers.** May the catalogue cite criterion and control numbers (`CC6.1`, `A.8.9`) beside our own wording? May it cite the short titles? Does the COSO copyright inside the criteria add a second owner to clear?
- **Q6. Names in ids.** Is an upstream word mark inside a machine id (`signal-ingest-trivy-report`, a plugin id containing `kubescape`, `ansible` or `k6`) "part of a product name" under the Linux Foundation, Red Hat and vendor policies? Does the answer change when a third party sells the plugin on our marketplace, and does the marketplace operator carry liability for a seller's listing?
- **Q7. Ansible boundary.** Is starting `ansible-playbook` or ansible-runner as a child process from a closed-source plugin "mere aggregation" under GPL-3.0? Are playbooks, roles and inventories our plugin generates independent works? Does shipping an executor image that contains ansible-core beside a closed plugin image in one Helm chart change either answer?
- **Q8. AGPL in hosted.** If an optional plugin in the hosted service starts unmodified k6 on behalf of a tenant, does AGPL-3.0 section 13 put any duty on us beyond offering the unmodified source? Does a k6 extension written by a seller change that?
- **Q9. Terraform.** Is an approval-gated executor plugin that starts a customer-installed Terraform binary an "embedded" or "hosted" use under the Additional Use Grant when the plugin is sold, or when it runs in our hosted service?
- **Q10. OSV and vulnerability data.** May a paid edition or the hosted service cache and re-serve OSV records from CC-BY-SA sources next to closed content? Is dropping the CC-BY-NC-ND source enough, or does querying the public API on behalf of paying tenants already count as commercial use of that source's records?
- **Q11. Trivy database.** If the hosted service ever runs a scanner itself, on what terms may it pull and use the vendor's vulnerability database for paying tenants, given that the data repository has no licence?
- **Q12. Government guidance.** Outside the United States, is the NSA and CISA guidance free to reproduce, and which parts (examples, figures) belong to third parties?
- **Q13. Mixed-content repositories.** Does copying an Apache-2.0 framework file from a scanner's repository into ours give us any defence if the file contains text its publisher had no right to sublicense?

## 9. Not verified today

- ISO copyright terms at iso.org (403).
- The NSA and CISA Kubernetes Hardening Guidance notices (403).
- The NVD API terms page itself (read only through a search index excerpt of the official page).
- endoflife.date API terms and rate limits (script-rendered page).
- The exact text of the CIS Product Vendor membership agreement, its price and its certification scope (only the public product page was read).
- Trademark pages of Trivy, Prowler, Kargo, Semaphore UI, NetBox, Wazuh and krr; whether k6 is on its vendor's mark list; AWX's separate logo guidelines.
- BSD-2-Clause status of Ansible `module_utils`.
- Licences of the dependency trees inside the krr image and inside any upstream image; this audit covers the top-level licence only.
- The terms of Kubescape's `ism` framework source and of the third-party ontology files inside open-ontologies.
- Foundation maturity levels and contributor agreements of each project.

## 10. Sources

Read on 2026-11-11.

- GitHub REST API, repositories and commits as listed in sections 3 and 4.
- https://www.cisecurity.org/terms-of-use-for-non-member-cis-products
- https://www.cisecurity.org/cis-securesuite/pricing-and-categories/product-vendor
- https://www.cisecurity.org/cis-securesuite/cis-securesuite-membership-terms-of-use (index page only)
- https://www.cisecurity.org/cis-benchmarks/cis-benchmarks-faq
- https://attack.mitre.org/resources/legal-and-branding/terms-of-use/
- https://www.aicpa-cima.com/resources/download/2017-trust-services-criteria-with-revised-points-of-focus-2022 and the PDF it serves (TSP Section 100)
- https://www.cisa.gov/known-exploited-vulnerabilities-catalog and https://www.cisa.gov/sites/default/files/licenses/kev/license.txt
- https://google.github.io/osv.dev/data/ and `docs/new_source.md` in `google/osv.dev`
- https://trivy.dev/latest/docs/scanner/vulnerability/
- https://www.linuxfoundation.org/legal/trademark-usage
- https://www.redhat.com/en/about/trademark-guidelines-and-policies
- https://grafana.com/trademark-policy/
- https://nvd.nist.gov/developers/terms-of-use (search index excerpt only)
- Not readable today: https://www.iso.org/copyright.html, https://media.defense.gov/2022/Aug/29/2003066362/-1/-1/0/CTR_KUBERNETES_HARDENING_GUIDANCE_1.2_20220829.PDF, https://endoflife.date/docs/api/v1/

## 11. Verifier corrections applied 2026-11-11

An adversarial verifier read the primary sources of this audit again on 2026-11-11. Its eight findings are applied in the licence record (`docs/design/licence-audit-2026-11.md`), which wins where it and the sections above differ. The sections above are left as audited. This is engineering analysis, not legal advice.

| # | Component | What the audit said | Correction applied | Where |
|---|---|---|---|---|
| 1 | Kubescape | Only `frameworks/cis-*.json` and `soc2.json` carry derived text; its own control ids and frameworks may be shown, and its own controls are a base of the first-party catalogue | The CIS-derived wording sits in `controls/` under Kubescape's own ids (about 132 of 295 files reference cisecurity.org, roughly `C-0092` upward, at 2bfe87670a), and `allcontrols` includes them. Verdict stays ok-with-conditions. The display allow-list works per control id and provenance, not per framework; the catalogue takes only controls with no third-party reference, behind a provenance filter with a test | Licence record §2.5, §2.6, §3, §7.4, Q13 |
| 2 | Trivy; Trivy Operator | Trivy conditions cover the vulnerability database only; Trivy Operator plain ok | Trivy Operator becomes ok-with-conditions. Both ship CIS-derived compliance specs and checks (`trivy-checks/pkg/compliance/*cis*`, the operator's `specs/*cis*`, check titles such as `KCV-0048`): never copied, off the display allow-list. `k8s-nsa-*` is held until the NSA and CISA notice is read | §2.5, §7.4 |
| 3 | Wazuh | Set C, which allows an artifact that contains the binary with a source offer | optional-plugin-only stays, in network mode only. No artifact of ours or of a seller contains or installs Wazuh: its LICENSE extends to rules, decoders and data files and counts an application that puts it into a proprietary installer as a derivative work. A new lawyer question | §2.5, §4, §7.6, Q16 |
| 4 | Tetragon | Plain ok, on the BSD-2-Clause election for `bpf/` | Already ok-with-conditions in the licence record. Added: `bpf/COPYING` grants the dual licence only "unless otherwise specified", several files are GPL-2.0-only, the BPF objects are treated as GPL-2.0 material in any mirror, and no `bpf/` source is copied into a closed plugin | §2.3, §5 rule 5, Q12 |
| 5 | krr (`rightsizing-runner`) | Plain ok; "the customer runs" every tool (section 2 item 1) | Already ok-with-conditions in the licence record: we ship or pull it, and an image we build carries the base-layer duties, the MPL-2.0 line for certifi and an SBOM. Section 2 item 1 should read "all except krr, which we ship and, in hosted, run". Every image-mode row carries the base-layer duty when mirrored | §2.2, the mode table |
| 6 | Terraform | A plugin may start a Terraform binary the customer installed (section 6) | replace-or-rebuild stands. Starting a customer-installed binary is cleared for the free community edition only: "Embedded" also covers a paid offering packaged so that Terraform must be accessed or downloaded for it to operate. A paid or hosted plugin works fully with OpenTofu as its default engine | §2.5, §3, §5 rule 7, §7.6, Q17 |
| 7 | Naming (use 5) | k6: referential use; foundation marks: the "for <mark>" forms | k6, use 5, is not cleared: its vendor's policy has no carve-out for naming a plugin after a tool, so no id, title or listing carries the name or the logo. The Linux Foundation usage page also says a mark should not be combined, hyphenated or abbreviated, so a hyphenated id that embeds a listed mark waits for the lawyer. OpenTofu, Tetragon and Pyrra are not on that list: UNVERIFIED | §7.5, Q4 |
| 8 | Coverage | Not applicable | No defect. All 58 repositories and the content terms matched. Still not readable: the NSA and CISA guidance, the NVD terms page, iso.org | §9 |

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
