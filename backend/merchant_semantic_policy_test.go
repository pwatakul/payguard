package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"testing"
)

func semanticTestRule(id, logic, field string, localConditions ...merchantPolicyCondition) merchantPolicyRule {
	return merchantPolicyRule{
		ID: id, Version: 2,
		Proposal: merchantPolicyProposal{
			Scope: "MERCHANT", Name: "Identify admission tickets", Action: "BLOCK",
			ProductTypes: []string{"physical"}, Stages: []string{"CHECKOUT"},
			Conditions: merchantPolicyConditions{Logic: logic, Rules: append([]merchantPolicyCondition{
				{Field: field, Operator: semanticOperator, Value: json.RawMessage(`"Theme park admission"`)},
			}, localConditions...)},
		},
	}
}

func TestMerchantSemanticRulesBatchOnlyRelevantEvidence(t *testing.T) {
	const title = "custom_title"
	evidence := merchantPolicyEvidence{
		product: "physical", stage: "CHECKOUT", domain: "tenant.example.test", mailboxRef: "secret-reference",
		custom: map[string]json.RawMessage{
			title:             json.RawMessage(`"One-day pass to enter a theme park"`),
			policyDescription: json.RawMessage(`"Shuttle bus to the park"`),
			"custom_unused":   json.RawMessage(`"unrelated description"`),
		},
	}
	block := semanticTestRule("admission", "AND", title, merchantPolicyCondition{Field: policyProduct, Operator: "eq", Value: json.RawMessage(`"physical"`)})
	nearby := semanticTestRule("shuttle", "AND", policyDescription)
	unmatched := semanticTestRule("excluded", "AND", "custom_unused", merchantPolicyCondition{Field: policyDomain, Operator: "eq", Value: json.RawMessage(`"another.example.test"`)})
	calls := 0
	jev := testMerchantJev(t, func(request *http.Request) (*http.Response, error) {
		calls++
		defer request.Body.Close()
		var body struct {
			State     map[string]merchantSemanticQuestion `json:"state"`
			Questions merchantJevQuestions                `json:"questions"`
		}
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		expectedState := map[string]merchantSemanticQuestion{
			"condition_1": {ObservedText: "One-day pass to enter a theme park", Criterion: "Theme park admission"},
			"condition_2": {ObservedText: "Shuttle bus to the park", Criterion: "Theme park admission"},
		}
		if len(body.Questions) != 2 || len(body.State) != 2 {
			t.Fatalf("unexpected question count: %+v", body)
		}
		for key, expected := range expectedState {
			got := body.State[key]
			if got.Rule == nil || got.Rule.Scope != composerMerchant || got.Rule.Conditions.Logic != "AND" {
				t.Fatalf("expected sanitized rule structure for %s: %+v", key, got)
			}
			got.Rule = nil
			if !reflect.DeepEqual(got, expected) {
				t.Fatalf("unexpected evidence for %s: %+v", key, got)
			}
		}
		for key, q := range body.Questions {
			if _, exists := body.State[key]; !exists || q.Type != "choice" || len(q.Criteria) != 3 {
				t.Fatalf("untyped semantic question: %+v", q)
			}
		}
		return testJevResponse(200, `{"id":"test-decision","model":"typesafe/jev-1.14","answers":{"condition_1":{"type":"choice","choice":"MATCH"},"condition_2":{"type":"choice","choice":"NO_MATCH"}}}`), nil
	})
	part := evaluateMerchantPoliciesWithJev(t.Context(), []merchantPolicyRule{block, nearby, unmatched}, evidence, jev, false, false)
	if calls != 1 || part.Action != "BLOCK" || !slices.Equal(part.MatchedRuleIDs, []string{block.ID}) || len(part.JevConditions) != 2 {
		t.Fatalf("unexpected policy decision: %+v (%d calls)", part, calls)
	}
	if got := part.JevConditions[0]; got.Answer != "MATCH" || got.Version != 2 || got.Status != "LIVE" || got.DecisionID != "test-decision" {
		t.Fatalf("wrong stored Jev evidence: %+v", got)
	}
}

func TestMerchantSemanticPolicyUnknownAndLocalShortCircuit(t *testing.T) {
	evidence := merchantPolicyEvidence{
		product: "physical", stage: "CHECKOUT", domain: "tenant.example.test",
		custom: map[string]json.RawMessage{policyDescription: json.RawMessage(`"One-day pass"`)},
	}
	rule := semanticTestRule("semantic", "AND", policyDescription)
	unknownResponse := `{"id":"test-decision","model":"typesafe/jev-1.14","answers":{"condition_1":{"type":"choice","choice":"UNKNOWN"}}}`
	for _, tc := range []struct {
		name            string
		response        string
		httpStatus      int
		input           json.RawMessage
		simulateTimeout bool
		expectedStatus  string
		expectedCalls   int
	}{
		{"missing", "", 0, nil, false, "NOT_RUN", 0},
		{"private email", "", 0, json.RawMessage(`"Buyer person@example.test"`), false, "NOT_RUN", 0},
		{"simulated timeout", "", 0, evidence.custom[policyDescription], true, "TIMEOUT", 0},
		{"provider unknown", unknownResponse, 200, evidence.custom[policyDescription], false, "LIVE", 1},
		{"provider failure", "", 502, evidence.custom[policyDescription], false, "UNAVAILABLE", 1},
		{"unrequested choice", `{"model":"typesafe/jev-1.14","answers":{"condition_1":{"type":"choice","choice":"REVIEW"}}}`, 200, evidence.custom[policyDescription], false, "UNAVAILABLE", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			jev := testMerchantJev(t, func(*http.Request) (*http.Response, error) {
				calls++
				return testJevResponse(tc.httpStatus, tc.response), nil
			})
			input := evidence
			input.custom = map[string]json.RawMessage{policyDescription: tc.input}
			decision := evaluateMerchantPoliciesWithJev(t.Context(), []merchantPolicyRule{rule}, input, jev, tc.simulateTimeout, false)
			if calls != tc.expectedCalls || decision.Action != "UNRESOLVED" || len(decision.JevConditions) != 1 ||
				decision.JevConditions[0].Answer != "UNKNOWN" || decision.JevConditions[0].Status != tc.expectedStatus {
				t.Fatalf("expected unresolved with %s, got %+v (%d calls)", tc.expectedStatus, decision, calls)
			}
			amount := int64(10)
			transaction := resolveMerchantRuleDecision(t.Context(), mockTransactionInput{AmountMinor: &amount},
				policyDecisionPart{Action: "CHALLENGE"}, "platform-v1", decision, jevRulesSnapshot{}, merchantJev{}, nil)
			if transaction.Outcome != "UNRESOLVED" || transaction.ReviewState != "NEEDS_DECISION" {
				t.Fatalf("unresolved rule fails to require manual review: %+v", transaction)
			}
		})
	}

	jev := testMerchantJev(t, func(*http.Request) (*http.Response, error) {
		t.Fatal("short-circuited rule reached Jev")
		return nil, nil
	})
	or := semanticTestRule("or", "OR", policyDescription, merchantPolicyCondition{Field: policyDomain, Operator: "eq", Value: json.RawMessage(`"tenant.example.test"`)})
	and := semanticTestRule("and", "AND", policyDescription, merchantPolicyCondition{Field: policyDomain, Operator: "eq", Value: json.RawMessage(`"other.example.test"`)})
	if part := evaluateMerchantPoliciesWithJev(t.Context(), []merchantPolicyRule{or, and, rule}, evidence, jev, false, false); part.Action != "BLOCK" ||
		!slices.Equal(part.MatchedRuleIDs, []string{or.ID}) || len(part.JevConditions) != 0 {
		t.Fatalf("decisive local conditions should skip Jev: %+v", part)
	}
	if part := evaluateMerchantPoliciesWithJev(t.Context(), []merchantPolicyRule{rule}, evidence, jev, false, true); len(part.JevConditions) != 0 {
		t.Fatalf("platform block should skip semantic calls: %+v", part)
	}
}

func TestMerchantSemanticPolicyMockIsConservative(t *testing.T) {
	rule := semanticTestRule("semantic", "AND", policyDescription)
	evidence := merchantPolicyEvidence{
		product: "physical", stage: "CHECKOUT", custom: map[string]json.RawMessage{policyDescription: json.RawMessage(`"Park pass"`)},
	}
	result := evaluateMerchantPoliciesWithJev(t.Context(), []merchantPolicyRule{rule}, evidence, merchantJev{}, false, false)
	if result.Action != "UNRESOLVED" || result.JevConditions[0].Status != "MOCKED" || result.JevConditions[0].Answer != "UNKNOWN" {
		t.Fatalf("mock guessed a semantic meaning: %+v", result)
	}
	evidence.custom[policyDescription] = json.RawMessage(`"  theme PARK admission  "`)
	result = evaluateMerchantPoliciesWithJev(t.Context(), []merchantPolicyRule{rule}, evidence, merchantJev{}, false, false)
	if result.Action != "BLOCK" || result.JevConditions[0].Status != "MOCKED" || result.JevConditions[0].Answer != "MATCH" {
		t.Fatalf("mock should recognize normalized exact equality: %+v", result)
	}
}

func TestMerchantSemanticTransactionReplayPostgres(t *testing.T) {
	repo := repository{openPolicyTestDatabase(t)}
	ctx := t.Context()
	merchant, _, err := repo.createMockMerchant(ctx, mockMerchantInput{
		Name:              "Merchant Jev fallback integration",
		PolicyIntegration: &merchantPolicyIntegration{Products: []string{"physical"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rule := semanticTestRule("new", "AND", policyDescription)
	saved, err := repo.mutateMerchantPolicy(ctx, merchant.ID, merchantPolicyMutation{Kind: "save", Proposal: rule.Proposal})
	if err != nil || saved.Status != "ACTIVE" {
		t.Fatalf("save semantic rule: %+v %v", saved, err)
	}
	amount := int64(100)
	order := mockTransactionInput{ID: "0fcadbc9-43ba-4c57-b1e9-5484a7a4a67a", Source: "checkout", Product: "physical", AmountMinor: &amount,
		CustomSignals: map[string]json.RawMessage{policyDescription: json.RawMessage(`"Theme park admission"`)}}
	checkout, created, err := repo.saveMockTransaction(ctx, merchant.ID, order)
	if err != nil || !created || checkout.Outcome != "HIGH_RISK" || len(checkout.PolicyDecision.Merchant.JevConditions) != 1 {
		t.Fatalf("checkout did not apply saved semantic rule: %+v %v", checkout, err)
	}
	want := merchantJevConditionResult{RuleID: saved.ID, Version: saved.Version, ConditionIndex: 0, Field: policyDescription, Answer: "MATCH", Status: "MOCKED", Model: "mock-jev-v1"}
	if got := checkout.PolicyDecision.Merchant.JevConditions[0]; got != want {
		t.Fatalf("incorrect pinned Jev evaluation: %+v", got)
	}
	simulationInput := order
	simulationInput.ID, simulationInput.Source = "a9d95104-0e53-498a-878a-942d139d262b", "simulation"
	simulation, created, err := repo.saveMockTransaction(ctx, merchant.ID, simulationInput)
	if err != nil || !created || simulation.Outcome != checkout.Outcome || !reflect.DeepEqual(simulation.PolicyDecision, checkout.PolicyDecision) {
		t.Fatalf("simulation and checkout differ: %+v %+v %v", checkout.PolicyDecision, simulation.PolicyDecision, err)
	}
	legacyScenario, err := repo.saveMockScenario(ctx, merchant.ID, "", "Legacy amount review", 25)
	if err != nil {
		t.Fatal(err)
	}
	legacy := order
	legacy.ID, legacy.ScenarioID, legacy.CustomSignals = "b0088641-e1e6-41e6-b778-f3fa45b57837", legacyScenario.ID, nil
	unresolved, created, err := repo.saveMockTransaction(ctx, merchant.ID, legacy)
	if err != nil || !created || unresolved.Outcome != "UNRESOLVED" || unresolved.ReviewState != "NEEDS_DECISION" ||
		len(unresolved.PolicyDecision.Merchant.JevConditions) != 1 || unresolved.PolicyDecision.Merchant.JevConditions[0].Status != "NOT_RUN" {
		t.Fatalf("legacy input silently bypassed a semantic rule: %+v %v", unresolved, err)
	}
	if _, err := repo.mutateMerchantPolicy(ctx, merchant.ID, merchantPolicyMutation{Kind: "disable", ID: saved.ID, ExpectedVersion: saved.Version}); err != nil {
		t.Fatal(err)
	}
	replayed, created, err := repo.saveMockTransaction(ctx, merchant.ID, order)
	if err != nil || created {
		t.Fatalf("replaying semantic transaction created a new decision or failed: %v", err)
	}
	first, firstErr := json.Marshal(checkout)
	replay, replayErr := json.Marshal(replayed)
	if firstErr != nil || replayErr != nil || !bytes.Equal(first, replay) {
		t.Fatalf("recorded semantic decision changed on replay: %s / %s (%v / %v)", first, replay, firstErr, replayErr)
	}
}
