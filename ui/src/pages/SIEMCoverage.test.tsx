import { describe, expect, it, beforeEach, vi } from "vitest";
import { render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SIEMCoverage } from "./SIEMCoverage";
import type {
  ContentPackEdgeCollector,
  ContentPackOTelConfigCandidate,
  ContentPackSourceHealthResponse,
  ContentPackSourceProposal,
  TenantConnectorPolicy,
} from "@/lib/api";

const mocks = vi.hoisted(() => {
  const listContentPackSourceProposals = vi.fn();
  const approveContentPackSourceProposal = vi.fn();
  const rejectContentPackSourceProposal = vi.fn();
  const getContentPackSourceHealth = vi.fn();
  const createContentPackSourceHealthInvestigation = vi.fn();
  const listSOCCases = vi.fn();
  const createSOCCaseNote = vi.fn();
  const exportSOCCase = vi.fn();
  const getTenantConnectorPolicy = vi.fn();
  const updateTenantConnectorPolicy = vi.fn();
  const listContentPackEdgeCollectors = vi.fn();
  const listContentPackOTelConfigCandidates = vi.fn();
  const getContentPackOTelConfigCandidate = vi.fn();
  const createContentPackOTelConfigCandidate = vi.fn();
  const approveContentPackOTelConfigCandidate = vi.fn();
  const queueContentPackOTelConfigCandidate = vi.fn();
  const toastSuccess = vi.fn();
  const toastError = vi.fn();
  const saveBlob = vi.fn();

  return {
    listContentPackSourceProposals,
    approveContentPackSourceProposal,
    rejectContentPackSourceProposal,
    getContentPackSourceHealth,
    createContentPackSourceHealthInvestigation,
    listSOCCases,
    createSOCCaseNote,
    exportSOCCase,
    getTenantConnectorPolicy,
    updateTenantConnectorPolicy,
    listContentPackEdgeCollectors,
    listContentPackOTelConfigCandidates,
    getContentPackOTelConfigCandidate,
    createContentPackOTelConfigCandidate,
    approveContentPackOTelConfigCandidate,
    queueContentPackOTelConfigCandidate,
    toastSuccess,
    toastError,
    saveBlob,
    apiClient: {
      listContentPackSourceProposals: (params: unknown) =>
        listContentPackSourceProposals(params),
      approveContentPackSourceProposal: (
        tenantId: string,
        proposalId: string,
        payload: unknown,
      ) => approveContentPackSourceProposal(tenantId, proposalId, payload),
      rejectContentPackSourceProposal: (
        tenantId: string,
        proposalId: string,
        payload: unknown,
      ) => rejectContentPackSourceProposal(tenantId, proposalId, payload),
      getContentPackSourceHealth: (tenantId: string, params: unknown) =>
        getContentPackSourceHealth(tenantId, params),
      createContentPackSourceHealthInvestigation: (
        tenantId: string,
        payload: unknown,
      ) => createContentPackSourceHealthInvestigation(tenantId, payload),
      listSOCCases: (params: unknown) => listSOCCases(params),
      createSOCCaseNote: (tenantId: string, caseId: string, payload: unknown) =>
        createSOCCaseNote(tenantId, caseId, payload),
      exportSOCCase: (caseId: string, tenantId: string) =>
        exportSOCCase(caseId, tenantId),
      getTenantConnectorPolicy: (tenantId: string) =>
        getTenantConnectorPolicy(tenantId),
      updateTenantConnectorPolicy: (tenantId: string, payload: unknown) =>
        updateTenantConnectorPolicy(tenantId, payload),
      listContentPackEdgeCollectors: (params: unknown) =>
        listContentPackEdgeCollectors(params),
      listContentPackOTelConfigCandidates: (params: unknown) =>
        listContentPackOTelConfigCandidates(params),
      getContentPackOTelConfigCandidate: (
        tenantId: string,
        candidateId: string,
      ) => getContentPackOTelConfigCandidate(tenantId, candidateId),
      createContentPackOTelConfigCandidate: (
        tenantId: string,
        payload: unknown,
      ) => createContentPackOTelConfigCandidate(tenantId, payload),
      approveContentPackOTelConfigCandidate: (
        tenantId: string,
        candidateId: string,
        payload: unknown,
      ) =>
        approveContentPackOTelConfigCandidate(tenantId, candidateId, payload),
      queueContentPackOTelConfigCandidate: (
        tenantId: string,
        candidateId: string,
        payload: unknown,
      ) => queueContentPackOTelConfigCandidate(tenantId, candidateId, payload),
    },
  };
});

vi.mock("@/hooks/useApiClient", () => ({
  useApiClient: () => mocks.apiClient,
}));

vi.mock("@/providers/AuthProvider", () => ({
  useAuth: () => ({
    profile: { roles: ["admin"] },
  }),
}));

vi.mock("@/providers/TenantProvider", () => ({
  useTenant: () => ({
    currentTenantId: "tenant-1",
    currentTenant: {
      id: "tenant-1",
      name: "Bank Tenant",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    },
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    success: mocks.toastSuccess,
    error: mocks.toastError,
  },
}));

vi.mock("@/lib/download", () => ({
  saveBlob: mocks.saveBlob,
}));

function expectKpiValue(label: string, value: string): void {
  const tile = screen
    .getAllByText(label)
    .map((item) => item.closest(".group"))
    .find((item): item is HTMLElement => item instanceof HTMLElement);
  expect(tile).toBeTruthy();
  expect(within(tile as HTMLElement).getByText(value)).toBeInTheDocument();
}

const samplePolicy: TenantConnectorPolicy = {
  tenant_id: "tenant-1",
  allow_medium_risk: false,
  allow_high_risk: false,
  auto_connect_programs: ["nginx"],
  approval_required_programs: ["postgres"],
  blocked_programs: ["temenos-t24"],
  updated_at: "2026-05-28T10:00:00Z",
};

const sampleProposal: ContentPackSourceProposal = {
  id: "proposal-row-1",
  tenant_id: "tenant-1",
  node_id: "node-1",
  proposal_id: "proposal-1",
  kind: "local_file",
  program: "nginx",
  source_id: "nginx.access",
  collector_type: "filelog",
  formatter: "nginx",
  status: "approval_required",
  confidence: 0.94,
  risk: "medium",
  auto_connect_eligible: false,
  requires_approval: true,
  reason: "Observed nginx listener and access log path",
  paths: ["/var/log/nginx/access.log"],
  evidence: ["tcp:443", "package:nginx"],
  labels: {
    policy_decision: "requires_approval",
    content_pack_source_id: "nginx.access",
  },
  first_seen_at: "2026-05-28T09:00:00Z",
  last_seen_at: "2026-05-28T10:00:00Z",
  created_at: "2026-05-28T09:00:00Z",
  updated_at: "2026-05-28T10:00:00Z",
};

const sampleApprovedProposal: ContentPackSourceProposal = {
  ...sampleProposal,
  status: "approved",
  collect_mode: "collect_raw",
  approved_by_subject: "soc.admin",
  approved_at: "2026-05-28T10:02:00Z",
};

const sampleHealth: ContentPackSourceHealthResponse = {
  tenant_id: "tenant-1",
  generated_at: "2026-05-28T10:01:00Z",
  totals: {
    sources: 1,
    collectors_reporting: 1,
    by_state: { collecting: 1 },
  },
  pagination: {
    total: 2,
    count: 1,
    limit: 100,
    offset: 0,
    nextOffset: 1,
    prevOffset: null,
  },
  items: [
    {
      runtime_state_id: "runtime-state-1",
      source_instance_id: "node-1/nginx.access",
      collector_id: "otelcol-1",
      source_id: "nginx.access",
      receiver_id: "filelog/nginx_access",
      node_id: "node-1",
      display_name: "NGINX access",
      coverage_state: "collecting",
      approval_required: true,
      approval_id: "proposal-row-1",
      metrics: {
        events_received: 120,
        events_parsed: 118,
        events_dropped: 0,
        queue_depth: 0,
        retry_count: 0,
      },
      labels: {
        collect_mode: "collect_parsed",
        raw_message_retained: "false",
      },
      last_health_at: "2026-05-28T10:00:00Z",
    },
  ],
};

const sampleCollector: ContentPackEdgeCollector = {
  id: "collector-row-1",
  tenant_id: "tenant-1",
  collector_id: "edge-otel-1",
  kind: "otel",
  display_name: "Edge OTel 1",
  endpoint: "edge-otel-1.local",
  version: "0.128.0",
  status: "healthy",
  running_config_version: "sha256:old",
  last_heartbeat_at: "2026-05-28T10:00:00Z",
  created_at: "2026-05-28T09:00:00Z",
  updated_at: "2026-05-28T10:00:00Z",
};

const sampleCandidate: ContentPackOTelConfigCandidate = {
  id: "candidate-1",
  tenant_id: "tenant-1",
  status: "rendered",
  config_version: "sha256:rendered",
  collector_id: "edge-otel-1",
  endpoint: "controlone.local:4317",
  source_ids: ["nginx.access"],
  created_at: "2026-05-28T10:03:00Z",
  updated_at: "2026-05-28T10:03:00Z",
};

const sampleSourceHealthCase = {
  case_id: "case-existing",
  tenant_id: "tenant-1",
  node_id: "node-1",
  title: "Parser failure investigation opened for Postgres audit",
  status: "open",
  severity: "high",
  source: "ai_investigation",
  trigger_type: "siem_source_health",
  trigger_event_type: "content_pack.source_health.parser_failed",
  dedup_key:
    "c1:siem-source-health:v1:tenant-1:node-1/postgres.audit:parser_failed",
  summary: "Parser failure investigation opened for Postgres audit",
  evidence: {
    source_id: "postgres.audit",
    coverage_state: "parser_failed",
    parser_id: "postgres.audit.otel",
    collector_id: "edge-parser-1",
    last_error: "grok parse failed on timestamp",
  },
  evidence_refs: [
    {
      id: "content_pack_source_runtime_state:runtime-state-1",
      kind: "content_pack_source_runtime_state",
    },
  ],
  notes: [
    {
      id: "note-existing",
      tenant_id: "tenant-1",
      case_id: "case-existing",
      note: "Parser owner is checking the deployed grok pattern.",
      citations: [
        {
          id: "content_pack_source_runtime_state:runtime-state-1",
          kind: "content_pack_source_runtime_state",
        },
      ],
      audit_id: "audit-existing",
      created_at: "2026-05-28T10:06:30Z",
    },
  ],
  export_url: "/api/v1/soc/cases/case-existing/export?tenant_id=tenant-1",
  created_at: "2026-05-28T10:05:00Z",
  updated_at: "2026-05-28T10:06:00Z",
};

describe("SIEMCoverage", () => {
  beforeEach(() => {
    mocks.listContentPackSourceProposals.mockReset();
    mocks.listContentPackSourceProposals.mockResolvedValue({
      data: [sampleProposal],
      pagination: {
        total: 1,
        count: 1,
        limit: 100,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });
    mocks.approveContentPackSourceProposal.mockReset();
    mocks.approveContentPackSourceProposal.mockResolvedValue(undefined);
    mocks.rejectContentPackSourceProposal.mockReset();
    mocks.rejectContentPackSourceProposal.mockResolvedValue(undefined);
    mocks.getContentPackSourceHealth.mockReset();
    mocks.getContentPackSourceHealth.mockResolvedValue(sampleHealth);
    mocks.createContentPackSourceHealthInvestigation.mockReset();
    mocks.createContentPackSourceHealthInvestigation.mockResolvedValue({
      case_id: "case-1",
      case: {
        case_id: "case-1",
        title: "Parser failure investigation opened for NGINX access",
        status: "open",
        severity: "high",
        trigger_type: "siem_source_health",
        trigger_event_type: "content_pack.source_health.parser_failed",
        summary: "Parser failure investigation opened for NGINX access",
        export_url: "/api/v1/soc/cases/case-1/export?tenant_id=tenant-1",
      },
    });
    mocks.listSOCCases.mockReset();
    mocks.listSOCCases.mockResolvedValue({
      data: [sampleSourceHealthCase],
      pagination: {
        total: 1,
        count: 1,
        limit: 5,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });
    mocks.createSOCCaseNote.mockReset();
    mocks.createSOCCaseNote.mockResolvedValue({
      id: "note-1",
      tenant_id: "tenant-1",
      case_id: "case-existing",
      note: "Parser owner confirmed timestamp format drift.",
      citations: [
        {
          id: "content_pack_source_runtime_state:runtime-state-1",
          kind: "content_pack_source_runtime_state",
        },
      ],
      audit_id: "audit-note-1",
      created_at: "2026-05-28T10:07:00Z",
      guardrails: ["tenant_scoped", "source_row_citations"],
    });
    mocks.exportSOCCase.mockReset();
    mocks.exportSOCCase.mockResolvedValue({
      export_version: "soc-case-export-v1",
      generated_at: "2026-05-28T10:08:00Z",
      tenant_id: "tenant-1",
      case: sampleSourceHealthCase,
      evidence: sampleSourceHealthCase.evidence_refs,
      notes: sampleSourceHealthCase.notes,
      guardrails: ["tenant_scoped", "source_row_citations"],
    });
    mocks.getTenantConnectorPolicy.mockReset();
    mocks.getTenantConnectorPolicy.mockResolvedValue(samplePolicy);
    mocks.updateTenantConnectorPolicy.mockReset();
    mocks.updateTenantConnectorPolicy.mockResolvedValue({
      ...samplePolicy,
      allow_medium_risk: true,
      auto_connect_programs: ["nginx", "redis"],
    });
    mocks.listContentPackEdgeCollectors.mockReset();
    mocks.listContentPackEdgeCollectors.mockResolvedValue({
      data: [sampleCollector],
      pagination: {
        total: 1,
        count: 1,
        limit: 200,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });
    mocks.listContentPackOTelConfigCandidates.mockReset();
    mocks.listContentPackOTelConfigCandidates.mockResolvedValue({
      data: [],
      pagination: {
        total: 0,
        count: 0,
        limit: 200,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });
    mocks.getContentPackOTelConfigCandidate.mockReset();
    mocks.getContentPackOTelConfigCandidate.mockResolvedValue({
      ...sampleCandidate,
      sources: [
        {
          source_id: "nginx.access",
          mode: "otel_filelog",
          receiver: "filelog",
          receiver_ids: ["filelog/controlone.nginx.access"],
          pipeline_id: "logs/controlone.nginx.access",
          pipeline_type: "logs",
          resource_processor_id: "resource/controlone.source.nginx.access",
          approval_ref: "proposal-row-1",
        },
      ],
      warnings: [],
      yaml: "receivers:\n  filelog/controlone.nginx.access: {}\n",
    });
    mocks.createContentPackOTelConfigCandidate.mockReset();
    mocks.createContentPackOTelConfigCandidate.mockResolvedValue({
      tenant_id: "tenant-1",
      generated_at: "2026-05-28T10:04:00Z",
      snapshot_id: "snapshot-1",
      snapshot_created_at: "2026-05-28T10:00:00Z",
      config_version: sampleCandidate.config_version,
      candidate_id: sampleCandidate.id,
      candidate_status: sampleCandidate.status,
      sources: [],
      config: {},
      yaml: "receivers: {}",
    });
    mocks.approveContentPackOTelConfigCandidate.mockReset();
    mocks.approveContentPackOTelConfigCandidate.mockResolvedValue(
      sampleCandidate,
    );
    mocks.queueContentPackOTelConfigCandidate.mockReset();
    mocks.queueContentPackOTelConfigCandidate.mockResolvedValue({
      ...sampleCandidate,
      status: "queued",
    });
    mocks.toastSuccess.mockReset();
    mocks.toastError.mockReset();
    mocks.saveBlob.mockReset();
  });

  it("loads policy, proposals, and source health for the active tenant", async () => {
    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 0,
      });
    });
    expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith("tenant-1", {
      limit: 100,
      offset: 0,
    });
    expect(mocks.getTenantConnectorPolicy).toHaveBeenCalledWith("tenant-1");
    expect(mocks.listContentPackEdgeCollectors).toHaveBeenCalledWith({
      tenantId: "tenant-1",
      limit: 200,
      offset: 0,
    });
    expect(mocks.listSOCCases).toHaveBeenCalledWith({
      tenantId: "tenant-1",
      triggerType: "siem_source_health",
      includeNotes: true,
      limit: 5,
      offset: 0,
    });

    expect(screen.getByText("Bank Tenant")).toBeInTheDocument();
    expect(screen.getByText("nginx")).toBeInTheDocument();
    expect(screen.getByText("NGINX access")).toBeInTheDocument();
    expect(
      screen.getByText("Showing 1-1 of 2 runtime source rows"),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Showing 1-1 of 1 source proposals"),
    ).toBeInTheDocument();
    expect(
      screen.getAllByText("Collect parsed only / raw retained: false").length,
    ).toBeGreaterThan(0);
    expect(screen.getByText("node-1/nginx.access")).toBeInTheDocument();
    expect(screen.getByText("approval: proposal-row-1")).toBeInTheDocument();
    expect(screen.getByText("otelcol-1")).toBeInTheDocument();
    expect(
      screen.getByRole("option", { name: /edge otel 1/i }),
    ).toBeInTheDocument();
    expect(screen.getAllByText("approval required").length).toBeGreaterThan(0);
    expect(
      screen.getByText(
        "Parser failure investigation opened for Postgres audit",
      ),
    ).toBeInTheDocument();
    expect(screen.getByText(/source: postgres\.audit/i)).toBeInTheDocument();
    expect(screen.getByText(/1 evidence ref/i)).toBeInTheDocument();
    expect(
      screen.getByText(/Parser owner is checking the deployed grok pattern/i),
    ).toBeInTheDocument();
  });

  it("adds a cited note to a source health investigation case", async () => {
    const user = userEvent.setup();
    render(<SIEMCoverage />);

    expect(
      await screen.findByText(
        "Parser failure investigation opened for Postgres audit",
      ),
    ).toBeInTheDocument();

    await user.type(
      screen.getByLabelText(/investigation note for case-existing/i),
      "Parser owner confirmed timestamp format drift.",
    );
    await user.click(screen.getByRole("button", { name: /add note/i }));

    await waitFor(() => {
      expect(mocks.createSOCCaseNote).toHaveBeenCalledWith(
        "tenant-1",
        "case-existing",
        {
          note: "Parser owner confirmed timestamp format drift.",
          citations: ["content_pack_source_runtime_state:runtime-state-1"],
        },
      );
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Source investigation note added",
    );
    expect(
      await screen.findByText(/Parser owner confirmed timestamp format drift/i),
    ).toBeInTheDocument();
  });

  it("downloads source health investigation exports through the authenticated API client", async () => {
    const user = userEvent.setup();
    render(<SIEMCoverage />);

    expect(
      await screen.findByText(
        "Parser failure investigation opened for Postgres audit",
      ),
    ).toBeInTheDocument();

    expect(
      screen.queryByRole("link", { name: /export/i }),
    ).not.toBeInTheDocument();
    const exportButtons = screen.getAllByRole("button", { name: /export/i });
    expect(exportButtons).toHaveLength(1);
    await user.click(exportButtons[0]);

    await waitFor(() => {
      expect(mocks.exportSOCCase).toHaveBeenCalledWith(
        "case-existing",
        "tenant-1",
      );
    });
    expect(mocks.saveBlob).toHaveBeenCalledTimes(1);
    expect(mocks.saveBlob.mock.calls[0][1]).toBe(
      "soc-case-case-existing-2026-05-28.json",
    );
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Source investigation export downloaded",
    );
  });

  it("uses source proposal summary totals for fleet-level proposal KPIs", async () => {
    mocks.listContentPackSourceProposals.mockResolvedValue({
      data: [sampleProposal],
      pagination: {
        total: 50,
        count: 1,
        limit: 100,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
      summary: {
        total: 50,
        by_status: {
          approval_required: 17,
          approved: 21,
          auto_eligible: 8,
          privacy_blocked: 4,
        },
      },
    });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 0,
      });
    });

    expectKpiValue("Proposals", "50");
    expectKpiValue("Approval required", "17");
    expectKpiValue("Approved", "21");
  });

  it("uses source health summary totals for runtime KPIs beyond the current page", async () => {
    mocks.getContentPackSourceHealth.mockResolvedValue({
      ...sampleHealth,
      totals: {
        sources: 50,
        collectors_reporting: 12,
        by_state: {
          collecting: 17,
          deployed: 4,
          backpressured: 3,
          parser_failed: 2,
          silent: 1,
        },
        metrics: {
          events_received: 5000,
          events_parsed: 4800,
        },
      },
      pagination: {
        total: 50,
        count: 1,
        limit: 100,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 0,
        },
      );
    });

    expectKpiValue("Collecting", "21");
    expectKpiValue("Events received", "5,000");
    expectKpiValue("Degraded", "6");
  });

  it("opens a source health detail drilldown with metrics and labels", async () => {
    const user = userEvent.setup();
    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(screen.getByText("NGINX access")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /^inspect$/i }));
    const detail = screen.getByRole("region", {
      name: /source health detail/i,
    });

    expect(within(detail).getByText("Source detail")).toBeInTheDocument();
    expect(within(detail).getByText("node-1/nginx.access")).toBeInTheDocument();
    expect(within(detail).getByText("Runtime metrics")).toBeInTheDocument();
    expect(within(detail).getByText("events received")).toBeInTheDocument();
    expect(within(detail).getByText("120")).toBeInTheDocument();
    expect(
      within(detail).getByText("collect_mode=collect_parsed"),
    ).toBeInTheDocument();

    await user.click(within(detail).getByRole("button", { name: /^close$/i }));
    expect(
      screen.queryByRole("region", { name: /source health detail/i }),
    ).not.toBeInTheDocument();
  });

  it("opens an investigation from degraded source health evidence", async () => {
    const user = userEvent.setup();
    mocks.getContentPackSourceHealth.mockResolvedValue({
      ...sampleHealth,
      totals: {
        sources: 1,
        collectors_reporting: 1,
        by_state: { parser_failed: 1 },
      },
      items: [
        {
          ...sampleHealth.items[0],
          runtime_state_id: "runtime-state-parser-1",
          coverage_state: "parser_failed",
          last_error: "grok parse failed on timestamp",
          metrics: {
            events_received: 42,
            parse_failures: 7,
          },
          recommended_actions: [
            {
              id: "open_source_health_investigation",
              action: "source_health.investigate",
              label: "Open parser investigation",
              description: "Parser failures are present for this source.",
              enabled: true,
            },
          ],
        },
      ],
    });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(screen.getByText("NGINX access")).toBeInTheDocument();
    });
    await user.click(screen.getByRole("button", { name: /^inspect$/i }));
    const detail = screen.getByRole("region", {
      name: /source health detail/i,
    });
    await user.click(
      within(detail).getByRole("button", {
        name: /open parser investigation/i,
      }),
    );

    await waitFor(() => {
      expect(
        mocks.createContentPackSourceHealthInvestigation,
      ).toHaveBeenCalledWith("tenant-1", {
        runtime_state_id: "runtime-state-parser-1",
        note: "Opened from SIEM source health: parser failed",
      });
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Source health investigation opened",
    );
    expect(within(detail).getByText("case-1")).toBeInTheDocument();
  });

  it("pages source health without losing tenant-scoped context", async () => {
    const user = userEvent.setup();
    mocks.getContentPackSourceHealth
      .mockResolvedValueOnce(sampleHealth)
      .mockResolvedValueOnce({
        ...sampleHealth,
        pagination: {
          total: 2,
          count: 1,
          limit: 100,
          offset: 1,
          nextOffset: null,
          prevOffset: 0,
        },
        items: [
          {
            ...sampleHealth.items[0],
            source_instance_id: "node-2/postgres.audit",
            collector_id: "otelcol-2",
            source_id: "postgres.audit",
            node_id: "node-2",
            display_name: "Postgres audit",
          },
        ],
      });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 0,
        },
      );
    });

    const sourceHealthPager = screen
      .getByText("Showing 1-1 of 2 runtime source rows")
      .closest("div") as HTMLElement;
    await user.click(
      within(sourceHealthPager).getByRole("button", { name: /^next$/i }),
    );

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 1,
        },
      );
    });
    expect(screen.getByText("Postgres audit")).toBeInTheDocument();
    expect(
      screen.getByText("Showing 2-2 of 2 runtime source rows"),
    ).toBeInTheDocument();
  });

  it("pages source proposals through the server-side pagination metadata", async () => {
    const user = userEvent.setup();
    mocks.listContentPackSourceProposals
      .mockResolvedValueOnce({
        data: [sampleProposal],
        pagination: {
          total: 2,
          count: 1,
          limit: 100,
          offset: 0,
          nextOffset: 1,
          prevOffset: null,
        },
      })
      .mockResolvedValueOnce({
        data: [
          {
            ...sampleApprovedProposal,
            id: "proposal-row-2",
            proposal_id: "proposal-2",
            program: "postgresql",
            source_id: "postgres.audit",
            reason: "Observed PostgreSQL audit logs",
          },
        ],
        pagination: {
          total: 2,
          count: 1,
          limit: 100,
          offset: 1,
          nextOffset: null,
          prevOffset: 0,
        },
      });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 0,
      });
    });

    const sourceProposalPager = screen
      .getByText("Showing 1-1 of 2 source proposals")
      .closest("div") as HTMLElement;
    await user.click(
      within(sourceProposalPager).getByRole("button", { name: /^next$/i }),
    );

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 1,
      });
    });
    expect(screen.getByText("postgresql")).toBeInTheDocument();
    expect(
      screen.getByText("Showing 2-2 of 2 source proposals"),
    ).toBeInTheDocument();
  });

  it("searches source health through the server-side query parameter", async () => {
    const user = userEvent.setup();
    mocks.getContentPackSourceHealth
      .mockResolvedValueOnce(sampleHealth)
      .mockResolvedValueOnce({
        ...sampleHealth,
        pagination: {
          total: 1,
          count: 1,
          limit: 100,
          offset: 0,
          nextOffset: null,
          prevOffset: null,
        },
      });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 0,
        },
      );
    });

    const healthSearchInput = screen.getByLabelText(/search source health/i);
    await user.type(healthSearchInput, "proposal-row-1");
    await user.click(
      within(healthSearchInput.closest("form") as HTMLElement).getByRole(
        "button",
        { name: /^search$/i },
      ),
    );

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 0,
          q: "proposal-row-1",
        },
      );
    });
  });

  it("filters source health by server-side runtime state", async () => {
    const user = userEvent.setup();
    mocks.getContentPackSourceHealth
      .mockResolvedValueOnce(sampleHealth)
      .mockResolvedValueOnce({
        ...sampleHealth,
        pagination: {
          total: 1,
          count: 1,
          limit: 100,
          offset: 0,
          nextOffset: null,
          prevOffset: null,
        },
      });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 0,
        },
      );
    });

    await user.selectOptions(
      screen.getByLabelText(/source state/i),
      "approval_required",
    );

    await waitFor(() => {
      expect(mocks.getContentPackSourceHealth).toHaveBeenCalledWith(
        "tenant-1",
        {
          limit: 100,
          offset: 0,
          state: "approval_required",
        },
      );
    });
  });

  it("filters source proposals through server-side status and search parameters", async () => {
    const user = userEvent.setup();
    mocks.listContentPackSourceProposals
      .mockResolvedValueOnce({
        data: [sampleProposal],
        pagination: {
          total: 1,
          count: 1,
          limit: 100,
          offset: 0,
          nextOffset: null,
          prevOffset: null,
        },
      })
      .mockResolvedValueOnce({
        data: [sampleProposal],
        pagination: {
          total: 1,
          count: 1,
          limit: 100,
          offset: 0,
          nextOffset: null,
          prevOffset: null,
        },
      })
      .mockResolvedValueOnce({
        data: [sampleProposal],
        pagination: {
          total: 1,
          count: 1,
          limit: 100,
          offset: 0,
          nextOffset: null,
          prevOffset: null,
        },
      });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 0,
      });
    });

    await user.selectOptions(
      screen.getByLabelText(/filter source proposals/i),
      "approval_required",
    );

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 0,
        status: "approval_required",
      });
    });

    const proposalSearchInput = screen.getByLabelText(/search proposals/i);
    await user.type(proposalSearchInput, "nginx");
    await user.click(
      within(proposalSearchInput.closest("form") as HTMLElement).getByRole(
        "button",
        { name: /^search$/i },
      ),
    );

    await waitFor(() => {
      expect(mocks.listContentPackSourceProposals).toHaveBeenCalledWith({
        tenantId: "tenant-1",
        limit: 100,
        offset: 0,
        q: "nginx",
        status: "approval_required",
      });
    });
  });

  it("updates the tenant connector policy from the policy panel", async () => {
    const user = userEvent.setup();
    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(screen.getByLabelText(/auto-connect programs/i)).toHaveValue(
        "nginx",
      );
    });

    await user.click(screen.getByRole("button", { name: /medium risk/i }));
    const autoPrograms = screen.getByLabelText(/auto-connect programs/i);
    await user.clear(autoPrograms);
    await user.type(autoPrograms, "nginx, Redis, nginx");
    await user.click(screen.getByRole("button", { name: /^save$/i }));

    await waitFor(() => {
      expect(mocks.updateTenantConnectorPolicy).toHaveBeenCalledWith(
        "tenant-1",
        {
          allow_medium_risk: true,
          allow_high_risk: false,
          auto_connect_programs: ["nginx", "redis"],
          approval_required_programs: ["postgres"],
          blocked_programs: ["temenos-t24"],
        },
      );
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Connector policy saved");
  });

  it("approves a source proposal from the proposal table", async () => {
    const user = userEvent.setup();
    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(
        screen.getByText("Observed nginx listener and access log path"),
      ).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /^approve$/i }));
    const dialog = screen.getByRole("dialog");
    await user.click(
      within(dialog).getByRole("button", { name: /^approve$/i }),
    );

    await waitFor(() => {
      expect(mocks.approveContentPackSourceProposal).toHaveBeenCalledWith(
        "tenant-1",
        "proposal-row-1",
        {
          note: "Approved from SIEM coverage",
          collect_mode: "collect_raw",
        },
      );
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith("Source proposal approved");
  });

  it("renders an approved parsed-only source proposal into an OTel config candidate", async () => {
    const user = userEvent.setup();
    mocks.listContentPackSourceProposals.mockResolvedValue({
      data: [
        {
          ...sampleApprovedProposal,
          collect_mode: "collect_parsed",
        },
      ],
      pagination: {
        total: 1,
        count: 1,
        limit: 100,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /^render$/i }),
      ).toBeInTheDocument();
    });

    await user.type(
      screen.getByLabelText(/otel exporter endpoint/i),
      "controlone.local:4317",
    );
    await user.click(screen.getByRole("button", { name: /^render$/i }));

    await waitFor(() => {
      expect(mocks.createContentPackOTelConfigCandidate).toHaveBeenCalledWith(
        "tenant-1",
        {
          endpoint: "controlone.local:4317",
          collector_id: "edge-otel-1",
          source_proposal_ids: ["proposal-row-1"],
        },
      );
    });
    expect(mocks.toastSuccess).toHaveBeenCalledWith(
      "Rendered candidate candidate-1",
    );
  });

  it("loads exact rendered YAML before approving a collector config candidate", async () => {
    const user = userEvent.setup();
    mocks.listContentPackOTelConfigCandidates.mockResolvedValue({
      data: [sampleCandidate],
      pagination: {
        total: 1,
        count: 1,
        limit: 200,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(screen.getByText("sha256:rendered")).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /^review$/i }));

    await waitFor(() => {
      expect(mocks.getContentPackOTelConfigCandidate).toHaveBeenCalledWith(
        "tenant-1",
        "candidate-1",
      );
    });
    expect(
      screen.getByText(/filelog\/controlone\.nginx\.access/i),
    ).toBeInTheDocument();

    const reviewPanel = screen
      .getByText("Candidate config review")
      .closest("div")?.parentElement?.parentElement;
    expect(reviewPanel).toBeTruthy();
    await user.click(
      within(reviewPanel as HTMLElement).getByRole("button", {
        name: /^approve$/i,
      }),
    );

    await waitFor(() => {
      expect(mocks.approveContentPackOTelConfigCandidate).toHaveBeenCalledWith(
        "tenant-1",
        "candidate-1",
        {
          note: "Approved from SIEM coverage",
          reviewed_config_version: "sha256:rendered",
        },
      );
    });
  });

  it("queues an approved collector config candidate with the expected config version", async () => {
    const user = userEvent.setup();
    mocks.listContentPackOTelConfigCandidates.mockResolvedValue({
      data: [
        {
          ...sampleCandidate,
          status: "approved",
          reviewed_config_version: sampleCandidate.config_version,
          reviewed_yaml_sha256: "rendered",
        },
      ],
      pagination: {
        total: 1,
        count: 1,
        limit: 200,
        offset: 0,
        nextOffset: null,
        prevOffset: null,
      },
    });

    render(<SIEMCoverage />);

    await waitFor(() => {
      expect(
        screen.getByRole("button", { name: /^queue$/i }),
      ).toBeInTheDocument();
    });

    await user.click(screen.getByRole("button", { name: /^queue$/i }));

    await waitFor(() => {
      expect(mocks.queueContentPackOTelConfigCandidate).toHaveBeenCalledWith(
        "tenant-1",
        "candidate-1",
        {
          collector_id: "edge-otel-1",
          note: "Queued from SIEM coverage",
          expected_config_version: "sha256:rendered",
        },
      );
    });
  });
});
