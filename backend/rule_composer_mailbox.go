package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

const compoundMailboxPlaceholder = "[EXACT_EMAIL]"

var (
	compoundMailboxOR         = regexp.MustCompile(`(?i)\b(?:or|either)\b|หรือ`)
	compoundMailboxMarker     = regexp.MustCompile(`(?i)\[EXACT_EMAIL\]`)
	compoundMailboxThaiAnd    = regexp.MustCompile(`(?i)(และ)\s*(ไอพี|ประเทศไอพี|ip\b|ราคา|ยอดเงิน|สินค้า)`)
	compoundMailboxDuration   = regexp.MustCompile(`(?i)(?:\bfor\s+(?:the\s+next\s+)?|เป็นเวลา\s*|นาน\s*|\[EXACT_EMAIL\]\s+)([0-9]+)\s*(minutes?\b|mins?\b|hours?\b|hrs?\b|นาที|ชั่วโมง|ชม\.?)[.]?\s*$`)
	compoundMailboxProduct    = regexp.MustCompile(`(?i)(?:\bproduct(?:\s+type)?\s*(?:=|is\b|equals?\b)\s*|ประเภทสินค้า\s*(?:=|คือ|เป็น)\s*)?(\b(?:flight tickets?|flights?|flight_ticket|hotel rooms?|hotels?|hotel_room|physical goods|physical|digital goods|digital|gateway payments?|payment_gateway)\b|เที่ยวบิน|ตั๋วเครื่องบิน|ห้องพัก|สินค้าทั่วไป|สินค้าดิจิทัล|เกตเวย์ชำระเงิน)`)
	compoundMailboxAmount     = regexp.MustCompile(`(?i)(?:\b(?:price|amount|total(?: price)?)\b|ราคา|ยอด(?:เงิน)?)\s*(?:is\s+)?(at least|at most|greater than|less than|does not equal|equals?|over|under|above|below|อย่างน้อย|ไม่เกิน|มากกว่า|น้อยกว่า|เท่ากับ|ไม่เท่ากับ|เกิน|>=|<=|!=|>|<|=)\s*([0-9]+(?:,[0-9]{3})*(?:\.[0-9]+)?)(?:\s*(minor units?\b|satang\b|THB\b|baht\b|สตางค์|บาท))?`)
	compoundMailboxConnectors = regexp.MustCompile(`(?i)\b(?:block|please|email(?: address)?|exact|mailbox|customer(?:'s)?|transactions?|orders?|and|when|where|with|for|the|only|if|on|has|a|an|is|of|using|all products)\b|บล็อก|อีเมล|ลูกค้า|ธุรกรรม|รายการ|การจอง|สินค้า|และ|เมื่อ|เป็น|คือ|ที่|เฉพาะ|ทุกสินค้า|สำหรับ|มี|\s|[,()]`)
)

type compoundMailboxIntent struct {
	product    string
	conditions []merchantPolicyCondition
	fullyKnown bool
}

func mailboxClarification(code, message string) composerInterpretation {
	return composerInterpretation{Status: "CLARIFICATION", ClarificationCode: code, Message: message}
}

func readCompoundMailboxIntent(predicate string, profile merchantPolicyProfile) (compoundMailboxIntent, composerInterpretation) {
	predicate = compoundMailboxThaiAnd.ReplaceAllString(predicate, " $1 $2")
	remainder := strings.ReplaceAll(predicate, compoundMailboxPlaceholder, "")
	var intent compoundMailboxIntent
	for _, match := range compoundMailboxProduct.FindAllStringSubmatch(remainder, -1) {
		productAlias := strings.ToLower(match[1])
		switch productAlias {
		case "flight", "flights", "เที่ยวบิน":
			productAlias = "flight_ticket"
		case "hotel", "hotels":
			productAlias = "hotel_room"
		}
		products := localComposerProducts(productAlias, merchantPolicyProfile{})
		if len(products) != 1 || (intent.product != "" && intent.product != products[0]) {
			return intent, mailboxClarification("MAILBOX_RULE_CLARIFICATION", "Choose one product for this exact email rule.")
		}
		intent.product = products[0]
		remainder = strings.Replace(remainder, match[0], "", 1)
	}
	_, clauses := splitCustomRuleConditions(predicate)
	for _, clause := range clauses {
		match := customRuleCountry.FindStringSubmatch(clause)
		if match == nil {
			continue
		}
		products := profile.Products
		if intent.product != "" {
			products = []string{intent.product}
		}
		field, available := composerAvailableField(profile, products, policyIPCountry, "eq")
		if !available || field.DataType != "country" {
			return intent, mailboxClarification("IP_COUNTRY_UNAVAILABLE", "Add the custom fraud signal custom_ip_country with type Country code in Merchant Details. Your integration must supply the customer's IP country for each transaction, using ISO codes such as US.")
		}
		country := customRuleCountryCode(unquoteRuleText(match[1]))
		if country == "" {
			return intent, mailboxClarification("IP_COUNTRY_REQUIRED", "Which country should the customer's IP match? Specify a country or an ISO code, such as US.")
		}
		value, _ := json.Marshal(country)
		intent.conditions = append(intent.conditions, merchantPolicyCondition{Field: policyIPCountry, Operator: "eq", Value: value})
		remainder = strings.Replace(remainder, clause, "", 1)
	}
	for _, match := range compoundMailboxAmount.FindAllStringSubmatch(remainder, -1) {
		if match[3] == "" {
			return intent, mailboxClarification("AMOUNT_UNIT_REQUIRED", "What unit is the amount in? Specify THB or minor units in the instruction.")
		}
		amount, unit := strings.ReplaceAll(match[2], ",", ""), strings.ToLower(match[3])
		parts := strings.SplitN(amount, ".", 2)
		if len(parts) == 2 && len(parts[1]) > 2 {
			return intent, mailboxClarification("MAILBOX_RULE_CLARIFICATION", "Enter a valid price with at most two decimal places for THB or a whole number in minor units.")
		}
		minor, valid := localComposerMinorAmount(amount, unit)
		if !valid {
			return intent, mailboxClarification("MAILBOX_RULE_CLARIFICATION", "Enter a valid price in THB or whole minor units.")
		}
		comparison := strings.ToLower(match[1])
		operator := localComposerComparisons[comparison]
		switch comparison {
		case "over", "above", "เกิน":
			operator = "gt"
		case "under", "below":
			operator = "lt"
		case "equal":
			operator = "eq"
		}
		intent.conditions = append(intent.conditions, merchantPolicyCondition{
			Field: policyAmount, Operator: operator, Value: json.RawMessage(strconv.FormatInt(minor, 10)),
		})
		remainder = strings.Replace(remainder, match[0], "", 1)
	}
	intent.fullyKnown = strings.Trim(compoundMailboxConnectors.ReplaceAllString(remainder, ""), ".") == ""
	return intent, composerInterpretation{}
}

// Validate a model's draft against the constraints the application can independently verify.
func (intent compoundMailboxIntent) accepts(proposal merchantPolicyProposal, profile merchantPolicyProfile) bool {
	if proposal.Action != "BLOCK" || proposal.Conditions.Logic != "AND" {
		return false
	}
	if intent.product != "" && !slices.Equal(proposal.ProductTypes, []string{intent.product}) {
		return false
	}
	for _, required := range intent.conditions {
		if !slices.ContainsFunc(proposal.Conditions.Rules, func(condition merchantPolicyCondition) bool {
			return condition.Field == required.Field && condition.Operator == required.Operator && string(condition.Value) == string(required.Value)
		}) {
			return false
		}
	}
	if !intent.fullyKnown {
		return true
	}
	if intent.product == "" && !slices.Equal(sortedPolicyValues(proposal.ProductTypes), sortedPolicyValues(profile.Products)) {
		return false
	}
	return !slices.ContainsFunc(proposal.Conditions.Rules, func(condition merchantPolicyCondition) bool {
		if condition.Field == policyProduct && intent.product != "" {
			return false // The validated product condition agrees with the selected product.
		}
		return !slices.ContainsFunc(intent.conditions, func(required merchantPolicyCondition) bool {
			return condition.Field == required.Field && condition.Operator == required.Operator && string(condition.Value) == string(required.Value)
		})
	})
}

func readCompoundMailboxDuration(sentence, predicate string) (string, int, string) {
	match := compoundMailboxDuration.FindStringSubmatch(predicate)
	if match == nil {
		if hasCustomRuleLifetime(emailRiskMention.ReplaceAllString(sentence, "email risk")) {
			return predicate, 0, mailboxBlockDurationMessage
		}
		return predicate, 0, ""
	}
	amount, err := strconv.Atoi(match[1])
	if err != nil {
		return predicate, 0, mailboxBlockDurationMessage
	}
	unit := strings.ToLower(match[2])
	if strings.HasPrefix(unit, "h") || strings.HasPrefix(unit, "ช") {
		if !validMailboxBlockDuration(amount, 0) {
			return predicate, 0, mailboxBlockDurationMessage
		}
		amount *= 60
	} else if !validMailboxBlockDuration(0, amount) {
		return predicate, 0, mailboxBlockDurationMessage
	}
	return strings.TrimSuffix(predicate, match[0]), amount, ""
}

func (s ruleComposer) interpretCompoundMailbox(w http.ResponseWriter, r *http.Request, actor mockMerchantActor, instruction, digest string, started time.Time, reviewWithJev bool) {
	invalidInstruction := func(message string) {
		s.audit("interpret_compound_mailbox", composerMerchant, actor, "INVALID_INSTRUCTION", "", started)
		problem(w, http.StatusBadRequest, message)
	}
	invalidDraft := func() {
		s.audit("interpret_compound_mailbox", composerMerchant, actor, "INVALID_PROVIDER_OUTPUT", "", started)
		problem(w, http.StatusBadGateway, "OpenAI returned an AI Rule draft that did not preserve the requested conditions. Try again.")
	}
	sentence := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(unquoteRuleText(instruction)), "."))
	action, predicate := customActionAndPredicate(sentence)
	if predicate == "" {
		predicate = sentence
	}
	predicate, duration, durationMessage := readCompoundMailboxDuration(sentence, predicate)
	ctx, cancel := context.WithTimeout(r.Context(), 18*time.Second)
	defer cancel()
	profile, err := s.access.merchantPolicyProfile(ctx, actor.MerchantID)
	if err != nil {
		demoWriteError(w, err)
		return
	}
	intent, clarification := readCompoundMailboxIntent(predicate, profile)
	var missingEvidence composerInterpretation
	if reviewWithJev && clarification.ClarificationCode == "IP_COUNTRY_UNAVAILABLE" {
		// Let the author and reviewer see the required signal without registering it
		// or relaxing validation against the merchant's real profile on save.
		missingEvidence = clarification
		profile = mailboxDraftIPCountryProfile(profile)
		intent, clarification = readCompoundMailboxIntent(predicate, profile)
	}
	if (action != "" && action != "BLOCK") || hasAmbiguousCustomAction(sentence) || compoundMailboxOR.MatchString(predicate) {
		clarification = mailboxClarification("MAILBOX_RULE_CLARIFICATION", "Use Block and join the exact email and its other conditions with AND.")
	}
	if clarification.Status == "" && durationMessage != "" {
		clarification = mailboxClarification("MAILBOX_RULE_CLARIFICATION", durationMessage)
	}
	if clarification.Status == "" && intent.fullyKnown && intent.product == "" && len(intent.conditions) == 0 && duration == 0 {
		clarification = mailboxClarification("MAILBOX_RULE_CLARIFICATION", "Add a product or another condition for this exact email rule, or provide a duration for a standalone email block.")
	}
	if len(profile.Products) == 0 || !slices.Contains(profile.Signals, policyMailbox) {
		invalidInstruction("Add a product that supports exact email matching in Merchant Details before drafting this rule.")
		return
	}
	if s.provider == nil {
		s.audit("interpret_compound_mailbox", composerMerchant, actor, "DISABLED", "", started)
		problem(w, http.StatusServiceUnavailable, "OpenAI rule drafting is unavailable. Ask an administrator to enable it and configure OPENAI_API_KEY.")
		return
	}
	prompt := composerMerchantPrompt(profile) + `
This request has one redacted exact-email condition that PayGuard adds locally. Do not include an email address, an email identity, or the redaction marker in the response. Only propose a rule if the user asks to block when the exact email and all the additional conditions match. Use BLOCK and AND, preserve the specified product scope and every other condition, and include at most four additional conditions. If only the exact email is requested, select all available products, use an empty conditions array, and require a duration. For a product-only exact-email restriction, include a product.type condition. Set one of durationHours or durationMinutes to the explicitly requested whole number of hours or minutes, and use null for the other field. Use null for both when no duration was requested. If an amount unit, action, duration, or condition is unclear, return CLARIFICATION.
An IP country condition requires the registered country field custom_ip_country. For example, "IP from USA" means an eq comparison with the value US. If this field is missing or unavailable for the requested product, return CLARIFICATION. Never use a card country, description, or other field to substitute for IP country.`
	if clarification.Status == "" && len(intent.conditions) != 0 {
		verified, _ := json.Marshal(intent.conditions)
		prompt += "\nPayGuard recognized these explicit typed predicates. Preserve every one when you produce the proposal, alongside the other requested conditions: " + string(verified)
	}
	profile.composerBoundMailbox = true
	data, err := s.provider.interpret(ctx, composerMerchant, prompt, instruction, profile)
	if err != nil {
		result := "PROVIDER_UNAVAILABLE"
		message := "AI Rule drafting could not reach OpenAI. Try again."
		if errors.Is(err, errComposerOutput) {
			result = "INVALID_PROVIDER_OUTPUT"
			message = "OpenAI returned an invalid AI Rule draft."
		}
		s.audit("interpret_compound_mailbox", composerMerchant, actor, result, "", started)
		problem(w, http.StatusBadGateway, message)
		return
	}
	output := clarification
	if output.Status == "" {
		output, err = parseComposerInterpretation(data, composerMerchant, profile)
		if err != nil || strings.Contains(strings.ToLower(string(data)), digest) ||
			(output.Status == "READY" && compoundMailboxMarker.Match(data)) {
			invalidDraft()
			return
		}
		output.Message = compoundMailboxMarker.ReplaceAllString(output.Message, "the specified email address")
	}
	output.Source, output.Model = composerSourceOpenAI, s.model
	if output.Status == "CLARIFICATION" {
		s.audit("interpret_compound_mailbox", composerMerchant, actor, output.Status, "", started)
		s.respondAIInterpretation(w, r, actor, output, composerMerchant, reviewWithJev)
		return
	}
	proposal, ok := output.Proposal.(merchantPolicyProposal)
	if !ok || len(proposal.Conditions.Rules) >= 5 || (!intent.fullyKnown && len(proposal.Conditions.Rules) == 0) ||
		!intent.accepts(proposal, profile) ||
		(duration != 0 && !validMailboxBlockDuration(proposal.DurationHours, proposal.DurationMinutes)) ||
		duration != proposal.DurationHours*60+proposal.DurationMinutes ||
		(duration == 0 && (proposal.DurationHours != 0 || proposal.DurationMinutes != 0)) {
		invalidDraft()
		return
	}
	name := []rune(strings.TrimSpace(proposal.Name))
	if len(name) == 0 || len(name) > 80 {
		invalidDraft()
		return
	}
	ref, err := merchantMailboxRef(ctx, s.access.repo.db, actor.MerchantID, digest)
	if err != nil {
		demoWriteError(w, err)
		return
	}
	value, _ := json.Marshal(ref)
	proposal.Conditions.Rules = append(proposal.Conditions.Rules, merchantPolicyCondition{Field: policyMailbox, Operator: "eq", Value: value})
	// Keep the proposed name and give separately bound rules distinct names.
	nameInput, _ := json.Marshal(proposal)
	nameDigest := sha256.Sum256(nameInput)
	suffix := fmt.Sprintf(" (%x)", nameDigest[:6])
	proposal.Name = string(name[:min(len(name), 80-len(suffix))]) + suffix
	output = validateLocalRule(proposal, profile)
	if output.Status != "READY" {
		invalidDraft()
		return
	}
	output.Source, output.Model = composerSourceOpenAI, s.model
	if missingEvidence.Status != "" {
		missingEvidence.Definition = definitionFromProposal(output.Proposal)
		validated := output.Proposal.(merchantPolicyProposal)
		missingEvidence.DraftProposal = &validated
		missingEvidence.Source, missingEvidence.Model = output.Source, output.Model
		output = missingEvidence
	}
	s.audit("interpret_compound_mailbox", composerMerchant, actor, output.Status, "", started)
	s.respondAIInterpretation(w, r, actor, output, composerMerchant, reviewWithJev)
}

func mailboxDraftIPCountryProfile(profile merchantPolicyProfile) merchantPolicyProfile {
	if !slices.Contains(profile.Signals, policyIPCountry) {
		profile.Signals = append(slices.Clone(profile.Signals), policyIPCountry)
	}
	profile.FieldRegistry = slices.DeleteFunc(slices.Clone(profile.FieldRegistry), func(field merchantCapability) bool {
		return field.Name == policyIPCountry
	})
	profile.FieldRegistry = append(profile.FieldRegistry, merchantCapability{
		Name: policyIPCountry, Label: "IP country", Products: profile.Products, Stages: profile.Stages,
		policyField: policyField{DataType: "country", Operators: []string{"eq"}, DataSource: "required merchant integration evidence", MissingValuePolicy: "no_match"},
	})
	return profile
}
