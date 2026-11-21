package main

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const merchantPolicySchema = `
ALTER TABLE merchant_demo_merchants ADD COLUMN IF NOT EXISTS policy_integration jsonb;
ALTER TABLE merchant_demo_transactions ADD COLUMN IF NOT EXISTS customer_email_domain text NOT NULL DEFAULT '';
ALTER TABLE merchant_demo_transactions ADD COLUMN IF NOT EXISTS policy_decision jsonb;
ALTER TABLE merchant_demo_transactions ADD COLUMN IF NOT EXISTS custom_signals jsonb;
CREATE TABLE IF NOT EXISTS merchant_policy_rule_versions (
	merchant_id uuid NOT NULL REFERENCES merchant_demo_merchants(id),
	rule_id uuid NOT NULL,
	version integer NOT NULL CHECK (version > 0),
	status text NOT NULL CHECK (status IN ('ACTIVE', 'PENDING_APPROVAL', 'DISABLED')),
	proposal jsonb NOT NULL,
	preview jsonb NOT NULL,
	created_by text NOT NULL,
	created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
	change_reason text NOT NULL,
	rollback_version integer,
	PRIMARY KEY (merchant_id, rule_id, version),
	FOREIGN KEY (merchant_id, rule_id, rollback_version) REFERENCES merchant_policy_rule_versions(merchant_id, rule_id, version)
);
ALTER TABLE merchant_policy_rule_versions ADD COLUMN IF NOT EXISTS instruction text NOT NULL DEFAULT '';
`

var (
	errPolicyVersion           = errors.New("This rule changed. Reload its latest version before retrying.")
	errPolicyRuleLimit         = errors.New("This merchant has reached the limit of 50 policy rules.")
	errPolicyIPCountryRequired = merchantPolicyValidationError{errors.New("Register custom_ip_country as a Country code signal for the selected products before using this rule.")}
)

type merchantPolicyIntegration struct {
	Products     []string                 `json:"products"`
	CustomFields []customSignalDefinition `json:"customFields,omitempty"`
	EmailDomain  bool                     `json:"emailDomain"`
	AmountMinor  bool                     `json:"amountMinor,omitempty"`
}

func (integration merchantPolicyIntegration) valid() bool {
	kinds := make(map[string]bool)
	for _, item := range integration.Products {
		if kinds[item] || merchantPolicyStage(item) == "" {
			return false
		}
		kinds[item] = true
	}
	return true
}

type merchantPolicyPreview struct {
	Evaluated       int      `json:"evaluated"`
	Matched         int      `json:"matched"`
	Missing         int      `json:"missing"`
	RequiresJev     int      `json:"requiresJev,omitempty"`
	ConflictRuleIDs []string `json:"conflictRuleIds,omitempty"`
	ApprovalReason  string   `json:"approvalReason,omitempty"`
	MissingSignals  []string `json:"missingSignals,omitempty"`
}

type merchantPolicyRule struct {
	ID              string                 `json:"id"`
	Version         int                    `json:"version"`
	ActiveVersion   int                    `json:"activeVersion,omitempty"`
	Status          string                 `json:"status"`
	Proposal        merchantPolicyProposal `json:"proposal"`
	Instruction     string                 `json:"instruction,omitempty"`
	Preview         merchantPolicyPreview  `json:"preview"`
	CreatedAt       time.Time              `json:"createdAt"`
	ExpiresAt       *time.Time             `json:"expiresAt,omitempty"`
	CreatedBy       string                 `json:"createdBy"`
	ChangeReason    string                 `json:"changeReason"`
	RollbackVersion *int                   `json:"rollbackVersion,omitempty"`
}

func (s mockMerchantService) merchantPolicyProfile(ctx context.Context, merchant string) (merchantPolicyProfile, error) {
	profile, _, err := readMerchantPolicyContext(ctx, s.repo.db, merchant)
	return profile, err
}

func readMerchantPolicyContext(ctx context.Context, db mockMerchantQueryer, merchant string) (merchantPolicyProfile, []merchantPolicyEvidence, error) {
	profile := merchantPolicyProfile{
		MerchantID: merchant, Products: []string{}, Stages: []string{}, Signals: []string{policyProduct},
		ScoringProfileVersion: merchantRuleProfileVersion,
	}
	var setup *merchantSetup
	var integration *merchantPolicyIntegration
	if err := db.QueryRow(ctx, `SELECT name,setup,policy_integration FROM merchant_demo_merchants
		WHERE id=$1::text::uuid AND deleted_at IS NULL`, merchant).Scan(&profile.BusinessName, &setup, &integration); err != nil {
		return profile, nil, err
	}
	var hasAmount, hasDomain bool
	if setup != nil {
		profile.BusinessModel = setup.BusinessModel
		hasAmount = slices.Contains(setup.AvailableSignals, bookingAmount)
	}
	if integration != nil {
		profile.Products = append(profile.Products, integration.Products...)
		hasDomain = integration.EmailDomain
		hasAmount = hasAmount || integration.AmountMinor
	}
	declaredAmount, declaredDomain := hasAmount, hasDomain
	observedAmount, observedDomain := map[string]bool{}, map[string]bool{}
	rows, err := db.Query(ctx, `SELECT product, amount_minor, customer_email_domain, customer_mailbox_ref, customer_email_random, custom_signals FROM merchant_demo_transactions
		WHERE merchant_id=$1::text::uuid ORDER BY created_at DESC,id DESC LIMIT 200`, merchant)
	if err != nil {
		return profile, nil, err
	}
	defer rows.Close()
	var evidence []merchantPolicyEvidence
	for rows.Next() {
		var item merchantPolicyEvidence
		if err := rows.Scan(&item.product, &item.amount, &item.domain, &item.mailboxRef, &item.randomLocalPart, &item.custom); err != nil {
			return profile, nil, err
		}
		item.stage = merchantPolicyStage(item.product)
		if item.stage == "" {
			continue
		}
		hasAmount = hasAmount || item.amount != nil
		hasDomain = hasDomain || item.domain != ""
		observedAmount[item.product] = observedAmount[item.product] || item.amount != nil
		observedDomain[item.product] = observedDomain[item.product] || item.domain != ""
		if !slices.Contains(profile.Products, item.product) {
			profile.Products = append(profile.Products, item.product)
		}
		evidence = append(evidence, item)
	}
	if err := rows.Err(); err != nil {
		return profile, nil, err
	}
	slices.Sort(profile.Products)
	for _, product := range profile.Products {
		stage := merchantPolicyStage(product)
		if stage != "" && !slices.Contains(profile.Stages, stage) {
			profile.Stages = append(profile.Stages, stage)
		}
	}
	slices.Sort(profile.Stages)
	if hasAmount {
		profile.Signals = append(profile.Signals, policyAmount)
	}
	if hasDomain {
		profile.Signals = append(profile.Signals, policyDomain, policyDisposableEmail)
	}
	if len(profile.Products) > 0 {
		profile.Signals = append(profile.Signals, policyMailbox, policyRandomEmail, policyDescription)
	}
	for _, signal := range profile.Signals {
		entry := merchantCapability{Name: signal, policyField: merchantPolicyFields[signal], Products: []string{}, Stages: []string{}}
		switch signal {
		case policyDescription:
			entry.Label = "Transaction description"
		case policyDisposableEmail:
			entry.Label = "Disposable email domain"
		case policyRandomEmail:
			entry.Label = "Random-looking email username"
		}
		for _, product := range profile.Products {
			if (signal == policyAmount && !declaredAmount && !observedAmount[product]) ||
				((signal == policyDomain || signal == policyDisposableEmail) && !declaredDomain && !observedDomain[product]) {
				continue
			}
			entry.Products = append(entry.Products, product)
			stage := merchantPolicyStage(product)
			if !slices.Contains(entry.Stages, stage) {
				entry.Stages = append(entry.Stages, stage)
			}
		}
		profile.FieldRegistry = append(profile.FieldRegistry, entry)
	}
	if integration != nil {
		for _, f := range integration.CustomFields {
			profile.Signals = append(profile.Signals, f.Key)
			profile.FieldRegistry = append(profile.FieldRegistry, merchantCapability{Name: f.Key, Label: f.Label,
				policyField: policyField{DataType: f.Type, Operators: customSignalOperators(f.Type), DataSource: "merchant integration", Privacy: "non-identifying metadata", MissingValuePolicy: "no_match"},
				Products:    profile.Products, Stages: profile.Stages})
		}
	}
	return profile, evidence, nil
}

func previewMerchantPolicy(proposal merchantPolicyProposal, evidence []merchantPolicyEvidence) merchantPolicyPreview {
	var preview merchantPolicyPreview
	for _, item := range evidence {
		if !slices.Contains(proposal.ProductTypes, item.product) || !slices.Contains(proposal.Stages, item.stage) {
			continue
		}
		preview.Evaluated++
		if slices.ContainsFunc(proposal.Conditions.Rules, func(c merchantPolicyCondition) bool { return c.Operator == semanticOperator }) {
			matched, missing, deferred := previewSemanticGroup(proposal.Conditions, item)
			switch {
			case matched:
				preview.Matched++
			case missing:
				preview.Missing++
			case deferred:
				preview.RequiresJev++
			}
			continue
		}
		matched, missing := proposal.evaluate(item)
		if matched {
			preview.Matched++
		}
		if missing {
			preview.Missing++
		}
	}
	return preview
}

// Historical samples are previewed locally. Only unresolved semantic comparisons
// with available evidence would send a question during an actual transaction.
func previewSemanticGroup(group merchantPolicyConditions, item merchantPolicyEvidence) (matched, missing, deferred bool) {
	for _, c := range group.Rules {
		if c.Operator == semanticOperator {
			if validCustomValue("text", item.custom[c.Field]) {
				deferred = true
			} else {
				missing = true
			}
			continue
		}
		match, unavailable := c.evaluate(item)
		if !unavailable && ((group.Logic == "AND" && !match) || (group.Logic == "OR" && match)) {
			return match, false, false
		}
		missing = missing || unavailable
	}
	if group.Logic == "OR" && deferred {
		return false, false, true
	}
	return false, missing, deferred
}

const merchantPolicyExpiry = `(CASE WHEN status='ACTIVE' THEN created_at+COALESCE(
	(proposal->>'durationMinutes')::integer * interval '1 minute',
	(proposal->>'durationHours')::integer * interval '1 hour'
) END)`

const merchantPolicyColumns = `rule_id::text, version, status, proposal, preview, created_at, created_by, change_reason, rollback_version, instruction, ` + merchantPolicyExpiry + `, statement_timestamp()`

func scanMerchantPolicy(row pgx.Row) (merchantPolicyRule, error) {
	var rule merchantPolicyRule
	var asOf time.Time
	err := row.Scan(&rule.ID, &rule.Version, &rule.Status, &rule.Proposal, &rule.Preview,
		&rule.CreatedAt, &rule.CreatedBy, &rule.ChangeReason, &rule.RollbackVersion, &rule.Instruction, &rule.ExpiresAt, &asOf)
	if rule.ExpiresAt != nil && !asOf.Before(*rule.ExpiresAt) {
		rule.Status = "EXPIRED"
	}
	return rule, err
}

func readMerchantPolicies(ctx context.Context, db mockMerchantQueryer, merchant string, effective bool) ([]merchantPolicyRule, error) {
	query := `SELECT DISTINCT ON (rule_id) ` + merchantPolicyColumns + `
		FROM merchant_policy_rule_versions WHERE merchant_id=$1::text::uuid`
	if effective {
		query += ` AND status <> 'PENDING_APPROVAL'`
	}
	rows, err := db.Query(ctx, query+` ORDER BY rule_id, version DESC`, merchant)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	rules := []merchantPolicyRule{}
	for rows.Next() {
		rule, err := scanMerchantPolicy(rows)
		if err != nil {
			return nil, err
		}
		if rule.ChangeReason != "delete" && (!effective || rule.Status == "ACTIVE") {
			rules = append(rules, rule)
		}
	}
	return rules, rows.Err()
}

func policyExclusiveText(p merchantPolicyProposal, field string) string {
	if p.Conditions.Logic != "AND" {
		return ""
	}
	for _, condition := range p.Conditions.Rules {
		if condition.Field == field && condition.Operator == "eq" {
			var value string
			if json.Unmarshal(condition.Value, &value) == nil {
				return value
			}
		}
	}
	return ""
}

func merchantPoliciesCouldConflict(candidate, active merchantPolicyProposal) bool {
	if candidate.Action == active.Action || !slices.ContainsFunc(candidate.ProductTypes, func(product string) bool {
		return slices.Contains(active.ProductTypes, product)
	}) {
		return false
	}
	for _, field := range []string{policyDomain, policyMailbox, policyProduct} {
		a, b := policyExclusiveText(candidate, field), policyExclusiveText(active, field)
		if a != "" && b != "" && a != b {
			return false
		}
	}
	return true
}

type merchantPolicyMutation struct {
	Actor           string
	Kind            string
	ID              string
	ExpectedVersion int
	RollbackVersion int
	Proposal        merchantPolicyProposal
	Instruction     string
	SaveAsDraft     bool
}

func merchantPolicyIPCountryAvailable(proposal merchantPolicyProposal, profile merchantPolicyProfile) bool {
	field, available := composerAvailableField(profile, proposal.ProductTypes, policyIPCountry, "eq")
	return available && field.DataType == "country"
}

func (r repository) mutateMerchantPolicy(ctx context.Context, merchant string, request merchantPolicyMutation) (merchantPolicyRule, error) {
	var result merchantPolicyRule
	err := pgx.BeginFunc(ctx, r.db, func(tx pgx.Tx) error {
		if err := lockMockMerchant(ctx, tx, merchant); err != nil {
			return err
		}
		var current merchantPolicyRule
		if request.ID != "" {
			var err error
			current, err = scanMerchantPolicy(tx.QueryRow(ctx, `SELECT `+merchantPolicyColumns+`
				FROM merchant_policy_rule_versions WHERE merchant_id=$1::text::uuid AND rule_id=$2::text::uuid ORDER BY version DESC LIMIT 1`,
				merchant, request.ID))
			if err != nil {
				return err
			}
			if current.ChangeReason == "delete" {
				return pgx.ErrNoRows
			}
			if current.Version != request.ExpectedVersion {
				return errPolicyVersion
			}
		}
		proposal := request.Proposal
		instruction := request.Instruction
		waitingForCountry := slices.Contains(current.Preview.MissingSignals, policyIPCountry)
		restoreDraft := false
		var rollback *int
		switch request.Kind {
		case "activate", "disable", "delete":
			if request.Kind == "activate" && current.Status == "ACTIVE" || request.Kind == "disable" && current.Status == "DISABLED" {
				result = current
				if current.Status == "ACTIVE" {
					result.ActiveVersion = current.Version
				}
				return nil
			}
			proposal = current.Proposal
			instruction = current.Instruction
		case "rollback":
			previous, err := scanMerchantPolicy(tx.QueryRow(ctx, `SELECT `+merchantPolicyColumns+`
				FROM merchant_policy_rule_versions WHERE merchant_id=$1::text::uuid AND rule_id=$2::text::uuid AND version=$3`,
				merchant, request.ID, request.RollbackVersion))
			if err != nil {
				return err
			}
			proposal, rollback = previous.Proposal, &request.RollbackVersion
			instruction = previous.Instruction
			restoreDraft = slices.Contains(previous.Preview.MissingSignals, policyIPCountry)
			waitingForCountry = waitingForCountry || restoreDraft
		}
		status := "ACTIVE"
		var preview merchantPolicyPreview
		active, err := readMerchantPolicies(ctx, tx, merchant, true)
		if err != nil {
			return err
		}
		if request.Kind == "disable" || request.Kind == "delete" {
			status = "DISABLED"
			preview.MissingSignals = current.Preview.MissingSignals
		} else {
			profile, evidence, err := readMerchantPolicyContext(ctx, tx, merchant)
			if err != nil {
				return err
			}
			hasCountry := slices.ContainsFunc(proposal.Conditions.Rules, func(condition merchantPolicyCondition) bool {
				return condition.Field == policyIPCountry
			})
			exactEmailDraft := request.SaveAsDraft && policyExclusiveText(proposal, policyMailbox) != ""
			missingCountry := hasCountry && (waitingForCountry || exactEmailDraft) && !merchantPolicyIPCountryAvailable(proposal, profile)
			if missingCountry {
				if !exactEmailDraft {
					return errPolicyIPCountryRequired
				}
				profile = mailboxDraftIPCountryProfile(profile)
			}
			if err := proposal.validate(profile); err != nil {
				return merchantPolicyValidationError{err}
			}
			if !missingCountry {
				preview = previewMerchantPolicy(proposal, evidence)
			}
			if request.ID == "" {
				rules, err := readMerchantPolicies(ctx, tx, merchant, false)
				if err != nil {
					return err
				}
				for _, existing := range rules {
					if existing.Status != "DISABLED" && strings.EqualFold(existing.Proposal.Name, proposal.Name) {
						if reflect.DeepEqual(existing.Proposal, proposal) {
							result = existing
							for _, activeRule := range active {
								if activeRule.ID == existing.ID {
									result.ActiveVersion = activeRule.Version
									break
								}
							}
							return nil
						}
						return merchantPolicyValidationError{errors.New("A rule with this name exists. Edit it or choose another name.")}
					}
				}
				if len(rules) >= 50 {
					return errPolicyRuleLimit
				}
			}
			for _, prior := range active {
				if prior.ID != request.ID && merchantPoliciesCouldConflict(proposal, prior.Proposal) {
					preview.ConflictRuleIDs = append(preview.ConflictRuleIDs, prior.ID)
				}
			}
			if request.Kind != "activate" {
				switch {
				case missingCountry:
					preview.MissingSignals = []string{policyIPCountry}
					preview.ApprovalReason = "Register the IP country signal before activating this draft."
				case request.SaveAsDraft || restoreDraft:
					preview.ApprovalReason = "Saved as a draft. Explicit activation is required."
				case len(preview.ConflictRuleIDs) > 0:
					preview.ApprovalReason = "This rule may overlap with active rules that take a different action. Review their actions before activating."
				case proposal.Action == "BLOCK" && broadMerchantBlock(proposal):
					preview.ApprovalReason = "This block can apply to every transaction in a selected product. Explicit activation is required."
				}
			}
			if preview.ApprovalReason != "" {
				status = "PENDING_APPROVAL"
			}
		}
		actor := request.Actor
		if actor == "" {
			actor = merchant
		}
		result, err = scanMerchantPolicy(tx.QueryRow(ctx, `
			INSERT INTO merchant_policy_rule_versions(merchant_id,rule_id,version,status,proposal,preview,created_by,change_reason,rollback_version,instruction)
			VALUES($1::text::uuid, COALESCE(NULLIF($2,'')::uuid,gen_random_uuid()),$3,$4,$5,$6,$9,$7,$8,$10)
			RETURNING `+merchantPolicyColumns, merchant, request.ID, current.Version+1, status, proposal, preview, request.Kind, rollback, actor, instruction))
		if status == "ACTIVE" {
			result.ActiveVersion = result.Version
		}
		if status == "PENDING_APPROVAL" {
			for _, prior := range active {
				if prior.ID == result.ID {
					result.ActiveVersion = prior.Version
					break
				}
			}
		}
		return err
	})
	return result, err
}

func broadMerchantBlock(proposal merchantPolicyProposal) bool {
	if proposal.Conditions.Logic == "OR" {
		return slices.ContainsFunc(proposal.Conditions.Rules, func(item merchantPolicyCondition) bool {
			return item.Field == policyProduct
		})
	}
	return !slices.ContainsFunc(proposal.Conditions.Rules, func(item merchantPolicyCondition) bool {
		return item.Field != policyProduct
	})
}
