package server

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/auth"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/behavioral"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/config"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/connect"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/correlation"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/doris"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/eventbus"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/ipintel"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/mfa"

	"github.com/CloudSpaceLab/control_one/controlplane/internal/secretbox"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/storage"
	"github.com/CloudSpaceLab/control_one/controlplane/internal/worker"
	"github.com/CloudSpaceLab/control_one/internal/compliance"
	"github.com/CloudSpaceLab/control_one/internal/provisioning"
	webauthnlib "github.com/go-webauthn/webauthn/webauthn"
)

// Store defines persistence operations used by the server.
type Store interface {
	CreateTenant(context.Context, *storage.Tenant) (*storage.Tenant, error)
	ListTenants(context.Context, string, int, int) ([]storage.Tenant, int, error)
	GetTenant(context.Context, uuid.UUID) (*storage.Tenant, error)
	UpdateTenant(context.Context, uuid.UUID, string) (*storage.Tenant, error)
	DeleteTenant(context.Context, uuid.UUID) error
	EnsureTenant(context.Context, uuid.UUID, string) (*storage.Tenant, error)
	GetNodeByHostname(context.Context, uuid.UUID, string) (*storage.Node, error)
	GetNodeByMachineID(context.Context, uuid.UUID, string) (*storage.Node, error)
	CreateNode(context.Context, *storage.Node) (*storage.Node, error)
	UpdateNode(context.Context, *storage.Node) (*storage.Node, error)
	DeleteNode(context.Context, uuid.UUID) error
	RetireNode(context.Context, uuid.UUID) error
	ListNodes(context.Context, uuid.UUID, string, int, int) ([]storage.Node, int, error)
	FindNodesByPublicIP(context.Context, string) ([]storage.Node, error)
	GetNode(context.Context, uuid.UUID) (*storage.Node, error)
	SetNodeState(context.Context, uuid.UUID, string) error
	ResetNodeForReenrollment(context.Context, uuid.UUID) error
	SetNodeAuthToken(context.Context, uuid.UUID, string) error
	ValidateNodeToken(context.Context, string) (*storage.Node, error)
	TouchNodeHeartbeat(context.Context, uuid.UUID) (*storage.Node, error)
	MarkNodeFirstScan(context.Context, uuid.UUID) (*storage.Node, error)
	UpdateNodeLabels(context.Context, uuid.UUID, map[string]any) error
	ListEnrollmentPendingNodesOlderThan(context.Context, time.Time) ([]storage.Node, error)
	UpdateNodeAgentVersion(context.Context, uuid.UUID, string) error
	GetPendingAgentUpdateJob(context.Context, uuid.UUID) (*storage.Job, error)
	GetUserByExternalID(context.Context, string) (*storage.User, error)
	GetUser(context.Context, uuid.UUID) (*storage.User, error)
	ListUsers(context.Context, int, int) ([]storage.User, int, error)
	SetUserRoles(context.Context, uuid.UUID, []string) error
	ListUserRoles(context.Context, uuid.UUID) ([]string, error)
	ListRoles(context.Context) ([]storage.Role, error)
	ListJobs(context.Context, uuid.UUID, string, storage.JobStatus, int, int) ([]storage.Job, int, error)
	CreateJob(context.Context, *storage.Job, *storage.JobEvent) (*storage.Job, error)
	UpdateJobStatus(context.Context, uuid.UUID, storage.JobStatus, string, map[string]any) error
	GetJob(context.Context, uuid.UUID) (*storage.Job, error)
	ListJobEvents(context.Context, uuid.UUID) ([]storage.JobEvent, error)
	CreateComplianceResults(context.Context, []storage.ComplianceResult) error
	ListComplianceResults(context.Context, uuid.UUID) ([]storage.ComplianceResult, error)
	ListComplianceResultsFiltered(context.Context, storage.ComplianceResultFilter, int, int) ([]storage.ComplianceResult, int, error)
	ListProvisioningTemplates(context.Context, storage.ProvisioningTemplateFilter, int, int) ([]storage.ProvisioningTemplate, int, error)
	CreateProvisioningTemplate(context.Context, *storage.ProvisioningTemplate) (*storage.ProvisioningTemplate, error)
	GetProvisioningTemplate(context.Context, uuid.UUID) (*storage.ProvisioningTemplate, error)
	UpdateProvisioningTemplate(context.Context, uuid.UUID, storage.UpdateProvisioningTemplateParams) (*storage.ProvisioningTemplate, error)
	CreateProvisioningTemplateVersion(context.Context, storage.CreateTemplateVersionParams) (*storage.ProvisioningTemplateVersion, error)
	PromoteProvisioningTemplateVersion(context.Context, uuid.UUID, int) (*storage.ProvisioningTemplateVersion, error)
	GetProvisioningTemplateVersion(context.Context, uuid.UUID, int) (*storage.ProvisioningTemplateVersion, error)
	GetPromotedProvisioningTemplateVersion(context.Context, uuid.UUID) (*storage.ProvisioningTemplateVersion, error)
	ListProvisioningTemplateVersions(context.Context, uuid.UUID, int, int) ([]storage.ProvisioningTemplateVersion, int, error)
	CreateAuditLog(context.Context, *storage.AuditLog) (*storage.AuditLog, error)
	ListAuditLogs(context.Context, storage.AuditLogFilter, int, int) ([]storage.AuditLog, int, error)
	ListPolicies(context.Context, storage.PolicyFilter, int, int) ([]storage.Policy, int, error)
	GetPolicy(context.Context, uuid.UUID) (*storage.Policy, error)
	CreatePolicy(context.Context, storage.CreatePolicyParams) (*storage.Policy, error)
	UpdatePolicy(context.Context, uuid.UUID, storage.UpdatePolicyParams) (*storage.Policy, error)
	DeletePolicy(context.Context, uuid.UUID) error
	ListPolicyVersions(context.Context, uuid.UUID, int, int) ([]storage.PolicyVersion, int, error)
	GetPolicyVersion(context.Context, uuid.UUID, int) (*storage.PolicyVersion, error)
	GetPromotedPolicyVersion(context.Context, uuid.UUID) (*storage.PolicyVersion, error)
	CreatePolicyVersion(context.Context, storage.CreatePolicyVersionParams) (*storage.PolicyVersion, error)
	PromotePolicyVersion(context.Context, uuid.UUID, int) (*storage.PolicyVersion, error)
	ListRollouts(context.Context, uuid.UUID, int, int) ([]storage.TemplateRollout, int, error)
	GetRollout(context.Context, uuid.UUID) (*storage.TemplateRollout, error)
	CreateRollout(context.Context, storage.CreateRolloutParams) (*storage.TemplateRollout, error)
	UpdateRollout(context.Context, uuid.UUID, storage.UpdateRolloutParams) (*storage.TemplateRollout, error)
	ListTelemetryMetrics(context.Context, storage.TelemetryMetricFilter, int, int) ([]storage.TelemetryMetric, int, error)
	ListTelemetryLogs(context.Context, storage.TelemetryLogFilter, int, int) ([]storage.TelemetryLog, int, error)
	CreateTelemetryMetrics(context.Context, []storage.CreateTelemetryMetricParams) error
	CreateTelemetryLogs(context.Context, []storage.CreateTelemetryLogParams) error
	GetComplianceAggregation(context.Context, storage.ComplianceResultFilter) (*storage.ComplianceAggregation, error)
	GetComplianceTrends(context.Context, storage.ComplianceResultFilter, int) ([]storage.ComplianceTrend, error)
	GetRemediationScript(context.Context, string, string) (*storage.RemediationScript, error)
	GetRemediationScriptByID(context.Context, uuid.UUID) (*storage.RemediationScript, error)
	ListRemediationScripts(context.Context, string, string, int, int) ([]storage.RemediationScript, int, error)
	CreateRemediationScript(context.Context, storage.CreateRemediationScriptParams) (*storage.RemediationScript, error)
	UpdateRemediationScript(context.Context, uuid.UUID, storage.UpdateRemediationScriptParams) (*storage.RemediationScript, error)
	ListWebhooks(context.Context, uuid.UUID, *bool, int, int) ([]storage.Webhook, int, error)
	ListWebhooksByEvent(context.Context, uuid.UUID, string) ([]storage.Webhook, error)
	GetEnabledWebhooksForEvent(context.Context, string) ([]storage.Webhook, error)
	CreateWebhook(context.Context, storage.CreateWebhookParams) (*storage.Webhook, error)
	GetWebhook(context.Context, uuid.UUID) (*storage.Webhook, error)
	UpdateWebhook(context.Context, uuid.UUID, storage.UpdateWebhookParams) (*storage.Webhook, error)
	DeleteWebhook(context.Context, uuid.UUID) error
	ListWebhookDeliveries(context.Context, uuid.UUID, *string, int, int) ([]storage.WebhookDelivery, int, error)
	RecordWebhookDelivery(context.Context, storage.WebhookDelivery) error
	GetRetentionPolicy(context.Context, uuid.UUID, string) (*storage.TelemetryRetentionPolicy, error)
	ListRetentionPolicies(context.Context, uuid.UUID, int, int) ([]storage.TelemetryRetentionPolicy, int, error)
	CreateRetentionPolicy(context.Context, storage.CreateRetentionPolicyParams) (*storage.TelemetryRetentionPolicy, error)
	DeleteExpiredTelemetry(context.Context, uuid.UUID, string) (int64, error)
	ListSecretGroups(context.Context, uuid.UUID, int, int) ([]storage.SecretGroup, int, error)
	GetSecretGroup(context.Context, uuid.UUID) (*storage.SecretGroup, error)
	CreateSecretGroup(context.Context, storage.CreateSecretGroupParams) (*storage.SecretGroup, error)
	UpdateSecretGroupSyncStatus(context.Context, uuid.UUID, string, error) error
	ListSecretSyncs(context.Context, uuid.UUID, int, int) ([]storage.SecretSync, int, error)
	ListEntitlements(context.Context, storage.EntitlementFilter, int, int) ([]storage.AccessEntitlement, int, error)
	GetEntitlement(context.Context, uuid.UUID) (*storage.AccessEntitlement, error)
	CreateEntitlement(context.Context, storage.CreateEntitlementParams) (*storage.AccessEntitlement, error)
	UpdateEntitlement(context.Context, uuid.UUID, storage.UpdateEntitlementParams) (*storage.AccessEntitlement, error)
	DeleteEntitlement(context.Context, uuid.UUID) error
	RecordAccessSync(context.Context, uuid.UUID, uuid.UUID, string, string, string, int, int, int, error) error
	CreateSessionRecording(context.Context, storage.CreateSessionRecordingParams) (*storage.SessionRecording, error)
	GetSessionRecording(context.Context, uuid.UUID) (*storage.SessionRecording, error)
	ListSessionRecordings(context.Context, storage.ListSessionRecordingsParams, int, int) ([]storage.SessionRecording, int, error)
	UpdateSessionRecording(context.Context, uuid.UUID, storage.UpdateSessionRecordingParams) (*storage.SessionRecording, error)
	CreateSessionEvent(context.Context, uuid.UUID, string, time.Time, map[string]any) (*storage.SessionEvent, error)
	ListSessionEvents(context.Context, uuid.UUID, int, int) ([]storage.SessionEvent, int, error)
	CreateEnrollmentToken(context.Context, storage.CreateEnrollmentTokenParams) (*storage.EnrollmentToken, error)
	GetEnrollmentTokenByHash(context.Context, string) (*storage.EnrollmentToken, error)
	ListEnrollmentTokens(context.Context, uuid.UUID, int, int) ([]storage.EnrollmentToken, int, error)
	RevokeEnrollmentToken(context.Context, uuid.UUID) error
	IncrementEnrollmentCount(context.Context, uuid.UUID) error
	CreateFleetEnrollmentResult(context.Context, *storage.FleetEnrollmentResult) error
	ListFleetEnrollmentResults(context.Context, uuid.UUID) ([]storage.FleetEnrollmentResult, error)
	CreatePolicyAssignment(context.Context, storage.CreatePolicyAssignmentParams) (*storage.PolicyAssignment, error)
	ListPolicyAssignments(context.Context, uuid.UUID, int, int) ([]storage.PolicyAssignment, int, error)
	DeletePolicyAssignment(context.Context, uuid.UUID) error
	GetEffectivePolicies(context.Context, uuid.UUID, uuid.UUID) ([]storage.PolicyWithVersion, error)
	GetLatestComplianceResultForRule(context.Context, uuid.UUID, string) (*storage.ComplianceResult, error)
	UpdateComplianceResultVerification(context.Context, uuid.UUID, bool, *uuid.UUID) error
	UpdateComplianceResultRollback(context.Context, uuid.UUID, uuid.UUID) error
	AcquireRemediationLease(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, time.Duration) (*storage.RemediationLease, error)
	ReleaseRemediationLease(context.Context, uuid.UUID) error
	CountTenantLeases(context.Context, uuid.UUID) (int, error)
	GetTenantRemediationConfig(context.Context, uuid.UUID) (*storage.TenantRemediationConfig, error)
	UpsertTenantRemediationConfig(context.Context, storage.TenantRemediationConfig) (*storage.TenantRemediationConfig, error)
	CreateRemediationApproval(context.Context, storage.CreateRemediationApprovalParams) (*storage.RemediationApproval, error)
	GetRemediationApproval(context.Context, uuid.UUID) (*storage.RemediationApproval, error)
	ListRemediationApprovals(context.Context, storage.ListRemediationApprovalsFilter, int, int) ([]storage.RemediationApproval, int, error)
	ResolveRemediationApproval(context.Context, uuid.UUID, storage.ApprovalStatus, uuid.UUID) (*storage.RemediationApproval, error)
	ExpireRemediationApprovals(context.Context, time.Time) (int, error)
	GetCircuitBreakerState(context.Context, uuid.UUID, string) (*storage.RemediationCircuitBreakerState, error)
	TripCircuitBreaker(context.Context, uuid.UUID, string, string) (*storage.RemediationCircuitBreakerState, error)
	AckCircuitBreaker(context.Context, uuid.UUID, string, uuid.UUID) (*storage.RemediationCircuitBreakerState, error)
	RemediationFailRate(context.Context, uuid.UUID, string, time.Duration) (*storage.RemediationFailRate, error)
	CreateCluster(context.Context, storage.CreateClusterParams) (*storage.Cluster, error)
	ListClusters(context.Context, uuid.UUID, int, int) ([]storage.Cluster, int, error)
	GetClusterByID(context.Context, uuid.UUID) (*storage.Cluster, error)
	GetClusterByName(context.Context, uuid.UUID, string) (*storage.Cluster, error)
	UpdateCluster(context.Context, uuid.UUID, storage.UpdateClusterParams) (*storage.Cluster, error)
	DeleteCluster(context.Context, uuid.UUID) error
	CountClustersByTenant(context.Context, uuid.UUID) (int, error)
	AddClusterMember(context.Context, uuid.UUID, uuid.UUID, string, int) (*storage.ClusterMember, error)
	RemoveClusterMember(context.Context, uuid.UUID, uuid.UUID) error
	ListClusterMembers(context.Context, uuid.UUID) ([]storage.ClusterMember, error)
	CreateClusterRollout(context.Context, storage.CreateClusterRolloutParams) (*storage.ClusterRollout, error)
	GetClusterRolloutByID(context.Context, uuid.UUID) (*storage.ClusterRollout, error)
	ListClusterRollouts(context.Context, uuid.UUID, int, int) ([]storage.ClusterRollout, int, error)
	UpdateClusterRollout(context.Context, uuid.UUID, storage.UpdateClusterRolloutParams) (*storage.ClusterRollout, error)
	DeleteClusterRollout(context.Context, uuid.UUID) error
	// Node cert rotation + history (Worktree B).
	RotateNodeCertificate(context.Context, uuid.UUID, string) (*storage.NodeCertHistory, error)
	GetNodeCertHistory(context.Context, uuid.UUID) ([]storage.NodeCertHistory, error)
	LatestNodeCertHistory(context.Context, uuid.UUID) (*storage.NodeCertHistory, error)
	// Cluster LB registration + label propagation (Worktree E).
	CreateClusterLBRegistration(context.Context, storage.CreateClusterLBRegistrationParams) (*storage.ClusterLBRegistration, error)
	MarkClusterLBRegistrationDeregistered(context.Context, uuid.UUID, uuid.UUID, string) error
	ListClusterLBRegistrationsForNode(context.Context, uuid.UUID) ([]storage.ClusterLBRegistration, error)
	ListClusterLBRegistrationsForCluster(context.Context, uuid.UUID) ([]storage.ClusterLBRegistration, error)
	PropagateClusterLabelsToNode(context.Context, uuid.UUID, uuid.UUID) error
	// Cluster rollout waves (Worktree D).
	CreateClusterRolloutWave(context.Context, storage.CreateClusterRolloutWaveParams) (*storage.ClusterRolloutWave, error)
	GetClusterRolloutWave(context.Context, uuid.UUID) (*storage.ClusterRolloutWave, error)
	GetClusterRolloutWaveByNumber(context.Context, uuid.UUID, int) (*storage.ClusterRolloutWave, error)
	ListClusterRolloutWaves(context.Context, uuid.UUID) ([]storage.ClusterRolloutWave, error)
	UpdateClusterRolloutWave(context.Context, uuid.UUID, storage.UpdateClusterRolloutWaveParams) (*storage.ClusterRolloutWave, error)
	// Provider credentials + hypervisor hosts (multi-host provisioning).
	CreateProviderCredential(context.Context, storage.CreateProviderCredentialParams) (*storage.ProviderCredential, error)
	UpdateProviderCredential(context.Context, uuid.UUID, storage.UpdateProviderCredentialParams) (*storage.ProviderCredential, error)
	GetProviderCredential(context.Context, uuid.UUID) (*storage.ProviderCredential, error)
	ListProviderCredentials(context.Context, uuid.UUID, string, int, int) ([]storage.ProviderCredential, int, error)
	DeleteProviderCredential(context.Context, uuid.UUID) error
	CreateHypervisorHost(context.Context, storage.CreateHypervisorHostParams) (*storage.HypervisorHost, error)
	GetHypervisorHost(context.Context, uuid.UUID) (*storage.HypervisorHost, error)
	ListHypervisorHosts(context.Context, uuid.UUID, string, int, int) ([]storage.HypervisorHost, int, error)
	UpdateHypervisorHost(context.Context, uuid.UUID, storage.UpdateHypervisorHostParams) (*storage.HypervisorHost, error)
	RecordHypervisorHostHealth(context.Context, uuid.UUID, string, string) (*storage.HypervisorHost, error)
	DeleteHypervisorHost(context.Context, uuid.UUID) error
	// Port + log monitoring rules.
	CreatePortRule(context.Context, storage.CreatePortRuleParams) (*storage.PortMonitoringRule, error)
	GetPortRule(context.Context, uuid.UUID) (*storage.PortMonitoringRule, error)
	ListPortRules(context.Context, storage.PortRuleFilter, int, int) ([]storage.PortMonitoringRule, int, error)
	UpdatePortRule(context.Context, uuid.UUID, storage.UpdatePortRuleParams) (*storage.PortMonitoringRule, error)
	DeletePortRule(context.Context, uuid.UUID) error
	CreateLogRule(context.Context, storage.CreateLogRuleParams) (*storage.LogMonitoringRule, error)
	GetLogRule(context.Context, uuid.UUID) (*storage.LogMonitoringRule, error)
	ListLogRules(context.Context, storage.LogRuleFilter, int, int) ([]storage.LogMonitoringRule, int, error)
	UpdateLogRule(context.Context, uuid.UUID, storage.UpdateLogRuleParams) (*storage.LogMonitoringRule, error)
	DeleteLogRule(context.Context, uuid.UUID) error
	// Dashboard events.
	CreateSecurityEvent(context.Context, storage.CreateSecurityEventParams) (*storage.SecurityEvent, error)
	ListSecurityEvents(context.Context, storage.SecurityEventFilter, int, int) ([]storage.SecurityEvent, int, error)
	CountSecurityEvents(context.Context, storage.SecurityEventFilter) (storage.SecurityEventCounts, error)
	GetSecurityEventSeries(context.Context, uuid.UUID, time.Time, string) ([]storage.SecurityEventPoint, error)
	CreateHealthIncident(context.Context, storage.CreateHealthIncidentParams) (*storage.HealthIncident, error)
	ResolveHealthIncident(context.Context, uuid.UUID) error
	CountOpenHealthIncidents(context.Context, uuid.UUID) (storage.SecurityEventCounts, error)
	CreateRuleTrigger(context.Context, storage.CreateRuleTriggerParams) (*storage.RuleTrigger, error)
	CountRuleTriggersSince(context.Context, uuid.UUID, time.Time) (map[string]int, error)
	CountRuleTriggersBetween(context.Context, uuid.UUID, time.Time, time.Time) (map[string]int, error)
	CountRemediationsSince(context.Context, uuid.UUID, time.Time, time.Time) (int, error)
	// Alerts.
	CreateAlert(context.Context, storage.CreateAlertParams) (*storage.Alert, error)
	GetAlert(context.Context, uuid.UUID) (*storage.Alert, error)
	ListAlerts(context.Context, storage.AlertFilter, int, int) ([]storage.Alert, int, error)
	AckAlert(context.Context, uuid.UUID, uuid.UUID) error
	ResolveAlert(context.Context, uuid.UUID, uuid.UUID) error
	// Access requests.
	CreateAccessRequest(context.Context, storage.CreateAccessRequestParams) (*storage.AccessRequest, error)
	GetAccessRequest(context.Context, uuid.UUID) (*storage.AccessRequest, error)
	ListAccessRequests(context.Context, storage.AccessRequestFilter, int, int) ([]storage.AccessRequest, int, error)
	DecideAccessRequest(context.Context, uuid.UUID, string, uuid.UUID, string, *time.Time) (*storage.AccessRequest, error)
	// SSH CA + certs.
	CreateSSHCA(context.Context, storage.CreateSSHCAParams) (*storage.SSHCA, error)
	GetActiveSSHCA(context.Context, uuid.UUID) (*storage.SSHCA, error)
	CreateIssuedCert(context.Context, storage.CreateIssuedCertParams) (*storage.IssuedCert, error)
	NextCertSerial(context.Context, uuid.UUID) (int64, error)
	ListIssuedCerts(context.Context, uuid.UUID, int, int) ([]storage.IssuedCert, int, error)
	// Command ACL.
	CreateCommandACL(context.Context, storage.CreateCommandACLParams) (*storage.CommandACL, error)
	GetCommandACL(context.Context, uuid.UUID) (*storage.CommandACL, error)
	ListCommandACLs(context.Context, uuid.UUID, int, int) ([]storage.CommandACL, int, error)
	DeleteCommandACL(context.Context, uuid.UUID) error
	// Correlation + behavioral.
	CreateCorrelationRule(context.Context, storage.CreateCorrelationRuleParams) (*storage.CorrelationRule, error)
	GetCorrelationRule(context.Context, uuid.UUID, uuid.UUID) (*storage.CorrelationRule, error)
	ListCorrelationRules(context.Context, uuid.UUID) ([]storage.CorrelationRule, error)
	DeleteCorrelationRule(context.Context, uuid.UUID, uuid.UUID) error
	UpsertBehavioralBaseline(context.Context, uuid.UUID, *uuid.UUID, string, string, map[string]any, int) error
	ListBehavioralBaselines(context.Context, uuid.UUID, uuid.UUID) ([]storage.BehavioralBaseline, error)
	CreatePortObservation(context.Context, storage.CreatePortObservationParams) error
	AggregatePortObservations(context.Context, uuid.UUID, time.Time) ([]storage.PortObservationStats, error)
	// MFA.
	CreateMFAFactor(context.Context, storage.CreateMFAFactorParams) (*storage.MFAFactor, error)
	GetMFAFactor(context.Context, uuid.UUID) (*storage.MFAFactor, error)
	ListMFAFactors(context.Context, uuid.UUID) ([]storage.MFAFactor, error)
	DisableMFAFactor(context.Context, uuid.UUID) error
	EnableMFAFactor(context.Context, uuid.UUID, string) error
	RecordMFAUse(context.Context, uuid.UUID, int64) error
	CreateStepUpChallenge(context.Context, uuid.UUID, string, string, []byte, time.Duration) (*storage.StepUpChallenge, error)
	ConsumeStepUpChallenge(context.Context, uuid.UUID) (*storage.StepUpChallenge, error)
	// Threat-intel feeds (operator-managed).
	CreateThreatFeed(context.Context, storage.CreateThreatFeedParams) (*storage.ThreatFeed, error)
	GetThreatFeed(context.Context, uuid.UUID) (*storage.ThreatFeed, error)
	ListThreatFeeds(context.Context, storage.ThreatFeedFilter) ([]storage.ThreatFeed, error)
	UpdateThreatFeed(context.Context, uuid.UUID, storage.UpdateThreatFeedParams) (*storage.ThreatFeed, error)
	DeleteThreatFeed(context.Context, uuid.UUID) error
	RecordThreatFeedRefresh(context.Context, uuid.UUID, string, string, int) error
	// Event ingest journal + hourly rollup.
	RecordEventIngest(context.Context, storage.CreateEventIngestBatchParams) (uuid.UUID, error)
	MarkEventIngestStatus(context.Context, uuid.UUID, string, string, string) error
	PendingEventIngestBatches(context.Context, time.Duration, int) ([]storage.EventIngestBatch, error)
	PruneAcceptedEventIngestBatches(context.Context, time.Duration) (int64, error)
	IncrementHourlyRollup(context.Context, uuid.UUID, *uuid.UUID, string, time.Time, int64, int64, int64, string) error
	QueryHourlyRollup(context.Context, uuid.UUID, time.Time, time.Time) ([]storage.HourlyRollupRow, error)
	// Tenant event-filter policy (smart capture knobs + forensic mode).
	GetTenantEventFilters(context.Context, uuid.UUID) (*storage.TenantEventFilters, error)
	UpsertTenantEventFilters(context.Context, storage.TenantEventFilters) error
	// Behavioural anomaly baselines + first-seen registries (Phase F).
	UpsertKnownDestination(context.Context, uuid.UUID, string) (storage.UpsertKnownDestinationResult, error)
	UpsertKnownExeHash(context.Context, uuid.UUID, string, string, int64, *uuid.UUID) (storage.UpsertKnownExeHashResult, error)
	GetConnectionDurationBaseline(context.Context, uuid.UUID, string, int) (*storage.ConnectionDurationBaseline, error)
	GetConnectionBytesBaseline(context.Context, uuid.UUID, string, int) (*storage.ConnectionBytesBaseline, error)
	UpsertKnownQueryHash(context.Context, uuid.UUID, string, string, string, string, string, int64, int64) (storage.UpsertKnownQueryHashResult, error)
	// Local + LDAP auth — bcrypt-hashed passwords, opaque session tokens,
	// and granular RBAC permission catalog. Phase 9.
	CreateLocalUser(context.Context, storage.CreateLocalUserParams) (*storage.LocalUser, error)
	VerifyLocalUserPassword(context.Context, string, string) (*storage.LocalUser, error)
	GetLocalUserByEmail(context.Context, string) (*storage.LocalUser, error)
	SetUserPassword(context.Context, uuid.UUID, string) error
	SetUserDisabled(context.Context, uuid.UUID, bool) error
	MarkLoginSuccess(context.Context, uuid.UUID) error
	IssueSession(context.Context, uuid.UUID, time.Duration, string, string) (*storage.Session, error)
	ValidateSessionToken(context.Context, string) (*storage.Session, *storage.LocalUser, error)
	RevokeSession(context.Context, uuid.UUID) error
	RevokeAllSessionsForUser(context.Context, uuid.UUID) error
	PurgeExpiredSessions(context.Context, time.Duration) (int64, error)
	ListPermissions(context.Context) ([]storage.Permission, error)
	ListRolesWithPermissions(context.Context) ([]storage.RolePermissions, error)
	SetRolePermissions(context.Context, uuid.UUID, []string) error
	CreateCustomRole(context.Context, string, string, []string) (*storage.RolePermissions, error)
	DeleteRoleByID(context.Context, uuid.UUID) error
	GetUserPermissions(context.Context, uuid.UUID) ([]string, error)
	// Custom dashboards (Phase 10).
	CreateDashboard(context.Context, uuid.UUID, uuid.UUID, string, string, bool) (*storage.CustomDashboard, error)
	ListDashboardsForUser(context.Context, uuid.UUID, uuid.UUID) ([]storage.CustomDashboard, error)
	GetDashboard(context.Context, uuid.UUID, uuid.UUID) (*storage.CustomDashboard, error)
	UpdateDashboard(context.Context, uuid.UUID, uuid.UUID, string, string, bool, json.RawMessage) error
	DeleteDashboard(context.Context, uuid.UUID, uuid.UUID) error
	CreateWidget(context.Context, storage.DashboardWidget) (*storage.DashboardWidget, error)
	UpdateWidget(context.Context, storage.DashboardWidget) error
	DeleteWidget(context.Context, uuid.UUID) error
	// Executive Risk Dashboard metrics (Phase 0)
	GetMTTDMetrics(context.Context, uuid.UUID, string, time.Time) (*storage.MTTDMetrics, error)
	GetMTTRMetrics(context.Context, uuid.UUID, string, time.Time) (*storage.MTTRMetrics, error)
	GetRemediationVelocity(context.Context, uuid.UUID, int) (*storage.RemediationVelocity, error)
	GetFindingAging(context.Context, uuid.UUID, string) (*storage.FindingAging, error)
	CalculateRiskScore(context.Context, uuid.UUID) (*storage.RiskScore, error)
	GetRiskScoreHistory(context.Context, uuid.UUID, int) ([]storage.RiskScorePoint, error)
	GetRemediationVelocityHistory(context.Context, uuid.UUID, int) ([]storage.RemediationVelocityPoint, error)
	GetComplianceByFramework(context.Context, uuid.UUID) ([]storage.FrameworkComplianceSummary, error)
	// Data classification / DLP (Sprint 2).
	ListDataClassificationRules(context.Context, uuid.UUID) ([]storage.DataClassificationRule, error)
	CreateDataClassificationRule(context.Context, *storage.DataClassificationRule) (*storage.DataClassificationRule, error)
	DeleteDataClassificationRule(context.Context, uuid.UUID) error
	UpsertColumnClassification(context.Context, *storage.ColumnClassification) (*storage.ColumnClassification, error)
	ListColumnClassifications(context.Context, uuid.UUID, int, int) ([]storage.ColumnClassification, int, error)
	ListPIIFindings(context.Context, uuid.UUID, *bool, int, int) ([]storage.PIIFinding, int, error)
	ResolvePIIFinding(context.Context, uuid.UUID, uuid.UUID) error
	CreatePIIFinding(context.Context, *storage.PIIFinding) (*storage.PIIFinding, error)
	// Compliance evidence + audit reports (Sprint 3).
	CreateComplianceEvidence(context.Context, *storage.ComplianceEvidence) (*storage.ComplianceEvidence, error)
	ListComplianceEvidence(context.Context, uuid.UUID, string, string, int, int) ([]storage.ComplianceEvidence, int, error)
	GetComplianceEvidence(context.Context, uuid.UUID) (*storage.ComplianceEvidence, error)
	DeleteComplianceEvidence(context.Context, uuid.UUID) error
	CreateAuditReport(context.Context, *storage.AuditReport) (*storage.AuditReport, error)
	ListAuditReports(context.Context, uuid.UUID, int, int) ([]storage.AuditReport, int, error)
	GetAuditReport(context.Context, uuid.UUID) (*storage.AuditReport, error)
	UpdateAuditReportStatus(context.Context, uuid.UUID, string, *string, *time.Time) error
	// Compliance framework control mappings + coverage (PR 1).
	ListControlMappings(context.Context, string) ([]storage.ControlMappingRow, error)
	GetControlCoverage(context.Context, uuid.UUID, string, time.Time, time.Time) ([]storage.ControlCoverage, error)
	CountResultsForReport(context.Context, uuid.UUID, string, time.Time, time.Time) (int, int, error)
	GetPerNodeMatrix(context.Context, uuid.UUID, string, time.Time, time.Time, int) ([]storage.NodeControlRow, error)
	// Heartbeat-driven inventory + firewall state (PR 2).
	ReplaceNodePackages(context.Context, uuid.UUID, []storage.NodePackage) error
	ListNodePackages(context.Context, uuid.UUID) ([]storage.NodePackage, error)
	GetNodeInventorySync(context.Context, uuid.UUID) (*storage.NodeInventorySync, error)
	UpsertNodeInventorySync(context.Context, storage.NodeInventorySync) error
	TouchNodeInventorySync(context.Context, uuid.UUID, string) (int64, error)
	UpsertNodeFirewallState(context.Context, storage.NodeFirewallState) error
	GetNodeFirewallState(context.Context, uuid.UUID) (*storage.NodeFirewallState, error)
	// Listening-services inventory (Phase 1 of /round-up knowledge graph).
	ReplaceNodeServices(context.Context, uuid.UUID, uuid.UUID, []storage.NodeService) error
	ListNodeServicesForNode(context.Context, uuid.UUID) ([]storage.NodeService, error)
	ListNodeServicesForTenant(context.Context, uuid.UUID) ([]storage.NodeService, error)
	// Ask CISO LLM config (Phase 2). Feature-gated by FEATURE_AI_ASK.
	GetAIConfig(context.Context, uuid.UUID) (*storage.AIConfig, error)
	UpsertAIConfig(context.Context, storage.AIConfig) error
	// Network security — operator-driven IP blocks fanned out to per-node rules (PR 3).
	CreateNodeFirewallRule(context.Context, storage.NodeFirewallRuleInsert) (*storage.NodeFirewallRule, error)
	SetNodeFirewallRuleJobID(context.Context, uuid.UUID, uuid.UUID) error
	MarkNodeFirewallRuleApplied(context.Context, uuid.UUID) error
	MarkNodeFirewallRuleFailed(context.Context, uuid.UUID, string) error
	MarkNodeFirewallRuleRemoved(context.Context, uuid.UUID) error
	ListPendingNodeFirewallRules(context.Context, uuid.UUID) ([]storage.NodeFirewallRule, error)
	ListNodeFirewallRulesForEntityAction(context.Context, uuid.UUID) ([]storage.NodeFirewallRule, error)
	ListActiveBlocks(context.Context, uuid.UUID, int, int, bool) ([]storage.ActiveBlock, error)
	GetNodeFirewallRuleByJobID(context.Context, uuid.UUID) (*storage.NodeFirewallRule, error)
	// Agent self-update rollout (PR 4a).
	GetAgentRolloutState(context.Context, uuid.UUID) (*storage.AgentRolloutState, error)
	UpsertAgentRolloutState(context.Context, uuid.UUID, storage.AgentRolloutUpdate) (*storage.AgentRolloutState, error)
	// Patch management — fleet OS package patching (PR 4).
	CreatePatchDeployment(context.Context, storage.PatchDeployment) (*storage.PatchDeployment, error)
	ListPatchDeployments(context.Context, uuid.UUID, int, int) ([]storage.PatchDeployment, error)
	GetPatchDeployment(context.Context, uuid.UUID) (*storage.PatchDeployment, error)
	UpdatePatchDeploymentStatus(context.Context, uuid.UUID, string, bool) error
	CreateNodePatchState(context.Context, storage.NodePatchState) (*storage.NodePatchState, error)
	SetNodePatchStateJobID(context.Context, uuid.UUID, uuid.UUID) error
	MarkNodePatchApplied(context.Context, uuid.UUID, int, string) error
	MarkNodePatchFailed(context.Context, uuid.UUID, string, string) error
	ListPendingNodePatchStates(context.Context, uuid.UUID) ([]storage.NodePatchState, error)
	ListNodePatchStatesForDeployment(context.Context, uuid.UUID) ([]storage.NodePatchState, error)
	GetNodePatchStateByJobID(context.Context, uuid.UUID) (*storage.NodePatchState, error)
	// Predictive server downtime — Use Case 5.
	GetNodeHealthScore(context.Context, uuid.UUID) (*storage.NodeHealthScore, error)
	UpsertNodeHealthScore(context.Context, storage.UpsertNodeHealthScoreParams) (*storage.NodeHealthScore, error)
	ListAtRiskNodes(context.Context, uuid.UUID, int) ([]storage.AtRiskNodeRow, error)
	// Patch management — Wave C (proxy / airgapped / Squid / windows).
	GetNodePatchConfig(context.Context, uuid.UUID) (*storage.NodePatchConfig, error)
	UpsertNodePatchConfig(context.Context, storage.NodePatchConfig) (*storage.NodePatchConfig, error)
	CreateMaintenanceWindow(context.Context, storage.MaintenanceWindow) (*storage.MaintenanceWindow, error)
	GetMaintenanceWindow(context.Context, uuid.UUID) (*storage.MaintenanceWindow, error)
	ListMaintenanceWindows(context.Context, uuid.UUID) ([]storage.MaintenanceWindow, error)
	MarkMaintenanceWindowOpen(context.Context, uuid.UUID, *uuid.UUID) error
	MarkMaintenanceWindowClosing(context.Context, uuid.UUID) error
	MarkMaintenanceWindowClosed(context.Context, uuid.UUID) error
	MarkMaintenanceWindowAborted(context.Context, uuid.UUID) error
	ForceCloseMaintenanceWindow(context.Context, uuid.UUID) error
	CreateSquidProxy(context.Context, storage.SquidProxy) (*storage.SquidProxy, error)
	GetSquidProxy(context.Context, uuid.UUID) (*storage.SquidProxy, error)
	ListSquidProxies(context.Context, uuid.UUID) ([]storage.SquidProxy, error)
	UpdateSquidProxyStatus(context.Context, uuid.UUID, string, string) error
	UpdateSquidProxyWhitelist(context.Context, uuid.UUID, []string) error
	// Compliance reviews.
	ListComplianceReviews(context.Context, uuid.UUID, int, int) ([]storage.ComplianceReview, int, error)
	CreateComplianceReview(context.Context, *storage.ComplianceReview) (*storage.ComplianceReview, error)
	GetComplianceReview(context.Context, uuid.UUID) (*storage.ComplianceReview, error)
	CompleteComplianceReview(context.Context, uuid.UUID, uuid.UUID, *string) error
	DeleteComplianceReview(context.Context, uuid.UUID) error
	// Trust Center (public compliance portal).
	CreateSubprocessor(context.Context, *storage.Subprocessor) error
	ListSubprocessors(context.Context, string) ([]storage.Subprocessor, error)
	DeleteSubprocessor(context.Context, string) error
	CreateCertification(context.Context, *storage.Certification) error
	ListCertifications(context.Context, string) ([]storage.Certification, error)
	DeleteCertification(context.Context, string) error
	CreateFAQItem(context.Context, *storage.SecurityFAQItem) error
	ListFAQItems(context.Context, string) ([]storage.SecurityFAQItem, error)
	DeleteFAQItem(context.Context, string) error
	CreateIncidentReport(context.Context, *storage.IncidentReport) error
	ListIncidentReports(context.Context, string, int) ([]storage.IncidentReport, error)
	DeleteIncidentReport(context.Context, string) error
	GetTrustCenterData(context.Context, string) (*storage.TrustCenterData, error)
	GetTenantByName(context.Context, string) (*storage.Tenant, error)
	// Misconduct & whistleblowing (UC7).
	CreateMisconductCase(context.Context, storage.CreateMisconductCaseParams) (*storage.MisconductCase, error)
	GetMisconductCase(context.Context, uuid.UUID) (*storage.MisconductCase, error)
	ListMisconductCases(context.Context, storage.MisconductCaseFilter, int, int) ([]storage.MisconductCase, int, error)
	UpdateMisconductCase(context.Context, uuid.UUID, storage.UpdateMisconductCaseParams) (*storage.MisconductCase, error)
	SetMisconductCaseRiskScore(context.Context, uuid.UUID, int) error
	CreateWhistleblowerSubmission(context.Context, storage.CreateWhistleblowerSubmissionParams) (*storage.WhistleblowerSubmission, error)
	GetWhistleblowerSubmission(context.Context, uuid.UUID) (*storage.WhistleblowerSubmission, error)
	ListAllWhistleblowerSubmissions(context.Context) ([]storage.WhistleblowerSubmission, error)
	SweepWhistleblowerSubmissions(context.Context, time.Time) (int64, error)
	AttachCaseEvidence(context.Context, uuid.UUID, uuid.UUID) (*storage.CaseEvidenceLink, error)
	ListCaseEvidence(context.Context, uuid.UUID) ([]storage.CaseEvidenceLink, error)
	CreateRiskSignal(context.Context, storage.CreateRiskSignalParams) (*storage.RiskSignal, error)
	ListRiskSignals(context.Context, uuid.UUID) ([]storage.RiskSignal, error)
	DeleteRiskSignalsForCase(context.Context, uuid.UUID) error
	CountAuditLogsForActor(context.Context, uuid.UUID, time.Time) (int, error)
	CountSecurityEventsBySeverity(context.Context, uuid.UUID, time.Time) (map[string]int, error)
	CountFailedComplianceForTenant(context.Context, uuid.UUID, time.Time) (int, error)
	// Finacle integration (UC6): per-tenant connections, shift configs, and
	// branched profiles. ListFinacleProfilesByShift drives the rotate worker.
	CreateFinacleConnection(context.Context, storage.CreateFinacleConnectionParams) (*storage.FinacleConnection, error)
	GetFinacleConnection(context.Context, uuid.UUID) (*storage.FinacleConnection, error)
	ListFinacleConnections(context.Context, uuid.UUID) ([]storage.FinacleConnection, error)
	UpdateFinacleConnection(context.Context, uuid.UUID, storage.UpdateFinacleConnectionParams) (*storage.FinacleConnection, error)
	DeleteFinacleConnection(context.Context, uuid.UUID) error
	CreateFinacleShiftConfig(context.Context, storage.CreateFinacleShiftConfigParams) (*storage.FinacleShiftConfig, error)
	GetFinacleShiftConfig(context.Context, uuid.UUID) (*storage.FinacleShiftConfig, error)
	ListFinacleShiftConfigs(context.Context, uuid.UUID) ([]storage.FinacleShiftConfig, error)
	UpdateFinacleShiftConfig(context.Context, uuid.UUID, storage.UpdateFinacleShiftConfigParams) (*storage.FinacleShiftConfig, error)
	DeleteFinacleShiftConfig(context.Context, uuid.UUID) error
	UpsertFinacleProfile(context.Context, storage.UpsertFinacleProfileParams) (*storage.FinacleProfile, error)
	UpdateFinacleProfile(context.Context, uuid.UUID, storage.UpdateFinacleProfileParams) (*storage.FinacleProfile, error)
	GetFinacleProfile(context.Context, uuid.UUID) (*storage.FinacleProfile, error)
	ListFinacleProfiles(context.Context, uuid.UUID, int, int) ([]storage.FinacleProfile, int, error)
	ListFinacleProfilesByShift(context.Context, uuid.UUID) ([]storage.FinacleProfile, error)
	MarkFinacleProfileRotated(context.Context, uuid.UUID, string) error
	DeleteFinacleProfile(context.Context, uuid.UUID) error
}

func (s *Server) handleWorkerStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}
	if s.worker == nil {
		http.Error(w, "worker unavailable", http.StatusServiceUnavailable)
		return
	}
	provider, ok := s.worker.(workerStatusProvider)
	if !ok {
		http.Error(w, "worker status unavailable", http.StatusServiceUnavailable)
		return
	}
	status := provider.Status()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(status); err != nil {
		s.logger.Warn("encode worker status", zap.Error(err))
	}
}

func (s *Server) authorize(w http.ResponseWriter, r *http.Request, allowedRoles ...string) (*auth.Principal, bool) {
	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return nil, false
	}

	if principal.Type == "agent" {
		return principal, true
	}

	if len(allowedRoles) == 0 {
		return principal, true
	}

	for _, role := range principal.Roles {
		for _, allowed := range allowedRoles {
			if strings.EqualFold(strings.TrimSpace(role), strings.TrimSpace(allowed)) {
				return principal, true
			}
		}
	}

	for _, role := range principal.Roles {
		if strings.EqualFold(strings.TrimSpace(role), roleAdmin) {
			return principal, true
		}
	}

	http.Error(w, http.StatusText(http.StatusForbidden), http.StatusForbidden)
	return nil, false
}

type registerNodeRequest struct {
	TenantID       uuid.UUID `json:"tenant_id"`
	TenantName     string    `json:"tenant_name"`
	Hostname       string    `json:"hostname"`
	OS             *string   `json:"os"`
	Arch           *string   `json:"arch"`
	PublicIP       *string   `json:"public_ip"`
	BootstrapToken string    `json:"bootstrap_token"`
}

func (r registerNodeRequest) validate() error {
	if strings.TrimSpace(r.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	if strings.TrimSpace(r.BootstrapToken) == "" {
		return fmt.Errorf("bootstrap_token is required")
	}
	if r.TenantID == uuid.Nil && strings.TrimSpace(r.TenantName) == "" {
		return fmt.Errorf("tenant_name is required when tenant_id is not provided")
	}
	return nil
}

type registerNodeResponse struct {
	NodeID            string           `json:"node_id"`
	TenantID          string           `json:"tenant_id"`
	Intervals         map[string]int64 `json:"intervals"`
	ProvisioningHints string           `json:"provisioning_hints"`
}

func defaultNodeIntervals() map[string]int64 {
	return map[string]int64{
		"heartbeat":   60,
		"scan":        300,
		"provision":   3600,
		"policy_sync": 600,
	}
}

func (s *Server) handleNodeRegistration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	var req registerNodeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenantID := req.TenantID
	if tenantID == uuid.Nil {
		if s.cfg.Registration.DefaultTenantID != "" {
			parsed, err := uuid.Parse(s.cfg.Registration.DefaultTenantID)
			if err != nil {
				http.Error(w, "invalid default tenant id", http.StatusInternalServerError)
				return
			}
			tenantID = parsed
		}
	}

	if tenantID == uuid.Nil {
		if strings.TrimSpace(req.TenantName) == "" {
			http.Error(w, "tenant_id or tenant_name required", http.StatusBadRequest)
			return
		}
		tenantID = uuid.New()
	}

	var bootstrapAllowed bool
	if len(s.cfg.Registration.BootstrapTokens) == 0 {
		bootstrapAllowed = true
	} else {
		token := strings.TrimSpace(req.BootstrapToken)
		for _, t := range s.cfg.Registration.BootstrapTokens {
			if token == strings.TrimSpace(t) {
				bootstrapAllowed = true
				break
			}
		}
	}
	if !bootstrapAllowed {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	if s.store == nil {
		http.Error(w, "registration store unavailable", http.StatusServiceUnavailable)
		return
	}

	tenant, err := s.store.EnsureTenant(r.Context(), tenantID, req.TenantName)
	if err != nil {
		s.logger.Error("ensure tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	hostname := strings.TrimSpace(req.Hostname)
	if existing, err := s.store.GetNodeByHostname(r.Context(), tenant.ID, hostname); err != nil {
		s.logger.Error("lookup existing node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	} else if existing != nil {
		s.logger.Info("node already registered",
			zap.String("tenant_id", tenant.ID.String()),
			zap.String("node_id", existing.ID.String()),
			zap.String("hostname", hostname),
		)
		respondRegistration(w, s.logger, registerNodeResponse{
			NodeID:            existing.ID.String(),
			TenantID:          tenant.ID.String(),
			Intervals:         defaultNodeIntervals(),
			ProvisioningHints: tenant.Name,
		})
		return
	}

	node := &storage.Node{
		ID:       uuid.New(),
		TenantID: tenant.ID,
		Hostname: hostname,
		OS:       toNullString(req.OS),
		Arch:     toNullString(req.Arch),
		PublicIP: toNullString(req.PublicIP),
	}

	created, err := s.store.CreateNode(r.Context(), node)
	if err != nil {
		s.logger.Error("register node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	respondRegistration(w, s.logger, registerNodeResponse{
		NodeID:            created.ID.String(),
		TenantID:          tenant.ID.String(),
		Intervals:         defaultNodeIntervals(),
		ProvisioningHints: tenant.Name,
	})
}

func respondRegistration(w http.ResponseWriter, logger *zap.Logger, resp registerNodeResponse) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil && logger != nil {
		logger.Warn("encode registration response", zap.Error(err))
	}
}

const (
	defaultListLimit = 100
	maxListLimit     = 500

	roleViewer   = "viewer"
	roleOperator = "operator"
	roleAdmin    = "admin"

	requestIDHeader = "X-Request-Id"
)

type contextKey string

const (
	contextKeyRequestID contextKey = "controlone.request_id"
)

func parseLimitOffset(values map[string][]string) (int, int, error) {
	limit := defaultListLimit
	if v := firstQueryValue(values, "limit"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed <= 0 {
			return 0, 0, fmt.Errorf("invalid limit")
		}
		if parsed > maxListLimit {
			parsed = maxListLimit
		}
		limit = parsed
	}

	offset := 0
	if v := firstQueryValue(values, "offset"); v != "" {
		parsed, err := strconv.Atoi(v)
		if err != nil || parsed < 0 {
			return 0, 0, fmt.Errorf("invalid offset")
		}
		offset = parsed
	}

	return limit, offset, nil
}

func firstQueryValue(values map[string][]string, key string) string {
	if values == nil {
		return ""
	}
	if vals, ok := values[key]; ok && len(vals) > 0 {
		return vals[0]
	}
	return ""
}

// TaskQueue defines minimal worker manager contract for enqueuing asynchronous tasks.
type TaskQueue interface {
	Enqueue(worker.Task) error
	// EnqueueAt schedules a task to run no earlier than the given time. A zero
	// time must behave exactly like Enqueue.
	EnqueueAt(worker.Task, time.Time) error
}

type workerStatusProvider interface {
	Status() worker.Status
}

// Server wraps the HTTP server lifecycle for the control plane API.
type Server struct {
	logger                  *zap.Logger
	cfg                     *config.Config
	http                    *http.Server
	store                   Store
	worker                  TaskQueue
	authMW                  *auth.Middleware
	baseRouter              *http.ServeMux
	jobHandlers             map[string]jobHandler
	provisioningEngine      *provisioning.Engine
	complianceEngine        *compliance.Engine
	complianceScheduler     *ComplianceScheduler
	retentionScheduler      *RetentionScheduler
	reviewReminderScheduler *ReviewReminderScheduler
	healthScheduler         *HealthScheduler
	agentSigningOnce        sync.Once
	agentSigning            *agentSigningMaterial

	// policyKeyOnce + policyKeyPEM cache the policy public key shipped to
	// agents in the enrollment response. Loaded at most once per process —
	// missing-file warnings surface on the first enrollment that needed it.
	// See enrollment.go's policyPublicKeyPEM().
	policyKeyOnce sync.Once
	policyKeyPEM  []byte

	// enrollmentReaper drives the background loop that flips nodes stuck in
	// enrollment_pending to enrollment_failed after enrollmentPendingTimeout.
	enrollmentReaper enrollmentReaperState
	// clockOverride is optional — tests override it to deterministically
	// drive the reaper without real wall-clock delays.
	clockOverride func() time.Time
	// auditAsync controls whether recordAudit dispatches the store write on a
	// goroutine. Production defaults to true; tests flip it to false per-server
	// to keep audit writes deterministic without touching a shared global.
	auditAsync bool
	// sealer encrypts provider credentials at rest. nil means secrets
	// encryption is not configured — mutating endpoints must refuse to write.
	sealer *secretbox.Sealer
	// eventBus delivers realtime events (policy.updated, alert.opened, ...)
	// to SSE subscribers and internal correlators. nil means events are a no-op.
	eventBus        *eventbus.Bus
	correlationCtx  context.Context
	correlationStop context.CancelFunc
	correlationEng  *correlation.Engine
	// webauthn handles registration + assertion verification. nil means
	// webauthn is disabled and the relevant endpoints return 503.
	webauthn *webauthnlib.WebAuthn
	// Doris analytic store. Both nil when not configured.
	dorisClient *doris.Client
	dorisWriter *doris.Writer
	// Per-(tenant, node) token-bucket rate limiter for /events/ingest. Lazily
	// initialised on first request.
	ingestLimiter *rateLimiterRegistry
	// LDAP authenticator — nil when LDAP is disabled in config. Login flow
	// falls through to it after local password verify fails.
	ldapProvider *auth.LDAPProvider
	// ipIntel handles geo + ASN + reputation lookups for Investigate.
	// nil when no provider is configured; handler degrades gracefully.
	ipIntel *ipintel.Service
	// connectRegistry powers the operator "onboard a server" wizard. Lazy
	// because most servers never invoke it; tests inject a stub via
	// connectRegistryOverride to skip real network calls.
	connectRegistryOnce     sync.Once
	connectRegistryInst     *connect.Registry
	connectRegistryOverride *connect.Registry

	// Misconduct & whistleblowing (UC7) — process-local rate limiter +
	// PoW challenge store. Lazily initialised on first /intake call.
	misconductOnce   sync.Once
	whistleblowerLim *whistleblowerLimiter

	// finacleClient drives the outbound Finacle connector for sync + shift
	// rotation. Tests inject a fake; production wires a real client at boot.
	// nil ⇒ stubFinacleClient, which always returns errFinacleUnconfigured
	// (handlers convert that into 503-style messages).
	finacleClient finacleClient
}

// deepHealthy reports whether all critical sub-systems are reachable. Used
// by /healthz/deep so load balancers can route around degraded replicas
// without taking healthy ones down on a transient blip.
func (s *Server) deepHealthy(ctx context.Context) bool {
	if s == nil {
		return false
	}
	if s.store == nil {
		return false
	}
	if s.dorisClient != nil {
		ctx, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		if err := s.dorisClient.Ping(ctx); err != nil {
			return false
		}
	}
	if s.dorisWriter != nil && !s.dorisWriter.Healthy() {
		return false
	}
	return true
}

func (s *Server) startCorrelationEngine() {
	if s == nil || s.eventBus == nil || s.store == nil {
		return
	}
	if s.correlationEng != nil {
		return
	}
	s.correlationEng = correlation.New(correlationStoreAdapter{s.store}, s.eventBus, s.logger)
	s.correlationCtx, s.correlationStop = context.WithCancel(context.Background())
	go s.correlationEng.Run(s.correlationCtx)
}

// startBehavioralRollup launches the periodic behavioral baseline computation
// in a goroutine. Safe to call multiple times; subsequent calls are no-ops.
func (s *Server) startBehavioralRollup() {
	if s == nil || s.store == nil {
		return
	}
	interval := time.Hour
	if s.cfg != nil && s.cfg.Jobs.Compliance.ScheduleEnabled {
		// Reuse compliance schedule as a "jobs enabled" signal for now.
	}
	rollup := behavioral.NewRollup(behavioralStoreAdapter{s.store}, s.logger, interval, 30)
	ctx, cancel := context.WithCancel(context.Background())
	go rollup.Run(ctx)
	_ = cancel
}

// behavioralStoreAdapter narrows Store to the behavioral package's needs.
type behavioralStoreAdapter struct {
	store Store
}

func (a behavioralStoreAdapter) AggregatePortObservations(ctx context.Context, tenantID uuid.UUID, since time.Time) ([]storage.PortObservationStats, error) {
	return a.store.AggregatePortObservations(ctx, tenantID, since)
}
func (a behavioralStoreAdapter) UpsertBehavioralBaseline(ctx context.Context, tenantID uuid.UUID, nodeID *uuid.UUID, signalType, dimension string, baseline map[string]any, windowDays int) error {
	return a.store.UpsertBehavioralBaseline(ctx, tenantID, nodeID, signalType, dimension, baseline, windowDays)
}
func (a behavioralStoreAdapter) ListTenants(ctx context.Context, namePrefix string, limit, offset int) ([]storage.Tenant, int, error) {
	return a.store.ListTenants(ctx, namePrefix, limit, offset)
}

// correlationStoreAdapter narrows Store to the CorrelationEngine's needs.
type correlationStoreAdapter struct {
	store Store
}

func (a correlationStoreAdapter) ListCorrelationRules(ctx context.Context, tenantID uuid.UUID) ([]storage.CorrelationRule, error) {
	return a.store.ListCorrelationRules(ctx, tenantID)
}
func (a correlationStoreAdapter) CreateAlert(ctx context.Context, p storage.CreateAlertParams) (*storage.Alert, error) {
	return a.store.CreateAlert(ctx, p)
}

// publishEvent fan-outs a realtime event to SSE subscribers. Safe to call
// when the bus is not configured (no-op).
func (s *Server) publishEvent(ev eventbus.Event) {
	if s == nil || s.eventBus == nil {
		return
	}
	s.eventBus.Publish(ev)
}

// Handler exposes the HTTP handler for testing.
func (s *Server) Handler() http.Handler {
	return s.http.Handler
}

func (s *Server) registerRoutes() {
	s.baseRouter.HandleFunc("/api/v1/ping", s.handlePing)
	// Local + LDAP login (Phase 9). /login + /logout do NOT require an
	// existing session; the auth middleware whitelists them.
	s.baseRouter.HandleFunc("/api/v1/auth/login", s.handleAuthLogin)
	s.baseRouter.HandleFunc("/api/v1/auth/logout", s.handleAuthLogout)
	s.baseRouter.HandleFunc("/api/v1/auth/me", s.handleAuthMe)
	// RBAC + permissions catalog.
	s.baseRouter.HandleFunc("/api/v1/permissions", s.handlePermissions)
	s.baseRouter.HandleFunc("/api/v1/roles/permissions", s.handleRolesWithPermissions)
	s.baseRouter.HandleFunc("/api/v1/roles/", s.handleRoleSubroutes)
	// Custom dashboards builder (Phase 10).
	s.baseRouter.HandleFunc("/api/v1/dashboards", s.handleDashboardsCollection)
	s.baseRouter.HandleFunc("/api/v1/dashboards/", s.handleDashboardSubroutes)
	s.baseRouter.HandleFunc("/api/v1/nodes", s.handleNodesCollection)
	s.baseRouter.HandleFunc("/api/v1/nodes/", s.handleNodeResource)
	s.baseRouter.HandleFunc("/api/v1/knowledge-graph/", s.handleKnowledgeGraph)
	s.baseRouter.HandleFunc("/api/v1/ai/config", s.handleAIConfig)
	s.baseRouter.HandleFunc("/api/v1/ai/test", s.handleAITest)
	s.baseRouter.HandleFunc("/api/v1/ai/ask", s.handleAIAsk)
	s.baseRouter.HandleFunc("/api/v1/tenants", s.handleTenantsCollection)
	s.baseRouter.HandleFunc("/api/v1/tenants/", s.handleTenantResource)
	s.baseRouter.HandleFunc("/api/v1/jobs", s.handleJobsCollection)
	s.baseRouter.HandleFunc("/api/v1/jobs/", s.handleJobSubroutes)
	s.baseRouter.HandleFunc("/api/v1/templates", s.handleTemplatesCollection)
	s.baseRouter.HandleFunc("/api/v1/templates/", s.handleTemplateSubroutes)
	s.baseRouter.HandleFunc("/api/v1/users", s.handleUsersCollection)
	s.baseRouter.HandleFunc("/api/v1/users/", s.handleUserSubroutes)
	s.baseRouter.HandleFunc("/api/v1/roles", s.handleRolesCollection)
	s.baseRouter.HandleFunc("/api/v1/compliance/evaluate", s.handleComplianceEvaluate)
	s.baseRouter.HandleFunc("/api/v1/me", s.handleProfile)
	s.baseRouter.HandleFunc("/api/v1/register", s.handleNodeRegistration)
	s.baseRouter.HandleFunc("/api/v1/audit", s.handleAuditCollection)
	s.baseRouter.HandleFunc("/api/v1/worker/status", s.handleWorkerStatus)
	s.baseRouter.HandleFunc("/api/v1/policies", s.handlePoliciesCollection)
	s.baseRouter.HandleFunc("/api/v1/policies/", s.handlePolicySubroutes)
	s.baseRouter.HandleFunc("/api/v1/compliance/nodes/", s.handleComplianceNodeHistory)
	s.baseRouter.HandleFunc("/api/v1/compliance/results", s.handleComplianceResults)
	s.baseRouter.HandleFunc("/api/v1/compliance/summary", s.handleComplianceSummary)
	s.baseRouter.HandleFunc("/api/v1/compliance/export", s.handleComplianceExport)
	s.baseRouter.HandleFunc("/api/v1/telemetry", s.handleTelemetryIngest)
	s.baseRouter.HandleFunc("/api/v1/heartbeat", s.handleAgentLivenessHeartbeat)
	s.baseRouter.HandleFunc("/api/v1/telemetry/metrics", s.handleTelemetryMetrics)
	s.baseRouter.HandleFunc("/api/v1/telemetry/logs", s.handleTelemetryLogs)
	s.baseRouter.HandleFunc("/api/v1/telemetry/nodes/", s.handleTelemetryNodeSubroutes)
	s.baseRouter.HandleFunc("/api/v1/compliance/trends", s.handleComplianceTrends)
	s.baseRouter.HandleFunc("/api/v1/compliance/control-posture", s.handleComplianceControlPosture)
	s.baseRouter.HandleFunc("/api/v1/remediation/scripts", s.handleRemediationScriptsCollection)
	s.baseRouter.HandleFunc("/api/v1/remediation/scripts/", s.handleRemediationScriptSubroutes)
	s.baseRouter.HandleFunc("/api/v1/remediation/approvals", s.handleRemediationApprovalsCollection)
	s.baseRouter.HandleFunc("/api/v1/remediation/approvals/", s.handleRemediationApprovalSubroutes)
	s.baseRouter.HandleFunc("/api/v1/remediation/circuit-breaker/", s.handleRemediationCircuitBreakerSubroutes)
	s.baseRouter.HandleFunc("/api/v1/telemetry/retention/policies", s.handleRetentionPoliciesCollection)
	s.baseRouter.HandleFunc("/api/v1/telemetry/retention/cleanup", s.handleRetentionCleanup)
	s.baseRouter.HandleFunc("/api/v1/secrets/groups", s.handleSecretGroupsCollection)
	s.baseRouter.HandleFunc("/api/v1/secrets/groups/", s.handleSecretGroupSubroutes)
	s.baseRouter.HandleFunc("/api/v1/secrets/sync", s.handleSecretsSync)
	s.baseRouter.HandleFunc("/api/v1/access/entitlements", s.handleEntitlementsCollection)
	s.baseRouter.HandleFunc("/api/v1/access/entitlements/", s.handleEntitlementSubroutes)
	s.baseRouter.HandleFunc("/api/v1/access/sync", s.handleAccessSync)
	s.baseRouter.HandleFunc("/api/v1/webhooks", s.handleWebhooksCollection)
	s.baseRouter.HandleFunc("/api/v1/sessions", s.handleSessionsCollection)
	s.baseRouter.HandleFunc("/api/v1/sessions/", s.handleSessionSubroutes)
	s.baseRouter.HandleFunc("/api/v1/webhooks/", s.handleWebhookSubroutes)
	s.baseRouter.HandleFunc("/api/v1/enrollment-tokens", s.handleEnrollmentTokensCollection)
	s.baseRouter.HandleFunc("/api/v1/enrollment-tokens/", s.handleEnrollmentTokenSubroutes)
	s.baseRouter.HandleFunc("/api/v1/enroll", s.handleEnroll)
	s.baseRouter.HandleFunc("/api/v1/agent/install-script", s.handleAgentInstallScript)
	s.baseRouter.HandleFunc("/api/v1/agent/binary", s.handleAgentBinary)
	s.baseRouter.HandleFunc("/api/v1/agent/binary/manifest", s.handleAgentBinaryManifest)
	s.baseRouter.HandleFunc("/api/v1/agent/public-key", s.handleAgentPublicKey)
	s.baseRouter.HandleFunc("/api/v1/agent-rollout", s.handleAgentRollout)
	s.baseRouter.HandleFunc("/api/v1/agent/bundle", s.handleAgentBundle)
	s.baseRouter.HandleFunc("/api/v1/fleet/enroll", s.handleFleetEnroll)
	s.baseRouter.HandleFunc("/api/v1/fleet/enroll/", s.handleFleetEnrollStatus)
	s.baseRouter.HandleFunc("/api/v1/compliance/scan", s.handleComplianceBatchScan)
	s.baseRouter.HandleFunc("/api/v1/clusters", s.handleClusters)
	s.baseRouter.HandleFunc("/api/v1/clusters/", s.handleClusterSubroutes)
	s.baseRouter.HandleFunc("/api/v1/provider-credentials", s.handleProviderCredentialsCollection)
	s.baseRouter.HandleFunc("/api/v1/provider-credentials/", s.handleProviderCredentialSubroutes)
	s.baseRouter.HandleFunc("/api/v1/hypervisor-hosts", s.handleHypervisorHostsCollection)
	s.baseRouter.HandleFunc("/api/v1/hypervisor-hosts/", s.handleHypervisorHostSubroutes)
	s.baseRouter.HandleFunc("/api/v1/events/stream", s.handleEventsStream)
	s.baseRouter.HandleFunc("/api/v1/rules/port", s.handlePortRulesCollection)
	s.baseRouter.HandleFunc("/api/v1/rules/port/", s.handlePortRuleSubroutes)
	s.baseRouter.HandleFunc("/api/v1/rules/log", s.handleLogRulesCollection)
	s.baseRouter.HandleFunc("/api/v1/rules/log/", s.handleLogRuleSubroutes)
	s.baseRouter.HandleFunc("/api/v1/security-events", s.handleSecurityEventsCollection)
	s.baseRouter.HandleFunc("/api/v1/health-incidents", s.handleHealthIncidentsCollection)
	// Predictive server downtime — Use Case 5 (PR 31).
	s.baseRouter.HandleFunc("/api/v1/health/at-risk", s.handleAtRiskFleet)
	s.baseRouter.HandleFunc("/api/v1/rule-triggers", s.handleRuleTriggersCollection)
	s.baseRouter.HandleFunc("/api/v1/dashboard/overview", s.handleDashboardOverview)
	s.baseRouter.HandleFunc("/api/v1/metrics/risk-score", s.handleMetricsRiskScore)
	s.baseRouter.HandleFunc("/api/v1/metrics/mttd", s.handleMetricsMTTD)
	s.baseRouter.HandleFunc("/api/v1/metrics/mttr", s.handleMetricsMTTR)
	s.baseRouter.HandleFunc("/api/v1/metrics/remediation-velocity", s.handleMetricsRemediationVelocity)
	s.baseRouter.HandleFunc("/api/v1/metrics/findings-aging", s.handleMetricsFindingsAging)
	// Per-role dashboard history + framework breakdown.
	s.baseRouter.HandleFunc("/api/v1/dashboard/metrics/risk-score/history", s.handleMetricsRiskScoreHistory)
	s.baseRouter.HandleFunc("/api/v1/dashboard/metrics/remediation-velocity/history", s.handleMetricsRemediationVelocityHistory)
	s.baseRouter.HandleFunc("/api/v1/dashboard/metrics/compliance/by-framework", s.handleMetricsComplianceByFramework)
	// Admin self-health, ingest, tenant activity, SLO and capacity dashboards.
	s.baseRouter.HandleFunc("/api/v1/admin/self-health", s.handleAdminSelfHealth)
	s.baseRouter.HandleFunc("/api/v1/admin/ingest/throughput", s.handleAdminIngestThroughput)
	s.baseRouter.HandleFunc("/api/v1/admin/tenants/activity", s.handleAdminTenantsActivity)
	s.baseRouter.HandleFunc("/api/v1/admin/slo", s.handleAdminSLO)
	s.baseRouter.HandleFunc("/api/v1/admin/capacity", s.handleAdminCapacity)

	// Onboarding wizard (operator "add a server" flow)
	s.baseRouter.HandleFunc("/api/v1/onboarding/test-connection", s.handleOnboardingTestConnection)
	s.baseRouter.HandleFunc("/api/v1/onboarding/protocols", s.handleOnboardingProtocols)

	s.baseRouter.HandleFunc("/api/v1/alerts", s.handleAlertsCollection)
	s.baseRouter.HandleFunc("/api/v1/alerts/", s.handleAlertSubroutes)
	s.baseRouter.HandleFunc("/api/v1/access-requests", s.handleAccessRequestsCollection)
	s.baseRouter.HandleFunc("/api/v1/access-requests/", s.handleAccessRequestSubroutes)
	s.baseRouter.HandleFunc("/api/v1/ssh-ca", s.handleSSHCA)
	s.baseRouter.HandleFunc("/api/v1/ssh-ca/sign-cert", s.handleSSHCASignCert)
	s.baseRouter.HandleFunc("/api/v1/command-acl", s.handleCommandACLCollection)
	s.baseRouter.HandleFunc("/api/v1/command-acl/", s.handleCommandACLSubroutes)
	s.baseRouter.HandleFunc("/api/v1/correlation-rules", s.handleCorrelationRulesCollection)
	s.baseRouter.HandleFunc("/api/v1/correlation-rules/", s.handleCorrelationRuleSubroutes)
	s.baseRouter.HandleFunc("/api/v1/compliance/recommendations", s.handleRecommendations)
	s.baseRouter.HandleFunc("/api/v1/compliance/simulate", s.handleSimulate)
	s.baseRouter.HandleFunc("/api/v1/reports", s.handleReportsCollection)
	s.baseRouter.HandleFunc("/api/v1/reports/", s.handleReportExport)
	s.baseRouter.HandleFunc("/api/v1/mfa/factors", s.handleMFAFactors)
	s.baseRouter.HandleFunc("/api/v1/mfa/factors/", s.handleMFAFactorSubroutes)
	s.baseRouter.HandleFunc("/api/v1/mfa/totp/enroll/begin", s.handleTOTPEnrollBegin)
	s.baseRouter.HandleFunc("/api/v1/mfa/totp/enroll/finish", s.handleTOTPEnrollFinish)
	s.baseRouter.HandleFunc("/api/v1/mfa/step-up/begin", s.handleStepUpBegin)
	s.baseRouter.HandleFunc("/api/v1/mfa/step-up/verify", s.handleStepUpVerify)
	s.baseRouter.HandleFunc("/api/v1/mfa/webauthn/enroll/begin", s.handleWebAuthnEnrollBegin)
	s.baseRouter.HandleFunc("/api/v1/mfa/webauthn/enroll/finish", s.handleWebAuthnEnrollFinish)
	s.baseRouter.HandleFunc("/api/v1/mfa/webauthn/step-up/begin", s.handleWebAuthnStepUpBegin)
	s.baseRouter.HandleFunc("/api/v1/threat-feeds", s.handleThreatFeedsCollection)
	s.baseRouter.HandleFunc("/api/v1/threat-feeds/", s.handleThreatFeedSubroutes)
	s.baseRouter.HandleFunc("/api/v1/events/ingest", s.handleEventsIngest)
	s.baseRouter.HandleFunc("/api/v1/connections", s.handleConnectionsList)
	s.baseRouter.HandleFunc("/api/v1/connections/", s.handleConnectionDetail)
	s.baseRouter.HandleFunc("/api/v1/connections/top-talkers", s.handleTopTalkers)
	s.baseRouter.HandleFunc("/api/v1/fleet/health", s.handleFleetHealth)
	// SIEM Investigate / entity-search surface (Phase Investigate).
	s.baseRouter.HandleFunc("/api/v1/search", s.handleInvestigateSearch)
	s.baseRouter.HandleFunc("/api/v1/entities/", s.handleEntitySubroutes)
	s.baseRouter.HandleFunc("/api/v1/saved-searches", s.handleSavedSearchesCollection)
	s.baseRouter.HandleFunc("/api/v1/saved-searches/", s.handleSavedSearchSubroute)
	// Network security — operator-driven IP block enforcement (PR 3).
	s.baseRouter.HandleFunc("/api/v1/network/active-blocks", s.handleListActiveBlocks)
	s.baseRouter.HandleFunc("/api/v1/network/blocks/", s.handleNetworkBlocksSubroute)
	// Patch management — fleet OS package patching (PR 4).
	s.baseRouter.HandleFunc("/api/v1/patch/deployments", s.handlePatchDeployments)
	s.baseRouter.HandleFunc("/api/v1/patch/deployments/", s.handlePatchDeploymentSubroute)
	// Patch management completion — Wave C: per-node config, maintenance
	// windows, Squid proxies.
	s.baseRouter.HandleFunc("/api/v1/patch/config", s.handlePatchConfig)
	s.baseRouter.HandleFunc("/api/v1/patch/maintenance-windows", s.handleMaintenanceWindowsCollection)
	s.baseRouter.HandleFunc("/api/v1/patch/maintenance-windows/", s.handleMaintenanceWindowSubroute)
	s.baseRouter.HandleFunc("/api/v1/patch/proxies", s.handleSquidProxiesCollection)
	s.baseRouter.HandleFunc("/api/v1/patch/proxies/", s.handleSquidProxySubroute)
	// Data classification / DLP (Sprint 2).
	s.baseRouter.HandleFunc("/api/v1/dlp/rules", s.handleDLPRulesCollection)
	s.baseRouter.HandleFunc("/api/v1/dlp/rules/", s.handleDLPRulesResource)
	s.baseRouter.HandleFunc("/api/v1/dlp/columns", s.handleDLPColumnsCollection)
	s.baseRouter.HandleFunc("/api/v1/dlp/findings", s.handleDLPFindingsCollection)
	s.baseRouter.HandleFunc("/api/v1/dlp/findings/", s.handleDLPFindingsResource)
	s.baseRouter.HandleFunc("/api/v1/dlp/seed-rules", s.handleDLPSeedRules)
	// Compliance evidence + audit reports + frameworks (Sprint 3).
	s.baseRouter.HandleFunc("/api/v1/compliance/evidence", s.handleComplianceEvidenceCollection)
	s.baseRouter.HandleFunc("/api/v1/compliance/evidence/", s.handleComplianceEvidenceResource)
	s.baseRouter.HandleFunc("/api/v1/compliance/frameworks", s.handleComplianceFrameworks)
	s.baseRouter.HandleFunc("/api/v1/compliance/reports", s.handleComplianceReportsCollection)
	s.baseRouter.HandleFunc("/api/v1/compliance/reports/", s.handleComplianceReportsResource)
	s.baseRouter.HandleFunc("/api/v1/compliance/reviews", s.handleComplianceReviewsCollection)
	s.baseRouter.HandleFunc("/api/v1/compliance/reviews/", s.handleComplianceReviewsResource)
	// Trust Center (public compliance portal).
	s.baseRouter.HandleFunc("/api/v1/trust/{name}", s.handleTrustCenterPublic)
	s.baseRouter.HandleFunc("/api/v1/trust/subprocessors", s.handleSubprocessorsCollection)
	s.baseRouter.HandleFunc("/api/v1/trust/subprocessors/{id}", s.handleSubprocessorResource)
	s.baseRouter.HandleFunc("/api/v1/trust/certifications", s.handleCertificationsCollection)
	s.baseRouter.HandleFunc("/api/v1/trust/certifications/{id}", s.handleCertificationResource)
	s.baseRouter.HandleFunc("/api/v1/trust/faq", s.handleFAQCollection)
	s.baseRouter.HandleFunc("/api/v1/trust/faq/{id}", s.handleFAQResource)
	s.baseRouter.HandleFunc("/api/v1/trust/incidents", s.handleIncidentsCollection)
	s.baseRouter.HandleFunc("/api/v1/trust/incidents/{id}", s.handleIncidentResource)
	// Misconduct & whistleblowing (UC7). The submit + intake-status + challenge
	// endpoints are public (auth middleware skips them); cases are gated by
	// roleInvestigator|roleAdmin inside the handler.
	s.baseRouter.HandleFunc("/api/v1/misconduct/submit", s.handleWhistleblowerSubmit)
	s.baseRouter.HandleFunc("/api/v1/misconduct/intake-status", s.handleIntakeStatus)
	s.baseRouter.HandleFunc("/api/v1/misconduct/challenge", s.handleWhistleblowerChallenge)
	s.baseRouter.HandleFunc("/api/v1/misconduct/cases", s.handleMisconductCasesCollection)
	s.baseRouter.HandleFunc("/api/v1/misconduct/cases/", s.handleMisconductCaseSubroutes)
	// Finacle integration (UC6).
	s.baseRouter.HandleFunc("/api/v1/finacle/connections", s.handleFinacleConnections)
	s.baseRouter.HandleFunc("/api/v1/finacle/connections/", s.handleFinacleConnectionSubroutes)
	s.baseRouter.HandleFunc("/api/v1/finacle/shift-configs", s.handleFinacleShiftConfigs)
	s.baseRouter.HandleFunc("/api/v1/finacle/shift-configs/", s.handleFinacleShiftConfigSubroutes)
	s.baseRouter.HandleFunc("/api/v1/finacle/profiles", s.handleFinacleProfiles)
	s.baseRouter.HandleFunc("/api/v1/finacle/profiles/", s.handleFinacleProfileSubroutes)
	s.baseRouter.HandleFunc("/api/v1/finacle/shift-rotate", s.handleFinacleShiftRotate)
}

func (s *Server) handleProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	principal, ok := auth.PrincipalFromContext(r.Context())
	if !ok {
		http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		return
	}

	resp := profileResponse{
		Subject: principal.Subject,
		Name:    principal.Name,
		Email:   principal.Email,
		Type:    principal.Type,
		Roles:   principal.Roles,
		Groups:  principal.Groups,
	}

	if s.store != nil && strings.TrimSpace(principal.Subject) != "" {
		user, err := s.store.GetUserByExternalID(r.Context(), principal.Subject)
		if err != nil {
			s.logger.Warn("lookup profile user", zap.Error(err))
		} else if user != nil {
			resp.User = &profileUserDetails{
				ID:          user.ID.String(),
				DisplayName: nullStringPtr(user.DisplayName),
				Email:       nullStringPtr(user.Email),
				CreatedAt:   user.CreatedAt.UTC().Format(time.RFC3339),
			}
			if roles, err := s.store.ListUserRoles(r.Context(), user.ID); err != nil {
				s.logger.Warn("list profile roles", zap.Error(err))
			} else if len(roles) > 0 {
				resp.StoredRoles = append([]string{}, roles...)
			}
		}
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode profile response", zap.Error(err))
	}
}

func (s *Server) handleUsersCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "user store unavailable", http.StatusServiceUnavailable)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleListUsers(w, r)
	default:
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserSubroutes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "user store unavailable", http.StatusServiceUnavailable)
		return
	}

	idStr := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/api/v1/users/")
	userID, err := uuid.Parse(idStr)
	if err != nil {
		http.Error(w, "invalid user id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetUser(w, r, userID)
	case http.MethodPatch:
		s.handleUpdateUserRoles(w, r, userID)
	default:
		w.Header().Set("Allow", "GET, PATCH")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	users, total, err := s.store.ListUsers(r.Context(), limit, offset)
	if err != nil {
		s.logger.Error("list users", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]userResponse, 0, len(users))
	for _, user := range users {
		roles, roleErr := s.store.ListUserRoles(r.Context(), user.ID)
		if roleErr != nil {
			s.logger.Warn("list roles for user", zap.Error(roleErr), zap.String("user_id", user.ID.String()))
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
		resp = append(resp, userResponseFromModel(user, roles))
	}

	payload := paginatedResponse[userResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Warn("encode users response", zap.Error(err))
	}
}

func (s *Server) handleGetUser(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		s.logger.Error("get user", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.NotFound(w, r)
		return
	}

	roles, err := s.store.ListUserRoles(r.Context(), user.ID)
	if err != nil {
		s.logger.Error("list user roles", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(userResponseFromModel(*user, roles)); err != nil {
		s.logger.Warn("encode user response", zap.Error(err))
	}
}

func (s *Server) handleUpdateUserRoles(w http.ResponseWriter, r *http.Request, userID uuid.UUID) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	var req updateUserRolesRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := s.store.SetUserRoles(r.Context(), userID, req.Roles); err != nil {
		s.logger.Error("set user roles", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	user, err := s.store.GetUser(r.Context(), userID)
	if err != nil {
		s.logger.Error("get user after update", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if user == nil {
		http.NotFound(w, r)
		return
	}

	roles, err := s.store.ListUserRoles(r.Context(), userID)
	if err != nil {
		s.logger.Error("list user roles after update", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(userResponseFromModel(*user, roles)); err != nil {
		s.logger.Warn("encode updated user response", zap.Error(err))
	}

	s.recordAudit(r.Context(), principal, uuid.Nil, "user.roles.update", "user", userID.String(), map[string]any{
		"roles": roles,
	})
}

func (s *Server) handleRolesCollection(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "role store unavailable", http.StatusServiceUnavailable)
		return
	}

	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if _, ok := s.authorize(w, r, roleAdmin); !ok {
		return
	}

	roles, err := s.store.ListRoles(r.Context())
	if err != nil {
		s.logger.Error("list roles", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]roleResponse, 0, len(roles))
	for _, role := range roles {
		resp = append(resp, roleResponseFromModel(role))
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode roles response", zap.Error(err))
	}
}

type profileResponse struct {
	Subject     string              `json:"subject"`
	Name        string              `json:"name"`
	Email       string              `json:"email"`
	Type        string              `json:"type"`
	Roles       []string            `json:"roles"`
	Groups      []string            `json:"groups"`
	StoredRoles []string            `json:"stored_roles,omitempty"`
	User        *profileUserDetails `json:"user,omitempty"`
}

type userResponse struct {
	ID          string   `json:"id"`
	ExternalID  string   `json:"external_id"`
	DisplayName *string  `json:"display_name,omitempty"`
	Email       *string  `json:"email,omitempty"`
	Roles       []string `json:"roles"`
	CreatedAt   string   `json:"created_at"`
}

func userResponseFromModel(u storage.User, roles []string) userResponse {
	return userResponse{
		ID:          u.ID.String(),
		ExternalID:  u.ExternalID,
		DisplayName: nullStringPtr(u.DisplayName),
		Email:       nullStringPtr(u.Email),
		Roles:       append([]string{}, roles...),
		CreatedAt:   u.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type roleResponse struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func roleResponseFromModel(role storage.Role) roleResponse {
	return roleResponse{
		ID:          role.ID.String(),
		Name:        role.Name,
		Description: nullStringPtr(role.Description),
		CreatedAt:   role.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type updateUserRolesRequest struct {
	Roles []string `json:"roles"`
}

func (r updateUserRolesRequest) validate() error {
	if len(r.Roles) == 0 {
		return fmt.Errorf("roles are required")
	}
	for i, role := range r.Roles {
		trimmed := strings.TrimSpace(role)
		if trimmed == "" {
			return fmt.Errorf("roles[%d] cannot be empty", i)
		}
		r.Roles[i] = trimmed
	}
	return nil
}

type profileUserDetails struct {
	ID          string  `json:"id"`
	DisplayName *string `json:"display_name,omitempty"`
	Email       *string `json:"email,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

func (s *Server) handlePing(w http.ResponseWriter, r *http.Request) {
	principal, ok := s.authorize(w, r)
	if !ok {
		return
	}

	resp := map[string]any{
		"message":   "pong",
		"principal": principal,
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		s.logger.Warn("encode ping response", zap.Error(err))
	}
}

func (s *Server) handleTenantsCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListTenants(w, r)
	case http.MethodPost:
		s.handleCreateTenant(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "tenant store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	namePrefix := strings.TrimSpace(r.URL.Query().Get("name_prefix"))

	tenants, total, err := s.store.ListTenants(r.Context(), namePrefix, limit, offset)
	if err != nil {
		s.logger.Error("list tenants", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]tenantResponse, 0, len(tenants))
	for _, t := range tenants {
		resp = append(resp, tenantResponseFromModel(t))
	}

	payload := paginatedResponse[tenantResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Warn("encode tenants response", zap.Error(err))
	}
}

func (s *Server) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "tenant store unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	var req createTenantRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenant := &storage.Tenant{
		Name: strings.TrimSpace(req.Name),
	}

	created, err := s.store.CreateTenant(r.Context(), tenant)
	if err != nil {
		s.logger.Error("create tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(tenantResponseFromModel(*created)); err != nil {
		s.logger.Warn("encode tenant response", zap.Error(err))
	}
	s.recordAudit(r.Context(), principal, created.ID, "tenant.create", "tenant", created.ID.String(), map[string]any{
		"name": created.Name,
	})
}

func (s *Server) handleTenantResource(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "tenant store unavailable", http.StatusServiceUnavailable)
		return
	}

	rest := strings.TrimPrefix(strings.TrimSuffix(r.URL.Path, "/"), "/api/v1/tenants/")
	// Subroutes under a tenant: dispatch by suffix.
	if i := strings.Index(rest, "/"); i >= 0 {
		switch rest[i+1:] {
		case "event-filters":
			s.handleTenantEventFilters(w, r)
			return
		case "remediation-config":
			s.handleTenantRemediationConfig(w, r)
			return
		}
		http.NotFound(w, r)
		return
	}

	tenantID, err := uuid.Parse(rest)
	if err != nil {
		http.Error(w, "invalid tenant id", http.StatusBadRequest)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetTenant(w, r, tenantID)
	case http.MethodPatch:
		s.handleUpdateTenant(w, r, tenantID)
	case http.MethodDelete:
		s.handleDeleteTenant(w, r, tenantID)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetTenant(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	tenant, err := s.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("get tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tenantResponseFromModel(*tenant)); err != nil {
		s.logger.Warn("encode tenant response", zap.Error(err))
	}
}

func (s *Server) handleUpdateTenant(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	var req updateTenantRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	name := strings.TrimSpace(*req.Name)
	updated, err := s.store.UpdateTenant(r.Context(), tenantID, name)
	if err != nil {
		s.logger.Error("update tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(tenantResponseFromModel(*updated)); err != nil {
		s.logger.Warn("encode tenant response", zap.Error(err))
	}
	s.recordAudit(r.Context(), principal, tenantID, "tenant.update", "tenant", tenantID.String(), map[string]any{
		"name": updated.Name,
	})
}

func (s *Server) handleDeleteTenant(w http.ResponseWriter, r *http.Request, tenantID uuid.UUID) {
	principal, ok := s.authorize(w, r, roleAdmin)
	if !ok {
		return
	}

	if err := s.store.DeleteTenant(r.Context(), tenantID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("delete tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	s.recordAudit(r.Context(), principal, tenantID, "tenant.delete", "tenant", tenantID.String(), nil)
}

type createTenantRequest struct {
	Name string `json:"name"`
}

func (r createTenantRequest) validate() error {
	if strings.TrimSpace(r.Name) == "" {
		return fmt.Errorf("name is required")
	}
	return nil
}

type tenantResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func tenantResponseFromModel(t storage.Tenant) tenantResponse {
	return tenantResponse{
		ID:        t.ID.String(),
		Name:      t.Name,
		CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

type updateTenantRequest struct {
	Name *string `json:"name"`
}

func (r updateTenantRequest) validate() error {
	if r.Name == nil {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(*r.Name) == "" {
		return fmt.Errorf("name cannot be empty")
	}
	return nil
}

func (s *Server) handleNodesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleListNodes(w, r)
	case http.MethodPost:
		s.handleCreateNode(w, r)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleListNodes(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "node store unavailable", http.StatusServiceUnavailable)
		return
	}

	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	limit, offset, err := parseLimitOffset(r.URL.Query())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	var tenantID uuid.UUID
	if tenantParam := strings.TrimSpace(r.URL.Query().Get("tenant_id")); tenantParam != "" {
		parsed, err := uuid.Parse(tenantParam)
		if err != nil {
			http.Error(w, "invalid tenant_id", http.StatusBadRequest)
			return
		}
		tenantID = parsed
	}

	hostnamePrefix := strings.TrimSpace(r.URL.Query().Get("hostname_prefix"))

	nodes, total, err := s.store.ListNodes(r.Context(), tenantID, hostnamePrefix, limit, offset)
	if err != nil {
		s.logger.Error("list nodes", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	resp := make([]nodeResponse, 0, len(nodes))
	for _, n := range nodes {
		resp = append(resp, nodeResponseFromModel(n))
	}

	payload := paginatedResponse[nodeResponse]{
		Data:       resp,
		Pagination: newPaginationMeta(total, limit, offset, len(resp)),
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		s.logger.Warn("encode nodes response", zap.Error(err))
	}
}

func (s *Server) handleCreateNode(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "node store unavailable", http.StatusServiceUnavailable)
		return
	}

	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	var req createNodeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	tenantID, err := uuid.Parse(req.TenantID)
	if err != nil {
		http.Error(w, "invalid tenant_id", http.StatusBadRequest)
		return
	}
	tenant, err := s.store.GetTenant(r.Context(), tenantID)
	if err != nil {
		s.logger.Error("get tenant", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if tenant == nil {
		http.Error(w, "tenant not found", http.StatusBadRequest)
		return
	}

	node := &storage.Node{
		TenantID: tenantID,
		Hostname: strings.TrimSpace(req.Hostname),
		OS:       toNullString(req.OS),
		Arch:     toNullString(req.Arch),
		PublicIP: toNullString(req.PublicIP),
	}

	created, err := s.store.CreateNode(r.Context(), node)
	if err != nil {
		s.logger.Error("create node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	if err := json.NewEncoder(w).Encode(nodeResponseFromModel(*created)); err != nil {
		s.logger.Warn("encode node response", zap.Error(err))
	}
	s.recordAudit(r.Context(), principal, tenantID, "node.create", "node", created.ID.String(), map[string]any{
		"hostname": created.Hostname,
	})
}

func (s *Server) handleNodeResource(w http.ResponseWriter, r *http.Request) {
	if s.store == nil {
		http.Error(w, "node store unavailable", http.StatusServiceUnavailable)
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/api/v1/nodes/")
	trimmed = strings.Trim(trimmed, "/")
	segments := strings.Split(trimmed, "/")
	if len(segments) == 0 || segments[0] == "" {
		http.NotFound(w, r)
		return
	}

	nodeID, err := uuid.Parse(segments[0])
	if err != nil {
		http.Error(w, "invalid node id", http.StatusBadRequest)
		return
	}

	if len(segments) == 2 && segments[1] == "retire" {
		s.handleRetireNode(w, r, nodeID)
		return
	}

	if len(segments) == 2 && segments[1] == "rotate-cert" {
		s.handleRotateCert(w, r, nodeID)
		return
	}

	if len(segments) == 2 && segments[1] == "heartbeat" {
		s.handleNodeHeartbeat(w, r, nodeID)
		return
	}

	if len(segments) == 2 && segments[1] == "update-agent" {
		s.handleNodeAgentUpdate(w, r, nodeID)
		return
	}

	if len(segments) == 2 && segments[1] == "health" {
		s.handleNodeHealth(w, r, nodeID)
		return
	}

	if len(segments) == 2 && segments[1] == "services" {
		s.handleNodeServices(w, r, nodeID)
		return
	}

	if len(segments) == 2 && segments[1] == "repair" {
		s.handleNodeRepair(w, r, nodeID)
		return
	}

	if len(segments) != 1 {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodGet:
		s.handleGetNode(w, r, nodeID)
	case http.MethodPatch:
		s.handleUpdateNode(w, r, nodeID)
	case http.MethodDelete:
		s.handleDeleteNode(w, r, nodeID)
	default:
		w.Header().Set("Allow", "GET, PATCH, DELETE")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleGetNode(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	if _, ok := s.authorize(w, r, roleViewer); !ok {
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodeResponseFromModel(*node)); err != nil {
		s.logger.Warn("encode node response", zap.Error(err))
	}
}

func (s *Server) handleUpdateNode(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	var req updateNodeRequest
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	if err := req.validate(); err != nil {
		http.Error(w, fmt.Sprintf("invalid payload: %v", err), http.StatusBadRequest)
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	if req.Hostname != nil {
		name := strings.TrimSpace(*req.Hostname)
		if name == "" {
			http.Error(w, "hostname cannot be empty", http.StatusBadRequest)
			return
		}
		node.Hostname = name
	}
	if req.OS != nil {
		node.OS = toNullString(req.OS)
	}
	if req.Arch != nil {
		node.Arch = toNullString(req.Arch)
	}
	if req.PublicIP != nil {
		node.PublicIP = toNullString(req.PublicIP)
	}

	updated, err := s.store.UpdateNode(r.Context(), node)
	if err != nil {
		s.logger.Error("update node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if updated == nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(nodeResponseFromModel(*updated)); err != nil {
		s.logger.Warn("encode node response", zap.Error(err))
	}
	s.recordAudit(r.Context(), principal, updated.TenantID, "node.update", "node", nodeID.String(), map[string]any{
		"hostname": updated.Hostname,
	})
}

func (s *Server) handleDeleteNode(w http.ResponseWriter, r *http.Request, nodeID uuid.UUID) {
	principal, ok := s.authorize(w, r, roleOperator, roleAdmin)
	if !ok {
		return
	}

	node, err := s.store.GetNode(r.Context(), nodeID)
	if err != nil {
		s.logger.Error("get node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	if node == nil {
		http.NotFound(w, r)
		return
	}

	if err := s.store.DeleteNode(r.Context(), nodeID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		s.logger.Error("delete node", zap.Error(err))
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
	s.recordAudit(r.Context(), principal, node.TenantID, "node.delete", "node", nodeID.String(), map[string]any{
		"hostname": node.Hostname,
	})
}

type createNodeRequest struct {
	TenantID string  `json:"tenant_id"`
	Hostname string  `json:"hostname"`
	OS       *string `json:"os"`
	Arch     *string `json:"arch"`
	PublicIP *string `json:"public_ip"`
}

func (r createNodeRequest) validate() error {
	if _, err := uuid.Parse(r.TenantID); err != nil {
		return fmt.Errorf("invalid tenant_id: %w", err)
	}
	if strings.TrimSpace(r.Hostname) == "" {
		return fmt.Errorf("hostname is required")
	}
	return nil
}

type nodeResponse struct {
	ID           string         `json:"id"`
	TenantID     string         `json:"tenant_id"`
	Hostname     string         `json:"hostname"`
	OS           *string        `json:"os,omitempty"`
	Arch         *string        `json:"arch,omitempty"`
	PublicIP     *string        `json:"public_ip,omitempty"`
	State        string         `json:"state"`
	LastSeenAt   *string        `json:"last_seen_at,omitempty"`
	FirstScanAt  *string        `json:"first_scan_at,omitempty"`
	Labels       map[string]any `json:"labels"`
	AgentVersion *string        `json:"agent_version,omitempty"`
	CreatedAt    string         `json:"created_at"`
	UpdatedAt    string         `json:"updated_at"`
}

func nodeResponseFromModel(n storage.Node) nodeResponse {
	resp := nodeResponse{
		ID:           n.ID.String(),
		TenantID:     n.TenantID.String(),
		Hostname:     n.Hostname,
		OS:           nullStringPtr(n.OS),
		Arch:         nullStringPtr(n.Arch),
		PublicIP:     nullStringPtr(n.PublicIP),
		State:        n.State,
		AgentVersion: nullStringPtr(n.AgentVersion),
		Labels:       n.Labels,
		CreatedAt:    n.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:    n.UpdatedAt.UTC().Format(time.RFC3339),
	}
	if resp.Labels == nil {
		resp.Labels = map[string]any{}
	}
	if n.LastSeenAt != nil {
		ts := n.LastSeenAt.UTC().Format(time.RFC3339)
		resp.LastSeenAt = &ts
	}
	if n.FirstScanAt != nil {
		ts := n.FirstScanAt.UTC().Format(time.RFC3339)
		resp.FirstScanAt = &ts
	}
	return resp
}

type updateNodeRequest struct {
	Hostname *string `json:"hostname"`
	OS       *string `json:"os"`
	Arch     *string `json:"arch"`
	PublicIP *string `json:"public_ip"`
}

func (r updateNodeRequest) validate() error {
	if r.Hostname == nil && r.OS == nil && r.Arch == nil && r.PublicIP == nil {
		return fmt.Errorf("at least one field must be provided")
	}
	if r.Hostname != nil && strings.TrimSpace(*r.Hostname) == "" {
		return fmt.Errorf("hostname cannot be empty")
	}
	return nil
}

func toNullString(value *string) sql.NullString {
	if value == nil {
		return sql.NullString{}
	}
	trimmed := strings.TrimSpace(*value)
	if trimmed == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: trimmed, Valid: true}
}

func nullStringPtr(ns sql.NullString) *string {
	if !ns.Valid {
		return nil
	}
	value := ns.String
	return &value
}

var systemPrincipal = &auth.Principal{
	Type: "system",
	Name: "controlplane",
}

func (s *Server) systemActor() *auth.Principal {
	return systemPrincipal
}

func (s *Server) buildJobExecution(jobID uuid.UUID, jobType string, maxAttempts int) func(context.Context) error {
	if maxAttempts <= 0 {
		maxAttempts = 1
	}
	var attemptCounter int32
	return func(ctx context.Context) error {
		currentAttempt := int(atomic.AddInt32(&attemptCounter, 1))
		principal := s.systemActor()

		s.configureJobIntegrations()
		handler, ok := s.jobHandlers[jobType]
		if !ok {
			return fmt.Errorf("no handler registered for job type %s", jobType)
		}

		finish := metricsTrackJob(jobType)
		outcome := metricsStatusSuccess
		defer func() { finish(outcome) }()

		job, err := s.store.GetJob(ctx, jobID)
		if err != nil {
			outcome = metricsStatusError
			return fmt.Errorf("load job: %w", err)
		}
		if job == nil {
			outcome = metricsStatusFailure
			return fmt.Errorf("job %s not found", jobID)
		}

		startFields := map[string]any{}
		if job.StartedAt == nil {
			startFields["started_at"] = time.Now()
		}
		startMsg := fmt.Sprintf("job started (attempt %d)", currentAttempt)
		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusRunning, startMsg, startFields); err != nil {
			outcome = metricsStatusError
			return fmt.Errorf("update job running: %w", err)
		}
		s.recordAudit(ctx, principal, job.TenantID, "job.running", "job", job.ID.String(), map[string]any{
			"type":         job.Type,
			"attempt":      currentAttempt,
			"max_attempts": maxAttempts,
		})

		if err := handler(ctx, job); err != nil {
			outcome = metricsStatusFailure
			s.logger.Error("job execution failed",
				zap.String("job_type", jobType),
				zap.String("job_id", jobID.String()),
				zap.Int("attempt", currentAttempt),
				zap.Error(err),
			)

			retries := job.Retries + 1
			fields := map[string]any{"retries": retries}
			status := storage.JobStatusQueued
			msg := fmt.Sprintf("attempt %d/%d failed: %v", currentAttempt, maxAttempts, err)
			if currentAttempt >= maxAttempts {
				status = storage.JobStatusFailed
				fields["finished_at"] = time.Now()
				msg = fmt.Sprintf("job failed after %d attempts: %v", currentAttempt, err)
			}
			if storeErr := s.store.UpdateJobStatus(ctx, jobID, status, msg, fields); storeErr != nil {
				s.logger.Error("update job failed", zap.Error(storeErr))
			}
			failureMetadata := map[string]any{
				"type":         job.Type,
				"attempt":      currentAttempt,
				"max_attempts": maxAttempts,
				"retries":      retries,
				"error":        err.Error(),
			}
			if status == storage.JobStatusQueued {
				s.recordAudit(ctx, principal, job.TenantID, "job.retry_scheduled", "job", job.ID.String(), failureMetadata)
			} else {
				s.recordAudit(ctx, principal, job.TenantID, "job.failed", "job", job.ID.String(), failureMetadata)
			}
			return err
		}

		successFields := map[string]any{"finished_at": time.Now()}
		successMsg := "job completed"
		if currentAttempt > 1 {
			successMsg = fmt.Sprintf("job completed after %d attempts", currentAttempt)
		}
		if err := s.store.UpdateJobStatus(ctx, jobID, storage.JobStatusSucceeded, successMsg, successFields); err != nil {
			outcome = metricsStatusError
			return fmt.Errorf("update job success: %w", err)
		}
		s.recordAudit(ctx, principal, job.TenantID, "job.succeeded", "job", job.ID.String(), map[string]any{
			"type":         job.Type,
			"attempt":      currentAttempt,
			"max_attempts": maxAttempts,
		})

		outcome = metricsStatusSuccess
		return nil
	}
}

// New constructs a Server with default routes and middleware.
func New(logger *zap.Logger, cfg *config.Config, store Store, worker TaskQueue) *Server {
	mux := http.NewServeMux()
	// /healthz is defined here as a closure so the post-construction Server
	// can plug in deep-storage health (Doris ping, writer health) below
	// without re-registering the route.
	var serverRef *Server
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		// Liveness only — we always 200 unless the binary is dead.
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("/healthz/deep", func(w http.ResponseWriter, r *http.Request) {
		if serverRef != nil && !serverRef.deepHealthy(r.Context()) {
			http.Error(w, "degraded", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	if cfg.Observability.EnableMetrics {
		path := cfg.Observability.MetricsPath
		if path == "" {
			path = "/metrics"
		}
		initServerMetrics()
		mux.Handle(path, promhttp.Handler())
	}

	var identityStore auth.IdentityStore
	if store != nil {
		if typed, ok := store.(auth.IdentityStore); ok {
			identityStore = typed
		}
	}

	authMW := auth.NewMiddleware(logger, cfg.TLS.RequireClientTLS, cfg.Auth, identityStore)

	httpServer := &http.Server{
		Addr: cfg.HTTP.Address,
		Handler: loggingMiddleware(logger,
			requestIDMiddleware(authMW.Wrap(mux))),
		ReadTimeout:  cfg.HTTP.ReadTimeout,
		WriteTimeout: cfg.HTTP.WriteTimeout,
	}

	s := &Server{logger: logger, cfg: cfg, http: httpServer, store: store, worker: worker, authMW: authMW, baseRouter: mux, auditAsync: true, eventBus: eventbus.New(64)}
	serverRef = s

	// IP intelligence (Investigate). Wires akyriako/ipquery + AbuseIPDB with
	// a Postgres-backed cache when *storage.Store is available; falls back
	// to an in-memory cache otherwise (tests + minimal deployments).
	if enabled, primary := ipintel.Validate(cfg.IPIntel); enabled {
		var cache ipintel.Cache = ipintel.NewMemCache()
		if concrete, ok := store.(*storage.Store); ok && concrete != nil {
			cache = storage.NewIPIntelCache(concrete.DB())
		}
		s.ipIntel = ipintel.New(cfg.IPIntel, cache)
		logger.Info("ipintel enabled", zap.String("primary_provider", primary))
	} else {
		logger.Info("ipintel disabled — set IPQUERY_BASE_URL or ABUSEIPDB_API_KEY to enable")
	}
	// Doris analytic store. Optional — when unconfigured, ingest stays
	// on Postgres + journal (see /events/ingest).
	if cfg.Doris.Enabled && cfg.Doris.DSN != "" {
		dorisCli, derr := doris.New(doris.Config{
			DSN:          cfg.Doris.DSN,
			HTTPEndpoint: cfg.Doris.HTTPEndpoint,
			Database:     cfg.Doris.Database,
			User:         cfg.Doris.User,
			Password:     cfg.Doris.Password,
		})
		if derr != nil {
			logger.Error("init doris client", zap.Error(derr))
		} else {
			s.dorisClient = dorisCli
			s.dorisWriter = doris.NewWriter(dorisCli, doris.WriterOptions{
				FlushInterval: 2 * time.Second,
				MaxBatchRows:  5000,
				Metrics:       doris.NewPrometheusReporter(nil),
				OnError: func(table string, e error) {
					logger.Warn("doris write", zap.String("table", table), zap.Error(e))
				},
			})
			if cfg.Doris.ApplyMigrations {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				if mErr := doris.ApplyMigrations(ctx, dorisCli); mErr != nil {
					logger.Error("apply doris migrations", zap.Error(mErr))
				}
				cancel()
			}
		}
	}
	if sealer, err := secretbox.NewSealerFromConfig(cfg.Secrets.EncryptionKey); err != nil {
		logger.Warn("init secrets sealer", zap.Error(err))
	} else {
		s.sealer = sealer
	}
	if cfg.WebAuthn.RPID != "" && cfg.WebAuthn.RPOrigin != "" {
		wa, werr := mfa.NewWebAuthn(mfa.WebAuthnConfig{
			RPID:     cfg.WebAuthn.RPID,
			RPName:   cfg.WebAuthn.RPName,
			RPOrigin: cfg.WebAuthn.RPOrigin,
		})
		if werr != nil {
			logger.Warn("init webauthn", zap.Error(werr))
		} else {
			s.webauthn = wa
		}
	}
	s.configureJobIntegrations()
	s.registerRoutes()

	if cfg.Jobs.Compliance.ScheduleEnabled {
		sched := NewComplianceScheduler(s)
		cronExpr := cfg.Jobs.Compliance.ScheduleCron
		if cronExpr == "" {
			cronExpr = "0 */6 * * *"
		}
		if err := sched.Start(cronExpr); err != nil {
			logger.Error("start compliance scheduler", zap.Error(err))
		} else {
			s.complianceScheduler = sched
		}
	}

	// Retention runs unconditionally — operators rely on it to keep
	// telemetry tables bounded. Hardcoded six-hour cron unless overridden.
	if store != nil {
		retention := NewRetentionScheduler(s)
		if err := retention.Start("0 */6 * * *"); err != nil {
			logger.Error("start retention scheduler", zap.Error(err))
		} else {
			s.retentionScheduler = retention
		}
	}

	// Review reminder scheduler — sends reminders for upcoming compliance reviews.
	if store != nil {
		reviewReminder := NewReviewReminderScheduler(s)
		if err := reviewReminder.Start("0 9 * * *"); err != nil {
			logger.Error("start review reminder scheduler", zap.Error(err))
		} else {
			s.reviewReminderScheduler = reviewReminder
		}
	}

	// Health scheduler — hourly EWMA baselines + predictive scoring.
	// Runs unconditionally when the store is available; no config gate.
	if store != nil {
		health := NewHealthScheduler(s)
		if err := health.Start("@hourly"); err != nil {
			logger.Error("start health scheduler", zap.Error(err))
		} else {
			s.healthScheduler = health
		}
	}

	// Bastion proxy — opt-in. When enabled the proxy listens on
	// cfg.Bastion.ListenAddr, authenticates operators via tenant-CA-signed
	// SSH certs, dials the target node via the existing mTLS tunnel
	// (sshproxy.MTLSDialer), and emits bastion.session.{open,close}
	// events into the eventbus + Doris so the timeline links every
	// privileged session to the connection rows it produces.
	if cfg.Bastion.Enabled {
		if err := s.startBastionProxy(context.Background()); err != nil {
			logger.Error("start bastion proxy", zap.Error(err))
		}
	}

	if cfg.LDAP.Enabled {
		ldapCfg := auth.LDAPConfig{
			Enabled:      cfg.LDAP.Enabled,
			URL:          cfg.LDAP.URL,
			StartTLS:     cfg.LDAP.StartTLS,
			SkipVerify:   cfg.LDAP.SkipVerify,
			BindDN:       cfg.LDAP.BindDN,
			BindPassword: cfg.LDAP.BindPassword,
			UserBaseDN:   cfg.LDAP.UserBaseDN,
			UserFilter:   cfg.LDAP.UserFilter,
			GroupBaseDN:  cfg.LDAP.GroupBaseDN,
			GroupFilter:  cfg.LDAP.GroupFilter,
			GroupAttr:    cfg.LDAP.GroupAttr,
			EmailAttr:    cfg.LDAP.EmailAttr,
			NameAttr:     cfg.LDAP.NameAttr,
			GroupRoleMap: cfg.LDAP.GroupRoleMap,
			DefaultRole:  cfg.LDAP.DefaultRole,
		}
		if p, err := auth.NewLDAPProvider(ldapCfg, logger); err != nil {
			logger.Error("init ldap provider", zap.Error(err))
		} else {
			s.ldapProvider = p
		}
	}

	return s
}

// Start begins serving HTTP requests.
func (s *Server) Start() error {
	s.startEnrollmentReaper()
	s.startCorrelationEngine()
	s.startBehavioralRollup()

	if !s.cfg.TLS.Enabled {
		return s.http.ListenAndServe()
	}

	tlsConfig, err := s.buildTLSConfig()
	if err != nil {
		return err
	}
	s.http.TLSConfig = tlsConfig

	return s.http.ListenAndServeTLS(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
}

// Stop gracefully shuts down the HTTP server and compliance scheduler.
func (s *Server) Stop(ctx context.Context) error {
	if s.complianceScheduler != nil {
		s.complianceScheduler.Stop()
	}
	if s.retentionScheduler != nil {
		s.retentionScheduler.Stop()
	}
	if s.healthScheduler != nil {
		s.healthScheduler.Stop()
	}
	s.stopEnrollmentReaper()
	shutdownCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	return s.http.Shutdown(shutdownCtx)
}

func loggingMiddleware(logger *zap.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(ww, r)
		fields := []zap.Field{
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.Int("status", ww.status),
			zap.Int64("bytes", ww.bytes),
			zap.Duration("duration", time.Since(start)),
		}
		if requestID, ok := requestIDFromContext(r.Context()); ok {
			fields = append(fields, zap.String("request_id", requestID))
		}
		logger.Info("http request",
			fields...,
		)
	})
}

func requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := strings.TrimSpace(r.Header.Get(requestIDHeader))
		if requestID == "" {
			requestID = uuid.NewString()
		}
		ctx := context.WithValue(r.Context(), contextKeyRequestID, requestID)
		w.Header().Set(requestIDHeader, requestID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func requestIDFromContext(ctx context.Context) (string, bool) {
	if ctx == nil {
		return "", false
	}
	val := ctx.Value(contextKeyRequestID)
	if requestID, ok := val.(string); ok && strings.TrimSpace(requestID) != "" {
		return requestID, true
	}
	return "", false
}

func (s *Server) buildTLSConfig() (*tls.Config, error) {
	if s.cfg.TLS.CertFile == "" || s.cfg.TLS.KeyFile == "" {
		return nil, fmt.Errorf("tls enabled but cert/key not configured")
	}

	cert, err := tls.LoadX509KeyPair(s.cfg.TLS.CertFile, s.cfg.TLS.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("load server key pair: %w", err)
	}

	tlsCfg := &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
	}

	// Three modes:
	//  • RequireClientTLS=true                       → require + verify client cert
	//  • RequireClientTLS=false + ClientCAFile set   → verify if given (mixed clients:
	//    UI uses bearer/cookie, agent presents a cert)
	//  • RequireClientTLS=false + no ClientCAFile    → don't request a client cert
	if s.cfg.TLS.ClientCAFile != "" {
		caPEM, err := os.ReadFile(s.cfg.TLS.ClientCAFile)
		if err != nil {
			return nil, fmt.Errorf("read client ca: %w", err)
		}
		pool := x509.NewCertPool()
		if !pool.AppendCertsFromPEM(caPEM) {
			return nil, fmt.Errorf("append client ca certs failed")
		}
		tlsCfg.ClientCAs = pool
		if s.cfg.TLS.RequireClientTLS {
			tlsCfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			tlsCfg.ClientAuth = tls.VerifyClientCertIfGiven
		}
	} else if s.cfg.TLS.RequireClientTLS {
		return nil, fmt.Errorf("client TLS required but client_ca_file missing")
	}

	return tlsCfg, nil
}

type responseWriter struct {
	http.ResponseWriter
	status int
	bytes  int64
}

func (w *responseWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *responseWriter) Write(b []byte) (int, error) {
	n, err := w.ResponseWriter.Write(b)
	w.bytes += int64(n)
	return n, err
}

// Flush implements http.Flusher by delegating to the underlying ResponseWriter.
// Without this, SSE handlers that do w.(http.Flusher) receive ok=false and
// return 500 "streaming unsupported" because loggingMiddleware wraps every
// request in *responseWriter, hiding the underlying Flusher implementation.
func (w *responseWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
