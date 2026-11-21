"use client";

import Link from "next/link";
import { FlaskConical, History, Pause, Pencil, Play, RotateCcw, Save, Sparkles, Trash2 } from "lucide-react";
import { useEffect, useId, useRef, useState, useSyncExternalStore } from "react";
import type { FormEvent } from "react";
import { getMerchantDemoRole, getMerchantDemoToken, merchantDemoRequest, merchantPolicyProducts, policyActionLabels, setMerchantDemoToken, subscribeMerchantDemoSession } from "../merchant-demo";
import type { DemoDirectory } from "../merchant-demo";
import { applyMailboxPriceUnit, containsMailbox, maskMailboxInstruction, merchantMailboxDigest, needsMailboxPriceUnit } from "../merchant-mailbox";
import { adminRulePath, useLocalAdminPreview } from "../local-admin-preview";
import { useProductionSession } from "../production-auth";
import { useWorkspaceView } from "../workspace-view";
import type { ReactNode } from "react";
import { actionButton, ruleActionTone, stateBadge } from "../action-styles";
import { merchantDate, merchantDetailsHref } from "./merchants/management-contracts";
import { LegacyJevRuleItem, type LegacyJevRules } from "./jev-rules/panel";
import type { Proposal as LegacyJevProposal } from "./jev-rules/proposal";
import { merchantFieldLabels, merchantOperatorLabels, policyFieldLabel, StructuredRuleBuilder } from "./structured-rule-builder";
import { outlineButton, primaryButton, ruleRequest, rulesEndpoint, stages } from "./system-rules/contracts";
import type { Preview, PreviewRun, Rule, RuleCatalogue, RuleInput } from "./system-rules/contracts";

const composer = "/api/v1/booking/rule-composer";
const policies = "/api/v1/booking/demo/merchant-policy";
const panel = "min-w-0 space-y-4 rounded-xl border border-line bg-white p-4 sm:p-5";

type Scope = "MERCHANT" | "PLATFORM";
export type PolicyAction = "BLOCK" | "REVIEW" | "CHALLENGE";
type PolicyCondition = { field: string; operator: string; value: string | number | boolean };
export type MerchantProposal = {
  scope: "MERCHANT";
  name: string;
  product_types: string[];
  stages: string[];
  conditions: { logic: "AND" | "OR"; rules: PolicyCondition[] };
  action: PolicyAction;
  durationHours?: number;
  durationMinutes?: number;
};
type PlatformProposal = { scope: "PLATFORM"; rule: RuleInput };
type Proposal = MerchantProposal | PlatformProposal;
type RuleDefinitionCondition = Omit<PolicyCondition, "value"> & { value: PolicyCondition["value"] | null };
type RuleDefinition = {
  schemaVersion: 1;
  scope: Scope;
  name: string;
  description?: string;
  productTypes?: string[];
  stages: string[];
  action: string;
  conditions: { logic: "AND" | "OR"; rules: RuleDefinitionCondition[] };
  duration?: { value: number; unit: "minute" | "hour" };
};
type Interpretation = {
  status: "READY" | "CLARIFICATION";
  proposal?: Proposal;
  draftProposal?: MerchantProposal;
  definition?: RuleDefinition;
  message?: string;
  clarificationCode?: string;
  source?: "openai" | "local" | "local_fallback" | "manual";
  model?: string;
  jevReview?: {
    status: "LIVE" | "MOCKED" | "TIMEOUT" | "UNAVAILABLE" | "NOT_RUN";
    model?: string;
    decisionId?: string;
    answer?: "ALIGNED" | "NEEDS_REVIEW" | "INSUFFICIENT";
    fallbackUsed?: boolean;
  };
  validation: { valid: boolean; errors?: string[]; warnings?: string[] };
  semanticDraft?: { field: string };
};
type DraftPreview = Interpretation & { sourceInstruction?: string };

type PolicyPreview = { evaluated: number; matched: number; missing: number; requiresJev?: number; conflictRuleIds?: string[]; approvalReason?: string; missingSignals?: string[] };
type PolicyRule = {
  id: string;
  version: number;
  activeVersion?: number;
  status: "ACTIVE" | "PENDING_APPROVAL" | "DISABLED" | "EXPIRED";
  proposal: MerchantProposal;
  instruction?: string;
  preview: PolicyPreview;
  expiresAt?: string;
};
type PolicySave = { rule: PolicyRule; status: PolicyRule["status"] };
type PlatformSave = { rule: Rule; status: "DRAFT"; preview?: Preview };
export type PolicyField = {
  fieldName: string;
  label?: string;
  dataType: string;
  allowedOperators: string[];
  supportedProductTypes: string[];
  availableStages: string[];
};
export type BusinessProfile = {
  merchantId: string;
  businessName: string;
  products: string[];
  stages: string[];
  signals: string[];
  scoringProfileVersion: string;
  fieldRegistry?: PolicyField[];
};

const scopeLabels = { MERCHANT: "Merchant AI Rule", PLATFORM: "Platform fraud rule" };
const products: Record<string, string> = merchantPolicyProducts;
const mailboxField = "customer.mailbox_ref";
const ipCountryField = "custom_ip_country";
const mailboxExample = "Block email buyer@example.test for 2 minutes";
const jevConditionLabel = "Custom rule · Jev";
const fields: Record<string, string> = {
  [ipCountryField]: "IP country",
  ...merchantFieldLabels,
  account_age_days: "Account age in days",
  device_orders: "Orders from this device in 60 seconds",
  ip_orders: "Orders from this IP in 60 seconds",
  payment_token_orders: "Orders using this payment token in 60 seconds",
  payment_token_missing: "Payment token is missing",
  shared_device_or_token_orders: "Maximum orders using the device or token in 60 seconds",
};
const statusLabels = {
  ACTIVE: "Active", PENDING_APPROVAL: "Pending approval", DISABLED: "Disabled", EXPIRED: "Expired", DRAFT: "Draft",
};
const statusTone = {
  ACTIVE: "positive", PENDING_APPROVAL: "warning", DISABLED: "neutral", EXPIRED: "neutral", DRAFT: "neutral",
} as const;
const prompts: Record<Scope, string> = {
  MERCHANT: "Review all products when amount is at least 50000 minor units",
  PLATFORM: "Send a booking for review if the device placed at least four orders within 60 seconds.",
};

const emptyToken = () => "";
const noRole = () => null;

export function useRuleCredential(scope: Scope) {
  const production = useProductionSession();
  const session = useSyncExternalStore(subscribeMerchantDemoSession, getMerchantDemoToken, emptyToken);
  const role = useSyncExternalStore(subscribeMerchantDemoSession, getMerchantDemoRole, noRole);
  return !production && role === (scope === "MERCHANT" ? "merchant" : "admin") ? session : "";
}

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : "The rule service is unavailable. Try again.";
}

function conditionLabel(field: string, profile?: BusinessProfile | null) {
  const capability = profile?.fieldRegistry?.find(entry => entry.fieldName === field);
  return capability ? policyFieldLabel(capability) : fields[field] ?? field;
}

function conditionDisplayValue({ field, value }: RuleDefinitionCondition) {
  if (field === "product.type") return products[String(value)] ?? String(value);
  if (typeof value === "boolean") return value ? "Yes" : "No";
  return String(value);
}

function describeSavedPolicy(proposal: MerchantProposal, profile?: BusinessProfile | null) {
  const stagesText = proposal.stages.map(stage => stages[stage as keyof typeof stages] ?? stage).join(", ");
  const productText = proposal.product_types.map(product => products[product] ?? product).join(", ");
  const matchText = proposal.conditions.logic === "OR" ? "At least one condition must match." : "All conditions must match.";
  const conditions = proposal.conditions.rules.map(condition => {
    const value = conditionDisplayValue(condition);
    const display = value.length > 240 ? value.slice(0, 239) + "…" : value;
    return `- ${conditionLabel(condition.field, profile)} ${merchantOperatorLabels[condition.operator] ?? condition.operator} ${display}`;
  });
  return [`${policyActionLabels[proposal.action]} ${productText} at ${stagesText}.`, matchText, ...conditions].join("\n");
}

function ProfileDetails({ profile }: { profile: BusinessProfile }) {
  return <dl className="mt-2 grid gap-2 text-sm text-muted sm:grid-cols-3">
    <div><dt className="font-medium text-ink">Products</dt><dd>{profile.products?.map(product => products[product] ?? product).join(", ") || "Unavailable"}</dd></div>
    <div><dt className="font-medium text-ink">Available signals</dt><dd>{profile.signals?.map(field => {
      const capability = profile.fieldRegistry?.find(entry => entry.fieldName === field);
      return capability ? policyFieldLabel(capability) : fields[field] ?? field;
    }).join(", ") || "Unavailable"}</dd></div>
    <div><dt className="font-medium text-ink">Scoring profile</dt><dd>{profile.scoringProfileVersion || "Unavailable"}</dd></div>
  </dl>;
}

async function authenticatedRequest<T>(path: string, token: string, init?: RequestInit) {
  const headers = new Headers(init?.headers);
  if (token) headers.set("Authorization", `Bearer ${token}`);
  if (init?.body) headers.set("Content-Type", "application/json");
  return ruleRequest<T>(path, { ...init, headers, cache: "no-store" });
}

function merchantPolicyRequest<T>(resource: string, token: string, init?: RequestInit, adminMerchantId?: string) {
  if (adminMerchantId) {
    const path = resource === "interpret" ? "policy-interpret" : resource === "profile" ? "policy-profile" : resource.replace(/^rules/, "policies");
    return merchantDemoRequest<T>(`/${encodeURIComponent(adminMerchantId)}/${path}`, token, init);
  }
  const path = resource === "interpret" ? `${composer}/interpret` : `${policies}/${resource}`;
  return authenticatedRequest<T>(path, token, init);
}

export function RuleAccess({ scope }: { scope: Scope }) {
  const token = useRuleCredential(scope);
  const production = useProductionSession();
  const localAdmin = useLocalAdminPreview();
  const [value, setValue] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const request = useRef<AbortController>(null);
  const id = useId();
  const role = scope === "MERCHANT" ? "merchant" : "admin";
  const roleLabel = scope === "MERCHANT" ? "Merchant" : "Platform administrator";

  useEffect(() => () => request.current?.abort(), []);

  async function connect(event: FormEvent) {
    event.preventDefault();
    const candidate = value.trim();
    if (!candidate || request.current) return;
    const controller = new AbortController();
    request.current = controller;
    setBusy(true);
    setError("");
    try {
      const directory = await merchantDemoRequest<DemoDirectory>("", candidate, { signal: controller.signal });
      if (controller.signal.aborted) return;
      if (directory.role !== role) throw new Error(`Use a valid ${roleLabel.toLowerCase()} token for this workspace.`);
      setValue("");
      setMerchantDemoToken(candidate, role);
    } catch (cause) {
      if (!controller.signal.aborted) setError(errorText(cause));
    } finally {
      if (!controller.signal.aborted) setBusy(false);
      request.current = null;
    }
  }

  if (production || (scope === "PLATFORM" && localAdmin && !token)) return null;

  if (token) return <div className="flex flex-wrap items-center justify-between gap-2 text-sm">
    <p className="font-semibold text-emerald-800">{roleLabel} connected</p>
    <button type="button" className={outlineButton} onClick={() => setMerchantDemoToken("")}>Disconnect</button>
  </div>;

  return <form className={panel} aria-label={`Connect as ${roleLabel.toLowerCase()}`} onSubmit={connect}>
    <div>
      <label htmlFor={id} className="text-sm font-semibold">{roleLabel} token</label>
      <div className="mt-1.5 flex flex-wrap items-center gap-2">
        <input id={id} type="password" value={value} autoComplete="off" className="control max-w-sm"
          disabled={busy} onChange={event => setValue(event.currentTarget.value)} />
        <button type="submit" className={primaryButton} disabled={busy || !value.trim()}>{busy ? "Connecting…" : "Connect"}</button>
      </div>
    </div>
    {scope === "MERCHANT" && <p className="text-xs text-muted">Use the access token issued for your merchant.</p>}
    {error && <p role="alert" className="text-sm text-red-800">{error}</p>}
  </form>;
}

function legacyRuleDefinition(proposal: Proposal): RuleDefinition {
  if (proposal.scope === "PLATFORM") {
    const { name, description, stage, action, conditions } = proposal.rule;
    return { schemaVersion: 1, scope: "PLATFORM", name, description, stages: [stage], action,
      conditions: { logic: "AND", rules: conditions.map(({ signal, operator, value }) => ({ field: signal, operator, value })) } };
  }
  const { name, product_types, stages, action, conditions, durationMinutes, durationHours } = proposal;
  const duration = durationMinutes ? { value: durationMinutes, unit: "minute" as const }
    : durationHours ? { value: durationHours, unit: "hour" as const } : undefined;
  return { schemaVersion: 1, scope: "MERCHANT", name, productTypes: product_types, stages, action, duration,
    conditions: { logic: conditions.logic, rules: conditions.rules.map(condition =>
      condition.field === mailboxField ? { ...condition, value: null } : condition) } };
}

export function RulePreview({ proposal, definition, mailbox, profile }: {
  proposal?: Proposal; definition?: RuleDefinition; mailbox?: string; profile?: BusinessProfile | null;
}) {
  const rule = definition?.schemaVersion === 1 && (!proposal || definition.scope === proposal.scope)
    ? definition : proposal ? legacyRuleDefinition(proposal) : undefined;
  if (!rule) return null;
  const { name, action, stages: ruleStages, duration } = rule;
  const merchant = rule.scope === "MERCHANT";
  const conditions = rule.conditions.rules;
  const logic = rule.conditions.logic === "OR" ? "Any condition" : "All conditions";
  const hasMailbox = conditions.some(condition => condition.field === mailboxField);
  return <div className="space-y-4 rounded-lg border border-line bg-paper p-4">
    <dl className="grid gap-x-5 gap-y-3 text-sm sm:grid-cols-2">
      <div><dt className="text-muted">Rule name</dt><dd className="font-semibold wrap-anywhere">{name}</dd></div>
      <div><dt className="text-muted">Policy source</dt><dd className="font-semibold">{scopeLabels[rule.scope]}</dd></div>
      {rule.productTypes && <div><dt className="text-muted">Products</dt><dd>{rule.productTypes.map(product => products[product] ?? product).join(", ")}</dd></div>}
      <div><dt className="text-muted">When</dt><dd>{ruleStages.map(stage => stages[stage as keyof typeof stages] ?? stage).join(", ")}</dd></div>
      <div><dt className="text-muted">Action</dt><dd className="mt-1"><RuleActionBadge action={action} /></dd></div>
      {duration && <div><dt className="text-muted">Duration</dt><dd>{duration.value} {duration.unit}{duration.value === 1 ? "" : "s"} from activation</dd></div>}
      {hasMailbox && !duration && <div><dt className="text-muted">Duration</dt><dd>Until disabled</dd></div>}
      {rule.description && <div className="sm:col-span-2"><dt className="text-muted">Description</dt><dd>{rule.description}</dd></div>}
    </dl>
    <div>
      <p className="text-sm font-semibold">{logic}</p>
      <ul className="mt-1 list-disc space-y-1 pl-5 text-sm">
        {conditions.map((condition, index) => {
          const { field, operator } = condition;
          const label = conditionLabel(field, profile);
          const display = field === mailboxField ? mailbox ?? "the specified address" : conditionDisplayValue(condition);
          return <li key={index} className="wrap-anywhere">
            {label} {merchantOperatorLabels[operator] ?? operator} {display}
            {merchant && <span className="ml-2 rounded-md bg-white px-1.5 py-0.5 text-xs font-medium text-muted">
              {operator === "semantic_match" ? jevConditionLabel : "Checked by PayGuard"}
            </span>}
          </li>;
        })}
      </ul>
      {merchant && conditions.some(condition => condition.operator === "semantic_match") && <p className="mt-2 text-xs text-muted">
        Jev checks the listed transaction text fields for meaning. If a required condition remains unresolved, the transaction goes to manual review.
      </p>}
    </div>
    {!hasMailbox && <details>
      <summary className="cursor-pointer text-xs font-semibold text-accent">Rule data (JSON)</summary>
      <pre className="mt-2 max-h-64 overflow-auto rounded-md border border-line bg-white p-3 text-xs">{JSON.stringify(rule, null, 2)}</pre>
    </details>}
  </div>;
}

function RuleActionBadge({ action }: { action: string }) {
  return <span className={stateBadge(ruleActionTone(action))}>{policyActionLabels[action] ?? action}</span>;
}

function JevDraftReview({ review }: { review: NonNullable<Interpretation["jevReview"]> }) {
  const result = review.answer === "ALIGNED" ? "Aligned" : review.answer === "NEEDS_REVIEW"
    ? "Needs review" : "More information needed";
  const message = {
    LIVE: `Jev review · ${result}`,
    MOCKED: "Jev review · Mock mode",
    TIMEOUT: "Jev review timed out",
    UNAVAILABLE: "Jev review unavailable",
    NOT_RUN: "Jev review did not run",
  }[review.status];
  const reassuring = review.status === "LIVE" && review.answer === "ALIGNED";
  return <div role="status" className="space-y-1 text-xs text-muted">
    <span className={stateBadge(reassuring ? "info" : "warning")}>{message}</span>
    {review.model && <p>{review.model}{review.fallbackUsed ? " · Fallback model" : ""}</p>}
    {review.decisionId && <p className="break-all">Decision {review.decisionId}</p>}
  </div>;
}

export function PolicyExpiry({ expiresAt, status }: Pick<PolicyRule, "expiresAt" | "status">) {
  if (!expiresAt) return null;
  return <p className="mt-1 text-xs text-muted">
    {status === "EXPIRED" ? "Expired" : "Expires"} <time dateTime={expiresAt}>{merchantDate(expiresAt, true)}</time>
  </p>;
}

export function RuleComposer({ scope, onSaved }: {
  scope: Scope;
  onSaved?: () => void;
}) {
  const token = useRuleCredential(scope);
  const localAdmin = useLocalAdminPreview();
  const production = useProductionSession();
  const permitted = production?.role === (scope === "PLATFORM" ? "platform_admin" : "merchant");
  return (token || permitted || (scope === "PLATFORM" && localAdmin)) && <ConnectedComposer key={token}
    scope={scope} token={token} localAdmin={localAdmin} onSaved={onSaved} />;
}

function useMerchantRuleProfile(token: string, adminMerchantId?: string) {
  const [profile, setProfile] = useState<BusinessProfile | null>(null);
  const [profileError, setProfileError] = useState("");
  const [revision, setRevision] = useState(0);
  useEffect(() => {
    const controller = new AbortController();
    setProfileError("");
    void merchantPolicyRequest<BusinessProfile>("profile", token, { signal: controller.signal }, adminMerchantId)
      .then(result => { if (!controller.signal.aborted) setProfile(result); })
      .catch(cause => { if (!controller.signal.aborted) setProfileError(errorText(cause)); });
    return () => controller.abort();
  }, [token, revision, adminMerchantId]);
  return { profile, profileError, retryProfile: () => setRevision(value => value + 1) };
}

function ConnectedComposer({ scope, token, localAdmin = false, adminMerchantId, onSaved, edit, onCancel, merchantProfile }: {
  scope: Scope; token: string; localAdmin?: boolean; adminMerchantId?: string; onSaved?: () => void; edit?: PolicyRule; onCancel?: () => void;
  merchantProfile?: ReturnType<typeof useMerchantRuleProfile>;
}) {
  const exactEmailEdit = edit?.proposal.conditions.rules.some(condition => condition.field === mailboxField);
  const { profile, profileError, retryProfile } = merchantProfile ?? {};
  const [editText] = useState(() => {
    if (!edit || exactEmailEdit) return { text: "", reconstructed: false };
    if (edit.instruction && !containsMailbox(edit.instruction)) {
      return { text: edit.instruction, reconstructed: false };
    }
    return { text: describeSavedPolicy(edit.proposal, profile), reconstructed: true };
  });
  const [instruction, setInstruction] = useState(editText.text);
  const [showPriceUnitQuestion, setShowPriceUnitQuestion] = useState(false);
  const [draft, setDraft] = useState<DraftPreview | null>(null);
  const [pending, setPending] = useState<"interpret" | "save" | null>(null);
  const [error, setError] = useState("");
  const [saved, setSaved] = useState<PolicySave | PlatformSave | null>(null);
  const [runId, setRunId] = useState("");
  const [runs, setRuns] = useState<PreviewRun[]>([]);
  const [runError, setRunError] = useState("");
  const [showConditions, setShowConditions] = useState(Boolean(edit) && !exactEmailEdit);
  const [builderProposal, setBuilderProposal] = useState<MerchantProposal | undefined>(edit?.proposal);
  const [builderRevision, setBuilderRevision] = useState(0);
  const [semanticField, setSemanticField] = useState<string>();
  const request = useRef<AbortController>(null);
  const editVersion = useRef(edit?.version);
  const id = useId();
  const hasMailboxInstruction = scope === "MERCHANT" && containsMailbox(instruction);
  const canUseBuilder = scope === "MERCHANT" && !exactEmailEdit && !hasMailboxInstruction;
  const previewMailbox = hasMailboxInstruction ? maskMailboxInstruction(instruction)?.mailbox : undefined;
  const unchangedSummary = editText.reconstructed && instruction.trim() === editText.text;
  const needsProducts = profile?.products.length === 0;

  useEffect(() => {
    const controller = new AbortController();
    const init = { signal: controller.signal };
    if (scope === "PLATFORM") {
      void authenticatedRequest<RuleCatalogue>(adminRulePath(rulesEndpoint, localAdmin, token), token, init).then(result => {
        if (!controller.signal.aborted) setRuns(result.runs ?? []);
      }).catch(cause => {
        if (!controller.signal.aborted) setRunError(errorText(cause));
      });
    }
    return () => {
      controller.abort();
      request.current?.abort();
    };
  }, [scope, token, localAdmin]);

  async function interpret(event: FormEvent) {
    event.preventDefault();
    await draftInstruction(instruction);
  }

  async function draftInstruction(text: string) {
    if (request.current || !text.trim() || (editText.reconstructed && text.trim() === editText.text) ||
        (scope === "MERCHANT" && (!profile || needsProducts))) return;
    const controller = new AbortController();
    request.current = controller;
    setPending("interpret");
    setDraft(null);
    setSaved(null);
    setError("");
    setShowPriceUnitQuestion(false);
    const containsAddress = scope === "MERCHANT" && containsMailbox(text);
    try {
      const payload: { scope: Scope; instruction: string; mailboxDigest?: string; requireAI: true; reviewWithJev: true } =
        { scope, instruction: text.trim(), requireAI: true, reviewWithJev: true };
      if (containsAddress) {
        if (!profile?.merchantId) throw new Error("The merchant profile is unavailable. Reconnect and try again.");
        const masked = maskMailboxInstruction(text);
        if (!masked) throw new Error("Enter one complete email address per rule.");
        payload.instruction = masked.instruction;
        payload.mailboxDigest = await merchantMailboxDigest(profile.merchantId, masked.mailbox);
      }
      if (controller.signal.aborted) return;
      const init = { method: "POST", body: JSON.stringify(payload), signal: controller.signal };
      const result = scope === "MERCHANT"
        ? await merchantPolicyRequest<Interpretation>("interpret", token, init, adminMerchantId)
        : await authenticatedRequest<Interpretation>(adminRulePath(`${composer}/interpret`, localAdmin, token), token, init);
      if (controller.signal.aborted) return;
      if (result.source !== "openai") throw new Error("This server did not generate the draft with OpenAI. Try again after updating the Rule Composer.");
      if (containsAddress && result.status === "CLARIFICATION" &&
          (result.clarificationCode === "AMOUNT_UNIT_REQUIRED" ||
            (!result.clarificationCode && needsMailboxPriceUnit(payload.instruction)))) {
        setShowPriceUnitQuestion(true);
        setDraft({ ...result, message: undefined });
        return;
      }
      if ((result.proposal && result.proposal.scope !== scope) || (result.definition && result.definition.scope !== scope)) {
        throw new Error("The proposal belongs to another rule system.");
      }
      if (result.draftProposal && (scope !== "MERCHANT" || result.draftProposal.scope !== scope ||
          !result.draftProposal.conditions.rules.some(condition => condition.field === mailboxField))) {
        throw new Error("The pending draft is missing the exact email condition. Draft the rule again.");
      }
      if (containsAddress && result.status === "READY" && (result.proposal?.scope !== "MERCHANT" ||
          !result.proposal.conditions.rules.some(condition => condition.field === mailboxField))) {
        throw new Error("The draft is missing the exact email condition. Try drafting the rule again.");
      }
      const sourceInstruction = result.proposal?.scope === "MERCHANT" && !containsAddress &&
        !result.proposal.conditions.rules.some(condition => condition.field === mailboxField)
        ? text.trim() : undefined;
      setDraft({ ...result, sourceInstruction });
      if (result.proposal?.scope === "MERCHANT" && !result.proposal.conditions.rules.some(condition => condition.field === mailboxField)) {
        setBuilderProposal(result.proposal);
        setBuilderRevision(value => value + 1);
        setSemanticField(undefined);
      }
    } catch (cause) {
      if (!controller.signal.aborted) {
        if (canUseBuilder && !containsAddress) setShowConditions(true);
        setError(errorText(cause));
      }
    } finally {
      if (!controller.signal.aborted) setPending(null);
      request.current = null;
    }
  }

  function choosePriceUnit(unit: "THB" | "minor units") {
    const updated = applyMailboxPriceUnit(instruction, unit);
    if (!updated) return;
    setInstruction(updated);
    void draftInstruction(updated);
  }

  const ready = draft?.status === "READY" && draft.validation.valid && Boolean(draft.proposal);
  const pendingProposal = draft?.status === "CLARIFICATION" && draft.clarificationCode === "IP_COUNTRY_UNAVAILABLE"
    ? draft.draftProposal : undefined;

  async function save() {
    const proposal = ready ? draft?.proposal : pendingProposal;
    if (request.current || !proposal) return;
    const controller = new AbortController();
    request.current = controller;
    setPending("save");
    setError("");
    try {
      const payload = proposal.scope === "MERCHANT" ? {
        proposal,
        expectedVersion: editVersion.current,
        instruction: draft?.sourceInstruction,
        saveAsDraft: pendingProposal ? true : undefined,
      } : { proposal, runId: runId || undefined };
      const init = { method: edit ? "PUT" : "POST", body: JSON.stringify(payload), signal: controller.signal };
      const result = proposal.scope === "MERCHANT"
        ? await merchantPolicyRequest<PolicySave>(`rules${edit ? `/${encodeURIComponent(edit.id)}` : ""}`, token, init, adminMerchantId)
        : await authenticatedRequest<PlatformSave>(adminRulePath(`${composer}/platform/save`, localAdmin, token), token, init);
      if (controller.signal.aborted) return;
      if (edit && "version" in result.rule) editVersion.current = result.rule.version;
      setSaved(result);
      onSaved?.();
    } catch (cause) {
      if (!controller.signal.aborted) setError(errorText(cause));
    } finally {
      if (!controller.signal.aborted) setPending(null);
      request.current = null;
    }
  }

  const savedForSetup = saved?.status === "PENDING_APPROVAL" && Boolean(saved.rule.preview.missingSignals?.length);
  const reason = saved?.status === "DRAFT"
    ? "Platform proposals are saved as drafts for review."
    : savedForSetup ? "Saved as a draft. Connect IP country, then activate it from Saved rules."
    : saved?.status === "PENDING_APPROVAL" ? "Review the validation evidence before activating this version." : "";
  const preview = saved && "preview" in saved.rule ? saved.rule.preview : saved && "preview" in saved ? saved.preview : undefined;
  const messages = [...(draft?.validation.errors ?? []), ...(draft?.validation.warnings ?? [])];
  const suggestedField = draft?.semanticDraft?.field;

  function resetDraft() {
    setDraft(null);
    setSaved(null);
    setError("");
    setShowPriceUnitQuestion(false);
  }

  function startJevCondition(field: string) {
    setBuilderProposal(undefined);
    setBuilderRevision(value => value + 1);
    setSemanticField(field);
    setShowConditions(true);
    setDraft(null);
  }

  return <section className={panel} aria-labelledby={`${id}-title`}>
    <div>
      <h2 id={`${id}-title`} className="text-lg font-semibold">{scope === "MERCHANT" ? "AI Rule" : "AI rule composer"}</h2>
      <p className="mt-1 text-sm text-muted">Describe what to check and what action to take in Thai or English. Review the draft before saving.</p>
    </div>
    {scope === "MERCHANT" && !profile && !profileError && <p role="status" className="text-sm text-muted">Loading available rule fields…</p>}
    {profileError && <div className="space-y-2">
      <p role="alert" className="text-sm text-red-800">Could not load the merchant’s rule fields. {profileError}</p>
      <button type="button" className={outlineButton} onClick={retryProfile}>Retry loading rule fields</button>
    </div>}
    {profile && <details className="text-sm text-muted">
      <summary className="cursor-pointer font-semibold text-ink">{profile.businessName} business profile</summary>
      <ProfileDetails profile={profile} />
    </details>}
    {needsProducts && <p className="text-sm text-muted">
      Add a product to the merchant profile to draft a rule.{" "}
      {adminMerchantId && <Link className="font-semibold text-accent hover:underline" href={merchantDetailsHref(adminMerchantId)}>Edit merchant details</Link>}
    </p>}
    {edit && <div className="flex flex-wrap items-center justify-between gap-2 rounded-lg bg-paper p-3 text-sm">
      <p>New version of <strong>{edit.proposal.name}</strong></p>
      <button type="button" className={outlineButton} disabled={pending !== null} onClick={onCancel}>{saved ? "Done" : "Cancel edit"}</button>
    </div>}
    <form className="space-y-2.5" onSubmit={interpret}>
      <label htmlFor={`${id}-instruction`} className="block text-sm font-semibold">Describe your rule</label>
      <textarea id={`${id}-instruction`} rows={3} maxLength={2000} className="control min-h-24 resize-y"
        aria-describedby={`${id}-instruction-help`} placeholder={exactEmailEdit ? mailboxExample : prompts[scope]} value={instruction} disabled={pending !== null} onChange={event => {
          setInstruction(event.currentTarget.value);
          resetDraft();
        }} />
      <p id={`${id}-instruction-help`} className="text-xs text-muted">
        {editText.reconstructed && <span className="mb-1 block">This text describes the saved conditions. Change it to generate a preview, or use Rule fields to edit the conditions directly.</span>}
        {exactEmailEdit && <span className="mb-1 block">Re-enter the email address and the conditions for the new version.</span>}
        {scope === "MERCHANT" ? <>Use bare domains for domain rules. You can combine an exact email address with a product and price, or add a duration for a temporary block. Full email addresses stay in PayGuard.</>
          : "Use policy conditions and domains. Leave out customer identities and card details."}
      </p>
      <button type="submit" className={ready ? outlineButton : primaryButton}
        disabled={!instruction.trim() || unchangedSummary || pending !== null || (scope === "MERCHANT" && (!profile || needsProducts))}>
        <Sparkles className="size-4 shrink-0" aria-hidden="true" />
        {pending === "interpret" ? "Drafting rule…" : "Draft rule"}
      </button>
    </form>
    {showPriceUnitQuestion && <div className="space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
      <p role="status">What unit is the price in?</p>
      {applyMailboxPriceUnit(instruction, "THB") ? <div className="flex flex-wrap gap-2">
        <button type="button" className={primaryButton} onClick={() => choosePriceUnit("THB")}>Baht (THB)</button>
        <button type="button" className={outlineButton} onClick={() => choosePriceUnit("minor units")}>Minor units (satang)</button>
      </div> : <p>Add a unit next to each amount in the instruction, then draft the rule again.</p>}
    </div>}
    {error && <p role="alert" className="rounded-lg bg-red-50 p-3 text-sm text-red-800">{error}</p>}
    {draft?.source && <div role="status">
      <span className={stateBadge(draft.source === "openai" ? "info" : draft.source === "local_fallback" ? "warning" : "neutral")}>
        {{ openai: draft.model ? `OpenAI · ${draft.model}` : "OpenAI", local: "Processed in PayGuard",
          local_fallback: "PayGuard fallback", manual: "Manual preview" }[draft.source]}
      </span>
    </div>}
    {draft?.jevReview && <JevDraftReview review={draft.jevReview} />}
    {suggestedField && canUseBuilder && profile && <div className="space-y-2 rounded-lg bg-paper p-3 text-sm">
      {draft?.message && <p role="status">{draft.message}</p>}
      <button className={outlineButton} type="button" disabled={pending !== null}
        onClick={() => startJevCondition(suggestedField)}>Set up Jev condition</button>
    </div>}
    {canUseBuilder && profile && <details className="rounded-lg border border-line p-3" open={showConditions}
      onToggle={event => setShowConditions(event.currentTarget.open)}>
      <summary className="cursor-pointer text-sm font-semibold">Rule fields</summary>
      <div className="mt-3 space-y-3">
        {semanticField && <p className="text-xs text-muted">
          Enter the meaning Jev should look for. Add any other conditions from your instruction before reviewing.
        </p>}
        <StructuredRuleBuilder key={builderRevision} profile={profile} initial={builderProposal} semanticField={semanticField}
          disabled={pending !== null} onChange={resetDraft}
          onPreview={proposal => {
            setSaved(null);
            setError("");
            setDraft({ status: "READY", source: "manual", proposal, validation: { valid: true } });
          }} />
      </div>
    </details>}
    {draft && (!suggestedField || !canUseBuilder) && <div className="space-y-3">
      {draft.status === "CLARIFICATION" && draft.clarificationCode === "IP_COUNTRY_UNAVAILABLE" ?
        <div className="space-y-2 rounded-lg border border-amber-200 bg-amber-50 p-3 text-sm text-amber-950">
          <p role="status">{draft.message}</p>
          {adminMerchantId && <Link className="font-semibold text-accent hover:underline"
            href={merchantDetailsHref(adminMerchantId)} target="_blank" rel="noopener">
            Open merchant setup in a new tab
          </Link>}
        </div> : draft.message && <p role="status" className="text-sm text-muted">{draft.message}</p>}
      {messages.length > 0 && <ul className="list-disc space-y-1 pl-5 text-sm text-amber-900">
        {messages.map((message, index) => <li key={index}>{message}</li>)}
      </ul>}
      {(draft.proposal || draft.definition) && <RulePreview
        proposal={draft.proposal} definition={draft.definition} mailbox={previewMailbox} profile={profile} />}
      {(ready || pendingProposal) && !saved && <>
        {pendingProposal && <p className="text-sm text-muted">
          Save this rule as a draft now. Connect IP country before activating it.
        </p>}
        {scope === "PLATFORM" && runs.length > 0 && <label className="block text-sm font-semibold">Simulation run for replay (optional)
          <select className="control mt-1 max-w-md" value={runId} onChange={event => setRunId(event.currentTarget.value)} disabled={pending !== null}>
            <option value="">Save as draft without a replay</option>
            {runs.map(run => <option key={run.id} value={run.id}>{products[run.product] ?? run.product} · Seed {run.seed} · {run.id.slice(0, 8)}</option>)}
          </select>
        </label>}
        {scope === "PLATFORM" && runError && <p className="text-sm text-muted">Simulation runs could not be loaded. {runError}</p>}
        <button type="button" className={primaryButton} disabled={pending !== null} onClick={() => void save()}>
          <Save className="size-4 shrink-0" aria-hidden="true" />
          {pending === "save" ? "Saving…" : scope === "PLATFORM" ? "Save draft" : "Save rule"}
        </button>
      </>}
    </div>}
    {saved && <div role="status" className={`rounded-lg border p-3 text-sm ${saved.status === "PENDING_APPROVAL"
      ? "border-amber-200 bg-amber-50 text-amber-950" : "border-sky-200 bg-sky-50 text-sky-950"}`}>
      <div className="flex flex-wrap items-center gap-2">
        <p className="font-semibold">Rule saved</p>
        <span className={stateBadge(statusTone[saved.status])}>{savedForSetup ? "Draft" : statusLabels[saved.status]}</span>
      </div>
      {reason && <p className="mt-1">{reason}</p>}
      {"expiresAt" in saved.rule && <PolicyExpiry expiresAt={saved.rule.expiresAt} status={saved.rule.status} />}
      {preview && ("evaluated" in preview ? <PolicySample preview={preview} />
        : <p className="mt-1">{preview.matched} matched out of {preview.eligible} eligible events · {preview.skipped} with missing signals</p>)}
    </div>}
  </section>;
}

type RuleHistory = { rule: PolicyRule; versions: PolicyRule[] };

function ConnectedPolicyRules({ token, adminMerchantId, profile, refreshKey, onEdit, onDelete, onRefreshProfile }: {
  token: string; adminMerchantId?: string; profile: BusinessProfile | null; refreshKey: number;
  onEdit: (rule: PolicyRule) => void; onDelete: (ruleId: string) => void; onRefreshProfile: () => void;
}) {
  const [rules, setRules] = useState<PolicyRule[]>([]);
  const [error, setError] = useState("");
  const [revision, setRevision] = useState(0);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState("");
  const [notice, setNotice] = useState("");
  const [history, setHistory] = useState<RuleHistory | null>(null);
  const [simulation, setSimulation] = useState<{ id: string; preview: PolicyPreview } | null>(null);
  const [confirmDelete, setConfirmDelete] = useState("");
  const [legacy, setLegacy] = useState<LegacyJevRules>();
  const [legacyError, setLegacyError] = useState("");
  const [legacyLoading, setLegacyLoading] = useState(false);
  const lifecycle = useRef<AbortController>(null);
  const scopedMerchantId = adminMerchantId ?? profile?.merchantId;
  const legacyPath = scopedMerchantId ? `/${encodeURIComponent(scopedMerchantId)}/jev/rules` : "";

  useEffect(() => () => lifecycle.current?.abort(), []);
  useEffect(() => {
    const controller = new AbortController();
    setLoading(true);
    setError("");
    void merchantPolicyRequest<{ rules: PolicyRule[] }>("rules", token, { signal: controller.signal }, adminMerchantId)
      .then(result => { if (!controller.signal.aborted) setRules(result.rules ?? []); })
      .catch(cause => { if (!controller.signal.aborted) setError(errorText(cause)); })
      .finally(() => { if (!controller.signal.aborted) setLoading(false); });
    return () => controller.abort();
  }, [token, adminMerchantId, refreshKey, revision]);

  useEffect(() => {
    if (!legacyPath) return;
    const controller = new AbortController();
    setLegacyLoading(true);
    setLegacyError("");
    void merchantDemoRequest<LegacyJevRules>(legacyPath, token, { signal: controller.signal })
      .then(result => { if (!controller.signal.aborted) setLegacy(result); })
      .catch(cause => { if (!controller.signal.aborted) setLegacyError(errorText(cause)); })
      .finally(() => { if (!controller.signal.aborted) setLegacyLoading(false); });
    return () => controller.abort();
  }, [legacyPath, token, revision]);

  async function changeLegacy(id: string, proposal?: LegacyJevProposal) {
    if (!legacy || !legacyPath || lifecycle.current) return false;
    const controller = new AbortController();
    lifecycle.current = controller;
    setBusy(id);
    setLegacyError("");
    setNotice("");
    try {
      const result = await merchantDemoRequest<LegacyJevRules>(`${legacyPath}/${encodeURIComponent(id)}`, token, {
        method: proposal ? "PUT" : "DELETE",
        body: JSON.stringify({ expectedVersion: legacy.version, ...(proposal ? { definition: proposal } : {}) }),
        signal: controller.signal,
      });
      if (controller.signal.aborted) return false;
      setLegacy(result);
      setNotice(proposal ? "Rule updated." : "Rule deleted.");
      return true;
    } catch (cause) {
      if (!controller.signal.aborted) setLegacyError(errorText(cause));
      return false;
    } finally {
      if (!controller.signal.aborted) setBusy("");
      lifecycle.current = null;
    }
  }

  async function change(rule: PolicyRule, operation: "disable" | "activate" | "rollback" | "simulate" | "history" | "delete", version?: number) {
    if (lifecycle.current) return;
    const controller = new AbortController();
    lifecycle.current = controller;
    setBusy(rule.id);
    setError("");
    setNotice("");
    const path = `rules/${encodeURIComponent(rule.id)}`;
    const { signal } = controller;
    try {
      if (operation === "history") {
        const result = await merchantPolicyRequest<RuleHistory>(path, token, { signal }, adminMerchantId);
        if (!signal.aborted) setHistory(result);
      } else if (operation === "simulate") {
        const result = await merchantPolicyRequest<{ preview: PolicyPreview }>(`${path}/simulate`, token, { method: "POST", signal }, adminMerchantId);
        if (!signal.aborted) setSimulation({ id: rule.id, preview: result.preview });
      } else if (operation === "delete") {
        await merchantPolicyRequest<unknown>(path, token, {
          method: "DELETE", body: JSON.stringify({ expectedVersion: rule.version }), signal,
        }, adminMerchantId);
        if (signal.aborted) return;
        setConfirmDelete("");
        setHistory(null);
        setSimulation(null);
        setNotice(`Deleted ${rule.proposal.name}.`);
        setRevision(value => value + 1);
        onDelete(rule.id);
      } else {
        const result = await merchantPolicyRequest<PolicySave>(`${path}/${operation}`, token, {
          method: "POST", body: JSON.stringify({ version, expectedVersion: rule.version }), signal,
        }, adminMerchantId);
        if (signal.aborted) return;
        setHistory(null);
        setSimulation(null);
        setNotice(`${rule.proposal.name} · ${statusLabels[result.status]}`);
        setRevision(value => value + 1);
      }
    } catch (cause) {
      if (!signal.aborted) setError(errorText(cause));
    } finally {
      if (!signal.aborted) setBusy("");
      lifecycle.current = null;
    }
  }

  const activeCount = rules.filter(rule => rule.status === "ACTIVE" || rule.activeVersion).length;
  return <section className={panel} aria-labelledby="merchant-policy-list">
    <div className="flex items-center justify-between gap-2">
      <div>
        <h2 id="merchant-policy-list" className="text-lg font-semibold">Saved rules</h2>
        {!loading && rules.length > 0 && <p className="mt-1 text-sm text-muted">
          {activeCount} active {activeCount === 1 ? "policy" : "policies"}
        </p>}
      </div>
      <button type="button" className={outlineButton} disabled={loading || legacyLoading || !!busy}
        onClick={() => { setRevision(value => value + 1); onRefreshProfile(); }}>Refresh rules</button>
    </div>
    {(loading || legacyLoading) && <p role="status" className="text-sm text-muted">Loading rules…</p>}
    {error && <p role="alert" className="text-sm text-red-800">{error}</p>}
    {legacyError && <p role="alert" className="text-sm text-red-800">Could not load or save booking review rules. {legacyError}</p>}
    {notice && <p role="status" className="text-sm text-emerald-900">{notice}</p>}
    {!loading && !legacyLoading && !error && !legacyError && !rules.length && !legacy?.rules.length && <p className="text-sm text-muted">No saved rules.</p>}
    {(rules.length > 0 || !!legacy?.rules.length) && <ul className="divide-y divide-line">
      {rules.map(rule => {
        const savedDraft = rule.status === "PENDING_APPROVAL" && rule.preview?.missingSignals?.includes(ipCountryField);
        const awaitingIPCountry = savedDraft && !profile?.fieldRegistry?.some(field =>
          field.fieldName === ipCountryField && field.dataType === "country" &&
          rule.proposal.product_types.every(product => field.supportedProductTypes.includes(product)) &&
          rule.proposal.stages.every(stage => field.availableStages.includes(stage)));
        return <li className="space-y-3 py-3" key={rule.id}>
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="min-w-0">
            <p className="text-sm font-semibold wrap-anywhere">{rule.proposal.name}</p>
            <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
              <span className={stateBadge(statusTone[rule.status])}>{savedDraft ? "Draft" : statusLabels[rule.status]}</span>
              <RuleActionBadge action={rule.proposal.action} />
              <span className="text-xs text-muted">Version {rule.version}</span>
              {rule.proposal.conditions.rules.some(condition => condition.operator === "semantic_match") &&
                <span className={stateBadge("info")}>{jevConditionLabel}</span>}
              {rule.activeVersion && rule.activeVersion !== rule.version &&
                <span className="text-xs text-muted">Version {rule.activeVersion} remains active</span>}
            </div>
            <PolicyExpiry expiresAt={rule.expiresAt} status={rule.status} />
            {rule.preview && <PolicySample preview={rule.preview} />}
            {awaitingIPCountry && <p className="mt-2 text-sm text-amber-950">Connect IP country, then refresh to activate this draft.</p>}
            {rule.status === "PENDING_APPROVAL" && Boolean(rule.preview?.conflictRuleIds?.length) &&
              <p className="mt-2 rounded-md border border-amber-200 bg-amber-50 px-2.5 py-2 text-sm text-amber-950">
                Multiple policies can match after activation. Block takes priority over Review, then Challenge.
              </p>}
          </div>
          <div className="flex flex-wrap gap-2">
            <button type="button" className={outlineButton} disabled={Boolean(busy)} onClick={() => {
              onEdit(rule);
              document.getElementById("merchant-policy-editor")?.focus();
            }}><Pencil className="size-4 shrink-0" aria-hidden="true" />Edit</button>
            <button type="button" className={outlineButton} disabled={Boolean(busy) || awaitingIPCountry} onClick={() => void change(rule, "simulate")}>
              <FlaskConical className="size-4 shrink-0" aria-hidden="true" />Test rule
            </button>
            <button type="button" className={outlineButton} disabled={Boolean(busy)} onClick={() => {
              if (history?.rule.id === rule.id) setHistory(null);
              else void change(rule, "history");
            }}><History className="size-4 shrink-0" aria-hidden="true" />{history?.rule.id === rule.id ? "Hide versions" : "Versions"}</button>
            {rule.status === "PENDING_APPROVAL" && <button type="button" className={actionButton.positive} disabled={Boolean(busy) || awaitingIPCountry}
              onClick={() => void change(rule, "activate")}><Play className="size-4 shrink-0" aria-hidden="true" />Activate</button>}
            {(rule.status === "ACTIVE" || rule.activeVersion) && <button type="button" className={actionButton.warning} disabled={Boolean(busy)}
              onClick={() => void change(rule, "disable")}><Pause className="size-4 shrink-0" aria-hidden="true" />Disable</button>}
            <button type="button" className={actionButton.dangerOutline} disabled={Boolean(busy) || confirmDelete === rule.id}
              onClick={() => setConfirmDelete(rule.id)}><Trash2 className="size-4 shrink-0" aria-hidden="true" />Delete</button>
          </div>
        </div>
        {confirmDelete === rule.id && <div className="space-y-2 rounded-md border border-red-200 bg-red-50 p-3" aria-label="Confirm rule deletion">
          <p className="text-sm text-red-900">Delete <strong>{rule.proposal.name}</strong> from future decisions? Saved transactions retain their rule versions.</p>
          <div className="flex flex-wrap gap-2">
            <button type="button" className={actionButton.danger}
              disabled={Boolean(busy)} onClick={() => void change(rule, "delete")}>
              <Trash2 className="size-4 shrink-0" aria-hidden="true" />Confirm deletion
            </button>
            <button type="button" className={outlineButton} disabled={Boolean(busy)} onClick={() => setConfirmDelete("")}>Cancel</button>
          </div>
        </div>}
        {busy === rule.id && <p role="status" className="text-xs text-muted">Working…</p>}
        {simulation?.id === rule.id && <div role="status" className="rounded-md bg-paper p-3">
          <p className="text-sm font-semibold">Test result</p>
          <PolicySample preview={simulation.preview} />
        </div>}
        {history?.rule.id === rule.id && <ul className="space-y-2 rounded-md border border-line bg-paper p-3">
          {history.versions.map(version => <li key={version.version} className="min-w-0 rounded-md border border-line bg-white p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <details className="min-w-0">
                <summary className="cursor-pointer text-sm font-semibold wrap-anywhere">
                  Version {version.version} · {statusLabels[version.status]} · {version.proposal.name}
                </summary>
                <PolicyExpiry expiresAt={version.expiresAt} status={version.status} />
                <div className="mt-2"><RulePreview proposal={version.proposal} profile={profile} /></div>
              </details>
              {version.version !== rule.version && <button type="button" className={actionButton.warning}
                disabled={Boolean(busy)} onClick={() => void change(rule, "rollback", version.version)}>
                <RotateCcw className="size-4 shrink-0" aria-hidden="true" />Restore version {version.version}
              </button>}
            </div>
          </li>)}
        </ul>}
      </li>;
      })}
      {legacy?.rules.map(rule => <LegacyJevRuleItem key={rule.id} rule={rule} version={legacy.version} busy={!!busy || legacyLoading}
        onChange={proposal => changeLegacy(rule.id, proposal)} />)}
    </ul>}
  </section>;
}

function PolicySample({ preview }: { preview: PolicyPreview }) {
  const overlap = preview.conflictRuleIds?.length ?? 0;
  return <div className="mt-1 text-xs text-muted">
    <p>
      {preview.matched} of {preview.evaluated} matched · {preview.missing} with missing signals
      {!!preview.requiresJev && ` · ${preview.requiresJev} require Jev at transaction time`}
      {overlap > 0 && ` · Potential overlap with ${overlap} other ${overlap === 1 ? "policy" : "policies"} at preview`}
    </p>
    {!overlap && preview.approvalReason && <p className="mt-1">{preview.approvalReason}</p>}
  </div>;
}

function MerchantPolicyWorkspace({ token, adminMerchantId }: { token: string; adminMerchantId?: string }) {
  const [refreshKey, setRefreshKey] = useState(0);
  const [edit, setEdit] = useState<PolicyRule>();
  const merchantProfile = useMerchantRuleProfile(token, adminMerchantId);
  return <>
    <div id="merchant-policy-editor" tabIndex={-1}>
      <ConnectedComposer key={`${edit?.id ?? ""}:${edit?.version ?? ""}`} scope="MERCHANT" token={token} adminMerchantId={adminMerchantId}
        merchantProfile={merchantProfile} edit={edit} onCancel={() => setEdit(undefined)} onSaved={() => setRefreshKey(value => value + 1)} />
    </div>
    <ConnectedPolicyRules token={token} adminMerchantId={adminMerchantId} profile={merchantProfile.profile} refreshKey={refreshKey}
      onEdit={setEdit} onRefreshProfile={merchantProfile.retryProfile}
      onDelete={id => setEdit(current => current?.id === id ? undefined : current)} />
  </>;
}

export function AdminMerchantPolicyWorkspace({ merchantId, token }: { merchantId: string; token: string }) {
  return <MerchantPolicyWorkspace key={`${merchantId}:${token}`} token={token} adminMerchantId={merchantId} />;
}

export function MerchantRulesWorkspace() {
  const token = useRuleCredential("MERCHANT");
  const production = useProductionSession();
  return <main className="mx-auto max-w-[1050px] space-y-5 px-4 pt-7 pb-12 sm:px-7 sm:pt-9 lg:px-10">
    <header>
      <h1 className="text-[30px] font-semibold tracking-tight sm:text-[36px]">Merchant AI Rules</h1>
    </header>
    <RuleAccess scope="MERCHANT" />
    {(token || production?.role === "merchant") && <MerchantPolicyWorkspace key={token} token={token} />}
    <div className="flex flex-wrap gap-4 text-sm font-semibold text-accent">
      <Link href="/settings/merchants?view=transactions" className="hover:underline">Open transactions</Link>
    </div>
  </main>;
}

export function SettingsWorkspace({ children }: { children: ReactNode }) {
  const merchant = useWorkspaceView() === "merchant";
  const production = useProductionSession();
  const token = useRuleCredential("MERCHANT");
  if (!merchant) return children;
  return <div className="mt-6 space-y-4">
    <RuleAccess scope="MERCHANT" />
    {(token || production?.role === "merchant") && <MerchantRiskProtection key={token} token={token} />}
  </div>;
}

function MerchantRiskProtection({ token }: { token: string }) {
  const [profile, setProfile] = useState<BusinessProfile | null>(null);
  const [error, setError] = useState("");
  useEffect(() => {
    const controller = new AbortController();
    void authenticatedRequest<BusinessProfile>(`${policies}/profile`, token, { signal: controller.signal })
      .then(result => { if (!controller.signal.aborted) setProfile(result); })
      .catch(cause => { if (!controller.signal.aborted) setError(errorText(cause)); });
    return () => controller.abort();
  }, [token]);
  return <section className={panel} aria-labelledby="merchant-business-profile">
    <h2 id="merchant-business-profile" className="text-lg font-semibold">Business Profile</h2>
    {error && <p role="alert" className="text-sm text-red-800">{error}</p>}
    {!error && !profile && <p role="status" className="text-sm text-muted">Loading your profile…</p>}
    {profile && <>
      <p className="font-semibold">{profile.businessName}</p>
      <div>
        <h3 className="text-sm font-semibold">Risk Protection</h3>
        <p className="mt-1 text-sm text-muted">{profile.scoringProfileVersion ? "Scoring profile active" : "No active scoring profile is recorded."}</p>
        <ProfileDetails profile={profile} />
      </div>
    </>}
  </section>;
}
