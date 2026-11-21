package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/text/language"
	"golang.org/x/text/language/display"
)

var (
	customMerchantAction   = regexp.MustCompile(`(?is)^(block\b|review\b|challenge\b|บล็อก|ส่งตรวจสอบ|ตรวจสอบ|ยืนยันเพิ่มเติม)\s*(.+)$`)
	customTrailingAction   = regexp.MustCompile(`(?is)^if\s+(.+?)(?:,\s*(?:then\s+)?|\s+then\s+)(block|review|challenge)(?:\s+(?:it|(?:the\s+)?(?:booking|transaction|payment|order|purchase)))?$`)
	customTrailingThai     = regexp.MustCompile(`(?s)^ถ้า\s*(.+?)\s*ให้\s*(บล็อก|ส่งตรวจสอบ|ตรวจสอบ|ยืนยันเพิ่มเติม)\s*(?:(?:รายการ|ธุรกรรม|การจอง|การชำระเงิน)(?:นี้|นั้น)?)?$`)
	customRepeatedAction   = regexp.MustCompile(`(?i)(?:\b(?:and|or|then)\s+|แต่|หรือ|และ|แล้ว|ให้)\s*(?:block\b|review\b|challenge\b|บล็อก|ส่งตรวจสอบ|ตรวจสอบ|ยืนยันเพิ่มเติม)`)
	customUnclearAction    = regexp.MustCompile(`(?i)\b(?:do not|don't|never|maybe|might|may|could|should we)\s+(?:block|review|challenge)\b|(?:ไม่(?:ต้อง)?|อาจ(?:จะ)?)\s*(?:บล็อก|ส่งตรวจสอบ|ตรวจสอบ|ยืนยันเพิ่มเติม)`)
	customRuleWord         = regexp.MustCompile(`(?i)^\s*(?:when\b|if\b|เมื่อ|ถ้า)\s*`)
	customExplicitLifetime = regexp.MustCompile(`(?i)\b(?:temporarily|from\s+now|for\s+the\s+next|for\s+a\s+while)\b|ชั่วคราว|มีผลถึง`)
	customDurationSuffix   = regexp.MustCompile(`(?i)(?:[0-9]+|\b(?:an?|one|two|three|four|five|six|seven|eight|nine|ten))\s*(?:(?:seconds?|secs?|s|minutes?|mins?|m|hours?|hrs?|h|days?|d|weeks?|wks?|months?|years?|yrs?)\b|วินาที|นาที|ชั่วโมง|ชม\.?|วัน|สัปดาห์|เดือน|ปี)\s*[.]?$`)
	customObservedTime     = regexp.MustCompile(`(?i)\b(?:in\s+the\s+last|in\s+the\s+past|last|past|within|age|older|younger|previous|inactive)\b|ที่ผ่านมา|ย้อนหลัง|ภายใน|อายุ`)
	customUntil            = regexp.MustCompile(`(?i)\buntil\b|จนถึง`)
	customRuleConjunction  = regexp.MustCompile(`(?i)\s+(and|or|และ|หรือ)\s+`)
	customRuleTyped        = regexp.MustCompile(`(?is)^(custom_[a-z][a-z0-9_]*)\s*(does not equal|at least|at most|greater than|less than|equals|contains|>=|<=|!=|=|>|<)\s*(.+)$`)
	customRuleMeaning      = regexp.MustCompile(`(?is)^(custom_[a-z][a-z0-9_]*)\s*(?:means|refers to|looks like|matches the meaning of|หมายถึง|สื่อถึง)\s*(.+)$`)
	customRuleCountry      = regexp.MustCompile(`(?is)^(?:ip(?: address)? country (?:is|equals)\b|ip(?: address)? (?:comes from|is from|from)\b|(?:ip|ไอพี)\s*(?:มาจาก|จาก)|ประเทศไอพี\s*(?:คือ|เท่ากับ))\s*(.+)$`)
	customRuleBareAmount   = regexp.MustCompile(`(?i)^(?:amount|ยอด(?:เงิน)?)\s*(?:is\s+)?(?:at least|at most|greater than|less than|equals|does not equal|อย่างน้อย|ไม่เกิน|มากกว่า|น้อยกว่า|เท่ากับ|ไม่เท่ากับ|>=|<=|>|<|=|!=)\s*[0-9]+(?:\.[0-9]+)?$`)
)

const policyIPCountry = "custom_ip_country"

// Draft from the original wording. No provider response is used to fill
// unsupported fields, and only the merchant can confirm and save the draft.
func interpretCustomMerchantRule(instruction string, profile merchantPolicyProfile) (composerInterpretation, bool) {
	sentence := strings.TrimSpace(strings.TrimSuffix(unquoteRuleText(instruction), "."))
	action, predicate := customActionAndPredicate(sentence)
	if action == "" || sensitiveComposerText(sentence) || hasCustomRuleLifetime(sentence) || hasAmbiguousCustomAction(sentence) {
		return composerInterpretation{}, false
	}
	predicate = strings.TrimSpace(customRuleWord.ReplaceAllString(predicate, ""))
	products := slices.Clone(profile.Products)

	scoped := localMerchantSentence.FindStringSubmatch(sentence)
	if scoped == nil {
		scoped = localMerchantThai.FindStringSubmatch(sentence)
	}
	if scoped != nil {
		if known := localComposerProducts(scoped[2], profile); known != nil {
			products, predicate = known, strings.TrimSpace(scoped[3])
		}
	}
	if !slices.Contains(profile.Signals, policyDescription) || predicate == "" ||
		localComposerProducts(predicate, profile) != nil {
		return composerInterpretation{}, false
	}

	group, ok := customMerchantConditions(predicate, profile, products)
	if !ok {
		return composerInterpretation{}, false
	}
	namePrefix := localActionTitle(action) + " rule"
	for _, condition := range group.Rules {
		if condition.Operator == semanticOperator {
			namePrefix = "Custom Jev " + strings.ToLower(action) + " rule"
			break
		}
	}
	source, _ := json.Marshal(group)
	digest := sha256.Sum256([]byte(strings.Join(sortedPolicyValues(products), ",") + "|" + string(source)))
	return validateLocalRule(merchantPolicyProposal{
		Scope: composerMerchant, Name: fmt.Sprintf("%s (%x)", namePrefix, digest[:4]),
		ProductTypes: products, Stages: merchantComposerStages(products), Action: action, Conditions: group,
	}, profile), true
}

func customActionAndPredicate(sentence string) (string, string) {
	if prefix := customMerchantAction.FindStringSubmatch(sentence); prefix != nil {
		return localComposerActions[strings.ToLower(prefix[1])], prefix[2]
	}
	for _, pattern := range []*regexp.Regexp{customTrailingAction, customTrailingThai} {
		if suffix := pattern.FindStringSubmatch(sentence); suffix != nil {
			return localComposerActions[strings.ToLower(suffix[2])], suffix[1]
		}
	}
	return "", ""
}

func hasCustomRuleLifetime(instruction string) bool {
	_, predicate := customActionAndPredicate(instruction)
	if predicate == "" {
		return false
	}
	return customExplicitLifetime.MatchString(predicate) ||
		(customUntil.MatchString(predicate) || customDurationSuffix.MatchString(predicate)) && !customObservedTime.MatchString(predicate)
}

func hasAmbiguousCustomAction(sentence string) bool {
	_, predicate := customActionAndPredicate(sentence)
	if predicate == "" {
		predicate = sentence
	}
	return customUnclearAction.MatchString(sentence) || customRepeatedAction.MatchString(predicate)
}

func customMerchantConditions(predicate string, profile merchantPolicyProfile, products []string) (merchantPolicyConditions, bool) {
	logic, clauses := splitCustomRuleConditions(predicate)
	if len(clauses) > 5 {
		// Keep the original expression when it cannot fit a flat group.
		logic, clauses = "AND", []string{predicate}
	}
	group := merchantPolicyConditions{Logic: logic}
	hasStructuredClause := false
	for _, clause := range clauses {
		if customMerchantAction.MatchString(clause) || customRuleBareAmount.MatchString(clause) {
			return merchantPolicyConditions{}, false
		}
		if condition, ok := customStructuredCondition(clause, profile, products); ok {
			group.Rules = append(group.Rules, condition)
			hasStructuredClause = true
			continue
		}
		if localPolicyAmount.MatchString(clause) || localPolicyThaiAmount.MatchString(clause) {
			if _, _, _, _, valid := localComposerCondition(clause); !valid {
				return merchantPolicyConditions{}, false
			}
		}
		group.Rules = append(group.Rules, semanticRuleCondition(policyDescription, clause))
	}
	if !hasStructuredClause {
		return merchantPolicyConditions{Logic: "AND", Rules: []merchantPolicyCondition{semanticRuleCondition(policyDescription, predicate)}}, true
	}
	return group, true
}

func splitCustomRuleConditions(predicate string) (string, []string) {
	var clauses []string
	var logic string
	start, cursor, depth := 0, 0, 0
	var quote rune
	for _, match := range customRuleConjunction.FindAllStringSubmatchIndex(predicate, -1) {
		for _, r := range predicate[cursor:match[0]] {
			switch {
			case quote != 0 && quote == r:
				quote = 0
			case quote != 0:
			case r == '"':
				quote = '"'
			case r == '“':
				quote = '”'
			case r == '(':
				depth++
			case r == ')':
				depth--
			}
		}
		cursor = match[1]
		if quote != 0 || depth != 0 {
			continue
		}
		next := "AND"
		if word := strings.ToLower(predicate[match[2]:match[3]]); word == "or" || word == "หรือ" {
			next = "OR"
		}
		if logic != "" && logic != next {
			return "AND", []string{predicate}
		}
		logic = next
		clauses = append(clauses, strings.TrimSpace(predicate[start:match[0]]))
		start = match[1]
	}
	if logic == "" {
		return "AND", []string{predicate}
	}
	return logic, append(clauses, strings.TrimSpace(predicate[start:]))
}

func customStructuredCondition(clause string, profile merchantPolicyProfile, products []string) (merchantPolicyCondition, bool) {
	if field, operator, value, _, ok := localComposerCondition(clause); ok &&
		composerConditionAvailable(profile, products, field, operator) {
		return merchantPolicyCondition{Field: field, Operator: operator, Value: value}, true
	}
	if match := customRuleMeaning.FindStringSubmatch(clause); match != nil {
		field := strings.ToLower(match[1])
		if composerConditionAvailable(profile, products, field, semanticOperator) {
			return semanticRuleCondition(field, unquoteRuleText(match[2])), true
		}
	}
	countryField, hasCountryField := composerAvailableField(profile, products, policyIPCountry, "eq")
	if match := customRuleCountry.FindStringSubmatch(clause); match != nil && hasCountryField && countryField.DataType == "country" {
		if country := customRuleCountryCode(match[1]); country != "" {
			value, _ := json.Marshal(country)
			return merchantPolicyCondition{Field: policyIPCountry, Operator: "eq", Value: value}, true
		}
	}
	if match := customRuleTyped.FindStringSubmatch(clause); match != nil {
		field, comparison := strings.ToLower(match[1]), strings.ToLower(match[2])
		operator := localComposerComparisons[comparison]
		if comparison == "contains" {
			operator = comparison
		}
		if capability, ok := composerAvailableField(profile, products, field, operator); ok {
			if value, ok := customRuleTypedValue(capability.DataType, match[3]); ok {
				return merchantPolicyCondition{Field: field, Operator: operator, Value: value}, true
			}
		}
	}
	return merchantPolicyCondition{}, false
}

func composerConditionAvailable(profile merchantPolicyProfile, products []string, field, operator string) bool {
	_, available := composerAvailableField(profile, products, field, operator)
	return available
}

func composerAvailableField(profile merchantPolicyProfile, products []string, field, operator string) (policyField, bool) {
	if !slices.Contains(profile.Signals, field) {
		return policyField{}, false
	}
	for _, capability := range profile.FieldRegistry {
		if capability.Name == field {
			return capability.policyField, slices.Contains(capability.Operators, operator) && !slices.ContainsFunc(products, func(product string) bool {
				return !slices.Contains(capability.Products, product)
			})
		}
	}
	definition := merchantPolicyFields[field]
	return definition, slices.Contains(definition.Operators, operator)
}

func semanticRuleCondition(field, criterion string) merchantPolicyCondition {
	value, _ := json.Marshal(criterion)
	return merchantPolicyCondition{Field: field, Operator: semanticOperator, Value: value}
}

func customRuleTypedValue(dataType, raw string) (json.RawMessage, bool) {
	var value any
	raw = unquoteRuleText(raw)
	switch dataType {
	case "text":
		value = raw
	case "country":
		country := customRuleCountryCode(raw)
		if country == "" {
			return nil, false
		}
		value = country
	case "boolean":
		switch strings.ToLower(raw) {
		case "true":
			value = true
		case "false":
			value = false
		default:
			return nil, false
		}
	case "number":
		integer, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, false
		}
		value = integer
	default:
		return nil, false
	}
	data, _ := json.Marshal(value)
	return data, validCustomValue(dataType, data)
}

func customRuleCountryCode(raw string) string {
	country := strings.TrimSpace(raw)
	if len(country) == 2 || len(country) == 3 {
		if region, err := language.ParseRegion(strings.ToUpper(country)); err == nil && region.IsCountry() && len(region.String()) == 2 {
			return region.String()
		}
	}
	english, thai := display.English.Regions(), display.Regions(language.Thai)
	for first := byte('A'); first <= 'Z'; first++ {
		for second := byte('A'); second <= 'Z'; second++ {
			code := string([]byte{first, second})
			region, err := language.ParseRegion(code)
			if err == nil && region.IsCountry() &&
				(strings.EqualFold(english.Name(region), country) || strings.EqualFold(thai.Name(region), country)) {
				return region.String()
			}
		}
	}
	return ""
}

func unquoteRuleText(value string) string {
	value = strings.TrimSpace(value)
	for _, quote := range [][2]string{{`"`, `"`}, {"“", "”"}} {
		if strings.HasPrefix(value, quote[0]) && strings.HasSuffix(value, quote[1]) &&
			len(value) > len(quote[0])+len(quote[1]) {
			return strings.TrimSpace(value[len(quote[0]) : len(value)-len(quote[1])])
		}
	}
	return value
}
