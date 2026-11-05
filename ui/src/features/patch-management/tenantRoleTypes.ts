export interface TenantRoleAssignment {
 id:string;user_id:string;name:string;role:string;global:boolean;expires_at?:string;
}
export interface TenantRoleChange {
 expected_revision:number;user_id:string;role:string;action:'grant'|'revoke';expires_at:string;reason:string;
}
export interface TenantRoleResponse {
 controls:{revision:number;assignments:TenantRoleAssignment[];history:{revision:number;actor_id:string;user_id:string;action:string;role:string;expires_at:string;reason:string;created_at:string}[]};
 can_manage:boolean;current_user_id:string;people:{id:string;name:string}[];people_total:number;people_limit:number;
 allowed_roles:string[];permissions:Record<string,boolean>;history_limit:number;has_global_assignment:boolean;scope:string;
}
