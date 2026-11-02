export interface PatchReviewOperationsPolicy {
    owner: string;
    reason: string;
    retention: {
        mode: 'retain' | 'archive_then_purge';
        days: number;
        legal_hold: boolean;
    };
    notifications: {
        enabled: boolean;
        channel: 'in_app';
        recipients: string[];
        repeat_hours: number;
    };
    automation: {
        mode: 'recommend_only' | 'approved_scheduled' | 'automatic';
        enabled: boolean;
        node_ids: string[];
        canary_node_ids: string[];
        packages: string[];
        exclusions: string[];
        window_id: string;
        transport: string;
        max_wave_size: number;
		observation_minutes?: number;
        health_profile: string;
        valid_until?: string;
        require_approval: boolean;
        require_exact_plan: boolean;
        require_fresh_assessment: boolean;
        require_health: boolean;
        require_reboot_verification: boolean;
        stop_on_failure: boolean;
    };
}

export interface PatchAutomationRun {
 id:string;tenant_id:string;approval_id:string;policy_revision:number;actor_id:string;
 authority:'individual_approval'|'tenant_policy';policy_hash:string;not_before:string;expires_at:string;
 status:'waiting'|'blocked'|'dispatched'|'cancelled'|'expired'|'failed';reason:string;
 execution_job_id?:string|null;created_at:string;checked_at?:string;
}
export interface PatchOperationsResponse {
    operations: {
        revision: number;
        policy: PatchReviewOperationsPolicy;
        updated_by: string;
        updated_at: string;
    };
    recipients: {
        id: string;
        name: string;
    }[];
    delivery: {
        checked_at?: string;
        delivered: number;
        last_error: string;
    };
    can_manage: boolean;
    current_user_id: string;
    effective_installation_mode: string;
    installation_activation_available: boolean;
    notification_channel: string;
    external_delivery_available: boolean;
}
export interface PatchRetentionPreview {
    revision: number;
    cutoff: string;
    count: number;
    sha256: string;
    blocked?: string;
}
