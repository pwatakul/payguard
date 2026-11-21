# Current work

## Production authentication

The working tree adds OIDC sign-in, server-side sessions, Go-enforced role and merchant scope, and separately attributed Platform Rule approval. Independent standard and race-enabled Go runs each recorded 376 passing tests and subtests, and the legacy-data migration check passed. Readiness still depends on real identity-provider and HTTPS browser acceptance. [Production authentication configuration and release gates](requirements/production-authentication.md)

## Merchant creation and OpenAI rule drafting

The working tree reduces merchant creation to one required field and one submission. Business and integration settings can be added later. **Draft rule** requires OpenAI for Platform and Merchant rules and uses the validated AI proposal as the preview and save structure. If OpenAI is unavailable, the editor shows an error. Merchant exact-email drafts send OpenAI a placeholder and bind the mailbox reference in Go. A compound block preserves the product and price conditions, and an ambiguous price asks for the unit. Existing API clients retain timed exact-email compatibility. [Merchant management walkthrough](simulation-runbook.md#merchant-management) · [Rule drafting contract](requirements/ai-rule-management.md)

- Chromium against an isolated Go backend verified that submitting only a merchant name created exactly one record and opened its details directly in the Local Sandbox Preview.
- Six isolated synthetic cases passed against the revised backend using live OpenAI on `gpt-4.1-mini`. They verified clarification for a missing price unit, English and Thai exact-email flight blocks with a strict comparison above 500,000 minor units for 5,000 THB, a two-minute exact-email draft, and general Merchant and Platform drafts.
- An independent full Go suite run against fresh PostgreSQL recorded 386 passing tests and subtests with zero failures or skips. Two command packages have no tests. Go vet and `git diff --check` passed. Fourteen frontend tests, TypeScript checking, the production build, and an isolated browser check passed.

### Exact-email draft with IP country

The previous composer returned HTTP 502 for a synthetic version of the Gother request. Gother's profile currently lacks the `custom_ip_country` field. The initial fix replaced the error with setup guidance and validated that the OpenAI draft requires the exact mailbox, a flight, a price strictly above 3,000 THB, and the integration's IP-country code `US`. Gother's integration configuration and saved rules were left untouched. [IP-country test workflow](simulation-runbook.md#exact-email-with-product-price-and-ip-country)

- Five synthetic cases passed against live OpenAI. The browser verified the setup guidance, and the frontend passed 14 Node tests, TypeScript checking, and the production build.
- The final Go suite reported 387 passing tests and subtests. Seven independent overlay checks and Go vet also passed. Upstream deployment was outside this verification.

### Jev review of OpenAI drafts

Both Platform and Merchant **Draft rule** actions use OpenAI to produce the rule structure and send its sanitized definition to the effective Jev provider for an advisory review. The preview identifies the OpenAI model and available Jev result, served model, and decision ID. With the price unit supplied, Gother can preview the full `AND` definition before registering the IP-country signal. The latest save workflow can keep that definition as an inactive draft. [Draft review contract](requirements/ai-rule-management.md#shared-rule-preview-data) · [Local walkthrough](simulation-runbook.md#ai-rule-composer)

- Four isolated synthetic cases passed with OpenAI `gpt-4.1-mini` followed by OpenRouter Jev `typesafe/jev-1.14` and live provider decision IDs.
- The earlier local smoke checks returned live Jev reviews for both scopes after the local Jev environment switched from mock to live. At that point Gother's response carried a display-only four-condition definition.
- Focused Go tests, their race-enabled run, and Go vet passed. The frontend passed 14 Node tests, TypeScript checking, and the production build.

### Saving a draft before IP-country setup

**Save rule** now stores a complete exact-email rule as a **Draft** when it requires IP country and the merchant has yet to register the signal. The draft retains all four conditions and leaves any earlier active version effective. After registering `custom_ip_country` as **Country code**, select **Refresh rules** and **Activate** on the saved draft. Activation validates the current field and uses the saved proposal. [IP-country test workflow](simulation-runbook.md#exact-email-with-product-price-and-ip-country)

- A disposable PostgreSQL workflow using live OpenAI and OpenRouter Jev verified that saving and reloading retains the draft, and retrying the same save returns the existing version. Activation was rejected before valid IP-country registration. After setup, explicit activation used the same saved four-condition rule without another AI call.
- The independent Go suite passed 391 tests and subtests on disposable PostgreSQL with zero failures or skips. Go vet, all 14 frontend tests, TypeScript checking, the frontend build, and the browser save and activate flow passed. The checks saved no rule for Gother.

## Concurrent Merchant Rules and shared AI Rule preview

Explicit activation can make multiple Merchant Rules effective together, including policies with potential overlap. Each rule retains its own effective version and history. The Platform and Merchant composer previews share the versioned `definition` format. Saves continue to use the validated proposal for each scope. [Rule data and lifecycle](requirements/ai-rule-management.md#shared-rule-preview-data) · [Multi-rule test walkthrough](simulation-runbook.md#test-multiple-active-merchant-rules)

- The isolated PostgreSQL lifecycle test activated overlapping Block, Review and Challenge rules. The transaction recorded all three matched versions. Disabling each stronger rule left the others effective for new transactions, and replay preserved the original result.
- Integrated verification passed 384 Go tests and subtests on disposable PostgreSQL with zero failures or skips. Go vet, 12 frontend tests, TypeScript checking, and the production build passed.
- Chromium with a synthetic API fixture verified that explicit activation changed the Merchant active count from one to two and that both composer previews rendered `Rule data (JSON)` with schema version 1.

## Action and status styling

Merchants, merchant creation and transactions, AI Rule, and Platform Rules and approval share a visual hierarchy. Navy identifies the primary action and a neutral outline identifies secondary actions. Green marks Activate, Approve, and Active. Amber marks Disable, Rollback, and Review. Red marks Delete, Reject, and Block. Blue marks Challenge, and neutral badges identify inactive and draft states. Text labels and icons convey meaning alongside color. Keyboard focus uses the existing global indicator.

Chromium verified the updated controls and status badges with synthetic data at 1440 px, 390 px, and 320 px. Mobile pages fit their viewports, and keyboard focus stayed visible. Sampled text contrast was at least 5.36:1. Twelve frontend tests, TypeScript checking, and the production build passed.

## PayGuard corporate identity

The v4 logo update uses the outlined horizontal logo without the tagline in expanded navigation and the mobile drawer. The collapsed sidebar and light and dark browser icons use the v4 symbol. The public compact logo uses a 954 × 266 viewBox to show the full shield and preserve the source paths. Shared controls use the established navy and pale blue palette, and the interface self-hosts IBM Plex Sans Thai in weights 400 through 700. [Crisp Logo Source Kit v4](../PayGuard_Logo_v4_Crisp_Vector/README.md), [brand palette](../PayGuard_Source/brand-colors.csv)

- `npm run build` and `npm run typecheck` passed on Node.js 24.20.0. All five public SVGs parsed successfully. Path comparisons confirmed the production geometry matches the v4 source, and SHA-256 hashes matched for all 19 source files.
- Chrome verified the expanded and collapsed sidebar at 1440 px and drawer navigation at 320 and 390 px without horizontal overflow. The shield and body fit inside the compact viewBox. Both favicon variants decoded, and the browser confirmed the palette and Latin and Thai font loading.
- The [local dashboard](http://127.0.0.1:3000/dashboard) returned HTTP 200 and served all five v4 SVGs with contents matching the production files.

## GitHub progress monitoring in Notion

Created the [PayGuard GitHub Activity database](https://app.notion.com/p/41f4da4de98381dcac67fb442ef0c35b) under the monitoring plan. The workflow is active on GitHub `main` at `40341b5`. See the [monitoring requirements](requirements/notion-progress.md) for repository configuration and rollout checks.

- Independent fixture and workflow YAML checks passed. A live local sync changed 18 activity records in Notion. The GitHub Actions repository secret and variable are configured.
- The [manual GitHub Actions run](https://github.com/pwatakul/payguard/actions/runs/44868685163) succeeded. The Notion health row showed Last Success at 11:59 Bangkok time on 21 November 2026. Verification of two scheduled runs is ongoing.

## Merchant AI Rule email conditions and editing

Gother's rule is active as version 4 in the local preview. It blocks Flight ticket and Hotel room bookings at `BOOKING` when the email uses a disposable domain or the part before `@` matches the random-looking username check. Version 2 contained only a Hotel room condition. Version 3 disabled it during the fix. Version 4 uses the same rule ID and retains the earlier versions and decisions in history. [Email rule contract](requirements/ai-rule-management.md#disposable-email-domains-and-random-looking-email-usernames)

**Edit** restores the revision's safe authored description. For versions without a saved description, it shows a summary of the actual saved conditions. Temporary exact-address rules request the email and duration again. [Rule lifecycle](requirements/ai-rule-management.md#rule-lifecycle-and-isolation)

- In the isolated backend, Flight and Hotel each blocked a known disposable domain and a random-looking Gmail username through both merchant transaction sources. An ordinary Gmail username, consecutive digits, punctuation, absent email, and an out-of-scope product did not trigger this rule. Missing evidence remained distinguishable from an observed false. The saved decision survived identical replay. A changed classification returned HTTP 409 and invalid shape metadata returned HTTP 422.
- The isolated composer produced both OR conditions from English and Thai prompts, returned clarification for incomplete input, and rejected an email-risk proposal containing only a product condition. Rule reads returned the safe saved instruction and excluded exact-address source text.
- In isolated Chrome, a random-looking email username triggered Rejected for a flight in Mock Checkout. A disposable email domain triggered Rejected for a hotel in Simulation Lab. Chrome also confirmed the count-only shape request, the saved source instruction in Edit, the legacy summary, and exact-address re-entry.
- The full Go suite passed against dedicated PostgreSQL. `npm run build`, `npm run typecheck`, and `node --test tests/*.test.mjs` passed. The eight Node tests cover the count-only shape and exact-address regressions.
- The local backend built from `14ae75a` reported ready on port 8082. An API read verified Gother's active version 4, the two OR conditions, and the saved description used in Edit. The rollout check created zero Gother test transactions. [Gother AI Rule on the local preview](http://127.0.0.1:3001/settings/merchants/f8a5a1b5-c47c-46a2-b870-ed7a6fa50510?tab=rules)

## Merchant transaction Risk Score and email changes

Merchant-selected Mock Checkout, Simulation Lab, and **Transactions** show the saved advisory score and its contributing factors. The initial scoring commit `56ee4e5` adds `merchant-risk-v1`. `merchant-risk-v2` adds an optional merchant-scoped Customer ID and 15 points for a confirmed address change after the previous disposable-email booking for that customer. The recorded rule and Jev actions drive the final decision. [Score formula and API contract](requirements/decision-engine.md#merchant-transaction-risk-score) · [Three-transaction walkthrough](simulation-runbook.md#test-the-risk-score-and-email-changes)

- Independent HTTP checks against isolated Go, PostgreSQL, and Redis verified tool parity, account-age and disposable-domain points, incomplete Jev evidence, score persistence on replay and manual disposition, an exact email block with an 85-point score, and tenant isolation. The full initial Go suite, frontend TypeScript check, and four existing Node tests passed.
- Independent v2 verification passed all 331 Go tests and subtests in both standard and race runs on disposable PostgreSQL. Go vet, frontend production build, TypeScript, and four Node tests passed. Chrome at desktop and 390 px widths verified the score and same-customer email-change flow. Live Jev returned **Low risk** and added 0 points to a disposable-email booking whose saved score was 45.

## Guest booking comparison with Jev

Merchant-selected Mock Checkout and Simulation Lab compare a repeat full email to the latest booking at the same merchant. The saved comparison shows whether Jev considers the bookings similar and whether it recommends review. [Comparison contract](requirements/decision-engine.md#guest-booking-comparison) · [Guest walkthrough](simulation-runbook.md#compare-guest-bookings-with-jev)

- The full Go suite passed 340 tests and subtests. Go vet passed. A live test with `typesafe/jev-1.14-20261014` compared two physical goods guest bookings with amounts of 7,900 and 8,100 minor units and changed descriptions. Jev returned **Different** with 96% similarity confidence and **Low risk**. Exact replay preserved the comparison. Chrome confirmed the comparison at desktop and 390 px widths.

## Transaction status and merchant rule navigation

Merchant-selected transaction results, the ledger, and dashboard show **Approved**, **Rejected**, or **Review**. The ledger has one **Status** filter. Platform Admin can **Approve** or **Reject** any **Review**, including Challenge. The detail shows the saved manual decision with the actor and time. Administrators reach shop policies through **Merchants → selected shop → AI Rule**. Merchants retain their scoped **Merchant AI Rules** entry. Legacy links still open the rules flow. [Status contract](requirements/decision-engine.md#transaction-status-and-manual-review) · [Review walkthrough](simulation-runbook.md#merchant-management)

- The combined Go suite passed all 340 tests and subtests in both standard and race runs, with zero failures or skips. Go vet, frontend production build, TypeScript, and four Node tests passed.
- Chrome at desktop and 390 px widths verified the **Approved**, **Rejected**, and **Review** states, immediate changes after manual decisions, and the saved manual decision, actor, and time after reload. The original score and assessment evidence remained exact. The scoped Merchant view could read the decisions, and review actions required Platform Admin. Dashboard counts matched the selected merchant's four ledger rows, with two Approved, one Rejected, and one Review.
- Chrome verified that Platform Admin could create a scoped two-minute rule through **Merchants → selected shop → AI Rule**, and that the Merchant workspace retained its scoped **Merchant AI Rules** entry.
- The local backend reported healthy on `1f85cd5`. The three status filters through the port 3001 preview returned two Approved, zero Rejected, and zero Review for the existing Test 001 ledger. Transaction `50b5cbb5-83d4-42b1-9dce-ea02cd740f2d` retained its manual approval and original Jev timeout evidence.

## Batch simulation decision evidence

Implemented and verified on the local preview. **Testing → Simulation Lab → Batch simulation results** contains **Event decisions**, **Decision evidence**, and **Shared signals in this run**. The list of earlier events replaces the Entity links graph, and the Fraud Network shortcut is removed. Existing bookmarks to `#fraud-network` still reach the results. The decision evidence shows the recorded engine action and mock Jev result alongside the display status. [Requirements](requirements/simulation-lab.md) · [UI flow](ui/simulation-lab.md) · [Saved run on the local preview](http://127.0.0.1:3001/simulation-lab?run=f3f70c97-ad98-4d74-95bd-e04462c1b137#batch-results)

- Desktop and 390 px Chrome checks opened the existing gateway run and scored it through read-only GET requests. The saved run showed 24 synthetic events with 19 Approved, zero Rejected, and five Review. The QA report returned 12 payment samples with zero unscored.
- For `order-2a-010-02`, the recorded 60-second account, device, IP, and token order counts were 1, 1, 5, and 1. The shared IP section linked to four earlier events, `order-2a-009-02`, `order-2a-008-02`, `order-2a-006-02`, and `order-2a-003-02`. The earlier graph counted a fifth account from a later event. Selecting a source moved focus to its evidence. **Back to selected event** focused the corresponding row. The first event displayed the empty state.
- The API returned HTTP 404 for an unknown shared run and HTTP 401 for a merchant-scoped read without credentials. Browser fixtures verified Block as Rejected and both Challenge and timeout as Review. They also covered retry ID reuse, scoped report authentication, and clearing results when the merchant token changed.
- From `frontend`, `node --test tests/*.test.mjs` passed all seven tests. `npm run typecheck` and the isolated `npm run build` passed.

## Unified merchant AI Rule

Platform Admin and Merchant now share **AI Rule** for merchant rule authoring and the saved list. Existing amount-based booking review rules stay editable in that list. Merchant access stays scoped to its own rules and read-only ledger. The local proxy distinguishes missing API routes, connection failures, timeouts and unreadable replies. Rule field loading provides a retry action. [Rule contract](requirements/ai-rule-management.md) · [Runbook](simulation-runbook.md#ai-rule-composer)

- A live OpenRouter Jev check on an isolated synthetic merchant used `typesafe/jev-1.14-20261014` with the general-purpose OpenAI composer disabled. Hong Kong Disneyland park admission produced `MATCH` and **Block** in Mock Checkout. Airport shuttle transport produced `NO_MATCH` and **Allow** in Simulation Lab. A missing transaction description produced `UNKNOWN` with `NOT_RUN` and **Review (unresolved)**. The live answers recorded provider decision IDs. Exact retries returned the original saved results.
- Focused backend unit and PostgreSQL integration tests passed for scoped authoring, local short-circuiting, approved outbound evidence, provider failure, tool parity and replay after disabling a rule. Run `go test -count=1 -run 'TestMerchantSemantic|TestManagedAuthoringWithoutComposerPostgres' ./...` from `backend` with a disposable `PAYGUARD_POLICY_TEST_DATABASE_URL`. The full Go test suite and race suite passed against disposable PostgreSQL, and Go vet passed. The frontend typecheck and isolated production build passed. The browser showed the semantic preview under the unified rule editor.
- The local assessment backend was healthy on port 8082. The page's profile, saved policies, existing booking review rules, and test context reads returned HTTP 200 through the Next.js proxy on port 3001.

## Exact-email blocks in minutes

The Merchant AI Rule editor accepts `block buyer@example.test 2min` and `บล็อกอีเมล buyer@example.test 2 นาที` for new rules or edits. PayGuard checks the merchant-scoped mailbox reference and expires the rule from activation. The API also accepts hour-based commands. [Test steps](simulation-runbook.md#temporary-exact-email-block)

- Focused Go and disposable PostgreSQL tests verified a 120-second expiry, block decisions from Mock Checkout and Simulation Lab while active, a fresh Allow result after expiry with no other restrictive rule, and preservation of the original Block on replay. Repeating the create request preserved the expiry. The frontend mailbox tests, TypeScript check, and isolated production build passed.

## Custom merchant rules evaluated by Jev

**Generate preview** proposes a **Custom rule · Jev** for safe merchant instructions with a clear action when the catalogue cannot express the requested condition directly. The preview shows the evidence that Jev will compare and retains the full condition. Supported conditions can run locally alongside Jev checks. Select **Save Rule** after reviewing the draft. Applicable conditions that lack sufficient evidence can send the transaction to **Review (unresolved)**. [Rule contract](requirements/ai-rule-management.md#conditions-checked-by-jev) · [Test steps](simulation-runbook.md#test-a-condition-with-live-jev)

- On a disposable merchant, a Thai/English instruction about a Hong Kong Disneyland ticket and an Indian IP produced an AND group with a local country check and a Jev ticket check. Live Jev returned MATCH for admission and NO_MATCH for an airport train ticket. The ledger recorded Block and Allow. A Japanese IP short-circuited the ticket check. Missing ticket evidence with an Indian IP produced Review (unresolved). Exact transaction retries returned their saved decisions. The browser displayed all four records.
- A live test on a merchant without a registered country field used a single Jev condition covering both clauses. A Disneyland admission description that included India returned `MATCH` and Block. The same admission with Japan returned `NO_MATCH` and Allow. The ticket title alone returned `UNKNOWN` and Review (unresolved).
- An independent two-minute wall-clock test verified that the matching email was blocked during the window and accepted on a fresh transaction after expiry. The original blocked transaction kept its result on replay. The full Go and race suites ran against disposable PostgreSQL. Go vet, frontend tests and typechecking passed.

## Merchant rule testing and local Platform Admin

Mock Checkout and Simulation Lab run the selected merchant's Platform Rules, Merchant Policies, published Jev review rules and configured Jev advisory. Choose a merchant and a product to run a test. Open its **Transactions** to compare each decision and saved version. Previous scenario transactions retain their original results and can still replay through the API. [Requirements](requirements/merchant-scenarios.md) · [Local evaluation](simulation-runbook.md#merchant-scenarios-and-jev)

The opt-in loopback workspace signs requests on the Next server. Platform Admin can manage merchants, Merchant Policies, amount-based Jev review rules, Platform drafts and Jev configuration without entering an administrator key in the browser. A merchant's own credential scopes the read-only ledger and rule editor. The independent checker approves or rolls back governed global Platform revisions. Register custom fields on the merchant's **Overview** and use them in either role's structured rule editor for that merchant. The same field catalogue drives both test forms. [Decision Engine](requirements/decision-engine.md) · [Custom field and policy contract](requirements/ai-rule-management.md#merchant-business-context-and-fields)

- In an isolated two-merchant test, the same mailbox returned Block for Merchant A's exact-email policy and Allow for Merchant B. A cross-tenant transaction read returned HTTP 404 in each direction. When A recorded a `verified_demo` fraud label on a Booking v1 event, B's event with the same UUID had an empty outcomes list and assessed as `LOW_RISK` with B's own approved profile. [Scope and outcome walkthrough](simulation-runbook.md#rule-scope-fraud-labels-and-email-evidence)
- The local Platform Rules approval test started with three active built-ins, zero active custom rules, and effective `rules-v1`. **Local approval verification 57bb77a7** used `account_age_days > 36500` with **Review**. Shared run `578b6abf-6b26-4bc8-9394-d06130e244a7` at seed `771121` yielded four eligible events and zero matches. Admin submission created version 1 in **Pending approval**. Admin approval returned HTTP 403. The independent checker approved version 2, which changed the effective merchant test-context ruleset, then withdrew it in version 3. The withdrawal set `activeVersion` to 0 and restored `rules-v1` with its three built-ins. The draft history and shared run remain available for local evaluation. Deleting the governed draft returns HTTP 409. The test created no merchant records. [Independent checker walkthrough](simulation-runbook.md#verify-global-approval-with-an-independent-checker)
- Independent HTTP checks against isolated Go, Next.js and PostgreSQL verified tokenless administrator CRUD for merchants, merchant policies, Jev review rules, and local and global Platform drafts. They covered Jev configuration, rejected invalid origins and unauthorized credentials or tenants, and compared custom title and country evidence, missing fields, temporary exact-email matching and immutable replays from both test sources.
- In the isolated browser preview, Platform Admin created and activated a policy for a registered ticket-title field without a browser token prompt. Physical-goods tests at 7,900 minor units in Mock Checkout and Simulation Lab each returned **High risk** and **Block** for a title containing `Disney`. Both records appeared in the same merchant's ledger and showed Mock Jev's **Low risk** answer separately.
- The standard and race Go suites each passed 285 tests and subtests with zero failures or skips against isolated PostgreSQL. TypeScript checking and the isolated Next.js production build passed.

## Decision Engine consolidation

This earlier slice grouped global Platform Rules, Merchant Rules, and administrator AI Configuration and introduced versioned Jev settings. It used a standalone synthetic diagnostic and scenario-based transactions. [Current Decision Engine contract](requirements/decision-engine.md)

### Integrated verification

- The standard and race Go suites each passed 265 tests and subtests with zero failures or skips against the same isolated PostgreSQL database. Go vet, Compose configuration validation, whitespace checks, and both the TypeScript check and production build on Node.js 24.22.3 passed.
- OpenRouter Jev returned `LIVE` for the integrated synthetic diagnostic in 0.99 seconds and for a merchant scenario in 0.49 seconds. The new scenario transaction pinned its Jev configuration version. After the administrator switched the saved configuration to Mock, an exact replay returned the previous decision and metadata.
- Chrome checked the final production frontend's live evidence display with simulated API responses matching the upstream result.

Before the upstream live Jev integration was combined with the Decision Engine changes, the following checks passed.

- The isolated Go diagnostic returned `MOCKED` for Mock. With OpenRouter configured, `POST /api/v1/booking/ai-configuration/jev/test` returned `LIVE` on `typesafe/jev-1.14` in 1.46 seconds with no fallback. The server sent its fixed synthetic fixture.
- Chrome checked the production frontend with simulated responses and then with the real isolated Go and PostgreSQL stack at 320, 390, 1024, and 1440 px. The real-stack check verified administrator save and a `MOCKED` test, both legacy redirects, distinct Merchant Rules sections, and the Advanced Risk Configuration route. The previous Jev rule API snapshot stayed intact. The Merchant UI denied direct access and the configuration GET, PUT, and test requests each returned 403 for the Merchant credential.
- `go test -count=2 -run TestJevConfigurationPreservesReviewRulesPostgres .` passed twice against the same disposable PostgreSQL database. The check toggled configuration versions and verified pinned legacy transaction replay. `go test -race -count=1 -run TestJev .` passed.
- The independent full standard and race Go suites each passed 252 tests and subtests with zero failures or skips. They ran sequentially against the same disposable PostgreSQL database. Three packages ran tests and two had none. Go vet, Compose configuration validation, `git diff --check`, TypeScript checking, and the Node.js production build passed.

## Merchant Management

The original Merchant Management slice added searchable and paginated directory reads, a persistent detail workspace, stored transaction metrics and filters, policy history and a merchant-scoped audit view. The current workspace uses the original Sandbox merchant IDs, credentials and ledger. The production event contract from the team Sheets remains design input. The current Booking API retains its v1 validation. [Local workflow](simulation-runbook.md#merchant-management)

- Before Decision Engine consolidation, the Merchant Management standard and race suites on Go 1.26 each passed 248 tests and subtests with no failures or skips. Go vet, Compose configuration validation, whitespace checks, and the Node.js 24 TypeScript check and production build passed.
- The dedicated PostgreSQL management test covered legacy response compatibility, pagination, search and sort, real aggregates and ledger filters, scoped rule history, audit and revocation, and one-time issuance. Retrying merchant creation with the same idempotency key returned 409 without reissuing the credential.
- Independent Chrome verification against Go and PostgreSQL covered merchant creation, one-time credential issuance, direct detail links, all seven tabs, ledger and audit views, legacy redirects, and cross-tenant denial. The directory and details fit at 320, 390, 1024, and 1440 px.

The team Sheet records **Done (Sandbox)** for `ADM-MER-001` through `ADM-MER-006` and `ADM-MER-008`. The matching status values were read back after the scoped update. PG-16 retains its separate completion record.

## AI Rule Management

The original [AI Rule Management slice](requirements/ai-rule-management.md) uses direct OpenAI Chat Completions to compose merchant policies and Platform Rules drafts. It reuses the existing merchant registration and evidence, applies active policies to new demo merchant transactions in Go, and records the separate decision sources and matched policy versions. The later unified workflow adds conditions checked by Jev at transaction time. [Local configuration and API](simulation-runbook.md#ai-rule-composer)

- The dedicated PostgreSQL policy suite passed tenant isolation, exact-domain normalization, spoof and subdomain rejection, missing and stage-specific evidence, conflicts, disable and rollback, replay, and decision precedence. The latest run across 200 local samples with one rule measured a p95 of 0.401 ms for the policy configuration read and 1.084 µs for evaluation.
- The isolated booking smoke passed two fixture runs covering baseline activation, replay, tenant isolation, and lineage.
- Independent verification of the OpenAI adapter baseline passed 241 Go tests in both standard and race runs, plus Go vet, TypeScript checking, the frontend build, and Compose validation. The local Platform draft, submission, independent approval, and rollback flow passed.
- An isolated live direct OpenAI smoke test with `gpt-4.1-mini` returned Go-validated exact-domain flight proposals for Thai and English prompts using `example.test` in 5.10 s and 1.87 s respectively. The smoke test stopped at interpretation.
- The team Sheet status for PG-16 in `01 ACTION PLAN LATEST!H18` was set to **Done** and read back. This status covers the typed product modules and separation of merchant policy resolution tracked in that row. [Action plan](https://docs.google.com/spreadsheets/d/1bW-Vv10o795JTBwESZnbeY9URWNRkXN3gFVWjCTXX6c/edit#gid=260117)

## Historical merchant scenarios and Jev

The following workflow and results document the original merchant scenario preview. Current tools select the merchant and product directly. [Current requirements](requirements/merchant-scenarios.md)

The original local preview implemented the scenario-based requirements. The original **Merchants** directory let demo administrators create and rename merchants and save multiple named scenarios, each with one booking amount threshold. Creation issued a scoped access token.

Mock Checkout and the **Merchant scenario simulation** panel in Simulation Lab saved rules, sources, and reasons for the selected merchant and scenario. Platform Admin could record a disposition alongside the saved assessment. Merchants could view their own ledger. [Current evaluation steps](simulation-runbook.md#merchant-scenarios-and-jev)

The original preview used a structured threshold form and direct demo review. The later AI Rule Management release added the composer and separate Merchant Policy engine. [AI Rule Management](#ai-rule-management) The full HOLD lifecycle in this merchant workspace remains open.

The upstream live Jev slice uses the OpenRouter Decisions API from Go in the two merchant-selected tools. Absent a saved AI Configuration version, the sandbox can enable it with `PAYGUARD_AI_MODE=live` and a server-side `OPENROUTER_API_KEY`. The pinned default model is `typesafe/jev-1.14`. With an observed amount, the stored result includes the typed Jev choice, provider decision ID, and served model when available. Local review rules can escalate the assessment, and a provider failure sends the case to manual review. The integration sends only the booking amount, scenario threshold, and whether a configured review rule matched. Credentials stay server-side. The deployment default is Mock. [Team Blueprint](https://docs.google.com/spreadsheets/d/1bW-Vv10o795JTBwESZnbeY9URWNRkXN3gFVWjCTXX6c/edit#gid=1604066438) · [OpenRouter Decisions API for Jev](https://openrouter.ai/typesafe/jev-1.14)

Before consolidation, independent Chrome verification on port 3001 recorded live `REVIEW` at 50,000 against a checkout threshold of 50,000 and live `LOW_RISK` at 50,000 against a single-scenario Simulation Lab threshold of 100,000. Both saved transactions showed distinct provider decision IDs and the served `typesafe/jev-1.14-20261014` model. Exact checkout replay preserved the saved metadata. The simulated timeout saved **Unresolved** under **Needs decision** without a provider decision ID.

The upstream backend passed `go test ./...` and `go vet ./...`. Tests covered the outbound evidence fields, typed response validation, provider failures, timeout, and replay flags. The Next.js production build and both local Compose configuration checks passed. Docker reported both app services healthy.

The earlier merchant assessment plan proposed `LOW_RISK`, `HIGH_RISK`, and `REVIEW`. **Review** covers both flagged cases and **Unresolved** assessments. Jev evidence, manual disposition, transaction stage, and payment action remain separate. The high-risk trigger and precedence are pending policy decisions. See the current rules section for implemented behavior. [Planned PayGuard risk results](requirements/merchant-scenarios.md#planned-payguard-risk-results)

The opt-in loopback preview opens the Merchants workspace and scenario pickers as a demo administrator. Set the Next server’s `PAYGUARD_LOCAL_MERCHANT_PREVIEW_TOKEN` to the API’s demo administrator token to enable it. The server uses that token for the loopback proxy. After creating a merchant, the administrator can copy that merchant’s scoped access token from the one-time confirmation. The old Risk Operations route redirects to **Merchants → Transactions** in this preview. Standalone routes and the ordinary authenticated flow remain available when the opt-in is disabled.

The administrator manages a selected merchant’s scenarios, **Jev Rules**, and **Platform Rules**. Published amount-based Jev rules apply to Mock Checkout and the single-booking merchant scenario simulation, with the supplemental ruleset version pinned in the result. Platform built-ins remain read-only, and Platform drafts are for testing. A Platform draft preview requires a compatible saved Simulation Lab run for the selected merchant.

Merchant creation and editing now save the original setup fields on the merchant record. The demo supports OTA or e-commerce, the Booking stage, the required booking timestamp, and optional booking amount availability. Existing name-only records appear as **Setup incomplete**, and the legacy name-only API remains compatible. The former standalone setup page opens **Merchants** in the automatic preview, and its separate navigation entry is removed. A confirmed **Delete merchant** removes the merchant from the directory and test pickers and deactivates its access. Stored history is retained and excluded from normal access.

- Live API checks verified the administrator directory and merchant-scoped access. In both Mock Checkout and Simulation Lab, an amount of 50,000 minor units returned **Low risk** at a threshold of 100,000 and **Review** at a threshold of 50,000.
- Live API checks verified missing amount versus zero, timeout and review resolution, replay pinned to an earlier revision, and cross-merchant denial.
- Go tests, race tests, and vet passed. The isolated frontend production build, TypeScript checks, and existing booking smoke checks also passed.
- Independent browser checks verified merchant creation, one-time token display, and saving two scenario drafts. The browser showed a **Low risk** checkout, a **Review** simulation with a matching rule, and an **Unresolved** simulation after a mock timeout. The merchant’s ledger contained exactly three transactions with the expected sources and outcomes. A direct link opened the saved transaction, and sign-in with the issued token showed only the merchant’s own workspace with merchant creation and editing unavailable.
- Before the merge, Chrome verified automatic loopback access, creating a merchant and scenario, following a deep link to checkout, and reopening the saved transaction after refreshing the ledger.
- Independent Chrome checks verified that the three old operations and rule URLs open their merchant workspace views without login. Two merchants started with the same scenario threshold and both returned **Low risk**. After publishing a Jev rule for merchant A, A returned **Review** and B remained **Low risk**. A Platform draft stayed scoped to its merchant and persisted on reload. Previously saved transactions retained their results.
- Earlier API checks verified rejection of unauthenticated direct access and disallowed preview origins and hosts, and isolated create and edit operations for Platform drafts. New checkout and scenario simulation results pinned the selected merchant’s supplemental Jev revision, and replay returned the original result.
- On the merged preview, Chrome verified delivery of the one-time scoped merchant credential. Two merchant credentials opened independently scoped, read-only ledgers. The Merchant workspace rejected an invalid credential and an administrator credential. Opening an administrator URL returned the signed-in merchant to **Transactions**. Merchant views had no mutation controls.
- The merged API returned HTTP 401 without credentials, HTTP 404 for a transaction belonging to another merchant, and HTTP 403 when a merchant requested configured rules or attempted a mutation. Each merchant could list and open its own saved transactions.
- Independent Chrome checks verified create, edit, and reload for two merchants. The legacy setup URL redirected to Merchants, and the sidebar had no standalone setup link. Canceling deletion kept the test merchant. Confirming deletion removed only that merchant, and both Mock Checkout and Simulation Lab omitted it from their pickers.

## P0 Dashboard layout refactor

The [Next.js root layout](../frontend/app/layout.tsx) wraps every page in one [AppShell](../frontend/app/app-shell.tsx). `/` redirects to `/dashboard`, and [Simulation Lab](../frontend/app/simulation-lab/page.tsx) opens at `/simulation-lab`. The desktop sidebar collapses from 240 px to 64 px, and a drawer serves narrow screens. The topbar holds the breadcrumb, workspace, notifications, user controls, and Sandbox status.

[Fraud Overview](../frontend/app/dashboard/page.tsx) summarizes saved merchant records and shows recent transactions. An empty ledger displays an empty state. The administrator’s **Transactions** link opens the merchant transaction workspace in the automatic preview. At this stage, the merchant sidebar had one destination, **Transactions**, and the merchant credential scoped the list and detail on the API. **Fraud Network** opened the evidence section in Simulation Lab. See the [current batch navigation](#batch-simulation-decision-evidence).

The menu review compared the combined workspace with GitHub `origin/main` at `74e7a88` and the current Drive action plan. The plan puts merchant setup, scoring profiles, Jev, and Mock Checkout in the demo, with the HOLD queue, dashboard KPIs, advanced rule builder and fraud-network work in later phases. The current request gives Platform Admin the merchant configuration and cross-merchant transaction workflows and limits merchant access to its own transaction list and details. The linked System Handbook returned a Drive permission error. The current Blueprint and Rule Builder Manual were accessible. [Team Drive](https://drive.google.com/drive/folders/1lKpQdDOkrqpF61vxN_wNN2HHo9NQjNnH) · [Action plan](https://docs.google.com/spreadsheets/d/1bW-Vv10o795JTBwESZnbeY9URWNRkXN3gFVWjCTXX6c/edit#gid=260117) · [Simulation Lab](https://docs.google.com/spreadsheets/d/1bW-Vv10o795JTBwESZnbeY9URWNRkXN3gFVWjCTXX6c/edit#gid=260115) · [Merchant policies](https://docs.google.com/spreadsheets/d/1bW-Vv10o795JTBwESZnbeY9URWNRkXN3gFVWjCTXX6c/edit#gid=260109) · [Rule Builder Manual](https://docs.google.com/document/d/1vYpo8hkpWS3jQ21ZiRBNThf8uUh2ZdVf_AtoNL7hQho/edit)

Chrome followed all eight Platform Admin sidebar links in the merged automatic preview. **Dashboard**, **Merchants**, **Scoring Profiles**, **Mock Checkout**, and **Simulation Lab** opened their respective pages. **Transactions** opened the ledger, **Fraud Network** jumped to **Event decisions** in Simulation Lab, and both **Settings** links worked. The check also verified `/` opens `/dashboard` and a legacy root link with merchant and scenario parameters opens `/simulation-lab` with the exact saved scenario selected.

Before the local reset, Dashboard matched API reads for 22 active merchants, 37 saved transactions, and 20 transactions needing a decision. A recent transaction link opened its exact saved detail. Both the Jev and Platform rule catalogues loaded from the selected merchant’s workspace.

The final backend Docker build stage passed `go test ./...` and `go vet ./...`. The Next.js webpack production build, TypeScript checks, Compose configuration validation, and whitespace checks passed.

The PostgreSQL reset on the active local assessment stack for port 3001 left zero merchants, scenarios, transactions, bookings, drafts, simulation runs, and QA labels. Omise charges remained at zero. The reset was scoped to that local stack.

Before the merchant-workspace merge, the dashboard branch recorded these checks.

- `go test ./...`, `npm run typecheck`, `npm run build` on Node.js 24.22.3, and `git diff --check` passed. HTTP checks verified the root redirect, the frontend routes, and read API parity for the Platform Rules catalogue, Omise configuration, Jev Rules, and Risk Operations.
- Chrome checked all 13 destinations at 320, 390, 1024, and 1440 px with one shell and no horizontal page scrolling. Desktop checks verified 240 and 64 px sidebar widths, breadcrumbs, and active links. Mobile checks covered drawer navigation, keyboard dismissal, focus return, and backdrop dismissal. Platform Admin could open and cancel a draft. In Merchant view, a direct Platform Rules reload displayed the restricted view and sent no Platform Rules API requests.

## Integrated main candidate

The earlier local integration candidate combined merchant and booking flows, Jev Rules, Risk Operations and governance with the Simulation Lab storefront, provider enrichment, Omise Sandbox checkout, and earlier project documentation.

- Go race tests and vet, Docker Compose configuration validation, and the frontend type check and Next.js production build on Node.js 24 passed.
- The booking smoke script passed two fixtures and verified `COLD_START` to `ACTIVE` baseline activation, replay, tenant isolation, and lineage.
- The integrated Go API with PostgreSQL and Redis returned both Jev Rules and Platform Rules. Live local checks verified the same run ID across separate tenants, tenant-scoped QA, Gateway, and rejection of blocked and cross-workspace Omise requests before a provider call. The enrichment check imported all 458,044 BINs and matched a disposable-email domain. These integration fixtures used local data and made no external charges.
- Independent Chrome checks verified all seven navigation routes and active-link states, Jev Rules, Mock Checkout, both storefronts, and the Omise control on an eligible shared-demo run. Gateway stayed simulated. Scoped run and QA requests carried the merchant credential, isolation held, and clearing the token removed previous evidence. The 390 px viewport had no horizontal overflow. Chrome sent no external requests in this check.

Omise uses the original synthetic demo prices for the shared Travel and Online store checkout. Merchant-scoped and Gateway flows use their simulated checkout. The [implementation progress table](payguard-tasks.md#verified-implementation-progress) and the historical verification below record their respective scopes.

## Earlier merges through `e58e59a`

- Simulation Lab delivered the synthetic storefronts, seeded runs, evidence and entity graph, and offline QA. [GitHub PR #1](https://github.com/pwatakul/payguard/pull/1)
- Checkout Contact, the platform System Rules dashboard, and the supplied PayGuard logo merged. Chrome verified the PNG in both headers at desktop and mobile widths. [GitHub PR #2](https://github.com/pwatakul/payguard/pull/2)

## PG-11 through PG-19 local evaluation

Local evaluation uses synthetic bookings and outcomes. The [evaluation runbook](simulation-runbook.md#local-evaluation) walks through the operations queue, fixed-window shadow replay, change approvals, and linked-entity simulation.

| Task | Local implementation |
| --- | --- |
| PG-11 | Demo merchant, viewer, and analyst credentials resolve a tenant and actor. Transaction details show masked synthetic contact information. Analyst reveals record actor, reason, and time in PostgreSQL. |
| PG-12 | Manual-review assessments appear as HOLDs. Analysts can claim and resolve their own cases or explicitly escalate an overdue case. The local deadline is 15 minutes and dispositions are review records. |
| PG-13 | Risk Operations shows a tenant-scoped transaction list, booking and assessment evidence, and KPIs computed from stored records and receipt and assessment timestamps. |
| PG-14 | A fixed holdout window and data cutoff compare recorded decisions with a candidate velocity rule. Reports include recall, false-positive rate, exclusion counts, and pinned versions. |
| PG-15 | Tenant change proposals support profiles, rules, rubrics, and product review lists. A different analyst records a decision. Expiry and rollback revisions preserve the history. |
| PG-16 | Simulation Lab uses separate travel, e-commerce, and gateway product modules. Merchant policy evaluates the exact configured flight domain separately and reports the action source. |
| PG-17 | An asynchronous dossier builds a typed, masked evidence export from a saved assessment and its versions. |
| PG-18 | A demo merchant token scopes saved Simulation Lab network runs and linked entity evidence to the merchant. |
| PG-19 | Risk Operations stores dashboard metrics and default filters per demo actor in the browser. The rule workspace provides a fixed-window preview and navigation to the visual rule editor. |

- Live PostgreSQL and Redis checks verified role gates and cross-tenant denial, masked and audited reveal, concurrent claim exclusivity, idempotent resolution, explicit overdue escalation and reassignment, and product, outcome, and HOLD filters. The queue returned measured assessment latency and freshness. The operations smoke passed on both an independent stack and the combined preview.
- A fixed shadow holdout replayed unchanged. The recorded decisions produced one true negative and one false negative. The candidate produced one true positive and one false positive. Assessment snapshots remained identical, and the tenant boundary held.
- Governance checks verified distinct maker and checker actors, decision replay and conflict, tenant denial, rejection, and rollback as a new revision. Product and policy checks covered the gateway stages and exact domain matching. The asynchronous dossier persisted typed citations and omitted raw contact and outcome data.
- Identical tenant-scoped network fixtures produced distinct entity keys across tenants. Reading another tenant’s run failed, and a legitimate shared IP alone did not trigger a block.
- Go tests and vet passed. The frontend production build and type check passed. Independent Chrome checks verified viewer masking, real queue filters and KPIs, dossier export, browser preferences by actor, shadow metrics, and rejection by a separate analyst. The 390 px layout fit without horizontal overflow.

PG-20 requires approved real outcome labels and shadow results. PG-21 requires Security and Legal review.

## Jev Rules

Implemented and verified the local demo for drafting a booking review rule from a supported prompt or AI-generated JSON, previewing it against saved assessments, and publishing it to the merchant’s Jev ruleset. [Requirements](requirements/jev-rules.md) · [UI](ui/jev-rules.md) · [Runbook](simulation-runbook.md#jev-rules)

- Independent API checks passed for unauthorized requests, strict validation, merchant isolation, read-only previews, amount threshold boundaries, missing versus zero and unselected amount, and AND conditions. New assessments pinned rule versions and hits, timeout kept its review handoff, and replay returned the prior result. Concurrent distinct pushes retained both rules. Identical and reordered-AND retries deduplicated.
- Chrome with the isolated Go API verified navigation from Simulation Lab, loading the default rules, drafting and reviewing a prompt, preview, and publication of version `booking-rules-v2`. The token stayed out of local and session storage, and the page fit a 390 px viewport. Local parser checks and the mocked UI test also passed.
- The isolated HTTP suite passed against a fresh database, and the published revision survived a backend rebuild and restart. `go test ./...`, `go test -race ./...`, and `go vet ./...` passed. The final frontend passed `npm run typecheck` and `npm run build`. `git diff --check` passed.

Implementation lives in the [Jev rules API and evaluator](../backend/booking_jev_rules.go), [booking assessment](../backend/booking_assessments.go), [Go regression tests](../backend/booking_jev_rules_test.go), and [Jev Rules page](../frontend/app/settings/jev-rules/page.tsx).

## PG-07 through PG-10 mock checkout, baseline and booking lineage

Implemented and verified the local synthetic checkout, outcome import, baseline activation, and tenant-scoped lineage. The demo runs at `/settings/mock-checkout`. [Requirements](requirements/checkout-and-lineage.md) · [Checkout UI](ui/mock-checkout.md) · [Runbook](simulation-runbook.md#mock-checkout-and-booking-lineage)

- PG-07 Chrome checks covered navigation and all four products, OTA checkout with amount zero, and e-commerce checkout with omitted amount, unavailable SDK data, and a Jev timeout. The visible assessment matched the saved response and lineage. A simulated lost response retried the same booking and got 200 from both APIs. The token stayed in memory and the page fit a 390 px viewport.
- PG-08 live checks verified that exactly three distinct eligible bookings moved the baseline from `COLD_START` to `ACTIVE`. Chrome displayed the active sample count. Earlier bookings retained their pinned baseline and rule version.
- PG-09 `python3 scripts/booking-smoke.py --base http://127.0.0.1:8082 --merchant b --threshold 3` passed. Both fixture passes produced `LOW_RISK` at velocity 1, `REVIEW` at velocity 5, and `UNRESOLVED` at velocity 1. A rerun on the same database preserved those signatures and advanced the active baseline by three samples. Duplicate replay and tenant scope passed, and assessment payloads contained no QA labels. The previous PG-01 and PG-03 through PG-06 API checks passed on merchant A.
- PG-10 live checks covered separately recorded analyst and chargeback provenance, out-of-order event and receipt timestamps, observed, unavailable and unknown SDK states, tenant isolation, and replay of the original assessment after outcomes were imported.
- Go race tests and vet, frontend type checking and production build, and `git diff --check` passed. DEC-04 and DEC-05 remain open. The team action plan is unchanged.

## PG-03 through PG-06 booking assessment

Implemented and verified all four tasks for the [local preview](http://127.0.0.1:3001/settings/booking-assessment). The demo covers deterministic Scoring Profile proposals and approval, booking evidence with pinned versions, trusted booking velocity and cold-start evidence, and typed mock Jev results with a timeout handoff. [Requirements and contract](requirements/booking-assessment.md) · [UI](ui/booking-assessment.md) · [Runbook](simulation-runbook.md#booking-assessment)

- PG-03 live API checks verified bearer authentication, tenant separation, the same proposal for reversed signal order, and rejection of unavailable and future-stage signals.
- PG-04 checks verified that changing the active profile preserves the original assessment and version on replay. Concurrent same-mode requests replayed the stored result, and changing mode for an assessed booking returned a conflict.
- PG-05 checks with PostgreSQL and Redis produced the same count of five in two fixture runs and exercised the elevated velocity rule. The boundary checks excluded bookings exactly 60 seconds earlier, bookings with future event timestamps, and earlier bookings received after the target. The API returned distinct evidence for a missing amount and an explicit zero and reported `COLD_START`.
- PG-06 checks verified typed Jev lineage and the timeout outcome `UNRESOLVED` with a `MANUAL_REVIEW` handoff. Independent Chrome verification exercised the missing-setup state, navigation from saved setup, explicit approval, a zero-amount booking, timeout, and same-booking retry after a lost response. The token remained out of persistent browser storage and the page fit a 390 px viewport.
- Rebuilding and recreating the local backend preserved the pinned assessment. The replay returned identical JSON, and the full API smoke checks passed again.
- Go unit tests, race tests and vet passed, as did frontend type checking, the isolated production build, and `git diff --check`. The broader action plan status is unchanged.

Implementation lives in the [profile API](../backend/booking_profiles.go), [assessment API](../backend/booking_assessments.go), [booking risk tests](../backend/booking_risk_test.go), and [demo screen](../frontend/app/settings/booking-assessment/page.tsx).

## PG-02 merchant setup

Implemented and independently verified the [initial local preview](http://127.0.0.1:3001/settings/merchant-setup). The form saves the merchant business model, Booking stage, and available signals as a schema version 1 record in the current browser. [Requirements](requirements/merchant-setup.md) · [UI](ui/merchant-setup.md)

- `npm run typecheck`, `npm run build` in an isolated directory, and `git diff --check` passed.
- Chrome 157 verified navigation from Simulation Lab and System Rules, active navigation, missing-stage feedback, and save and reload for an OTA setup with booking amount. Editing the setup to e-commerce with only the required `occurredAt` signal persisted after reload.
- Chrome verified field-level errors for an invalid saved stage and signal, followed by a successful correction. A simulated `QuotaExceededError` retained the selections and save succeeded on retry. The page fit a 390 px viewport without horizontal overflow.
- The implementation added the [route](../frontend/app/settings/merchant-setup/page.tsx), [form](../frontend/app/settings/merchant-setup/setup-form.tsx), and shared navigation for Simulation Lab and System Rules. [PG-02 through PG-06 implementation](https://github.com/pwatakul/payguard/commit/c08fe09fb3b4b9cacb19ab9f32411078586151ad)
- The broader action plan status and integrated PG-01 verification are pending.

## Simulation Lab storefront and Omise Sandbox

Implemented and verified locally on `codex/omise-simulation-checkout`. The lab offers **Travel** and **Online store**, an inline THB summary, and the existing synthetic controls and contact. Eligible first orders in the shared synthetic demo can use the Omise-hosted test card form. Merchant-scoped scenarios and the Gateway checkout use simulated payments. [Storefront UI](ui/simulation-lab.md), [sandbox runbook](simulation-runbook.md#omise-sandbox-checkout)

- Go tests, race checks, and vet passed. Frontend type checking and the Next.js production build passed on Node.js 24.
- The Omise Thailand test-account check confirmed THB support. Sandbox API tests completed a flight for two travelers at 918,000 satang and two physical items at 498,000 satang. Duplicate and concurrent requests for the same run returned its original charge ID. Runs with final decisions of `BLOCK` and `UNSUCCESSFUL` were denied at checkout.
- Chrome completed the official Omise-hosted form on localhost and showed successful test payments for the Travel flight at 459,000 satang and the Online store digital gift card at 75,000 satang on mobile.
- Independent Chrome checks covered all four product images and selection, booking and delivery controls, quantity and THB totals, contact and decision evidence, and Offline QA. A changed cart or blocked first checkout disabled payment. A fresh `ALLOW` enabled it. The page fit 390 px and 320 px viewports without horizontal scrolling. Independent Go race and vet checks passed, and Rule Base and enrichment paths matched their branch base.

## Provider enrichment

Implemented and verified locally on `codex/provider-enrichment`. The authenticated Go endpoint returns IP, BIN, and email-domain evidence. Local BIN and domain masters operate with external services disabled. Rule Base files match the upstream base at `a97fcbe`. [Requirements](requirements/provider-enrichment.md), [runbook](simulation-runbook.md#provider-enrichment)

- Go race tests, vet, Docker Compose configuration validation, and the backend image build passed. The container importer reused the PostgreSQL snapshot, and the container user could read the local domain overlay. Provider fallback, normalization, and cache TTL checks used mocked responses.
- The PostgreSQL importer loaded 458,044 BINs, including 454,496 six-digit and 3,548 eight-digit records. Of these, 220,620 had all four canonical fields, including 217,083 six-digit and 3,537 eight-digit records.
- The local HTTP test returned complete exact matches for all six sampled BINs (`424242`, `540131`, `511332`, `371449`, `45717360`, and `43638410`). `010051` returned partial evidence under `UNKNOWN`. `00210299` returned its shorter match as reference evidence only. `00000000` returned no master match.
- Email checks covered 231,582 bundled domains and the 244,118-domain union with the local overlay. `mailinator.com` and a child domain returned all four source identifiers. `gmail.com` returned `listed: false`.
## PG-01 booking intake

Implemented the versioned booking intake on the local branch. The [runbook](simulation-runbook.md#booking-event-intake) documents the demo bearer token configuration, event contract, and replay responses.

The pasted merchant data synthesis is recorded in the [Merchant event intake contract](requirements/merchant-event-contract.md). The note distinguishes Booking Event v1 from the proposed production envelope and stage and product extensions. Resolving the hotel guest-count and digital delivery-method requirements and publishing a single OpenAPI contract are pending.

- Go tests and vet passed in the Docker Go toolchain. Live checks passed for validation, authentication, concurrent idempotency, conflicting retries, tenant separation, and missing versus zero amounts.
- An identical request returned the saved response after a backend restart. Verification against the latest upstream application changes is pending. The team Sheet remains open until that integration is verified.

## System Rules dashboard · 21 November 2026

The dashboard shows active platform built-ins in evaluation order, saves editable drafts in PostgreSQL, and previews candidate conditions against one selected synthetic run. Active rule execution and Merchant Policy retain their existing behavior. See the [requirements](requirements/system-rules.md), [UI](ui/system-rules.md), and [runbook](simulation-runbook.md#system-rules).

- Go race tests and vet passed. Frontend type checking and the Next.js production build passed on Node.js 24.
- Live API checks with PostgreSQL and Redis verified the fresh rule catalogue, built-in protection, typed and stage-aware validation, draft edits, stale-version rejection, and persistence after a backend restart.
- Repeated previews returned the same results and skipped events with missing signals. Both the saved decision record and a fresh same-seed run were unchanged after draft and preview operations.
- Chrome checks on desktop and mobile covered navigation, search and filters, built-in details, draft creation and editing, retry and conflict states, and the one-to-five-condition limit. Changing stages cleared unavailable signals. A saved-run preview evaluated 12 events and matched 5, with matching event IDs shown in the editor.

## Simulation Lab checkout contact · 21 November 2026

The first order displays the submitted synthetic contact in both stages. The full contact stays in browser memory, and Go receives only the normalized `.test` email domain. See the [requirements](requirements/simulation-lab.md), [UI](ui/simulation-lab.md), and [runbook](simulation-runbook.md#verify-checkout-contact-and-merchant-policy).

- Go race tests and vet passed. Frontend type checking and the Next.js production build passed on Node.js 24.
- Live API tests with PostgreSQL and Redis verified domain validation, case-insensitive replay, a 409 for a different domain under the same run ID, first-order scope, exact policy matching, disabled and non-flight policy behavior, and timeout precedence.
- Chromium verified editable contact and the submitted snapshot, including invalid email and a simulated 503. Request and response checks and a PostgreSQL query confirmed that the submitted name, full email, and phone were absent from API traffic and persisted run data.

## Simulation Lab · 21 November 2026

Local implementation and verification complete for the [requirements](requirements/simulation-lab.md) and [UI](ui/simulation-lab.md). Use the [runbook](simulation-runbook.md) to start the demo and repeat the checks.

- Go race tests and vet passed. Frontend clean install, type check and Next.js production build passed on Node.js 24.
- A live Go API smoke test with PostgreSQL 16 and Redis 7 passed for all four products, concurrent idempotent replay, conflicting run IDs, deterministic results across run IDs, event ordering, duplicate PSP retries, shared Wi-Fi, rotating ring IPs, isolated labels, exact-domain policy, missing token and mock timeout. A Redis outage returned 503 and left no completed run.
- Chrome smoke checks exercised the setup, decisions and offline QA for normal e-commerce and mixed OTA, including the mobile layout. The default mixed run produced 24 decisions, deduplicated 12 PSP retries and scored 12 synthetic payment samples.
- The [Google Drive System Handbook](https://docs.google.com/document/d/1RtzQI1VIRXabDKUBPGcfQM8NxJZs1u_geFWFYC8Hq_I/edit) records the original local scope in section 12. Production rollout, production merchant authentication, live Jev, and production payment integration are future work.
