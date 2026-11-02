export interface PatchReviewRule {
    name: string;
    minimum_severity: string;
    require_kev: boolean;
    exposure: string;
    importance: string;
    priority: string;
    due_hours: number;
}
export interface PatchReviewPolicy {
    name: string;
    owner: string;
    mode: 'recommend_only';
    rules: PatchReviewRule[];
    retention_days: number;
}
export interface PatchReviewPolicyRecord {
    revision: number;
    policy: PatchReviewPolicy;
    updated_by: string;
    updated_at: string;
}
export interface PatchPriorityInput {
    severity: string;
    kev: boolean;
    exposure: string;
    importance: string;
    compatibility: string;
    evidence_current: boolean;
    first_seen_at: string;
}
export interface PatchPriorityDecision {
    priority: string;
    rule?: string;
    reasons: string[];
    due_at?: string;
    overdue: boolean;
    installation_authorized: false;
}
export interface PatchFindingTracking {
    context_expires_at?: string;
    owner: string;
    escalate_to: string;
    exposure: string;
    importance: string;
    compatibility: string;
    due_at?: string;
    exception_until?: string;
    reason: string;
}
export interface PatchReviewFinding {
    finding: {
        id: string;
        node_id: string;
        package_name: string;
        installed_version: string;
        cve_id: string;
        severity: string;
        kev: boolean;
        first_seen_at: string;
        last_seen_at: string;
    };
    tracking?: {
        revision: number;
        tracking: PatchFindingTracking;
    };
    decision: PatchPriorityDecision;
    exception_status: string;
    escalation_required: boolean;
    evidence_current: boolean;
}
export interface PatchReviewResponse {
    pagination?:{limit:number;cursor?:string;next_cursor?:string;history_cursor:number;next_history_cursor:number};
    permissions: {manage_policy:boolean;review_findings:boolean};
    tenant_id: string;
    generated_at: string;
    policy: PatchReviewPolicyRecord | null;
    findings: PatchReviewFinding[];
    total_findings: number;
    findings_truncated: boolean;
    events: {
        id: number;
        kind: string;
        actor: string;
        revision: number;
        created_at: string;
        document: unknown;
    }[];
    history_truncated: boolean;
    history_window_days: number;
    retention: string;
    automation: {
        effective_mode: string;
        installation_authorized: false;
        stop_conditions: string[];
    };
    approval_history?: unknown[];
    approval_history_truncated?: boolean;
}
