# Console — Open-Source License Analysis

> **Decision (2026-06-15):** MooseQuest chose **AGPL-3.0 + a Contributor License
> Agreement**. The repo `LICENSE` now contains the AGPL-3.0 text and a draft CLA
> lives at [`CLA.md`](../../CLA.md). The CLA draft and the trademark questions in
> §8 still need attorney review before they are relied upon. The analysis below
> is retained as the rationale for that decision.

> **Status: research-only. This document does NOT constitute legal advice.**
> Before changing the LICENSE file, filing trademark registrations, drafting a CLA, or executing any dual-licensing arrangement, consult a software/IP attorney licensed in your operating jurisdiction.
> **Last updated: 2026-06-15.** Sources cited as of that date — verify for freshness before acting.

---

## TL;DR (3 sentences)

Console is a **self-hostable server binary** — the category of software for which the AGPL-3.0 "network use" clause was explicitly designed. Staying on MIT would maximize adoption and let anyone (including cloud competitors) embed or host Console without contributing back.

AGPL-3.0 flips that default and requires anyone who **modifies and network-hosts** Console to publish their source changes. If MooseQuest LLC ever plans to sell a hosted or commercial edition, pairing AGPL-3.0 with a Contributor License Agreement (CLA) that grants MooseQuest a commercial sublicense right is the standard playbook used by Grafana, Unleash, and similar tools — but it requires a copyright attorney to draft the CLA and to advise on the relicensing mechanics.

---

## Context: What Console Is

Console is a Go binary (no cgo, pure-Go SQLite via `modernc.org/sqlite`) that runs an HTTP server combining **feature flags** and **status monitoring**. It is invoked as `./console serve` and exposes a web dashboard and JSON API over the network. Users interact with it remotely through a browser or HTTP client. This matters because the AGPL "network use" clause is triggered precisely by this pattern: a modified version of the software served to remote users over a network.

The repo `LICENSE` is now **AGPL-3.0** (copyright MooseQuest LLC, 2026). It was a placeholder **MIT** license prior to the 2026-06-15 decision.

---

## Section 1: License Comparison

### OSI Approval Status at a Glance

| License | OSI-Approved? | Category |
|---|---|---|
| MIT | Yes | Permissive |
| Apache-2.0 | Yes | Permissive |
| MPL-2.0 | Yes | Weak copyleft |
| GPL-3.0 | Yes | Strong copyleft (distribution-triggered) |
| AGPL-3.0 | Yes | Strong copyleft (distribution- OR network-triggered) |
| BSL 1.1 | **No** | Source-available |
| FSL 1.1 | **No** | Source-available |
| FCL 1.0 | **No** | Source-available |
| SSPL v1 | **No** (OSI rejected; MongoDB withdrew submission) | Source-available |

OSI-approved status matters for: package registries that block non-OSI licenses, corporate legal policies that only allow OSI-approved dependencies, and integration with major Linux distributions.

Sources: [OSI Approved Licenses](https://opensource.org/licenses) · [MongoDB withdrew SSPL from OSI consideration](https://www.opensourceforu.com/2019/03/mongodb-withdraws-its-server-side-public-license-from-osi-consideration-process/) · [SSPL OSI status](https://www.mongodb.com/legal/licensing/server-side-public-license/faq)

---

### Detailed License Profiles

#### MIT

- **Core obligation:** Include the copyright notice and license text in all copies. That is the entirety of the requirement.
- **Patent grant:** None explicit. Users rely on implied patent license, which is weaker and jurisdiction-dependent.
- **SaaS/network use:** No obligation triggered. A cloud provider can take Console, host it, modify it, charge for it, and never release a line of code.
- **Effect on adoption:** Maximum. No friction. Any developer, any enterprise, any package manager.
- **Effect on closed-source embedding:** Permitted without restriction.
- **Effect on cloud-provider hosting:** Permitted without restriction.

Sources: [MIT License text — SPDX](https://spdx.org/licenses/MIT.html)

---

#### Apache-2.0

- **Core obligation:** Preserve copyright notice, license text, and any NOTICE file. State changes made to files.
- **Patent grant:** Explicit, broad: each contributor grants a perpetual, worldwide, non-exclusive, royalty-free patent license covering their contributions. Includes a patent-retaliation clause: sue the project over patents and your patent license terminates.
- **SaaS/network use:** No obligation triggered. Same as MIT for cloud-hosting purposes.
- **Effect on adoption:** Very high. Preferred by many enterprises over MIT because of the explicit patent grant.
- **Effect on closed-source embedding:** Permitted without restriction.
- **Effect on cloud-provider hosting:** Permitted without restriction.
- **Key difference from MIT:** The patent grant is the material advantage. For a Go binary with no patent-encumbered code today, this matters more as Console's user base grows.

Sources: [Apache License 2.0 text](https://www.apache.org/licenses/LICENSE-2.0) · [Apache 2.0 vs. MIT patent grant — FOSSA](https://fossa.com/blog/open-source-licenses-101-apache-license-2-0/) · [Snyk on Apache 2.0](https://snyk.io/articles/apache-license/)

---

#### MPL-2.0 (Mozilla Public License)

- **Core obligation:** File-level copyleft: modifications to MPL-covered *files* must be released under MPL-2.0. New files you add alongside them can be under any license (including proprietary).
- **Patent grant:** Yes, similar scope to Apache-2.0, with retaliation clause.
- **SaaS/network use:** No obligation triggered by network use. The SaaS loophole exists for MPL just as for MIT/Apache — a cloud provider can modify MPL-covered files and serve the result without sharing those modifications.
- **Effect on adoption:** Good; OSI-approved, used by Firefox, Terraform (pre-BSL era).
- **Effect on closed-source embedding:** Permitted if the embedding project doesn't need to modify MPL files; modifications to MPL files must be disclosed.
- **Effect on cloud-provider hosting:** Not required to release; the file-level copyleft does not extend to network delivery.
- **For Console specifically:** MPL closes the embedding concern slightly (someone who patches Console's Go files must release those patches), but does not address the cloud-hosting scenario. Less commonly used in the infrastructure/devtool space than MIT, Apache, or AGPL.

Sources: [MPL 2.0 full text — Mozilla](https://www.mozilla.org/en-US/MPL/2.0/) · [MPL 2.0 FAQ — Mozilla](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)

---

#### GPL-3.0

- **Core obligation:** Strong copyleft triggered by **distribution** (giving someone a binary). Anyone who distributes a GPL-3.0 binary must provide the corresponding source.
- **Patent grant:** Yes. Includes anti-tivoization clause (hardware must allow user-installed modified software).
- **SaaS/network use:** **Not triggered.** Running GPL-3.0 software as a network service without distributing binaries creates no source-sharing obligation. This is the "Application Service Provider (ASP) loophole" that AGPL was created to close.
- **Effect on adoption:** Reduces adoption among companies that cannot or will not ship GPL software in their products. Many corporate policies prohibit GPL dependencies.
- **Effect on cloud-provider hosting:** A cloud provider can run GPL Console internally or as a hosted service and is not required to release modifications.
- **For Console specifically:** GPL-3.0 is strictly worse than AGPL-3.0 for MooseQuest's stated goal — it has all of GPL's adoption friction with none of the network-use protection.

Sources: [GPL-3.0 text — GNU](https://www.gnu.org/licenses/gpl-3.0.html)

---

#### AGPL-3.0

- **Core obligation:** Everything in GPL-3.0, PLUS Section 13 (see detailed treatment in Section 2 below).
- **Patent grant:** Yes, same as GPL-3.0.
- **SaaS/network use:** **Triggered.** If you modify AGPL software and run the modified version so that users interact with it over a network, you must prominently offer those remote users access to the corresponding source code.
- **Effect on adoption:** Lower than MIT/Apache. Some enterprise legal policies block AGPL ("copyleft firewall" concerns). Common in infrastructure, analytics, and monitoring tools where it is well-understood.
- **Effect on closed-source embedding:** Not permitted without a commercial license (copyleft extends to the combined work if distributed).
- **Effect on cloud-provider hosting:** If the provider runs an **unmodified** Console, no source obligation (nothing to disclose — the source is already public). If the provider modifies Console, they must publish those modifications. This is the key asymmetry: it does not prevent cloud hosting, but it eliminates the free-rider advantage of proprietary modifications.
- **For Console specifically:** AGPL is the strongest OSI-approved choice for protecting against competitors building closed-source forks of Console's server. It is the license used by Unleash (confirmed), Grafana (relicensed to AGPL in 2021), and PostHog (MIT with AGPL for some components).

Sources: [AGPL-3.0 full text — GNU](https://www.gnu.org/licenses/agpl-3.0.html) · [FSF on AGPLv3](https://www.fsf.org/bulletin/2021/fall/the-fundamentals-of-the-agplv3) · [Unleash LICENSE — GitHub](https://github.com/Unleash/unleash/blob/main/LICENSE)

---

#### BSL 1.1 (Business Source License)

- **Core obligation:** Source is available; production use is restricted until the Change Date (default: 4 years from release), after which the code converts to an OSI-approved license (the licensor specifies which one). The licensor can define an "Additional Use Grant" to permit specific production uses.
- **Patent grant:** None specified in the standard BSL text.
- **SaaS/network use:** Defined by the Additional Use Grant the licensor writes. HashiCorp's BSL, for example, allows production use but prohibits creating a "competitive product."
- **Effect on adoption:** Significantly lower than OSI licenses. Many package registries reject it. Many enterprise policies block it. Linux distributions (Debian, Fedora, etc.) cannot ship BSL software.
- **Effect on closed-source embedding:** Depends on the Additional Use Grant.
- **Effect on cloud-provider hosting:** Restricted under the standard terms; the Additional Use Grant defines the boundary.
- **Notable precedent:** HashiCorp relicensed Terraform from MPL-2.0 to BSL 1.1 in August 2023. The community forked it as OpenTofu (now under the Linux Foundation), demonstrating that BSL relicensing creates fork risk and community backlash.

Sources: [BSL 1.1 text — MariaDB](https://mariadb.com/bsl11/) · [HashiCorp BSL announcement](https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license) · [HashiCorp BSL page](https://www.hashicorp.com/en/bsl) · [FOSSA on BSL](https://fossa.com/blog/business-source-license-requirements-provisions-history/)

---

#### FSL 1.1 (Functional Source License)

- **Core obligation:** You may use, modify, and redistribute the software for any purpose **except** offering a competing commercial product or service. After 2 years per-version, converts to MIT or Apache-2.0 (licensor's choice).
- **Patent grant:** Not specified in the FSL text; confirm with counsel.
- **SaaS/network use:** Permitted for non-competing uses; prohibited for competing SaaS.
- **Effect on adoption:** Lower than OSI licenses; better optics than BSL because the conversion window is 2 years instead of 4 and the terms are simpler.
- **Limitation for self-hosted tools:** FSL offers limited protection for self-hosted projects — someone could fork the code, remove or bypass commercial gates, and self-host without paying. The FCL (below) was created to address this.
- **Created by:** Sentry. Sentry itself re-relicensed from BSD-3-Clause to BSL (2019) and then to FSL (November 2023).

Sources: [FSL home page](https://fsl.software/) · [Sentry FSL blog post](https://blog.sentry.io/introducing-the-functional-source-license-freedom-without-free-riding/) · [TLDRLegal FSL](https://www.tldrlegal.com/license/functional-source-license-fsl)

---

#### FCL 1.0 (Fair Core License)

- **Core obligation:** Same non-compete prohibition as FSL, but adds Elastic License 2.0-style restrictions that protect **self-hosted commercial features**. Converts to Apache-2.0 or MIT after 2 years.
- **Patent grant:** Not specified; confirm with counsel.
- **SaaS/network use:** Permitted for non-competing uses.
- **For self-hosted tools:** More appropriate than FSL for a tool like Console that ships as a self-hostable binary with potential future commercial features, because it closes the "fork and bypass the paywall" attack that FSL does not address.
- **Used by:** Flipt (server code is under FCL 1.0 as of their most recent LICENSE file).

Sources: [FCL home — fcl.dev](https://fcl.dev/) · [Flipt LICENSE — GitHub](https://github.com/flipt-io/flipt/blob/main/LICENSE)

---

#### SSPL v1 (Server Side Public License)

- **Core obligation:** If you offer SSPL-licensed software "as a service," you must open-source not just your modifications but **the entire infrastructure stack** used to provide that service — provisioning, monitoring, automation, everything. This is far more demanding than AGPL.
- **Patent grant:** Not specified.
- **SaaS/network use:** The most restrictive possible treatment.
- **OSI status:** Rejected. MongoDB withdrew its submission in 2019 after OSI indicated it would not approve.
- **Real-world outcome:** Redis adopted SSPL + RSALv2 in March 2024. Within months, the Linux Foundation backed a community fork (Valkey). AWS, Google Cloud, and others moved to Valkey. Redis subsequently reversed course (2025).
- **For Console:** The strongest possible cloud-provider deterrent, but with the highest adoption cost and the highest risk of community fork. Not recommended unless Console becomes a dominant infrastructure tool with significant cloud-provider free-rider behavior.

Sources: [MongoDB SSPL FAQ](https://www.mongodb.com/legal/licensing/server-side-public-license/faq) · [Redis SSPL + RSALv2 announcement](https://redis.io/blog/redis-adopts-dual-source-available-licensing/) · [Redis Valkey fork coverage](https://www.softwareseni.com/the-redis-valkey-fork-how-enterprises-rapidly-migrated-after-the-sspl-license-change/) · [Redis reversal](https://kuray.dev/blog/backend-development/rediss-u-turn-abandoning-sspl-and-returning-to-open-source-202505)

---

### Comparison Table

| License | OSI | Patent grant | SaaS trigger | Closed embedding | Cloud hosting | Adoption impact |
|---|---|---|---|---|---|---|
| MIT | Yes | No | None | Free | Free | Maximum |
| Apache-2.0 | Yes | Yes (explicit) | None | Free | Free | Very high |
| MPL-2.0 | Yes | Yes | None | File-level CL | Free | High |
| GPL-3.0 | Yes | Yes | **None** (loophole) | Copyleft on distrib. | Free | Reduced |
| AGPL-3.0 | Yes | Yes | **Yes — modified version served over network** | Copyleft on distrib. | Modifications must be open | Reduced vs. MIT; standard in infra tools |
| BSL 1.1 | No | No | Per Additional Use Grant | Per Additional Use Grant | Per Additional Use Grant | Significantly reduced; distro-blocked |
| FSL 1.1 | No | Unspecified | Non-competing only | Non-competing only | Non-competing only | Reduced; 2-yr to OSS |
| FCL 1.0 | No | Unspecified | Non-competing + self-host gate | Protects commercial features | Non-competing only | Reduced; 2-yr to OSS |
| SSPL v1 | No | No | Entire stack must open-source | Extremely restrictive | Entire infra stack open-source | Severely reduced; caused Redis fork |

---

## Section 2: The AGPL-3.0 "Network Use" Clause — Precise Treatment

### Exact Text — Section 13, AGPL-3.0

> "Notwithstanding any other provision of this License, if you modify the Program, your modified version must prominently offer all users interacting with it remotely through a computer network (if your version supports such interaction) an opportunity to receive the Corresponding Source of your version by providing access to the Corresponding Source from a network server at no charge, through some standard or customary means of facilitating copying of software."

Source: [GNU AGPL-3.0 text](https://www.gnu.org/licenses/agpl-3.0.html) (Section 13)

### What Triggers the Obligation

Three conditions must all be true:

1. **You modified the Program.** Running an unmodified copy of Console creates no obligation to publish source (the source is already public). The obligation only attaches to *your modifications*.
2. **Your modified version supports remote network interaction.** Console does — it runs `./console serve` and exposes an HTTP API and dashboard to users over a network. This condition is met by design.
3. **Users interact with your modified version remotely through a computer network.** If the only users of your modified Console are internal company employees on a private network with no outside access, the legal picture is more nuanced (internal-only use has historically been treated as less clearly triggering AGPL). If any external party interacts with the network endpoint, the obligation is clearly triggered.

### What "Corresponding Source" Means

"Corresponding Source" is defined in Section 1 of GPL-3.0 (incorporated by AGPL): it is the source code needed to generate, install, and run the object code — in Console's case, the Go source code for any modified version. It does not require open-sourcing the operator's proprietary business logic that is *separate* from and *not linked into* Console. The boundary of what must be disclosed is the Console binary itself and any modifications to it, not the apps that call Console's API.

### What the AGPL Does NOT Require

- Publishing source for internal-use-only private deployments (contested; not definitively resolved by caselaw — ask your attorney).
- Publishing source for modifications to external scripts, configs, or CI/CD tooling that deploys Console, if those aren't compiled into the binary.
- Publishing source for the *users of Console's feature flags/status API* — their app code is not a "modification" of Console.

### The "Unmodified Cloud Hosting" Scenario

If a cloud provider takes Console's AGPL source, compiles it **without any modifications**, and hosts it as a service: they have no source to disclose (the source is already public). AGPL does not prevent cloud hosting of unmodified code. What it prevents is a cloud provider taking Console, adding proprietary improvements (better UI, extra integrations, better scaling logic), and offering that enhanced version as a hosted product without disclosing those improvements.

Sources: [GNU AGPL Section 13](https://www.gnu.org/licenses/agpl-3.0.html) · [FSF on AGPLv3 fundamentals](https://www.fsf.org/bulletin/2021/fall/the-fundamentals-of-the-agplv3) · [Opensource.com on AGPLv3 source obligations](https://opensource.com/article/17/1/providing-corresponding-source-agplv3-license) · [Kyle Mitchell — Reading AGPL](https://writing.kemitchell.com/2021/01/24/Reading-AGPL)

---

## Section 3: Dual-Licensing and CLA/DCO Mechanics

### The Dual-Licensing Model

A company releases its software under a copyleft license (AGPL-3.0) for the community, **and** separately offers commercial licenses to companies that need to:

- Embed Console in a proprietary product (copyleft would otherwise infect the product).
- Host a modified Console without disclosing their modifications.
- Receive contractual warranties, indemnities, or SLA commitments that an open-source license cannot provide.

The company can only offer that commercial license if it is the **copyright holder** (or holds sufficient rights) over all the code in the repository. This is the CLA's purpose.

### CLA (Contributor License Agreement)

A CLA is a legal agreement between a contributor and the project's legal entity (MooseQuest LLC). It grants the company either:

- **Copyright assignment:** The contributor transfers copyright in their contributions to MooseQuest LLC. MooseQuest then owns all code and can sublicense it commercially.
- **Broad sublicense:** The contributor retains copyright but grants MooseQuest an irrevocable, worldwide right to sublicense their contributions on any terms MooseQuest chooses, including proprietary commercial licenses.

**Without a CLA:** Every contributor retains copyright in their contributions. To offer a commercial license that includes any third-party contributions, MooseQuest would need the explicit consent of every contributor — practically impossible at scale.

**With a CLA:** MooseQuest can sell commercial licenses that include all contributions, because contributors have already granted that right.

Examples of companies using AGPL + CLA dual-licensing:
- **Grafana Labs:** Relicensed Grafana, Loki, and Tempo from Apache-2.0 to AGPLv3 in April 2021. Updated their CLA to an Apache Software Foundation-based CLA that "clearly spells out the license terms and is balanced between the contributor's interests and Grafana Labs' rights to relicense." Source: [Grafana relicensing blog post](https://grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3/)
- **Unleash:** AGPL-3.0, commercial edition available. Source: [Unleash/unleash LICENSE](https://github.com/Unleash/unleash/blob/main/LICENSE)

### DCO (Developer Certificate of Origin)

A DCO is a much lighter-weight alternative. Contributors add a `Signed-off-by:` line to each commit, certifying (under the [Developer Certificate of Origin](https://developercertificate.org/)) that:
1. They have the right to submit the contribution under the project's license.
2. They understand it will be permanently public.

**What a DCO does NOT do:** A DCO does not grant MooseQuest any additional rights beyond what the AGPL already grants to all recipients. Contributors retain copyright. A DCO alone is **insufficient** for dual-licensing, because MooseQuest cannot sublicense contributions commercially without the contributor's consent.

**When DCO is appropriate:** Projects with no commercial edition — where the goal is simply to establish a clean provenance record. Spring Framework switched from CLA to DCO in January 2025 ([source](https://spring.io/blog/2025/01/06/hello-dco-goodbye-cla-simplifying-contributions-to-spring/)), but Spring has no commercial sublicensing need.

### CLA vs. DCO Trade-offs

| Factor | CLA | DCO |
|---|---|---|
| Enables dual-licensing / commercial edition | **Yes** | No |
| Contributor friction | Requires signing a legal document (one-time per project) | Requires only a git commit sign-off |
| Community perception | Some contributors view CLAs as corporate overreach or IP-grab | Generally trusted; widely adopted (Linux kernel uses DCO) |
| Tooling | [CLA assistant (SAP)](https://cla-assistant.io/) automates GitHub PR sign-off checks | Built into git with `--signoff` flag; no extra tooling needed |
| Relicensing later | Easier (rights already granted) | Requires tracking down every contributor |
| Legal robustness | Strong when drafted by an attorney | Lighter; courts have not definitively adjudicated all scenarios |

Sources: [FINOS on CLAs and DCOs](https://osr.finos.org/docs/bok/artifacts/clas-and-dcos) · [Kyle Mitchell — DCO is not a CLA](https://writing.kemitchell.com/2021/07/02/DCO-Not-CLA) · [CLA assistant — GitHub](https://github.com/cla-assistant/cla-assistant) · [Wikipedia on CLAs](https://en.wikipedia.org/wiki/Contributor_license_agreement) · [Wikipedia on DCO](https://en.wikipedia.org/wiki/Developer_Certificate_of_Origin)

### Tooling: CLA Assistant

CLA assistant ([cla-assistant.io](https://cla-assistant.io/), provided by SAP as a free hosted service) integrates with GitHub. When a contributor opens a PR, the bot comments with signing instructions. Once the contributor signs (via a GitHub OAuth flow), the PR check turns green. Repository owners can audit who signed each version of the CLA. This is the most common tooling for open-source projects running GitHub-based CLAs.

### The Inbound-Contribution Problem Before a CLA Exists

Console was under MIT prior to the 2026-06-15 decision; the repo `LICENSE` is now AGPL-3.0. Any third-party contributions merged while it was MIT remain MIT-licensed, and with the move to AGPL + CLA, **retroactive CLA coverage of existing MIT contributions is a legal question for an attorney.** The cleaner situation is to adopt CLA and AGPL simultaneously, before significant external contributions accumulate.

---

## Section 4: What Comparable Tools Actually Chose

All claims below are verified against the project's LICENSE file or an official relicensing announcement. No claims asserted from memory.

| Tool | License | Verified source | Notes |
|---|---|---|---|
| Unleash | AGPL-3.0 | [Unleash/unleash LICENSE](https://github.com/Unleash/unleash/blob/main/LICENSE) | Enterprise hosted edition sold separately. AGPL-3.0-or-later. |
| Flagsmith | BSD-3-Clause | [Flagsmith/flagsmith LICENSE.md](https://github.com/Flagsmith/flagsmith/blob/main/LICENSE.md) | Permissive. Self-host freely; hosted SaaS sold separately. |
| GrowthBook | MIT (core) + proprietary (enterprise dirs) | [growthbook/growthbook LICENSE](https://github.com/growthbook/growthbook/blob/main/LICENSE) | Open-core model: MIT for most code, proprietary license for `enterprise/` directories. |
| Flipt | FCL 1.0 (server) + MIT (client SDKs) | [flipt-io/flipt LICENSE](https://github.com/flipt-io/flipt/blob/main/LICENSE) | Server under Fair Core License. Clients (SDKs) remain MIT. |
| PostHog | MIT (core) + proprietary (`ee/` dir) | [PostHog/posthog LICENSE](https://github.com/PostHog/posthog/blob/master/LICENSE) | Open-core model; FOSS mirror available separately. |
| Grafana | AGPL-3.0 (since 2021) | [Grafana relicensing blog](https://grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3/) | Relicensed from Apache-2.0 to AGPLv3 in April 2021. Stated rationale: prevent cloud "strip-mining"; encourage contribution-back. CLA updated simultaneously. |
| Sentry | FSL 1.1 (since Nov 2023) | [Sentry FSL blog post](https://blog.sentry.io/introducing-the-functional-source-license-freedom-without-free-riding/) | Path: BSD-3-Clause (2008) → BSL (2019) → FSL (2023). Rationale: FSL closes BSL's ergonomic friction while still blocking competing SaaS. |
| MongoDB | SSPL v1 (since Oct 2018) | [MongoDB SSPL press release](https://www.mongodb.com/company/newsroom/press-releases/mongodb-issues-new-server-side-public-license-for-mongodb-community-server) | OSI rejected SSPL; Linux distros dropped MongoDB. |
| HashiCorp / Terraform | BSL 1.1 (since Aug 2023) | [HashiCorp BSL blog](https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license) | Relicensed from MPL-2.0. Community forked as OpenTofu (Linux Foundation). |
| Redis | BSD → SSPL + RSALv2 (Mar 2024) → returning to open source (2025) | [Redis SSPL announcement](https://redis.io/blog/redis-adopts-dual-source-available-licensing/) · [Redis u-turn](https://kuray.dev/blog/backend-development/rediss-u-turn-abandoning-sspl-and-returning-to-open-source-202505) | AWS and others shifted to Valkey fork within months. Redis reversed course in 2025. |

**Pattern observation (not advice):** Among self-hostable server tools in the feature-flags / monitoring space, the split is roughly:
- Pure permissive (MIT/BSD/Apache): Flagsmith, PostHog core, GrowthBook core.
- AGPL copyleft: Unleash, Grafana.
- Source-available: Flipt (FCL), Sentry (FSL).
- Strong source-available / proprietary: MongoDB (SSPL), HashiCorp (BSL), Redis (SSPL) — all three saw significant community-fork or backlash events.

---

## Section 5: Decision Matrix

The operator should identify which primary intent applies, then use the matrix below to frame the conversation with counsel.

| Primary intent | Recommended license(s) to investigate | Key tradeoffs | What to ask your attorney |
|---|---|---|---|
| **Maximize adoption** — get Console widely used, build ecosystem, trust, integrations. Commercial monetization is secondary or later. | MIT or Apache-2.0 | Easiest for any developer/company to adopt. No cloud-hosting protection. Closed-source forks permitted. Patent grant is a reason to prefer Apache-2.0 over MIT. | "What are the implications of switching from MIT to Apache-2.0 now, while contributions are minimal?" |
| **Protect a future hosted/commercial edition** — MooseQuest plans to sell a SaaS or enterprise edition and does not want competitors to host modified Console without contributing back. | AGPL-3.0 + CLA | Requires drafting and enforcing a CLA. Contributors will see the CLA before opening a PR. Reduces casual contribution. AGPL is blocked by some enterprise legal policies but is well-understood in the infra space. | "Draft a CLA that assigns or sublicenses contributions to MooseQuest LLC. What is the scope and can it survive relicensing later?" |
| **Protect commercial features in a self-hosted binary** — Console may gain paid commercial features that users run themselves, and MooseQuest needs to prevent someone from forking the binary and bypassing the paywall. | FCL 1.0 (converts to Apache-2.0/MIT after 2 years) or FSL 1.1 | Not OSI-approved; blocked by distros and many enterprise policies. FCL specifically addresses self-hosted feature gating, which FSL does not. Converts to open source after 2 years, which limits long-term lock-in. | "Is FCL enforceable in our jurisdiction? Does the 2-year window align with our commercial roadmap?" |
| **Keep it simple** — No commercial edition planned; MooseQuest just wants to ship an OSS tool cleanly. | MIT (the pre-2026-06-15 license) or Apache-2.0 | Maximum simplicity. The only upgrade to consider is Apache-2.0 for the patent grant. | "Should we upgrade to Apache-2.0 now? Any concerns with our existing single external dependency (modernc.org/sqlite)?" |
| **Prevent cloud providers specifically (maximum protection)** | SSPL v1 | Evidence from MongoDB and Redis: high probability of community fork and loss of Linux distribution packaging. OSI-rejected, which triggers corporate policy blocks. Not recommended without large market share and legal budget for enforcement. | "Given the Redis/MongoDB precedent, is SSPL viable for a tool still early (0.x)? What are the enforcement realities?" |
| **Time-limited protection converting to OSS** — MooseQuest wants protection now but is comfortable with the code becoming MIT/Apache-2.0 in 2 years. | FSL 1.1 or BSL 1.1 (with custom Additional Use Grant) | FSL is cleaner than BSL (less custom drafting). BSL requires writing the Additional Use Grant, which needs an attorney. Neither is OSI-approved. Used by Sentry (FSL) and Flipt (FCL). | "Is 2 years sufficient protection for Console's commercial roadmap? What happens to contributions made during the proprietary period when the code converts?" |

---

## Section 6: Jurisdiction and Cost Flags

### Federal (US)

- Copyright in Console is automatic from the moment of creation (no registration required). Registering with the US Copyright Office strengthens enforcement remedies (statutory damages, attorney's fees). Fee: $65 for a single online application as of 2026.
- Source: [US Copyright Office registration](https://www.copyright.gov/registration/)

### State of Formation / Operating State

- This analysis does not change materially by US state for open-source licensing purposes. The relevant law is federal (US Copyright Act, 17 U.S.C.) and contract law (for CLAs).
- CLA enforceability is a question of contract law, which varies by state. An attorney in MooseQuest LLC's operating state should draft and review the CLA.

### International Considerations

- AGPL is recognized and used internationally. Its enforceability in specific jurisdictions (particularly Germany, where software license enforcement litigation is most active in Europe) is well-established.
- Source-available licenses (BSL, FSL, SSPL) are less tested internationally.

### Cost Estimates (Out-of-Pocket)

| Item | Estimated cost | Notes |
|---|---|---|
| Changing LICENSE file (MIT → Apache-2.0) | $0 filing fee | Attorney review: 0.5–1 hr to confirm no issues with existing dependency (modernc.org/sqlite under MIT) |
| Changing LICENSE file (MIT → AGPL-3.0) | $0 filing fee | Attorney: 1–3 hrs to advise on CLA + relicensing. CLA drafting: 2–5 hrs attorney time. |
| CLA assistant setup | $0 (SAP hosted service) | Requires a CLA document (attorney-drafted). |
| Copyright registration (optional, strengthens enforcement) | $65 federal filing fee | [US Copyright Office](https://www.copyright.gov/registration/) |
| Attorney rates (software IP attorney, US) | $300–$600/hr typical range | Unsourced estimate — confirm with specific attorneys. |

---

## Section 7: Timing and Deadlines

**There are no hard regulatory deadlines** governing the license choice. However, there are strategic timing considerations:

1. **Before significant external contributions:** The cleanest time to adopt a CLA is *before* any third-party contributors merge code. Once external contributors have contributed under MIT (the pre-2026-06-15 license), relicensing requires their consent or a legal opinion on what MIT contributions can be relicensed to under AGPL. The repo is at v0.3.0 and still early (0.x) — this is a low-complexity moment to make this decision.

2. **Copyright registration window:** Registration before any infringement occurs (or within 3 months of first publication) is required to claim statutory damages and attorney's fees in US copyright litigation. For a 0.x project this is not urgent, but if enforcement is ever a goal, registration should happen before the tool gains wide adoption. Source: [17 U.S.C. § 412](https://www.copyright.gov/title17/92chap4.html)

3. **No S-Corp or tax election deadlines are triggered by a license change** (license choice is IP/contract law, not a tax event).

---

## Section 8: Questions for Your Attorney

The following questions are research output — not legal advice. Bring these to a **software/IP attorney** (not a general business attorney; this requires copyright and open-source license experience):

1. **CLA vs. copyright assignment:** Should the CLA grant MooseQuest LLC a sublicense to relicense contributions, or should it assign copyright outright? What are the implications of each for contributor relations and for future fundraising/acquisition due diligence?

2. **Relicensing existing MIT contributions:** Console had a MIT LICENSE file prior to the 2026-06-15 decision (the repo `LICENSE` is now AGPL-3.0). If any external contributions were merged under MIT before the CLA is in place, what rights does MooseQuest have to relicense those contributions to AGPL + commercial?

3. **CLA enforceability:** Is a GitHub-flow CLA (signed via CLA assistant) legally enforceable as a binding contract in our operating state? What is required for consideration and acceptance?

4. **AGPL "internal use" boundary:** If a company self-hosts a modified Console for internal employees only (no external network access), does Section 13 apply? Is there a risk that a court finds even internal corporate network access triggers AGPL?

5. **AGPL + commercial license pricing and scope:** What should the commercial license grant that the AGPL does not? What restrictions should it contain? Does it need to be registered anywhere?

6. **"Console" trademark search and registration:** Is "Console" (as a software product name) available for trademark registration in Class 42 (software as a service)? The word "console" is highly descriptive — what is the likelihood of registration vs. common-law protection only? Should the trademark be registered in MooseQuest LLC's name or a separate IP-holding entity?

7. **IP assignment from contractors:** Have all past and future contractors working on Console signed written IP assignment agreements? Without this, MooseQuest may not own all the copyright in the codebase. (This is a separate issue from the license choice but is required for CLA or dual-licensing to work.)

8. **Relicensing from AGPL to something else later:** If Console starts at AGPL and the CLA grants sublicense rights, what is the process and legal risk of later changing the public license to FSL, BSL, or back to permissive? What consent is needed?

9. **License compatibility with modernc.org/sqlite:** The only external dependency is `modernc.org/sqlite`. What is its current license, and is it compatible with AGPL-3.0, FSL, and FCL?

10. **Open-core structure:** If a future "Console Enterprise" edition ships additional features, should those features live in a separate repository, a separate directory with a separate LICENSE file, or be gated purely at runtime? What are the IP and licensing implications of each approach?

---

## Sources

All URLs cited in this document:

- [GNU AGPL-3.0 full text](https://www.gnu.org/licenses/agpl-3.0.html)
- [FSF — Fundamentals of the AGPLv3](https://www.fsf.org/bulletin/2021/fall/the-fundamentals-of-the-agplv3)
- [Opensource.com — Providing source under AGPLv3](https://opensource.com/article/17/1/providing-corresponding-source-agplv3-license)
- [Kyle Mitchell — Reading AGPL (/dev/lawyer)](https://writing.kemitchell.com/2021/01/24/Reading-AGPL)
- [Kyle Mitchell — DCO is not a CLA (/dev/lawyer)](https://writing.kemitchell.com/2021/07/02/DCO-Not-CLA)
- [MIT License — SPDX](https://spdx.org/licenses/MIT.html)
- [Apache License 2.0 full text](https://www.apache.org/licenses/LICENSE-2.0)
- [FOSSA — Apache License 2.0 explained](https://fossa.com/blog/open-source-licenses-101-apache-license-2-0/)
- [Snyk — Apache 2.0](https://snyk.io/articles/apache-license/)
- [Mozilla MPL 2.0 full text](https://www.mozilla.org/en-US/MPL/2.0/)
- [Mozilla MPL 2.0 FAQ](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)
- [GPL-3.0 full text — GNU](https://www.gnu.org/licenses/gpl-3.0.html)
- [OSI — Approved Licenses](https://opensource.org/licenses)
- [BSL 1.1 full text — MariaDB](https://mariadb.com/bsl11/)
- [FOSSA — BSL history and requirements](https://fossa.com/blog/business-source-license-requirements-provisions-history/)
- [HashiCorp — BSL announcement blog](https://www.hashicorp.com/en/blog/hashicorp-adopts-business-source-license)
- [HashiCorp — BSL terms page](https://www.hashicorp.com/en/bsl)
- [FSL home — fsl.software](https://fsl.software/)
- [Sentry — Introducing the FSL](https://blog.sentry.io/introducing-the-functional-source-license-freedom-without-free-riding/)
- [TLDRLegal — FSL](https://www.tldrlegal.com/license/functional-source-license-fsl)
- [FCL home — fcl.dev](https://fcl.dev/)
- [MongoDB — SSPL press release](https://www.mongodb.com/company/newsroom/press-releases/mongodb-issues-new-server-side-public-license-for-mongodb-community-server)
- [MongoDB — SSPL FAQ](https://www.mongodb.com/legal/licensing/server-side-public-license/faq)
- [OpenSourceForu — MongoDB withdrew SSPL from OSI](https://www.opensourceforu.com/2019/03/mongodb-withdraws-its-server-side-public-license-from-osi-consideration-process/)
- [Redis — SSPL + RSALv2 announcement](https://redis.io/blog/redis-adopts-dual-source-available-licensing/)
- [Redis — RSALv2 + SSPL post](https://redis.io/blog/rsalv2-sspl-announcement/)
- [Redis u-turn — returning to open source (2025)](https://kuray.dev/blog/backend-development/rediss-u-turn-abandoning-sspl-and-returning-to-open-source-202505)
- [SoftwareSeni — Valkey fork after Redis SSPL](https://www.softwareseni.com/the-redis-valkey-fork-how-enterprises-rapidly-migrated-after-the-sspl-license-change/)
- [Grafana Labs — Relicensing to AGPLv3 (April 2021)](https://grafana.com/blog/grafana-loki-tempo-relicensing-to-agplv3/)
- [Grafana Labs — CEO Q&A on relicensing](https://grafana.com/blog/2021/04/20/qa-with-our-ceo-on-relicensing/)
- [Sentry — 2019 relicensing (BSD → BSL)](https://blog.sentry.io/2019/11/06/relicensing-sentry/)
- [Unleash — LICENSE file](https://github.com/Unleash/unleash/blob/main/LICENSE)
- [Flagsmith — LICENSE.md](https://github.com/Flagsmith/flagsmith/blob/main/LICENSE.md)
- [GrowthBook — LICENSE](https://github.com/growthbook/growthbook/blob/main/LICENSE)
- [Flipt — LICENSE](https://github.com/flipt-io/flipt/blob/main/LICENSE)
- [PostHog — LICENSE](https://github.com/PostHog/posthog/blob/master/LICENSE)
- [FINOS — CLAs and DCOs](https://osr.finos.org/docs/bok/artifacts/clas-and-dcos)
- [CLA assistant — cla-assistant.io](https://cla-assistant.io/)
- [CLA assistant — GitHub repo](https://github.com/cla-assistant/cla-assistant)
- [Wikipedia — Contributor License Agreement](https://en.wikipedia.org/wiki/Contributor_license_agreement)
- [Wikipedia — Developer Certificate of Origin](https://en.wikipedia.org/wiki/Developer_Certificate_of_Origin)
- [Spring — Switching from CLA to DCO (Jan 2025)](https://spring.io/blog/2025/01/06/hello-dco-goodbye-cla-simplifying-contributions-to-spring/)
- [Wikipedia — Software relicensing](https://en.wikipedia.org/wiki/Software_relicensing)
- [Open Source Guides — The Legal Side of Open Source](https://opensource.guide/legal/)
- [US Copyright Office — Registration](https://www.copyright.gov/registration/)
- [17 U.S.C. § 412 — Copyright registration and infringement remedies](https://www.copyright.gov/title17/92chap4.html)

---

> **Reminder:** This document is research-only. It does not constitute legal advice. Before changing the LICENSE file, adopting a CLA, pursuing dual-licensing, or registering a trademark, consult a **software/IP attorney** licensed in your operating jurisdiction with experience in open-source licensing. The final license choice is the operator's decision.
