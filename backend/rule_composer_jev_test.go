package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPlatformOpenAIDraftPassesStructureToJev(t *testing.T) {
	var jevCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		jevCalls.Add(1)
		var request struct {
			State     composerJevState     `json:"state"`
			Questions merchantJevQuestions `json:"questions"`
		}
		if r.Header.Get("Authorization") != "Bearer jev-test-key" || json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("invalid Jev request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		definition := request.State.Definition
		if definition == nil || request.State.Scope != composerPlatform || request.State.Status != "READY" ||
			definition.Action != "REVIEW" || !reflect.DeepEqual(definition.Stages, []string{"PAYMENT"}) ||
			definition.Conditions.Logic != "AND" || !reflect.DeepEqual(definition.Conditions.Rules, []ruleDefinitionCondition{
			{Field: "device_orders", Operator: "gte", Value: float64(3)},
		}) || request.Questions[composerJevQuestion].Type != "choice" {
			t.Errorf("unexpected Jev review state: %+v", request.State)
		}
		fmt.Fprint(w, `{"id":"jev-draft-test-id","model":"typesafe/jev-1.14","answers":{"draft_review":{"type":"choice","choice":"ALIGNED"}}}`)
	}))
	defer server.Close()

	s := ruleComposer{
		access: composerAuthorization(repository{}, "review-admin"),
		provider: testComposerProvider(func(_ context.Context, _, _, _ string) ([]byte, error) {
			return []byte(validPlatformComposition), nil
		}),
		jev: merchantJev{live: true, key: "jev-test-key", model: defaultMerchantJevModel, endpoint: server.URL, client: server.Client()},
	}
	for _, flag := range []string{"", `,"reviewWithJev":true`} {
		body := `{"scope":"PLATFORM","instruction":"Review payment when a device has 3 orders.","requireAI":true` + flag + "}"
		req := httptest.NewRequest(http.MethodPost, composerPath+"/interpret", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer review-admin")
		response := httptest.NewRecorder()
		s.interpret(response, req)
		var result struct {
			Source   string                   `json:"source"`
			Status   string                   `json:"status"`
			Jev      *composerJevReview       `json:"jevReview"`
			Proposal platformComposerProposal `json:"proposal"`
		}
		if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &result) != nil ||
			result.Status != "READY" || result.Source != composerSourceOpenAI || result.Proposal.Rule == nil || result.Proposal.Rule.Name == "" {
			t.Fatalf("expected unchanged OpenAI proposal, got %d %s", response.Code, response.Body.String())
		}
		if flag == "" && (result.Jev != nil || jevCalls.Load() != 0) {
			t.Fatal("open-ended legacy interpretation must not call Jev")
		}
		if flag != "" && (result.Jev == nil || result.Jev.Status != "LIVE" || result.Jev.Answer != "ALIGNED" ||
			result.Jev.DecisionID != "jev-draft-test-id" || jevCalls.Load() != 1) {
			t.Fatalf("missing live review: %+v, calls=%d", result.Jev, jevCalls.Load())
		}
	}
}

func TestMerchantJevReviewPreservesClarificationDefinitionAndPrivacy(t *testing.T) {
	mailboxRef := strings.Repeat("a1", 32)
	definition := &ruleDefinition{
		SchemaVersion: 1, Scope: composerMerchant, Name: "name@example.test", Description: "private description",
		Action: "BLOCK", ProductTypes: []string{"flight_ticket"}, Stages: []string{"BOOKING"},
		Conditions: ruleDefinitionGroup{Logic: "AND", Rules: []ruleDefinitionCondition{
			{Field: policyMailbox, Operator: "eq", Value: mailboxRef},
			{Field: policyAmount, Operator: "gt", Value: json.RawMessage("300000")},
			{Field: policyIPCountry, Operator: "eq", Value: json.RawMessage(`"US"`)},
			{Field: policyDescription, Operator: "contains", Value: json.RawMessage(`"198.51.100.7"`)},
			{Field: "custom_text", Operator: "eq", Value: mailboxRef},
		}},
		Duration: &ruleDuration{Value: 2, Unit: "minute"},
	}
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		var request struct {
			State json.RawMessage `json:"state"`
		}
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			t.Error("could not parse Jev request")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		for _, secret := range []string{mailboxRef, "name@example.test", "private description", "198.51.100.7", "private clarification"} {
			if strings.Contains(string(request.State), secret) {
				t.Error("private value was present in Jev state")
			}
		}
		var state composerJevState
		if json.Unmarshal(request.State, &state) != nil || state.Status != "CLARIFICATION" || state.Scope != composerMerchant ||
			state.ClarificationCode != "IP_COUNTRY_UNAVAILABLE" || state.Definition == nil {
			t.Errorf("incorrect clarification state: %s", request.State)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		safe := state.Definition
		expected := []ruleDefinitionCondition{
			{Field: policyMailbox, Operator: "eq"},
			{Field: policyAmount, Operator: "gt", Value: float64(300000)},
			{Field: policyIPCountry, Operator: "eq", Value: "US"},
			{Field: policyDescription, Operator: "contains"},
			{Field: "custom_text", Operator: "eq"},
		}
		if safe.Action != "BLOCK" || !reflect.DeepEqual(safe.ProductTypes, definition.ProductTypes) ||
			!reflect.DeepEqual(safe.Stages, definition.Stages) || !reflect.DeepEqual(safe.Duration, definition.Duration) ||
			safe.Conditions.Logic != "AND" || !reflect.DeepEqual(safe.Conditions.Rules, expected) {
			t.Errorf("incorrect draft structure: %+v", safe)
		}
		fmt.Fprint(w, `{"id":"jev-draft-incomplete","model":"typesafe/jev-1.14","answers":{"draft_review":{"type":"choice","choice":"INSUFFICIENT"}}}`)
	}))
	defer server.Close()

	s := ruleComposer{jev: merchantJev{live: true, key: "test-key", model: defaultMerchantJevModel, endpoint: server.URL, client: server.Client()}}
	output := composerInterpretation{Status: "CLARIFICATION", Source: composerSourceOpenAI, ClarificationCode: "IP_COUNTRY_UNAVAILABLE",
		Message: "private clarification", Definition: definition}
	response := httptest.NewRecorder()
	s.respondAIInterpretation(response, httptest.NewRequest(http.MethodPost, "/", nil), mockMerchantActor{}, output, composerMerchant, true)
	var result composerInterpretation
	if json.Unmarshal(response.Body.Bytes(), &result) != nil || result.JevReview == nil || result.JevReview.Status != "LIVE" ||
		result.JevReview.Answer != "INSUFFICIENT" || result.Proposal != nil || result.Definition == nil || result.Message != output.Message || calls.Load() != 1 {
		t.Fatalf("unexpected clarification response: %d %s", response.Code, response.Body.String())
	}
}

func TestJevReviewReportsUnavailableAndMockedExplicitly(t *testing.T) {
	s := ruleComposer{jev: merchantJev{live: true, model: defaultMerchantJevModel}}
	if status := s.reviewAIInterpretation(t.Context(), composerMerchant, composerInterpretation{}).Status; status != "UNAVAILABLE" {
		t.Fatalf("expected unavailable for missing key, got %s", status)
	}
	s.jev.live = false
	if review := s.reviewAIInterpretation(t.Context(), composerMerchant, composerInterpretation{}); review.Status != "MOCKED" ||
		review.Model != "mock-jev-v1" || review.DecisionID != "" || review.Answer != "" {
		t.Fatalf("unexpected mocked review: %+v", review)
	}
}
