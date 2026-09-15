import { useCallback, useEffect, useMemo, useState } from 'react';
import { AlertTriangle, ArrowRight, Bell, CheckCircle2, ExternalLink, ListChecks, Plus, RefreshCw, Shield, ShieldCheck, Trash2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import { ConfirmModal } from '../components/ConfirmModal';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '../components/ui/dialog';
import { IpActionMenu } from '../components/kit/IpActionMenu';
import {
  Chart,
  DataTable,
  EmptyState,
  EntityChip,
  KpiTile,
  Panel,
  SectionHeader,
  SelectField,
  StatusTag,
  type StateTone,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import { useEventStream } from '../hooks/useEventStream';
import { useTenant } from '../providers/TenantProvider';
import { classifyValue } from '../lib/entity';
import { formatBytes } from '../lib/format';
import { CORRELATION_RULE_TEMPLATES, correlationRuleTemplate } from '../lib/correlationTemplates';
import type { Alert, AlertDispositionValue, CorrelationCondition, CorrelationRule, UpdateAlertDispositionPayload } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

type PageTab = 'alerts' | 'rules';

const STATE_FILTERS = ['open', 'acked', 'resolved'] as const;
const ALERTS_POLL_MS = 30_000;
const CORRELATION_EVENT_TYPES = [
  { value: 'security.event', label: 'Security event' },
  { value: 'events.anomaly', label: 'Anomaly detected' },
  { value: 'rule.triggered', label: 'Detection rule triggered' },
  { value: 'compliance.fired', label: 'Compliance finding' },
  { value: 'health.incident', label: 'Health incident' },
  { value: 'remediation.applied', label: 'Remediation applied' },
] as const;
const CORRELATION_DIMENSIONS = [
  { value: 'node_id', label: 'Host / node' },
  { value: 'tenant_id', label: 'Entire tenant' },
  { value: 'src_ip', label: 'Source IP' },
  { value: 'dst_ip', label: 'Destination IP' },
  { value: 'user_name', label: 'User name' },
  { value: 'correlation_id', label: 'Correlation ID' },
] as const;
const CORRELATION_CONDITION_FIELDS = [
  { value: 'src_ip', label: 'Source IP' }, { value: 'dst_ip', label: 'Destination IP' },
  { value: 'src_port', label: 'Source port' }, { value: 'dst_port', label: 'Destination port' },
  { value: 'protocol', label: 'Protocol' }, { value: 'user_name', label: 'User name' },
  { value: 'auth_result', label: 'Authentication result' }, { value: 'status_code', label: 'HTTP status' },
  { value: 'http_method', label: 'HTTP method' }, { value: 'path', label: 'Request path' },
  { value: 'source', label: 'Event source' }, { value: 'severity', label: 'Event severity' },
  { value: 'direction', label: 'Traffic direction' }, { value: 'bytes_out', label: 'Outbound bytes' },
] as const;
const CORRELATION_OPERATORS = [
  { value: 'eq', label: 'equals' }, { value: 'neq', label: 'does not equal' },
  { value: 'contains', label: 'contains' }, { value: 'gt', label: 'greater than' },
  { value: 'gte', label: 'greater than or equal' }, { value: 'lt', label: 'less than' },
  { value: 'lte', label: 'less than or equal' },
] as const;

const ALERT_DISPOSITION_OPTIONS: Array<{
  value: AlertDispositionValue;
  label: string;
  description: string;
  tone: StateTone;
}> = [
  {
    value: 'resolved',
    label: 'Resolved, containment verified',
    description: 'Control evidence, drift status, and owner decision are sufficient to close this alert.',
    tone: 'healthy',
  },
  {
    value: 'true_positive',
    label: 'True positive, keep active',
    description: 'The signal is real and still needs containment, remediation, or case follow-up.',
    tone: 'critical',
  },
  {
    value: 'false_positive',
    label: 'False positive',
    description: 'The signal does not represent malicious or risky activity after review.',
    tone: 'unknown',
  },
  {
    value: 'benign_positive',
    label: 'Benign positive',
    description: 'The detection was valid, but the activity is expected and authorized.',
    tone: 'info',
  },
  {
    value: 'accepted_risk',
    label: 'Accepted risk',
    description: 'The risk remains open by business decision and needs documented ownership.',
    tone: 'warning',
  },
  {
    value: 'suppressed',
    label: 'Suppressed with expiry',
    description: 'The alert should stop paging for a time-boxed maintenance or exception window.',
    tone: 'warning',
  },
];

interface AlertResolutionFact {
  label: string;
  value: string;
  tone: StateTone;
}

interface AlertResolutionPlan {
  facts: AlertResolutionFact[];
  steps: string[];
  gate: string;
  actions: Array<{ label: string; to: string }>;
  posture: Array<{ mode: string; scope: string; reason: string; equivalent: string; tone: StateTone }>;
}

function severityTone(sev: string | undefined): StateTone {
  switch ((sev ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'degraded';
    case 'medium':
      return 'warning';
    case 'low':
      return 'info';
    case 'info':
      return 'info';
    default:
      return 'unknown';
  }
}

function stateTone(state: string): StateTone {
  switch (state) {
    case 'open':
      return 'critical';
    case 'acked':
      return 'warning';
    case 'resolved':
      return 'healthy';
    default:
      return 'unknown';
  }
}

function dispositionOption(value: string | undefined) {
  return ALERT_DISPOSITION_OPTIONS.find((option) => option.value === value);
}

function dispositionLabel(value: string | undefined): string {
  const known = dispositionOption(value);
  if (known) return known.label;
  return (value ?? '').replace(/_/g, ' ').replace(/\b\w/g, (match) => match.toUpperCase());
}

function dispositionTone(value: string | undefined): StateTone {
  return dispositionOption(value)?.tone ?? 'unknown';
}

export function alertDispositionPill(alert: Alert): { label: string; value: string; tone: StateTone } | null {
  const value = alert.disposition?.value;
  if (!value) return null;
  return {
    label: 'Disposition',
    value: dispositionLabel(value),
    tone: dispositionTone(value),
  };
}

export function alertContextPills(alert: Alert): Array<{ label: string; value: string; tone: StateTone }> {
  const ctx = alert.context ?? {};
  const pills: Array<{ label: string; value: string; tone: StateTone }> = [];
  addContextPill(pills, 'Signal', contextString(ctx, 'event_type'), 'info');
  addContextPill(pills, 'App', contextString(ctx, 'application_name', 'app', 'vhost'), 'healthy');
  addContextPill(pills, 'Parser', contextString(ctx, 'parser_profile'), 'info');
  addContextPill(pills, 'Log', basename(contextString(ctx, 'source_file')), 'unknown');
  addContextPill(pills, 'Group', contextString(ctx, 'server_group'), 'warning');
  const country = contextString(ctx, 'country_code', 'country');
  const asn = contextString(ctx, 'asn');
  addContextPill(pills, 'Origin', [country, asn ? `ASN ${asn}` : ''].filter(Boolean).join(' / '), 'degraded');
  return pills.slice(0, 6);
}

function addContextPill(
  pills: Array<{ label: string; value: string; tone: StateTone }>,
  label: string,
  value: string,
  tone: StateTone,
) {
  if (!value || pills.some((pill) => pill.value === value)) return;
  pills.push({ label, value, tone });
}

function contextString(ctx: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = ctx[key];
    if (typeof value === 'string' && value.trim()) return value.trim();
    if (typeof value === 'number' && Number.isFinite(value)) return String(value);
  }
  return '';
}

function contextBytes(ctx: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = ctx[key];
    if (typeof value === 'number' && Number.isFinite(value)) return formatBytes(value);
    if (typeof value === 'string' && value.trim()) {
      const trimmed = value.trim();
      const numeric = Number(trimmed);
      return Number.isFinite(numeric) ? formatBytes(numeric) : trimmed;
    }
  }
  return '';
}

function basename(path: string): string {
  if (!path) return '';
  return path.split(/[\\/]/).filter(Boolean).pop() ?? path;
}

function errorMessage(error: unknown, fallback: string): string {
  return error instanceof Error && error.message ? error.message : fallback;
}

export function Alerts(): JSX.Element {
  const client = useApiClient();
  const { tenants, currentTenantId, setCurrentTenantId } = useTenant();
  const [pageTab, setPageTab] = useState<PageTab>('alerts');
  const [state, setState] = useState<typeof STATE_FILTERS[number]>('open');
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [loading, setLoading] = useState(false);
  const [alertsError, setAlertsError] = useState<string | null>(null);
  const [alertActionError, setAlertActionError] = useState<string | null>(null);
  const [resolveError, setResolveError] = useState<string | null>(null);
  const [ackingId, setAckingId] = useState<string | null>(null);
  const [resolveTargetId, setResolveTargetId] = useState<string | null>(null);
  const [resolvingAlert, setResolvingAlert] = useState(false);

  // Correlation rules state
  const [rules, setRules] = useState<CorrelationRule[]>([]);
  const [rulesLoading, setRulesLoading] = useState(false);
  const [rulesError, setRulesError] = useState<string | null>(null);
  const [rulesReloadToken, setRulesReloadToken] = useState(0);
  const [deleteRuleId, setDeleteRuleId] = useState<string | null>(null);
  const [deleteRuleError, setDeleteRuleError] = useState<string | null>(null);
  const [deletingRule, setDeletingRule] = useState(false);
  const [showCreateRule, setShowCreateRule] = useState(false);
  const [editRuleId, setEditRuleId] = useState<string | null>(null);
  const [newRuleName, setNewRuleName] = useState('');
  const [newRuleDescription, setNewRuleDescription] = useState('');
  const [newRuleEventType, setNewRuleEventType] = useState('security.event');
  const [newRuleSpecificEventType, setNewRuleSpecificEventType] = useState('');
  const [newRuleWindowSeconds, setNewRuleWindowSeconds] = useState(60);
  const [newRuleThreshold, setNewRuleThreshold] = useState(3);
  const [newRuleGroupBy, setNewRuleGroupBy] = useState<string[]>(['node_id']);
  const [newRuleSeverity, setNewRuleSeverity] = useState('medium');
  const [newRuleEnabled, setNewRuleEnabled] = useState(true);
  const [newRuleSuppressionSeconds, setNewRuleSuppressionSeconds] = useState(300);
  const [newRuleConditions, setNewRuleConditions] = useState<CorrelationCondition[]>([]);
  const [newRuleConditionGroups, setNewRuleConditionGroups] = useState<CorrelationCondition[][]>([]);
  const [newRuleDistinctField, setNewRuleDistinctField] = useState('');
  const [newRuleSequenceEventType, setNewRuleSequenceEventType] = useState('');
  const [newRuleSequenceThreshold, setNewRuleSequenceThreshold] = useState(0);
  const [newRuleSequenceConditions, setNewRuleSequenceConditions] = useState<CorrelationCondition[]>([]);
  const [newRuleAggregateField, setNewRuleAggregateField] = useState('');
  const [newRuleAggregateThreshold, setNewRuleAggregateThreshold] = useState(0);
  const [newRuleTemplateId, setNewRuleTemplateId] = useState('');
  const [creatingRule, setCreatingRule] = useState(false);
  const [createRuleError, setCreateRuleError] = useState<string | null>(null);

  const tenantId = currentTenantId ?? '';

  const refresh = useCallback(async () => {
    if (!tenantId) {
      setAlerts([]);
      setLoading(false);
      setAlertsError(null);
      setAlertActionError(null);
      return;
    }
    setLoading(true);
    try {
      const resp = await client.listAlerts({ tenantId, state, limit: 100, offset: 0 });
      setAlerts(resp.data);
      setAlertsError(null);
    } catch (err) {
      setAlerts([]);
      setAlertsError(errorMessage(err, 'Alert list failed.'));
    } finally {
      setLoading(false);
    }
  }, [client, tenantId, state]);

  useEffect(() => {
    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, ALERTS_POLL_MS);
    return () => window.clearInterval(timer);
  }, [refresh]);

  useEventStream(tenantId, ['alert.opened'], () => refresh());

  const ack = useCallback(async (alert: Alert) => {
    if (ackingId || resolvingAlert) return;
    setAckingId(alert.id);
    setAlertActionError(null);
    try {
      await client.ackAlert(alert.id);
      await refresh();
    } catch (err) {
      setAlertActionError(`Ack failed for ${alert.title}: ${errorMessage(err, 'Ack failed.')}`);
    } finally {
      setAckingId(null);
    }
  }, [ackingId, client, refresh, resolvingAlert]);

  const resolve = async (id: string, payload: UpdateAlertDispositionPayload) => {
    setResolvingAlert(true);
    setResolveError(null);
    setAlertActionError(null);
    try {
      await client.updateAlertDisposition(id, payload);
      setResolveTargetId(null);
      await refresh();
    } catch (err) {
      setResolveError(errorMessage(err, 'Resolve failed.'));
    } finally {
      setResolvingAlert(false);
    }
  };

  // Correlation rules
  useEffect(() => {
    let cancelled = false;
    if (!tenantId) {
      setRules([]);
      setRulesLoading(false);
      setRulesError(null);
      return () => { cancelled = true; };
    }
    setRulesLoading(true);
    setRulesError(null);
    client
      .listCorrelationRules({ tenantId })
      .then((r) => {
        if (!cancelled) {
          setRules(r.data ?? []);
          setRulesError(null);
        }
      })
      .catch((err) => {
        if (!cancelled) {
          setRules([]);
          setRulesError(errorMessage(err, 'Correlation rules failed to load.'));
        }
      })
      .finally(() => { if (!cancelled) setRulesLoading(false); });
    return () => { cancelled = true; };
  }, [client, tenantId, rulesReloadToken]);

  const resetRuleForm = () => {
    setEditRuleId(null);
    setNewRuleName('');
    setNewRuleDescription('');
    setNewRuleEventType('security.event');
    setNewRuleSpecificEventType('');
    setNewRuleWindowSeconds(60);
    setNewRuleThreshold(3);
    setNewRuleGroupBy(['node_id']);
    setNewRuleSeverity('medium');
    setNewRuleEnabled(true);
    setNewRuleSuppressionSeconds(300);
    setNewRuleConditions([]);
    setNewRuleConditionGroups([]);
    setNewRuleDistinctField('');
    setNewRuleSequenceEventType(''); setNewRuleSequenceThreshold(0); setNewRuleSequenceConditions([]);
    setNewRuleAggregateField(''); setNewRuleAggregateThreshold(0);
    setNewRuleTemplateId('');
  };

  const applyRuleTemplate = (templateId: string) => {
    const template = correlationRuleTemplate(templateId);
    if (!template) return;
    setEditRuleId(null);
    setNewRuleTemplateId(template.id);
    setNewRuleName(template.name);
    setNewRuleDescription(template.description);
    setNewRuleEventType(template.eventCategory);
    setNewRuleSpecificEventType(template.eventType);
    setNewRuleWindowSeconds(template.windowSeconds);
    setNewRuleThreshold(template.threshold);
    setNewRuleGroupBy([...template.groupBy]);
    setNewRuleSeverity(template.severity);
    setNewRuleEnabled(false);
    setNewRuleSuppressionSeconds(template.suppressionSeconds);
    setNewRuleConditions(template.conditions.map((condition) => ({ ...condition })));
    setNewRuleConditionGroups(template.conditionGroups.map((group) => group.map((condition) => ({ ...condition }))));
    setNewRuleDistinctField(template.distinctField);
    setNewRuleSequenceEventType(template.sequenceEventType); setNewRuleSequenceThreshold(template.sequenceThreshold);
    setNewRuleSequenceConditions(template.sequenceConditions.map((condition) => ({ ...condition })));
    setNewRuleAggregateField(template.aggregateField); setNewRuleAggregateThreshold(template.aggregateThreshold);
    setShowCreateRule(true);
  };

  const editRule = (rule: CorrelationRule) => {
    setEditRuleId(rule.id);
    setNewRuleName(rule.name);
    setNewRuleDescription(rule.description ?? '');
    setNewRuleEventType(rule.event_types[0] ?? 'security.event');
    setNewRuleSpecificEventType(rule.event_type ?? '');
    setNewRuleWindowSeconds(rule.window_seconds);
    setNewRuleThreshold(rule.threshold);
    setNewRuleGroupBy(rule.group_by?.length ? rule.group_by : [rule.dimension]);
    setNewRuleSeverity(rule.severity);
    setNewRuleEnabled(rule.enabled);
    setNewRuleSuppressionSeconds(rule.suppression_seconds ?? 0);
    setNewRuleConditions(rule.conditions ?? []);
    setNewRuleConditionGroups(rule.condition_groups ?? []);
    setNewRuleDistinctField(rule.distinct_field ?? '');
    setNewRuleSequenceEventType(rule.sequence_event_type ?? ''); setNewRuleSequenceThreshold(rule.sequence_threshold ?? 0);
    setNewRuleSequenceConditions(rule.sequence_conditions ?? []);
    setNewRuleAggregateField(rule.aggregate_field ?? ''); setNewRuleAggregateThreshold(rule.aggregate_threshold ?? 0);
    setShowCreateRule(true);
  };

  const updateCondition = (index: number, patch: Partial<CorrelationCondition>) => {
    setNewRuleConditions((current) => current.map((condition, i) => i === index ? { ...condition, ...patch } : condition));
  };

  const updateConditionGroup = (groupIndex: number, conditionIndex: number, patch: Partial<CorrelationCondition>) => {
    setNewRuleConditionGroups((current) => current.map((group, gi) => gi === groupIndex
      ? group.map((condition, ci) => ci === conditionIndex ? { ...condition, ...patch } : condition)
      : group));
  };

  const updateSequenceCondition = (index: number, patch: Partial<CorrelationCondition>) => {
    setNewRuleSequenceConditions((current) => current.map((condition, i) => i === index ? { ...condition, ...patch } : condition));
  };

  const handleSaveRule = async () => {
    if (!newRuleName.trim() || !tenantId) return;
    setCreatingRule(true);
    setCreateRuleError(null);
    try {
      const payload = {
        tenant_id: tenantId,
        name: newRuleName.trim(),
        description: newRuleDescription.trim() || undefined,
        event_types: [newRuleEventType],
        event_type: newRuleSpecificEventType.trim(),
        window_seconds: newRuleWindowSeconds,
        threshold: newRuleThreshold,
        dimension: newRuleGroupBy[0],
        group_by: newRuleGroupBy,
        suppression_seconds: newRuleSuppressionSeconds,
        conditions: newRuleConditions,
        condition_groups: newRuleConditionGroups,
        distinct_field: newRuleDistinctField,
        sequence_event_type: newRuleSequenceEventType.trim(), sequence_threshold: newRuleSequenceThreshold,
        sequence_conditions: newRuleSequenceConditions, aggregate_field: newRuleAggregateField,
        aggregate_threshold: newRuleAggregateThreshold,
        severity: newRuleSeverity,
        enabled: newRuleEnabled,
      };
      if (editRuleId) await client.updateCorrelationRule(editRuleId, tenantId, payload);
      else await client.createCorrelationRule(payload);
      resetRuleForm();
      setShowCreateRule(false);
      setRulesError(null);
      setRulesReloadToken((n) => n + 1);
    } catch (err) {
      setCreateRuleError(errorMessage(err, 'Create failed.'));
    } finally {
      setCreatingRule(false);
    }
  };

  const toggleRule = async (rule: CorrelationRule) => {
    if (!tenantId) return;
    setCreatingRule(true);
    try {
      await client.updateCorrelationRule(rule.id, tenantId, {
        tenant_id: tenantId, name: rule.name, description: rule.description,
        event_types: rule.event_types, event_type: rule.event_type ?? '',
        window_seconds: rule.window_seconds, threshold: rule.threshold,
        dimension: rule.dimension, group_by: rule.group_by?.length ? rule.group_by : [rule.dimension],
        suppression_seconds: rule.suppression_seconds ?? 0, severity: rule.severity, enabled: !rule.enabled,
        conditions: rule.conditions ?? [],
        condition_groups: rule.condition_groups ?? [], distinct_field: rule.distinct_field ?? '',
        sequence_event_type: rule.sequence_event_type ?? '', sequence_threshold: rule.sequence_threshold ?? 0,
        sequence_conditions: rule.sequence_conditions ?? [], aggregate_field: rule.aggregate_field ?? '',
        aggregate_threshold: rule.aggregate_threshold ?? 0,
      });
      setRulesReloadToken((n) => n + 1);
      setRulesError(null);
    } catch (err) {
      setRulesError(err instanceof Error ? err.message : 'rule update failed');
    } finally { setCreatingRule(false); }
  };

  const handleDeleteRule = async () => {
    if (!deleteRuleId || !tenantId) return;
    setDeletingRule(true);
    setDeleteRuleError(null);
    try {
      await client.deleteCorrelationRule(deleteRuleId, tenantId);
      setDeleteRuleId(null);
      setRulesReloadToken((n) => n + 1);
    } catch (err) {
      setDeleteRuleError(errorMessage(err, 'Delete failed.'));
    } finally {
      setDeletingRule(false);
    }
  };

  const counts = useMemo(() => {
    const c = { critical: 0, high: 0, total: alerts.length };
    for (const a of alerts) {
      const sev = (a.severity ?? '').toLowerCase();
      if (sev === 'critical') c.critical += 1;
      else if (sev === 'high') c.high += 1;
    }
    return c;
  }, [alerts]);
  const resolveTarget = useMemo(
    () => alerts.find((alert) => alert.id === resolveTargetId) ?? null,
    [alerts, resolveTargetId],
  );
  const deleteRule = useMemo(
    () => rules.find((rule) => rule.id === deleteRuleId) ?? null,
    [deleteRuleId, rules],
  );

  const columns = useMemo<ColumnDef<Alert>[]>(() => [
    {
      accessorKey: 'severity',
      header: 'Severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)} className="font-mono uppercase">
          {row.original.severity || 'unknown'}
        </StatusTag>
      ),
    },
    {
      id: 'title',
      header: 'Title',
      cell: ({ row }) => {
        const pills = alertContextPills(row.original);
        return (
          <div className="flex flex-col gap-1">
            <span className="font-medium text-foreground">{row.original.title}</span>
            {row.original.summary ? (
              <span className="text-xs text-text-muted">{row.original.summary}</span>
            ) : null}
            {pills.length > 0 ? (
              <div className="flex flex-wrap gap-1">
                {pills.map((pill) => (
                  <StatusTag key={`${pill.label}:${pill.value}`} tone={pill.tone} className="max-w-[220px] truncate">
                    {pill.label}: {pill.value}
                  </StatusTag>
                ))}
              </div>
            ) : null}
          </div>
        );
      },
    },
    {
      accessorKey: 'source',
      header: 'Source',
      cell: ({ getValue }) => {
        const v = getValue() as string;
        const det = v ? classifyValue(v) : { type: 'unknown' as const };
        return det.type !== 'unknown' ? (
          <EntityChip type={det.type} value={v} />
        ) : (
          <span className="font-mono text-xs text-text-secondary">{v || '-'}</span>
        );
      },
    },
    {
      accessorKey: 'opened_at',
      header: 'Opened',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums text-text-secondary">
          {new Date(getValue() as string).toLocaleString()}
        </span>
      ),
    },
    {
      accessorKey: 'state',
      header: 'State',
      cell: ({ row, getValue }) => {
        const s = getValue() as string;
        const disposition = alertDispositionPill(row.original);
        return (
          <div className="flex flex-col items-start gap-1">
            <StatusTag tone={stateTone(s)}>{s}</StatusTag>
            {disposition ? <StatusTag tone={disposition.tone}>{disposition.value}</StatusTag> : null}
          </div>
        );
      },
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex items-center gap-2">
          {row.original.state === 'open' ? (
            <Button
              variant="secondary"
              size="sm"
              onClick={() => ack(row.original)}
              aria-label={`Acknowledge alert ${row.original.title}`}
              loading={ackingId === row.original.id}
              disabled={resolvingAlert || (ackingId !== null && ackingId !== row.original.id)}
            >
              Ack
            </Button>
          ) : null}
          {row.original.state !== 'resolved' ? (
            <Button
              variant="primary"
              size="sm"
              onClick={() => {
                setResolveError(null);
                setResolveTargetId(row.original.id);
              }}
              aria-label={`Review alert ${row.original.title}`}
              disabled={ackingId !== null || resolvingAlert}
            >
              Review
            </Button>
          ) : null}
        </div>
      ),
    },
  ], [ack, ackingId, resolvingAlert]);

  const ruleColumns: ColumnDef<CorrelationRule>[] = [
    {
      header: 'Name',
      accessorKey: 'name',
      cell: ({ row }) => <span className="font-medium text-foreground">{row.original.name}</span>,
    },
    {
      header: 'Event category',
      id: 'event_types',
      cell: ({ row }) => (
        <span className="font-mono text-xs text-text-muted">
          {(row.original.event_types ?? []).join(', ') || 'All subscribed streams'}
        </span>
      ),
    },
    {
      header: 'Trigger',
      id: 'trigger',
      cell: ({ row }) => (
        <span className="text-xs text-text-secondary">
          {row.original.threshold} event{row.original.threshold === 1 ? '' : 's'} / {row.original.window_seconds}s
          <span className="block font-mono text-text-muted">{row.original.event_type || 'any type'}</span>
          <span className="block font-mono text-text-muted">by {(row.original.group_by?.length ? row.original.group_by : [row.original.dimension]).join(' + ')}</span>
          <span className="block text-text-muted">suppress {row.original.suppression_seconds ?? 0}s</span>
          {(Array.isArray(row.original.conditions) ? row.original.conditions : []).map((condition, index) => (
            <span className="block font-mono text-text-muted" key={`${condition.field}-${index}`}>
              {condition.field} {condition.operator} {String(condition.value)}
            </span>
          ))}
          {(Array.isArray(row.original.condition_groups) ? row.original.condition_groups : []).map((group, groupIndex) => (
            <span className="block font-mono text-text-muted" key={`group-${groupIndex}`}>
              OR {group.map((condition) => `${condition.field} ${condition.operator} ${String(condition.value)}`).join(' AND ')}
            </span>
          ))}
          {row.original.distinct_field ? <span className="block text-text-muted">count distinct {row.original.distinct_field}</span> : null}
          {row.original.sequence_event_type ? <span className="block text-text-muted">after {row.original.sequence_threshold} × {row.original.sequence_event_type}</span> : null}
          {row.original.aggregate_field ? <span className="block text-text-muted">sum {row.original.aggregate_field} ≥ {row.original.aggregate_threshold}</span> : null}
        </span>
      ),
    },
    {
      header: 'Severity',
      accessorKey: 'severity',
      cell: ({ row }) => (
        <StatusTag tone={severityTone(row.original.severity)} className="uppercase">
          {row.original.severity}
        </StatusTag>
      ),
    },
    {
      header: 'Enabled',
      accessorKey: 'enabled',
      cell: ({ row }) =>
        row.original.enabled ? (
          <StatusTag tone="healthy">On</StatusTag>
        ) : (
          <StatusTag tone="unknown">Off</StatusTag>
        ),
    },
    {
      header: 'Created',
      accessorKey: 'created_at',
      cell: ({ row }) => (
        <span className="font-mono text-xs text-text-muted">
          {new Date(row.original.created_at).toLocaleDateString()}
        </span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <div className="flex gap-1">
          <Button variant="ghost" size="sm" onClick={() => editRule(row.original)}>View / edit</Button>
          <Button variant="ghost" size="sm" disabled={creatingRule} onClick={() => void toggleRule(row.original)}>
            {row.original.enabled ? 'Disable' : 'Enable'}
          </Button>
          <Button
            variant="ghost"
            size="sm"
            onClick={() => {
              setDeleteRuleError(null);
              setDeleteRuleId(row.original.id);
            }}
            aria-label={`Delete correlation rule ${row.original.name}`}
            disabled={deletingRule}
          >
            <Trash2 className="h-3.5 w-3.5 text-state-critical" />
          </Button>
        </div>
      ),
    },
  ];

  const allClear = !loading && alerts.length === 0 && state === 'open';

  const tabClass = (t: PageTab) =>
    `px-4 py-2 text-sm font-medium rounded-t-md transition-colors ${
      pageTab === t
        ? 'bg-surface text-foreground border border-border-subtle border-b-surface'
        : 'text-text-secondary hover:text-foreground'
    }`;

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="VISIBILITY / ALERTS"
        title="Alerts"
        description="Deduped inbox from correlation, rules, and compliance."
        actions={
          <Button variant="secondary" size="md" onClick={refresh} disabled={loading}>
            <RefreshCw className="h-4 w-4" /> Refresh
          </Button>
        }
      />

      <div className="flex gap-1 border-b border-border-subtle">
        <button type="button" className={tabClass('alerts')} onClick={() => setPageTab('alerts')}>
          Inbox
          {counts.total > 0 && (
            <span className="ml-1.5 rounded-full bg-state-critical/20 px-1.5 py-0.5 text-xs text-state-critical">
              {counts.total}
            </span>
          )}
        </button>
        <button type="button" className={tabClass('rules')} onClick={() => setPageTab('rules')}>
          Correlation rules
        </button>
      </div>

      {pageTab === 'alerts' && (
        <>
          <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
            <KpiTile label="OPEN ALERTS" value={counts.total} tone={counts.total > 0 ? 'warning' : 'healthy'} />
            <KpiTile label="CRITICAL" value={counts.critical} tone={counts.critical > 0 ? 'critical' : 'healthy'} />
            <KpiTile label="HIGH" value={counts.high} tone={counts.high > 0 ? 'degraded' : 'healthy'} />
          </div>

          <CriticalResponseCenter
            alerts={alerts}
            onOpenResolve={(id) => setResolveTargetId(id)}
          />

          {alerts.length > 0 && (
            <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
              <Panel padding="md" eyebrow="ALERT VOLUME" title="Opened per day (last 7 days)" toneAccent="critical">
                <Chart
                  kind="bar"
                  height={160}
                  ariaLabel="Alert volume by day"
                  data={(() => {
                    const buckets: Record<string, number> = {};
                    const now = Date.now();
                    for (let i = 6; i >= 0; i--) {
                      const d = new Date(now - i * 86_400_000);
                      buckets[d.toISOString().slice(5, 10)] = 0;
                    }
                    for (const a of alerts) {
                      const k = (a.opened_at ?? '').slice(5, 10);
                      if (k in buckets) buckets[k] = (buckets[k] ?? 0) + 1;
                    }
                    return {
                      labels: Object.keys(buckets),
                      datasets: [{
                        label: 'Alerts',
                        data: Object.values(buckets),
                        backgroundColor: 'rgba(239,68,68,0.65)',
                        borderColor: 'rgba(239,68,68,1)',
                        borderWidth: 1,
                      }],
                    };
                  })()}
                />
              </Panel>
              <Panel padding="md" eyebrow="SEVERITY MIX" title="Distribution" toneAccent="warning">
                <Chart
                  kind="doughnut"
                  height={160}
                  ariaLabel="Alert severity distribution"
                  data={{
                    labels: ['Critical', 'High', 'Medium', 'Low', 'Info'],
                    datasets: [{
                      data: (() => {
                        const c = { critical: 0, high: 0, medium: 0, low: 0, info: 0 };
                        for (const a of alerts) {
                          const s = (a.severity ?? 'info').toLowerCase() as keyof typeof c;
                          if (s in c) c[s]++;
                        }
                        return [c.critical, c.high, c.medium, c.low, c.info];
                      })(),
                      backgroundColor: [
                        'rgba(239,68,68,0.85)',
                        'rgba(245,158,11,0.85)',
                        'rgba(99,102,241,0.75)',
                        'rgba(100,116,139,0.65)',
                        'rgba(148,163,184,0.55)',
                      ],
                      borderWidth: 0,
                    }],
                  }}
                />
              </Panel>
            </div>
          )}

          <Panel padding="md" eyebrow="FILTERS" title="Refine">
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-3">
              <FilterSelect
                label="Tenant"
                value={tenantId}
                onChange={(v) => setCurrentTenantId(v)}
                options={tenants.map((t) => ({ label: t.name, value: t.id }))}
              />
              <FilterSelect
                label="State"
                value={state}
                onChange={(v) => setState(v as typeof STATE_FILTERS[number])}
                options={STATE_FILTERS.map((s) => ({ label: s, value: s }))}
              />
            </div>
          </Panel>

          {alertsError && (
            <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Alert data unavailable">
              <p className="text-sm text-state-critical" role="alert">{alertsError}</p>
            </Panel>
          )}

          {alertActionError && (
            <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Alert action failed">
              <p className="text-sm text-state-critical" role="alert">{alertActionError}</p>
            </Panel>
          )}

          <Panel padding="sm" tone="inset" eyebrow={`ALERTS / ${alerts.length}`} title="Inbox">
            <DataTable
              columns={columns}
              rows={alerts}
              rowKey={(r) => r.id}
              loading={loading}
              compact
              empty={
                alertsError ? (
                  <EmptyState
                    icon={<Bell />}
                    title="Alerts could not be loaded"
                    description="Resolve the error above and refresh."
                  />
                ) : allClear ? (
                  <EmptyState
                    tone="success"
                    icon={<ShieldCheck />}
                    title="All clear"
                    description="No open alerts. Detection rules are healthy and the inbox is empty."
                  />
                ) : (
                  <EmptyState
                    icon={<Bell />}
                    title="No alerts"
                    description={`No alerts in state "${state}".`}
                  />
                )
              }
            />
          </Panel>
        </>
      )}

      {pageTab === 'rules' && (
        <Panel
          padding="md"
          eyebrow="CORRELATION RULES"
          title="Detection rules"
          actions={
            <div className="flex flex-wrap items-center gap-2">
              <select
                aria-label="Create rule from template"
                className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                value={newRuleTemplateId}
                onChange={(event) => applyRuleTemplate(event.target.value)}
              >
                <option value="">Create from template…</option>
                {CORRELATION_RULE_TEMPLATES.map((template) => (
                  <option key={template.id} value={template.id}>{template.name}</option>
                ))}
              </select>
              <Button variant="primary" size="sm" onClick={() => { resetRuleForm(); setCreateRuleError(null); setShowCreateRule(true); }}>
                <Plus className="h-3.5 w-3.5" /> New rule
              </Button>
            </div>
          }
        >
          {showCreateRule && (
            <div className="mb-4 rounded-md border border-border-subtle bg-elevated p-4">
              <p className="mb-3 text-sm font-medium text-foreground">{editRuleId ? 'View or edit correlation rule' : 'New correlation rule'}</p>
              {newRuleTemplateId ? (
                <div className="mb-3 rounded-md border border-brand-500/30 bg-brand-500/5 px-3 py-2 text-xs text-text-secondary">
                  Template loaded. Review every value, choose whether to enable the rule, then create it.
                </div>
              ) : null}
              <div className="grid gap-3 md:grid-cols-2 xl:grid-cols-3">
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-name">Name</Label>
                  <Input
                    id="rule-name"
                    value={newRuleName}
                    onChange={(e) => {
                      setNewRuleName(e.target.value);
                      if (createRuleError) setCreateRuleError(null);
                    }}
                    placeholder="Brute-force SSH"
                    className="h-8"
                  />
                </div>
                <div className="flex flex-col gap-1 md:col-span-2">
                  <Label htmlFor="rule-description">Description</Label>
                  <Input
                    id="rule-description"
                    value={newRuleDescription}
                    onChange={(e) => setNewRuleDescription(e.target.value)}
                    placeholder="Alert after repeated events in the selected window"
                    className="h-8"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-event-type">Event category</Label>
                  <select
                    id="rule-event-type"
                    className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                    value={newRuleEventType}
                    onChange={(e) => setNewRuleEventType(e.target.value)}
                  >
                    {CORRELATION_EVENT_TYPES.map((eventType) => (
                      <option key={eventType.value} value={eventType.value}>{eventType.label}</option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-threshold">Event threshold</Label>
                  <Input
                    id="rule-threshold"
                    type="number"
                    min={1}
                    value={newRuleThreshold}
                    onChange={(e) => setNewRuleThreshold(Math.max(1, Number(e.target.value) || 1))}
                    className="h-8"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-specific-event-type">Specific event type</Label>
                  <Input
                    id="rule-specific-event-type"
                    value={newRuleSpecificEventType}
                    onChange={(e) => setNewRuleSpecificEventType(e.target.value)}
                    placeholder="ssh.authentication_failure (blank matches any)"
                    className="h-8"
                  />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-window">Time window (seconds)</Label>
                  <Input
                    id="rule-window"
                    type="number"
                    min={1}
                    value={newRuleWindowSeconds}
                    onChange={(e) => setNewRuleWindowSeconds(Math.max(1, Number(e.target.value) || 1))}
                    className="h-8"
                  />
                </div>
                <div className="flex flex-col gap-2 md:col-span-2 xl:col-span-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <Label>Field conditions</Label>
                      <p className="text-xs text-text-muted">Every condition must match before an event is counted.</p>
                    </div>
                    <Button
                      variant="secondary"
                      size="sm"
                      disabled={newRuleConditions.length >= 10}
                      onClick={() => setNewRuleConditions((current) => [...current, { field: 'dst_port', operator: 'eq', value: '22' }])}
                    >
                      <Plus className="h-3.5 w-3.5" /> Add condition
                    </Button>
                  </div>
                  {newRuleConditions.map((condition, index) => (
                    <div className="grid gap-2 md:grid-cols-[1fr_1fr_1fr_auto]" key={index}>
                      <select
                        aria-label={`Condition ${index + 1} field`}
                        className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                        value={condition.field}
                        onChange={(e) => updateCondition(index, { field: e.target.value })}
                      >
                        {CORRELATION_CONDITION_FIELDS.map((field) => <option key={field.value} value={field.value}>{field.label}</option>)}
                      </select>
                      <select
                        aria-label={`Condition ${index + 1} operator`}
                        className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                        value={condition.operator}
                        onChange={(e) => updateCondition(index, { operator: e.target.value as CorrelationCondition['operator'] })}
                      >
                        {CORRELATION_OPERATORS.map((operator) => <option key={operator.value} value={operator.value}>{operator.label}</option>)}
                      </select>
                      <Input
                        aria-label={`Condition ${index + 1} value`}
                        className="h-8"
                        value={String(condition.value)}
                        onChange={(e) => updateCondition(index, { value: e.target.value })}
                      />
                      <Button
                        variant="ghost"
                        size="sm"
                        aria-label={`Remove condition ${index + 1}`}
                        onClick={() => setNewRuleConditions((current) => current.filter((_, i) => i !== index))}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  ))}
                </div>
                <div className="flex flex-col gap-2 md:col-span-2 xl:col-span-3">
                  <div className="flex items-center justify-between">
                    <div>
                      <Label>Alternative condition groups</Label>
                      <p className="text-xs text-text-muted">At least one group must match. Conditions inside a group must all match.</p>
                    </div>
                    <Button variant="secondary" size="sm" disabled={newRuleConditionGroups.length >= 10}
                      onClick={() => setNewRuleConditionGroups((current) => [...current, [{ field: 'dst_port', operator: 'eq', value: '80' }]])}>
                      <Plus className="h-3.5 w-3.5" /> Add OR group
                    </Button>
                  </div>
                  {newRuleConditionGroups.map((group, groupIndex) => (
                    <div className="rounded-md border border-border-subtle p-3" key={groupIndex}>
                      <div className="mb-2 flex items-center justify-between">
                        <span className="text-xs font-medium text-text-secondary">Group {groupIndex + 1}</span>
                        <div className="flex gap-1">
                          <Button variant="ghost" size="sm" disabled={group.length >= 10}
                            onClick={() => setNewRuleConditionGroups((current) => current.map((item, index) => index === groupIndex ? [...item, { field: 'dst_port', operator: 'eq', value: '80' }] : item))}>
                            <Plus className="h-3.5 w-3.5" /> Add AND condition
                          </Button>
                          <Button variant="ghost" size="sm" aria-label={`Remove condition group ${groupIndex + 1}`}
                            onClick={() => setNewRuleConditionGroups((current) => current.filter((_, index) => index !== groupIndex))}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </div>
                      {group.map((condition, conditionIndex) => (
                        <div className="mb-2 grid gap-2 md:grid-cols-[1fr_1fr_1fr_auto]" key={conditionIndex}>
                          <select aria-label={`Group ${groupIndex + 1} condition ${conditionIndex + 1} field`} className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground" value={condition.field} onChange={(e) => updateConditionGroup(groupIndex, conditionIndex, { field: e.target.value })}>
                            {CORRELATION_CONDITION_FIELDS.map((field) => <option key={field.value} value={field.value}>{field.label}</option>)}
                          </select>
                          <select aria-label={`Group ${groupIndex + 1} condition ${conditionIndex + 1} operator`} className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground" value={condition.operator} onChange={(e) => updateConditionGroup(groupIndex, conditionIndex, { operator: e.target.value as CorrelationCondition['operator'] })}>
                            {CORRELATION_OPERATORS.map((operator) => <option key={operator.value} value={operator.value}>{operator.label}</option>)}
                          </select>
                          <Input aria-label={`Group ${groupIndex + 1} condition ${conditionIndex + 1} value`} className="h-8" value={String(condition.value)} onChange={(e) => updateConditionGroup(groupIndex, conditionIndex, { value: e.target.value })} />
                          <Button variant="ghost" size="sm" aria-label={`Remove group ${groupIndex + 1} condition ${conditionIndex + 1}`} disabled={group.length === 1}
                            onClick={() => setNewRuleConditionGroups((current) => current.map((item, index) => index === groupIndex ? item.filter((_, ci) => ci !== conditionIndex) : item))}>
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      ))}
                    </div>
                  ))}
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-distinct-field">Count distinct values</Label>
                  <select id="rule-distinct-field" className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground" value={newRuleDistinctField} onChange={(e) => setNewRuleDistinctField(e.target.value)}>
                    <option value="">Count every matching event</option>
                    {CORRELATION_CONDITION_FIELDS.map((field) => <option key={field.value} value={field.value}>{field.label}</option>)}
                  </select>
                  <span className="text-xs text-text-muted">Use this for scans or attempts across multiple accounts.</span>
                </div>
                <div className="flex flex-col gap-2 md:col-span-2 xl:col-span-3">
                  <div>
                    <Label>Prerequisite event sequence</Label>
                    <p className="text-xs text-text-muted">Require earlier matching events in the same time window and group.</p>
                  </div>
                  <div className="grid gap-2 md:grid-cols-[2fr_1fr_auto]">
                    <Input aria-label="Prerequisite event type" className="h-8" value={newRuleSequenceEventType} placeholder="authentication.failure (optional)" onChange={(e) => setNewRuleSequenceEventType(e.target.value)} />
                    <Input aria-label="Prerequisite event threshold" className="h-8" type="number" min={0} value={newRuleSequenceThreshold} onChange={(e) => setNewRuleSequenceThreshold(Math.max(0, Number(e.target.value) || 0))} />
                    <Button variant="secondary" size="sm" disabled={!newRuleSequenceEventType.trim() || newRuleSequenceConditions.length >= 10} onClick={() => setNewRuleSequenceConditions((current) => [...current, { field: 'auth_result', operator: 'eq', value: 'failure' }])}>
                      <Plus className="h-3.5 w-3.5" /> Add prerequisite condition
                    </Button>
                  </div>
                  {newRuleSequenceConditions.map((condition, index) => (
                    <div className="grid gap-2 md:grid-cols-[1fr_1fr_1fr_auto]" key={index}>
                      <select aria-label={`Prerequisite condition ${index + 1} field`} className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground" value={condition.field} onChange={(e) => updateSequenceCondition(index, { field: e.target.value })}>
                        {CORRELATION_CONDITION_FIELDS.map((field) => <option key={field.value} value={field.value}>{field.label}</option>)}
                      </select>
                      <select aria-label={`Prerequisite condition ${index + 1} operator`} className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground" value={condition.operator} onChange={(e) => updateSequenceCondition(index, { operator: e.target.value as CorrelationCondition['operator'] })}>
                        {CORRELATION_OPERATORS.map((operator) => <option key={operator.value} value={operator.value}>{operator.label}</option>)}
                      </select>
                      <Input aria-label={`Prerequisite condition ${index + 1} value`} className="h-8" value={String(condition.value)} onChange={(e) => updateSequenceCondition(index, { value: e.target.value })} />
                      <Button variant="ghost" size="sm" aria-label={`Remove prerequisite condition ${index + 1}`} onClick={() => setNewRuleSequenceConditions((current) => current.filter((_, i) => i !== index))}><Trash2 className="h-3.5 w-3.5" /></Button>
                    </div>
                  ))}
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-aggregate-field">Aggregate numeric field</Label>
                  <select id="rule-aggregate-field" className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground" value={newRuleAggregateField} onChange={(e) => setNewRuleAggregateField(e.target.value)}>
                    <option value="">No aggregate</option><option value="bytes_out">Outbound bytes</option><option value="bytes_in">Inbound bytes</option>
                  </select>
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-aggregate-threshold">Aggregate threshold</Label>
                  <Input id="rule-aggregate-threshold" className="h-8" type="number" min={0} value={newRuleAggregateThreshold} onChange={(e) => setNewRuleAggregateThreshold(Math.max(0, Number(e.target.value) || 0))} />
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-dimension">Group events by</Label>
                  <select multiple
                    id="rule-dimension"
                    className="min-h-20 rounded-md border border-border-subtle bg-surface px-2 py-1 text-sm text-foreground"
                    value={newRuleGroupBy}
                    onChange={(e) => setNewRuleGroupBy(Array.from(e.target.selectedOptions, (option) => option.value).slice(0, 3))}
                  >
                    {CORRELATION_DIMENSIONS.map((dimension) => (
                      <option key={dimension.value} value={dimension.value}>{dimension.label}</option>
                    ))}
                  </select>
                  <span className="text-xs text-text-muted">Select up to three fields. Use Ctrl/Cmd to select multiple.</span>
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-severity">Severity</Label>
                  <select
                    id="rule-severity"
                    className="h-8 rounded-md border border-border-subtle bg-surface px-2 text-sm text-foreground"
                    value={newRuleSeverity}
                    onChange={(e) => setNewRuleSeverity(e.target.value)}
                  >
                    {['low', 'medium', 'high', 'critical'].map((s) => (
                      <option key={s} value={s}>{s}</option>
                    ))}
                  </select>
                </div>
                <div className="flex flex-col gap-1">
                  <Label htmlFor="rule-suppression">Notification suppression (seconds)</Label>
                  <Input
                    id="rule-suppression"
                    type="number"
                    min={0}
                    value={newRuleSuppressionSeconds}
                    onChange={(e) => setNewRuleSuppressionSeconds(Math.max(0, Number(e.target.value) || 0))}
                    className="h-8"
                  />
                </div>
                <label className="flex items-center gap-2 text-sm text-foreground">
                  <input type="checkbox" checked={newRuleEnabled} onChange={(e) => setNewRuleEnabled(e.target.checked)} />
                  Enabled
                </label>
                <div className="flex items-end gap-2">
                  <Button variant="primary" size="sm" onClick={handleSaveRule} disabled={creatingRule || !newRuleName.trim() || !tenantId || newRuleGroupBy.length === 0}>
                    {creatingRule ? 'Saving...' : editRuleId ? 'Save changes' : 'Create'}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => { resetRuleForm(); setCreateRuleError(null); setShowCreateRule(false); }} disabled={creatingRule}>
                    Cancel
                  </Button>
                </div>
              </div>
              {createRuleError ? (
                <p className="mt-3 rounded-md border border-state-critical/40 bg-state-critical/10 px-3 py-2 text-sm text-state-critical" role="alert">
                  Correlation rule create failed: {createRuleError}
                </p>
              ) : null}
              {!tenantId && (
                <p className="mt-2 text-xs text-state-warning">Select a tenant in the Inbox tab first.</p>
              )}
            </div>
          )}

          {rulesError ? (
            <Panel padding="md" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Correlation rules unavailable">
              <p className="text-sm text-state-critical" role="alert">{rulesError}</p>
            </Panel>
          ) : null}

          <DataTable
            columns={ruleColumns}
            rows={rules}
            rowKey={(r) => r.id}
            loading={rulesLoading}
            empty={
              rulesError ? (
                <EmptyState
                  icon={<ShieldCheck />}
                  title="Correlation rules could not be loaded"
                  description="Resolve the error above and refresh."
                />
              ) : (
                <EmptyState
                  icon={<ShieldCheck />}
                  title="No correlation rules"
                  description="Create a rule to start correlating events into alerts."
                />
              )
            }
          />
        </Panel>
      )}

      <ConfirmModal
        open={deleteRuleId !== null}
        title="Delete correlation rule?"
        body={`This will stop ${deleteRule?.name ?? 'this rule'} from generating alerts immediately.`}
        confirmLabel={deletingRule ? 'Deleting...' : 'Delete'}
        confirmDisabled={deletingRule}
        cancelDisabled={deletingRule}
        variant="danger"
        onConfirm={handleDeleteRule}
        onCancel={() => {
          setDeleteRuleId(null);
          setDeleteRuleError(null);
        }}
      >
        {deleteRuleError ? (
          <p className="rounded-md border border-state-critical/40 bg-state-critical/10 px-3 py-2 text-sm text-state-critical" role="alert">
            Correlation rule delete failed: {deleteRuleError}
          </p>
        ) : null}
      </ConfirmModal>

      <ResolveAlertModal
        alert={resolveTarget}
        open={resolveTargetId !== null && resolveTarget !== null}
        resolving={resolvingAlert}
        error={resolveError}
        onConfirm={(payload) => { if (resolveTarget) void resolve(resolveTarget.id, payload); }}
        onCancel={() => {
          setResolveTargetId(null);
          setResolveError(null);
        }}
        onActionTaken={() => { void refresh(); }}
      />
    </div>
  );
}

function CriticalResponseCenter({
  alerts,
  onOpenResolve,
}: {
  alerts: Alert[];
  onOpenResolve: (id: string) => void;
}) {
  const critical = alerts.filter((alert) => (alert.severity ?? '').toLowerCase() === 'critical');
  const groups = criticalAlertGroups(critical);
  if (critical.length === 0 || groups.length === 0) return null;

  return (
    <Panel
      padding="md"
      eyebrow="SMART RESPONSE"
      title="Critical response center"
      toneAccent="critical"
      actions={
        <Button asChild variant="outline" size="sm">
          <Link to="/control-room">
            Control Room
            <ArrowRight />
          </Link>
        </Button>
      }
    >
      <div className="mb-4 grid gap-3 lg:grid-cols-[1.1fr_0.9fr]">
        <div className="rounded-lg border border-state-critical/25 bg-state-critical/5 p-3">
          <div className="flex items-start gap-2">
            <AlertTriangle className="mt-0.5 h-4 w-4 text-state-critical" />
            <div>
              <p className="text-sm font-semibold text-foreground">
                {critical.length} critical alert{critical.length === 1 ? '' : 's'} need a containment decision.
              </p>
              <p className="mt-1 text-sm text-text-secondary">
                Treat 100% confidence signals as action-ready: contain first, then relax only when audit,
                remediation, and drift evidence show the affected scope is clean.
              </p>
            </div>
          </div>
        </div>
        <div className="rounded-lg border border-border-subtle bg-surface p-3">
          <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
            Posture-template target
          </p>
          <p className="mt-1 text-sm text-text-secondary">
            Use posture-template semantics: TTL emergency override, explicit ingress/egress policy, Control One/DNS/NTP/update allowlists,
            canary rollout, rollback, and drift verification per node.
          </p>
        </div>
      </div>

      <div className="grid grid-cols-1 gap-3 xl:grid-cols-3">
        {groups.map((group) => (
          <div key={group.category} className="flex flex-col gap-3 rounded-lg border border-border-subtle bg-elevated p-3">
            <div className="flex flex-wrap items-center justify-between gap-2">
              <StatusTag tone="critical">{group.count} critical</StatusTag>
              <StatusTag tone={group.tone}>{group.label}</StatusTag>
            </div>
            <div>
              <p className="text-sm font-medium text-foreground">{group.immediate}</p>
              <p className="mt-1 text-sm text-text-secondary">{group.guidance}</p>
            </div>
            <div className="rounded-md border border-border-subtle bg-surface px-3 py-2">
              <p className="font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">Recommended posture</p>
              <p className="mt-1 text-sm text-foreground">{group.postureMode}</p>
              <p className="mt-1 text-xs text-text-muted">{group.postureReason}</p>
            </div>
            <div className="flex flex-wrap gap-1.5">
              {group.ips.map((ip) => (
                <StatusTag key={ip} tone="critical" className="font-mono">{ip}</StatusTag>
              ))}
              {group.scope ? <StatusTag tone="info">{group.scope}</StatusTag> : null}
            </div>
            <div className="mt-auto grid grid-cols-1 gap-2 sm:grid-cols-2">
              <Button type="button" variant="primary" size="sm" onClick={() => onOpenResolve(group.alert.id)}>
                Review top alert
              </Button>
              <Button asChild variant="outline" size="sm">
                <Link to={group.primaryAction.to}>
                  {group.primaryAction.label}
                  <ArrowRight />
                </Link>
              </Button>
            </div>
          </div>
        ))}
      </div>
    </Panel>
  );
}

interface CriticalResponseGroup {
  category: string;
  label: string;
  count: number;
  alert: Alert;
  ips: string[];
  scope: string;
  immediate: string;
  guidance: string;
  postureMode: string;
  postureReason: string;
  primaryAction: { label: string; to: string };
  tone: StateTone;
}

function criticalAlertGroups(alerts: Alert[]): CriticalResponseGroup[] {
  const grouped = new Map<string, Alert[]>();
  for (const alert of alerts) {
    const category = alertCategory(alert);
    grouped.set(category, [...(grouped.get(category) ?? []), alert]);
  }

  return Array.from(grouped.entries())
    .map(([category, rows]) => {
      const alert = [...rows].sort((a, b) => Date.parse(b.opened_at ?? '') - Date.parse(a.opened_at ?? ''))[0] ?? rows[0];
      const playbook = criticalPlaybook(category, alert);
      const posture = postureRecommendations(alert, category, alertScope(alert), alertSourceIP(alert))[0];
      return {
        category,
        label: titleCase(category),
        count: rows.length,
        alert,
        ips: uniqueStrings(rows.map(alertSourceIP).filter(Boolean)).slice(0, 4),
        scope: alertScope(alert),
        immediate: playbook.immediate,
        guidance: playbook.guidance,
        postureMode: posture?.mode ?? 'moderate containment',
        postureReason: posture?.reason ?? 'Apply the narrowest containment action that preserves audit and rollback evidence.',
        primaryAction: playbook.primaryAction,
        tone: playbook.tone,
      };
    })
    .sort((a, b) => b.count - a.count || a.label.localeCompare(b.label))
    .slice(0, 6);
}

function criticalPlaybook(category: string, alert: Alert): {
  immediate: string;
  guidance: string;
  primaryAction: { label: string; to: string };
  tone: StateTone;
} {
  const ip = alertSourceIP(alert);
  switch (category) {
    case 'exfiltration':
      return {
        immediate: 'Contain egress before inbox cleanup.',
        guidance: 'Move the affected scope to update-only or full lockdown, verify outbound destinations, and preserve bytes-out evidence.',
        primaryAction: { label: 'Egress behavior', to: '/security/network?tab=ip-behavior' },
        tone: 'critical',
      };
    case 'credential':
      return {
        immediate: 'Stop authentication pressure and prove no session landed.',
        guidance: 'Block the source, inspect successful sessions after the failure window, and rotate credentials when exposure is plausible.',
        primaryAction: { label: 'Access review', to: '/access' },
        tone: 'critical',
      };
    case 'exploit':
    case 'scanner':
      return {
        immediate: 'Protect the public listener now.',
        guidance: 'Block the source, enable webserver enforcement or default-deny ingress, and patch exposed paths before resolving.',
        primaryAction: { label: 'Webserver controls', to: '/security/webservers' },
        tone: 'critical',
      };
    case 'malware':
      return {
        immediate: 'Assume host compromise until disproven.',
        guidance: 'Airgap the node with a TTL, verify agent/service integrity, and preserve control-plane audit evidence.',
        primaryAction: { label: 'Affected servers', to: '/nodes' },
        tone: 'critical',
      };
    case 'exposure':
      return {
        immediate: 'Reduce exposure confidence gaps first.',
        guidance: 'Review public listeners, protect them with default-deny or whitelist-only posture, and verify drift per node.',
        primaryAction: { label: 'Exposure path', to: '/control-room/exposure' },
        tone: 'warning',
      };
    case 'data':
      return {
        immediate: 'Contain data movement and protect secrets.',
        guidance: 'Stop suspicious egress, rotate exposed credentials or tokens, and keep only evidence required by active assertions.',
        primaryAction: { label: 'Data security', to: '/data-security' },
        tone: 'critical',
      };
    case 'access':
      return {
        immediate: 'Contain privileged access changes.',
        guidance: 'Review session, MFA, PAM, and role-change evidence; revoke or narrow access before marking resolved.',
        primaryAction: { label: 'Access controls', to: '/access' },
        tone: 'warning',
      };
    case 'patch':
      return {
        immediate: 'Move to update-only remediation safely.',
        guidance: 'Allow only package repositories, Control One API, DNS/NTP, and approved proxies while the fix deploys.',
        primaryAction: { label: 'Patch posture', to: '/infrastructure/patch' },
        tone: 'warning',
      };
    case 'compliance':
      return {
        immediate: 'Tie remediation to exact control evidence.',
        guidance: 'Repair the failed assertion and remove redundant collection that is not used for measurement.',
        primaryAction: { label: 'Compliance', to: '/compliance' },
        tone: 'warning',
      };
    default:
      return {
        immediate: ip ? `Contain ${ip} and verify evidence.` : 'Contain the affected scope and verify evidence.',
        guidance: 'Use the smallest safe block or isolation action, then close only when audit and remediation records support the decision.',
        primaryAction: { label: 'Investigate', to: ip ? `/investigate/ip/${encodeURIComponent(ip)}?audit=1` : '/investigate' },
        tone: 'warning',
      };
  }
}

function ResolveAlertModal({
  alert,
  open,
  resolving,
  error,
  onConfirm,
  onCancel,
  onActionTaken,
}: {
  alert: Alert | null;
  open: boolean;
  resolving: boolean;
  error?: string | null;
  onConfirm: (payload: UpdateAlertDispositionPayload) => void;
  onCancel: () => void;
  onActionTaken: () => void;
}) {
  const plan = alert ? alertResolutionPlan(alert) : null;
  const ip = alert ? alertSourceIP(alert) : '';
  const [disposition, setDisposition] = useState<AlertDispositionValue>('resolved');
  const [reason, setReason] = useState('');
  const [suppressUntil, setSuppressUntil] = useState('');

  useEffect(() => {
    if (!open) return;
    setDisposition(alert?.disposition?.value ?? 'resolved');
    setReason(alert?.disposition?.reason ?? '');
    setSuppressUntil(toDateTimeLocal(alert?.disposition?.suppress_until));
  }, [open, alert?.id, alert?.disposition?.value, alert?.disposition?.reason, alert?.disposition?.suppress_until]);

  const selectedDisposition = dispositionOption(disposition);
  const reasonMissing = reason.trim().length === 0;
  const suppressMissing = disposition === 'suppressed' && suppressUntil.trim().length === 0;
  const suppressInvalid = suppressUntil.trim().length > 0 && Number.isNaN(Date.parse(suppressUntil));
  const confirmDisabled = !alert || reasonMissing || suppressMissing || suppressInvalid;

  const handleConfirm = () => {
    if (confirmDisabled) return;
    const payload: UpdateAlertDispositionPayload = {
      disposition,
      reason: reason.trim(),
    };
    if (disposition === 'suppressed' && suppressUntil.trim()) {
      payload.suppress_until = new Date(suppressUntil).toISOString();
    }
    onConfirm(payload);
  };

  return (
    <Dialog open={open} onOpenChange={(next) => { if (!next && !resolving) onCancel(); }}>
      <DialogContent className="max-h-[85vh] max-w-4xl overflow-y-auto">
        <DialogHeader>
          <DialogTitle>Resolve alert with evidence</DialogTitle>
          <DialogDescription>
            Resolve should mean containment or a documented false-positive decision exists, not just inbox cleanup.
          </DialogDescription>
        </DialogHeader>

        {alert && plan ? (
          <div className="grid gap-4 lg:grid-cols-[1fr_18rem]">
            <div className="space-y-3">
              <div className="rounded-lg border border-border-subtle bg-elevated p-3">
                <div className="flex flex-wrap items-start justify-between gap-2">
                  <div>
                    <p className="text-xs uppercase tracking-wide text-text-muted">{alert.severity} alert</p>
                    <h3 className="mt-1 text-base font-semibold text-foreground">{alert.title}</h3>
                  </div>
                  <StatusTag tone={severityTone(alert.severity)}>{alert.state}</StatusTag>
                </div>
                {alert.summary ? <p className="mt-2 text-sm text-text-secondary">{alert.summary}</p> : null}
                {plan.facts.length > 0 ? (
                  <div className="mt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
                    {plan.facts.map((fact) => (
                      <div key={`${fact.label}:${fact.value}`} className="rounded-md border border-border-subtle bg-surface px-2.5 py-2">
                        <p className="text-[0.68rem] uppercase tracking-wide text-text-muted">{fact.label}</p>
                        <StatusTag tone={fact.tone} className="mt-1 max-w-full truncate">
                          {fact.value}
                        </StatusTag>
                      </div>
                    ))}
                  </div>
                ) : null}
              </div>

              <div className="rounded-lg border border-border-subtle bg-surface p-3">
                <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                  <ListChecks className="h-4 w-4 text-brand-400" />
                  Recommended resolution actions
                </div>
                <ol className="space-y-2">
                  {plan.steps.map((step, index) => (
                    <li key={step} className="flex gap-2 text-sm text-text-secondary">
                      <span className="mt-0.5 flex h-5 w-5 shrink-0 items-center justify-center rounded-full bg-brand-500/15 font-mono text-[0.7rem] text-brand-400">
                        {index + 1}
                      </span>
                      <span>{step}</span>
                    </li>
                  ))}
                </ol>
              </div>

              <div className="grid gap-2 sm:grid-cols-2">
                {plan.actions.map((action) => (
                  <Button key={action.to} asChild variant="outline" size="sm" className="justify-between">
                    <Link to={action.to}>
                      {action.label}
                      <ArrowRight />
                    </Link>
                  </Button>
                ))}
              </div>
            </div>

            <div className="space-y-3">
              {ip ? (
                <div className="rounded-lg border border-state-critical/25 bg-state-critical/5 p-3">
                  <div className="flex items-start gap-2">
                    <Shield className="mt-0.5 h-4 w-4 text-state-critical" />
                    <div>
                      <p className="text-sm font-medium text-foreground">Contain source IP</p>
                      <p className="mt-1 font-mono text-xs text-text-secondary">{ip}</p>
                    </div>
                  </div>
                  <IpActionMenu
                    ip={ip}
                    onActionTaken={onActionTaken}
                    trigger={(
                      <Button type="button" variant="danger" size="sm" className="mt-3 w-full">
                        <Shield />
                        Block / allow IP
                      </Button>
                    )}
                  />
                </div>
              ) : null}

              <div className="rounded-lg border border-border-subtle bg-surface p-3">
                <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                  <ShieldCheck className="h-4 w-4 text-brand-400" />
                  Posture recommendation
                </div>
                <div className="space-y-2">
                  {plan.posture.map((item) => (
                    <div key={`${item.mode}:${item.scope}`} className="rounded-md border border-border-subtle bg-elevated p-2">
                      <div className="flex flex-wrap items-center gap-2">
                        <StatusTag tone={item.tone}>{item.mode}</StatusTag>
                        <span className="text-xs text-text-muted">{item.scope}</span>
                      </div>
                      <p className="mt-1 text-xs text-text-secondary">{item.reason}</p>
                      <p className="mt-1 text-xs text-text-muted">{item.equivalent}</p>
                    </div>
                  ))}
                </div>
              </div>

              <div className="rounded-lg border border-border-subtle bg-surface p-3">
                <div className="mb-2 flex items-center gap-2 text-sm font-medium text-foreground">
                  <CheckCircle2 className="h-4 w-4 text-state-healthy" />
                  Resolution gate
                </div>
                <p className="text-sm text-text-secondary">{plan.gate}</p>
              </div>

              <div className="rounded-lg border border-border-subtle bg-surface p-3">
                <SelectField
                  id="alert-disposition"
                  label="Disposition"
                  value={disposition}
                  onChange={(event) => setDisposition(event.target.value as AlertDispositionValue)}
                >
                  {ALERT_DISPOSITION_OPTIONS.map((option) => (
                    <option key={option.value} value={option.value}>
                      {option.label}
                    </option>
                  ))}
                </SelectField>
                {selectedDisposition ? (
                  <p className="mt-2 text-xs text-text-secondary">{selectedDisposition.description}</p>
                ) : null}
                {disposition === 'suppressed' ? (
                  <div className="mt-3 flex flex-col gap-1.5">
                    <Label htmlFor="alert-suppress-until">Suppress until</Label>
                    <Input
                      id="alert-suppress-until"
                      type="datetime-local"
                      value={suppressUntil}
                      onChange={(event) => setSuppressUntil(event.target.value)}
                    />
                    {suppressMissing || suppressInvalid ? (
                      <p className="text-xs text-state-critical" role="alert">
                        Select a valid suppression expiry.
                      </p>
                    ) : null}
                  </div>
                ) : null}
                <div className="mt-3 flex flex-col gap-1.5">
                  <Label htmlFor="alert-disposition-reason">Evidence reason</Label>
                  <textarea
                    id="alert-disposition-reason"
                    className="min-h-24 w-full rounded-md border border-border-subtle bg-surface px-3 py-2 text-sm text-foreground placeholder:text-text-muted focus-visible:border-border-strong focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-brand-500/30"
                    value={reason}
                    onChange={(event) => setReason(event.target.value)}
                    placeholder="Summarize the raw evidence, controls applied, owner approval, and remaining risk."
                  />
                  {reasonMissing ? (
                    <p className="text-xs text-state-critical" role="alert">
                      Evidence reason is required for alert disposition.
                    </p>
                  ) : null}
                </div>
              </div>

              <Button asChild variant="ghost" size="sm" className="w-full justify-between">
                <Link to="/audit">
                  Review audit trail
                  <ExternalLink />
                </Link>
              </Button>
            </div>
          </div>
        ) : null}

        <DialogFooter>
          {error ? (
            <p className="mr-auto rounded-md border border-state-critical/40 bg-state-critical/10 px-3 py-2 text-sm text-state-critical" role="alert">
              Alert disposition failed: {error}
            </p>
          ) : null}
          <Button type="button" variant="secondary" onClick={onCancel} disabled={resolving}>
            Cancel
          </Button>
          <Button type="button" variant="primary" onClick={handleConfirm} loading={resolving} disabled={confirmDisabled || resolving}>
            Record disposition
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function toDateTimeLocal(value?: string): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const pad = (part: number) => String(part).padStart(2, '0');
  return [
    date.getFullYear(),
    '-',
    pad(date.getMonth() + 1),
    '-',
    pad(date.getDate()),
    'T',
    pad(date.getHours()),
    ':',
    pad(date.getMinutes()),
  ].join('');
}

function alertResolutionPlan(alert: Alert): AlertResolutionPlan {
  const ip = alertSourceIP(alert);
  const category = alertCategory(alert);
  const scope = alertScope(alert);
  const facts = alertResolutionFacts(alert, category, scope, ip);
  const steps: string[] = [];
  const actions: Array<{ label: string; to: string }> = [];
  const posture = postureRecommendations(alert, category, scope, ip);

  if (ip) {
    steps.push('Open the IP lifecycle and verify the observed paths, country/ASN, blacklist hits, and audit history match this alert.');
    steps.push('Block the IP on affected nodes first; use fleet-wide scope when the same source is active across more than one node or server group.');
    steps.push('Confirm Active Blocks shows the rule applied or queued, then watch for repeated traffic, 4xx/5xx spikes, or outbound transfer after the block.');
    actions.push({ label: 'Open IP investigation', to: `/investigate/ip/${encodeURIComponent(ip)}?audit=1` });
    actions.push({ label: 'Open active blocks', to: '/security/network?tab=blocks' });
  } else {
    steps.push('Inspect the source event and linked entity before changing alert state.');
    actions.push({ label: 'Open search & lifecycle', to: '/investigate' });
  }

  if (category === 'exfiltration') {
    steps.push('Review outbound bytes, destination classes, and egress allowlists; move the affected scope to update-only or full lockdown if transfer continues.');
    actions.push({ label: 'Review IP behavior', to: '/security/network?tab=ip-behavior' });
    actions.push({ label: 'Review exposure posture', to: '/control-room/exposure' });
  } else if (category === 'credential') {
    steps.push('Review auth failures, rotate exposed credentials if needed, and validate no successful session followed the attack window.');
    actions.push({ label: 'Review access', to: '/access' });
  } else if (category === 'exploit' || category === 'scanner') {
    steps.push('Review probed paths and webserver config, then apply capture/enforcement or patch the exposed application before closing.');
    actions.push({ label: 'Open webserver controls', to: '/security/webservers' });
  } else if (category === 'malware') {
    steps.push('Treat this as host compromise until disproven: isolate the node, verify agent/service integrity, and inspect audit for unauthorized control actions.');
    actions.push({ label: 'Open nodes', to: '/nodes' });
    actions.push({ label: 'Review audit trail', to: '/audit' });
  } else if (category === 'exposure') {
    steps.push('Review public listeners, firewall posture, and protected-listener evidence; move the affected scope to whitelist-only or default-deny ingress until drift is clean.');
    actions.push({ label: 'Review exposure confidence', to: '/control-room/exposure' });
    actions.push({ label: 'Review host firewall', to: '/security/network?tab=firewall' });
  } else if (category === 'data') {
    steps.push('Contain suspicious data movement, rotate exposed secrets or tokens, and keep only evidence that maps to an active assertion or incident decision.');
    actions.push({ label: 'Open data security', to: '/data-security' });
    actions.push({ label: 'Review secrets', to: '/secrets' });
  } else if (category === 'access') {
    steps.push('Review privileged session, MFA, PAM, and role-change evidence; revoke or narrow access before closing the alert.');
    actions.push({ label: 'Review access', to: '/access' });
    actions.push({ label: 'Review sessions', to: '/sessions' });
  } else if (category === 'patch') {
    steps.push('Check whether the affected node should move to proxy/update-only posture, then deploy the missing fix inside the approved maintenance window.');
    actions.push({ label: 'Open patch posture', to: '/infrastructure/patch' });
  } else if (category === 'compliance') {
    steps.push('Map the failed control to required evidence, remove unused collection, and re-run the framework assertion after remediation.');
    actions.push({ label: 'Open compliance', to: '/compliance' });
  } else {
    steps.push('Choose the minimum safe posture change for the affected scope, apply the linked remediation, and document why the signal is benign if no control change is needed.');
    actions.push({ label: 'Review Control Room', to: '/control-room' });
    actions.push({ label: 'Review audit trail', to: '/audit' });
  }
  if ((alert.severity ?? '').toLowerCase() === 'critical') {
    steps.push('For a critical signal, prefer a time-boxed containment action first, then relax only after the source, scope, and drift evidence are clean.');
    actions.push({ label: 'Review Control Room', to: '/control-room' });
    actions.push({ label: 'Review audit trail', to: '/audit' });
  }
  steps.push('Capture the final evidence: applied controls, drift status, owner decision, and the reason this alert can be closed.');

  return {
    facts,
    steps,
    gate: ip
      ? 'Close only after a block/allow decision, containment result, or false-positive note is visible in audit/remediation history.'
      : 'Close only after the investigation has an owner decision and the remediation evidence is captured.',
    actions: dedupeActions(actions),
    posture,
  };
}

function alertResolutionFacts(alert: Alert, category: string, scope: string, ip: string): AlertResolutionFact[] {
  const ctx = alert.context ?? {};
  const facts: AlertResolutionFact[] = [];
  const country = contextString(ctx, 'country_code', 'country');
  const asn = contextString(ctx, 'asn');

  addFact(facts, 'Category', titleCase(category), category === 'generic' ? 'warning' : 'info');
  addFact(facts, 'Scope', scope, 'info');
  addFact(facts, 'Source IP', ip, 'critical');
  addFact(facts, 'Signal', contextString(ctx, 'event_type', 'signal', 'reason'), 'info');
  addFact(facts, 'App', contextString(ctx, 'application_name', 'app', 'vhost'), 'healthy');
  addFact(facts, 'Origin', [country, asn ? `ASN ${asn}` : ''].filter(Boolean).join(' / '), 'degraded');
  addFact(facts, 'Confidence', formatPercentLike(contextString(ctx, 'confidence', 'score', 'auto_alert_threshold', 'threat_confidence')), severityTone(alert.severity));
  addFact(facts, 'Request burst', contextString(ctx, 'request_burst', 'requests_1m', 'request_count', 'events'), 'warning');
  addFact(facts, 'Outbound', contextBytes(ctx, 'outbound_transfer', 'outbound_bytes', 'bytes_out'), 'critical');
  addFact(facts, 'Probed paths', contextListString(ctx, 'top_probed_paths', 'probed_paths', 'paths'), 'warning');
  return facts.slice(0, 9);
}

function addFact(facts: AlertResolutionFact[], label: string, value: string, tone: StateTone) {
  if (!value || facts.some((fact) => fact.label === label || fact.value === value)) return;
  facts.push({ label, value, tone });
}

function contextListString(ctx: Record<string, unknown>, ...keys: string[]): string {
  for (const key of keys) {
    const value = ctx[key];
    if (Array.isArray(value)) {
      return value
        .map((item) => String(item).trim())
        .filter(Boolean)
        .slice(0, 4)
        .join(', ');
    }
    if (typeof value === 'string' && value.trim()) return value.trim();
  }
  return '';
}

function formatPercentLike(value: string): string {
  if (!value) return '';
  if (value.includes('%')) return value;
  const numeric = Number(value);
  if (Number.isFinite(numeric) && numeric >= 0 && numeric <= 100) return `${numeric}%`;
  return value;
}

function titleCase(value: string): string {
  return value
    .split(/[\s_-]+/)
    .filter(Boolean)
    .map((part) => part.charAt(0).toUpperCase() + part.slice(1))
    .join(' ');
}

function alertCategory(alert: Alert): string {
  const haystack = `${alert.source} ${alert.title} ${alert.summary ?? ''} ${JSON.stringify(alert.context ?? {})}`.toLowerCase();
  if (haystack.includes('exfil')) return 'exfiltration';
  if (haystack.includes('credential') || haystack.includes('auth failure') || haystack.includes('brute')) return 'credential';
  if (haystack.includes('exploit') || haystack.includes('rce') || haystack.includes('injection')) return 'exploit';
  if (haystack.includes('scanner') || haystack.includes('probe')) return 'scanner';
  if (haystack.includes('malware') || haystack.includes('tamper') || haystack.includes('shutdown') || haystack.includes('service stop')) return 'malware';
  if (haystack.includes('exposure') || haystack.includes('public listener') || haystack.includes('firewall') || haystack.includes('open port')) return 'exposure';
  if (haystack.includes('secret') || haystack.includes('token') || haystack.includes('api key') || haystack.includes('dlp') || haystack.includes('pii')) return 'data';
  if (haystack.includes('privilege') || haystack.includes('sudo') || haystack.includes('mfa') || haystack.includes('pam') || haystack.includes('role') || haystack.includes('session')) return 'access';
  if (haystack.includes('patch') || haystack.includes('cve') || haystack.includes('vulnerab')) return 'patch';
  if (haystack.includes('compliance') || haystack.includes('soc') || haystack.includes('iso') || haystack.includes('nist')) return 'compliance';
  return 'generic';
}

function alertScope(alert: Alert): string {
  const ctx = alert.context ?? {};
  const node = alert.node_id || contextString(ctx, 'node_id', 'hostname', 'host');
  const group = contextString(ctx, 'server_group', 'group', 'cluster');
  const region = contextString(ctx, 'region', 'country_code', 'country');
  const app = contextString(ctx, 'application_name', 'app', 'vhost');
  if (node) return `node ${node}`;
  if (group) return `server group ${group}`;
  if (app && region) return `${app} in ${region}`;
  if (app) return `application ${app}`;
  if (region) return `region/country ${region}`;
  return 'affected tenant scope';
}

function postureRecommendations(
  alert: Alert,
  category: string,
  scope: string,
  ip: string,
): Array<{ mode: string; scope: string; reason: string; equivalent: string; tone: StateTone }> {
  const critical = (alert.severity ?? '').toLowerCase() === 'critical';
  const baseTone: StateTone = critical ? 'critical' : 'warning';
  switch (category) {
    case 'exfiltration':
      return [
        {
          mode: 'aggressive egress lockdown',
          scope,
          reason: 'Outbound transfer risk needs a posture that denies unapproved destinations while preserving Control One, DNS/NTP, and update endpoints.',
          equivalent: 'Today: airgap or whitelist affected nodes, then review egress and Active Blocks.',
          tone: 'critical',
        },
        {
          mode: 'auto-remediation: aggressive',
          scope,
          reason: 'High-confidence exfiltration should allow fast containment with blast-radius caps and rollback evidence.',
          equivalent: 'Template target: egress default deny, approved update/API allowlist, drift reporting.',
          tone: 'warning',
        },
      ];
    case 'credential':
      return [
        {
          mode: 'moderate ingress lockdown',
          scope,
          reason: 'Credential attacks should tighten management paths and raise inbound anomaly thresholds without cutting off known-good operations.',
          equivalent: ip ? `Today: block ${ip} on affected nodes, rotate credentials, and review successful sessions.` : 'Today: restrict management ports and review access.',
          tone: baseTone,
        },
      ];
    case 'exploit':
    case 'scanner':
      return [
        {
          mode: 'aggressive ingress protection',
          scope,
          reason: 'Exploit and scanner activity should move public listeners behind default-deny firewall or webserver enforcement until patched.',
          equivalent: 'Today: enable whitelist-only containment or default-deny inbound rules; use webserver capture/enforce for app paths.',
          tone: baseTone,
        },
      ];
    case 'malware':
      return [
        {
          mode: 'emergency full lockdown',
          scope,
          reason: 'Tamper, malware, or unauthorized service-control attempts require a TTL-bound override that blocks ingress and egress except break-glass/control/update allowlists.',
          equivalent: 'Today: airgap the node for 1h, verify agent integrity, and preserve audit evidence before recovery.',
          tone: 'critical',
        },
      ];
    case 'exposure':
      return [
        {
          mode: 'aggressive ingress protection',
          scope,
          reason: 'Exposure-critical alerts should convert public listener gaps into protected, drift-checked desired state.',
          equivalent: 'Today: apply whitelist-only/default-deny ingress, verify public listeners, and review exposure confidence.',
          tone: baseTone,
        },
      ];
    case 'data':
      return [
        {
          mode: 'egress default deny plus secret rotation',
          scope,
          reason: 'Data and secret alerts need fast containment plus evidence minimization so unused collection is not retained.',
          equivalent: 'Today: restrict egress, rotate exposed credentials, and link only necessary evidence to the incident.',
          tone: 'critical',
        },
      ];
    case 'access':
      return [
        {
          mode: 'privileged-access containment',
          scope,
          reason: 'Privileged access alerts should narrow access, require verification, and leave a clear audit trail before resolution.',
          equivalent: 'Today: revoke or narrow sessions/roles, confirm MFA/PAM evidence, and document owner approval.',
          tone: baseTone,
        },
      ];
    case 'patch':
      return [
        {
          mode: 'maintenance/update-only',
          scope,
          reason: 'Patch-critical alerts should allow package repositories, Control One API, DNS/NTP, and approved proxies while denying unrelated traffic.',
          equivalent: 'Today: use proxy/airgapped patch mode and keep maintenance firewall windows time-boxed.',
          tone: 'warning',
        },
      ];
    case 'compliance':
      return [
        {
          mode: 'evidence-preserving moderate',
          scope,
          reason: 'Compliance-critical alerts need remediation without collecting redundant data that is not used by assertions.',
          equivalent: 'Template target: link expected evidence to controls and remove unused collection paths.',
          tone: 'warning',
        },
      ];
    default:
      return [
        {
          mode: critical ? 'moderate containment' : 'observe and verify',
          scope,
          reason: 'Start with the least disruptive posture that contains the affected scope while preserving telemetry and rollback.',
          equivalent: 'Today: acknowledge, investigate, apply the narrowest block/isolation action, and verify drift/audit evidence.',
          tone: baseTone,
        },
      ];
  }
}

function dedupeActions(actions: Array<{ label: string; to: string }>): Array<{ label: string; to: string }> {
  const seen = new Set<string>();
  return actions.filter((action) => {
    if (seen.has(action.to)) return false;
    seen.add(action.to);
    return true;
  });
}

function alertSourceIP(alert: Alert): string {
  const ctx = alert.context ?? {};
  const direct = contextString(ctx, 'source_ip', 'src_ip', 'remote_ip', 'remote_addr', 'client_ip', 'ip');
  return extractIPv4(direct) || extractIPv4(`${alert.title} ${alert.summary ?? ''}`);
}

function extractIPv4(value: string): string {
  const match = value.match(/\b(?:\d{1,3}\.){3}\d{1,3}\b/);
  return match?.[0] ?? '';
}

function uniqueStrings(values: string[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    const key = value.trim().toLowerCase();
    if (!key || seen.has(key)) continue;
    seen.add(key);
    out.push(value.trim());
  }
  return out;
}

function FilterSelect({
  label,
  value,
  onChange,
  options,
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  options: { label: string; value: string }[];
}) {
  return (
    <SelectField
      label={label}
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {options.map((o) => (
        <option key={o.value} value={o.value}>
          {o.label}
        </option>
      ))}
    </SelectField>
  );
}
