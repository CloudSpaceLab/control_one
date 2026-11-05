import type {PatchReviewOperationsPolicy} from './operationsTypes';
import type {PatchReviewPolicy} from './reviewTypes';
export interface OrganizationPolicy {
  name: string;
  owner_id: string;
  approver_id: string;
  responsibilities: string;
  scope: string;
  customer_authorization: string;
  exclusions: string;
  reason: string;
  effective_at: string;
  expires_at: string;
}
export interface OrganizationPolicyRecord {
  revision: number;
  status: 'draft' | 'review' | 'approved' | 'withdrawn';
  policy: OrganizationPolicy;
  urgency_revision: number;
  operations_revision: number;
  authored_by: string;
  changed_by: string;
  approved_by?: string;
  approved_at?: string;
  updated_at: string;
  bound_controls?: {urgency?:PatchReviewPolicy;operations?:PatchReviewOperationsPolicy};
}
export interface OrganizationPolicyResponse {
  record: OrganizationPolicyRecord | null;
  history: OrganizationPolicyRecord[];
  history_limit: number;
  administrators: {id: string; name: string}[];
  approvers?: {id:string;name:string}[];
  can_manage: boolean;
  can_approve?: boolean;
  current_user_id: string;
  review_status: string;
  installation_authorized: false;
  execution_integration: string;
}
