# Mise: Configuration-as-Code for Restaurant POS Platforms

**Everything in its place, at every location.**

## Product Roadmap & Go-to-Market Strategy

*Working document — August 2026*

---

## The One-Liner

Mise is a CLI tool that lets multi-location restaurant operators define, version-control, and deploy POS configuration (menus, tax rates, service charges, employee roles) as declarative code — with plan/apply workflows, drift detection, and instant rollback via git.

---

## Product Roadmap

### Phase 0: Foundation (Months 1–3)

**Goal:** Build the core engine and prove the model works end-to-end with one POS platform.

**Why start with Square, not Toast:** Square's API is fully read-write out of the box — no partner approval process, no gated access. You can build the complete fetch → plan → apply → drift-detect loop on day one. Toast's standard API is read-only; write access requires partner certification that could take months. Starting with Square lets you ship a working product while the Toast partnership is in progress.

**Core engine (platform-agnostic):**

The engine is the heart of Mise — it doesn't know or care which POS platform it's talking to. It handles four things: reading declarative config files (YAML), maintaining a state file that records the last-known configuration of every resource, computing diffs between the declared state and the live state, and orchestrating the apply sequence with dependency resolution (a menu item depends on its tax rate existing first, so the tax rate must be created before the menu item).

**CLI commands:**

`mise init` creates a new workspace, generates the config scaffold, and connects to the POS platform via API credentials.

`mise fetch` pulls the current live configuration from the POS and generates YAML files representing every resource. This is the critical onboarding command — operators don't have to write config files from scratch. They run fetch, and their entire current setup becomes a versioned, human-readable file set they can commit to git.

`mise plan` compares the declared config files against the live POS state and outputs a human-readable diff: "3 resources to add, 1 to update, 0 to destroy." Nothing is changed — this is a preview only.

`mise apply` executes the changes shown in the plan, updating the POS via API and recording the new state.

`mise drift` compares the live POS state against the last-known state file and reports any changes made outside of Mise (someone changed a tax rate through the GUI, for example).

**Square adapter (v0.1):**

The first adapter covers Square's Catalog API and Locations API. Resources supported in v0.1:

- `square_location` — location details, address, business hours, settings
- `square_catalog_item` — menu items with descriptions, images, variations
- `square_catalog_category` — item categories (appetizers, mains, desserts)
- `square_catalog_tax` — tax rate definitions, applied to items/categories
- `square_catalog_discount` — discount definitions (percentage, fixed)
- `square_catalog_modifier_list` — modifier groups (toppings, sizes, temperatures)

**Config file format (YAML):**

```yaml
# mise.yaml — root configuration
provider:
  platform: square
  access_token: ${SQUARE_ACCESS_TOKEN}
  environment: production

location_groups:
  georgia:
    filter:
      state: GA
  all:
    filter: "*"
```

```yaml
# tax/georgia.yaml
resources:
  - type: square_catalog_tax
    name: ga_state_sales_tax
    properties:
      name: "GA State Sales Tax"
      percentage: "4.0"
      applies_to: FOOD
      locations: ${group.georgia}

  - type: square_catalog_tax
    name: ga_alcohol_tax
    properties:
      name: "GA Alcohol Tax"
      percentage: "6.0"
      applies_to: ALCOHOL
      locations: ${group.georgia}
```

**State file:**

The state file (`mise.state.json`) records the mapping between declared resource names and live POS resource IDs, plus the last-known property values. This is what makes drift detection possible — Mise can compare "what the state file says the GA sales tax rate is" against "what the Square API says it actually is right now."

**Deliverable:** A working CLI tool that can fetch a Square restaurant's full catalog and tax configuration into YAML files, plan changes, apply them, and detect drift. An operator managing 5 Square locations can use Mise to change a tax rate in one YAML file and have it applied across all 5 locations in one command.

---

### Phase 1: Audit & Drift — Toast Read-Only (Months 3–5)

**Goal:** Bring Toast operators into the ecosystem using Toast's standard API (read-only, self-service access). Deliver immediate value through visibility and audit capabilities — no write access needed.

**Toast adapter (read-only, v0.1):**

Using Toast's Configuration API, the adapter supports reading all 24 resource types:

- `toast_tax_rate` — percent, fixed, and tax-table rate definitions
- `toast_service_charge` — auto-gratuity, kitchen surcharges, service fees
- `toast_discount` — discount definitions and rules
- `toast_dining_option` — dine-in, takeout, delivery, catering
- `toast_revenue_center` — bar, patio, main dining room, banquet
- `toast_sales_category` — food, beverage, alcohol, retail, merchandise
- `toast_menu` — top-level menu containers
- `toast_menu_group` — appetizers, entrees, desserts, kids menu
- `toast_menu_item` — individual items with pricing and modifiers
- `toast_modifier_group` — toppings, sides, temperatures, sizes
- `toast_alternate_payment_type` — gift cards, house accounts, vouchers
- `toast_service_area` — server sections, bar zones
- `toast_table` — table definitions and numbering
- `toast_printer` — kitchen printer and receipt printer assignments
- `toast_void_reason` / `toast_no_sale_reason` / `toast_payout_reason` — operational reason codes
- `toast_tip_withholding` — tip distribution and withholding rules
- `toast_break_type` — employee break policies

Additional APIs integrated:

- Toast Labor API: `toast_employee` (read employee records and job assignments)
- Toast Order Management Configuration API: `toast_ordering_schedule` (online ordering hours and settings)

**Key commands for Toast (read-only mode):**

`mise fetch` — Pulls the entire configuration for all locations in a Toast management group. Iterates over every restaurant GUID (because Toast's API is location-scoped). Generates a complete YAML representation of the chain's POS configuration. This alone is valuable — many operators have never seen their full configuration in one place.

`mise drift` — Compares the fetched baseline against the current live state. Reports differences per location: "Location #14 (Nashville) has a different takeout surcharge than the declared baseline." This is the audit and compliance wedge — operators can run this weekly and catch unauthorized changes.

`mise diff --location nashville --location atlanta` — Compares the configuration of two specific locations side by side. Surfaces inconsistencies that may or may not be intentional.

`mise report --type tax-rates` — Generates a cross-location report of a specific resource type. "Here are the tax rates at every location — are they all correct?"

**What Mise cannot do yet for Toast:** Apply changes. The plan and apply commands are disabled for Toast until partner write access is approved. The CLI tells operators: "To apply changes, Mise needs write access. Contact your Mise account manager to enable this." This creates demand pull for the Phase 2 partnership.

**Deliverable:** A Toast operator managing 40 locations can run `mise fetch` once, commit the result to git, and then run `mise drift` weekly to catch any configuration changes made through the GUI that weren't authorized. They get a cross-location view of their entire POS configuration for the first time — and an audit trail that satisfies compliance requirements.

---

### Phase 2: Full Toast IaC (Months 5–8)

**Goal:** Secure Toast Partner API access (write permissions) and deliver the complete plan/apply workflow for Toast.

**Toast Partner Program application:**

The application is stronger with Phase 1 users as evidence: "We have X operators managing Y locations who are already using Mise for drift detection. They are requesting write capabilities." Toast's partner program evaluates whether an integration adds value to their ecosystem — a tool that helps multi-location operators manage their configuration more reliably is aligned with Toast's growth strategy.

**Full Toast adapter (v1.0):**

With write access, all commands work:

`mise plan` — Shows exactly what would change across which locations before any API calls are made.

`mise apply` — Pushes changes to Toast via the API and triggers a publish operation for affected locations. Includes a confirmation step: "This will modify tax rates at 3 locations. Proceed? [y/N]"

`mise apply --dry-run` — Validates that the planned changes would succeed (API permissions, valid values, dependency checks) without actually executing them.

**Rollback via git:**

Since all configuration is stored in version-controlled YAML files, rollback is a git operation: `git revert HEAD` followed by `mise apply`. The state file ensures Mise knows the current live state, so the apply computes the correct diff to restore the previous configuration.

**Location-scoping workaround:**

Because Toast's API requires a per-location header for every request, Mise handles multi-location operations by parallelizing API calls across locations. An apply that touches 40 locations makes 40 parallel API calls (respecting Toast's rate limits of 20 requests per second), with progress reporting and error handling per location.

**Deliverable:** A Toast operator can write a YAML file that says "all Georgia locations should charge 4.5% food sales tax," run `mise plan` to preview the change, run `mise apply` to push it, and have the change live across all Georgia locations in under a minute. If it's wrong, they revert the git commit and apply again.

---

### Phase 3: Enterprise & Team Features (Months 8–12)

**Goal:** Make Mise production-ready for large franchise operations with multiple team members and compliance requirements.

**Remote state backend:** The state file moves from a local JSON file to a remote backend (S3, a managed Mise cloud service, or a self-hosted option). This enables team collaboration — multiple operators can work against the same state without conflicts. Includes state locking to prevent concurrent applies.

**CI/CD integration:** Mise runs in a CI/CD pipeline (GitHub Actions, GitLab CI). A franchise's configuration repo has branch protection — changes go through pull requests, are reviewed by operations leadership, and are applied automatically when merged to main. This is the "GitOps for restaurants" workflow.

**Policy-as-code:** Operators define compliance rules that Mise enforces before any apply:

```yaml
# policies/tax-compliance.yaml
policies:
  - name: minimum_food_tax
    description: "All locations must charge at least state minimum food tax"
    rule:
      resource: toast_tax_rate
      condition: percentage >= state_minimum_tax_rate
      severity: error  # blocks apply

  - name: alcohol_tax_required
    description: "All locations serving alcohol must have an alcohol tax configured"
    rule:
      resource: toast_menu_item
      condition: if category == "ALCOHOL" then has_tax_rate("alcohol")
      severity: warning  # warns but allows apply
```

**Scheduled drift detection:** Mise Cloud runs `mise drift` on a schedule (daily, weekly) and sends alerts via email, Slack, or webhook when unauthorized changes are detected. The operator gets a message: "Drift detected at Nashville location — takeout surcharge changed from 3% to 2% at 2:47 PM yesterday by user JSmith."

**Multi-brand support:** For operators like Flynn Group (Pizza Hut, Applebee's, Arby's, Wendy's under one company), Mise supports multiple provider configurations in one workspace. Each brand has its own config directory and state, but drift reports and compliance checks run across the entire portfolio.

**Deliverable:** A 100-location franchise operates Mise through GitHub. Configuration changes are proposed as pull requests, reviewed by the Director of Operations, and auto-applied when merged. Nightly drift checks catch unauthorized GUI changes. Tax compliance policies block any apply that would violate state tax rules.

---

### Phase 4: Platform Expansion (Months 12–18)

**Goal:** Add adapters for additional POS platforms and adjacent systems.

**New POS adapters:**

- Clover (Fiserv) — large market share, particularly in counter-service restaurants
- Lightspeed Restaurant — strong in international markets
- Revel Systems — already popular with 5+ unit franchise chains
- TouchBistro — growing in the independent segment

**Adjacent platform adapters:**

- Stripe — manage product catalog, pricing, and webhook configuration alongside POS settings. Many restaurants use Stripe for online payments even when Toast/Square handles in-store POS.
- DoorDash/Uber Eats — manage storefront configuration, menu availability, and pricing across delivery platforms from the same config files as the POS.

**Cross-platform consistency:** An operator using Toast for POS and DoorDash for delivery can define a menu item once in Mise and have it propagated to both platforms with platform-specific overrides (different pricing for delivery vs. dine-in).

**Deliverable:** Mise becomes a multi-platform configuration management tool — the Terraform of restaurant operations. An operator's git repo contains the single source of truth for their entire technology stack's configuration.

---

## Go-to-Market Strategy

### The Buyer

The primary buyer is the **Director of IT, Director of Operations Technology, or VP of Operations** at a multi-location restaurant group or franchise. They typically oversee 20–500+ locations. They report to the COO or CEO. Their daily pain is exactly what The Human Bean's Senior Director described: spending "hours, days, weeks" on configuration changes that should be instant.

Secondary buyers include **franchise consultants** (who set up new franchise locations and need repeatable configuration), **restaurant technology integrators** (who manage POS setups for multiple clients), and **restaurant CPAs/compliance firms** (who need audit trails for tax compliance).

### The Entry Wedge: Drift Detection (Free)

The go-to-market doesn't start with "adopt infrastructure as code for your restaurant" — that's a foreign concept to this buyer. It starts with a question they already care about:

**"Are your tax rates correct across all your locations right now? Can you prove it?"**

The free tier of Mise covers up to 5 locations and includes `fetch`, `drift`, and `diff` commands. The pitch is:

*"Connect Mise to your Toast or Square account. In 60 seconds, you'll have a complete snapshot of every tax rate, service charge, and menu configuration across all your locations — and you'll see exactly where things don't match."*

This requires only read access (available via Toast's self-service standard API). No commitment, no write access, no risk. The operator runs it once, sees a drift report that reveals three locations with incorrect tax rates they didn't know about, and thinks: "I need this running every week."

### The Upgrade Path: Plan & Apply (Paid)

Once an operator is using drift detection regularly, the natural next question is: "Can I fix these inconsistencies from here instead of logging into Toast Web for each location?"

That's the paid tier — full plan/apply capabilities, scheduled drift detection, Slack/email alerts, and multi-user support. Pricing model:

- **Starter (free):** Up to 5 locations. Fetch, drift, diff. Read-only.
- **Professional ($8/location/month):** Unlimited locations. Full plan/apply. Scheduled drift detection. Email alerts. Git integration.
- **Enterprise ($15/location/month):** Everything in Professional, plus CI/CD integration, policy-as-code, compliance reporting, SSO, dedicated support.

For a 40-location chain on Professional: $320/month. Compare that to the cost of one POS misconfiguration audit ($15,000+), one Toast Professional Services engagement (likely $10,000+), or even one week of a Director of Operations' time spent manually updating tax rates across locations.

### Sales Channels

**Channel 1: Toast & Square partner ecosystems.** Both platforms have partner directories and integration marketplaces. Being listed as a Toast Partner Integration means operators discover Mise when searching for multi-location management tools in Toast's ecosystem.

**Channel 2: Restaurant technology conferences.** The National Restaurant Association Show (NRA Show), MURTEC (Multi-Unit Restaurant Technology Conference), and the Restaurant Finance & Development Conference are where franchise IT directors and operations VPs gather. A booth or speaking slot at MURTEC is directly in front of the target buyer.

**Channel 3: Restaurant CPA and compliance firms.** Firms like The Fork CPAs and Restaurant CPAs regularly advise multi-location operators on tax compliance. Mise's drift detection and audit trail capabilities make it a natural recommendation: "Use this tool to make sure your POS tax rates are correct before we file." Offer a referral partnership.

**Channel 4: Franchise development consultants.** When a franchise opens a new location, someone has to configure the POS to match the brand standard. Mise makes this a one-command operation: `mise apply --template brand-standard --location new-nashville`. Franchise development consultants who set up 20+ locations per year would champion this.

**Channel 5: Content marketing.** Write the content that franchise IT directors are already searching for: "How to manage Toast menu changes across multiple locations," "Multi-state tax compliance for restaurant franchises," "POS configuration audit checklist." These are long-tail SEO terms with a highly qualified audience.

### The Pitch (30-Second Version)

*"You manage [X] restaurant locations. Every time a tax rate changes, a menu item launches, or a service charge adjusts, someone has to log into your POS dashboard and make that change location by location. If they miss one, you might not know for months — until an audit catches it.*

*Mise gives you one file that defines what every location's configuration should look like. Change the file, review the preview, push the update. Every location. Thirty seconds. And if anything changes that shouldn't, you get an alert the same day."*

### The Pitch (Tax Compliance Angle)

*"A 1% tax misconfiguration across $500K in annual sales can cost $15,000 or more in a three-year audit. Mise checks your tax rates across every location, every night, and alerts you the moment something doesn't match. Think of it as an automated compliance auditor for your POS — one that costs less per month than one hour of your accountant's time."*

### The Pitch (New Location Onboarding Angle)

*"Opening a new location? Instead of spending two days configuring your POS from scratch — recreating menus, tax rates, service charges, employee roles — run one command. Mise clones your brand-standard configuration to the new location in under a minute. Identical setup, every time, with zero manual data entry."*

---

## Key Risks & Mitigations

**Risk: Toast could build this themselves.** Toast's Restaurant Config Platform team is already working on configuration management internally. They could ship a native IaC-like capability. **Mitigation:** Platform vendors rarely build great IaC tools — AWS didn't build Terraform, Salesforce didn't build Salto. Toast's incentive is to keep operators inside Toast Web, not to make configuration portable. Additionally, Mise's multi-platform story (Toast + Square + Clover) is something Toast would never build.

**Risk: Toast Partner API approval takes too long or is denied.** The 8-stage partner process is gated and unpredictable. **Mitigation:** Start with Square (open API), deliver value there, and build the Toast adapter in read-only mode first. The Phase 1 user base creates demand-side pressure that strengthens the partner application. If Toast partnership is delayed, the Square product stands on its own.

**Risk: Restaurant operators won't adopt a code-centric workflow.** The buyer profile is different from DevOps engineers. YAML files and CLI commands may feel foreign. **Mitigation:** The drift-detection entry wedge doesn't require writing any code — it's a one-command audit tool. The fetch command generates all config files automatically. And Phase 3's CI/CD integration means the Director of Operations can review changes in a GitHub pull request (a web interface they can learn in 10 minutes) rather than writing YAML directly.

**Risk: The market is too niche.** Only operators with 20+ locations need this, which is a subset of the restaurant market. **Mitigation:** The subset is where the money is. A 100-location franchise paying $15/location/month is $18,000 ARR from one customer. 100 such customers is $1.8M ARR. And the multi-platform expansion in Phase 4 opens adjacent markets (retail POS, hospitality, any multi-location service business).

---

## Success Metrics by Phase

**Phase 0 (Month 3):** 10 Square operators using Mise for at least one plan/apply cycle. Validated that the core engine handles real-world configuration complexity.

**Phase 1 (Month 5):** 30 Toast operators using drift detection on a weekly basis. At least 3 operators have discovered a configuration inconsistency they didn't know about. Toast Partner application submitted with user evidence.

**Phase 2 (Month 8):** Toast write access approved. First 10 Toast operators using full plan/apply. At least 1 operator managing 50+ locations.

**Phase 3 (Month 12):** 100+ operators across Toast and Square. $10K+ MRR. CI/CD integration used by at least 5 enterprise operators. First franchise development consultant partnership signed.

**Phase 4 (Month 18):** Third POS adapter shipped. Cross-platform operations validated. $50K+ MRR. Series seed or revenue-funded growth.
