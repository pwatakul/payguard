package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequireOpenAIForPlatformDraft(t *testing.T) {
	composer := ruleComposer{access: composerAuthorization(repository{}, "test-platform-key")}
	try := func(want int) {
		t.Helper()
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, composerPath+"/interpret",
			strings.NewReader(`{"scope":"PLATFORM","instruction":"Review large orders","requireAI":true}`))
		request.Header.Set("Authorization", "Bearer test-platform-key")
		composer.interpret(response, request)
		if response.Code != want {
			t.Fatalf("Status %d, expected %d: %s", response.Code, want, response.Body.String())
		}
	}
	try(http.StatusServiceUnavailable)
	composer.provider = testComposerProvider(func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("provider unavailable")
	})
	try(http.StatusBadGateway)
	composer.provider = testComposerProvider(func(context.Context, string, string, string) ([]byte, error) {
		return []byte(validPlatformComposition), nil
	})
	try(http.StatusOK)
}

func TestComposePrivateMailboxWithOpenAIPostgres(t *testing.T) {
	db := openPolicyTestDatabase(t)
	repo := repository{db}
	newMerchant := func(name string) (mockMerchant, string) {
		t.Helper()
		merchant, token, err := repo.createMockMerchant(t.Context(), mockMerchantInput{
			Name: name, PolicyIntegration: &merchantPolicyIntegration{
				Products: []string{"hotel_room", "flight_ticket"}, AmountMinor: true,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return merchant, token
	}
	merchant, token := newMerchant("Private mailbox composer")
	other, _ := newMerchant("Other private mailbox composer")
	digest := strings.Repeat("b", 64)
	ref, err := merchantMailboxRef(t.Context(), db, merchant.ID, digest)
	if err != nil {
		t.Fatal(err)
	}
	otherRef, err := merchantMailboxRef(t.Context(), db, other.ID, digest)
	if err != nil || ref == otherRef || ref == digest {
		t.Fatalf("Expected a distinct merchant-scoped reference, err=%v", err)
	}
	composer := ruleComposer{access: composerAuthorization(repo, "test-mailbox-platform-key"), model: "test-model"}
	calls := 0
	const address = "buyer@example.test"
	const ready = `{"status":"READY","message":"","proposal":{"scope":"MERCHANT","name":"High-value flight", "product_types":["flight_ticket"],"stages":["BOOKING"],"action":"BLOCK","conditions":{"logic":"AND","rules":[{"field":"order.amount_minor","operator":"gt","value":500000}]},"durationHours":null,"durationMinutes":null}}`
	draft := ready
	provider := testComposerProvider(func(_ context.Context, scope, prompt, instruction string) ([]byte, error) {
		calls++
		if scope != composerMerchant || strings.ContainsAny(instruction, "@") ||
			strings.Contains(prompt+instruction, digest) || strings.Contains(prompt+instruction, ref) ||
			strings.Contains(prompt+instruction, policyMailbox) || strings.Contains(prompt+instruction, address) {
			t.Fatal("OpenAI must receive only the redacted instruction and available non-mailbox fields.")
		}
		return []byte(draft), nil
	})
	send := func(instruction, mailboxDigest string, status int) []byte {
		t.Helper()
		payload, err := json.Marshal(map[string]any{
			"scope": composerMerchant, "instruction": instruction, "mailboxDigest": mailboxDigest, "requireAI": true,
		})
		if err != nil {
			t.Fatal(err)
		}
		request := httptest.NewRequest(http.MethodPost, composerPath+"/interpret", bytes.NewReader(payload))
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		composer.interpret(response, request)
		if response.Code != status {
			t.Fatalf("Status %d, expected %d: %s", response.Code, status, response.Body.String())
		}
		if strings.Contains(response.Body.String(), digest) || strings.Contains(response.Body.String(), address) {
			t.Fatal("Response contains private source data.")
		}
		return response.Body.Bytes()
	}
	const english = "Block email [EXACT_EMAIL] and product = flight and price > 5000 THB"
	const thai = "บล็อกอีเมล [EXACT_EMAIL] และสินค้าเป็นตั๋วเครื่องบินเมื่อราคามากกว่า 5,000 บาท"
	send(english, digest, http.StatusServiceUnavailable)
	composer.provider = provider
	send("Block email "+address+" and product = flight and price > 5000 THB", digest, http.StatusUnprocessableEntity)
	send("Block email [EXACT_EMAIL] with "+strings.ToUpper(digest), digest, http.StatusUnprocessableEntity)
	if calls != 0 {
		t.Fatalf("Invalid instructions reached OpenAI (%d calls).", calls)
	}
	if response := send("Block email [EXACT_EMAIL] or product = flight", digest, http.StatusOK); !bytes.Contains(response, []byte("conditions with AND")) ||
		!bytes.Contains(response, []byte(`"source":"openai"`)) {
		t.Fatal("OR must require correction after an OpenAI call.")
	}
	if response := send(strings.TrimSuffix(english, " THB"), digest, http.StatusOK); !bytes.Contains(response, []byte("What unit")) {
		t.Fatalf("Expected unit clarification even when OpenAI guesses a unit: %s", response)
	}
	for _, instruction := range []string{english, thai} {
		var response struct {
			Status, Source string
			Proposal       merchantPolicyProposal
			Definition     ruleDefinition
		}
		if err := json.Unmarshal(send(instruction, digest, http.StatusOK), &response); err != nil {
			t.Fatal(err)
		}
		if response.Status != "READY" || response.Source != composerSourceOpenAI ||
			response.Proposal.Conditions.Logic != "AND" {
			t.Fatalf("Expected a validated OpenAI proposal: %+v", response)
		}
		for _, condition := range response.Definition.Conditions.Rules {
			if condition.Field == policyMailbox && condition.Value != nil {
				t.Fatal("Public definition contains the private mailbox reference.")
			}
		}
		amount := int64(500001)
		matches := func(product, mailbox string, amount *int64) bool {
			matched, _ := response.Proposal.evaluate(merchantPolicyEvidence{
				product: product, stage: "BOOKING", mailboxRef: mailbox, amount: amount,
			})
			return matched
		}
		equal := amount - 1
		if !matches("flight_ticket", ref, &amount) || matches("flight_ticket", ref, &equal) ||
			matches("hotel_room", ref, &amount) || matches("flight_ticket", otherRef, &amount) {
			t.Fatal("Expected a matching mailbox AND flight ticket AND an amount strictly above 5000 THB.")
		}
	}
	draft = strings.ReplaceAll(ready, `"operator":"gt"`, `"operator":"gte"`)
	send(english, digest, http.StatusBadGateway)
	draft = strings.ReplaceAll(ready, `"value":500000`, `"value":5000`)
	send(english, digest, http.StatusBadGateway)
	draft = strings.ReplaceAll(ready, `"flight_ticket"`, `"hotel_room"`)
	send(english, digest, http.StatusBadGateway)
	draft = `{"status":"READY","message":"","proposal":{"scope":"MERCHANT","name":"Temporary block","product_types":["flight_ticket","hotel_room"],"stages":["BOOKING"],"action":"BLOCK","conditions":{"logic":"AND","rules":[]},"durationHours":null,"durationMinutes":2}}`
	if response := send("Block email [EXACT_EMAIL] for 2 minutes", digest, http.StatusOK); !bytes.Contains(response, []byte(`"durationMinutes":2`)) {
		t.Fatalf("Expected an OpenAI-authored timed mailbox rule: %s", response)
	}
	send("Block email [EXACT_EMAIL] and when an order includes a gift card for 2 minutes", digest, http.StatusBadGateway)
	composer.provider = testComposerProvider(func(context.Context, string, string, string) ([]byte, error) {
		return nil, errors.New("provider unavailable")
	})
	send(english, digest, http.StatusBadGateway)
	send("Review all flights when amount is greater than 5000 THB", "", http.StatusBadGateway)
	if calls != 9 {
		t.Fatalf("Expected nine OpenAI interactions, got %d.", calls)
	}
}

func TestPrivateMailboxIPCountryDraftPostgres(t *testing.T) {
	db := openPolicyTestDatabase(t)
	repo := repository{db}
	createMerchant := func(name string, fields []customSignalDefinition) (mockMerchant, string) {
		t.Helper()
		merchant, token, err := repo.createMockMerchant(t.Context(), mockMerchantInput{
			Name: name, PolicyIntegration: &merchantPolicyIntegration{
				Products: []string{"flight_ticket", "hotel_room"}, AmountMinor: true, CustomFields: fields,
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		return merchant, token
	}
	_, unregistered := createMerchant("Mailbox country unavailable", nil)
	merchant, registered := createMerchant("Mailbox country configured", []customSignalDefinition{
		{Key: policyIPCountry, Label: "Customer IP country", Type: "country"},
	})
	const markerReply = `{"status":"CLARIFICATION","message":"Please clarify [EXACT_EMAIL] and the price unit.","proposal":null}`
	const ready = `{"status":"READY","message":"","proposal":{"scope":"MERCHANT","name":"US high-value flight","product_types":["flight_ticket"],"stages":["BOOKING"],"action":"BLOCK","conditions":{"logic":"AND","rules":[{"field":"custom_ip_country","operator":"eq","value":"US"},{"field":"order.amount_minor","operator":"gt","value":300000}]},"durationHours":null,"durationMinutes":null}}`
	const instruction = "Block email [EXACT_EMAIL] and product = flight and price > 3000 and IP from USA"
	const withUnits = "Block email [EXACT_EMAIL] and product = flight and price > 3000 THB and IP from USA"
	digest := strings.Repeat("7", 64)
	composer := ruleComposer{access: composerAuthorization(repo, "test-mailbox-country-key"), model: "test-model"}
	var prompt, providerInstruction string
	reply, calls := markerReply, 0
	composer.provider = testComposerProvider(func(_ context.Context, _, policy, instruction string) ([]byte, error) {
		calls++
		prompt, providerInstruction = policy, instruction
		return []byte(reply), nil
	})
	send := func(token, instruction string, status int, jevReview ...bool) []byte {
		t.Helper()
		payload, _ := json.Marshal(map[string]any{
			"scope": composerMerchant, "instruction": instruction, "mailboxDigest": digest, "requireAI": true,
			"reviewWithJev": len(jevReview) > 0 && jevReview[0],
		})
		req := httptest.NewRequest(http.MethodPost, composerPath+"/interpret", bytes.NewReader(payload))
		req.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		before := calls
		composer.interpret(response, req)
		if response.Code != status {
			t.Fatalf("Expected HTTP %d, got %d: %s", status, response.Code, response.Body.String())
		}
		if calls != before+1 || providerInstruction != instruction || strings.Contains(prompt+providerInstruction, digest) {
			t.Fatal("Draft must call OpenAI with the redacted instruction only.")
		}
		return response.Body.Bytes()
	}
	result := send(unregistered, instruction, http.StatusOK)
	if !bytes.Contains(result, []byte(`"clarificationCode":"IP_COUNTRY_UNAVAILABLE"`)) ||
		!bytes.Contains(result, []byte(`"source":"openai"`)) || bytes.Contains(result, []byte(`"proposal"`)) {
		t.Fatalf("Missing IP country evidence must take precedence over ambiguous price units: %s", result)
	}
	result = send(registered, instruction, http.StatusOK)
	if !bytes.Contains(result, []byte(`"clarificationCode":"AMOUNT_UNIT_REQUIRED"`)) || bytes.Contains(result, []byte(`"proposal"`)) {
		t.Fatalf("An available IP country field allows clarification about ambiguous price units: %s", result)
	}
	result = send(unregistered, instruction, http.StatusOK, true)
	if !bytes.Contains(result, []byte(`"clarificationCode":"AMOUNT_UNIT_REQUIRED"`)) {
		t.Fatalf("The Jev draft path should ask for the unit before trying to draft the missing signal: %s", result)
	}
	result = send(registered, "Block email [EXACT_EMAIL] when the booking is suspicious", http.StatusOK)
	if !bytes.Contains(result, []byte("the specified email address")) || bytes.Contains(result, []byte("[EXACT_EMAIL]")) {
		t.Fatalf("An OpenAI clarification should present the private reference in ordinary words: %s", result)
	}
	reply = ready
	result = send(unregistered, withUnits, http.StatusOK, true)
	if !bytes.Contains(result, []byte(`"clarificationCode":"IP_COUNTRY_UNAVAILABLE"`)) ||
		!bytes.Contains(result, []byte(`"definition":`)) ||
		!bytes.Contains(result, []byte(`"draftProposal":`)) ||
		!bytes.Contains(result, []byte(`"field":"custom_ip_country","operator":"eq","value":"US"`)) ||
		!bytes.Contains(result, []byte(`"field":"customer.mailbox_ref","operator":"eq","value":null`)) ||
		!bytes.Contains(result, []byte(`"jevReview":`)) || bytes.Contains(result, []byte(`"proposal"`)) {
		t.Fatalf("A missing integration must allow a Jev-reviewed draft without an activatable proposal: %s", result)
	}
	for _, text := range []string{withUnits, "บล็อกอีเมล [EXACT_EMAIL] และตั๋วเครื่องบิน และราคาเกิน 3,000 บาท และไอพีมาจากสหรัฐอเมริกา"} {
		var result struct {
			Status, Source string
			Proposal       merchantPolicyProposal
		}
		if err := json.Unmarshal(send(registered, text, http.StatusOK), &result); err != nil {
			t.Fatal(err)
		}
		if result.Status != "READY" || result.Source != composerSourceOpenAI ||
			!strings.Contains(prompt, `{"field":"custom_ip_country","operator":"eq","value":"US"}`) ||
			!strings.Contains(prompt, `{"field":"order.amount_minor","operator":"gt","value":300000}`) {
			t.Fatalf("Model did not supply the complete grounded rule: %+v", result)
		}
		ref, err := merchantMailboxRef(t.Context(), db, merchant.ID, digest)
		if err != nil {
			t.Fatal(err)
		}
		amount := int64(300001)
		evidence := merchantPolicyEvidence{
			product: "flight_ticket", stage: "BOOKING", mailboxRef: ref, amount: &amount,
			custom: map[string]json.RawMessage{policyIPCountry: json.RawMessage(`"US"`)},
		}
		if matched, missing := result.Proposal.evaluate(evidence); !matched || missing {
			t.Fatal("US IP country with the other matching conditions must block.")
		}
		evidence.custom[policyIPCountry] = json.RawMessage(`"TH"`)
		if matched, missing := result.Proposal.evaluate(evidence); matched || missing {
			t.Fatal("A different IP country must not match.")
		}
		evidence.custom = nil
		if matched, missing := result.Proposal.evaluate(evidence); matched || !missing {
			t.Fatal("Absent country evidence must be missing rather than matching.")
		}
	}
	reply = strings.Replace(ready, `{"field":"custom_ip_country","operator":"eq","value":"US"},`, "", 1)
	send(registered, withUnits, http.StatusBadGateway)
	reply = strings.Replace(ready, `"value":"US"`, `"value":"TH"`, 1)
	send(registered, withUnits, http.StatusBadGateway)
}
