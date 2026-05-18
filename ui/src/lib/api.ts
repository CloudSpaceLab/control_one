// Default to same-origin (empty string) for production builds,
// localhost:8443 for local development
const DEFAULT_API_BASE_URL = import.meta.env.PROD ? '' : 'http://localhost:8443';
const HTTP_STATUS_UNAUTHORIZED = 401;

export type HypervisorProvider = 'aws' | 'azure' | 'vmware' | 'libvirt';

export interface ProviderCredential {
  id: string;
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  created_at: string;
  updated_at: string;
  rotated_at?: string;
}

export interface CreateProviderCredentialPayload {
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  config: Record<string, unknown>;
}

export interface HypervisorHost {
  id: string;
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  endpoint_url: string;
  credential_id?: string;
  datacenter?: string;
  labels: Record<string, unknown>;
  health_status: string;
  health_message?: string;
  last_verified_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateHypervisorHostPayload {
  tenant_id: string;
  provider: HypervisorProvider;
  name: string;
  endpoint_url: string;
  credential_id?: string;
  datacenter?: string;
  labels?: Record<string, unknown>;
}

export interface HypervisorHostVerifyResponse {
  host: HypervisorHost;
  status: string;
  message?: string;
}

export interface SeverityBreakdown {
  critical: number;
  high: number;
  medium: number;
  low: number;
  total: number;
}

export interface NodeCountsBreakdown {
  total: number;
  healthy: number;
  offline: number;
}

export interface ComplianceSnapshot {
  total: number;
  passed: number;
  failed: number;
}

// Executive Risk Dashboard Types
export interface RiskComponent {
  name: string;
  weight: number;
  raw_score: number;
  max_score: number;
  description: string;
}

export interface RiskScore {
  score: number;
  max_score: number;
  percent: number;
  trend_direction: 'up' | 'down' | 'stable';
  trend_delta: number;
  components: RiskComponent[];
  calculated_at: string;
}

export interface MTTDMetrics {
  severity: string;
  mean_minutes: number;
  median_minutes: number;
  p95_minutes: number;
  event_count: number;
  period: string;
  calculated_at: string;
}

export interface MTTRMetrics {
  severity: string;
  mean_minutes: number;
  median_minutes: number;
  p95_minutes: number;
  remediation_count: number;
  period: string;
  calculated_at: string;
}

export interface RemediationVelocity {
  period: string;
  period_count: number;
  remediations: number;
  avg_per_period: number;
  trend_direction: 'up' | 'down' | 'stable';
  trend_percent: number;
}

export interface FindingAging {
  severity: string;
  less_than_7_days: number;
  days_7_to_30: number;
  days_30_to_90: number;
  over_90_days: number;
  total_open: number;
}

export interface PeriodDelta {
  current: number;
  previous: number;
  delta_pct: number;
}

export interface SecurityEventSeriesPoint {
  ts: string;
  critical: number;
  high: number;
  total: number;
}

export interface ComplianceSeriesPoint {
  ts: string;
  pass_rate: number;
  total: number;
}

export interface DashboardOverview {
  tenant_id?: string;
  generated_at: string;
  node_counts: NodeCountsBreakdown;
  security_event_counts: SeverityBreakdown;
  health_incident_counts: SeverityBreakdown;
  compliance_summary: ComplianceSnapshot;
  rule_trigger_counts_24h: Record<string, number>;
  remediations_applied_24h: number;
  // Period-comparison fields (populated when ?period= is supplied)
  period?: string;
  security_event_delta?: PeriodDelta;
  rule_trigger_delta?: PeriodDelta;
  remediation_delta?: PeriodDelta;
  compliance_pass_rate?: number;
  compliance_pass_delta?: PeriodDelta;
  security_event_series?: SecurityEventSeriesPoint[];
  compliance_series?: ComplianceSeriesPoint[];
}

export type ControlRoomTone = 'healthy' | 'warning' | 'degraded' | 'critical' | 'info' | 'unknown';

export interface ControlRoomMetric {
  label: string;
  value: string;
  tone: ControlRoomTone;
  hint?: string;
  drilldown?: string;
}

export interface ControlRoomDrilldownItem {
  label: string;
  value: string;
  tone: ControlRoomTone;
  hint?: string;
  drilldown?: string;
}

export interface ControlRoomLane {
  id: 'server-health' | 'security' | 'app-db-health' | 'exposure' | 'ip-behavior' | 'patch-posture' | string;
  title: string;
  tone: ControlRoomTone;
  score: number;
  summary: string;
  primary_metric: ControlRoomMetric;
  secondary_metric: ControlRoomMetric;
  drilldown: string;
  updated_at: string;
  metrics: ControlRoomMetric[];
  items?: ControlRoomDrilldownItem[];
}

export interface ControlRoomIncident {
  id: string;
  title: string;
  severity: string;
  source: string;
  summary?: string;
  drilldown?: string;
  opened_at?: string;
}

export interface ControlRoomStaleWarning {
  id: string;
  tone: ControlRoomTone;
  message: string;
  drilldown?: string;
}

export interface ControlRoomIPFinding {
  id: string;
  source_ip?: string;
  country_code?: string;
  asn?: string;
  category: string;
  severity: string;
  score: number;
  reason: string;
  evidence?: Record<string, unknown>;
  last_seen_at: string;
  drilldown: string;
}

export interface ControlRoomIPBehavior {
  request_count: number;
  bytes_out: number;
  countries: IPBehaviorCountrySummary[];
  findings: ControlRoomIPFinding[];
}

export interface ControlRoomWebserver {
  id: string;
  node_id: string;
  kind: string;
  version?: string;
  service: string;
  config_path?: string;
  log_path?: string;
  error_log_path?: string;
  capture_ready: boolean;
  enforce_ready: boolean;
  vhosts?: Record<string, unknown>[];
  capabilities?: Record<string, unknown>;
  last_action?: ControlRoomWebserverAction;
  last_receipt?: ControlRoomWebserverReceipt;
  observed_at: string;
}

export interface ControlRoomWebserverAction {
  id: string;
  job_id?: string;
  action: string;
  status: string;
  result?: Record<string, unknown>;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

export interface ControlRoomWebserverReceipt {
  id: string;
  action: string;
  validation_status: string;
  reload_status: string;
  rollback_ref?: string;
  checksum_before?: string;
  checksum_after?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface ControlRoomWebservers {
  total: number;
  capture_ready: number;
  enforce_ready: number;
  instances: ControlRoomWebserver[];
}

export interface ControlRoomIsolationNode {
  id: string;
  hostname: string;
  mode: NetworkIsolationMode | string;
  active: boolean;
  expired: boolean;
  local_only: boolean;
  expires_at?: string;
  reason?: string;
  allowed_applications?: string[];
  allowlist_cidrs?: string[];
  updated_at?: string;
}

export interface ControlRoomIsolation {
  online: number;
  whitelist: number;
  airgapped: number;
  protected: number;
  whitelist_gaps: number;
  expired: number;
  expiring_soon: number;
  nodes: ControlRoomIsolationNode[];
}

export interface ControlRoomFirewallNode {
  node_id: string;
  hostname: string;
  firewall_type?: string;
  known: boolean;
  enabled: boolean;
  default_deny: boolean;
  stale: boolean;
  observed_at?: string;
}

export interface ControlRoomFirewall {
  enabled: number;
  disabled: number;
  unknown: number;
  default_deny: number;
  stale: number;
  nodes: ControlRoomFirewallNode[];
}

export interface ControlRoomAction {
  id: string;
  label: string;
  tone: ControlRoomTone;
  count: number;
  drilldown: string;
}

export interface ControlRoomOverview {
  tenant_id: string;
  generated_at: string;
  period: string;
  lanes: ControlRoomLane[];
  top_incidents: ControlRoomIncident[];
  stale_warnings: ControlRoomStaleWarning[];
  ip_behavior: ControlRoomIPBehavior;
  webservers: ControlRoomWebservers;
  isolation: ControlRoomIsolation;
  firewall: ControlRoomFirewall;
  pending_actions: ControlRoomAction[];
}

// History + framework series
export interface RiskScorePoint { ts: string; score: number; }
export interface RiskScoreHistory { points: RiskScorePoint[]; }
export interface RemediationVelocityPoint { ts: string; count: number; }
export interface RemediationVelocityHistory { points: RemediationVelocityPoint[]; }
export interface FrameworkComplianceSummary {
  name: string;
  pass: number;
  fail: number;
  coverage: number;
}
export interface ComplianceByFramework { frameworks: FrameworkComplianceSummary[]; }

// Admin endpoints
export interface AdminSelfHealth {
  api_p95_ms: number;
  nats_lag_ms: number;
  db_p95_ms: number;
  queue_depth: number;
  status: 'ok' | 'degraded' | 'down';
}

export interface AdminIngestSeriesPoint {
  ts: string;
  events_per_sec: number;
  bytes_per_sec: number;
}
export interface AdminIngestThroughput {
  series: AdminIngestSeriesPoint[];
  totals: { events: number; bytes: number };
}

export interface AdminTenantActivity {
  tenant_id: string;
  name: string;
  events_24h: number;
  nodes: number;
  users_active: number;
  last_seen?: string;
}
export interface AdminTenantsActivity {
  active_count: number;
  total_count: number;
  top: AdminTenantActivity[];
}

export interface AdminSLOEntry {
  name: string;
  target: number;
  actual: number;
  burn_rate: number;
  window: string;
}
export interface AdminSLO { slos: AdminSLOEntry[]; }

export interface AdminCapacity {
  disk_used: number;
  disk_total: number;
  doris_status: string;
  postgres_status: string;
  retention_days_remaining: number;
}

// Investigate / search
export interface ClassificationChip {
  label: string;
  tone?: 'healthy' | 'warning' | 'degraded' | 'critical' | 'info' | 'unknown';
  severity?: 'healthy' | 'warning' | 'degraded' | 'critical' | 'info' | 'unknown' | string;
}

export interface SearchHit {
  type: string;
  id: string;
  score: number;
  snippet?: string;
  classification?: ClassificationChip[];
}
export interface SearchFacet { type: string; count: number; }
export interface InvestigateSearchResult {
  facets: SearchFacet[];
  items: SearchHit[];
  next_cursor?: string;
}

export interface EntityDetail {
  type: string;
  id: string;
  classification?: ClassificationChip[];
  first_seen?: string;
  last_seen?: string;
  counts?: {
    events: number;
    alerts: number;
    audit: number;
    sessions: number;
    remediations: number;
  };
  meta?: Record<string, unknown>;
}

export interface LifecycleItem {
  ts: string;
  source: string;
  severity?: string;
  actor?: string;
  target?: string;
  summary: string;
  raw_id?: string;
}
export interface EntityLifecycle {
  items: LifecycleItem[];
  next_cursor?: string;
}

export interface RelatedEntity {
  type: string;
  id: string;
  score: number;
  co_occurrences: number;
}
export interface EntityRelated { related: RelatedEntity[]; }

export interface IpEnrichment {
  addr: string;
  classification?: ClassificationChip[];
  geo?: {
    country?: string;
    country_code?: string;
    city?: string;
    region?: string;
    asn?: number | string;
    org?: string;
    isp?: string;
    timezone?: string;
    latitude?: number;
    longitude?: number;
  };
  threat_feeds?: { feed: string; severity?: string; first_seen?: string }[];
  reputation_score?: number;
  source?: string;
}

export interface SavedSearch {
  id: string;
  tenant_id: string;
  owner_user_id: string;
  name: string;
  query: string;
  entity_type?: string;
  filters?: Record<string, unknown>;
  shared: boolean;
  created_at: string;
  updated_at: string;
}
export interface SavedSearchInput {
  name: string;
  query: string;
  entity_type?: string;
  filters?: Record<string, unknown>;
  shared?: boolean;
}

// Onboarding wizard
export type OnboardingProtocol = 'ssh' | 'winrm' | 'rdp';
export type OnboardingAuth = 'password' | 'private_key' | 'agent';
export interface TestConnectionPayload {
  protocol: OnboardingProtocol;
  host: string;
  port?: number;
  username?: string;
  auth?: OnboardingAuth;
  password?: string;
  private_key?: string;
  passphrase?: string;
  https?: boolean;
  skip_verify?: boolean;
  timeout_ms?: number;
}
export interface ConnectionProbe {
  reachable: boolean;
  latency_ms?: number;
  os?: string;
  os_version?: string;
  hostname?: string;
  architecture?: string;
  capabilities?: string[];
  banner?: string;
  distro?: string;
  cpu_count?: number;
  memory_mb?: number;
  detected_at: string;
}
export interface TestConnectionResult {
  ok: boolean;
  probe?: ConnectionProbe;
  error?: string;
}

export interface PortRule {
  id: string;
  tenant_id: string;
  policy_id?: string;
  name: string;
  port: number;
  protocol: 'tcp' | 'udp';
  expected_state: 'open' | 'closed';
  target_labels: Record<string, unknown>;
  severity: string;
  action: string;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreatePortRulePayload {
  tenant_id: string;
  policy_id?: string;
  name: string;
  port: number;
  protocol: 'tcp' | 'udp';
  expected_state: 'open' | 'closed';
  target_labels?: Record<string, unknown>;
  severity?: string;
  action?: string;
  enabled?: boolean;
}

export interface Alert {
  id: string;
  tenant_id: string;
  rule_id?: string;
  node_id?: string;
  source: string;
  severity: string;
  title: string;
  summary?: string;
  state: 'open' | 'acked' | 'resolved';
  dedup_key?: string;
  context: Record<string, unknown>;
  opened_at: string;
  acked_at?: string;
  acked_by?: string;
  resolved_at?: string;
  resolved_by?: string;
}

export interface AccessRequest {
  id: string;
  tenant_id: string;
  user_id?: string;
  target_node_id?: string;
  target_resource_type: 'ssh' | 'rdp' | 'db';
  requested_access: string;
  justification?: string;
  status: 'pending' | 'approved' | 'denied' | 'expired' | 'revoked';
  ttl_seconds: number;
  requested_at: string;
  decided_at?: string;
  decided_by?: string;
  decision_reason?: string;
  expires_at?: string;
}

export interface CreateAccessRequestPayload {
  tenant_id: string;
  target_node_id?: string;
  target_resource_type: 'ssh' | 'rdp' | 'db';
  requested_access: string;
  justification?: string;
  ttl_seconds?: number;
}

export interface SessionRecording {
  id: string;
  node_id: string;
  user_id?: string;
  session_type: string;
  started_at: string;
  ended_at?: string;
  duration_seconds?: number;
  status: string;
  metadata?: Record<string, unknown>;
  artifact_path?: string;
  artifact_size_bytes?: number;
  checksum?: string;
  created_at: string;
  updated_at: string;
}

export type SessionEventKind = 'input' | 'output' | 'resize' | 'command' | 'other';

export interface SessionEvent {
  at: string;
  kind: SessionEventKind;
  payload: string;
  command?: string;
  cols?: number;
  rows?: number;
  sequence: number;
}

export interface SessionParsedResponse {
  session_id: string;
  data: SessionEvent[];
  count: number;
}

export type ThreatFeedType =
  | 'spamhaus_drop'
  | 'spamhaus_edrop'
  | 'firehol_l1'
  | 'tor_exit'
  | 'abuseipdb'
  | 'otx'
  | 'custom_lines'
  | 'custom_spamhaus';

export interface ThreatFeed {
  id: string;
  tenant_id: string;
  name: string;
  feed_type: ThreatFeedType;
  url?: string;
  has_api_key: boolean;
  score_floor: number;
  refresh_seconds: number;
  category?: string;
  enabled: boolean;
  last_status?: string;
  last_error?: string;
  last_indicator_count: number;
  last_refreshed_at?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateThreatFeedPayload {
  tenant_id: string;
  name: string;
  feed_type: ThreatFeedType;
  url?: string;
  api_key?: string;
  score_floor?: number;
  refresh_seconds?: number;
  category?: string;
  enabled?: boolean;
}

export interface UpdateThreatFeedPayload {
  name?: string;
  url?: string;
  api_key?: string;
  clear_api_key?: boolean;
  score_floor?: number;
  refresh_seconds?: number;
  category?: string;
  enabled?: boolean;
}

export interface SimulateResult {
  rule_type: string;
  window_days: number;
  nodes_would_fail: number;
  nodes_would_pass: number;
  summary: string;
  sample?: Record<string, unknown>[];
}

export interface ReportDesc {
  slug: string;
  title: string;
  description: string;
  default_range: string;
  formats: string[];
}

export interface Recommendation {
  kind: string;
  title: string;
  rationale: string;
  confidence: number;
  evidence: Record<string, unknown>;
  draft: Record<string, unknown>;
}

export interface LogRule {
  id: string;
  tenant_id: string;
  policy_id?: string;
  name: string;
  log_source: string;
  pattern: string;
  severity: string;
  window_seconds: number;
  threshold: number;
  action: string;
  target_labels: Record<string, unknown>;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

export interface CreateLogRulePayload {
  tenant_id: string;
  policy_id?: string;
  name: string;
  log_source: string;
  pattern: string;
  severity?: string;
  window_seconds?: number;
  threshold?: number;
  action?: string;
  target_labels?: Record<string, unknown>;
  enabled?: boolean;
}

export interface PaginatedItems<T> {
  items: T[];
  pagination: ServerPaginationMeta;
}

export interface Tenant {
  id: string;
  name: string;
  created_at: string;
}

export interface UpdateTenantPayload {
  name: string;
}

export interface CreateTenantPayload {
  name: string;
}

export type NodeState =
  | 'enrollment_pending'
  | 'active'
  | 'enrollment_failed'
  | 'retired';

export interface NodeSummary {
  id: string;
  tenant_id: string;
  hostname: string;
  os?: string;
  arch?: string;
  public_ip?: string;
  state: NodeState | string;
  last_seen_at?: string;
  first_scan_at?: string;
  agent_version?: string;
  labels?: Record<string, unknown>;
  created_at: string;
  updated_at: string;
}

export interface FleetEnrollTarget {
  host: string;
  port?: number;
  user?: string;
}

export interface FleetEnrollRequest {
  targets: FleetEnrollTarget[];
  ssh_user?: string;
  // base64-encoded PEM
  ssh_key?: string;
  ssh_password?: string;
  token: string;
  tenant_id?: string;
  compliance_policy_id?: string;
  parallel?: number;
  labels?: Record<string, string>;
}

export interface FleetEnrollResponse {
  job_id: string;
  status: string;
  message: string;
}

export interface FleetEnrollResult {
  id: string;
  host: string;
  port: number;
  success: boolean;
  node_id?: string;
  error_message?: string;
  ssh_output?: string;
  duration_ms?: number;
  created_at: string;
}

export interface FleetEnrollStatus {
  job_id: string;
  status: string;
  results: FleetEnrollResult[];
}

export interface RegisterNodePayload {
  tenant_id?: string;
  tenant_name?: string;
  /** Optional — the installed agent will self-report the real hostname on first connect. */
  hostname?: string;
  os?: string;
  arch?: string;
  public_ip?: string;
  bootstrap_token: string;
}

export interface RegisterNodeResponse {
  node_id: string;
  tenant_id: string;
  intervals: Record<string, number>;
  provisioning_hints: string;
}

export type Node = NodeSummary;

export interface UpdateNodePayload {
  hostname?: string;
  os?: string;
  arch?: string;
  public_ip?: string;
  labels?: Record<string, unknown>;
}

export type NetworkIsolationMode = 'online' | 'whitelist' | 'airgapped';

export interface NodeIsolationPayload {
  mode: NetworkIsolationMode;
  duration_seconds?: number;
  expires_at?: string;
  reason?: string;
  allowed_applications?: string[];
  allowlist_cidrs?: string[];
}

export interface APIClientOptions {
  baseUrl?: string;
  token?: string | null;
}

export interface ProfileUserDetails {
  id: string;
  display_name?: string;
  email?: string;
  created_at: string;
}

export interface Profile {
  subject: string;
  name: string;
  email: string;
  type: string;
  roles: string[];
  groups: string[];
  stored_roles?: string[];
  user?: ProfileUserDetails;
}

export interface User {
  id: string;
  external_id: string;
  display_name?: string;
  email?: string;
  roles: string[];
  created_at: string;
}

export interface Role {
  id: string;
  name: string;
  description?: string;
  created_at: string;
}

export interface ListUsersParams {
  limit?: number;
  offset?: number;
}

export interface UpdateUserRolesPayload {
  roles: string[];
}

export type JobStatus =
  | 'queued'
  | 'running'
  | 'succeeded'
  | 'failed'
  | 'cancelled';

export interface JobEvent {
  id: string;
  status: JobStatus | string;
  message?: string;
  created_at: string;
}

export interface Job {
  id: string;
  tenant_id?: string;
  type: string;
  status: JobStatus | string;
  payload?: unknown;
  retries: number;
  max_retries: number;
  scheduled_at?: string;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
  events?: JobEvent[];
}

export interface WorkerStatus {
  backend: string;
  started: boolean;
  queue_depth: number;
  active: number;
  last_error?: string;
}

export interface CreateJobRequest {
  type: string;
  tenant_id?: string;
  payload?: unknown;
  max_retries?: number;
}

export interface ListJobsParams {
  tenantId?: string;
  status?: JobStatus | string;
  type?: string;
  limit?: number;
  offset?: number;
}

export interface ListTenantsParams {
  namePrefix?: string;
  limit?: number;
  offset?: number;
}

export interface ListNodesParams {
  tenantId?: string;
  hostnamePrefix?: string;
  limit?: number;
  offset?: number;
}

export interface Template {
  id: string;
  name: string;
  provider: string;
  description?: string;
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
  archived_at?: string;
  promoted_version_id?: string;
  promoted_version?: TemplateVersion;
}

export interface TemplateVersion {
  id: string;
  version: number;
  checksum?: string;
  body: string;
  metadata_schema?: unknown;
  rollout_notes?: string;
  created_by?: string;
  created_at: string;
  promoted_at?: string;
}

export interface ListTemplatesParams {
  provider?: string;
  namePrefix?: string;
  includeArchived?: boolean;
  limit?: number;
  offset?: number;
}

export interface CreateTemplatePayload {
  name: string;
  provider: string;
  description?: string;
  labels?: Record<string, string>;
}

export interface UpdateTemplatePayload {
  name?: string;
  provider?: string;
  description?: string;
  labels?: Record<string, string>;
  archived?: boolean;
}

export interface ListTemplateVersionsParams {
  limit?: number;
  offset?: number;
}

export interface CreateTemplateVersionPayload {
  body: string;
  checksum?: string;
  metadata_schema?: unknown;
  rollout_notes?: string;
}

export interface ComplianceResult {
  id: string;
  job_id: string;
  tenant_id?: string;
  node_id?: string;
  scan_id?: string;
  rule_id: string;
  passed: boolean;
  severity?: string;
  details?: string;
  remediation?: string;
  metadata?: Record<string, unknown>;
  checked_at?: string;
  created_at: string;
}

export interface ComplianceSummary {
  total: number;
  passed: number;
  failed: number;
  by_severity: Record<string, number>;
  by_rule_id?: Record<string, number>;
  last_checked?: string;
}

export interface ComplianceTrend {
  date: string;
  passed: number;
  failed: number;
  total: number;
}

export interface ListComplianceResultsParams {
  tenant_id?: string;
  node_id?: string;
  job_id?: string;
  scan_id?: string;
  rule_id?: string;
  passed?: boolean;
  severity?: string;
  framework?: string;
  since?: string;
  until?: string;
  limit?: number;
  offset?: number;
}

export interface ComplianceTrendsParams {
  tenant_id?: string;
  node_id?: string;
  days?: number;
}

// ── Compliance policies ───────────────────────────────────────────────────────

export interface Policy {
  id: string;
  tenant_id?: string;
  name: string;
  description?: string;
  rule_type: string;
  enabled: boolean;
  labels: Record<string, string>;
  created_at: string;
  updated_at: string;
  archived_at?: string;
}

export interface PolicyVersion {
  id: string;
  version: number;
  rule_definition: string;
  checksum?: string;
  metadata?: Record<string, unknown>;
  created_by?: string;
  created_at: string;
  promoted_at?: string;
}

export interface CreatePolicyPayload {
  tenant_id?: string;
  name: string;
  description?: string;
  rule_type: string;
  enabled: boolean;
  labels?: Record<string, string>;
}

export interface CreatePolicyVersionPayload {
  rule_definition: string;
  checksum?: string;
  metadata?: Record<string, unknown>;
}

export interface ComplianceEvaluatePayload {
  node_id: string;
  region: string;
  rulesets: string[];
  certifications?: string[];
  policies?: Record<string, string>;
  use_real_scan?: boolean;
}

export interface ComplianceEvaluateResult {
  rule_id: string;
  passed: boolean;
  severity?: string;
  details?: string;
  checked_at?: string;
}

export interface ComplianceEvaluateResponse {
  results: ComplianceEvaluateResult[];
  metadata?: {
    no_policies_assigned?: boolean;
    [key: string]: unknown;
  };
}

export interface AuditLog {
  id: string;
  tenant_id?: string;
  actor_id?: string;
  actor_type: string;
  action: string;
  resource_type: string;
  resource_id?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface ListAuditLogsParams {
  tenant_id?: string;
  actor_type?: string;
  action?: string;
  resource_type?: string;
  resource_id?: string;
  since?: string;
  until?: string;
  limit?: number;
  offset?: number;
}

export interface TelemetryMetric {
  id: string;
  tenant_id?: string;
  node_id?: string;
  metric_name: string;
  metric_value: number;
  metric_unit?: string;
  labels?: Record<string, string>;
  timestamp: string;
  created_at: string;
}

export interface TelemetryLog {
  id: string;
  tenant_id?: string;
  node_id?: string;
  log_level: string;
  log_message: string;
  log_source?: string;
  log_program?: string;
  labels?: Record<string, string>;
  timestamp: string;
  created_at: string;
}

export interface ListTelemetryMetricsParams {
  tenant_id?: string;
  node_id?: string;
  metric_name?: string;
  since?: string;
  until?: string;
  limit?: number;
  offset?: number;
}

export interface ListTelemetryLogsParams {
  tenant_id?: string;
  node_id?: string;
  log_level?: string;
  log_source?: string;
  since?: string;
  until?: string;
  limit?: number;
  offset?: number;
}

export interface Webhook {
  id: string;
  tenant_id?: string;
  name: string;
  url: string;
  events: string[];
  enabled: boolean;
  verify_ssl: boolean;
  timeout_seconds: number;
  retry_count: number;
  headers?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
  last_triggered_at?: string;
  last_success_at?: string;
  last_failure_at?: string;
  failure_count: number;
  created_at: string;
  updated_at: string;
  created_by?: string;
}

export interface CreateWebhookPayload {
  tenant_id?: string;
  name: string;
  url: string;
  events: string[];
  secret?: string;
  enabled?: boolean;
  verify_ssl?: boolean;
  timeout_seconds?: number;
  retry_count?: number;
  headers?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface SecretGroup {
  id: string;
  tenant_id?: string;
  name: string;
  backend: string;
  endpoint?: string;
  sync_interval_seconds?: number;
  last_sync_at?: string;
  sync_status: string;
  sync_error?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSecretGroupPayload {
  tenant_id?: string;
  name: string;
  backend: string;
  endpoint?: string;
  sync_interval_seconds?: number;
}

export interface SecretSync {
  id: string;
  secret_group_id: string;
  node_id?: string;
  secret_path: string;
  secret_version?: string;
  synced_at: string;
  sync_status: string;
  sync_error?: string;
  metadata?: Record<string, unknown>;
}

export interface EnrollmentToken {
  id: string;
  tenant_id: string;
  name: string;
  token?: string;
  max_nodes: number;
  nodes_enrolled: number;
  labels?: Record<string, string>;
  capabilities?: string[];
  expires_at: string;
  revoked_at?: string | null;
  created_by?: string | null;
  created_at: string;
}

export interface ListEnrollmentTokensParams {
  tenant_id?: string;
  limit?: number;
  offset?: number;
}

export interface CreateEnrollmentTokenPayload {
  name: string;
  tenant_id: string;
  max_nodes: number;
  ttl: string; // Go duration string, e.g. "24h"
  labels?: Record<string, string>;
  capabilities?: string[];
}

export interface BundleDownloadOptions {
  os: string;
  arch: string;
  token: string;
}

export interface ListSecretGroupsParams {
  tenant_id?: string;
  limit?: number;
  offset?: number;
}

export interface ListSecretSyncsParams {
  limit?: number;
  offset?: number;
}

export interface UpdateWebhookPayload {
  name?: string;
  url?: string;
  events?: string[];
  secret?: string;
  enabled?: boolean;
  verify_ssl?: boolean;
  timeout_seconds?: number;
  retry_count?: number;
  headers?: Record<string, unknown>;
  metadata?: Record<string, unknown>;
}

export interface ListWebhooksParams {
  tenant_id?: string;
  enabled?: boolean;
  limit?: number;
  offset?: number;
}

interface ServerPaginationMeta {
  total: number;
  count: number;
  limit: number;
  offset: number;
  next_offset?: number | null;
  prev_offset?: number | null;
}

export interface PaginationMeta {
  total: number;
  count: number;
  limit: number;
  offset: number;
  nextOffset: number | null;
  prevOffset: number | null;
}

interface RawPaginatedResponse<T> {
  data: T[];
  pagination: ServerPaginationMeta;
}

export interface PaginatedResponse<T> {
  data: T[];
  pagination: PaginationMeta;
}

// Predictive server downtime — Use Case 5 (PR 31).
// risk_level is one of: low | medium | high | critical | calibrating.
// "calibrating" means the predict job hasn't seen enough samples yet
// (<24 per metric); the UI must surface "Calibrating (N/24 samples)"
// rather than render fake zeros.
export type NodeHealthRiskLevel = 'low' | 'medium' | 'high' | 'critical' | 'calibrating';

export interface NodeHealthScore {
  node_id: string;
  score: number;
  risk_level: NodeHealthRiskLevel;
  components: Record<string, unknown>;
  computed_at?: string;
}

export interface AIConfigResponse {
  tenant_id: string;
  provider: string;
  model: string;
  base_url: string;
  has_api_key: boolean;
  updated_at?: string;
}

export interface AIConfigPut {
  provider: string;
  model: string;
  base_url: string;
  /** Empty preserves the previously stored key. */
  api_key: string;
}

export interface NodePackage {
  node_id: string;
  name: string;
  version: string;
  source: string;
  arch?: string | null;
  installed_at?: string | null;
  // Server-computed via name-pattern heuristic (kernel, libc, systemd,
  // Microsoft.*, etc.). The PackagesTab uses this to default-hide OS
  // baseline noise so operators see real application packages.
  is_system?: boolean;
}

export interface NodeService {
  id: string;
  node_id: string;
  tenant_id: string;
  pid: number;
  process: string;
  binary_path: string;
  listen_addr: string;
  port: number;
  service_kind: string;
  probe_status?: number | null;
  probe_server?: string | null;
  probe_title?: string | null;
  probe_content_type?: string | null;
  observed_at: string;
}

export interface AtRiskNode {
  node_id: string;
  tenant_id: string;
  hostname: string;
  score: number;
  risk_level: NodeHealthRiskLevel;
  components: Record<string, unknown>;
  computed_at: string;
}

export interface AtRiskFleetResponse {
  data: AtRiskNode[];
  total_count: number;
  critical: number;
  high: number;
}

async function safeErrorMessage(response: Response): Promise<string | undefined> {
  try {
    const data = await response.json();
    if (data && typeof data.message === 'string') {
      return data.message;
    }
  } catch {
    // ignore json parse errors
  }
  return response.statusText;
}

export class APIError extends Error {
  public readonly status: number;

  constructor(message: string, status: number) {
    super(message);
    this.name = 'APIError';
    this.status = status;
  }
}

export class APIClient {
  private readonly baseUrl: string;
  private token: string | null | undefined;
  private unauthorizedHandler?: () => void;

  constructor({ baseUrl, token }: APIClientOptions = {}) {
    const configured = baseUrl ?? (import.meta.env.VITE_API_URL as string | undefined);
    this.baseUrl = APIClient.normalizeBase(configured);
    this.token = token ?? null;
  }

  /**
   * Normalize a base URL once. Every request path in this client starts with
   * `/api/v1/...`, so the base URL must never carry its own `/api` segment;
   * we strip trailing slashes plus an optional `/api` (or `/api/v1`) suffix
   * defensively so that mis-configured deploys don't produce `/api/api/...`.
   * Same-origin (PROD) is the default when nothing is configured.
   */
  static normalizeBase(configured: string | undefined | null): string {
    if (configured === undefined || configured === null) {
      return DEFAULT_API_BASE_URL;
    }
    if (configured === '') return '';
    let resolved = configured.replace(/\/+$/, '');
    resolved = resolved.replace(/\/api(?:\/v\d+)?$/i, '');
    return resolved;
  }

  setToken(token: string | null): void {
    this.token = token;
  }

  onUnauthorized(handler?: () => void): void {
    this.unauthorizedHandler = handler;
  }

  async getProfile(): Promise<Profile> {
    return this.request<Profile>('/api/v1/me');
  }

  // Email/password login. Returns a session token the caller stores via
  // AuthProvider.signIn; from then on every request is Bearer-authed
  // exactly like the legacy static-token path.
  async loginWithPassword(email: string, password: string): Promise<LoginResponse> {
    return this.request<LoginResponse>('/api/v1/auth/login', {
      method: 'POST',
      body: JSON.stringify({ email, password }),
    });
  }

  async logout(): Promise<void> {
    await this.request<void>('/api/v1/auth/logout', { method: 'POST' });
  }

  async getCurrentUser(): Promise<CurrentUser> {
    return this.request<CurrentUser>('/api/v1/auth/me');
  }

  // ---- RBAC ---------------------------------------------------------
  async listPermissions(): Promise<Permission[]> {
    return this.request<Permission[]>('/api/v1/permissions');
  }
  async listRolesWithPermissions(): Promise<RoleWithPermissions[]> {
    return this.request<RoleWithPermissions[]>('/api/v1/roles/permissions');
  }
  async setRolePermissions(roleId: string, permissions: string[]): Promise<void> {
    await this.request<void>(`/api/v1/roles/${encodeURIComponent(roleId)}/permissions`, {
      method: 'PUT',
      body: JSON.stringify({ permissions }),
    });
  }
  async createCustomRole(payload: { name: string; description: string; permissions: string[] }): Promise<RoleWithPermissions> {
    return this.request<RoleWithPermissions>('/api/v1/roles/', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }
  async deleteRole(roleId: string): Promise<void> {
    await this.request<void>(`/api/v1/roles/${encodeURIComponent(roleId)}`, { method: 'DELETE' });
  }

  // ---- Custom dashboards -------------------------------------------
  async listDashboards(tenantId: string): Promise<CustomDashboard[]> {
    return this.request<CustomDashboard[]>(`/api/v1/dashboards?tenant_id=${encodeURIComponent(tenantId)}`);
  }
  async getDashboard(id: string): Promise<CustomDashboard> {
    return this.request<CustomDashboard>(`/api/v1/dashboards/${encodeURIComponent(id)}`);
  }
  async createDashboard(payload: { tenant_id: string; name: string; description?: string; shared?: boolean }): Promise<CustomDashboard> {
    return this.request<CustomDashboard>('/api/v1/dashboards', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }
  async updateDashboard(id: string, payload: Partial<CustomDashboard>): Promise<void> {
    await this.request<void>(`/api/v1/dashboards/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }
  async deleteDashboard(id: string): Promise<void> {
    await this.request<void>(`/api/v1/dashboards/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }
  async createWidget(dashboardId: string, payload: WidgetPayload): Promise<DashboardWidget> {
    return this.request<DashboardWidget>(`/api/v1/dashboards/${encodeURIComponent(dashboardId)}/widgets`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }
  async updateWidget(dashboardId: string, widgetId: string, payload: WidgetPayload): Promise<void> {
    await this.request<void>(`/api/v1/dashboards/${encodeURIComponent(dashboardId)}/widgets/${encodeURIComponent(widgetId)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }
  async deleteWidget(dashboardId: string, widgetId: string): Promise<void> {
    await this.request<void>(`/api/v1/dashboards/${encodeURIComponent(dashboardId)}/widgets/${encodeURIComponent(widgetId)}`, { method: 'DELETE' });
  }

  async listTenants(params: ListTenantsParams = {}): Promise<PaginatedResponse<Tenant>> {
    const search = new URLSearchParams();
    if (params.namePrefix) {
      search.set('name_prefix', params.namePrefix);
    }
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }

    const query = search.toString();
    const suffix = query ? `?${query}` : '';
    const response = await this.request<RawPaginatedResponse<Tenant>>(`/api/v1/tenants${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listNodes(options: ListNodesParams = {}): Promise<PaginatedResponse<NodeSummary>> {
    const search = new URLSearchParams();
    if (options.tenantId) {
      search.set('tenant_id', options.tenantId);
    }
    if (options.hostnamePrefix) {
      search.set('hostname_prefix', options.hostnamePrefix);
    }
    if (typeof options.limit === 'number') {
      search.set('limit', options.limit.toString());
    }
    if (typeof options.offset === 'number') {
      search.set('offset', options.offset.toString());
    }

    const query = search.toString();
    const suffix = query ? `?${query}` : '';
    const response = await this.request<RawPaginatedResponse<NodeSummary>>(`/api/v1/nodes${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listJobs(params: ListJobsParams = {}): Promise<PaginatedResponse<Job>> {
    const search = new URLSearchParams();
    if (params.tenantId) {
      search.set('tenant_id', params.tenantId);
    }
    if (params.status) {
      search.set('status', params.status);
    }
    if (params.type) {
      search.set('type', params.type);
    }
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }

    const query = search.toString();
    const suffix = query ? `?${query}` : '';
    const response = await this.request<RawPaginatedResponse<Job>>(`/api/v1/jobs${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getJob(jobId: string): Promise<Job> {
    const encoded = encodeURIComponent(jobId);
    return this.request<Job>(`/api/v1/jobs/${encoded}`);
  }

  async cancelJob(jobId: string): Promise<Job> {
    const encoded = encodeURIComponent(jobId);
    return this.request<Job>(`/api/v1/jobs/${encoded}/cancel`, {
      method: 'POST',
    });
  }

  async getWorkerStatus(): Promise<WorkerStatus> {
    return this.request<WorkerStatus>('/api/v1/worker/status');
  }

  async createJob(payload: CreateJobRequest): Promise<Job> {
    return this.request<Job>('/api/v1/jobs', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async createTenant(payload: CreateTenantPayload): Promise<Tenant> {
    return this.request<Tenant>('/api/v1/tenants', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getTenant(tenantId: string): Promise<Tenant> {
    const encoded = encodeURIComponent(tenantId);
    return this.request<Tenant>(`/api/v1/tenants/${encoded}`);
  }

  async updateTenant(tenantId: string, payload: UpdateTenantPayload): Promise<Tenant> {
    const encoded = encodeURIComponent(tenantId);
    return this.request<Tenant>(`/api/v1/tenants/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deleteTenant(tenantId: string): Promise<void> {
    const encoded = encodeURIComponent(tenantId);
    await this.request<void>(`/api/v1/tenants/${encoded}`, { method: 'DELETE' });
  }

  async registerNode(payload: RegisterNodePayload): Promise<RegisterNodeResponse> {
    return this.request<RegisterNodeResponse>('/api/v1/register', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getNode(nodeId: string): Promise<Node> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<Node>(`/api/v1/nodes/${encoded}`);
  }

  async updateNode(nodeId: string, payload: UpdateNodePayload): Promise<Node> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<Node>(`/api/v1/nodes/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async setNodeIsolation(nodeId: string, payload: NodeIsolationPayload): Promise<Node> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<Node>(`/api/v1/nodes/${encoded}/isolation`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteNode(nodeId: string): Promise<void> {
    const encoded = encodeURIComponent(nodeId);
    await this.request<void>(`/api/v1/nodes/${encoded}`, { method: 'DELETE' });
  }

  async getNodeHealth(nodeId: string): Promise<NodeHealthScore> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<NodeHealthScore>(`/api/v1/nodes/${encoded}/health`);
  }

  async repairNodeViaSSH(
    nodeId: string,
    payload: {
      ssh_user: string;
      ssh_key?: string; // base64-encoded PEM
      ssh_password?: string;
      host_override?: string;
      port?: number;
    },
  ): Promise<{ job_id: string; host: string; port: number; expire_at: string; message: string }> {
    const encoded = encodeURIComponent(nodeId);
    return this.request(`/api/v1/nodes/${encoded}/repair`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async listNodeServices(nodeId: string): Promise<{ data: NodeService[] }> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<{ data: NodeService[] }>(`/api/v1/nodes/${encoded}/services`);
  }

  async listNodePackages(nodeId: string): Promise<{ data: NodePackage[] }> {
    const encoded = encodeURIComponent(nodeId);
    return this.request<{ data: NodePackage[] }>(`/api/v1/nodes/${encoded}/packages`);
  }

  async getAIConfig(tenantId: string): Promise<AIConfigResponse> {
    return this.request<AIConfigResponse>(`/api/v1/ai/config?tenant_id=${encodeURIComponent(tenantId)}`);
  }

  async updateAIConfig(tenantId: string, payload: AIConfigPut): Promise<void> {
    await this.request<void>(`/api/v1/ai/config?tenant_id=${encodeURIComponent(tenantId)}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async testAIConfig(tenantId: string): Promise<{ ok: boolean; reply?: string; error?: string }> {
    return this.request<{ ok: boolean; reply?: string; error?: string }>(
      `/api/v1/ai/test?tenant_id=${encodeURIComponent(tenantId)}`,
      { method: 'POST' },
    );
  }

  async askAI(tenantId: string, question: string): Promise<{ answer: string; citations?: string[] }> {
    return this.request<{ answer: string; citations?: string[] }>(
      `/api/v1/ai/ask?tenant_id=${encodeURIComponent(tenantId)}`,
      { method: 'POST', body: JSON.stringify({ question }) },
    );
  }

  async getKnowledgeGraphMarkdown(tenantId: string): Promise<string> {
    const encoded = encodeURIComponent(tenantId);
    const resp = await fetch(`${this.baseUrl}/api/v1/knowledge-graph/${encoded}.md`, {
      headers: this.token ? { Authorization: `Bearer ${this.token}` } : {},
      credentials: 'include',
    });
    if (!resp.ok) {
      throw new Error(`knowledge graph fetch failed: ${resp.status}`);
    }
    return resp.text();
  }

  async listAtRiskNodes(tenantId?: string): Promise<AtRiskFleetResponse> {
    const search = new URLSearchParams();
    if (tenantId) {
      search.set('tenant_id', tenantId);
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    return this.request<AtRiskFleetResponse>(`/api/v1/health/at-risk${suffix}`);
  }

  async listUsers(params: ListUsersParams = {}): Promise<PaginatedResponse<User>> {
    const search = new URLSearchParams();
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<User>>(`/api/v1/users${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getUser(userID: string): Promise<User> {
    const encoded = encodeURIComponent(userID);
    return this.request<User>(`/api/v1/users/${encoded}`);
  }

  async updateUserRoles(userID: string, payload: UpdateUserRolesPayload): Promise<User> {
    const encoded = encodeURIComponent(userID);
    return this.request<User>(`/api/v1/users/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async listRoles(): Promise<Role[]> {
    return this.request<Role[]>('/api/v1/roles');
  }

  async listTemplates(params: ListTemplatesParams = {}): Promise<PaginatedResponse<Template>> {
    const search = new URLSearchParams();
    if (params.provider) {
      search.set('provider', params.provider);
    }
    if (params.namePrefix) {
      search.set('name_prefix', params.namePrefix);
    }
    if (typeof params.includeArchived === 'boolean') {
      search.set('include_archived', params.includeArchived ? 'true' : 'false');
    }
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<Template>>(`/api/v1/templates${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async createTemplate(payload: CreateTemplatePayload): Promise<Template> {
    return this.request<Template>('/api/v1/templates', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getTemplate(templateId: string): Promise<Template> {
    const encoded = encodeURIComponent(templateId);
    return this.request<Template>(`/api/v1/templates/${encoded}`);
  }

  async updateTemplate(templateId: string, payload: UpdateTemplatePayload): Promise<Template> {
    const encoded = encodeURIComponent(templateId);
    return this.request<Template>(`/api/v1/templates/${encoded}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async listTemplateVersions(
    templateId: string,
    params: ListTemplateVersionsParams = {},
  ): Promise<PaginatedResponse<TemplateVersion>> {
    const search = new URLSearchParams();
    if (typeof params.limit === 'number') {
      search.set('limit', params.limit.toString());
    }
    if (typeof params.offset === 'number') {
      search.set('offset', params.offset.toString());
    }
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const encoded = encodeURIComponent(templateId);
    const response = await this.request<RawPaginatedResponse<TemplateVersion>>(
      `/api/v1/templates/${encoded}/versions${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async createTemplateVersion(
    templateId: string,
    payload: CreateTemplateVersionPayload,
  ): Promise<TemplateVersion> {
    const encoded = encodeURIComponent(templateId);
    return this.request<TemplateVersion>(`/api/v1/templates/${encoded}/versions`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async promoteTemplateVersion(templateId: string, versionNumber: number): Promise<TemplateVersion> {
    const encoded = encodeURIComponent(templateId);
    return this.request<TemplateVersion>(
      `/api/v1/templates/${encoded}/versions/${versionNumber}/promote`,
      { method: 'POST' },
    );
  }

  async listComplianceResults(
    params: ListComplianceResultsParams = {},
  ): Promise<PaginatedResponse<ComplianceResult>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.node_id) search.set('node_id', params.node_id);
    if (params.job_id) search.set('job_id', params.job_id);
    if (params.scan_id) search.set('scan_id', params.scan_id);
    if (params.rule_id) search.set('rule_id', params.rule_id);
    if (typeof params.passed === 'boolean') search.set('passed', params.passed.toString());
    if (params.severity) search.set('severity', params.severity);
    if (params.framework) search.set('framework', params.framework);
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<ComplianceResult>>(
      `/api/v1/compliance/results${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getComplianceSummary(params: { tenant_id?: string; node_id?: string } = {}): Promise<ComplianceSummary> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.node_id) search.set('node_id', params.node_id);
    const suffix = search.toString() ? `?${search.toString()}` : '';
    return this.request<ComplianceSummary>(`/api/v1/compliance/summary${suffix}`);
  }

  async getComplianceTrends(params: ComplianceTrendsParams = {}): Promise<ComplianceTrend[]> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.node_id) search.set('node_id', params.node_id);
    if (typeof params.days === 'number') search.set('days', params.days.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<{ trends: ComplianceTrend[] }>(`/api/v1/compliance/trends${suffix}`);
    return response.trends || [];
  }

  // ── Compliance policies ─────────────────────────────────────────────────────

  async listPolicies(params: { tenant_id?: string; rule_type?: string; enabled?: boolean; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<Policy>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.rule_type) search.set('rule_type', params.rule_type);
    if (typeof params.enabled === 'boolean') search.set('enabled', String(params.enabled));
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    const response = await this.request<RawPaginatedResponse<Policy>>(`/api/v1/policies${qs ? `?${qs}` : ''}`);
    return { data: response.data, pagination: normalizePagination(response.pagination) };
  }

  async createPolicy(payload: CreatePolicyPayload): Promise<Policy> {
    return this.request<Policy>('/api/v1/policies', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updatePolicy(id: string, payload: Partial<CreatePolicyPayload> & { archived?: boolean }): Promise<Policy> {
    return this.request<Policy>(`/api/v1/policies/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deletePolicy(id: string): Promise<void> {
    await this.request<void>(`/api/v1/policies/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listPolicyVersions(policyId: string): Promise<PaginatedResponse<PolicyVersion>> {
    const response = await this.request<RawPaginatedResponse<PolicyVersion>>(
      `/api/v1/policies/${encodeURIComponent(policyId)}/versions`,
    );
    return { data: response.data, pagination: normalizePagination(response.pagination) };
  }

  async createPolicyVersion(policyId: string, payload: CreatePolicyVersionPayload): Promise<PolicyVersion> {
    return this.request<PolicyVersion>(`/api/v1/policies/${encodeURIComponent(policyId)}/versions`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async promotePolicyVersion(policyId: string, version: number): Promise<void> {
    await this.request<void>(`/api/v1/policies/${encodeURIComponent(policyId)}/versions/${version}/promote`, {
      method: 'POST',
    });
  }

  async evaluateCompliance(payload: ComplianceEvaluatePayload): Promise<ComplianceEvaluateResponse> {
    return this.request<ComplianceEvaluateResponse>('/api/v1/compliance/evaluate', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  // ── Audit logs ──────────────────────────────────────────────────────────────

  async listAuditLogs(params: ListAuditLogsParams = {}): Promise<PaginatedResponse<AuditLog>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.actor_type) search.set('actor_type', params.actor_type);
    if (params.action) search.set('action', params.action);
    if (params.resource_type) search.set('resource_type', params.resource_type);
    if (params.resource_id) search.set('resource_id', params.resource_id);
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<AuditLog>>(`/api/v1/audit${suffix}`);
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listTelemetryMetrics(
    params: ListTelemetryMetricsParams = {},
  ): Promise<PaginatedResponse<TelemetryMetric>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.node_id) search.set('node_id', params.node_id);
    if (params.metric_name) search.set('metric_name', params.metric_name);
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<TelemetryMetric>>(
      `/api/v1/telemetry/metrics${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listTelemetryLogs(
    params: ListTelemetryLogsParams = {},
  ): Promise<PaginatedResponse<TelemetryLog>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.node_id) search.set('node_id', params.node_id);
    if (params.log_level) search.set('log_level', params.log_level);
    if (params.log_source) search.set('log_source', params.log_source);
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<TelemetryLog>>(
      `/api/v1/telemetry/logs${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getNodeTelemetryMetrics(
    nodeId: string,
    params: {
      tenant_id?: string;
      metric_name?: string;
      since?: string;
      until?: string;
      limit?: number;
      offset?: number;
    } = {},
  ): Promise<PaginatedResponse<TelemetryMetric>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (params.metric_name) search.set('metric_name', params.metric_name);
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const encoded = encodeURIComponent(nodeId);
    const response = await this.request<RawPaginatedResponse<TelemetryMetric>>(
      `/api/v1/telemetry/nodes/${encoded}/metrics${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listWebhooks(params: ListWebhooksParams = {}): Promise<PaginatedResponse<Webhook>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (typeof params.enabled === 'boolean') search.set('enabled', params.enabled.toString());
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<{ items: Webhook[]; total: number; limit: number; offset: number }>(
      `/api/v1/webhooks${suffix}`,
    );
    return {
      data: response.items,
      pagination: {
        total: response.total,
        count: response.items.length,
        limit: response.limit,
        offset: response.offset,
        nextOffset: response.offset + response.items.length < response.total ? response.offset + response.items.length : null,
        prevOffset: response.offset > 0 ? Math.max(0, response.offset - response.limit) : null,
      },
    };
  }

  async getWebhook(webhookId: string): Promise<Webhook> {
    const encoded = encodeURIComponent(webhookId);
    return this.request<Webhook>(`/api/v1/webhooks/${encoded}`);
  }

  async createWebhook(payload: CreateWebhookPayload): Promise<Webhook> {
    return this.request<Webhook>('/api/v1/webhooks', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateWebhook(webhookId: string, payload: UpdateWebhookPayload): Promise<Webhook> {
    const encoded = encodeURIComponent(webhookId);
    return this.request<Webhook>(`/api/v1/webhooks/${encoded}`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  async deleteWebhook(webhookId: string): Promise<void> {
    const encoded = encodeURIComponent(webhookId);
    return this.request<void>(`/api/v1/webhooks/${encoded}`, {
      method: 'DELETE',
    });
  }

  async testWebhook(webhookId: string, payload: { event_type: string; payload?: Record<string, unknown> }): Promise<{ success: boolean; http_status_code?: number; response_body?: string; error?: string }> {
    const encoded = encodeURIComponent(webhookId);
    return this.request(`/api/v1/webhooks/${encoded}/test`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async listSecretGroups(params: ListSecretGroupsParams = {}): Promise<PaginatedResponse<SecretGroup>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<SecretGroup>>(
      `/api/v1/secrets/groups${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async getSecretGroup(groupId: string): Promise<SecretGroup> {
    const encoded = encodeURIComponent(groupId);
    return this.request<SecretGroup>(`/api/v1/secrets/groups/${encoded}`);
  }

  async createSecretGroup(payload: CreateSecretGroupPayload): Promise<SecretGroup> {
    return this.request<SecretGroup>('/api/v1/secrets/groups', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteSecretGroup(groupId: string): Promise<void> {
    const encoded = encodeURIComponent(groupId);
    return this.request<void>(`/api/v1/secrets/groups/${encoded}`, {
      method: 'DELETE',
    });
  }

  async syncSecretGroup(groupId: string): Promise<void> {
    const encoded = encodeURIComponent(groupId);
    return this.request<void>(`/api/v1/secrets/groups/${encoded}/sync`, {
      method: 'POST',
    });
  }

  // ── Fleet enrollment (Sprint 2 Pillar 1.7) ────────────────────────────

  async startFleetEnroll(payload: FleetEnrollRequest): Promise<FleetEnrollResponse> {
    return this.request<FleetEnrollResponse>('/api/v1/fleet/enroll', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getFleetEnrollStatus(jobId: string, tenantId: string): Promise<FleetEnrollStatus> {
    const encoded = encodeURIComponent(jobId);
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    return this.request<FleetEnrollStatus>(`/api/v1/fleet/enroll/${encoded}?${search.toString()}`);
  }

  async listSecretSyncs(groupId: string, params: ListSecretSyncsParams = {}): Promise<PaginatedResponse<SecretSync>> {
    const search = new URLSearchParams();
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const encoded = encodeURIComponent(groupId);
    const response = await this.request<RawPaginatedResponse<SecretSync>>(
      `/api/v1/secrets/groups/${encoded}/syncs${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async createEnrollmentToken(payload: CreateEnrollmentTokenPayload): Promise<EnrollmentToken> {
    return this.request<EnrollmentToken>('/api/v1/enrollment-tokens', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async listEnrollmentTokens(
    params: ListEnrollmentTokensParams = {},
  ): Promise<PaginatedResponse<EnrollmentToken>> {
    const search = new URLSearchParams();
    if (params.tenant_id) search.set('tenant_id', params.tenant_id);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    const response = await this.request<RawPaginatedResponse<EnrollmentToken>>(
      `/api/v1/enrollment-tokens${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async listProviderCredentials(params: { tenantId?: string; provider?: string; limit?: number; offset?: number } = {}): Promise<PaginatedItems<ProviderCredential>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.provider) search.set('provider', params.provider);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    return this.request<PaginatedItems<ProviderCredential>>(`/api/v1/provider-credentials${suffix}`);
  }

  async createProviderCredential(payload: CreateProviderCredentialPayload): Promise<ProviderCredential> {
    return this.request<ProviderCredential>(`/api/v1/provider-credentials`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async rotateProviderCredential(id: string, config: Record<string, unknown>): Promise<ProviderCredential> {
    const encoded = encodeURIComponent(id);
    return this.request<ProviderCredential>(`/api/v1/provider-credentials/${encoded}/rotate`, {
      method: 'POST',
      body: JSON.stringify({ config }),
    });
  }

  async deleteProviderCredential(id: string): Promise<void> {
    const encoded = encodeURIComponent(id);
    await this.request<void>(`/api/v1/provider-credentials/${encoded}`, { method: 'DELETE' });
  }

  async listHypervisorHosts(params: { tenantId?: string; provider?: string; limit?: number; offset?: number } = {}): Promise<PaginatedItems<HypervisorHost>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.provider) search.set('provider', params.provider);
    if (typeof params.limit === 'number') search.set('limit', params.limit.toString());
    if (typeof params.offset === 'number') search.set('offset', params.offset.toString());
    const suffix = search.toString() ? `?${search.toString()}` : '';
    return this.request<PaginatedItems<HypervisorHost>>(`/api/v1/hypervisor-hosts${suffix}`);
  }

  async createHypervisorHost(payload: CreateHypervisorHostPayload): Promise<HypervisorHost> {
    return this.request<HypervisorHost>(`/api/v1/hypervisor-hosts`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteHypervisorHost(id: string): Promise<void> {
    const encoded = encodeURIComponent(id);
    await this.request<void>(`/api/v1/hypervisor-hosts/${encoded}`, { method: 'DELETE' });
  }

  async verifyHypervisorHost(id: string): Promise<HypervisorHostVerifyResponse> {
    const encoded = encodeURIComponent(id);
    return this.request<HypervisorHostVerifyResponse>(`/api/v1/hypervisor-hosts/${encoded}/verify`, {
      method: 'POST',
    });
  }

  async getDashboardOverview(tenantId?: string, period?: string): Promise<DashboardOverview> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    if (period) search.set('period', period);
    const qs = search.toString();
    const path = `/api/v1/dashboard/overview${qs ? `?${qs}` : ''}`;
    return this.request<DashboardOverview>(path);
  }

  async getControlRoomOverview(tenantId?: string | null, period?: string): Promise<ControlRoomOverview> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    if (period) search.set('period', period);
    const qs = search.toString();
    return this.request<ControlRoomOverview>(`/api/v1/control-room/overview${qs ? `?${qs}` : ''}`);
  }

  // ---- Executive Risk Dashboard Metrics -------------------------------------------
  async getRiskScore(tenantId?: string): Promise<RiskScore> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    const qs = search.toString();
    return this.request<RiskScore>(`/api/v1/metrics/risk-score${qs ? `?${qs}` : ''}`);
  }

  async getMTTDMetrics(tenantId?: string, severity?: string, days?: number): Promise<MTTDMetrics> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    if (severity) search.set('severity', severity);
    if (days) search.set('days', String(days));
    const qs = search.toString();
    return this.request<MTTDMetrics>(`/api/v1/metrics/mttd${qs ? `?${qs}` : ''}`);
  }

  async getMTTRMetrics(tenantId?: string, severity?: string, days?: number): Promise<MTTRMetrics> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    if (severity) search.set('severity', severity);
    if (days) search.set('days', String(days));
    const qs = search.toString();
    return this.request<MTTRMetrics>(`/api/v1/metrics/mttr${qs ? `?${qs}` : ''}`);
  }

  async getRemediationVelocity(tenantId?: string, period?: number): Promise<RemediationVelocity> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    if (period) search.set('period', String(period));
    const qs = search.toString();
    return this.request<RemediationVelocity>(`/api/v1/metrics/remediation-velocity${qs ? `?${qs}` : ''}`);
  }

  async getFindingsAging(tenantId?: string, severity?: string): Promise<FindingAging> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    if (severity) search.set('severity', severity);
    const qs = search.toString();
    return this.request<FindingAging>(`/api/v1/metrics/findings-aging${qs ? `?${qs}` : ''}`);
  }

  // ── Dashboard / metric history ─────────────────────────────────────────
  async getRiskScoreHistory(tenantId?: string, days = 90): Promise<RiskScoreHistory> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    search.set('days', String(days));
    return this.request<RiskScoreHistory>(`/api/v1/dashboard/metrics/risk-score/history?${search.toString()}`);
  }

  async getRemediationVelocityHistory(tenantId?: string, days = 90): Promise<RemediationVelocityHistory> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    search.set('days', String(days));
    return this.request<RemediationVelocityHistory>(`/api/v1/dashboard/metrics/remediation-velocity/history?${search.toString()}`);
  }

  async getComplianceByFramework(tenantId?: string): Promise<ComplianceByFramework> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    const qs = search.toString();
    return this.request<ComplianceByFramework>(`/api/v1/dashboard/metrics/compliance/by-framework${qs ? `?${qs}` : ''}`);
  }

  // ── Admin endpoints ────────────────────────────────────────────────────
  async getAdminSelfHealth(): Promise<AdminSelfHealth> {
    return this.request<AdminSelfHealth>('/api/v1/admin/self-health');
  }

  async getAdminIngestThroughput(
    stream = 'events',
    interval = '1m',
    period = '1h',
  ): Promise<AdminIngestThroughput> {
    const search = new URLSearchParams();
    search.set('stream', stream);
    search.set('interval', interval);
    search.set('period', period);
    return this.request<AdminIngestThroughput>(`/api/v1/admin/ingest/throughput?${search.toString()}`);
  }

  async getAdminTenantsActivity(period = '24h'): Promise<AdminTenantsActivity> {
    const search = new URLSearchParams();
    search.set('period', period);
    return this.request<AdminTenantsActivity>(`/api/v1/admin/tenants/activity?${search.toString()}`);
  }

  async getAdminSLO(): Promise<AdminSLO> {
    return this.request<AdminSLO>('/api/v1/admin/slo');
  }

  async getAdminCapacity(): Promise<AdminCapacity> {
    return this.request<AdminCapacity>('/api/v1/admin/capacity');
  }

  // ── Investigate / search ───────────────────────────────────────────────
  async investigateSearch(params: {
    q: string;
    tenantId?: string | null;
    types?: string[];
    since?: string;
    until?: string;
    cursor?: string;
    limit?: number;
  }): Promise<InvestigateSearchResult> {
    const search = new URLSearchParams();
    search.set('q', params.q);
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.types?.length) search.set('types', params.types.join(','));
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (params.cursor) search.set('cursor', params.cursor);
    if (params.limit) search.set('limit', String(params.limit));
    return this.request<InvestigateSearchResult>(`/api/v1/search?${search.toString()}`);
  }

  async getEntity(type: string, id: string, params: { tenantId?: string | null } = {}): Promise<EntityDetail> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    const qs = search.toString();
    return this.request<EntityDetail>(`/api/v1/entities/${type}/${encodeURIComponent(id)}${qs ? `?${qs}` : ''}`);
  }

  async getEntityLifecycle(
    type: string,
    id: string,
    params: { tenantId?: string | null; since?: string; until?: string; sources?: string[]; cursor?: string; limit?: number } = {},
  ): Promise<EntityLifecycle> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (params.sources?.length) search.set('sources', params.sources.join(','));
    if (params.cursor) search.set('cursor', params.cursor);
    if (params.limit) search.set('limit', String(params.limit));
    const qs = search.toString();
    return this.request<EntityLifecycle>(
      `/api/v1/entities/${type}/${encodeURIComponent(id)}/lifecycle${qs ? `?${qs}` : ''}`,
    );
  }

  async getEntityRelated(type: string, id: string, params: { tenantId?: string | null } = {}): Promise<EntityRelated> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    const qs = search.toString();
    return this.request<EntityRelated>(`/api/v1/entities/${type}/${encodeURIComponent(id)}/related${qs ? `?${qs}` : ''}`);
  }

  async enrichIp(addr: string, tenantId?: string | null): Promise<IpEnrichment> {
    const search = new URLSearchParams();
    if (tenantId) search.set('tenant_id', tenantId);
    const qs = search.toString();
    return this.request<IpEnrichment>(`/api/v1/entities/ip/${encodeURIComponent(addr)}/enrich${qs ? `?${qs}` : ''}`);
  }

  async listSavedSearches(params: { tenantId?: string | null } = {}): Promise<{ items: SavedSearch[] }> {
    // Backend returns PaginatedResponse { data: [...] } but consumers expect { items: [...] }.
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    const qs = search.toString();
    const resp = await this.request<PaginatedResponse<SavedSearch>>(`/api/v1/saved-searches${qs ? `?${qs}` : ''}`);
    return { items: resp.data ?? [] };
  }

  async createSavedSearch(payload: SavedSearchInput, params: { tenantId?: string | null } = {}): Promise<SavedSearch> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    const qs = search.toString();
    return this.request<SavedSearch>(`/api/v1/saved-searches${qs ? `?${qs}` : ''}`, {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteSavedSearch(id: string): Promise<void> {
    await this.request<unknown>(`/api/v1/saved-searches/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async addEntityTag(type: string, id: string, tag: string, params: { tenantId?: string | null } = {}): Promise<void> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    const qs = search.toString();
    await this.request<unknown>(`/api/v1/entities/${type}/${encodeURIComponent(id)}/tags${qs ? `?${qs}` : ''}`, {
      method: 'POST',
      body: JSON.stringify({ tag }),
    });
  }

  async entityAction(
    type: string,
    id: string,
    payload: {
      action: 'block' | 'allow' | 'quarantine';
      reason?: string;
      ttl?: number;
      scope?: 'fleet' | 'affected';
    },
    params: { tenantId?: string | null } = {},
  ): Promise<EntityActionResponse> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    const qs = search.toString();
    return this.request<EntityActionResponse>(
      `/api/v1/entities/${type}/${encodeURIComponent(id)}/actions${qs ? `?${qs}` : ''}`,
      { method: 'POST', body: JSON.stringify(payload) },
    );
  }

  async getIPBehaviorOverview(params: { tenantId: string; since?: string }): Promise<IPBehaviorOverview> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    return this.request<IPBehaviorOverview>(`/api/v1/ip-behavior/overview?${search.toString()}`);
  }

  async listIPBehaviorCountries(params: { tenantId: string; since?: string }): Promise<{ countries: IPBehaviorCountrySummary[]; since: string }> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    return this.request<{ countries: IPBehaviorCountrySummary[]; since: string }>(
      `/api/v1/ip-behavior/countries?${search.toString()}`,
    );
  }

  async getIPBehaviorCountryDetail(params: { tenantId: string; code: string; since?: string }): Promise<IPBehaviorCountrySummary> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    return this.request<IPBehaviorCountrySummary>(
      `/api/v1/ip-behavior/countries/${encodeURIComponent(params.code)}?${search.toString()}`,
    );
  }

  async getIPBehaviorIPProfile(params: { tenantId: string; ip: string; since?: string }): Promise<IPBehaviorIPProfile> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    return this.request<IPBehaviorIPProfile>(
      `/api/v1/ip-behavior/ips/${encodeURIComponent(params.ip)}?${search.toString()}`,
    );
  }

  async listIPBehaviorBaselines(params: {
    tenantId: string;
    dimension?: string;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<IPBehaviorBaseline>> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.dimension) search.set('dimension', params.dimension);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    return this.request<PaginatedResponse<IPBehaviorBaseline>>(`/api/v1/ip-behavior/baselines?${search.toString()}`);
  }

  async createBlockProposal(payload: {
    tenant_id: string;
    ip_cidr: string;
    reason: string;
    score?: number;
    ttl_seconds?: number;
    scope?: string;
    target_type?: string;
    target_id?: string;
    server_group?: string;
    app?: string;
    vhost?: string;
    enforcement?: string;
    finding_id?: string;
    protected_override?: boolean;
    protected_override_reason?: string;
  }): Promise<IPBlockProposal> {
    return this.request<IPBlockProposal>('/api/v1/network/block-proposals', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async createASNBlockProposals(payload: {
    tenant_id: string;
    asn: string;
    since?: string;
    limit?: number;
    reason: string;
    score?: number;
    ttl_seconds?: number;
    scope?: string;
    target_type?: string;
    server_group?: string;
    app?: string;
    vhost?: string;
    enforcement?: string;
    protected_override?: boolean;
    protected_override_reason?: string;
  }): Promise<ASNBlockProposalsResponse> {
    return this.request<ASNBlockProposalsResponse>('/api/v1/network/block-proposals/asn', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async listBlockProposals(params: {
    tenantId: string;
    status?: string;
    ipCidr?: string;
    serverGroup?: string;
    app?: string;
    vhost?: string;
    findingId?: string;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<IPBlockProposal>> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.status) search.set('status', params.status);
    if (params.ipCidr) search.set('ip_cidr', params.ipCidr);
    if (params.serverGroup) search.set('server_group', params.serverGroup);
    if (params.app) search.set('app', params.app);
    if (params.vhost) search.set('vhost', params.vhost);
    if (params.findingId) search.set('finding_id', params.findingId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    return this.request<PaginatedResponse<IPBlockProposal>>(`/api/v1/network/block-proposals?${search.toString()}`);
  }

  async approveBlockProposal(id: string): Promise<IPBlockProposal> {
    return this.request<IPBlockProposal>(`/api/v1/network/block-proposals/${encodeURIComponent(id)}/approve`, {
      method: 'POST',
    });
  }

  async promoteBlockProposal(id: string): Promise<IPBlockProposal> {
    return this.request<IPBlockProposal>(`/api/v1/network/block-proposals/${encodeURIComponent(id)}/promote`, {
      method: 'POST',
    });
  }

  async rejectBlockProposal(id: string, reason?: string): Promise<IPBlockProposal> {
    return this.request<IPBlockProposal>(`/api/v1/network/block-proposals/${encodeURIComponent(id)}/reject`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    });
  }

  async rollbackBlockProposal(id: string, reason?: string): Promise<IPBlockProposal> {
    return this.request<IPBlockProposal>(`/api/v1/network/block-proposals/${encodeURIComponent(id)}/rollback`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    });
  }

  async listWebserverInstances(params: { tenantId: string; nodeId?: string; limit?: number; offset?: number }): Promise<PaginatedResponse<WebserverInstance>> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.nodeId) search.set('node_id', params.nodeId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    return this.request<PaginatedResponse<WebserverInstance>>(`/api/v1/webservers?${search.toString()}`);
  }

  async createWebserverInventoryScan(payload: WebserverConfigActionPayload): Promise<WebserverConfigActionResponse> {
    return this.request<WebserverConfigActionResponse>('/api/v1/webservers/inventory', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async planWebserverConfig(instanceId: string, payload: WebserverConfigActionPayload): Promise<WebserverConfigActionResponse> {
    return this.request<WebserverConfigActionResponse>(
      `/api/v1/webservers/${encodeURIComponent(instanceId)}/config/plan`,
      { method: 'POST', body: JSON.stringify(payload) },
    );
  }

  async applyWebserverConfig(instanceId: string, payload: WebserverConfigActionPayload): Promise<WebserverConfigActionResponse> {
    return this.request<WebserverConfigActionResponse>(
      `/api/v1/webservers/${encodeURIComponent(instanceId)}/config/apply`,
      { method: 'POST', body: JSON.stringify(payload) },
    );
  }

  async rollbackWebserverConfig(instanceId: string, payload: WebserverConfigActionPayload): Promise<WebserverConfigActionResponse> {
    return this.request<WebserverConfigActionResponse>(
      `/api/v1/webservers/${encodeURIComponent(instanceId)}/config/rollback`,
      { method: 'POST', body: JSON.stringify(payload) },
    );
  }

  async listWebserverConfigActions(params: {
    tenantId: string;
    instanceId: string;
    limit?: number;
  }): Promise<PaginatedResponse<WebserverConfigActionHistory>> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    return this.request<PaginatedResponse<WebserverConfigActionHistory>>(
      `/api/v1/webservers/${encodeURIComponent(params.instanceId)}/config/actions?${search.toString()}`,
    );
  }

  async listWebserverConfigReceipts(params: {
    tenantId: string;
    instanceId: string;
    limit?: number;
  }): Promise<PaginatedResponse<WebserverConfigReceipt>> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    return this.request<PaginatedResponse<WebserverConfigReceipt>>(
      `/api/v1/webservers/${encodeURIComponent(params.instanceId)}/config/receipts?${search.toString()}`,
    );
  }

  async listActiveBlocks(params: {
    tenantId: string;
    limit?: number;
    offset?: number;
    includeRemoved?: boolean;
  }): Promise<{ blocks: ActiveBlock[]; generated_at: string }> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    if (params.includeRemoved) search.set('include_removed', 'true');
    return this.request<{ blocks: ActiveBlock[]; generated_at: string }>(
      `/api/v1/network/active-blocks?${search.toString()}`,
    );
  }

  async listBlockNodes(entityActionId: string): Promise<{ rules: NodeFirewallRule[] }> {
    return this.request<{ rules: NodeFirewallRule[] }>(
      `/api/v1/network/blocks/${encodeURIComponent(entityActionId)}/nodes`,
    );
  }

  async listPatchDeployments(params: { tenantId: string; limit?: number; offset?: number }): Promise<{ deployments: PatchDeployment[]; generated_at: string }> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    return this.request<{ deployments: PatchDeployment[]; generated_at: string }>(
      `/api/v1/patch/deployments?${search.toString()}`,
    );
  }

  async createPatchDeployment(payload: {
    tenant_id: string;
    node_ids?: string[];
    mode?: 'direct' | 'proxy' | 'airgapped' | 'auto';
    reason?: string;
  }): Promise<PatchDeployResponse> {
    return this.request<PatchDeployResponse>(
      '/api/v1/patch/deployments',
      { method: 'POST', body: JSON.stringify(payload) },
    );
  }

  // ── Patch approvals (PR #65 c1-patch-approval-gate) ────────────────────
  async listPatchApprovals(params: {
    status?: 'pending' | 'approved' | 'denied' | 'expired';
    tenantId?: string;
    deploymentId?: string;
    nodeId?: string;
    limit?: number;
    offset?: number;
  } = {}): Promise<PaginatedResponse<PatchApproval>> {
    const search = new URLSearchParams();
    if (params.status) search.set('status', params.status);
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.deploymentId) search.set('deployment_id', params.deploymentId);
    if (params.nodeId) search.set('node_id', params.nodeId);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const query = search.toString();
    const suffix = query ? `?${query}` : '';
    const response = await this.request<RawPaginatedResponse<PatchApproval>>(
      `/api/v1/patch/approvals${suffix}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async approvePatchApproval(id: string): Promise<PatchApproval> {
    return this.request<PatchApproval>(
      `/api/v1/patch/approvals/${encodeURIComponent(id)}/approve`,
      { method: 'POST' },
    );
  }

  async denyPatchApproval(id: string): Promise<PatchApproval> {
    return this.request<PatchApproval>(
      `/api/v1/patch/approvals/${encodeURIComponent(id)}/deny`,
      { method: 'POST' },
    );
  }

  async listPatchDeploymentNodes(deploymentId: string): Promise<{ rows: NodePatchState[] }> {
    return this.request<{ rows: NodePatchState[] }>(
      `/api/v1/patch/deployments/${encodeURIComponent(deploymentId)}/nodes`,
    );
  }

  // ── Patch config (per-node mode) ───────────────────────────────────────
  async getNodePatchConfig(nodeId: string): Promise<NodePatchConfig> {
    return this.request<NodePatchConfig>(
      `/api/v1/patch/config?node_id=${encodeURIComponent(nodeId)}`,
    );
  }

  async upsertNodePatchConfig(payload: {
    node_id: string;
    mode: 'direct' | 'proxy' | 'airgapped';
    proxy_id?: string;
    window_id?: string;
  }): Promise<NodePatchConfig> {
    return this.request<NodePatchConfig>('/api/v1/patch/config', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  // ── Maintenance windows ────────────────────────────────────────────────
  async listMaintenanceWindows(tenantId: string): Promise<{ windows: MaintenanceWindow[] }> {
    return this.request<{ windows: MaintenanceWindow[] }>(
      `/api/v1/patch/maintenance-windows?tenant_id=${encodeURIComponent(tenantId)}`,
    );
  }

  async createMaintenanceWindow(payload: {
    tenant_id: string;
    name: string;
    node_ids: string[];
    opens_at: string;
    closes_at: string;
    allow_repos: string[];
  }): Promise<MaintenanceWindow> {
    return this.request<MaintenanceWindow>('/api/v1/patch/maintenance-windows', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async openMaintenanceWindow(id: string): Promise<{ status: string; action: string }> {
    return this.request<{ status: string; action: string }>(
      `/api/v1/patch/maintenance-windows/${encodeURIComponent(id)}/open`,
      { method: 'POST' },
    );
  }

  async closeMaintenanceWindow(id: string): Promise<{ status: string; action: string }> {
    return this.request<{ status: string; action: string }>(
      `/api/v1/patch/maintenance-windows/${encodeURIComponent(id)}/close`,
      { method: 'POST' },
    );
  }

  async forceCloseMaintenanceWindow(id: string): Promise<{ status: string }> {
    return this.request<{ status: string }>(
      `/api/v1/patch/maintenance-windows/${encodeURIComponent(id)}/force-close`,
      { method: 'POST' },
    );
  }

  // ── Squid proxies ──────────────────────────────────────────────────────
  async listSquidProxies(tenantId: string): Promise<{ proxies: SquidProxy[] }> {
    return this.request<{ proxies: SquidProxy[] }>(
      `/api/v1/patch/proxies?tenant_id=${encodeURIComponent(tenantId)}`,
    );
  }

  async createSquidProxy(payload: {
    tenant_id: string;
    host: string;
    port?: number;
    whitelist: string[];
  }): Promise<SquidProxy> {
    return this.request<SquidProxy>('/api/v1/patch/proxies', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async installSquidProxy(id: string, nodeId: string): Promise<{ status: string }> {
    return this.request<{ status: string }>(
      `/api/v1/patch/proxies/${encodeURIComponent(id)}/install`,
      { method: 'POST', body: JSON.stringify({ node_id: nodeId }) },
    );
  }

  async reconfigureSquidProxy(
    id: string,
    nodeId: string,
    whitelist: string[],
  ): Promise<{ status: string; validate: string }> {
    return this.request<{ status: string; validate: string }>(
      `/api/v1/patch/proxies/${encodeURIComponent(id)}/reconfigure`,
      { method: 'POST', body: JSON.stringify({ node_id: nodeId, whitelist }) },
    );
  }

  async removeSquidProxy(id: string): Promise<{ status: string }> {
    return this.request<{ status: string }>(
      `/api/v1/patch/proxies/${encodeURIComponent(id)}`,
      { method: 'DELETE' },
    );
  }

  // ── Onboarding wizard ──────────────────────────────────────────────────
  async listOnboardingProtocols(): Promise<{ protocols: OnboardingProtocol[] }> {
    return this.request<{ protocols: OnboardingProtocol[] }>('/api/v1/onboarding/protocols');
  }

  async testServerConnection(payload: TestConnectionPayload): Promise<TestConnectionResult> {
    return this.request<TestConnectionResult>('/api/v1/onboarding/test-connection', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async listPortRules(params: { tenantId?: string; policyId?: string; enabled?: boolean; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<PortRule>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.policyId) search.set('policy_id', params.policyId);
    if (typeof params.enabled === 'boolean') search.set('enabled', String(params.enabled));
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<PortRule>>(`/api/v1/rules/port${qs ? `?${qs}` : ''}`);
  }

  async createPortRule(payload: CreatePortRulePayload): Promise<PortRule> {
    return this.request<PortRule>('/api/v1/rules/port', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deletePortRule(id: string): Promise<void> {
    await this.request<void>(`/api/v1/rules/port/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listLogRules(params: { tenantId?: string; policyId?: string; enabled?: boolean; logSource?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<LogRule>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.policyId) search.set('policy_id', params.policyId);
    if (params.logSource) search.set('log_source', params.logSource);
    if (typeof params.enabled === 'boolean') search.set('enabled', String(params.enabled));
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<LogRule>>(`/api/v1/rules/log${qs ? `?${qs}` : ''}`);
  }

  async createLogRule(payload: CreateLogRulePayload): Promise<LogRule> {
    return this.request<LogRule>('/api/v1/rules/log', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteLogRule(id: string): Promise<void> {
    await this.request<void>(`/api/v1/rules/log/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listAlerts(params: { tenantId?: string; state?: string; severity?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<Alert>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.state) search.set('state', params.state);
    if (params.severity) search.set('severity', params.severity);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<Alert>>(`/api/v1/alerts${qs ? `?${qs}` : ''}`);
  }

  async ackAlert(id: string): Promise<void> {
    await this.request<void>(`/api/v1/alerts/${encodeURIComponent(id)}/ack`, { method: 'POST' });
  }

  async resolveAlert(id: string): Promise<void> {
    await this.request<void>(`/api/v1/alerts/${encodeURIComponent(id)}/resolve`, { method: 'POST' });
  }

  async listAccessRequests(params: { tenantId?: string; status?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<AccessRequest>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.status) search.set('status', params.status);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<AccessRequest>>(`/api/v1/access-requests${qs ? `?${qs}` : ''}`);
  }

  async createAccessRequest(payload: CreateAccessRequestPayload): Promise<AccessRequest> {
    return this.request<AccessRequest>('/api/v1/access-requests', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async approveAccessRequest(id: string, reason = ''): Promise<AccessRequest> {
    return this.request<AccessRequest>(`/api/v1/access-requests/${encodeURIComponent(id)}/approve`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    });
  }

  async denyAccessRequest(id: string, reason = ''): Promise<AccessRequest> {
    return this.request<AccessRequest>(`/api/v1/access-requests/${encodeURIComponent(id)}/deny`, {
      method: 'POST',
      body: JSON.stringify({ reason }),
    });
  }

  async listRecommendations(tenantId: string): Promise<{ data: Recommendation[] }> {
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    return this.request<{ data: Recommendation[] }>(`/api/v1/compliance/recommendations?${search.toString()}`);
  }

  async listReports(): Promise<{ data: ReportDesc[] }> {
    return this.request<{ data: ReportDesc[] }>('/api/v1/reports');
  }

  async listSessions(params: { tenantId?: string; nodeId?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<SessionRecording>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.nodeId) search.set('node_id', params.nodeId);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<SessionRecording>>(`/api/v1/sessions${qs ? `?${qs}` : ''}`);
  }

  async getSessionParsed(id: string, search?: string): Promise<SessionParsedResponse> {
    const qs = new URLSearchParams();
    if (search) qs.set('search', search);
    const suffix = qs.toString();
    return this.request<SessionParsedResponse>(`/api/v1/sessions/${encodeURIComponent(id)}/parsed${suffix ? `?${suffix}` : ''}`);
  }

  async getSessionTranscript(id: string): Promise<string> {
    const resp = await fetch(`${this.baseUrl}/api/v1/sessions/${encodeURIComponent(id)}/transcript`, {
      headers: { ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}) },
    });
    if (!resp.ok) throw new APIError('Failed to load transcript', resp.status);
    return resp.text();
  }

  async listThreatFeeds(tenantId: string): Promise<{ data: ThreatFeed[] }> {
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    return this.request<{ data: ThreatFeed[] }>(`/api/v1/threat-feeds?${search.toString()}`);
  }

  async createThreatFeed(payload: CreateThreatFeedPayload): Promise<ThreatFeed> {
    return this.request<ThreatFeed>('/api/v1/threat-feeds', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateThreatFeed(id: string, payload: UpdateThreatFeedPayload): Promise<ThreatFeed> {
    return this.request<ThreatFeed>(`/api/v1/threat-feeds/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deleteThreatFeed(id: string): Promise<void> {
    await this.request<void>(`/api/v1/threat-feeds/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async simulateRule(payload: { tenant_id: string; rule_type: string; window_days?: number; rule: Record<string, unknown> }): Promise<SimulateResult> {
    return this.request<SimulateResult>('/api/v1/compliance/simulate', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  buildReportExportUrl(slug: string, params: { tenantId?: string; since?: string } = {}): string {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    const qs = search.toString();
    return `${this.baseUrl}/api/v1/reports/${encodeURIComponent(slug)}${qs ? `?${qs}` : ''}`;
  }

  // streamEvents opens an authenticated Server-Sent Events stream using
  // fetch + ReadableStream. Browsers' native EventSource cannot set custom
  // Authorization headers, so we parse the SSE wire format ourselves. Returns
  // an AbortController.abort-style cleanup function.
  streamEvents(
    opts: { tenantId: string; topics?: string[]; nodeId?: string },
    onEvent: (ev: { topic: string; payload: unknown; tenant_id: string; node_id?: string }) => void,
    onError?: (err: unknown) => void,
  ): () => void {
    const controller = new AbortController();
    const search = new URLSearchParams();
    search.set('tenant_id', opts.tenantId);
    if (opts.nodeId) search.set('node_id', opts.nodeId);
    if (opts.topics && opts.topics.length) search.set('topics', opts.topics.join(','));
    const url = `${this.baseUrl}/api/v1/events/stream?${search.toString()}`;

    const run = async () => {
      try {
        const resp = await fetch(url, {
          headers: {
            Accept: 'text/event-stream',
            ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
          },
          signal: controller.signal,
        });
        if (!resp.ok || !resp.body) {
          throw new Error(`events stream status ${resp.status}`);
        }
        const reader = resp.body.getReader();
        const decoder = new TextDecoder();
        let buf = '';
        while (!controller.signal.aborted) {
          const { value, done } = await reader.read();
          if (done) break;
          buf += decoder.decode(value, { stream: true });
          let idx;
          while ((idx = buf.indexOf('\n\n')) !== -1) {
            const frame = buf.slice(0, idx);
            buf = buf.slice(idx + 2);
            const dataLine = frame.split('\n').find((l) => l.startsWith('data: '));
            if (!dataLine) continue;
            try {
              const parsed = JSON.parse(dataLine.slice(6));
              onEvent(parsed);
            } catch {
              // ignore malformed frame
            }
          }
        }
      } catch (err) {
        if (!controller.signal.aborted && onError) {
          onError(err);
        }
      }
    };
    void run();
    return () => controller.abort();
  }

  // buildBundleDownloadUrl returns the fully qualified GET URL for the air-gapped
  // bundle endpoint. The wizard points `window.location` at this URL so the
  // browser handles the tarball download directly.
  buildBundleDownloadUrl(options: BundleDownloadOptions): string {
    const search = new URLSearchParams();
    search.set('os', options.os);
    search.set('arch', options.arch);
    search.set('token', options.token);
    return `${this.baseUrl}/api/v1/agent/bundle?${search.toString()}`;
  }

  async listOfflineContentBundles(params: {
    tenantId: string;
    bundleId?: string;
    status?: string;
    limit?: number;
    offset?: number;
  }): Promise<{ items: OfflineContentBundle[]; pagination: PaginationMeta }> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.bundleId) search.set('bundle_id', params.bundleId);
    if (params.status) search.set('status', params.status);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    return this.request<{ items: OfflineContentBundle[]; pagination: PaginationMeta }>(
      `/api/v1/offline-bundles?${search.toString()}`,
    );
  }

  async importOfflineContentBundle(tenantId: string, bundle: Blob): Promise<OfflineContentBundle> {
    const search = new URLSearchParams({ tenant_id: tenantId });
    return this.request<OfflineContentBundle>(`/api/v1/offline-bundles?${search.toString()}`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/octet-stream' },
      body: bundle,
    });
  }

  async rollbackOfflineContentBundle(tenantId: string, bundleId: string, sequence: number): Promise<OfflineContentBundle> {
    const search = new URLSearchParams({ tenant_id: tenantId });
    return this.request<OfflineContentBundle>(`/api/v1/offline-bundles/${encodeURIComponent(bundleId)}/rollback?${search.toString()}`, {
      method: 'POST',
      body: JSON.stringify({ bundle_id: bundleId, sequence }),
    });
  }

  private async request<T>(path: string, init: RequestInit = {}): Promise<T> {
    const response = await fetch(`${this.baseUrl}${path}`, {
      ...init,
      headers: {
        'Content-Type': 'application/json',
        ...(this.token ? { Authorization: `Bearer ${this.token}` } : {}),
        ...(init.headers ?? {}),
      },
    });

    if (!response.ok) {
      if (response.status === HTTP_STATUS_UNAUTHORIZED && this.unauthorizedHandler) {
        this.unauthorizedHandler();
      }
      const message = await safeErrorMessage(response);
      throw new APIError(message || `Request failed with status ${response.status}`, response.status);
    }

    if (response.status === 204 || response.status === 205 || response.headers.get('Content-Length') === '0') {
      return undefined as T;
    }

    return (await response.json()) as T;
  }

  // ---- Connections / forensics (Phase 7) -------------------------------

  async listConnections(params: ListConnectionsParams = {}): Promise<ConnectionRow[]> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.ip) search.set('ip', params.ip);
    if (params.nodeId) search.set('node_id', params.nodeId);
    if (typeof params.openOnly === 'boolean') search.set('open_only', String(params.openOnly));
    if (params.since) search.set('since', params.since);
    if (params.until) search.set('until', params.until);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    const q = search.toString();
    const resp = await this.request<ConnectionRow[] | { data: ConnectionRow[] }>(`/api/v1/connections${q ? `?${q}` : ''}`);
    return Array.isArray(resp) ? resp : resp.data ?? [];
  }

  async getConnectionDetail(connID: string): Promise<ConnectionDetail> {
    return this.request<ConnectionDetail>(`/api/v1/connections/${encodeURIComponent(connID)}`);
  }

  async listTopTalkers(params: { tenantId?: string; since?: string; limit?: number } = {}): Promise<TopTalker[]> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    const q = search.toString();
    return this.request<TopTalker[]>(`/api/v1/connections/top-talkers${q ? `?${q}` : ''}`);
  }

  async fleetHealthSnapshot(params: { tenantId?: string; since?: string } = {}): Promise<FleetHealthSnapshot> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.since) search.set('since', params.since);
    const q = search.toString();
    return this.request<FleetHealthSnapshot>(`/api/v1/fleet/health${q ? `?${q}` : ''}`);
  }

  async getTenantEventFilters(tenantId: string): Promise<TenantEventFilters> {
    return this.request<TenantEventFilters>(`/api/v1/tenants/${encodeURIComponent(tenantId)}/event-filters`);
  }

  async updateTenantEventFilters(tenantId: string, payload: Partial<TenantEventFilters>): Promise<TenantEventFilters> {
    return this.request<TenantEventFilters>(`/api/v1/tenants/${encodeURIComponent(tenantId)}/event-filters`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  // ── Agent self-update ─────────────────────────────────────────────────
  async updateAgent(nodeId: string, targetVersion?: string): Promise<AgentUpdateResponse> {
    return this.request<AgentUpdateResponse>(`/api/v1/nodes/${encodeURIComponent(nodeId)}/update-agent`, {
      method: 'POST',
      body: JSON.stringify({ target_version: targetVersion ?? '' }),
    });
  }

  // ── Behavioral baselines ──────────────────────────────────────────────
  async listBehavioralBaselines(params: { tenantId?: string; nodeId?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<BehavioralBaseline>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.nodeId) search.set('node_id', params.nodeId);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<BehavioralBaseline>>(`/api/v1/behavioral/baselines${qs ? `?${qs}` : ''}`);
  }

  async listAnomalies(params: { tenantId?: string; baselineId?: string; sourceIp?: string; resolved?: boolean; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<BehavioralAnomaly>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (params.baselineId) search.set('baseline_id', params.baselineId);
    if (params.sourceIp) search.set('src_ip', params.sourceIp);
    if (typeof params.resolved === 'boolean') search.set('resolved', String(params.resolved));
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<BehavioralAnomaly>>(`/api/v1/behavioral/anomalies${qs ? `?${qs}` : ''}`);
  }

  async suppressAnomaly(id: string): Promise<BehavioralAnomaly> {
    return this.request<BehavioralAnomaly>(`/api/v1/behavioral/anomalies/${encodeURIComponent(id)}/suppress`, {
      method: 'POST',
    });
  }

  async resolveAnomaly(id: string): Promise<BehavioralAnomaly> {
    return this.request<BehavioralAnomaly>(`/api/v1/behavioral/anomalies/${encodeURIComponent(id)}/resolve`, {
      method: 'POST',
    });
  }

  // ── Correlation rules ─────────────────────────────────────────────────
  async listCorrelationRules(params: { tenantId?: string; enabled?: boolean; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<CorrelationRule>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (typeof params.enabled === 'boolean') search.set('enabled', String(params.enabled));
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<CorrelationRule>>(`/api/v1/correlation-rules${qs ? `?${qs}` : ''}`);
  }

  async createCorrelationRule(payload: CreateCorrelationRulePayload): Promise<CorrelationRule> {
    return this.request<CorrelationRule>('/api/v1/correlation-rules', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  // ---- DLP / Data Classification (Sprint 2) --------------------------------

  async listDLPRules(tenantId: string): Promise<PaginatedResponse<DataClassificationRule>> {
    return this.request<PaginatedResponse<DataClassificationRule>>(
      `/api/v1/dlp/rules?tenant_id=${encodeURIComponent(tenantId)}`,
    );
  }

  async createDLPRule(payload: CreateDLPRulePayload): Promise<DataClassificationRule> {
    return this.request<DataClassificationRule>('/api/v1/dlp/rules', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  // ---- Sprint 3: Compliance Evidence + Audit Reports + Frameworks ----------

  async uploadComplianceEvidence(formData: FormData): Promise<ComplianceEvidence> {
    const url = `${this.baseUrl}/api/v1/compliance/evidence`;
    const headers: Record<string, string> = {};
    if (this.token) headers['Authorization'] = `Bearer ${this.token}`;
    const res = await fetch(url, { method: 'POST', headers, body: formData });
    if (res.status === HTTP_STATUS_UNAUTHORIZED) {
      this.unauthorizedHandler?.();
      throw new APIError('Unauthorized', res.status);
    }
    if (!res.ok) {
      const msg = await safeErrorMessage(res);
      throw new APIError(msg ?? res.statusText, res.status);
    }
    return res.json() as Promise<ComplianceEvidence>;
  }

  async listComplianceEvidence(params: {
    tenantId: string;
    framework?: string;
    evidenceType?: string;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<ComplianceEvidence>> {
    const q = new URLSearchParams({ tenant_id: params.tenantId });
    if (params.framework) q.set('framework', params.framework);
    if (params.evidenceType) q.set('evidence_type', params.evidenceType);
    if (params.limit !== undefined) q.set('limit', String(params.limit));
    if (params.offset !== undefined) q.set('offset', String(params.offset));
    const raw = await this.request<{ data: ComplianceEvidence[]; pagination: ServerPaginationMeta }>(
      `/api/v1/compliance/evidence?${q.toString()}`
    );
    return {
      data: raw.data ?? [],
      pagination: {
        total: raw.pagination?.total ?? 0,
        count: (raw.data ?? []).length,
        limit: raw.pagination?.limit ?? (params.limit ?? 50),
        offset: raw.pagination?.offset ?? (params.offset ?? 0),
        nextOffset: raw.pagination?.next_offset ?? null,
        prevOffset: raw.pagination?.prev_offset ?? null,
      },
    };
  }

  async deleteComplianceEvidence(id: string, tenantId: string): Promise<void> {
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    await this.request<void>(
      `/api/v1/compliance/evidence/${encodeURIComponent(id)}?${search.toString()}`,
      { method: 'DELETE' },
    );
  }

  async listComplianceFrameworks(): Promise<{ frameworks: string[]; controls: Record<string, FrameworkControl[]> }> {
    return this.request<{ frameworks: string[]; controls: Record<string, FrameworkControl[]> }>(
      '/api/v1/compliance/frameworks'
    );
  }

  async getControlPosture(params: {
    framework: string;
    tenant_id: string;
    period_start?: string;
    period_end?: string;
  }): Promise<ControlPostureResponse> {
    const search = new URLSearchParams();
    search.set('framework', params.framework);
    search.set('tenant_id', params.tenant_id);
    if (params.period_start) search.set('period_start', params.period_start);
    if (params.period_end) search.set('period_end', params.period_end);
    return this.request<ControlPostureResponse>(`/api/v1/compliance/control-posture?${search.toString()}`);
  }

  async createAuditReport(payload: CreateAuditReportPayload): Promise<AuditReport> {
    return this.request<AuditReport>('/api/v1/compliance/reports', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteCorrelationRule(id: string, tenantId: string): Promise<void> {
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    await this.request<void>(
      `/api/v1/correlation-rules/${encodeURIComponent(id)}?${search.toString()}`,
      { method: 'DELETE' },
    );
  }

  // ── Command ACLs ──────────────────────────────────────────────────────
  async listCommandACLs(params: { tenantId?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<CommandACL>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<CommandACL>>(`/api/v1/command-acls${qs ? `?${qs}` : ''}`);
  }

  async createCommandACL(payload: CreateCommandACLPayload): Promise<CommandACL> {
    return this.request<CommandACL>('/api/v1/command-acls', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async deleteCommandACL(id: string): Promise<void> {
    await this.request<void>(`/api/v1/command-acls/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  // ── Cluster rollout waves ─────────────────────────────────────────────
  async listClusterRolloutWaves(params: { tenantId?: string; limit?: number; offset?: number } = {}): Promise<PaginatedResponse<ClusterRolloutWave>> {
    const search = new URLSearchParams();
    if (params.tenantId) search.set('tenant_id', params.tenantId);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    const qs = search.toString();
    return this.request<PaginatedResponse<ClusterRolloutWave>>(`/api/v1/rollout/waves${qs ? `?${qs}` : ''}`);
  }

  async updateClusterRolloutWave(id: string, payload: UpdateClusterRolloutWavePayload): Promise<ClusterRolloutWave> {
    return this.request<ClusterRolloutWave>(`/api/v1/rollout/waves/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  // ── MFA factors ───────────────────────────────────────────────────────
  async listMFAFactors(): Promise<{ factors: MFAFactor[] }> {
    return this.request<{ factors: MFAFactor[] }>('/api/v1/auth/mfa/factors');
  }

  async deleteMFAFactor(id: string): Promise<void> {
    await this.request<void>(`/api/v1/auth/mfa/factors/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async getMFARecoveryCodes(): Promise<{ codes: string[] }> {
    return this.request<{ codes: string[] }>('/api/v1/auth/mfa/recovery-codes');
  }

  // TOTP enrollment
  async beginTOTPEnroll(): Promise<{ factor_id: string; secret: string; provisioning_uri: string }> {
    return this.request<{ factor_id: string; secret: string; provisioning_uri: string }>('/api/v1/mfa/totp/enroll/begin', { method: 'POST' });
  }

  async finishTOTPEnroll(factorId: string, code: string, label: string): Promise<{ factor_id: string; verified: boolean }> {
    return this.request<{ factor_id: string; verified: boolean }>('/api/v1/mfa/totp/enroll/finish', {
      method: 'POST',
      body: JSON.stringify({ factor_id: factorId, code, label }),
    });
  }

  // WebAuthn enrollment
  async beginWebAuthnEnroll(): Promise<{ challenge: unknown; user: unknown }> {
    return this.request<{ challenge: unknown; user: unknown }>('/api/v1/mfa/webauthn/enroll/begin', { method: 'POST' });
  }

  async finishWebAuthnEnroll(credential: unknown): Promise<{ factor_id: string; verified: boolean }> {
    return this.request<{ factor_id: string; verified: boolean }>('/api/v1/mfa/webauthn/enroll/finish', {
      method: 'POST',
      body: JSON.stringify(credential),
    });
  }

  // ── Trust Center admin ───────────────────────────────────────────────
  async listSubprocessors(tenantId: string): Promise<unknown[]> {
    return this.request<unknown[]>(`/api/v1/trust/subprocessors?tenant_id=${encodeURIComponent(tenantId)}`);
  }

  async createSubprocessor(tenantId: string, data: Record<string, unknown>): Promise<unknown> {
    return this.request<unknown>('/api/v1/trust/subprocessors', {
      method: 'POST',
      body: JSON.stringify({ ...data, tenant_id: tenantId }),
    });
  }

  async deleteSubprocessor(id: string): Promise<void> {
    await this.request<void>(`/api/v1/trust/subprocessors/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listCertifications(tenantId: string): Promise<unknown[]> {
    return this.request<unknown[]>(`/api/v1/trust/certifications?tenant_id=${encodeURIComponent(tenantId)}`);
  }

  async createCertification(tenantId: string, data: Record<string, unknown>): Promise<unknown> {
    return this.request<unknown>('/api/v1/trust/certifications', {
      method: 'POST',
      body: JSON.stringify({ ...data, tenant_id: tenantId }),
    });
  }

  async deleteCertification(id: string): Promise<void> {
    await this.request<void>(`/api/v1/trust/certifications/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listFAQItems(tenantId: string): Promise<unknown[]> {
    return this.request<unknown[]>(`/api/v1/trust/faq?tenant_id=${encodeURIComponent(tenantId)}`);
  }

  async createFAQItem(tenantId: string, data: Record<string, unknown>): Promise<unknown> {
    return this.request<unknown>('/api/v1/trust/faq', {
      method: 'POST',
      body: JSON.stringify({ ...data, tenant_id: tenantId }),
    });
  }

  async deleteFAQItem(id: string): Promise<void> {
    await this.request<void>(`/api/v1/trust/faq/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listIncidents(tenantId: string): Promise<unknown[]> {
    return this.request<unknown[]>(`/api/v1/trust/incidents?tenant_id=${encodeURIComponent(tenantId)}`);
  }

  async createIncident(tenantId: string, data: Record<string, unknown>): Promise<unknown> {
    return this.request<unknown>('/api/v1/trust/incidents', {
      method: 'POST',
      body: JSON.stringify({ ...data, tenant_id: tenantId }),
    });
  }

  async deleteIncident(id: string): Promise<void> {
    await this.request<void>(`/api/v1/trust/incidents/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  // ── Tenant remediation config ─────────────────────────────────────────
  async getTenantRemediationConfig(tenantId: string): Promise<TenantRemediationConfig> {
    return this.request<TenantRemediationConfig>(`/api/v1/tenants/${encodeURIComponent(tenantId)}/remediation-config`);
  }

  async upsertTenantRemediationConfig(tenantId: string, payload: Partial<TenantRemediationConfig>): Promise<TenantRemediationConfig> {
    return this.request<TenantRemediationConfig>(`/api/v1/tenants/${encodeURIComponent(tenantId)}/remediation-config`, {
      method: 'PUT',
      body: JSON.stringify(payload),
    });
  }

  // ── Worker pool status ────────────────────────────────────────────────
  async getWorkerPoolStatus(): Promise<WorkerPoolStatus> {
    return this.request<WorkerPoolStatus>('/api/v1/admin/worker-pool');
  }

  // ---- DLP / Data Classification (Sprint 2) --------------------------------

  async deleteDLPRule(id: string): Promise<void> {
    await this.request<void>(`/api/v1/dlp/rules/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listColumnClassifications(params: {
    tenantId: string;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<ColumnClassification>> {
    const q = new URLSearchParams({ tenant_id: params.tenantId });
    if (params.limit !== undefined) q.set('limit', String(params.limit));
    if (params.offset !== undefined) q.set('offset', String(params.offset));
    return this.request<PaginatedResponse<ColumnClassification>>(`/api/v1/dlp/columns?${q}`);
  }

  async listPIIFindings(params: {
    tenantId: string;
    resolved?: boolean;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<PIIFinding>> {
    const q = new URLSearchParams({ tenant_id: params.tenantId });
    if (params.resolved !== undefined) q.set('resolved', String(params.resolved));
    if (params.limit !== undefined) q.set('limit', String(params.limit));
    if (params.offset !== undefined) q.set('offset', String(params.offset));
    return this.request<PaginatedResponse<PIIFinding>>(`/api/v1/dlp/findings?${q}`);
  }

  async resolvePIIFinding(id: string): Promise<void> {
    await this.request<void>(`/api/v1/dlp/findings/${encodeURIComponent(id)}/resolve`, {
      method: 'POST',
    });
  }

  async seedDLPRules(tenantId: string): Promise<{ seeded: number }> {
    return this.request<{ seeded: number }>('/api/v1/dlp/seed-rules', {
      method: 'POST',
      body: JSON.stringify({ tenant_id: tenantId }),
    });
  }

  // ---- Sprint 3: Audit Reports --------------------------------

  async listAuditReports(params: {
    tenantId: string;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<AuditReport>> {
    const q = new URLSearchParams({ tenant_id: params.tenantId });
    if (params.limit !== undefined) q.set('limit', String(params.limit));
    if (params.offset !== undefined) q.set('offset', String(params.offset));
    const raw = await this.request<{ data: AuditReport[]; pagination: ServerPaginationMeta }>(
      `/api/v1/compliance/reports?${q.toString()}`
    );
    return {
      data: raw.data ?? [],
      pagination: {
        total: raw.pagination?.total ?? 0,
        count: (raw.data ?? []).length,
        limit: raw.pagination?.limit ?? (params.limit ?? 50),
        offset: raw.pagination?.offset ?? (params.offset ?? 0),
        nextOffset: raw.pagination?.next_offset ?? null,
        prevOffset: raw.pagination?.prev_offset ?? null,
      },
    };
  }

  buildReportDownloadUrl(id: string): string {
    return `${this.baseUrl}/api/v1/compliance/reports/${encodeURIComponent(id)}/download`;
  }

  buildEvidenceDownloadUrl(id: string, tenantId: string): string {
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    return `${this.baseUrl}/api/v1/compliance/evidence/${encodeURIComponent(id)}/download?${search.toString()}`;
  }

  // ---- Compliance Reviews ----------------------------------------

  async listComplianceReviews(params: {
    tenantId: string;
    limit?: number;
    offset?: number;
  }): Promise<PaginatedResponse<ComplianceReview>> {
    const q = new URLSearchParams({ tenant_id: params.tenantId });
    if (params.limit !== undefined) q.set('limit', String(params.limit));
    if (params.offset !== undefined) q.set('offset', String(params.offset));
    const raw = await this.request<{ data: ComplianceReview[]; pagination: ServerPaginationMeta }>(
      `/api/v1/compliance/reviews?${q.toString()}`
    );
    return {
      data: raw.data ?? [],
      pagination: {
        total: raw.pagination?.total ?? 0,
        count: (raw.data ?? []).length,
        limit: raw.pagination?.limit ?? (params.limit ?? 50),
        offset: raw.pagination?.offset ?? (params.offset ?? 0),
        nextOffset: raw.pagination?.next_offset ?? null,
        prevOffset: raw.pagination?.prev_offset ?? null,
      },
    };
  }

  async createComplianceReview(payload: CreateComplianceReviewPayload): Promise<ComplianceReview> {
    return this.request<ComplianceReview>('/api/v1/compliance/reviews', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async completeComplianceReview(id: string, notes?: string): Promise<ComplianceReview> {
    return this.request<ComplianceReview>(`/api/v1/compliance/reviews/${encodeURIComponent(id)}/complete`, {
      method: 'POST',
      body: JSON.stringify({ notes }),
    });
  }

  async deleteComplianceReview(id: string): Promise<void> {
    await this.request<void>(`/api/v1/compliance/reviews/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  // ---- Misconduct & whistleblowing (UC7) -----------------------------
  // Investigator-gated case CRUD. The public submit + status endpoints are
  // exported as standalone functions below (they bypass the bearer-token
  // header so they work on the unauthenticated /intake routes).

  async listMisconductCases(params: {
    tenantId: string;
    status?: 'open' | 'investigating' | 'closed';
    limit?: number;
    offset?: number;
  }): Promise<{ data: MisconductCase[]; pagination: { total: number; limit: number; offset: number } }> {
    const search = new URLSearchParams();
    search.set('tenant_id', params.tenantId);
    if (params.status) search.set('status', params.status);
    if (typeof params.limit === 'number') search.set('limit', String(params.limit));
    if (typeof params.offset === 'number') search.set('offset', String(params.offset));
    return this.request(`/api/v1/misconduct/cases?${search.toString()}`);
  }

  async createMisconductCase(payload: CreateMisconductCasePayload): Promise<MisconductCase> {
    return this.request<MisconductCase>('/api/v1/misconduct/cases', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async getMisconductCase(id: string): Promise<MisconductCase> {
    return this.request<MisconductCase>(`/api/v1/misconduct/cases/${encodeURIComponent(id)}`);
  }

  async updateMisconductCase(id: string, payload: UpdateMisconductCasePayload): Promise<MisconductCase> {
    return this.request<MisconductCase>(`/api/v1/misconduct/cases/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async listCaseSignals(id: string): Promise<{ data: RiskSignal[] }> {
    return this.request(`/api/v1/misconduct/cases/${encodeURIComponent(id)}/signals`);
  }

  async listCaseEvidence(id: string): Promise<{ data: CaseEvidenceLink[] }> {
    return this.request(`/api/v1/misconduct/cases/${encodeURIComponent(id)}/evidence`);
  }

  async attachCaseEvidence(id: string, evidenceId: string): Promise<CaseEvidenceLink> {
    return this.request<CaseEvidenceLink>(`/api/v1/misconduct/cases/${encodeURIComponent(id)}/evidence`, {
      method: 'POST',
      body: JSON.stringify({ evidence_id: evidenceId }),
    });
  }

  // ── Finacle integration (UC6) ────────────────────────────────────────────
  async listFinacleConnections(tenantId: string): Promise<{ connections: FinacleConnection[] }> {
    return this.request<{ connections: FinacleConnection[] }>(
      `/api/v1/finacle/connections?tenant_id=${encodeURIComponent(tenantId)}`,
    );
  }

  async createFinacleConnection(payload: CreateFinacleConnectionPayload): Promise<FinacleConnection> {
    return this.request<FinacleConnection>('/api/v1/finacle/connections', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateFinacleConnection(id: string, payload: UpdateFinacleConnectionPayload): Promise<FinacleConnection> {
    return this.request<FinacleConnection>(`/api/v1/finacle/connections/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deleteFinacleConnection(id: string): Promise<void> {
    await this.request<void>(`/api/v1/finacle/connections/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async testFinacleConnection(id: string): Promise<FinacleConnectionTestResult> {
    return this.request<FinacleConnectionTestResult>(
      `/api/v1/finacle/connections/${encodeURIComponent(id)}/test`,
      { method: 'POST' },
    );
  }

  async listFinacleShiftConfigs(tenantId: string): Promise<{ configs: FinacleShiftConfig[] }> {
    return this.request<{ configs: FinacleShiftConfig[] }>(
      `/api/v1/finacle/shift-configs?tenant_id=${encodeURIComponent(tenantId)}`,
    );
  }

  async createFinacleShiftConfig(payload: CreateFinacleShiftConfigPayload): Promise<FinacleShiftConfig> {
    return this.request<FinacleShiftConfig>('/api/v1/finacle/shift-configs', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }

  async updateFinacleShiftConfig(id: string, payload: UpdateFinacleShiftConfigPayload): Promise<FinacleShiftConfig> {
    return this.request<FinacleShiftConfig>(`/api/v1/finacle/shift-configs/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async deleteFinacleShiftConfig(id: string): Promise<void> {
    await this.request<void>(`/api/v1/finacle/shift-configs/${encodeURIComponent(id)}`, { method: 'DELETE' });
  }

  async listFinacleProfiles(
    tenantId: string,
    params: { limit?: number; offset?: number } = {},
  ): Promise<PaginatedResponse<FinacleProfile>> {
    const search = new URLSearchParams();
    search.set('tenant_id', tenantId);
    if (params.limit !== undefined) search.set('limit', String(params.limit));
    if (params.offset !== undefined) search.set('offset', String(params.offset));
    const response = await this.request<RawPaginatedResponse<FinacleProfile>>(
      `/api/v1/finacle/profiles?${search.toString()}`,
    );
    return {
      data: response.data,
      pagination: normalizePagination(response.pagination),
    };
  }

  async updateFinacleProfile(id: string, payload: UpdateFinacleProfilePayload): Promise<FinacleProfile> {
    return this.request<FinacleProfile>(`/api/v1/finacle/profiles/${encodeURIComponent(id)}`, {
      method: 'PATCH',
      body: JSON.stringify(payload),
    });
  }

  async triggerFinacleShiftRotate(payload: FinacleShiftRotatePayload): Promise<FinacleShiftRotateResponse> {
    return this.request<FinacleShiftRotateResponse>('/api/v1/finacle/shift-rotate', {
      method: 'POST',
      body: JSON.stringify(payload),
    });
  }
}

// ---- Public misconduct intake helpers (no auth) ----------------------
//
// These mirror the trust-center pattern (see TrustCenter.tsx — uses raw
// fetch instead of the APIClient because the bearer token would be
// undefined on the /intake routes anyway and we don't want to leak any
// session token to a public surface).

export interface MisconductChallenge {
  challenge: string;
  difficulty: number;
}

export interface MisconductSubmitResponse {
  token: string;
  message: string;
}

export interface MisconductStatusResponse {
  status: 'received' | 'under_review' | 'closed' | 'unknown';
}

export async function fetchMisconductChallenge(): Promise<MisconductChallenge> {
  const r = await fetch('/api/v1/misconduct/challenge');
  if (!r.ok) throw new Error(`HTTP ${r.status}`);
  return r.json();
}

export async function submitWhistleblowerReport(payload: {
  description: string;
  approximate_date: string;
  subject_role: string;
  challenge: string;
  nonce: string;
}): Promise<MisconductSubmitResponse> {
  const r = await fetch('/api/v1/misconduct/submit', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(payload),
  });
  if (!r.ok) {
    const text = await r.text();
    throw new APIError(text || `HTTP ${r.status}`, r.status);
  }
  return r.json();
}

export async function fetchIntakeStatus(token: string): Promise<MisconductStatusResponse> {
  const r = await fetch('/api/v1/misconduct/intake-status', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  });
  if (!r.ok) throw new APIError(`HTTP ${r.status}`, r.status);
  return r.json();
}

// ---- Misconduct types (UC7) -----------------------------------------

export interface MisconductCase {
  id: string;
  tenant_id: string;
  status: 'open' | 'investigating' | 'closed';
  opened_at: string;
  opened_by?: string;
  summary: string;
  risk_score: number;
  subject_user_id?: string;
  subject_label?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateMisconductCasePayload {
  tenant_id: string;
  summary: string;
  subject_user_id?: string;
  subject_label?: string;
}

export interface UpdateMisconductCasePayload {
  status?: 'open' | 'investigating' | 'closed';
  summary?: string;
  subject_user_id?: string;
  subject_label?: string;
}

export interface RiskSignal {
  id: string;
  case_id: string;
  signal_type: string;
  severity: 'critical' | 'high' | 'medium' | 'low';
  source_id?: string;
  source_table?: string;
  occurred_at: string;
  weight: number;
}

export interface CaseEvidenceLink {
  case_id: string;
  evidence_id: string;
  attached_at: string;
}

// ---- Connection / forensic types -------------------------------------

export interface ListConnectionsParams {
  tenantId?: string;
  ip?: string;
  nodeId?: string;
  openOnly?: boolean;
  since?: string;
  until?: string;
  limit?: number;
}

// ── Network Security (PR 3) ────────────────────────────────────────────────

export interface EntityActionResponse {
  id: string;
  tenant_id: string;
  entity_type: string;
  entity_id: string;
  action: string;
  reason?: string;
  ttl_seconds?: number;
  expires_at?: string;
  created_by?: string;
  created_at: string;
  nodes_dispatched: number;
  node_ids?: string[];
  scope?: 'affected' | 'fleet';
}

export interface ActiveBlock {
  EntityActionID: string;
  TenantID: string;
  EntityType: string;
  EntityID: string;
  Action: string;
  Reason?: string;
  ExpiresAt?: string;
  CreatedAt: string;
  TotalNodes: number;
  NodesApplied: number;
  NodesFailed: number;
  NodesPending: number;
  NodesRemoved: number;
}

export interface IPBlockProposal {
  id: string;
  tenant_id: string;
  finding_id?: string;
  entity_action_id?: string;
  ip_cidr: string;
  scope: string;
  target_type: string;
  target_id?: string;
  server_group: string;
  app: string;
  vhost: string;
  enforcement: string;
  status: 'proposed' | 'approved' | 'canary' | 'dispatching' | 'active' | 'failed' | 'expired' | 'removed' | 'denied' | 'rejected' | 'rolled_back';
  reason: string;
  score: number;
  expires_at?: string;
  approved_by?: string;
  approved_at?: string;
  last_error?: string;
  protected_override: boolean;
  protected_override_reason?: string;
  created_at: string;
  updated_at: string;
}

export interface ASNBlockProposalSkipped {
  source_ip: string;
  reason: string;
}

export interface ASNBlockProposalsResponse {
  asn: string;
  total_candidates: number;
  created: IPBlockProposal[];
  skipped: ASNBlockProposalSkipped[];
  limit: number;
  generated_at: string;
}

export interface IPBehaviorCountrySummary {
  country_code: string;
  country: string;
  unique_source_ips: number;
  request_count: number;
  bytes_out: number;
  status_counts: Record<string, number>;
  first_seen_at: string;
  last_seen_at: string;
  top_asns?: string[];
  top_apps?: string[];
  server_groups?: string[];
}

export interface IPBehaviorOverview {
  tenant_id: string;
  since: string;
  request_count: number;
  bytes_out: number;
  status_counts: Record<string, number>;
  top_countries: IPBehaviorCountrySummary[];
  generated_at: string;
}

export interface IPBehaviorIPProfile {
  source_ip: string;
  countries: string[];
  asns: string[];
  isps?: string[];
  apps?: string[];
  server_groups?: string[];
  node_ids?: string[];
  history?: IPBehaviorHistoryPoint[];
  request_count: number;
  bytes_out: number;
  status_counts: Record<string, number>;
  first_seen_at: string;
  last_seen_at: string;
}

export interface IPBehaviorHistoryPoint {
  hour_ts: string;
  request_count: number;
  bytes_out: number;
  status_counts: Record<string, number>;
}

export interface IPBehaviorBaseline {
  id?: string;
  tenant_id?: string;
  dimension?: string;
  dimension_key?: string;
  baseline?: Record<string, unknown>;
  window_days?: number;
  sample_count?: number;
  computed_at?: string;
  ID?: string;
  TenantID?: string;
  Dimension?: string;
  DimensionKey?: string;
  Baseline?: Record<string, unknown>;
  WindowDays?: number;
  SampleCount?: number;
  ComputedAt?: string;
}

export interface WebserverInstance {
  ID: string;
  TenantID: string;
  NodeID: string;
  Kind: string;
  Version: string;
  ServiceName: string;
  ConfigPath: string;
  AccessLogPath: string;
  ErrorLogPath: string;
  VHosts?: Record<string, unknown>[];
  Capabilities?: Record<string, unknown>;
  ObservedAt: string;
}

export interface WebserverConfigActionResponse {
  job_id: string;
  action_id: string;
  status: string;
  created_at?: string;
}

export interface WebserverConfigActionHistory {
  id: string;
  tenant_id: string;
  node_id: string;
  webserver_instance_id?: string;
  job_id?: string;
  action: string;
  status: string;
  policy?: Record<string, unknown>;
  result?: Record<string, unknown>;
  error_message?: string;
  created_at: string;
  updated_at: string;
}

export interface WebserverConfigReceipt {
  id: string;
  tenant_id: string;
  node_id: string;
  webserver_instance_id?: string;
  action_id?: string;
  action: string;
  checksum_before?: string;
  checksum_after?: string;
  validation_status: string;
  reload_status: string;
  rollback_ref?: string;
  diff?: string;
  metadata?: Record<string, unknown>;
  created_at: string;
}

export interface WebserverConfigActionPayload {
  tenant_id: string;
  node_id: string;
  policy?: Record<string, unknown>;
}

export interface OfflineContentBundle {
  id?: string;
  tenant_id?: string;
  bundle_id: string;
  version: string;
  sequence: number;
  status: string;
  public_key_fingerprint: string;
  manifest_sha256: string;
  storage_path: string;
  contents: Array<Record<string, unknown>>;
  warnings: string[];
  error?: string;
  imported_at: string;
  issued_at?: string;
  expires_at?: string;
}

export interface NodeFirewallRule {
  ID: string;
  EntityActionID: string;
  NodeID: string;
  TenantID: string;
  Action: string;
  Direction: string;
  Protocol?: string;
  Port?: number;
  Source?: string;
  Dest?: string;
  Tag: string;
  Status: 'pending' | 'applied' | 'failed' | 'removed';
  Error?: string;
  JobID?: string;
  RequestedAt: string;
  AppliedAt?: string;
  RemovedAt?: string;
}

// ── Patch Management (PR 4) ────────────────────────────────────────────────

export interface PatchDeployment {
  ID: string;
  TenantID: string;
  Mode: 'direct' | 'proxy' | 'airgapped';
  Status: 'pending' | 'in_progress' | 'completed' | 'partial' | 'failed';
  TargetNodeCount: number;
  RequestedBy?: string;
  RequestedAt: string;
  StartedAt?: string;
  FinishedAt?: string;
  Summary?: Record<string, unknown>;
  nodes_pending?: number;
  nodes_applied?: number;
  nodes_failed?: number;
}

export interface NodePatchState {
  ID: string;
  DeploymentID: string;
  NodeID: string;
  TenantID: string;
  Status: 'pending' | 'applied' | 'failed';
  PackagesUpgraded?: number;
  LogTail?: string;
  Error?: string;
  JobID?: string;
  RequestedAt: string;
  AppliedAt?: string;
}

// PatchDeployResponse mirrors patchDeployResponse on the server. The
// awaiting_approval list (PR #65) carries one entry per node parked behind
// the approval gate when the tenant has patch_requires_approval=true.
export interface PatchDeployResponse {
  deployment: PatchDeployment;
  node_count: number;
  succeeded?: string[];
  failed?: { node_id: string; error: string }[];
  gate_blocked?: { node_id: string; reason: string }[];
  awaiting_approval?: { node_id: string; approval_id?: string; reason?: string }[];
}

// PatchApproval mirrors patchApprovalResponse on the server.
export interface PatchApproval {
  id: string;
  tenant_id: string;
  deployment_id: string;
  node_id: string;
  mode: string;
  proxy_id?: string;
  window_id?: string;
  status: 'pending' | 'approved' | 'denied' | 'expired';
  approved_by?: string;
  approved_at?: string;
  created_at: string;
  expires_at: string;
  job_id?: string;
}

// ── Patch Management — Wave C (proxy / airgapped / Squid / windows) ──────

export interface NodePatchConfig {
  NodeID: string;
  Mode: 'direct' | 'proxy' | 'airgapped';
  ProxyID?: string;
  WindowID?: string;
  UpdatedAt: string;
}

export interface MaintenanceWindow {
  ID: string;
  TenantID: string;
  Name: string;
  NodeIDs: string[];
  OpensAt: string;
  ClosesAt: string;
  AllowRepos: string[];
  Status: 'scheduled' | 'open' | 'closing' | 'closed' | 'aborted';
  OpenedBy?: string;
  ForceClosedAt?: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface SquidProxy {
  ID: string;
  TenantID: string;
  Host: string;
  Port: number;
  Status: 'installing' | 'healthy' | 'degraded' | 'removing' | 'removed';
  Whitelist: string[];
  LastValidatedAt?: string;
  LastError?: string;
  CreatedAt: string;
  UpdatedAt: string;
}

export interface ConnectionRow {
  conn_id: string;
  node_id?: string;
  correlation_id?: string;
  bastion_session_id?: string;
  started_at: string;
  ended_at?: string;
  duration_ms?: number;
  direction?: string;
  pid?: number;
  process_name?: string;
  cmdline?: string;
  user_name?: string;
  src_ip?: string;
  src_port?: number;
  dst_ip?: string;
  dst_port?: number;
  protocol?: string;
  bytes_in?: number;
  bytes_out?: number;
  packets_in?: number;
  packets_out?: number;
  threat_match?: boolean;
  threat_feed?: string;
  threat_score?: number;
  closed_reason?: string;
}

export interface ForensicEvent {
  ts: string;
  source: 'event' | 'file' | 'db' | 'log' | 'alert' | 'process';
  event_type: string;
  pid?: number;
  process_name?: string;
  user_name?: string;
  path?: string;
  op?: string;
  bytes?: number;
  query_text?: string;
  rows_affected?: number;
  exec_time_ms?: number;
  message?: string;
  severity?: string;
}

export interface ConnectionDetail {
  connection: ConnectionRow;
  events: ForensicEvent[];
}

export interface TopTalker {
  ip: string;
  bytes_out: number;
  bytes_in: number;
  conn_count: number;
  threat_match: boolean;
}

export interface NodeHealthSummary {
  node_id: string;
  hostname?: string;
  cluster_id?: string;
  cpu_p95?: number;
  mem_p95?: number;
  conn_count?: number;
  threat_hits?: number;
  alerts_open?: number;
  state?: 'healthy' | 'warning' | 'degraded' | 'critical' | 'unknown';
}

export interface FleetHealthSnapshot {
  source: 'doris' | 'postgres-fallback';
  totals: {
    nodes: number;
    healthy: number;
    warning: number;
    degraded: number;
    critical: number;
    unknown: number;
  };
  nodes: NodeHealthSummary[];
}

export interface TenantEventFilters {
  tenant_id: string;
  capture_external: boolean;
  capture_internal_summary: boolean;
  capture_listening_changes: boolean;
  capture_files: boolean;
  capture_db_queries: boolean;
  threat_match_full: boolean;
  file_paths_watch: string[];
  file_size_min_bytes: number;
  allowlist_cidrs: string[];
  denylist_cidrs: string[];
  trusted_proxy_cidrs: string[];
  db_query_text_capture: boolean;
  forensic_mode: boolean;
}

function normalizePagination(meta: ServerPaginationMeta): PaginationMeta {
  return {
    total: meta.total,
    count: meta.count,
    limit: meta.limit,
    offset: meta.offset,
    nextOffset: meta.next_offset ?? null,
    prevOffset: meta.prev_offset ?? null,
  };
}

// ---- Phase 9 + 10 types ----------------------------------------------

export interface LoginResponse {
  token: string;
  expires_at: string;
  user_id: string;
  email: string;
  display_name?: string;
  roles: string[];
  permissions: string[];
}

export interface CurrentUser {
  user_id?: string;
  subject?: string;
  email?: string;
  display_name?: string;
  type?: string;
  auth_provider?: string;
  roles?: string[];
  groups?: string[];
  permissions?: string[];
}

export interface Permission {
  name: string;
  description: string;
  category: string;
}

export interface RoleWithPermissions {
  id: string;
  name: string;
  description: string;
  permissions: string[];
}

export type WidgetType = 'db_query' | 'sys_resources' | 'log_size' | 'network_bytes';

export interface DashboardWidget {
  id: string;
  dashboard_id: string;
  title: string;
  widget_type: WidgetType;
  spec: Record<string, unknown>;
  node_ids: string[];
  refresh_seconds: number;
  sort_order: number;
}

export interface CustomDashboard {
  id: string;
  tenant_id: string;
  owner_id: string;
  name: string;
  description: string;
  layout: Record<string, unknown>;
  shared: boolean;
  created_at: string;
  updated_at: string;
  widgets?: DashboardWidget[];
}

export interface WidgetPayload {
  title: string;
  widget_type: WidgetType;
  spec: Record<string, unknown>;
  node_ids: string[];
  refresh_seconds: number;
  sort_order: number;
}

// ── Sprint 1 types ────────────────────────────────────────────────────────

export interface AgentUpdateResponse {
  node_id: string;
  job_id: string;
  message: string;
}

export interface BehavioralBaseline {
  id: string;
  tenant_id: string;
  node_id?: string;
  metric: string;
  window: string;
  mean: number;
  stddev: number;
  sample_count: number;
  created_at: string;
  updated_at: string;
}

export interface BehavioralAnomaly {
  id: string;
  tenant_id: string;
  baseline_id: string;
  node_id?: string;
  source_ip?: string;
  country_code?: string;
  asn?: string;
  metric: string;
  severity?: string;
  status?: string;
  reason?: string;
  observed_value: number;
  z_score: number;
  evidence?: Record<string, unknown>;
  resolved: boolean;
  resolved_at?: string;
  created_at: string;
  last_seen_at?: string;
}

export interface CorrelationRule {
  id: string;
  tenant_id: string;
  name: string;
  description?: string;
  enabled: boolean;
  conditions: Record<string, unknown>;
  severity: string;
  created_at: string;
  updated_at: string;
}

export interface CreateCorrelationRulePayload {
  tenant_id: string;
  name: string;
  description?: string;
  enabled?: boolean;
  conditions: Record<string, unknown>;
  severity: string;
}

export interface CommandACL {
  id: string;
  tenant_id: string;
  name: string;
  pattern: string;
  action: 'allow' | 'deny';
  roles: string[];
  created_at: string;
  updated_at: string;
}

export interface CreateCommandACLPayload {
  tenant_id: string;
  name: string;
  pattern: string;
  action: 'allow' | 'deny';
  roles: string[];
}

export interface ClusterRolloutWave {
  id: string;
  tenant_id: string;
  name: string;
  order: number;
  status: 'pending' | 'running' | 'paused' | 'done' | 'aborted';
  node_count: number;
  done_count: number;
  started_at?: string;
  finished_at?: string;
  created_at: string;
  updated_at: string;
}

export interface UpdateClusterRolloutWavePayload {
  status?: 'paused' | 'running' | 'aborted';
}

export interface MFAFactor {
  id: string;
  type: 'totp' | 'webauthn' | 'recovery';
  name: string;
  created_at: string;
  last_used_at?: string;
}

export interface TenantRemediationConfig {
  TenantID: string;
  MinApprovalSeverity: string;
  ChangeWindows: { days: number[]; start_hour: number; end_hour: number; timezone?: string; label?: string }[];
  CriticalOverride: boolean;
  CircuitBreakerWindowMin: number;
  CircuitBreakerFailPct: number;
  CircuitBreakerMinSamples: number;
  UpdatedAt?: string;
}

export interface WorkerPoolStatus {
  workers: number;
  queue_depth: number;
  active: number;
  idle: number;
  jobs_processed_total: number;
  jobs_failed_total: number;
}

// ---- DLP / Data Classification types (Sprint 2) ---------------------------

export interface DataClassificationRule {
  id: string;
  tenant_id: string;
  name: string;
  pii_type: string;
  regex: string;
  severity: string;
  enabled: boolean;
  created_at: string;
}

export interface ColumnClassification {
  id: string;
  tenant_id: string;
  node_id: string;
  database_name: string;
  schema_name: string;
  table_name: string;
  column_name: string;
  pii_type?: string;
  encrypted?: boolean;
  encryption_kind?: string;
  min_value_length?: number;
  max_value_length?: number;
  sample_count?: number;
  last_scanned_at?: string;
}

export interface PIIFinding {
  id: string;
  tenant_id: string;
  severity: string;
  details?: string;
  resolved_at?: string;
  resolved_by?: string;
  created_at: string;
}

export interface CreateDLPRulePayload {
  tenant_id: string;
  name: string;
  pii_type: string;
  regex: string;
  severity: string;
}

// ---- Sprint 3: Compliance Evidence + Audit Reports + Frameworks -----------

export interface ComplianceEvidence {
  id: string;
  tenant_id: string;
  evidence_type: string;
  framework?: string;
  control_ref?: string;
  title: string;
  description?: string;
  file_path?: string;
  file_size_bytes?: number;
  mime_type?: string;
  checksum?: string;
  uploaded_by: string;
  uploaded_at: string;
  expires_at?: string;
}

export interface AuditReport {
  id: string;
  tenant_id: string;
  framework: string;
  period_start: string;
  period_end: string;
  status: string;
  pdf_path?: string;
  generated_by?: string;
  generated_at?: string;
}

export interface FrameworkControl {
  framework: string;
  control_id: string;
  title: string;
  description: string;
  applicability?: string;
}

export interface ControlCoverage {
  framework: string;
  control_id: string;
  title: string;
  applicability?: string;
  status: 'PASS' | 'PARTIAL' | 'FAIL' | 'NO_COVERAGE';
  nodes_checked: number;
  nodes_passing: number;
  nodes_failing: number;
  evidence_count: number;
  last_checked_at?: string;
}

export interface ControlPostureResponse {
  framework: string;
  tenant_id: string;
  period_start: string;
  period_end: string;
  generated_at: string;
  coverage: ControlCoverage[];
}

export interface CreateAuditReportPayload {
  tenant_id: string;
  framework: string;
  period_start: string;
  period_end: string;
}

export interface ComplianceReview {
  id: string;
  tenant_id: string;
  review_type: string;
  scheduled_for?: string;
  completed_at?: string;
  reviewed_by?: string;
  status: 'pending' | 'completed' | 'overdue';
  notes?: string;
  recurrence?: string;
  created_at: string;
}

export interface CreateComplianceReviewPayload {
  tenant_id: string;
  review_type: string;
  scheduled_for?: string;
  recurrence?: string;
  notes?: string;
}

// ── Finacle integration (UC6) ────────────────────────────────────────────────
export type FinacleAuthMethod = 'oauth2_client_credentials' | 'basic';

export type FinacleShiftModel = '3_shift' | '2_shift' | 'branch_hours' | 'always_on';

export interface FinacleConnection {
  id: string;
  tenant_id: string;
  host: string;
  auth_method: FinacleAuthMethod;
  credential_ref?: string;
  last_sync_at?: string;
  last_error?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateFinacleConnectionPayload {
  tenant_id: string;
  host: string;
  auth_method: FinacleAuthMethod;
  credential_ref?: string;
}

export interface UpdateFinacleConnectionPayload {
  host?: string;
  auth_method?: FinacleAuthMethod;
  credential_ref?: string;
}

export interface FinacleConnectionTestResult {
  status: 'ok' | 'failed';
  message?: string;
  connection: FinacleConnection;
}

export interface FinacleShiftBand {
  name: string;
  start: string; // "HH:MM"
  end: string;   // "HH:MM"
}

export interface FinacleShiftConfig {
  id: string;
  tenant_id: string;
  branch_id?: string;
  model: FinacleShiftModel;
  shifts: FinacleShiftBand[];
  grace_minutes: number;
  created_at: string;
  updated_at: string;
}

export interface CreateFinacleShiftConfigPayload {
  tenant_id: string;
  branch_id?: string;
  model: FinacleShiftModel;
  shifts: FinacleShiftBand[];
  grace_minutes: number;
}

export interface UpdateFinacleShiftConfigPayload {
  branch_id?: string;
  model?: FinacleShiftModel;
  shifts?: FinacleShiftBand[];
  grace_minutes?: number;
}

export interface FinacleProfile {
  id: string;
  tenant_id: string;
  finacle_uid: string;
  branch_id?: string;
  role?: string;
  shift_id?: string;
  status: string;
  last_rotated_at?: string;
}

export interface UpdateFinacleProfilePayload {
  branch_id?: string;
  role?: string;
  shift_id?: string;
  status?: string;
}

export interface FinacleShiftRotatePayload {
  tenant_id: string;
  shift_id: string;
  direction: 'enable' | 'disable';
}

export interface FinacleShiftRotateResponse {
  approval_path_job_id?: string;
  rotate_job_id?: string;
}
