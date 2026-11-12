# Licence audit: the agent and AI layer

Date: 2026-11-11. Every licence fact below was read on 2026-11-11 at the primary source named in section 9 (the LICENSE file at a tag or at the head of the default branch, the GitHub licence API, the package index metadata, or the publisher's own terms page). Anything not read that way is marked UNVERIFIED. This is engineering analysis, not legal advice; section 8 lists the questions a lawyer must answer.

Scope: HolmesGPT with its toolsets, skills, prompts and its LiteLLM dependency; LiteLLM; Ollama; vLLM; the MCP specification and its Go and Python SDKs; the MCP server behind the plan's `assistant-mcp` container; krr; Robusta; k8sgpt; DeepSeek Harness (dsh) and Cordis; use of an OpenAI-compatible API; open-weight model licences; and the agent harnesses a contributor might wrap as a harness plugin.

Owner decisions this audit assumes: the agent layer is first-party and new; HolmesGPT is one optional agent-harness plugin that a user may pick as the harness of the incident, compliance or FinOps agent; nothing in the core depends on it.

## 1. Answer in short

1. No component the plan names in this group has a licence that forces a rebuild. HolmesGPT (Apache-2.0), krr (MIT), vLLM (Apache-2.0), Ollama (MIT), the MCP specification and SDKs (Apache-2.0 with MIT remainders, MIT), dsh and Cordis (MIT) all allow the five uses, with conditions listed in section 4.
2. Two artefacts next to named components are not usable and must never enter an artifact of ours. Both already have a first-party replacement in the plan or an obvious one:
   - LiteLLM's `enterprise/` directory and the `litellm-enterprise` package (proprietary; production use needs a paid BerriAI licence). The official LiteLLM proxy image installs it. The replacement is the first-party `llm-gateway`, which the plan already chose.
   - The LangGraph Platform server (`langgraph-api`, Elastic-2.0), if a contributor ever wraps LangGraph. The replacement is our own remote-plugin host around the MIT library.
3. Four things are acceptable only as something a user installs, never in a default bundle or the hosted default: the LiteLLM proxy as a server, the Claude Agent SDK, dsh itself as a wrapped harness, and open-weight models whose licence has a Model-as-a-Service clause.
4. The decision to treat HolmesGPT as an optional harness plugin is not required by its licence, and its licence does not get in the way of it. It does remove one indirect exposure: with HolmesGPT out of the core, LiteLLM is no longer a transitive dependency of the default path.

## 2. The five uses and the use modes

Uses: U1 community edition (Apache-2.0 repository, binary, images, chart, compose file that bundle or pull the upstream); U2 our hosted multi-tenant service running the upstream unmodified; U3 a later commercial enterprise edition on the same core, with closed plugins in the same binary or as separate processes; U4 third parties selling plugins that wrap or call the upstream; U5 the upstream's name in a plugin id or description.

Use modes, strictest last:

| Mode | What it means | What it triggers |
|---|---|---|
| Network | We or the customer call a service the customer runs | Nothing under a software licence; the service's terms, if any |
| Pull | Our chart or compose file references the upstream image by digest; the user's machine pulls it from the upstream registry | No redistribution by us; we ship the upstream licence text beside the chart |
| Ship | An image, mirror or offline bundle of ours contains the upstream | Redistribution: every notice obligation of every layer in that image, including the base image |
| Link | Upstream code is compiled into our binary or imported by our Python package | Redistribution plus the licence must be on the dependency allow-list |
| Copy | Upstream files or text sit in our repository | Attribution and licence text in the repository; for Apache-2.0, a note of changes |

## 3. Table

| Component | Role in the plan | Use mode | Licence verified today | Verdict |
|---|---|---|---|---|
| HolmesGPT 0.46.0 (code, toolsets, skills, prompts, chart) | Named as the MVP investigator in the canonical record; by owner decision now one optional agent-harness plugin | Pull, unmodified image, driven over HTTP by an in-process Go client. Copy for recorded tool lists and toolset ids in connector packs | Apache-2.0, one LICENSE file for the whole tree, no NOTICE file. Contributions under DCO sign-off, no CLA | ok-with-conditions |
| LiteLLM SDK 1.103.5 as a dependency of HolmesGPT | Reaches the model from inside the HolmesGPT image | Inside the upstream image only; we do not link it | MIT. The wheel excludes `litellm/proxy/enterprise`; HolmesGPT's lock file has no `litellm-enterprise` | ok-with-conditions |
| LiteLLM proxy (server, official image, `litellm[proxy]`) | A proxy the customer runs in front of other vendors; "no embedded LiteLLM" | Network | MIT for the core; the `proxy` extra and the official image install `litellm-enterprise` (LicenseRef-Proprietary) | optional-plugin-only |
| LiteLLM `enterprise/` directory and `litellm-enterprise` package | Not used by the plan | None allowed | BerriAI Enterprise licence: production use only with a paid subscription; copying, distributing and selling forbidden | replace-or-rebuild |
| Ollama | Customer-run model server; local model in the lab | Network; Pull in the lab only | MIT (v0.40.16 line, head of `main`) | ok-with-conditions |
| vLLM | Customer-run model server; local model in the lab | Network; Pull in the lab only | Apache-2.0 (head of `main`) | ok-with-conditions |
| MCP specification | Protocol of the tool facade on `/v1/tools`, of `tool-mcp-bridge` and of `assistant-mcp` | Implement the protocol; Copy only if schema files are taken | In transition: Apache-2.0 for new and relicensed contributions, MIT for the rest, CC-BY-4.0 for documentation | ok-with-conditions |
| MCP Go SDK | Not named by the plan; the likely library for the Go facade | Link, inside a plugin, not inside `sdk/kernel` | Same transition notice as the specification (GitHub reports NOASSERTION) | ok-with-conditions |
| MCP Python SDK | The Python SDK for remote plugins; also a HolmesGPT dependency | Link in Python plugins | MIT | ok |
| Dashboard MCP server (container `assistant-mcp`) | Read-only MCP endpoint for a customer's own AI client, P2, on request | Pull, unmodified, `--disable-write` | Apache-2.0 (v2.3.0 line, head of `main`); contributions need a CLA | ok-with-conditions |
| krr 1.31 | `rightsizing-runner`, a weekly scheduled job | Pull or Ship, unmodified separate process | MIT (tag v1.31.3) | ok |
| Robusta (engine and playbooks) | Research lead only: borrow the trigger and action shape, not the runtime. Named in no architecture file | None; Copy only if playbook text were taken | MIT (0.54.0 line) | ok |
| k8sgpt | Research lead only. Named in no architecture file | None; a contributor could wrap it | Apache-2.0 (v0.4.41), DCO | ok |
| DeepSeek Harness (dsh) as the model we follow | The architecture the plugin system follows; no code copied | Ideas only | MIT, Copyright 2026 DeepSeek | ok |
| Cordis 4.1.0 and cordis-plugin-loader 1.1.0 | The model ported to Go as `sdk/kernel`; vendored as a test-only oracle | Copy (test tree only); the port is a reimplementation | MIT, Copyright 2021-present Shigma | ok-with-conditions |
| The Cordis paper (arXiv 2608.25512) | Describes the model | Ideas only | arXiv non-exclusive distribution licence 1.0: no reuse right for the text or figures | ok-with-conditions |
| OpenAI-compatible API | The surface of `llm-gateway` and the shape of `llm-provider-openai-compatible` | Implement the interface; Network to the customer's upstream | No licence applies to an interface we implement ourselves. `openai/openai-openapi` MIT and `openai/openai-go` Apache-2.0 by the licence API only | ok-with-conditions |
| Open-weight models under Apache-2.0 or MIT | The local model of the lab run; a customer's own model | Weights are never in an artifact of ours; the lab pulls them | Apache-2.0: Qwen3.8-27B, gemma-4, gpt-oss, granite-4.2, Mistral-Small-4. MIT: DeepSeek-V4 | ok |
| Llama family weights | As above | As above | Llama 3.3 Community License; Llama 4 under its own licence (`llama4`), text not read | ok-with-conditions |
| Open-weight models with a Model-as-a-Service clause | As above | As above | Qwen Community License 1.0 (Qwen3.8-Flash-Next); Mistral Ecosystem Open-Weight License (Mistral-Large-4) | optional-plugin-only |

Harnesses a contributor might wrap are in section 5.

## 4. Conditions per use, and the reasoning for every verdict that is not plain ok

### 4.1 HolmesGPT: ok-with-conditions

Verified: tag 0.46.0 (commit 586045cae6, 2026-11-04) carries one Apache-2.0 LICENSE with "Copyright 2025 Robusta Dev LTD" and no NOTICE file; no other licence file exists anywhere in the tree, so the toolsets (45 entries under `holmes/plugins/toolsets`), the skills (`holmes/plugins/skills`, which replaced the runbooks directory; there is no `runbooks` directory at this tag), the prompts and the Helm chart are all Apache-2.0. CONTRIBUTING asks for `git commit -s` (DCO). It is a CNCF sandbox project by its own README.

| Use | Condition |
|---|---|
| U1 | Pull, do not ship. The image is built on `python:3.11-alpine` and adds `kubectl`, `argocd` and `helm`; an image, mirror or offline bundle of ours that contains it redistributes every one of those layers, including the GPL userland of the base image, and takes on their notice and source obligations. If we only reference the upstream image by digest, we ship the Apache-2.0 text beside the chart and nothing else. Anything copied into our repository (recorded tool lists, toolset ids, tool descriptions, prompt text, chart templates) keeps an attribution line, the Apache-2.0 text, and a note of what we changed |
| U2 | Allowed. Apache-2.0 has no network clause. One instance per tenant or shared is an engineering choice, not a licence one |
| U3 | Allowed. It runs as a separate process; nothing of it is linked into our binary, so a closed plugin in the same binary is unaffected |
| U4 | Allowed. A seller that redistributes the image or the code takes on the same notice duties; a seller that only calls it takes on none |
| U5 | The mark is not licensed by Apache-2.0 (section 6). Working rule from the Linux Foundation trademark policy read today: our name first, "plugin for HolmesGPT" as description, no logo, no implication of endorsement. Whether the policy covers this mark is UNVERIFIED |

Risk: low. DCO without a CLA means no single party can relicense the existing code; foundation governance makes a licence change unlikely. Two engineering notes that are not licence terms: the `robusta` and `robusta_platform_mcp` toolsets and the `supabase` and `sentry-sdk` dependencies exist to reach a commercial service, so they stay off in our rendered configuration; the image pins `litellm`, see 4.2.

### 4.2 LiteLLM SDK inside HolmesGPT: ok-with-conditions

Verified: the root LICENSE says everything under `enterprise/` is under `enterprise/LICENSE`, everything else MIT. At tag v1.103.5 the build configuration excludes `litellm/proxy/enterprise` (a symlink to `../../enterprise`) from the wheel, and `litellm-enterprise` is pulled only by the `proxy` extra. HolmesGPT 0.46.0 depends on `litellm = "1.103.5"` with no extra, and its lock file contains no `litellm-enterprise`. PyPI reports MIT for `litellm` 1.103.5.

Condition: this holds only while HolmesGPT depends on the base package. Add one release check on the pinned HolmesGPT version: fail if its lock file names `litellm-enterprise` or a `litellm[proxy]` extra. If we ever copy LiteLLM's price file (`model_prices_and_context_window.json`) as the static price table of `llm-gateway`, that is a Copy of MIT content and needs the MIT notice; the plan does not say where the table comes from.

Risk: medium. Single vendor, open-core, a CLA on every contribution (so BerriAI can relicense), a release cadence of several tags per day, and a codebase that is moving to a Rust core. All of that sits behind HolmesGPT and, after the owner's decision, behind an optional plugin.

### 4.3 LiteLLM proxy as a server: optional-plugin-only

The official image is built with `--extra proxy`, which installs `litellm-enterprise` (LicenseRef-Proprietary on PyPI). The enterprise licence text forbids copying, distributing and selling that software and allows production use only under a paid subscription. An artifact of ours that contained or mirrored the official image would redistribute it.

| Use | Condition |
|---|---|
| U1 | Network only. No profile, chart or compose file of ours pulls or ships the proxy image. The plan already says the customer runs it |
| U2 | We do not run the official proxy image in the hosted service. If hosted ever needs a vendor proxy, build one from the base MIT package without the `proxy` extra, or extend `llm-gateway` |
| U3, U4 | A plugin may call a LiteLLM the customer runs. A seller that wants enterprise features needs its own agreement with BerriAI |
| U5 | Descriptive use only |

### 4.4 LiteLLM `enterprise/` and `litellm-enterprise`: replace-or-rebuild

Blocks U1 (cannot be distributed), U2 and U3 (production use needs a paid seat licence), U4 (cannot be sold). What we build instead is already in the plan: the first-party `llm-gateway` with its own virtual keys, budget and metering. Rule for contributors: nothing is ported, copied or paraphrased from `enterprise/`.

### 4.5 Ollama and vLLM: ok-with-conditions

Both are permissive (MIT, Apache-2.0) and both are used over the network on the customer's side, or pulled by the lab, which never ships.

Conditions: (a) keep them Network or lab-only. Their GPU images bundle vendor GPU runtime libraries (vLLM's final image is based on `nvidia/cuda`; Ollama's build installs the CUDA toolkit and ROCm), which come under the GPU vendor's own redistribution terms, not under MIT or Apache-2.0. Do not mirror them or put them in an offline bundle without reading those terms; that reading was not done today (UNVERIFIED). (b) The weights a server pulls keep their own licence (section 4.9); a model library entry is not a licence grant from the server project. (c) Ollama's terms page (updated August 2026) defines its "Service" as "website, software, APIs, and related services" and reserves the brand; whether those terms reach the MIT binary taken from the repository is a lawyer question.

Risk: Ollama is a single vendor with a paid cloud offer and no foundation; vLLM is a foundation-style community project. Neither is on a path the product requires.

### 4.6 MCP specification and Go SDK: ok-with-conditions

Verified: the LICENSE of both repositories opens with a transition notice. New contributions and those whose authors consented are Apache-2.0; contributions whose authors have not consented remain MIT; documentation that is not specification is CC-BY-4.0. The specification's README still says "MIT License". The Python SDK's LICENSE is plain MIT.

Conditions: (a) implementing the protocol needs no licence. (b) If we copy the schema files, or link the Go SDK, ship both the Apache-2.0 and the MIT text with the transition notice. (c) Licence scanners return NOASSERTION for both repositories, so our allow-list check fails until the pair is recorded as a reviewed exception. (d) Do not copy the CC-BY-4.0 documentation into a shipped artifact; link to it. (e) Keep the SDK out of `sdk/kernel`, which has no `require` line anyway.

All five uses are allowed. U5: "MCP" and "Model Context Protocol" are used descriptively; ownership of the marks is UNVERIFIED.

### 4.7 Dashboard MCP server (`assistant-mcp`): ok-with-conditions

Apache-2.0, run unmodified as its own container. Conditions: Pull, not Ship; ship the licence text beside the chart. U5 is the real constraint: the upstream's name contains its vendor's mark, the plan already keeps that mark out of every product, plugin and row name, and the hosted service needs either a trademark licence or mark-free wording. The container id `assistant-mcp` complies. Risk: single vendor with a CLA, and a vendor that has relicensed other projects before; a later release could change terms, so the release check pins the tag and its licence.

### 4.8 dsh, Cordis and the paper: what "port the model, copy no code" means

- **An MIT project.** MIT lets anyone use, copy, modify, sublicense and sell. Its one condition attaches to "copies or substantial portions of the Software": keep the copyright and permission notice. It grants nothing on names and says nothing on patents.
- **Ideas in a paper.** Copyright covers the paper's words and figures, not the model it describes. The paper is on arXiv under the non-exclusive distribution licence, which gives readers no right to reuse its text. So: implement freely, cite it, and do not paste its text or figures into our documentation.
- **Why the verdict still carries a condition.** The port is written with the source open: the plugin record maps kernel parts to named source files, quotes line numbers, pins to 4.1.0 and compares behaviour in a differential test. A close translation of source code into another language can be a derivative work even when no line is copied. Whether ours is one is a legal question. The cost of not having to answer it is one notice: carry the Cordis MIT notice ("Copyright (c) 2021-present Shigma") in `THIRD_PARTY_LICENSES/` for `sdk/kernel` and say in the package documentation that it is a Go port of the Cordis model. MIT inbound is compatible with Apache-2.0 outbound, and with a closed enterprise edition.
- **The test oracle.** Vendoring Cordis 4.1.0 and the loader 1.1.0 is a Copy. Keep each LICENSE file as shipped. The copy we pin is the one republished under the `@deepseek-ai` scope from the `vendor/` directory of the dsh repository, which records local modifications; the upstream `cordiverse/cordis` release line is at v4.0.0-rc.12. Record that provenance next to the vendored tree. It is test-only and never enters a shipped binary.
- **Names.** "Cordis", "dsh" and "DeepSeek" stay out of our product, kernel and plugin names. "A Go port of the Cordis model" in documentation is descriptive.
- **Patents.** MIT has no patent grant. Whether the authors or their employers filed on the method is UNVERIFIED and is a lawyer question.

All five uses are allowed for the model and for the port.

### 4.9 OpenAI-compatible API: ok-with-conditions

We implement a wire format and call an endpoint the customer chose. No upstream code is involved unless we take a client library (`openai/openai-go`, Apache-2.0 by the licence API) or copy the published OpenAPI description (`openai/openai-openapi`, MIT by the licence API); either would need its notice. Conditions: write "OpenAI-compatible" only as a description of the interface, never as a product, plugin or row name beyond the generic provider row, and use no logo. Calling a vendor's hosted API is governed by that vendor's service terms between the vendor and whoever holds the key: the customer on self-host, us on hosted. On hosted, passing tenants' requests to a vendor under our key needs that vendor's terms to allow it; the terms of the vendors were not read today (UNVERIFIED), apart from Anthropic's in section 5.

### 4.10 Open-weight models

No weights are in any artifact of ours: the lab pulls them, the release notes name the model and its licence, and a customer chooses its own. The licence therefore binds whoever runs the model: the customer on self-host, us in the lab and on hosted.

| Family and example read today | Licence | Commercial use | What it means for us |
|---|---|---|---|
| Qwen: `Qwen/Qwen3.8-27B` | Apache-2.0 | Unrestricted | Fine for the lab and hosted |
| Qwen: `Qwen/Qwen3.8-Flash-Next` | Qwen Community License 1.0 | Allowed, but a licensee that "conducts a Model as a Service or AI Work Assistant business" needs a separate licence before any commercial use; above 100 million monthly users or USD 20 million monthly revenue the model name must be shown | Not for hosted, not the lab default. The largest model, `Qwen3.8-2.4T-A95B`, is under yet another licence (`qwen3.8-max`), not read |
| Gemma: `google/gemma-4-31B-it-qat-w4a16-ct` | Apache-2.0 | Unrestricted | Fine. Gemma 3 and earlier are under the Gemma terms (tag `gemma`), not read today |
| Llama: `meta-llama/Llama-3.3-70B-Instruct` | Llama 3.3 Community License, 2024-12-06 | Allowed. Redistribution needs a copy of the agreement, a notice file, "Built with Llama" shown prominently; a model improved with it must be named "Llama..."; the acceptable use policy is part of the licence; a licensee above 700 million monthly users must ask Meta; no trademark licence | Usable in the lab and by customers. Not an open-source licence, so it is not what the release notes should present as the open default. Llama 4 has its own licence (`llama4`), not read |
| Mistral: `mistralai/Mistral-Small-4-119B-2603` | Apache-2.0 | Unrestricted | Fine |
| Mistral: `mistralai/Mistral-Large-4-1T-A52B` | Mistral Ecosystem Open-Weight License | Allowed, but a licensee that "operates a Model as a Service business" needs a commercial licence. The definition excludes "end-user products with model capabilities solely embedded within specific features or harnesses" and internal use | Probably outside the clause, but not for hosted until a lawyer says so |
| DeepSeek: `deepseek-ai/DeepSeek-V4-Flash-0731` | MIT | Unrestricted | Fine |
| OpenAI: `openai/gpt-oss-20b`; IBM: `ibm-granite/granite-4.2-8b` | Apache-2.0 | Unrestricted | Fine |

Reasoning for optional-plugin-only on the Model-as-a-Service rows: our hosted service gives tenants a chat and an investigation that run on a model we host. Whether that is "giving a third party access to language model inference in a manner that allows such third parties to exercise meaningful control over the inputs" is exactly what the clause leaves open. Permissive weights exist in the same families, so the hosted service and the lab default use only Apache-2.0 or MIT weights, and a customer who self-hosts decides for itself. The licence tag is per model, not per family: the lab's compatibility matrix needs the tag of the exact repository and revision.

## 5. Agent harnesses a contributor might wrap as a harness plugin

A harness plugin is a remote plugin: the harness runs in its own process and our plugin speaks to it. The licence question is therefore whether that process may be shipped by us, shipped by a seller, or only installed by the user.

| Harness | Licence verified today | Resale and hosted-use terms | Verdict |
|---|---|---|---|
| HolmesGPT | Apache-2.0 | None | ok-with-conditions (4.1) |
| dsh, through `dsh --profile acp` or `--profile sdk` | MIT for dsh's own packages. Its install closure is not all permissive: its notices file lists `@anthropic-ai/claude-agent-sdk` and the Claude Code platform payloads ("SEE LICENSE"), a LibreOffice kit under MPL-2.0, and the installed tree holds LGPL-3.0-or-later image libraries | The Claude Code payloads are under Anthropic's Commercial Terms (below). The dsh notices file says its owner authorises distribution of those payloads; that authorisation is the dsh project's, not ours | optional-plugin-only: the user installs dsh; no image or bundle of ours contains it |
| Agent Client Protocol (the wire protocol dsh's `acp` profile speaks) | Apache-2.0 by the licence API; text not read | None known | ok |
| Claude Agent SDK, Python | Repository LICENSE is MIT, but the README states that use is governed by Anthropic's Commercial Terms of Service, and the wheel bundles the Claude Code CLI | Commercial Terms (effective 2025-06-17): may "power products and services Customer makes available to its own customers and end users"; may not "resell the Services except as expressly approved", build a competing product, or reverse engineer. The documentation adds that a third party may not offer claude.ai login or rate limits and must use API keys. Branding: "Claude Agent" or "Powered by Claude" allowed, "Claude Code" not | optional-plugin-only |
| Claude Agent SDK, TypeScript | No open licence: "All rights reserved", Commercial Terms | As above | optional-plugin-only |
| OpenAI Agents SDK (Python) | MIT | None in the licence. Calls to OpenAI's API fall under OpenAI's service terms (not read); the SDK also drives other providers | ok |
| LangGraph (library, checkpointers, CLI, Python SDK) | MIT; PyPI `langgraph` 1.2.20 reports MIT | None | ok |
| LangGraph Platform server (`langgraph-api` 0.17.1) | Elastic-2.0 by PyPI metadata | Elastic-2.0 forbids offering the software to third parties as a hosted or managed service and is outside our dependency allow-list (standard licence text, not re-read today) | replace-or-rebuild |
| Pydantic AI | MIT | None | ok |
| Google ADK (Python, Go), Microsoft Agent Framework, CrewAI, OpenHands, goose, opencode, Codex CLI, Eino | Apache-2.0, MIT, MIT, MIT, Apache-2.0, MIT, Apache-2.0, Apache-2.0 by the GitHub licence API only | LICENSE text, sub-directory licences and service terms not read: UNVERIFIED | unverified |

Reasoning for the verdicts that are not ok:

- **Claude Agent SDK.** The wrapper code a contributor writes can be Apache-2.0, but what it drives is a proprietary executable under commercial terms. It blocks U1 as a default (we cannot redistribute the CLI in an Apache-2.0 bundle, and the community index lists open licences only), and it conditions U2 and U4 (resale needs Anthropic's approval; each operator needs its own API key and agreement). As a plugin a user installs, with the user's own key, it is acceptable.
- **dsh as a harness.** dsh's own code is MIT and may be shipped. The problem is what a normal dsh install pulls in. A harness plugin that says "install dsh, then enable me" is fine; an image of ours with dsh inside is not, unless someone first builds a profile whose closure is clean and proves it with a licence scan.
- **LangGraph Platform server.** Blocks U1 (not on the allow-list) and U2 (hosted-service limitation). What we do instead: a contributor's plugin imports the MIT library and runs it in the remote-plugin host we already provide.

## 6. Naming (U5), one rule for the whole group

No licence in this group grants a trademark. Apache-2.0 says so in section 6; MIT is silent; the Llama licence and the Ollama terms reserve their marks explicitly. The working rule, taken from the Linux Foundation trademark policy read today ("A trademark should not be used as part of your product name"; correct form: "<your product/company name> plug-in for <mark>"):

- A plugin id is an identifier under our own owner segment. It may end in the upstream's lower-case name where that is the only way to say what the plugin talks to. Its title is ours first: "Agent harness plugin for HolmesGPT".
- No upstream logo in the product or the index. No "official", "certified" or "powered by" unless the upstream's own guideline offers that wording.
- The vendor mark of the default dashboard stack stays out of every product, plugin and row name, as the plan already requires.
- A third-party seller follows the same rule, and the index's listing terms should say so when the marketplace is built.

Whether an id such as `harness-holmesgpt` counts as "part of your product name" is a lawyer question.

## 7. What changes in the plan

1. Add the release check of 4.2 (no `litellm-enterprise`, no `proxy` extra in the pinned HolmesGPT lock file) to the release checks of the licence section.
2. Record the MCP specification and Go SDK as a reviewed dual-licence exception in the allow-list, with both texts shipped.
3. Add the Cordis MIT notice for `sdk/kernel` and the provenance of the vendored oracle.
4. State in the lab ADR that the hosted default and the lab default model carry an Apache-2.0 or MIT tag, read per repository and revision.
5. State in the contribution review list that a harness plugin names the licence of the process it drives, separately from the licence of its own code, and that a plugin whose process is not under an open licence is not listed in the community index.
6. The canonical record still describes HolmesGPT as the only MVP investigator and lists it among the upstreams the core renders. This audit holds under either wording; the record itself is for its owner to amend.

## 8. Questions for a lawyer

1. Is the Go kernel, written with the Cordis source open and tested against it, a derivative work of Cordis? If we carry the MIT notice regardless, is anything else needed?
2. Does any patent or pending application of the paper's authors or their employers cover the model, given that MIT carries no patent grant?
3. Does our hosted service, running an open-weight model for tenants, fall under "Model as a Service" in the Qwen Community License 1.0 or the Mistral Ecosystem Open-Weight License? Does the "AI Work Assistant" definition in the Qwen licence reach an operations assistant?
4. May a plugin id or an index listing contain an upstream's name (`harness-holmesgpt`, a provider plugin named after its upstream) under the Linux Foundation policy and under vendor policies, also when a third party sells that plugin?
5. Does the Linux Foundation trademark policy apply to the HolmesGPT and k8sgpt marks, and who owns "MCP" and "Model Context Protocol"?
6. Do Ollama's terms of service, which define the service to include "software", bind a user of the MIT binary taken from the public repository?
7. If we ever mirror or bundle an upstream image (HolmesGPT, a model server, the dashboard MCP server), which obligations of the base image and of bundled GPU vendor libraries do we take on, and what is the minimum compliant offline bundle?
8. On hosted, may we pass tenants' requests to a model vendor's API under our own key, under each vendor's terms, and is that "reselling the Services" under Anthropic's Commercial Terms?
9. Is describing an interface as "OpenAI-compatible" in product text and in a provider plugin's description acceptable use of that mark?
10. For the marketplace: when a seller's paid plugin drives a process under commercial terms (the Claude Agent SDK, an enterprise proxy), what must the listing terms require so that the liability stays with the seller?

## 9. Sources, all read 2026-11-11

| Subject | Source | Version or commit |
|---|---|---|
| HolmesGPT licence, dependencies, lock file, tree, Dockerfile, CONTRIBUTING, README | `https://github.com/HolmesGPT/holmesgpt/blob/0.46.0/LICENSE` and the same tag for `pyproject.toml`, `poetry.lock`, `Dockerfile`, `CONTRIBUTING.md`, `README.md`; GitHub licence API | Tag 0.46.0, commit 586045cae6 (2026-11-04); head of `master` 7745255d3f |
| LiteLLM root licence and enterprise licence | `https://github.com/BerriAI/litellm/blob/main/LICENSE`, `.../enterprise/LICENSE.md` | Head of `main` 4b498d1bba |
| LiteLLM packaging | `https://github.com/BerriAI/litellm/blob/v1.103.5/pyproject.toml`, `.../enterprise/pyproject.toml`, `Dockerfile`, `CONTRIBUTING.md` (CLA); `https://pypi.org/pypi/litellm/1.103.5/json`; `https://pypi.org/pypi/litellm-enterprise/json` | Tag v1.103.5, commit dec1691c90; `litellm-enterprise` 0.1.83 |
| Ollama | `https://github.com/ollama/ollama/blob/main/LICENSE`, `Dockerfile`; `https://ollama.com/terms` | Head of `main` 8a89b76cc7; latest release v0.40.16; terms updated August 2026 |
| vLLM | `https://github.com/vllm-project/vllm/blob/main/LICENSE`, `docker/Dockerfile` | Head of `main` c7dd34ccea; latest release v0.33.0rc3 |
| MCP specification | `https://github.com/modelcontextprotocol/modelcontextprotocol/blob/main/LICENSE`, `README.md` | Head of `main` 632a0c9a65 |
| MCP Go SDK | `https://github.com/modelcontextprotocol/go-sdk/blob/main/LICENSE` | Head of `main` 328036ea20 |
| MCP Python SDK | `https://github.com/modelcontextprotocol/python-sdk/blob/main/LICENSE` | Head of `main` 77001c96f9 |
| Dashboard MCP server | `https://github.com/grafana/mcp-grafana/blob/main/LICENSE`, `CONTRIBUTING.md` | Head of `main` c8cafb6785; latest release v2.3.0 |
| krr | `https://github.com/robusta-dev/krr/blob/v1.31.3/LICENSE` | Tag v1.31.3, commit 17c769d53c |
| Robusta | `https://github.com/robusta-dev/robusta/blob/master/LICENSE`, `README.md` | Head of `master` 9b3b48714b; latest release 0.54.0 |
| k8sgpt | `https://github.com/k8sgpt-ai/k8sgpt/blob/main/LICENSE`, `CONTRIBUTING.md` | Head of `main` 0f776a215f; latest release v0.4.41 |
| dsh | `https://github.com/deepseek-ai/deepseek-harness/blob/master/LICENSE`, `THIRD_PARTY_NOTICES.md`; the installed package `@deepseek-ai/dsh` and its `LICENSE` | Head of `master` fcc0ad4621; package 0.2.2-rc.1 |
| Cordis | The installed `@deepseek-ai/cordis` and `@deepseek-ai/cordis-plugin-loader` `LICENSE` files; `https://github.com/cordiverse/cordis/blob/main/LICENSE` | 4.1.0 and 1.1.0; upstream head 261c5fdae6, latest upstream release v4.0.0-rc.12 |
| The paper | `https://arxiv.org/abs/2608.25512`, licence link `http://arxiv.org/licenses/nonexclusive-distrib/1.0/` | v1, 2026-08-26 |
| Claude Agent SDK | `https://github.com/anthropics/claude-agent-sdk-python` (`LICENSE`, `README.md`); `https://github.com/anthropics/claude-agent-sdk-typescript` (`LICENSE.md`, `README.md`); `https://code.claude.com/docs/en/agent-sdk/overview`; `https://www.anthropic.com/legal/commercial-terms` | Heads 3cf3446b0f and 2007ac4092; terms effective 2025-06-17 |
| OpenAI Agents SDK, Pydantic AI | `https://github.com/openai/openai-agents-python/blob/main/LICENSE`; `https://github.com/pydantic/pydantic-ai/blob/main/LICENSE` | Heads d689616578 and 0458d8c162 |
| LangGraph | `https://github.com/langchain-ai/langgraph` (LICENSE files per library); `https://pypi.org/pypi/langgraph/json`; `https://pypi.org/pypi/langgraph-api/json` | Head e5e37b5d70; 1.2.20; 0.17.1 |
| Other harnesses, ACP, OpenAI client and OpenAPI description | GitHub licence API (`/repos/<owner>/<repo>`) only | Default branch on the day |
| Model licences | `https://huggingface.co/api/models/<id>` (the publisher's `license`, `license_name`, `license_link`); `https://huggingface.co/Qwen/Qwen3.8-Flash-Next/raw/main/LICENSE`; `https://huggingface.co/mistralai/Mistral-Large-4-1T-A52B/raw/main/LICENSE`; `https://dev.meta.ai/llama/llama3_3/license` | Revisions: Qwen3.8-27B 37561816e6, Qwen3.8-Flash-Next 812d96e10a, Llama-3.3-70B-Instruct 6f6073b423, Mistral-Large-4 af26e7869a, Mistral-Small-4 a11f36bebf, gemma-4-31B 52f3f65bc7, DeepSeek-V4-Flash-0731 7872f01b1d, gpt-oss-20b 6cee5e81ee, granite-4.2-8b a704214412 |
| Trademark policy | `https://www.linuxfoundation.org/legal/trademark-usage` | Page as served on the day |

UNVERIFIED, in one place: who owns the HolmesGPT, k8sgpt and MCP marks; the CNCF intellectual-property policy; the GPU vendors' redistribution terms inside the model-server images; OpenAI's service terms and brand guideline; the Gemma terms for Gemma 3 and earlier; the Llama 4, `qwen3.8-max` and Devstral 2 licence texts; the Elastic-2.0 text for `langgraph-api` (metadata only); the LICENSE text and sub-directory licences of the harnesses listed as unverified in section 5; the published HolmesGPT image's actual layer contents (the Dockerfile was read, the image was not inspected); any patent filing on the Cordis model.

---

The Grafana Labs Marks are trademarks of Grafana Labs, and are used with Grafana Labs' permission. We are not affiliated with, endorsed or sponsored by Grafana Labs or its affiliates.
