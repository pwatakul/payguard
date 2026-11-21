package main

import (
	"bytes"
	"context"
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func openPolicyTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()
	address := os.Getenv("PAYGUARD_POLICY_TEST_DATABASE_URL")
	if address == "" {
		t.Skip("PAYGUARD_POLICY_TEST_DATABASE_URL is unset")
	}
	parsed, err := url.Parse(address)
	if err != nil || !strings.HasPrefix(parsed.Path, "/payguard_policy_test_") {
		t.Fatal("A dedicated database named payguard_policy_test_* is required.")
	}
	db, err := pgxpool.New(t.Context(), address)
	if err != nil {
		t.Fatal("Could not configure dedicated test database.")
	}
	t.Cleanup(db.Close)
	if err := initialize(t.Context(), db); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestMerchantPolicyLifecyclePostgres(t *testing.T) {
	db := openPolicyTestDatabase(t)
	repo := repository{db}
	admin := "merchant-policy-test-admin"
	mux := http.NewServeMux()
	registerMerchantDemo(mux, repo, admin)
	request := func(method, path, token string, value any, status int, output any) {
		t.Helper()
		var body bytes.Buffer
		if value != nil {
			if err := json.NewEncoder(&body).Encode(value); err != nil {
				t.Fatal(err)
			}
		}
		req := httptest.NewRequest(method, path, &body)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != status {
			t.Fatalf("%s %s: got status %d, expected %d: %s", method, path, response.Code, status, response.Body.String())
		}
		if output != nil {
			if err := json.Unmarshal(response.Body.Bytes(), output); err != nil {
				t.Fatal(err)
			}
		}
	}
	createMerchant := func(name string) (mockMerchant, string) {
		t.Helper()
		var created struct {
			Merchant mockMerchant `json:"merchant"`
			Token    string       `json:"token"`
		}
		request(http.MethodPost, merchantDemoPath, admin, mockMerchantInput{
			Name: name, Setup: &merchantSetup{SchemaVersion: 1, BusinessModel: "ota", Stage: "booking",
				AvailableSignals: []string{bookingTimestamp, bookingAmount}},
			PolicyIntegration: &merchantPolicyIntegration{Products: []string{"flight_ticket", "hotel_room", "digital", "payment_gateway"}, EmailDomain: true},
		}, http.StatusCreated, &created)
		if created.Token == "" {
			t.Fatal("Merchant credential missing.")
		}
		return created.Merchant, created.Token
	}
	first, tokenA := createMerchant("Policy test merchant A")
	second, tokenB := createMerchant("Policy test merchant B")
	policyEndpoint := merchantPolicyPath + "/rules"
	request(http.MethodGet, policyEndpoint, "", nil, http.StatusUnauthorized, nil)
	request(http.MethodGet, policyEndpoint, admin, nil, http.StatusForbidden, nil)
	var profile merchantPolicyProfile
	request(http.MethodGet, merchantPolicyPath+"/profile", tokenA, nil, http.StatusOK, &profile)
	if profile.BusinessName != first.Name || len(profile.Products) != 4 || len(profile.Stages) != 2 {
		t.Fatalf("profile: %+v", profile)
	}

	type change struct {
		Rule merchantPolicyRule `json:"rule"`
	}
	policy := exampleMerchantPolicy()
	var saved change
	request(http.MethodPost, policyEndpoint, tokenA, map[string]any{"proposal": policy}, http.StatusCreated, &saved)
	if saved.Rule.Status != "ACTIVE" || saved.Rule.Version != 1 {
		t.Fatalf("saved: %+v", saved.Rule)
	}
	rulePath := policyEndpoint + "/" + saved.Rule.ID
	request(http.MethodGet, rulePath, tokenB, nil, http.StatusNotFound, nil)
	request(http.MethodPost, rulePath+"/disable", tokenB, map[string]int{"expectedVersion": 1}, http.StatusNotFound, nil)
	request(http.MethodPost, rulePath+"/simulate", tokenB, nil, http.StatusNotFound, nil)
	var otherRules struct {
		Rules []merchantPolicyRule `json:"rules"`
	}
	request(http.MethodGet, policyEndpoint, tokenB, nil, http.StatusOK, &otherRules)
	if len(otherRules.Rules) != 0 {
		t.Fatal("Tenant B received tenant A rules.")
	}

	firstScenario, err := repo.saveMockScenario(t.Context(), first.ID, "", "Flight acceptance", 5000)
	if err != nil {
		t.Fatal(err)
	}
	secondScenario, err := repo.saveMockScenario(t.Context(), second.ID, "", "Flight acceptance", 5000)
	if err != nil {
		t.Fatal(err)
	}
	newTransaction := func(merchant mockMerchant, scenario mockScenario, id, domain string, expected int) mockTransaction {
		t.Helper()
		var result struct {
			Transaction mockTransaction `json:"transaction"`
		}
		amount := int64(50)
		request(http.MethodPost, merchantDemoPath+"/"+merchant.ID+"/transactions", admin,
			mockTransactionInput{ID: id, ScenarioID: scenario.ID, Source: "checkout", Product: "flight_ticket", AmountMinor: &amount,
				CustomerEmailDomain: domain}, expected, &result)
		return result.Transaction
	}
	blocked := newTransaction(first, firstScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9900", "SKYCANNNER.COM", http.StatusCreated)
	if blocked.PolicyDecision.Final.Action != "BLOCK" || blocked.PolicyDecision.Final.Source != "MERCHANT_POLICY" ||
		blocked.PolicyDecision.Platform.Action != "ALLOW" || blocked.PolicyDecision.Merchant.RuleVersions[saved.Rule.ID] != 1 ||
		blocked.Outcome != "LOW_RISK" {
		t.Fatalf("blocked result: %+v", blocked.PolicyDecision)
	}
	allowed := newTransaction(second, secondScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9900", "skycannner.com", http.StatusCreated)
	if allowed.PolicyDecision.Final.Action != "ALLOW" {
		t.Fatalf("tenant B result: %+v", allowed.PolicyDecision)
	}
	allowed = newTransaction(first, firstScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9903", "sub.skycannner.com", http.StatusCreated)
	if allowed.PolicyDecision.Final.Action != "ALLOW" {
		t.Fatalf("subdomain result: %+v", allowed.PolicyDecision)
	}

	activateOverlappingRule := func(action, name string, overlapIDs ...string) merchantPolicyRule {
		t.Helper()
		proposal := exampleMerchantPolicy()
		proposal.Action, proposal.Name = action, name
		var pending change
		request(http.MethodPost, policyEndpoint, tokenA, map[string]any{"proposal": proposal}, http.StatusCreated, &pending)
		if pending.Rule.Status != "PENDING_APPROVAL" || len(pending.Rule.Preview.ConflictRuleIDs) != len(overlapIDs) {
			t.Fatalf("pending: %+v", pending.Rule)
		}
		path := policyEndpoint + "/" + pending.Rule.ID + "/activate"
		request(http.MethodPost, path, tokenB, map[string]int{"expectedVersion": 1}, http.StatusNotFound, nil)
		request(http.MethodPost, path, tokenA, map[string]int{"expectedVersion": 2}, http.StatusConflict, nil)
		var active change
		request(http.MethodPost, path, tokenA, map[string]int{"expectedVersion": 1}, http.StatusOK, &active)
		if active.Rule.Status != "ACTIVE" || active.Rule.Version != 2 || active.Rule.ActiveVersion != 2 ||
			active.Rule.Preview.ApprovalReason != "" || len(active.Rule.Preview.ConflictRuleIDs) != len(overlapIDs) {
			t.Fatalf("activated: %+v", active.Rule)
		}
		for _, id := range overlapIDs {
			if !slices.Contains(active.Rule.Preview.ConflictRuleIDs, id) {
				t.Fatalf("activated version omitted overlap with %s: %+v", id, active.Rule.Preview)
			}
		}
		return active.Rule
	}
	review := activateOverlappingRule("REVIEW", "Review the same flight", saved.Rule.ID)
	challenge := activateOverlappingRule("CHALLENGE", "Challenge the same flight", saved.Rule.ID, review.ID)
	assertPolicyDecision := func(transaction mockTransaction, action string, versions map[string]int) {
		t.Helper()
		decision := transaction.PolicyDecision
		if decision == nil || decision.Final != (policyFinal{action, "MERCHANT_POLICY"}) ||
			decision.Merchant.Action != action || !maps.Equal(decision.Merchant.RuleVersions, versions) ||
			len(decision.Merchant.MatchedRuleIDs) != len(versions) {
			t.Fatalf("expected %s with rules %v, got %+v", action, versions, decision)
		}
		for ruleID := range versions {
			if !slices.Contains(decision.Merchant.MatchedRuleIDs, ruleID) {
				t.Fatalf("matched rules %v omitted %s", decision.Merchant.MatchedRuleIDs, ruleID)
			}
		}
	}
	allVersions := map[string]int{saved.Rule.ID: 1, review.ID: 2, challenge.ID: 2}
	multiMatch := newTransaction(first, firstScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9906", "skycannner.com", http.StatusCreated)
	assertPolicyDecision(multiMatch, "BLOCK", allVersions)

	request(http.MethodPost, rulePath+"/disable", tokenA, map[string]int{"expectedVersion": 1}, http.StatusOK, nil)
	replayed := newTransaction(first, firstScenario, blocked.ID, "skycannner.com", http.StatusOK)
	assertPolicyDecision(replayed, "BLOCK", map[string]int{saved.Rule.ID: 1})
	replayed = newTransaction(first, firstScenario, multiMatch.ID, "skycannner.com", http.StatusOK)
	assertPolicyDecision(replayed, "BLOCK", allVersions)
	reviewed := newTransaction(first, firstScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9901", "skycannner.com", http.StatusCreated)
	assertPolicyDecision(reviewed, "REVIEW", map[string]int{review.ID: 2, challenge.ID: 2})

	request(http.MethodPost, policyEndpoint+"/"+review.ID+"/disable", tokenA, map[string]int{"expectedVersion": 2}, http.StatusOK, nil)
	challenged := newTransaction(first, firstScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9907", "skycannner.com", http.StatusCreated)
	assertPolicyDecision(challenged, "CHALLENGE", map[string]int{challenge.ID: 2})

	var rollback change
	request(http.MethodPost, rulePath+"/rollback", tokenA, map[string]int{"version": 1, "expectedVersion": 2}, http.StatusOK, &rollback)
	if rollback.Rule.Version != 3 || rollback.Rule.Status != "PENDING_APPROVAL" ||
		!slices.Equal(rollback.Rule.Preview.ConflictRuleIDs, []string{challenge.ID}) {
		t.Fatalf("rollback=%+v", rollback.Rule)
	}
	request(http.MethodPost, rulePath+"/activate", tokenA, map[string]int{"expectedVersion": 3}, http.StatusOK, nil)
	afterRollback := newTransaction(first, firstScenario, "9fae5a36-59ae-4fb5-a773-f62b3bce9902", "skycannner.com", http.StatusCreated)
	assertPolicyDecision(afterRollback, "BLOCK", map[string]int{saved.Rule.ID: 4, challenge.ID: 2})

	shopPolicy := merchantPolicyProposal{
		Scope: "MERCHANT", Name: "Large checkout review", ProductTypes: []string{"digital", "payment_gateway"},
		Stages: []string{"CHECKOUT"}, Action: "REVIEW", Conditions: merchantPolicyConditions{
			Logic: "AND", Rules: []merchantPolicyCondition{{Field: policyAmount, Operator: "gte", Value: json.RawMessage("100")}},
		},
	}
	request(http.MethodPost, policyEndpoint, tokenA, map[string]any{"proposal": shopPolicy}, http.StatusCreated, nil)
	for _, scenario := range []struct{ id, product, source string }{
		{"9fae5a36-59ae-4fb5-a773-f62b3bce9904", "digital", "checkout"},
		{"9fae5a36-59ae-4fb5-a773-f62b3bce9905", "payment_gateway", "simulation"},
	} {
		var result struct {
			Transaction mockTransaction `json:"transaction"`
		}
		amount := int64(100)
		request(http.MethodPost, merchantDemoPath+"/"+first.ID+"/transactions", admin, mockTransactionInput{
			ID: scenario.id, ScenarioID: firstScenario.ID, Source: scenario.source, Product: scenario.product, AmountMinor: &amount,
		}, http.StatusCreated, &result)
		if result.Transaction.ReviewState != "NEEDS_DECISION" || result.Transaction.PolicyDecision.Final != (policyFinal{"REVIEW", "MERCHANT_POLICY"}) {
			t.Fatalf("failed %s review: %+v", scenario.product, result.Transaction.PolicyDecision)
		}
	}
}

func TestMerchantPolicySavePendingIPCountryPostgres(t *testing.T) {
	db := openPolicyTestDatabase(t)
	repo := repository{db}
	mux := http.NewServeMux()
	registerMerchantDemo(mux, repo, "pending-country-admin")
	call := func(method, path, token string, input any, expected int) []byte {
		t.Helper()
		payload, err := json.Marshal(input)
		if err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(method, path, bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		mux.ServeHTTP(response, req)
		if response.Code != expected {
			t.Fatalf("%s %s returned %d, want %d: %s", method, path, response.Code, expected, response.Body.String())
		}
		return response.Body.Bytes()
	}
	mkMerchant := func(name string) (mockMerchant, string) {
		t.Helper()
		merchant, token, err := repo.createMockMerchant(t.Context(), mockMerchantInput{
			Name: name, PolicyIntegration: &merchantPolicyIntegration{Products: []string{"flight_ticket"}, AmountMinor: true},
		})
		if err != nil {
			t.Fatal(err)
		}
		return merchant, token
	}
	merchant, token := mkMerchant("pending network evidence merchant")
	other, otherToken := mkMerchant("other network evidence merchant")
	mailbox, err := merchantMailboxRef(t.Context(), db, merchant.ID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	otherMailbox, err := merchantMailboxRef(t.Context(), db, other.ID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	reference, err := json.Marshal(mailbox)
	if err != nil {
		t.Fatal(err)
	}
	base := merchantPolicyProposal{
		Scope: "MERCHANT", Name: "High-value flight", Action: "REVIEW", ProductTypes: []string{"flight_ticket"}, Stages: []string{"BOOKING"},
		Conditions: merchantPolicyConditions{Logic: "AND", Rules: []merchantPolicyCondition{
			{Field: policyAmount, Operator: "gt", Value: json.RawMessage(`300000`)},
		}},
	}
	compound := base
	compound.Action = "BLOCK"
	compound.Conditions.Rules = append(slices.Clone(base.Conditions.Rules),
		merchantPolicyCondition{Field: policyMailbox, Operator: "eq", Value: reference},
		merchantPolicyCondition{Field: policyIPCountry, Operator: "eq", Value: json.RawMessage(`"US"`)},
	)
	invalid := compound
	invalid.Conditions.Rules = append(slices.Clone(compound.Conditions.Rules),
		merchantPolicyCondition{Field: "custom_other_required", Operator: "eq", Value: json.RawMessage(`"value"`)},
	)
	endpoint := merchantPolicyPath + "/rules"
	call(http.MethodPost, endpoint, token, map[string]any{"proposal": compound}, http.StatusUnprocessableEntity)
	call(http.MethodPost, endpoint, token, map[string]any{"proposal": invalid, "saveAsDraft": true}, http.StatusUnprocessableEntity)

	decode := func(raw []byte) merchantPolicyRule {
		t.Helper()
		var saved struct{ Rule merchantPolicyRule }
		if err := json.Unmarshal(raw, &saved); err != nil {
			t.Fatal(err)
		}
		return saved.Rule
	}
	draftInput := map[string]any{"proposal": compound, "saveAsDraft": true}
	firstDraft := decode(call(http.MethodPost, endpoint, token, draftInput, http.StatusCreated))
	if firstDraft.Status != "PENDING_APPROVAL" || firstDraft.ActiveVersion != 0 ||
		!slices.Equal(firstDraft.Preview.MissingSignals, []string{policyIPCountry}) || firstDraft.ExpiresAt != nil {
		t.Fatalf("Expected an inactive draft waiting for country evidence: %+v", firstDraft)
	}
	retry := decode(call(http.MethodPost, endpoint, token, draftInput, http.StatusCreated))
	if retry.ID != firstDraft.ID || retry.Version != firstDraft.Version {
		t.Fatalf("Retry duplicated the draft: %+v", retry)
	}
	rules, err := readMerchantPolicies(t.Context(), db, merchant.ID, true)
	if err != nil || len(rules) != 0 {
		t.Fatalf("New draft became effective: %+v, %v", rules, err)
	}
	draftPath := endpoint + "/" + firstDraft.ID
	call(http.MethodGet, draftPath, otherToken, nil, http.StatusNotFound)
	call(http.MethodPost, draftPath+"/activate", otherToken, map[string]any{"expectedVersion": 1}, http.StatusNotFound)
	call(http.MethodPost, draftPath+"/activate", token, map[string]any{"expectedVersion": 1}, http.StatusUnprocessableEntity)
	call(http.MethodPost, draftPath+"/simulate", token, nil, http.StatusUnprocessableEntity)

	base.Name = "Existing active flight review"
	active := decode(call(http.MethodPost, endpoint, token, map[string]any{"proposal": base}, http.StatusCreated))
	if active.Status != "ACTIVE" || active.ActiveVersion != 1 {
		t.Fatalf("Existing rule did not activate: %+v", active)
	}
	activePath := endpoint + "/" + active.ID
	compound.Name = base.Name
	edit := decode(call(http.MethodPut, activePath, token, map[string]any{"proposal": compound, "expectedVersion": 1, "saveAsDraft": true}, http.StatusOK))
	if edit.Status != "PENDING_APPROVAL" || edit.ActiveVersion != 1 || edit.Version != 2 {
		t.Fatalf("Saving a draft edit must retain the previously effective version: %+v", edit)
	}
	rules, err = readMerchantPolicies(t.Context(), db, merchant.ID, true)
	if err != nil || len(rules) != 1 || rules[0].ID != active.ID || rules[0].Version != 1 {
		t.Fatalf("Draft edit displaced effective rule: %+v, %v", rules, err)
	}
	newerDraft := decode(call(http.MethodPut, activePath, token, map[string]any{"proposal": base, "expectedVersion": 2, "saveAsDraft": true}, http.StatusOK))
	if newerDraft.Status != "PENDING_APPROVAL" || newerDraft.ActiveVersion != 1 || newerDraft.Version != 3 {
		t.Fatalf("Expected another inactive draft version: %+v", newerDraft)
	}
	rollback := map[string]any{"expectedVersion": 3, "version": 2}
	call(http.MethodPost, activePath+"/rollback", token, rollback, http.StatusUnprocessableEntity)

	register := func(kind string) {
		t.Helper()
		_, err := repo.updateMockMerchant(t.Context(), merchant.ID, mockMerchantInput{
			Name: merchant.Name, PolicyIntegration: &merchantPolicyIntegration{Products: []string{"flight_ticket"}, AmountMinor: true,
				CustomFields: []customSignalDefinition{{Key: policyIPCountry, Type: kind, Label: "IP country"}},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	register("text")
	call(http.MethodPost, draftPath+"/activate", token, map[string]any{"expectedVersion": 1}, http.StatusUnprocessableEntity)
	call(http.MethodPost, activePath+"/rollback", token, rollback, http.StatusUnprocessableEntity)
	register("country")
	call(http.MethodPost, draftPath+"/simulate", token, nil, http.StatusOK)
	restored := decode(call(http.MethodPost, activePath+"/rollback", token, rollback, http.StatusOK))
	if restored.Status != "PENDING_APPROVAL" || restored.ActiveVersion != 1 || len(restored.Preview.MissingSignals) != 0 {
		t.Fatalf("Restored draft must still require explicit activation: %+v", restored)
	}
	published := decode(call(http.MethodPost, draftPath+"/activate", token, map[string]any{"expectedVersion": 1}, http.StatusOK))
	if published.Status != "ACTIVE" || published.ActiveVersion != 2 || len(published.Preview.MissingSignals) != 0 {
		t.Fatalf("Draft did not activate after a valid country signal was registered: %+v", published)
	}
	amount := int64(300001)
	evidence := merchantPolicyEvidence{product: "flight_ticket", stage: "BOOKING", amount: &amount, mailboxRef: mailbox}
	if matched, missing := published.Proposal.evaluate(evidence); matched || !missing {
		t.Fatal("Missing transaction IP country must remain unresolved after activation.")
	}
	evidence.custom = map[string]json.RawMessage{policyIPCountry: json.RawMessage(`"US"`)}
	if matched, missing := published.Proposal.evaluate(evidence); !matched || missing {
		t.Fatal("The bound mailbox, flight, amount and US must match the published rule.")
	}
	evidence.mailboxRef = otherMailbox
	if matched, _ := published.Proposal.evaluate(evidence); matched {
		t.Fatal("Another merchant's mailbox reference must not match the rule.")
	}
}

func TestMerchantPolicyLocalTiming(t *testing.T) {
	db := openPolicyTestDatabase(t)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	repo := repository{db}
	merchant, _, err := repo.createMockMerchant(ctx, mockMerchantInput{Name: "Policy timing test",
		PolicyIntegration: &merchantPolicyIntegration{Products: []string{"flight_ticket"}, EmailDomain: true}})
	if err != nil {
		t.Fatal(err)
	}
	policy := exampleMerchantPolicy()
	if _, err := repo.mutateMerchantPolicy(ctx, merchant.ID, merchantPolicyMutation{Kind: "save", Proposal: policy}); err != nil {
		t.Fatal(err)
	}
	rules, err := readMerchantPolicies(ctx, db, merchant.ID, true)
	if err != nil {
		t.Fatal(err)
	}
	evidence := merchantPolicyEvidence{product: "flight_ticket", stage: "BOOKING", domain: "skycannner.com"}
	const samples = 200
	reads, evaluations := make([]time.Duration, samples), make([]time.Duration, samples)
	for index := range reads {
		start := time.Now()
		if _, err := readMerchantPolicies(ctx, db, merchant.ID, false); err != nil {
			t.Fatal(err)
		}
		reads[index] = time.Since(start)
		start = time.Now()
		if got := evaluateMerchantPolicies(rules, evidence); got.Action != "BLOCK" {
			t.Fatalf("evaluation=%v", got)
		}
		evaluations[index] = time.Since(start)
	}
	reportPolicyTiming(t, "database rule read", reads)
	reportPolicyTiming(t, "in-memory evaluation", evaluations)
}

func reportPolicyTiming(t *testing.T, name string, samples []time.Duration) {
	slices.Sort(samples)
	t.Logf("%s p95: %s (%d local samples, one active rule)", name, samples[len(samples)*95/100-1], len(samples))
}
