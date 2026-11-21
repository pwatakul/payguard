package main

import (
	"errors"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

const merchantPolicyPath = "/api/v1/booking/demo/merchant-policy"

type merchantPolicyValidationError struct{ error }

func (s mockMerchantService) registerMerchantPolicies(mux *http.ServeMux) {
	mux.HandleFunc("GET "+merchantPolicyPath+"/profile", s.policyProfile)
	mux.HandleFunc("GET "+merchantPolicyPath+"/rules", s.policyList)
	mux.HandleFunc("GET "+merchantPolicyPath+"/rules/{id}", s.policyHistory)
	mux.HandleFunc("POST "+merchantPolicyPath+"/rules", s.policyMutate)
	mux.HandleFunc("PUT "+merchantPolicyPath+"/rules/{id}", s.policyMutate)
	mux.HandleFunc("DELETE "+merchantPolicyPath+"/rules/{id}", s.policyMutate)
	adminPath := merchantDemoPath + "/{merchantId}"
	mux.HandleFunc("GET "+adminPath+"/policy-profile", s.policyProfile)
	mux.HandleFunc("POST "+adminPath+"/policies", s.policyMutate)
	mux.HandleFunc("PUT "+adminPath+"/policies/{id}", s.policyMutate)
	mux.HandleFunc("DELETE "+adminPath+"/policies/{id}", s.policyMutate)
	for _, operation := range []string{"activate", "disable", "rollback", "simulate"} {
		mux.HandleFunc("POST "+adminPath+"/policies/{id}/"+operation, s.policyMutate)
		mux.HandleFunc("POST "+merchantPolicyPath+"/rules/{id}/"+operation, s.policyMutate)
	}
}

func (s mockMerchantService) policyActor(w http.ResponseWriter, r *http.Request) (string, bool) {
	if r.PathValue("merchantId") != "" {
		if _, ok := s.authorize(w, r, true); !ok {
			return "", false
		}
		return demoPathID(w, r, "merchantId")
	}
	actor, ok := s.authorize(w, r, false)
	if ok && (actor.Admin || actor.MerchantID == "") {
		problem(w, http.StatusForbidden, "A credential belonging to the merchant is required.")
		return "", false
	}
	return actor.MerchantID, ok
}

func merchantPolicyError(w http.ResponseWriter, err error) {
	var validation merchantPolicyValidationError
	switch {
	case errors.As(err, &validation):
		problem(w, http.StatusUnprocessableEntity, validation.Error())
	case errors.Is(err, errPolicyVersion), errors.Is(err, errPolicyRuleLimit):
		problem(w, http.StatusConflict, err.Error())
	case errors.Is(err, pgx.ErrNoRows):
		problem(w, http.StatusNotFound, "Merchant policy not found.")
	default:
		bookingRiskUnavailable(w, err)
	}
}

func (s mockMerchantService) policyProfile(w http.ResponseWriter, r *http.Request) {
	merchant, ok := s.policyActor(w, r)
	if !ok {
		return
	}
	profile, err := s.merchantPolicyProfile(r.Context(), merchant)
	if err != nil {
		merchantPolicyError(w, err)
		return
	}
	respond(w, http.StatusOK, profile)
}

func (s mockMerchantService) policyList(w http.ResponseWriter, r *http.Request) {
	merchant, ok := s.policyActor(w, r)
	if !ok {
		return
	}
	s.respondMerchantPolicies(w, r, merchant)
}
func (s mockMerchantService) respondMerchantPolicies(w http.ResponseWriter, r *http.Request, merchant string) {
	rules, err := readMerchantPolicies(r.Context(), s.repo.db, merchant, false)
	if err != nil {
		merchantPolicyError(w, err)
		return
	}
	effective, err := readMerchantPolicies(r.Context(), s.repo.db, merchant, true)
	if err != nil {
		merchantPolicyError(w, err)
		return
	}
	for i := range rules {
		if rules[i].Status == "ACTIVE" {
			rules[i].ActiveVersion = rules[i].Version
			continue
		}
		if rules[i].Status != "PENDING_APPROVAL" {
			continue
		}
		for _, current := range effective {
			if rules[i].ID == current.ID {
				rules[i].ActiveVersion = current.Version
				break
			}
		}
	}
	respond(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s mockMerchantService) policyHistory(w http.ResponseWriter, r *http.Request) {
	merchant, ok := s.policyActor(w, r)
	if !ok {
		return
	}
	s.respondMerchantPolicyHistory(w, r, merchant)
}
func (s mockMerchantService) respondMerchantPolicyHistory(w http.ResponseWriter, r *http.Request, merchant string) {
	id, ok := demoPathID(w, r, "id")
	if !ok {
		return
	}

	rows, err := s.repo.db.Query(r.Context(), `SELECT `+merchantPolicyColumns+`
		FROM merchant_policy_rule_versions WHERE merchant_id=$1::text::uuid AND rule_id=$2::text::uuid ORDER BY version DESC LIMIT 100`,
		merchant, id)
	if err != nil {
		merchantPolicyError(w, err)
		return
	}
	defer rows.Close()
	history := []merchantPolicyRule{}
	for rows.Next() {
		item, err := scanMerchantPolicy(rows)
		if err != nil {
			merchantPolicyError(w, err)
			return
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		merchantPolicyError(w, err)
		return
	}
	if len(history) == 0 {
		merchantPolicyError(w, pgx.ErrNoRows)
		return
	}
	respond(w, http.StatusOK, map[string]any{"rule": history[0], "versions": history})
}

func (s mockMerchantService) policyMutate(w http.ResponseWriter, r *http.Request) {
	merchant, ok := s.policyActor(w, r)
	if !ok {
		return
	}
	input := struct {
		Proposal        merchantPolicyProposal `json:"proposal"`
		Version         int                    `json:"version"`
		ExpectedVersion int                    `json:"expectedVersion"`
		Instruction     string                 `json:"instruction"`
		SaveAsDraft     bool                   `json:"saveAsDraft"`
	}{}
	operation := "save"
	id := r.PathValue("id")
	if id != "" {
		if _, valid := demoPathID(w, r, "id"); !valid {
			return
		}
		if r.Method == http.MethodDelete {
			operation = "delete"
		} else if r.Method == http.MethodPut {
			operation = "edit"
		} else {
			operation = r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
		}
	}
	if operation == "simulate" {
		rule, err := scanMerchantPolicy(s.repo.db.QueryRow(r.Context(), `SELECT `+merchantPolicyColumns+`
			FROM merchant_policy_rule_versions WHERE merchant_id=$1::text::uuid AND rule_id=$2::text::uuid ORDER BY version DESC LIMIT 1`, merchant, id))
		if err != nil {
			merchantPolicyError(w, err)
			return
		}
		profile, evidence, err := readMerchantPolicyContext(r.Context(), s.repo.db, merchant)
		if err != nil {
			merchantPolicyError(w, err)
			return
		}
		if slices.Contains(rule.Preview.MissingSignals, policyIPCountry) {
			if !merchantPolicyIPCountryAvailable(rule.Proposal, profile) {
				merchantPolicyError(w, errPolicyIPCountryRequired)
				return
			}
			if err := rule.Proposal.validate(profile); err != nil {
				merchantPolicyError(w, merchantPolicyValidationError{err})
				return
			}
		}
		respond(w, http.StatusOK, map[string]any{"preview": previewMerchantPolicy(rule.Proposal, evidence)})
		return
	}
	if !decodeBookingRiskJSON(w, r, &input) {
		return
	}
	if input.SaveAsDraft && operation != "save" && operation != "edit" {
		merchantPolicyError(w, merchantPolicyValidationError{errors.New("Save as draft is available when creating or editing a rule.")})
		return
	}
	instruction := strings.TrimSpace(input.Instruction)
	if instruction != "" {
		if operation != "save" && operation != "edit" {
			problem(w, http.StatusUnprocessableEntity, "Supply a rule instruction when saving or editing a rule.")
			return
		}
		if utf8.RuneCountInString(instruction) > 2000 || sensitiveComposerText(instruction) ||
			strings.IndexFunc(instruction, func(r rune) bool {
				return unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r'
			}) != -1 || slices.ContainsFunc(input.Proposal.Conditions.Rules, func(condition merchantPolicyCondition) bool {
			return condition.Field == policyMailbox
		}) {
			problem(w, http.StatusUnprocessableEntity, "Save a rule description of up to 2,000 characters without personal data or credentials.")
			return
		}
		if emailRiskMention.MatchString(instruction) && !emailRiskProposalHasSignal(input.Proposal) {
			problem(w, http.StatusUnprocessableEntity, "Add the email condition from your instruction before saving.")
			return
		}
	}
	if id != "" && input.ExpectedVersion <= 0 {
		problem(w, http.StatusUnprocessableEntity, "The latest expectedVersion is required.")
		return
	}
	if operation == "rollback" && (input.Version <= 0 || input.Version >= input.ExpectedVersion) {
		problem(w, http.StatusUnprocessableEntity, "Select an older rule version to restore.")
		return
	}
	actor := merchant
	if r.PathValue("merchantId") != "" {
		actor = platformMaker
	}
	rule, err := s.repo.mutateMerchantPolicy(r.Context(), merchant, merchantPolicyMutation{
		Actor: auditActor(r.Context(), actor), Kind: operation, ID: id, ExpectedVersion: input.ExpectedVersion, RollbackVersion: input.Version,
		Proposal: input.Proposal, Instruction: instruction, SaveAsDraft: input.SaveAsDraft,
	})
	if err != nil {
		merchantPolicyError(w, err)
		return
	}
	status := http.StatusOK
	if operation == "save" {
		status = http.StatusCreated
	}
	respond(w, status, map[string]any{"rule": rule, "status": rule.Status})
}
