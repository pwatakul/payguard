package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/language"
)

// Merchant integrations declare optional non-identifying transaction metadata.
type customSignalDefinition struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
}

var customSignalKey = regexp.MustCompile(`^custom_[a-z][a-z0-9_]{0,39}$`)

func customSignalOperators(kind string) []string {
	switch kind {
	case "text":
		return []string{"eq", "contains", semanticOperator}
	case "country", "boolean":
		return []string{"eq", "ne"}
	case "number":
		return []string{"eq", "ne", "gte", "gt", "lte", "lt"}
	}
	return nil
}

func validateCustomDefinitions(fields []customSignalDefinition) error {
	if len(fields) > 16 {
		return errors.New("Register up to 16 custom fields per merchant.")
	}
	seen := make(map[string]bool, len(fields))
	for _, field := range fields {
		if !customSignalKey.MatchString(field.Key) || seen[field.Key] {
			return errors.New("Each custom field needs a unique custom_ key using lowercase letters, digits and underscores.")
		}
		if len(customSignalOperators(field.Type)) == 0 || !safeCustomText(field.Label, 60) {
			return errors.New("Choose a supported field type and a descriptive label of up to 60 characters without personal data.")
		}
		seen[field.Key] = true
	}
	return nil
}

func safeCustomText(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && utf8.RuneCountInString(value) <= limit &&
		!strings.ContainsFunc(value, unicode.IsControl) && !sensitiveComposerText(value)
}

func validCustomValue(kind string, value json.RawMessage) bool {
	switch kind {
	case "number":
		n, err := strconv.ParseInt(string(value), 10, 64)
		return err == nil && n >= -jevRuleMaxValue && n <= jevRuleMaxValue
	case "boolean":
		return string(value) == "true" || string(value) == "false"
	case "text", "country":
		var s string
		if json.Unmarshal(value, &s) != nil || !safeCustomText(s, 120) {
			return false
		}
		if kind == "country" {
			region, err := language.ParseRegion(s)
			return err == nil && len(s) == 2 && region.IsCountry() && region.String() == s
		}
		return true
	}
	return false
}

func validateTransactionCustomSignals(ctx context.Context, tx pgx.Tx, merchant string, values map[string]json.RawMessage) error {
	if len(values) == 0 {
		return nil
	}
	var integration *merchantPolicyIntegration
	if err := tx.QueryRow(ctx, "SELECT policy_integration FROM merchant_demo_merchants WHERE id=$1::text::uuid AND deleted_at IS NULL", merchant).Scan(&integration); err != nil {
		return err
	}
	for key, value := range values {
		if key == policyDescription && validCustomValue("text", value) {
			continue
		}
		if integration != nil && slices.ContainsFunc(integration.CustomFields, func(f customSignalDefinition) bool {
			return f.Key == key && validCustomValue(f.Type, value)
		}) {
			continue
		}
		return merchantPolicyValidationError{errors.New("A custom signal is unregistered, has an invalid type, or contains sensitive data.")}
	}
	return nil
}

func equalCustomSignals(a, b map[string]json.RawMessage) bool {
	if len(a) != len(b) {
		return false
	}
	for key, value := range a {
		var left, right any
		if json.Unmarshal(value, &left) != nil || json.Unmarshal(b[key], &right) != nil {
			return false
		}
		switch left.(type) {
		case string, float64, bool:
			switch right.(type) {
			case string, float64, bool:
				if left == right {
					continue
				}
			}
		}
		return false
	}
	return true
}

func evaluateCustomCondition(c merchantPolicyCondition, values map[string]json.RawMessage) (bool, bool) {
	value, ok := values[c.Field]
	if !ok || len(value) == 0 {
		return false, true
	}
	switch value[0] {
	case '"':
		var actual, expected string
		if json.Unmarshal(value, &actual) != nil || json.Unmarshal(c.Value, &expected) != nil {
			return false, true
		}
		actual, expected = strings.ToLower(strings.TrimSpace(actual)), strings.ToLower(strings.TrimSpace(expected))
		switch c.Operator {
		case "eq":
			return actual == expected, false
		case "ne":
			return actual != expected, false
		case "contains":
			return strings.Contains(actual, expected), false
		}
	case 't', 'f':
		switch c.Operator {
		case "eq":
			return bytes.Equal(value, c.Value), false
		case "ne":
			return !bytes.Equal(value, c.Value), false
		}
	default:
		actual, aerr := strconv.ParseInt(string(value), 10, 64)
		expected, eerr := strconv.ParseInt(string(c.Value), 10, 64)
		if aerr != nil || eerr != nil {
			return false, true
		}
		return compareRuleNumber(actual, c.Operator, expected), false
	}
	return false, false
}

// Keep a rule's field type stable across changes and rollback to older versions.
func checkCustomFieldChanges(ctx context.Context, tx pgx.Tx, merchant string, next *merchantPolicyIntegration) error {
	if next == nil {
		return nil
	}
	var current *merchantPolicyIntegration
	if err := tx.QueryRow(ctx, "SELECT policy_integration FROM merchant_demo_merchants WHERE id=$1::text::uuid AND deleted_at IS NULL FOR UPDATE", merchant).Scan(&current); err != nil {
		return err
	}
	if current == nil {
		return nil
	}
	if next.CustomFields == nil {
		next.CustomFields = current.CustomFields
		return nil
	}
	for _, old := range current.CustomFields {
		if slices.ContainsFunc(next.CustomFields, func(f customSignalDefinition) bool { return f.Key == old.Key && f.Type == old.Type }) {
			continue
		}
		var used bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM merchant_policy_rule_versions WHERE merchant_id=$1::text::uuid
			AND NOT COALESCE(preview->'missingSignals' ? $2::text, false)
			AND proposal->'conditions'->'rules' @> jsonb_build_array(jsonb_build_object('field',$2::text)))
			OR EXISTS (SELECT 1 FROM merchant_demo_transactions WHERE merchant_id=$1::text::uuid AND custom_signals ? $2::text)`, merchant, old.Key).Scan(&used); err != nil {
			return err
		}
		if used {
			return merchantPolicyValidationError{fmt.Errorf("Keep the key and type for %s because it has recorded rules or transactions.", old.Key)}
		}
	}
	return nil
}
