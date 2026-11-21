# AI Rule Management

Platform Admin and Merchant use **AI Rule** to create and review typed proposals in Thai or English and manage saved rules. The selected merchant’s field catalogue controls what the editor offers. Existing amount-based booking review rules appear in **Saved rules**, where either role can edit or remove them for the authorized merchant. Platform Rules drafts have their own workspace. [Setup and API examples](../simulation-runbook.md#ai-rule-composer) · [Current verification](../current-work.md#unified-merchant-ai-rule)

## Administration and execution

| Component | Responsibility |
| --- | --- |
| Shared composer | The Platform and Merchant **Draft rule** buttons send `requireAI: true` and `reviewWithJev: true`. OpenAI turns eligible Thai or English instructions into a typed proposal. Go validates the proposal and passes its sanitized structure to the effective Jev provider for an advisory review. The UI reports unavailable or invalid OpenAI drafts without substituting a local draft. Go validates the proposal again on save. |
| Platform Rules | The demo administrator composes and saves a global `DRAFT` in the existing Platform Rules store. A saved shared Simulation Lab run supplies evidence for submission. A separate checker can approve a revision for new Simulation Lab runs. The built-in rules retain their existing version and behavior. |
| Merchant Policy | The merchant's own bearer credential supplies its tenant. Platform Admin selects a merchant and can manage its rules with the same editor. PostgreSQL stores immutable policy versions. Go evaluates active policies in both merchant test tools and records matches and versions. |
| Jev | Mock or OpenRouter Jev supplies a draft review in both scopes and supports eligible semantic conditions and a separate amount-based risk advisory for merchant transactions. The administrator can test the effective settings with a fixed synthetic fixture in [AI Configuration](decision-engine.md#jev-configuration). |
| Decision resolver | The resolver records the Platform, Merchant Policy, Jev and final outputs. The highest precedence belongs to a platform `BLOCK`, then a merchant `BLOCK`. Other actions rank `REVIEW`, `CHALLENGE`, `UNRESOLVED` or `UNSUCCESSFUL`, and `ALLOW`. The platform wins a tie. Local decisions win a tie with Jev. A merchant policy match records a business decision. Confirmed fraud uses the existing outcome path. |

## Merchant business context and fields

The profile reads the existing merchant record, its saved setup, optional `policyIntegration` metadata, and up to 200 recent transactions belonging to that merchant. Product availability comes from the declared catalogue or a recorded product. Amount availability comes from a declared booking amount signal or an observed amount. Domain availability comes from declared domain support or an observed normalized domain. A merchant can be created with only its name. Add products and signals under **Business and integration settings** at creation or later from the merchant's **Overview**. The composer needs at least one supported product and exposes missing capabilities so the operator can adjust the policy. The existing Booking Assessment profile workflow retains its own approvals and historical versions.

| Field | Type and operators | Availability and missing data |
| --- | --- | --- |
| `product.type` | Enum, `eq` | `flight_ticket` and `hotel_room` use `BOOKING`. `physical` and `digital` use `CHECKOUT`. `payment_gateway` uses `CHECKOUT` in the merchant panel in Simulation Lab. The proposal includes the corresponding products and stages. |
| `customer.email_domain` | String, `eq` | Uses the normalized bare domain. IDNA normalization and case folding preserve exact equality. A subdomain, suffix lookalike, or absent domain fails the comparison. |
| `customer.email_disposable` | Boolean, `eq`, `ne` | Checks the normalized domain against the disposable email domain list. An absent domain or unavailable list leaves the signal missing. A supplied domain absent from the list produces `false`. |
| `customer.email_local_part_random` | Boolean, `eq`, `ne` | Checks the shape of the part before `@`. A bare domain, a legacy transaction without the shape result, or an input whose shape cannot be calculated leaves this signal missing. |
| `customer.mailbox_ref` | String, `eq` | Uses a server-derived reference scoped to the merchant. A different mailbox on the same domain or absent mailbox evidence fails the comparison. |
| `order.amount_minor` | Nonnegative integer, `eq`, `ne`, `gt`, `gte`, `lt`, `lte` | Uses minor units. A missing amount fails the comparison. An observed zero is a valid amount. |
| `transaction.description` | Text, `eq`, `contains`, `semantic_match` | Optional description of the item or service. Submit it as a `customSignals` entry with this key. |
| Registered `custom_*` field | Text (`eq`, `contains`, `semantic_match`), country code or boolean (`eq`, `ne`), whole number (`eq`, `ne`, `gt`, `gte`, `lt`, `lte`) | The merchant sends the field under `customSignals`. Text comparisons ignore case and whitespace at the ends. `contains` matches a literal substring. Country values use uppercase ISO alpha-2 codes. False and zero are observed values. |

Merchant rules contain `scope`, `name`, `product_types`, `stages`, `conditions`, and `action`. Conditions contain one flat `AND` or `OR` group with one to five typed entries and no executable expressions. Supported actions are `BLOCK`, `REVIEW`, and `CHALLENGE`. If an applicable policy cannot be evaluated due to absent evidence, the Merchant Policy result can report `UNAVAILABLE` and identify the fields in `missingSignals`. Register as many as 16 non-identifying fields on the merchant record. The field key and type stay stable once a rule or transaction uses the field. Country fields rely on evidence supplied by the integration. Payment failure windows, automatic network-country enrichment, check-in timing, subscriptions, nested groups, and explicit subdomain operators remain outside this contract.

A Merchant exact-email rule uses `BLOCK` and binds a `customer.mailbox_ref` equality condition to the product scope and other typed conditions with `AND`. An instruction to block one mailbox on flight purchases over 5,000 THB narrows the scope to flights and uses `order.amount_minor gt 500000`. An amount with an unspecified unit triggers a clarification after the OpenAI request, preserving the mailbox, product, and comparison. A compound rule can remain active without an expiry when the instruction requests none. [Browser workflow](../simulation-runbook.md#exact-email-with-product-and-price)

An exact-email rule can also require the country associated with a transaction's IP address. Register `custom_ip_country` as **Country code** for the merchant and supply the network country in `customSignals` as uppercase ISO alpha-2. For “IP from USA,” the OpenAI proposal must preserve `custom_ip_country eq US` along with the exact mailbox, flight scope, and strict amount comparison. The rule uses the integration's network-country evidence. Card BIN country and automatic IP enrichment are outside this evaluation path. For a Jev-reviewed draft, the composer asks for the amount unit when omitted. With an explicit unit, OpenAI can build the complete rule before the country field is registered. Go sends its sanitized definition to Jev and shows **Save rule** with setup guidance. Saving preserves the rule as an inactive draft. After the field is registered, refresh **Saved rules** and activate that draft using its saved conditions. A proposal that omits or changes the requested country is ineligible to save. [IP-country test workflow](../simulation-runbook.md#exact-email-with-product-price-and-ip-country)

A temporary exact-email rule starts its timer when the new version becomes `ACTIVE`. For compatibility, the legacy standalone exact-email preview request accepts `durationMinutes` from 1 to 525,600 or `durationHours` from 1 to 8,760. Both fields take whole numbers. Whole-hour durations expressed in minutes produce the corresponding hour-based proposal. The standalone command proposes the block for all supported products in the selected merchant's profile and shows those products before saving. The API reports its `expiresAt` timestamp and derives `EXPIRED` at the cutoff. New transactions no longer apply that version after expiry. An identical create retry preserves the existing expiration, and replaying a saved transaction returns the original decision. [Browser workflow](../simulation-runbook.md#temporary-exact-email-block)

```json
{
  "scope": "MERCHANT",
  "name": "Restricted flight email domain",
  "product_types": ["flight_ticket"],
  "stages": ["BOOKING"],
  "conditions": {
    "logic": "AND",
    "rules": [
      {
        "field": "customer.email_domain",
        "operator": "eq",
        "value": "skycannner.com"
      }
    ]
  },
  "action": "BLOCK"
}
```

Platform proposals use the existing stage-specific System Rules signal catalogue. Numeric fields support `eq`, `ne`, `gt`, `gte`, `lt`, and `lte`. The `payment_token_missing` boolean supports `eq` and `ne`. Platform drafts use `REVIEW` or `CHALLENGE` and join one to five conditions with AND. Get the current fields and their stages from `GET /api/v1/system/rules`. [Existing System Rules contract](system-rules.md)

## Disposable email domains and random-looking email usernames

In **Describe your rule**, enter `Block flight tickets and hotel rooms when the email is disposable or the local part looks randomly generated`. The preview should include both products at `BOOKING` and a group with two native email conditions. An incomplete email-risk instruction prompts for clarification. A proposal named for disposable or random-looking email must include an email-risk condition before it can be saved.

```json
{
  "scope": "MERCHANT",
  "name": "Block by email risk",
  "product_types": ["flight_ticket", "hotel_room"],
  "stages": ["BOOKING"],
  "conditions": {
    "logic": "OR",
    "rules": [
      {"field": "customer.email_disposable", "operator": "eq", "value": true},
      {"field": "customer.email_local_part_random", "operator": "eq", "value": true}
    ]
  },
  "action": "BLOCK"
}
```

Either true condition triggers the block. The local-part check matches an ASCII alphanumeric username with at least 12 characters, including at least four letters, four digits, and four transitions between adjacent letters and digits. These checks describe observed email characteristics. A match records a merchant policy decision.

Mock Checkout and the merchant panel in Simulation Lab calculate `customerEmailShape` in the browser for an eligible full address. The request sends four counts for length, letters, digits, and transitions along with the normalized domain and mailbox digest. Go validates the counts and stores the nullable Boolean classification with the transaction. The browser retains the full address. Both email conditions are evaluated locally, and their evidence stays within PayGuard.

## Conditions checked by Jev

Select **Draft rule** to see which conditions PayGuard can check locally and which need Jev. PayGuard uses `contains` for a literal substring and `eq` for an exact comparison when those operators fit the instruction. **Custom rule · Jev** uses `semantic_match` on `transaction.description` or the registered custom text field named in the preview. A compound instruction can combine local and semantic conditions in an `AND` or `OR` group. Review the product, action, evidence field and complete condition before selecting **Save rule**. Answer any question about an incomplete condition before saving. A complete exact-email rule awaiting IP-country setup can be saved as an inactive draft. Checkout and Simulation Lab offer the same available inputs.

For a country condition that PayGuard can check locally, register `custom_ip_country` as a country field and supply an ISO alpha-2 value such as `IN` in the transaction. A semantic condition can compare a country stated in the supplied text. A country absent from the transaction is missing evidence.

This rule blocks physical goods checkout when the supplied country is India and the transaction description means admission to Hong Kong Disneyland.

```json
{
  "scope": "MERCHANT",
  "name": "Hong Kong Disneyland admissions from India",
  "product_types": ["physical"],
  "stages": ["CHECKOUT"],
  "conditions": {
    "logic": "AND",
    "rules": [
      {"field": "custom_ip_country", "operator": "eq", "value": "IN"},
      {"field": "transaction.description", "operator": "semantic_match", "value": "admission to Hong Kong Disneyland"}
    ]
  },
  "action": "BLOCK"
}
```

Go evaluates eligible local conditions first. A local false in an AND group or a local true in an OR group settles that group without a semantic request. A conclusive Platform or Merchant block also skips the semantic request. If a semantic comparison is needed and its text is available, Go sends the observed text, saved criterion, and sanitized saved rule structure for each needed condition with an opaque question key. The observed text can contain up to 120 characters and the criterion up to 2,000 characters. Detected email addresses, raw IP addresses, mailbox references, card data and credentials stay out of the request. In Mock mode, Jev returns `MATCH` for case-insensitive equality after trimming. Other comparisons return `UNKNOWN`. In Live mode, Go validates the typed Jev answers. The amount-based advisory uses its own evidence and settings. [Jev configuration](decision-engine.md#jev-configuration)

| Answer | Effect |
| --- | --- |
| `MATCH` | The condition is true. PayGuard applies the saved action if the full rule matches. |
| `NO_MATCH` | The condition is false. PayGuard resolves the AND or OR group with the other conditions. |
| `UNKNOWN` | A timeout, unavailable or invalid provider response, missing safe evidence, or an uncertain answer can leave the group unresolved. An unresolved applicable group sends the transaction to **Review (unresolved)** under **Needs decision**, unless a conclusive block applies. |

Saved transactions pin the rule ID, version, zero-based condition index, field, answer and provider status in `policyDecision.merchantPolicyDecision.jevConditions`, with the model and provider decision ID when available. Missing safe evidence records `NOT_RUN` and `UNKNOWN`. The saved Jev settings version is recorded when available. Replay returns the stored decision and evidence. The rule preview uses recent stored evidence to count cases that would need Jev at transaction time. [Live test walkthrough](../simulation-runbook.md#test-a-condition-with-live-jev)

## Shared rule preview data

A validated `READY` interpretation includes a read-only `definition` assembled by Go from the validated `proposal`. Both Platform and Merchant previews use this shape.

| Field | Contract |
| --- | --- |
| `schemaVersion` | `1` for this preview format. |
| `scope` | `PLATFORM` or `MERCHANT`, based on the proposal's evaluation scope. |
| `name`, `description` | The rule name and optional description. |
| `stages`, `productTypes` | Stage codes and optional product codes. Platform stages retain their existing codes. Merchant proposals include their product scope. |
| `action` | The validated action for the rule's scope. |
| `conditions` | `logic` and a `rules` array. Each rule contains `field`, `operator`, and a typed `value`. Platform conditions use `AND` and map their signal ID to `field`. Merchant conditions retain `AND` or `OR`. |
| `duration` | Optional `{value, unit}` for a temporary Merchant rule. The unit is `minute` or `hour`. |

The preview sets the exact-mailbox condition's `value` to `null`. When OpenAI produces a complete rule and the merchant still needs to register IP country, `CLARIFICATION` can carry the preview `definition` and a tenant-bound `draftProposal`. To save it as an inactive version, submit that `draftProposal` as `proposal` with `saveAsDraft: true`. Include the current `expectedVersion` when editing a saved rule. An amount-unit question still requires a response before saving. Go validates the proposal when saving and checks the current merchant registry on activation. Drafts and revisions use the existing version table.

With `reviewWithJev: true`, the composer requires OpenAI and returns a separate `jevReview` alongside the interpretation. Jev receives the scope, status, any clarification code, and available structured definition, including stage, products, action, conditions and duration. The request omits rule names, descriptions, the raw instruction, full email addresses, mailbox digests, and mailbox references. The masked mailbox condition retains its field and operator. Clients can omit `reviewWithJev` to keep their existing interpretation behavior.

AI Configuration determines the effective Jev settings. The draft shows a live advisory answer, served model, and decision ID when available. Mock, timeout, and unavailable states are distinct. A draft review does not save, approve, activate, or change a policy.

## Rule lifecycle and isolation

A merchant can have several `ACTIVE` rule IDs, with one effective version per ID. Save, edit, and rollback validate current capabilities, run a bounded preview over stored evidence, and check for potential overlap with active policies that take a different action. Potential overlap or a product-wide block puts the new version in `PENDING_APPROVAL`. Saving a complete rule with `saveAsDraft: true` also creates a `PENDING_APPROVAL` version. If it awaits the IP-country signal, its preview records that signal in `missingSignals` and **Saved rules** shows **Draft**. An earlier active version of that rule can still apply while its new version is pending. The overlap check is conservative and can flag disjoint numeric ranges.

**Activate** revalidates the selected version, records current overlap in its preview, and makes the selected rule version effective alongside the merchant's other active rules. A draft that requires IP country can be activated once the merchant has registered a **Country code** field for the rule's products and stages. The integration supplies the actual country for each transaction. If multiple Merchant Policies match, PayGuard records the matched rule IDs and versions and applies `BLOCK > REVIEW > CHALLENGE` within Merchant Policy. The final decision continues to use the existing Platform, Merchant Policy, and Jev precedence.

Disable records `DISABLED` for the selected rule. Delete removes the selected rule from the current list and evaluation by appending a version. Rollback copies an earlier proposal into a new version. Mutations are tenant-scoped and use the latest `expectedVersion`. Each tenant can save up to 50 rules. Historical versions, previews, and transaction decisions retain their evidence.

The API checks the session or sandbox bearer credential before interpreting, reading or mutating tenant rules. The composer rejects raw email addresses, IP addresses, card numbers, and credentials in API instructions. Use bare email domains or synthetic signal descriptions for general rules. For one exact-email address in a Merchant rule, the browser replaces the address with `[EXACT_EMAIL]` and sends a separate digest for the selected merchant to Go. OpenAI receives the sanitized instruction and the permitted field catalogue. Go derives the mailbox reference from the digest and a merchant-specific key, binds the reference to the AI proposal, and validates the result. The raw address stays in the browser. The digest and reference stay within PayGuard. Backend composition logs record operation, scope, actor, outcome, timing, and provider or draft metadata. `OPENAI_API_KEY` stays in the backend environment. OpenRouter is the provider option for Jev draft reviews and transaction assessments.

A revision saved from a natural-language preview records the safe source instruction. **Edit** fills **Describe your rule** with that instruction. For an older or structured rule, the editor generates a description of the saved conditions and identifies it as a summary. To edit an exact-address rule, enter the address and describe the conditions and any duration you want to save. The revision's source instruction excludes the address.

Platform change revisions append the approved rule input, shared run preview, reason, actor, time, and source revision. The administrator submits the current draft version and the independent checker approves the pending revision. The checker can restore an earlier active revision or disable the rule by appending another version. New Simulation Lab runs pin the effective platform ruleset and matching rule revisions. Previously saved runs and replays retain their original result.

At transaction time, Go evaluates native policy conditions locally and calls Jev for semantic comparisons that still need a result. The Merchant Policy evaluator applies to the merchant-selected path in Mock Checkout and Simulation Lab. Approved Platform revisions apply to new generated Simulation Lab runs and to merchant-selected tests when the stage and signals are available. General booking assessments and live payment authorization have their existing evaluation paths. Automatic scoring-profile discovery and promotion across all booking flows and production role governance are separate work.

## Acceptance checks

- Create a merchant with only its name. Check the direct navigation in Local Sandbox Preview and OIDC, the one-time credential receipt in the ordinary Sandbox, and the ability to add products from **Overview**.
- With OpenAI configured, draft supported English and Thai policies using the merchant's declared fields. Check the `openai` source and model, and verify that a clarification leaves the rule unsaved. A mocked provider run verifies routing and validation. Record a live OpenAI run separately.
- With OpenAI disabled or unavailable, select **Draft rule** in both Platform and Merchant scopes and check that the UI shows the failure without presenting a local draft. Check that incomplete or malformed proposals require correction before saving. Exercise local interpretation separately through an existing API request that omits `requireAI`.
- Verify exact normalized domain matching and missing amount handling. Check that a rule written by one merchant has no effect on the other merchant or on stored transaction snapshots.
- In both merchant test tools, exercise the combined Flight and Hotel email block with a disposable domain, a random-looking username on a regular domain, an ordinary username, and missing email evidence. Check that either positive signal blocks and that a product-only proposal named for random email is rejected. Reload a rule written from a safe instruction, an older structured rule, and an exact-address rule and check their respective **Edit** experiences.
- Preview and save a 12-hour exact-email block. In both merchant test tools, check the target mailbox, another mailbox on the same domain, and the bare domain. Verify the block is limited to the selected merchant, expires 12 hours after activation, and preserves earlier decision snapshots on replay.
- Draft a compound exact-email block for flights over 5,000 THB. Check the sanitized provider request and the preview for the exact mailbox binding, flight scope, strict greater-than comparison, and absence of an expiry. Test another mailbox on the same domain, another product, and an amount equal to the threshold. Omit the unit and check that the editor asks for it after the AI request while retaining the other conditions.
- Draft a compound exact-email block for flights over 3,000 THB from a US IP. Omit the amount unit and check that the editor asks for it. Before registering the IP-country field, select THB and verify that OpenAI produces all four requirements and Jev receives the sanitized definition. Select **Save rule**, reload **Saved rules**, and confirm the draft stays inactive. Register the field as **Country code**, refresh, and activate the saved draft. Test a different country and missing country evidence.
- Verify `MATCH`, `NO_MATCH`, and unresolved results in the merchant's ledger from both tools. Test local short-circuiting, missing evidence, provider timeout, and exact replay after a rule version changes.
- Verify that potentially overlapping policies stay pending until explicitly activated and can then be effective together. Check the matched IDs and versions and that Block takes priority over Review and Challenge. Disable one rule and confirm the others still apply to new transactions. Replay the original transaction and confirm the original decision and evidence.
- Compare `definition.schemaVersion`, conditions and typed values in Platform and Merchant previews and the Jev requests. Verify that exact-mailbox values stay masked in the preview and Jev request, and that saving a rule awaiting IP country passes the bound `draftProposal` to the save endpoint with `saveAsDraft: true`. Check the distinct live, mock, timeout, and unavailable review states.
- Verify the recorded Platform, Merchant Policy, Jev and final outputs, version history, disable, and rollback. Check that a Merchant Policy rejection leaves confirmed fraud evidence untouched.
- Verify that a Platform Admin save produces a draft and that merchant and unauthenticated credentials cannot invoke admin-only writes. Verify that approval requires the independent checker, a new Simulation Lab run pins the active rule revision, and rollback preserves stored decisions.
