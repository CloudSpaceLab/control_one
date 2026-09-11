import {
  useCallback,
  useEffect,
  useMemo,
  useState,
  type FormEvent,
} from "react";
import type { ColumnDef } from "@tanstack/react-table";
import {
  Check,
  DatabaseZap,
  Download,
  RefreshCw,
  Save,
  ShieldAlert,
  ShieldCheck,
  ShieldX,
} from "lucide-react";
import { toast } from "sonner";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { ConfirmModal } from "@/components/ConfirmModal";
import {
  DataTable,
  EmptyState,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from "@/components/kit";
import { useApiClient } from "@/hooks/useApiClient";
import { useAuth } from "@/providers/AuthProvider";
import { useTenant } from "@/providers/TenantProvider";
import { saveBlob } from "@/lib/download";
import type {
  ContentPackEdgeCollector,
  ContentPackOTelConfigCandidate,
  ContentPackOTelConfigCandidateDetail,
  ContentPackSourceHealth,
  ContentPackSourceHealthResponse,
  ContentPackSourceHealthInvestigationCase,
  ContentPackSourceProposal,
  ContentPackSourceProposalCollectMode,
  ContentPackSourceProposalSummary,
  PaginationMeta,
  SOCCase,
  TenantConnectorPolicy,
} from "@/lib/api";

type ProposalStatusFilter =
  | "all"
  | "approval_required"
  | "auto_eligible"
  | "approved"
  | "rejected"
  | "privacy_blocked";

type SourceHealthStateFilter =
  | "all"
  | "proposed"
  | "approval_required"
  | "approved"
  | "config_rendered"
  | "deployed"
  | "collecting"
  | "parser_healthy"
  | "parser_failed"
  | "silent"
  | "backpressured"
  | "unsupported"
  | "privacy_blocked"
  | "stale";

interface PolicyDraft {
  allowMediumRisk: boolean;
  allowHighRisk: boolean;
  autoConnectPrograms: string;
  approvalRequiredPrograms: string;
  blockedPrograms: string;
}

interface ConfirmState {
  kind: "approve" | "reject" | "privacy_block";
  proposal: ContentPackSourceProposal;
}

interface DeploymentDraft {
  endpoint: string;
  collectorId: string;
}

const COLLECT_MODE_LABELS: Record<
  ContentPackSourceProposalCollectMode,
  string
> = {
  collect_raw: "Collect raw logs",
  collect_parsed: "Collect parsed only",
  metadata_only: "Metadata only",
  observe_only: "Observe only",
  disabled: "Disabled",
};

const SOURCE_PROPOSAL_PAGE_LIMIT = 100;
const SOURCE_HEALTH_PAGE_LIMIT = 100;
const SOURCE_HEALTH_CASE_LIMIT = 5;

const SOURCE_HEALTH_STATE_OPTIONS: Array<{
  value: SourceHealthStateFilter;
  label: string;
}> = [
  { value: "all", label: "All states" },
  { value: "approval_required", label: "Approval required" },
  { value: "collecting", label: "Collecting" },
  { value: "parser_healthy", label: "Parser healthy" },
  { value: "parser_failed", label: "Parser failed" },
  { value: "silent", label: "Silent" },
  { value: "backpressured", label: "Backpressured" },
  { value: "stale", label: "Stale" },
  { value: "approved", label: "Approved" },
  { value: "config_rendered", label: "Config rendered" },
  { value: "deployed", label: "Deployed" },
  { value: "proposed", label: "Proposed" },
  { value: "privacy_blocked", label: "Privacy blocked" },
  { value: "unsupported", label: "Unsupported" },
];

const EMPTY_POLICY_DRAFT: PolicyDraft = {
  allowMediumRisk: false,
  allowHighRisk: false,
  autoConnectPrograms: "",
  approvalRequiredPrograms: "",
  blockedPrograms: "",
};

function policyToDraft(policy: TenantConnectorPolicy | null): PolicyDraft {
  if (!policy) return EMPTY_POLICY_DRAFT;
  return {
    allowMediumRisk: policy.allow_medium_risk,
    allowHighRisk: policy.allow_high_risk,
    autoConnectPrograms: (policy.auto_connect_programs ?? []).join(", "),
    approvalRequiredPrograms: (policy.approval_required_programs ?? []).join(
      ", ",
    ),
    blockedPrograms: (policy.blocked_programs ?? []).join(", "),
  };
}

function splitProgramList(value: string): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of value.split(",")) {
    const normalized = item.trim().toLowerCase();
    if (!normalized || seen.has(normalized)) continue;
    seen.add(normalized);
    out.push(normalized);
  }
  return out;
}

function statusTone(status?: string): StateTone {
  switch ((status ?? "").toLowerCase()) {
    case "approved":
    case "collecting":
    case "deployed":
    case "parser_healthy":
      return "healthy";
    case "auto_eligible":
    case "proposed":
    case "rendered":
    case "config_rendered":
      return "info";
    case "approval_required":
    case "queued":
    case "stale":
    case "backpressured":
      return "warning";
    case "privacy_blocked":
    case "parser_failed":
    case "silent":
    case "failed":
      return "critical";
    case "rejected":
    case "unsupported":
      return "degraded";
    default:
      return "unknown";
  }
}

function severityTone(severity?: string): StateTone {
  switch ((severity ?? "").toLowerCase()) {
    case "critical":
    case "high":
      return "critical";
    case "medium":
    case "warning":
      return "warning";
    case "low":
    case "info":
      return "info";
    default:
      return "unknown";
  }
}

function socCaseEvidenceValue(item: SOCCase, key: string): string {
  const value = item.evidence?.[key];
  if (typeof value === "string") return value.trim();
  if (typeof value === "number" || typeof value === "boolean")
    return String(value);
  return "";
}

function sourceHealthCaseEvidenceLine(item: SOCCase): string {
  const sourceId = socCaseEvidenceValue(item, "source_id");
  const coverageState = socCaseEvidenceValue(item, "coverage_state");
  const parserId = socCaseEvidenceValue(item, "parser_id");
  const collectorId = socCaseEvidenceValue(item, "collector_id");
  const lastError = socCaseEvidenceValue(item, "last_error");
  const parts: string[] = [];
  if (sourceId) parts.push(`source: ${sourceId}`);
  if (coverageState) parts.push(`state: ${formatStatus(coverageState)}`);
  if (parserId) parts.push(`parser: ${parserId}`);
  if (collectorId) parts.push(`collector: ${collectorId}`);
  if (lastError) parts.push(`error: ${lastError}`);
  return parts.join(" / ");
}

function sourceHealthCaseEvidenceRefsLine(item: SOCCase): string {
  const refs = item.evidence_refs ?? [];
  if (refs.length === 0) return "";
  const first = refs[0];
  const suffix = refs.length > 1 ? ` +${refs.length - 1}` : "";
  return `${refs.length} evidence ref${refs.length === 1 ? "" : "s"}: ${first.kind} ${first.id}${suffix}`;
}

function sourceHealthCasePrimaryEvidenceRef(item: SOCCase): string {
  return item.evidence_refs?.[0]?.id?.trim() ?? "";
}

function sourceHealthCaseExportFilename(
  item: Pick<SOCCase, "case_id" | "created_at" | "updated_at">,
): string {
  const safeId =
    item.case_id
      .replace(/[^a-zA-Z0-9-]+/g, "-")
      .replace(/^-|-$/g, "")
      .slice(0, 48) || "case";
  const stamp = (
    item.updated_at ||
    item.created_at ||
    new Date().toISOString()
  ).slice(0, 10);
  return `soc-case-${safeId}-${stamp}.json`;
}

function formatStatus(value?: string): string {
  return (value || "unknown").replace(/_/g, " ");
}

function formatTime(value?: string): string {
  if (!value) return "-";
  const parsed = new Date(value);
  if (Number.isNaN(parsed.getTime())) return value;
  return parsed.toLocaleString();
}

function metricValue(row: ContentPackSourceHealth, key: string): number {
  const value = row.metrics?.[key];
  return typeof value === "number" && Number.isFinite(value) ? value : 0;
}

function sourceHealthLabel(
  row: ContentPackSourceHealth,
  ...keys: string[]
): string {
  for (const key of keys) {
    const value = row.labels?.[key]?.trim();
    if (value) return value;
  }
  return "";
}

function sourceHealthEvidenceLine(row: ContentPackSourceHealth): string {
  const collectMode = sourceHealthLabel(
    row,
    "collect_mode",
    "control_one.collect_mode",
  );
  const rawRetained = sourceHealthLabel(
    row,
    "raw_message_retained",
    "control_one.raw_message_retained",
  );
  const parts: string[] = [];
  if (collectMode) {
    const label =
      COLLECT_MODE_LABELS[
        collectMode as ContentPackSourceProposalCollectMode
      ] ?? formatStatus(collectMode);
    parts.push(label);
  }
  if (rawRetained) {
    parts.push(`raw retained: ${formatStatus(rawRetained)}`);
  }
  return parts.join(" / ");
}

function sourceHealthApprovalLine(row: ContentPackSourceHealth): string {
  const approvalId = (row.approval_id ?? "").trim();
  if (approvalId) return `approval: ${approvalId}`;
  return row.approval_required ? "approval required" : "";
}

function sourceHealthInstanceLine(row: ContentPackSourceHealth): string {
  const instanceId = (row.source_instance_id ?? "").trim();
  if (!instanceId || instanceId === row.source_id) return "";
  return instanceId;
}

function sourceHealthStableKey(row: ContentPackSourceHealth): string {
  return (
    row.source_instance_id?.trim() ||
    [row.node_id, row.collector_id, row.source_id].filter(Boolean).join(":")
  );
}

function sourceHealthInvestigationAction(row: ContentPackSourceHealth) {
  return (row.recommended_actions ?? []).find(
    (action) => action.action === "source_health.investigate",
  );
}

function sourceHealthMapKey(nodeId?: string, sourceId?: string): string {
  const node = (nodeId ?? "").trim();
  const source = (sourceId ?? "").trim();
  if (!node || !source) return "";
  return `${node}/${source}`;
}

function compactList(values?: string[], fallback = "-"): string {
  if (!values || values.length === 0) return fallback;
  if (values.length <= 2) return values.join(", ");
  return `${values.slice(0, 2).join(", ")} +${values.length - 2}`;
}

function proposalCanBeDecided(proposal: ContentPackSourceProposal): boolean {
  return ["proposed", "auto_eligible", "approval_required", "stale"].includes(
    proposal.status,
  );
}

function proposalCollectMode(
  proposal: ContentPackSourceProposal,
): ContentPackSourceProposalCollectMode {
  const mode = String(
    proposal.collect_mode || "",
  ).trim() as ContentPackSourceProposalCollectMode;
  if (mode && Object.prototype.hasOwnProperty.call(COLLECT_MODE_LABELS, mode))
    return mode;
  return "collect_raw";
}

function proposalCollectModeCanRenderOTel(
  proposal: ContentPackSourceProposal,
): boolean {
  return ["collect_raw", "collect_parsed"].includes(
    proposalCollectMode(proposal),
  );
}

export function SIEMCoverage(): JSX.Element {
  const api = useApiClient();
  const { profile } = useAuth();
  const { currentTenantId, currentTenant } = useTenant();
  const [proposals, setProposals] = useState<ContentPackSourceProposal[]>([]);
  const [proposalPagination, setProposalPagination] =
    useState<PaginationMeta | null>(null);
  const [proposalSummary, setProposalSummary] =
    useState<ContentPackSourceProposalSummary | null>(null);
  const [proposalOffset, setProposalOffset] = useState(0);
  const [health, setHealth] = useState<ContentPackSourceHealth[]>([]);
  const [healthSummary, setHealthSummary] = useState<
    ContentPackSourceHealthResponse["totals"] | null
  >(null);
  const [selectedHealth, setSelectedHealth] =
    useState<ContentPackSourceHealth | null>(null);
  const [busyHealthInvestigationKey, setBusyHealthInvestigationKey] = useState<
    string | null
  >(null);
  const [sourceHealthInvestigationCase, setSourceHealthInvestigationCase] =
    useState<ContentPackSourceHealthInvestigationCase | null>(null);
  const [sourceHealthCases, setSourceHealthCases] = useState<SOCCase[]>([]);
  const [sourceHealthCasePagination, setSourceHealthCasePagination] =
    useState<PaginationMeta | null>(null);
  const [sourceHealthCaseNoteDrafts, setSourceHealthCaseNoteDrafts] = useState<
    Record<string, string>
  >({});
  const [busySourceHealthCaseNoteId, setBusySourceHealthCaseNoteId] = useState<
    string | null
  >(null);
  const [exportingSourceHealthCaseId, setExportingSourceHealthCaseId] =
    useState<string | null>(null);
  const [healthPagination, setHealthPagination] =
    useState<PaginationMeta | null>(null);
  const [healthOffset, setHealthOffset] = useState(0);
  const [healthSearchDraft, setHealthSearchDraft] = useState("");
  const [healthQuery, setHealthQuery] = useState("");
  const [healthStateFilter, setHealthStateFilter] =
    useState<SourceHealthStateFilter>("all");
  const [collectors, setCollectors] = useState<ContentPackEdgeCollector[]>([]);
  const [candidates, setCandidates] = useState<
    ContentPackOTelConfigCandidate[]
  >([]);
  const [candidateDetail, setCandidateDetail] =
    useState<ContentPackOTelConfigCandidateDetail | null>(null);
  const [policy, setPolicy] = useState<TenantConnectorPolicy | null>(null);
  const [policyDraft, setPolicyDraft] =
    useState<PolicyDraft>(EMPTY_POLICY_DRAFT);
  const [deploymentDraft, setDeploymentDraft] = useState<DeploymentDraft>({
    endpoint: "",
    collectorId: "",
  });
  const [statusFilter, setStatusFilter] = useState<ProposalStatusFilter>("all");
  const [proposalSearchDraft, setProposalSearchDraft] = useState("");
  const [proposalQuery, setProposalQuery] = useState("");
  const [loading, setLoading] = useState(false);
  const [savingPolicy, setSavingPolicy] = useState(false);
  const [busyProposalId, setBusyProposalId] = useState<string | null>(null);
  const [busyCandidateId, setBusyCandidateId] = useState<string | null>(null);
  const [reviewingCandidateId, setReviewingCandidateId] = useState<
    string | null
  >(null);
  const [error, setError] = useState<string | null>(null);
  const [confirm, setConfirm] = useState<ConfirmState | null>(null);
  const [approvalCollectMode, setApprovalCollectMode] =
    useState<ContentPackSourceProposalCollectMode>("collect_raw");

  const roles = profile?.roles ?? [];
  const isAdmin = roles.includes("admin");
  const canReviewCollectorConfig = isAdmin || roles.includes("operator");
  const canOpenSourceHealthInvestigation =
    canReviewCollectorConfig || roles.includes("investigator");

  const openProposalDecision = useCallback(
    (kind: ConfirmState["kind"], proposal: ContentPackSourceProposal) => {
      setApprovalCollectMode(proposalCollectMode(proposal));
      setConfirm({ kind, proposal });
    },
    [],
  );

  const load = useCallback(async () => {
    if (!currentTenantId) {
      setProposals([]);
      setProposalPagination(null);
      setProposalSummary(null);
      setProposalOffset(0);
      setHealth([]);
      setSelectedHealth(null);
      setBusyHealthInvestigationKey(null);
      setSourceHealthInvestigationCase(null);
      setSourceHealthCases([]);
      setSourceHealthCasePagination(null);
      setSourceHealthCaseNoteDrafts({});
      setBusySourceHealthCaseNoteId(null);
      setExportingSourceHealthCaseId(null);
      setHealthSummary(null);
      setHealthPagination(null);
      setHealthOffset(0);
      setHealthSearchDraft("");
      setHealthQuery("");
      setHealthStateFilter("all");
      setCollectors([]);
      setCandidates([]);
      setCandidateDetail(null);
      setPolicy(null);
      setPolicyDraft(EMPTY_POLICY_DRAFT);
      setDeploymentDraft({ endpoint: "", collectorId: "" });
      setStatusFilter("all");
      setProposalSearchDraft("");
      setProposalQuery("");
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const [
        proposalResp,
        healthResp,
        caseResp,
        policyResp,
        collectorResp,
        candidateResp,
      ] = await Promise.all([
        api.listContentPackSourceProposals({
          tenantId: currentTenantId,
          limit: SOURCE_PROPOSAL_PAGE_LIMIT,
          offset: proposalOffset,
          ...(proposalQuery ? { q: proposalQuery } : {}),
          ...(statusFilter !== "all" ? { status: statusFilter } : {}),
        }),
        api.getContentPackSourceHealth(currentTenantId, {
          limit: SOURCE_HEALTH_PAGE_LIMIT,
          offset: healthOffset,
          ...(healthQuery ? { q: healthQuery } : {}),
          ...(healthStateFilter !== "all" ? { state: healthStateFilter } : {}),
        }),
        api.listSOCCases({
          tenantId: currentTenantId,
          triggerType: "siem_source_health",
          includeNotes: true,
          limit: SOURCE_HEALTH_CASE_LIMIT,
          offset: 0,
        }),
        api.getTenantConnectorPolicy(currentTenantId),
        api.listContentPackEdgeCollectors({
          tenantId: currentTenantId,
          limit: 200,
          offset: 0,
        }),
        api.listContentPackOTelConfigCandidates({
          tenantId: currentTenantId,
          limit: 200,
          offset: 0,
        }),
      ]);
      setProposals(proposalResp.data ?? []);
      setProposalPagination(proposalResp.pagination ?? null);
      setProposalSummary(proposalResp.summary ?? null);
      const nextHealth = healthResp.items ?? [];
      setHealth(nextHealth);
      setSelectedHealth((current) => {
        if (!current) return null;
        const currentKey = sourceHealthStableKey(current);
        return (
          nextHealth.find((row) => sourceHealthStableKey(row) === currentKey) ??
          null
        );
      });
      setHealthSummary(healthResp.totals ?? null);
      setHealthPagination(healthResp.pagination ?? null);
      setSourceHealthCases(caseResp.data ?? []);
      setSourceHealthCasePagination(caseResp.pagination ?? null);
      const nextCollectors = collectorResp.data ?? [];
      setCollectors(nextCollectors);
      setCandidates(candidateResp.data ?? []);
      setPolicy(policyResp);
      setPolicyDraft(policyToDraft(policyResp));
      setDeploymentDraft((draft) => ({
        endpoint: draft.endpoint,
        collectorId: draft.collectorId || nextCollectors[0]?.collector_id || "",
      }));
    } catch (err) {
      const message =
        err instanceof Error ? err.message : "Failed to load SIEM coverage";
      setError(message);
    } finally {
      setLoading(false);
    }
  }, [
    api,
    currentTenantId,
    healthOffset,
    healthQuery,
    healthStateFilter,
    proposalOffset,
    proposalQuery,
    statusFilter,
  ]);

  useEffect(() => {
    load().catch(() => {});
  }, [load]);

  const healthBySource = useMemo(() => {
    const out = new Map<string, ContentPackSourceHealth>();
    for (const item of health) {
      const nodeScopedKey = sourceHealthMapKey(
        item.node_id || item.collector_id,
        item.source_id,
      );
      if (nodeScopedKey) out.set(nodeScopedKey, item);
      if (item.source_id && !out.has(item.source_id)) {
        out.set(item.source_id, item);
      }
    }
    return out;
  }, [health]);

  const proposalPageTotal = proposalPagination?.total ?? proposals.length;
  const proposalPageOffset = proposalPagination?.offset ?? proposalOffset;
  const proposalPageCount = proposalPagination?.count ?? proposals.length;
  const proposalPageStart = proposalPageTotal > 0 ? proposalPageOffset + 1 : 0;
  const proposalPageEnd =
    proposalPageTotal > 0
      ? Math.min(proposalPageOffset + proposalPageCount, proposalPageTotal)
      : 0;

  const proposalTotals = useMemo(() => {
    const byStatus = proposalSummary?.by_status ?? {};
    const total = proposalSummary?.total ?? proposalPageTotal;
    const approvalRequired = proposals.filter(
      (p) => p.status === "approval_required",
    ).length;
    const approved = proposals.filter((p) => p.status === "approved").length;
    const autoEligible = proposals.filter(
      (p) => p.status === "auto_eligible",
    ).length;
    const blocked = proposals.filter(
      (p) => p.status === "privacy_blocked",
    ).length;
    return {
      total,
      approvalRequired:
        proposalSummary?.by_status?.approval_required ?? approvalRequired,
      approved: byStatus.approved ?? approved,
      autoEligible: byStatus.auto_eligible ?? autoEligible,
      blocked: byStatus.privacy_blocked ?? blocked,
    };
  }, [proposalPageTotal, proposalSummary, proposals]);

  const healthTotals = useMemo(() => {
    const byState = healthSummary?.by_state ?? {};
    const collecting = health.filter((h) =>
      ["collecting", "deployed"].includes(h.coverage_state),
    ).length;
    const degraded = health.filter((h) =>
      ["backpressured", "parser_failed", "silent"].includes(h.coverage_state),
    ).length;
    const received = health.reduce(
      (sum, h) => sum + metricValue(h, "events_received"),
      0,
    );
    const summaryCollecting =
      (byState.collecting ?? 0) + (byState.deployed ?? 0);
    const summaryDegraded =
      (byState.backpressured ?? 0) +
      (byState.parser_failed ?? 0) +
      (byState.silent ?? 0);
    return {
      collecting: healthSummary ? summaryCollecting : collecting,
      degraded: healthSummary ? summaryDegraded : degraded,
      received: healthSummary?.metrics?.events_received ?? received,
    };
  }, [health, healthSummary]);

  const healthPageTotal = healthPagination?.total ?? health.length;
  const healthPageOffset = healthPagination?.offset ?? healthOffset;
  const healthPageCount = healthPagination?.count ?? health.length;
  const healthPageStart = healthPageTotal > 0 ? healthPageOffset + 1 : 0;
  const healthPageEnd =
    healthPageTotal > 0
      ? Math.min(healthPageOffset + healthPageCount, healthPageTotal)
      : 0;
  const sourceHealthCaseTotal =
    sourceHealthCasePagination?.total ?? sourceHealthCases.length;
  const selectedHealthMetrics = useMemo(
    () =>
      Object.entries(selectedHealth?.metrics ?? {})
        .filter(
          (entry): entry is [string, number] =>
            typeof entry[1] === "number" && Number.isFinite(entry[1]),
        )
        .sort(([left], [right]) => left.localeCompare(right)),
    [selectedHealth],
  );
  const selectedHealthLabels = useMemo(
    () =>
      Object.entries(selectedHealth?.labels ?? {})
        .filter(([, value]) => value.trim() !== "")
        .sort(([left], [right]) => left.localeCompare(right)),
    [selectedHealth],
  );

  const applyHealthSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setHealthOffset(0);
    setHealthQuery(healthSearchDraft.trim());
  };

  const clearHealthSearch = () => {
    setHealthSearchDraft("");
    setHealthOffset(0);
    setHealthQuery("");
    setHealthStateFilter("all");
  };

  const applyProposalSearch = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    setProposalOffset(0);
    setProposalQuery(proposalSearchDraft.trim());
  };

  const clearProposalSearch = () => {
    setProposalSearchDraft("");
    setProposalOffset(0);
    setProposalQuery("");
    setStatusFilter("all");
  };

  const openSourceHealthInvestigation = async (
    row: ContentPackSourceHealth,
  ) => {
    if (!currentTenantId || !row.runtime_state_id) return;
    const key = sourceHealthStableKey(row);
    setBusyHealthInvestigationKey(key);
    try {
      const resp = await api.createContentPackSourceHealthInvestigation(
        currentTenantId,
        {
          runtime_state_id: row.runtime_state_id,
          note: `Opened from SIEM source health: ${formatStatus(row.coverage_state)}`,
        },
      );
      setSourceHealthInvestigationCase(resp.case);
      const existingCase = sourceHealthCases.some(
        (item) => item.case_id === resp.case.case_id,
      );
      setSourceHealthCases((current) => {
        return [
          resp.case,
          ...current.filter((item) => item.case_id !== resp.case.case_id),
        ].slice(0, SOURCE_HEALTH_CASE_LIMIT);
      });
      if (!existingCase) {
        setSourceHealthCasePagination((page) =>
          page
            ? {
                ...page,
                total: page.total + 1,
                count: Math.min(SOURCE_HEALTH_CASE_LIMIT, page.count + 1),
              }
            : page,
        );
      }
      toast.success("Source health investigation opened");
    } catch (err) {
      toast.error(
        err instanceof Error
          ? err.message
          : "Investigation could not be opened",
      );
    } finally {
      setBusyHealthInvestigationKey(null);
    }
  };

  const submitSourceHealthCaseNote = async (
    event: FormEvent<HTMLFormElement>,
    item: SOCCase,
  ) => {
    event.preventDefault();
    if (!currentTenantId) return;
    const note = (sourceHealthCaseNoteDrafts[item.case_id] ?? "").trim();
    if (!note) return;
    const evidenceRef = sourceHealthCasePrimaryEvidenceRef(item);
    setBusySourceHealthCaseNoteId(item.case_id);
    try {
      const created = await api.createSOCCaseNote(
        currentTenantId,
        item.case_id,
        {
          note,
          citations: evidenceRef ? [evidenceRef] : [],
        },
      );
      setSourceHealthCases((current) =>
        current.map((row) =>
          row.case_id === item.case_id
            ? { ...row, notes: [created, ...(row.notes ?? [])] }
            : row,
        ),
      );
      setSourceHealthCaseNoteDrafts((current) => ({
        ...current,
        [item.case_id]: "",
      }));
      toast.success("Source investigation note added");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Investigation note was not saved",
      );
    } finally {
      setBusySourceHealthCaseNoteId(null);
    }
  };

  const downloadSourceHealthCaseExport = async (item: SOCCase) => {
    const tenantId = item.tenant_id || currentTenantId;
    if (!tenantId) return;
    setExportingSourceHealthCaseId(item.case_id);
    try {
      const packet = await api.exportSOCCase(item.case_id, tenantId);
      saveBlob(
        new Blob([JSON.stringify(packet, null, 2)], {
          type: "application/json",
        }),
        sourceHealthCaseExportFilename(item),
      );
      toast.success("Source investigation export downloaded");
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Export could not be downloaded",
      );
    } finally {
      setExportingSourceHealthCaseId(null);
    }
  };

  const candidateTotals = useMemo(() => {
    const rendered = candidates.filter(
      (candidate) => candidate.status === "rendered",
    ).length;
    const queued = candidates.filter(
      (candidate) => candidate.status === "queued",
    ).length;
    const deployed = candidates.filter(
      (candidate) => candidate.status === "deployed",
    ).length;
    const failed = candidates.filter(
      (candidate) => candidate.status === "failed",
    ).length;
    return { rendered, queued, deployed, failed };
  }, [candidates]);

  const savePolicy = async () => {
    if (!currentTenantId) return;
    setSavingPolicy(true);
    try {
      const updated = await api.updateTenantConnectorPolicy(currentTenantId, {
        allow_medium_risk: policyDraft.allowMediumRisk,
        allow_high_risk: policyDraft.allowHighRisk,
        auto_connect_programs: splitProgramList(
          policyDraft.autoConnectPrograms,
        ),
        approval_required_programs: splitProgramList(
          policyDraft.approvalRequiredPrograms,
        ),
        blocked_programs: splitProgramList(policyDraft.blockedPrograms),
      });
      setPolicy(updated);
      setPolicyDraft(policyToDraft(updated));
      toast.success("Connector policy saved");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Policy save failed");
    } finally {
      setSavingPolicy(false);
    }
  };

  const runProposalDecision = async () => {
    if (!confirm || !currentTenantId) return;
    const { kind, proposal } = confirm;
    setBusyProposalId(proposal.id);
    try {
      if (kind === "approve") {
        await api.approveContentPackSourceProposal(
          currentTenantId,
          proposal.id,
          {
            note: "Approved from SIEM coverage",
            collect_mode: approvalCollectMode,
          },
        );
        toast.success("Source proposal approved");
      } else {
        await api.rejectContentPackSourceProposal(
          currentTenantId,
          proposal.id,
          {
            reason:
              kind === "privacy_block"
                ? "Privacy blocked from SIEM coverage"
                : "Rejected from SIEM coverage",
            privacy_blocked: kind === "privacy_block",
          },
        );
        toast.success(
          kind === "privacy_block"
            ? "Source proposal privacy-blocked"
            : "Source proposal rejected",
        );
      }
      setConfirm(null);
      await load();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Proposal decision failed",
      );
    } finally {
      setBusyProposalId(null);
    }
  };

  const createCandidateFromProposal = useCallback(
    async (proposal: ContentPackSourceProposal) => {
      if (!currentTenantId) return;
      const endpoint = deploymentDraft.endpoint.trim();
      if (!endpoint) {
        toast.error("OTel exporter endpoint is required");
        return;
      }
      setBusyProposalId(proposal.id);
      try {
        const rendered = await api.createContentPackOTelConfigCandidate(
          currentTenantId,
          {
            endpoint,
            collector_id: deploymentDraft.collectorId.trim() || undefined,
            source_proposal_ids: [proposal.id],
          },
        );
        toast.success(
          `Rendered candidate ${rendered.candidate_id || rendered.config_version}`,
        );
        await load();
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : "Candidate render failed",
        );
      } finally {
        setBusyProposalId(null);
      }
    },
    [
      api,
      currentTenantId,
      deploymentDraft.collectorId,
      deploymentDraft.endpoint,
      load,
    ],
  );

  const approveCandidate = async (
    candidate: ContentPackOTelConfigCandidate,
  ) => {
    if (!currentTenantId) return;
    setBusyCandidateId(candidate.id);
    try {
      const updated = await api.approveContentPackOTelConfigCandidate(
        currentTenantId,
        candidate.id,
        {
          note: "Approved from SIEM coverage",
          reviewed_config_version: candidate.config_version,
        },
      );
      setCandidateDetail((current) =>
        current?.id === updated.id ? { ...current, ...updated } : current,
      );
      toast.success("Collector config candidate approved");
      await load();
    } catch (err) {
      toast.error(
        err instanceof Error ? err.message : "Candidate approval failed",
      );
    } finally {
      setBusyCandidateId(null);
    }
  };

  const queueCandidate = useCallback(
    async (candidate: ContentPackOTelConfigCandidate) => {
      if (!currentTenantId) return;
      const collectorId = (
        deploymentDraft.collectorId ||
        candidate.collector_id ||
        candidate.target_collector_id ||
        ""
      ).trim();
      if (!collectorId) {
        toast.error("Collector ID is required");
        return;
      }
      setBusyCandidateId(candidate.id);
      try {
        const updated = await api.queueContentPackOTelConfigCandidate(
          currentTenantId,
          candidate.id,
          {
            collector_id: collectorId,
            note: "Queued from SIEM coverage",
            expected_config_version: candidate.config_version,
          },
        );
        setCandidateDetail((current) =>
          current?.id === updated.id ? { ...current, ...updated } : current,
        );
        toast.success("Collector config queued");
        await load();
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : "Candidate queue failed",
        );
      } finally {
        setBusyCandidateId(null);
      }
    },
    [api, currentTenantId, deploymentDraft.collectorId, load],
  );

  const reviewCandidate = useCallback(
    async (candidate: ContentPackOTelConfigCandidate) => {
      if (!currentTenantId) return;
      setReviewingCandidateId(candidate.id);
      try {
        const detail = await api.getContentPackOTelConfigCandidate(
          currentTenantId,
          candidate.id,
        );
        setCandidateDetail(detail);
      } catch (err) {
        toast.error(
          err instanceof Error ? err.message : "Candidate review failed",
        );
      } finally {
        setReviewingCandidateId(null);
      }
    },
    [api, currentTenantId],
  );

  const proposalColumns = useMemo<ColumnDef<ContentPackSourceProposal>[]>(
    () => [
      {
        id: "source",
        header: "Source",
        cell: ({ row }) => {
          const p = row.original;
          const sourceID =
            p.source_id || p.labels?.content_pack_source_id || p.program;
          const sourceHealth =
            healthBySource.get(sourceHealthMapKey(p.node_id, sourceID)) ||
            healthBySource.get(sourceID);
          const evidenceLine = sourceHealth
            ? sourceHealthEvidenceLine(sourceHealth)
            : "";
          return (
            <div className="flex min-w-[180px] flex-col gap-1">
              <span className="font-medium text-foreground">{p.program}</span>
              <span className="font-mono text-xs text-text-muted">
                {sourceID}
              </span>
              {sourceHealth && (
                <StatusTag
                  tone={statusTone(sourceHealth.coverage_state)}
                  className="w-fit"
                >
                  {formatStatus(sourceHealth.coverage_state)}
                </StatusTag>
              )}
              {evidenceLine && (
                <span className="text-xs text-text-secondary">
                  {evidenceLine}
                </span>
              )}
            </div>
          );
        },
      },
      {
        accessorKey: "status",
        header: "Decision",
        cell: ({ getValue, row }) => {
          const mode = proposalCollectMode(row.original);
          return (
            <div className="flex flex-col gap-1">
              <StatusTag tone={statusTone(String(getValue()))}>
                {formatStatus(String(getValue()))}
              </StatusTag>
              {row.original.status === "approved" && (
                <span className="text-xs text-text-muted">
                  {COLLECT_MODE_LABELS[mode]}
                </span>
              )}
            </div>
          );
        },
      },
      {
        accessorKey: "risk",
        header: "Risk",
        cell: ({ getValue, row }) => {
          const risk = String(getValue() || "unknown");
          return (
            <div className="flex flex-col gap-1">
              <StatusTag
                tone={
                  risk === "high" || risk === "critical"
                    ? "critical"
                    : risk === "medium"
                      ? "warning"
                      : "healthy"
                }
              >
                {risk}
              </StatusTag>
              <span className="text-xs text-text-muted">
                {row.original.labels?.policy_decision || "-"}
              </span>
            </div>
          );
        },
      },
      {
        id: "paths",
        header: "Paths",
        cell: ({ row }) => (
          <span className="max-w-[260px] truncate font-mono text-xs text-text-secondary">
            {compactList(row.original.paths)}
          </span>
        ),
      },
      {
        id: "reason",
        header: "Evidence",
        cell: ({ row }) => (
          <div className="max-w-[320px] text-xs text-text-secondary">
            <div className="line-clamp-2">{row.original.reason || "-"}</div>
            <div className="mt-1 text-text-muted">
              {compactList(row.original.evidence, "")}
            </div>
          </div>
        ),
      },
      {
        id: "last_seen",
        header: "Last seen",
        cell: ({ row }) => (
          <span className="font-mono text-xs">
            {formatTime(row.original.last_seen_at)}
          </span>
        ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => {
          const proposal = row.original;
          const disabled =
            !isAdmin ||
            !proposalCanBeDecided(proposal) ||
            busyProposalId === proposal.id;
          const canRender =
            isAdmin &&
            proposal.status === "approved" &&
            proposalCollectModeCanRenderOTel(proposal) &&
            busyProposalId !== proposal.id;
          return (
            <div className="flex justify-end gap-2">
              {proposal.status === "approved" ? (
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={!canRender}
                  onClick={() => createCandidateFromProposal(proposal)}
                >
                  <DatabaseZap className="h-4 w-4" />
                  Render
                </Button>
              ) : (
                <>
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={disabled}
                    onClick={() => openProposalDecision("approve", proposal)}
                  >
                    <Check className="h-4 w-4" />
                    Approve
                  </Button>
                  <Button
                    size="sm"
                    variant="outline"
                    disabled={disabled}
                    onClick={() =>
                      openProposalDecision("privacy_block", proposal)
                    }
                  >
                    <ShieldX className="h-4 w-4" />
                    Block
                  </Button>
                </>
              )}
            </div>
          );
        },
      },
    ],
    [
      busyProposalId,
      healthBySource,
      isAdmin,
      createCandidateFromProposal,
      openProposalDecision,
    ],
  );

  const healthColumns = useMemo<ColumnDef<ContentPackSourceHealth>[]>(
    () => [
      {
        id: "source",
        header: "Source",
        cell: ({ row }) => {
          const evidenceLine = sourceHealthEvidenceLine(row.original);
          const approvalLine = sourceHealthApprovalLine(row.original);
          const instanceLine = sourceHealthInstanceLine(row.original);
          return (
            <div className="flex min-w-[180px] flex-col gap-1">
              <span className="font-medium text-foreground">
                {row.original.display_name || row.original.source_id}
              </span>
              <span className="font-mono text-xs text-text-muted">
                {row.original.source_id}
              </span>
              {instanceLine && (
                <span className="font-mono text-xs text-text-muted">
                  {instanceLine}
                </span>
              )}
              {evidenceLine && (
                <span className="text-xs text-text-secondary">
                  {evidenceLine}
                </span>
              )}
              {approvalLine && (
                <span className="text-xs text-text-secondary">
                  {approvalLine}
                </span>
              )}
            </div>
          );
        },
      },
      {
        accessorKey: "coverage_state",
        header: "State",
        cell: ({ getValue }) => (
          <StatusTag tone={statusTone(String(getValue()))}>
            {formatStatus(String(getValue()))}
          </StatusTag>
        ),
      },
      {
        id: "events",
        header: "Events",
        cell: ({ row }) => (
          <div className="font-mono text-xs tabular-nums">
            {metricValue(row.original, "events_received").toLocaleString()} in /{" "}
            {metricValue(row.original, "events_parsed").toLocaleString()} parsed
          </div>
        ),
      },
      {
        id: "pressure",
        header: "Pressure",
        cell: ({ row }) => {
          const dropped = metricValue(row.original, "events_dropped");
          const queue = metricValue(row.original, "queue_depth");
          const retries = metricValue(row.original, "retry_count");
          return (
            <span className="font-mono text-xs tabular-nums">
              d:{dropped.toLocaleString()} q:{queue.toLocaleString()} r:
              {retries.toLocaleString()}
            </span>
          );
        },
      },
      {
        id: "collector",
        header: "Collector",
        cell: ({ row }) => (
          <div className="flex max-w-[240px] flex-col gap-1">
            <span className="font-mono text-xs">
              {row.original.collector_id || "-"}
            </span>
            <span className="truncate font-mono text-xs text-text-muted">
              {row.original.receiver_id || row.original.collector_mode || "-"}
            </span>
          </div>
        ),
      },
      {
        id: "last_health",
        header: "Last health",
        cell: ({ row }) => (
          <span className="font-mono text-xs">
            {formatTime(row.original.last_health_at)}
          </span>
        ),
      },
      {
        id: "error",
        header: "Error",
        cell: ({ row }) => (
          <span className="max-w-[260px] truncate text-xs text-text-secondary">
            {row.original.last_error || "-"}
          </span>
        ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => (
          <div className="flex justify-end">
            <Button
              size="sm"
              variant="secondary"
              onClick={() => {
                setSelectedHealth(row.original);
                setSourceHealthInvestigationCase(null);
              }}
            >
              Inspect
            </Button>
          </div>
        ),
      },
    ],
    [],
  );

  const candidateColumns = useMemo<ColumnDef<ContentPackOTelConfigCandidate>[]>(
    () => [
      {
        id: "candidate",
        header: "Candidate",
        cell: ({ row }) => (
          <div className="flex min-w-[180px] flex-col gap-1">
            <span className="font-mono text-xs text-foreground">
              {row.original.config_version}
            </span>
            <span className="font-mono text-xs text-text-muted">
              {row.original.id}
            </span>
          </div>
        ),
      },
      {
        accessorKey: "status",
        header: "Status",
        cell: ({ getValue }) => (
          <StatusTag tone={statusTone(String(getValue()))}>
            {formatStatus(String(getValue()))}
          </StatusTag>
        ),
      },
      {
        id: "sources",
        header: "Sources",
        cell: ({ row }) => (
          <span className="max-w-[260px] truncate font-mono text-xs text-text-secondary">
            {compactList(row.original.source_ids)}
          </span>
        ),
      },
      {
        id: "collector",
        header: "Collector",
        cell: ({ row }) => (
          <div className="flex max-w-[220px] flex-col gap-1">
            <span className="font-mono text-xs">
              {row.original.target_collector_id ||
                row.original.collector_id ||
                "-"}
            </span>
            <span className="truncate text-xs text-text-muted">
              {row.original.endpoint || "-"}
            </span>
          </div>
        ),
      },
      {
        id: "updated",
        header: "Updated",
        cell: ({ row }) => (
          <span className="font-mono text-xs">
            {formatTime(row.original.updated_at)}
          </span>
        ),
      },
      {
        id: "actions",
        header: "",
        cell: ({ row }) => {
          const candidate = row.original;
          const busy = busyCandidateId === candidate.id;
          return (
            <div className="flex justify-end gap-2">
              <Button
                size="sm"
                variant="secondary"
                disabled={
                  !canReviewCollectorConfig ||
                  reviewingCandidateId === candidate.id
                }
                onClick={() => reviewCandidate(candidate)}
              >
                <DatabaseZap className="h-4 w-4" />
                Review
              </Button>
              {candidate.status === "approved" && (
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={!isAdmin || busy}
                  onClick={() => queueCandidate(candidate)}
                >
                  <DatabaseZap className="h-4 w-4" />
                  Queue
                </Button>
              )}
            </div>
          );
        },
      },
    ],
    [
      busyCandidateId,
      canReviewCollectorConfig,
      isAdmin,
      queueCandidate,
      reviewCandidate,
      reviewingCandidateId,
    ],
  );

  const confirmTitle =
    confirm?.kind === "approve"
      ? "Approve source proposal"
      : confirm?.kind === "privacy_block"
        ? "Privacy block source proposal"
        : "Reject source proposal";

  if (!currentTenantId) {
    return (
      <div className="space-y-6">
        <SectionHeader
          title="SIEM coverage"
          description="Connector proposals, source health, and collection policy."
        />
        <EmptyState
          icon={<DatabaseZap />}
          title="No tenant selected"
          description="Choose a tenant to load connector coverage."
        />
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <SectionHeader
        eyebrow={currentTenant?.name ?? undefined}
        title="SIEM coverage"
        description="Connector proposals, source health, and collection policy."
        actions={
          <Button variant="secondary" onClick={load} loading={loading}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
        }
      />

      {error && (
        <Panel toneAccent="critical" title="Load failed">
          <p className="text-sm text-text-secondary">{error}</p>
        </Panel>
      )}

      <div className="grid gap-4 md:grid-cols-2 xl:grid-cols-4">
        <KpiTile
          label="Proposals"
          value={proposalTotals.total.toLocaleString()}
          icon={<DatabaseZap />}
          loading={loading}
        />
        <KpiTile
          label="Approval required"
          value={proposalTotals.approvalRequired.toLocaleString()}
          tone="warning"
          icon={<ShieldAlert />}
          loading={loading}
        />
        <KpiTile
          label="Approved"
          value={proposalTotals.approved.toLocaleString()}
          tone="healthy"
          icon={<ShieldCheck />}
          loading={loading}
        />
        <KpiTile
          label="Collecting"
          value={healthTotals.collecting.toLocaleString()}
          tone={healthTotals.degraded > 0 ? "warning" : "healthy"}
          icon={<Check />}
          loading={loading}
        />
      </div>

      <Panel
        title="Connector policy"
        eyebrow="AUTO-CONNECT"
        toneAccent={
          policyDraft.allowHighRisk
            ? "critical"
            : policyDraft.allowMediumRisk
              ? "warning"
              : "healthy"
        }
        actions={
          <Button
            onClick={savePolicy}
            loading={savingPolicy}
            disabled={!isAdmin}
          >
            <Save className="h-4 w-4" />
            Save
          </Button>
        }
      >
        <div className="flex flex-wrap gap-2">
          <Button
            type="button"
            variant={policyDraft.allowMediumRisk ? "primary" : "secondary"}
            onClick={() =>
              setPolicyDraft((p) => ({
                ...p,
                allowMediumRisk: !p.allowMediumRisk,
              }))
            }
            disabled={!isAdmin}
          >
            <ShieldAlert className="h-4 w-4" />
            Medium risk
          </Button>
          <Button
            type="button"
            variant={policyDraft.allowHighRisk ? "danger" : "secondary"}
            onClick={() =>
              setPolicyDraft((p) => ({ ...p, allowHighRisk: !p.allowHighRisk }))
            }
            disabled={!isAdmin}
          >
            <ShieldX className="h-4 w-4" />
            High risk
          </Button>
        </div>
        <div className="grid gap-4 lg:grid-cols-3">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="siem-auto-programs">Auto-connect programs</Label>
            <Input
              id="siem-auto-programs"
              value={policyDraft.autoConnectPrograms}
              onChange={(event) =>
                setPolicyDraft((p) => ({
                  ...p,
                  autoConnectPrograms: event.target.value,
                }))
              }
              disabled={!isAdmin}
              placeholder="nginx, haproxy"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="siem-approval-programs">
              Approval-required programs
            </Label>
            <Input
              id="siem-approval-programs"
              value={policyDraft.approvalRequiredPrograms}
              onChange={(event) =>
                setPolicyDraft((p) => ({
                  ...p,
                  approvalRequiredPrograms: event.target.value,
                }))
              }
              disabled={!isAdmin}
              placeholder="postgresql, mysql"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="siem-blocked-programs">Blocked programs</Label>
            <Input
              id="siem-blocked-programs"
              value={policyDraft.blockedPrograms}
              onChange={(event) =>
                setPolicyDraft((p) => ({
                  ...p,
                  blockedPrograms: event.target.value,
                }))
              }
              disabled={!isAdmin}
              placeholder="temenos-t24"
            />
          </div>
        </div>
        {policy?.updated_at && (
          <div className="font-mono text-xs text-text-muted">
            Updated {formatTime(policy.updated_at)}
          </div>
        )}
      </Panel>

      <Panel
        title="Collector deployment"
        eyebrow="OTEL EDGE"
        toneAccent={
          candidateTotals.failed > 0
            ? "critical"
            : candidateTotals.queued > 0
              ? "warning"
              : "accent"
        }
      >
        <div className="grid gap-3 md:grid-cols-4">
          <KpiTile
            label="Collectors"
            value={collectors.length.toLocaleString()}
            size="sm"
            loading={loading}
          />
          <KpiTile
            label="Rendered"
            value={candidateTotals.rendered.toLocaleString()}
            size="sm"
            tone="info"
            loading={loading}
          />
          <KpiTile
            label="Queued"
            value={candidateTotals.queued.toLocaleString()}
            size="sm"
            tone="warning"
            loading={loading}
          />
          <KpiTile
            label="Deployed"
            value={candidateTotals.deployed.toLocaleString()}
            size="sm"
            tone="healthy"
            loading={loading}
          />
        </div>
        <div className="grid gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="siem-otel-endpoint">OTel exporter endpoint</Label>
            <Input
              id="siem-otel-endpoint"
              value={deploymentDraft.endpoint}
              onChange={(event) =>
                setDeploymentDraft((draft) => ({
                  ...draft,
                  endpoint: event.target.value,
                }))
              }
              disabled={!isAdmin}
              placeholder="controlone.local:4317"
            />
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="siem-edge-collector">Edge collector</Label>
            <SelectField
              id="siem-edge-collector"
              value={deploymentDraft.collectorId}
              onChange={(event) =>
                setDeploymentDraft((draft) => ({
                  ...draft,
                  collectorId: event.target.value,
                }))
              }
              disabled={!isAdmin || collectors.length === 0}
            >
              {collectors.length === 0 ? (
                <option value="">No registered collectors</option>
              ) : (
                collectors.map((collector) => (
                  <option
                    key={collector.collector_id}
                    value={collector.collector_id}
                  >
                    {collector.display_name || collector.collector_id}
                  </option>
                ))
              )}
            </SelectField>
          </div>
        </div>
        <DataTable
          columns={candidateColumns}
          rows={candidates}
          rowKey={(row) => row.id}
          loading={loading}
          compact
          empty={
            <EmptyState
              icon={<DatabaseZap />}
              title="No collector candidates"
              description="Approved source proposals can be rendered into collector config candidates."
            />
          }
        />
        {candidateDetail && (
          <div className="space-y-3 rounded-lg border border-border-subtle bg-surface p-4">
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="space-y-1">
                <div className="text-sm font-semibold text-foreground">
                  Candidate config review
                </div>
                <div className="font-mono text-xs text-text-muted">
                  {candidateDetail.config_version}
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <StatusTag tone={statusTone(candidateDetail.status)}>
                  {formatStatus(candidateDetail.status)}
                </StatusTag>
                {candidateDetail.status === "rendered" && (
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={
                      !isAdmin || busyCandidateId === candidateDetail.id
                    }
                    onClick={() => approveCandidate(candidateDetail)}
                  >
                    <Check className="h-4 w-4" />
                    Approve
                  </Button>
                )}
                {candidateDetail.status === "approved" && (
                  <Button
                    size="sm"
                    variant="secondary"
                    disabled={
                      !isAdmin || busyCandidateId === candidateDetail.id
                    }
                    onClick={() => queueCandidate(candidateDetail)}
                  >
                    <DatabaseZap className="h-4 w-4" />
                    Queue
                  </Button>
                )}
              </div>
            </div>
            <div className="flex flex-wrap gap-2">
              {candidateDetail.sources.map((source) => (
                <StatusTag
                  key={`${candidateDetail.id}:${source.source_id}:${source.mode}:${source.collect_mode ?? ""}`}
                  tone="info"
                >
                  {source.source_id} / {source.mode}
                  {source.collect_mode
                    ? ` / ${formatStatus(source.collect_mode)}`
                    : ""}
                </StatusTag>
              ))}
            </div>
            {candidateDetail.warnings &&
              candidateDetail.warnings.length > 0 && (
                <div className="rounded-md border border-warning-500/30 bg-warning-500/10 p-3 text-xs text-warning-700">
                  {candidateDetail.warnings.join(" | ")}
                </div>
              )}
            <pre className="max-h-[420px] overflow-auto rounded-md border border-border-subtle bg-elevated p-3 text-xs leading-5 text-text-secondary">
              <code>{candidateDetail.yaml}</code>
            </pre>
          </div>
        )}
      </Panel>

      <Panel
        title="Source proposals"
        eyebrow="DISCOVERY"
        actions={
          <SelectField
            value={statusFilter}
            onChange={(event) => {
              setProposalOffset(0);
              setStatusFilter(event.target.value as ProposalStatusFilter);
            }}
            className="min-w-[180px]"
            aria-label="Filter source proposals"
          >
            <option value="all">All statuses</option>
            <option value="approval_required">Approval required</option>
            <option value="auto_eligible">Auto eligible</option>
            <option value="approved">Approved</option>
            <option value="rejected">Rejected</option>
            <option value="privacy_blocked">Privacy blocked</option>
          </SelectField>
        }
      >
        <form
          className="grid gap-2 md:grid-cols-[minmax(0,1fr)_auto_auto]"
          onSubmit={applyProposalSearch}
        >
          <div className="space-y-1">
            <Label htmlFor="source-proposal-search">Search proposals</Label>
            <Input
              id="source-proposal-search"
              value={proposalSearchDraft}
              onChange={(event) => setProposalSearchDraft(event.target.value)}
              placeholder="node, program, source, risk, label"
            />
          </div>
          <Button
            type="submit"
            variant="secondary"
            disabled={loading}
            className="self-end"
          >
            Search
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={
              loading ||
              (!proposalSearchDraft && !proposalQuery && statusFilter === "all")
            }
            onClick={clearProposalSearch}
            className="self-end"
          >
            Clear
          </Button>
        </form>
        <DataTable
          columns={proposalColumns}
          rows={proposals}
          rowKey={(row) => row.id}
          loading={loading}
          compact
          empty={
            <EmptyState
              icon={<DatabaseZap />}
              title="No source proposals"
              description="No local connector proposals were returned for this tenant."
            />
          }
        />
        <div className="flex flex-col gap-2 border-y border-border-subtle py-3 text-xs text-text-secondary md:flex-row md:items-center md:justify-between">
          <span>
            Showing {proposalPageStart.toLocaleString()}-
            {proposalPageEnd.toLocaleString()} of{" "}
            {proposalPageTotal.toLocaleString()} source proposals
          </span>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={loading || proposalPagination?.prevOffset == null}
              onClick={() => {
                if (proposalPagination?.prevOffset != null) {
                  setProposalOffset(proposalPagination.prevOffset);
                }
              }}
            >
              Previous
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={loading || proposalPagination?.nextOffset == null}
              onClick={() => {
                if (proposalPagination?.nextOffset != null) {
                  setProposalOffset(proposalPagination.nextOffset);
                }
              }}
            >
              Next
            </Button>
          </div>
        </div>
      </Panel>

      <Panel
        title="Source health"
        eyebrow="RUNTIME"
        toneAccent={healthTotals.degraded > 0 ? "warning" : "healthy"}
      >
        <div className="grid gap-3 md:grid-cols-3">
          <KpiTile
            label="Sources"
            value={healthPageTotal.toLocaleString()}
            size="sm"
            loading={loading}
          />
          <KpiTile
            label="Events received"
            value={healthTotals.received.toLocaleString()}
            size="sm"
            tone="info"
            loading={loading}
          />
          <KpiTile
            label="Degraded"
            value={healthTotals.degraded.toLocaleString()}
            size="sm"
            tone={healthTotals.degraded > 0 ? "warning" : "healthy"}
            loading={loading}
          />
        </div>
        <form
          className="grid gap-2 md:grid-cols-[minmax(0,1fr)_220px_auto_auto]"
          onSubmit={applyHealthSearch}
        >
          <div className="space-y-1">
            <Label htmlFor="source-health-search">Search source health</Label>
            <Input
              id="source-health-search"
              value={healthSearchDraft}
              onChange={(event) => setHealthSearchDraft(event.target.value)}
              placeholder="node, source, collector, approval, label"
            />
          </div>
          <SelectField
            id="source-health-state"
            label="Source state"
            value={healthStateFilter}
            onChange={(event) => {
              setHealthOffset(0);
              setHealthStateFilter(
                event.target.value as SourceHealthStateFilter,
              );
            }}
          >
            {SOURCE_HEALTH_STATE_OPTIONS.map((option) => (
              <option key={option.value} value={option.value}>
                {option.label}
              </option>
            ))}
          </SelectField>
          <Button
            type="submit"
            variant="secondary"
            disabled={loading}
            className="self-end"
          >
            Search
          </Button>
          <Button
            type="button"
            variant="outline"
            disabled={
              loading ||
              (!healthSearchDraft &&
                !healthQuery &&
                healthStateFilter === "all")
            }
            onClick={clearHealthSearch}
            className="self-end"
          >
            Clear
          </Button>
        </form>
        <div className="flex flex-col gap-2 border-y border-border-subtle py-3 text-xs text-text-secondary md:flex-row md:items-center md:justify-between">
          <span>
            Showing {healthPageStart.toLocaleString()}-
            {healthPageEnd.toLocaleString()} of{" "}
            {healthPageTotal.toLocaleString()} runtime source rows
          </span>
          <div className="flex gap-2">
            <Button
              size="sm"
              variant="outline"
              disabled={loading || healthPagination?.prevOffset == null}
              onClick={() => {
                if (healthPagination?.prevOffset != null) {
                  setHealthOffset(healthPagination.prevOffset);
                }
              }}
            >
              Previous
            </Button>
            <Button
              size="sm"
              variant="outline"
              disabled={loading || healthPagination?.nextOffset == null}
              onClick={() => {
                if (healthPagination?.nextOffset != null) {
                  setHealthOffset(healthPagination.nextOffset);
                }
              }}
            >
              Next
            </Button>
          </div>
        </div>
        <DataTable
          columns={healthColumns}
          rows={health}
          rowKey={(row, index) =>
            sourceHealthStableKey(row) ||
            `${row.collector_id}:${row.source_id}:${index}`
          }
          loading={loading}
          compact
          empty={
            <EmptyState
              icon={<ShieldCheck />}
              title="No source health"
              description="No runtime source health has been reported yet."
            />
          }
        />
        {selectedHealth && (
          <section
            aria-label="Source health detail"
            className="space-y-4 border-t border-border-subtle pt-4"
          >
            <div className="flex flex-wrap items-start justify-between gap-3">
              <div className="space-y-1">
                <div className="text-sm font-semibold text-foreground">
                  Source detail
                </div>
                <div className="font-mono text-xs text-text-muted">
                  {sourceHealthStableKey(selectedHealth)}
                </div>
              </div>
              <div className="flex flex-wrap gap-2">
                <StatusTag tone={statusTone(selectedHealth.coverage_state)}>
                  {formatStatus(selectedHealth.coverage_state)}
                </StatusTag>
                {sourceHealthInvestigationAction(selectedHealth) && (
                  <Button
                    type="button"
                    size="sm"
                    variant="secondary"
                    disabled={
                      !canOpenSourceHealthInvestigation ||
                      !selectedHealth.runtime_state_id ||
                      !sourceHealthInvestigationAction(selectedHealth)
                        ?.enabled ||
                      busyHealthInvestigationKey ===
                        sourceHealthStableKey(selectedHealth)
                    }
                    onClick={() =>
                      openSourceHealthInvestigation(selectedHealth)
                    }
                  >
                    <ShieldAlert className="h-4 w-4" />
                    {sourceHealthInvestigationAction(selectedHealth)?.label ??
                      "Open investigation"}
                  </Button>
                )}
                <Button
                  type="button"
                  size="sm"
                  variant="outline"
                  onClick={() => {
                    setSelectedHealth(null);
                    setSourceHealthInvestigationCase(null);
                  }}
                >
                  Close
                </Button>
              </div>
            </div>

            {sourceHealthInvestigationAction(selectedHealth)?.description && (
              <div className="text-xs text-text-secondary">
                {sourceHealthInvestigationAction(selectedHealth)?.description}
              </div>
            )}
            {sourceHealthInvestigationCase && (
              <div className="flex flex-wrap items-center gap-2 border-l-2 border-state-info/60 pl-3 text-xs text-text-secondary">
                <span>Investigation case opened</span>
                <span className="font-mono text-foreground">
                  {sourceHealthInvestigationCase.case_id}
                </span>
                {sourceHealthInvestigationCase.export_url && (
                  <Button
                    type="button"
                    size="sm"
                    variant="link"
                    className="h-auto px-0 text-xs"
                    loading={
                      exportingSourceHealthCaseId ===
                      sourceHealthInvestigationCase.case_id
                    }
                    onClick={() =>
                      void downloadSourceHealthCaseExport(
                        sourceHealthInvestigationCase,
                      )
                    }
                  >
                    <Download className="h-3.5 w-3.5" />
                    Export
                  </Button>
                )}
              </div>
            )}

            <div className="grid gap-4 lg:grid-cols-3">
              <div className="space-y-2">
                <div className="text-xs font-semibold uppercase text-text-muted">
                  Identity
                </div>
                <dl className="space-y-1 text-xs">
                  {[
                    ["source", selectedHealth.source_id],
                    ["display", selectedHealth.display_name],
                    ["node", selectedHealth.node_id],
                    ["collector", selectedHealth.collector_id],
                    ["receiver", selectedHealth.receiver_id],
                    ["parser", selectedHealth.parser_id],
                    ["config", selectedHealth.config_version],
                    ["content", selectedHealth.content_version],
                    ["approval", selectedHealth.approval_id],
                  ].map(([label, value]) => (
                    <div
                      key={label}
                      className="grid grid-cols-[84px_1fr] gap-2"
                    >
                      <dt className="text-text-muted">{label}</dt>
                      <dd className="break-all font-mono text-text-secondary">
                        {value || "-"}
                      </dd>
                    </div>
                  ))}
                </dl>
              </div>

              <div className="space-y-2">
                <div className="text-xs font-semibold uppercase text-text-muted">
                  Runtime
                </div>
                <dl className="space-y-1 text-xs">
                  {[
                    ["last event", formatTime(selectedHealth.last_event_at)],
                    ["last parsed", formatTime(selectedHealth.last_parsed_at)],
                    ["last health", formatTime(selectedHealth.last_health_at)],
                    ["last error", selectedHealth.last_error || "-"],
                  ].map(([label, value]) => (
                    <div
                      key={label}
                      className="grid grid-cols-[84px_1fr] gap-2"
                    >
                      <dt className="text-text-muted">{label}</dt>
                      <dd className="break-words text-text-secondary">
                        {value}
                      </dd>
                    </div>
                  ))}
                </dl>
                {sourceHealthEvidenceLine(selectedHealth) && (
                  <div className="text-xs text-text-secondary">
                    {sourceHealthEvidenceLine(selectedHealth)}
                  </div>
                )}
              </div>

              <div className="space-y-2">
                <div className="text-xs font-semibold uppercase text-text-muted">
                  Runtime metrics
                </div>
                {selectedHealthMetrics.length > 0 ? (
                  <dl className="space-y-1 text-xs">
                    {selectedHealthMetrics.map(([label, value]) => (
                      <div
                        key={label}
                        className="grid grid-cols-[minmax(96px,1fr)_auto] gap-2"
                      >
                        <dt className="text-text-muted">
                          {formatStatus(label)}
                        </dt>
                        <dd className="font-mono tabular-nums text-text-secondary">
                          {value.toLocaleString()}
                        </dd>
                      </div>
                    ))}
                  </dl>
                ) : (
                  <div className="text-xs text-text-muted">
                    No runtime metrics reported.
                  </div>
                )}
              </div>
            </div>

            {selectedHealthLabels.length > 0 && (
              <div className="space-y-2">
                <div className="text-xs font-semibold uppercase text-text-muted">
                  Evidence labels
                </div>
                <div className="flex flex-wrap gap-2">
                  {selectedHealthLabels.map(([label, value]) => (
                    <span
                      key={label}
                      className="max-w-full break-all font-mono text-xs text-text-secondary"
                    >
                      {label}={value}
                    </span>
                  ))}
                </div>
              </div>
            )}
          </section>
        )}
      </Panel>

      <Panel
        title="Source investigations"
        eyebrow="SOC CASES"
        toneAccent={sourceHealthCaseTotal > 0 ? "warning" : "healthy"}
      >
        <div className="flex flex-col gap-2 text-xs text-text-secondary md:flex-row md:items-center md:justify-between">
          <span>
            {sourceHealthCaseTotal.toLocaleString()} SIEM source-health case
            {sourceHealthCaseTotal === 1 ? "" : "s"}
          </span>
          <Button size="sm" variant="outline" disabled={loading} onClick={load}>
            <RefreshCw className="h-4 w-4" />
            Refresh
          </Button>
        </div>
        {sourceHealthCases.length > 0 ? (
          <div className="divide-y divide-border-subtle border-y border-border-subtle">
            {sourceHealthCases.map((item) => (
              <div
                key={item.case_id}
                className="grid gap-3 py-3 text-sm lg:grid-cols-[minmax(0,1fr)_auto]"
              >
                <div className="min-w-0 space-y-1">
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="font-medium text-foreground">
                      {item.title || item.summary || item.case_id}
                    </span>
                    <StatusTag tone={severityTone(item.severity)}>
                      {formatStatus(item.severity)}
                    </StatusTag>
                    <StatusTag tone={statusTone(item.status)}>
                      {formatStatus(item.status)}
                    </StatusTag>
                  </div>
                  <div className="flex flex-wrap gap-2 font-mono text-xs text-text-muted">
                    <span>{item.case_id}</span>
                    {item.trigger_event_type && (
                      <span>{item.trigger_event_type}</span>
                    )}
                    {item.node_id && <span>node:{item.node_id}</span>}
                  </div>
                  <div className="text-xs text-text-secondary">
                    Updated {formatTime(item.updated_at || item.created_at)}
                  </div>
                  {sourceHealthCaseEvidenceLine(item) && (
                    <div className="text-xs text-text-secondary">
                      {sourceHealthCaseEvidenceLine(item)}
                    </div>
                  )}
                  {sourceHealthCaseEvidenceRefsLine(item) && (
                    <div className="break-all font-mono text-xs text-text-muted">
                      {sourceHealthCaseEvidenceRefsLine(item)}
                    </div>
                  )}
                  {(item.notes?.[0]?.note ?? "").trim() && (
                    <div className="text-xs text-text-secondary">
                      Latest note: {item.notes?.[0]?.note}
                    </div>
                  )}
                  {canOpenSourceHealthInvestigation && (
                    <form
                      className="grid gap-2 pt-1 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end"
                      onSubmit={(event) =>
                        submitSourceHealthCaseNote(event, item)
                      }
                    >
                      <div className="min-w-0">
                        <Label
                          htmlFor={`source-case-note-${item.case_id}`}
                          className="sr-only"
                        >
                          Investigation note for {item.case_id}
                        </Label>
                        <textarea
                          id={`source-case-note-${item.case_id}`}
                          className="flex min-h-[68px] w-full rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground placeholder:text-text-muted focus-visible:border-border-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/30 disabled:opacity-50"
                          rows={2}
                          value={sourceHealthCaseNoteDrafts[item.case_id] ?? ""}
                          onChange={(event) =>
                            setSourceHealthCaseNoteDrafts((current) => ({
                              ...current,
                              [item.case_id]: event.target.value,
                            }))
                          }
                          placeholder="Add cited triage note"
                          disabled={busySourceHealthCaseNoteId === item.case_id}
                        />
                      </div>
                      <Button
                        type="submit"
                        size="sm"
                        variant="outline"
                        disabled={
                          busySourceHealthCaseNoteId === item.case_id ||
                          !(
                            sourceHealthCaseNoteDrafts[item.case_id] ?? ""
                          ).trim()
                        }
                      >
                        Add note
                      </Button>
                    </form>
                  )}
                </div>
                <div className="flex items-start justify-end">
                  {item.export_url ? (
                    <Button
                      type="button"
                      size="sm"
                      variant="outline"
                      loading={exportingSourceHealthCaseId === item.case_id}
                      onClick={() => void downloadSourceHealthCaseExport(item)}
                    >
                      <Download className="h-4 w-4" />
                      Export
                    </Button>
                  ) : (
                    <span className="text-xs text-text-muted">No export</span>
                  )}
                </div>
              </div>
            ))}
          </div>
        ) : (
          <EmptyState
            icon={<ShieldCheck />}
            title="No source investigations"
            description="Degraded source-health rows can open SOC cases from the source detail panel."
          />
        )}
      </Panel>

      <ConfirmModal
        open={Boolean(confirm)}
        title={confirmTitle}
        body={
          confirm
            ? `${confirm.proposal.program} / ${confirm.proposal.source_id || confirm.proposal.proposal_id}`
            : undefined
        }
        confirmLabel={
          confirm?.kind === "approve"
            ? "Approve"
            : confirm?.kind === "privacy_block"
              ? "Privacy block"
              : "Reject"
        }
        variant={confirm?.kind === "approve" ? "default" : "danger"}
        onConfirm={runProposalDecision}
        onCancel={() => setConfirm(null)}
      >
        {confirm?.kind === "approve" && (
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="siem-approval-collect-mode">Collection mode</Label>
            <SelectField
              id="siem-approval-collect-mode"
              value={approvalCollectMode}
              onChange={(event) =>
                setApprovalCollectMode(
                  event.target.value as ContentPackSourceProposalCollectMode,
                )
              }
            >
              <option value="collect_raw">
                {COLLECT_MODE_LABELS.collect_raw}
              </option>
              <option value="metadata_only">
                {COLLECT_MODE_LABELS.metadata_only}
              </option>
              <option value="observe_only">
                {COLLECT_MODE_LABELS.observe_only}
              </option>
              <option value="collect_parsed">
                {COLLECT_MODE_LABELS.collect_parsed}
              </option>
              <option value="disabled">{COLLECT_MODE_LABELS.disabled}</option>
            </SelectField>
          </div>
        )}
      </ConfirmModal>
    </div>
  );
}
