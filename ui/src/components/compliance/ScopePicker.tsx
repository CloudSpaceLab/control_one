import { useEffect, useId, useMemo, useState } from 'react';
import { SelectField } from '../kit';
import { Input } from '../ui/input';
import { useApiClient } from '../../hooks/useApiClient';
import { cn } from '../../lib/utils';
import type {
  AssignmentScopeType,
  Cluster,
  EnrollmentToken,
  HypervisorHost,
  NodeSummary,
  ScopedAssignmentPayload,
} from '../../lib/api';

export interface ScopePickerValue {
  scope_type: AssignmentScopeType;
  scope_id?: string;
  selector?: Record<string, unknown>;
  selectorText?: string;
}

interface ScopePickerProps {
  tenantId?: string;
  value: ScopePickerValue;
  onChange: (value: ScopePickerValue) => void;
  disabled?: boolean;
  className?: string;
  idPrefix?: string;
}

interface ScopeTargetOption {
  value: string;
  label: string;
  meta?: string;
}

interface TargetState {
  nodes: NodeSummary[];
  clusters: Cluster[];
  hypervisors: HypervisorHost[];
  tokens: EnrollmentToken[];
  loading: boolean;
  error: string | null;
}

const SCOPE_OPTIONS: Array<{ value: AssignmentScopeType; label: string }> = [
  { value: 'tenant', label: 'Org-wide' },
  { value: 'node', label: 'Node' },
  { value: 'cluster', label: 'Cluster' },
  { value: 'hypervisor_host', label: 'Hypervisor' },
  { value: 'enrollment_token', label: 'Fleet token' },
  { value: 'label_selector', label: 'Label selector' },
];

const TARGET_SCOPES = new Set<AssignmentScopeType>([
  'node',
  'cluster',
  'hypervisor_host',
  'enrollment_token',
]);

export function scopeTypeLabel(scopeType: AssignmentScopeType): string {
  return SCOPE_OPTIONS.find((option) => option.value === scopeType)?.label ?? scopeType;
}

export function selectorToText(selector?: Record<string, unknown>): string {
  if (!selector || Object.keys(selector).length === 0) {
    return '';
  }
  return Object.entries(selector)
    .map(([key, rawValue]) => `${key}=${String(rawValue)}`)
    .join(', ');
}

function parseSelectorText(input: string): { selector?: Record<string, unknown>; error?: string } {
  const trimmed = input.trim();
  if (!trimmed) {
    return { error: 'Label selector is required.' };
  }
  if (trimmed.startsWith('{')) {
    try {
      const parsed = JSON.parse(trimmed) as unknown;
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        return { error: 'Selector JSON must be an object.' };
      }
      return { selector: parsed as Record<string, unknown> };
    } catch {
      return { error: 'Selector JSON is invalid.' };
    }
  }

  const selector: Record<string, string> = {};
  for (const segment of trimmed.split(/[,\n]+/)) {
    const item = segment.trim();
    if (!item) {
      continue;
    }
    const splitAt = item.indexOf('=');
    if (splitAt <= 0 || splitAt === item.length - 1) {
      return { error: 'Use key=value labels separated by commas.' };
    }
    selector[item.slice(0, splitAt).trim()] = item.slice(splitAt + 1).trim();
  }
  if (Object.keys(selector).length === 0) {
    return { error: 'Label selector is required.' };
  }
  return { selector };
}

export function buildScopedAssignmentPayload(
  tenantId: string,
  value: ScopePickerValue,
): { payload?: ScopedAssignmentPayload; error?: string } {
  if (!tenantId) {
    return { error: 'Tenant is required.' };
  }

  if (value.scope_type === 'tenant') {
    return { payload: { tenant_id: tenantId, scope_type: 'tenant' } };
  }

  if (value.scope_type === 'label_selector') {
    const parsed =
      value.selectorText !== undefined
        ? parseSelectorText(value.selectorText)
        : { selector: value.selector };
    if (parsed.error || !parsed.selector || Object.keys(parsed.selector).length === 0) {
      return { error: parsed.error ?? 'Label selector is required.' };
    }
    return {
      payload: {
        tenant_id: tenantId,
        scope_type: 'label_selector',
        selector: parsed.selector,
      },
    };
  }

  if (TARGET_SCOPES.has(value.scope_type) && !value.scope_id) {
    return { error: `${scopeTypeLabel(value.scope_type)} target is required.` };
  }

  return {
    payload: {
      tenant_id: tenantId,
      scope_type: value.scope_type,
      scope_id: value.scope_id,
    },
  };
}

export function describeAssignmentScope(scope: {
  scope_type: AssignmentScopeType;
  scope_id?: string;
  node_id?: string;
  selector?: Record<string, unknown>;
}): string {
  if (scope.scope_type === 'tenant') {
    return 'Org-wide';
  }
  if (scope.scope_type === 'label_selector') {
    const selector = selectorToText(scope.selector);
    return selector ? `Labels ${selector}` : 'Label selector';
  }
  const id = scope.scope_id ?? scope.node_id ?? '';
  const suffix = id ? ` ${id.slice(0, 8)}` : '';
  return `${scopeTypeLabel(scope.scope_type)}${suffix}`;
}

export function ScopePicker({
  tenantId,
  value,
  onChange,
  disabled = false,
  className,
  idPrefix,
}: ScopePickerProps): JSX.Element {
  const generatedId = useId().replace(/:/g, '');
  const resolvedId = idPrefix ?? `scope-${generatedId}`;
  const api = useApiClient();
  const [targets, setTargets] = useState<TargetState>({
    nodes: [],
    clusters: [],
    hypervisors: [],
    tokens: [],
    loading: false,
    error: null,
  });

  useEffect(() => {
    let cancelled = false;
    if (!tenantId) {
      setTargets({
        nodes: [],
        clusters: [],
        hypervisors: [],
        tokens: [],
        loading: false,
        error: null,
      });
      return () => {
        cancelled = true;
      };
    }

    setTargets((previous) => ({ ...previous, loading: true, error: null }));
    Promise.allSettled([
      api.listNodes({ tenantId, limit: 500 }),
      api.listClusters({ tenantId, limit: 500 }),
      api.listHypervisorHosts({ tenantId, limit: 500 }),
      api.listEnrollmentTokens({ tenant_id: tenantId, limit: 500 }),
    ]).then(([nodesResult, clustersResult, hypervisorsResult, tokensResult]) => {
      if (cancelled) {
        return;
      }
      const failed = [nodesResult, clustersResult, hypervisorsResult, tokensResult].some(
        (result) => result.status === 'rejected',
      );
      setTargets({
        nodes: nodesResult.status === 'fulfilled' ? nodesResult.value.data : [],
        clusters: clustersResult.status === 'fulfilled' ? clustersResult.value.data : [],
        hypervisors: hypervisorsResult.status === 'fulfilled' ? hypervisorsResult.value.items : [],
        tokens: tokensResult.status === 'fulfilled' ? tokensResult.value.data : [],
        loading: false,
        error: failed ? 'Some assignment targets could not be loaded.' : null,
      });
    });

    return () => {
      cancelled = true;
    };
  }, [api, tenantId]);

  const targetOptions = useMemo<ScopeTargetOption[]>(() => {
    switch (value.scope_type) {
      case 'node':
        return targets.nodes.map((node) => ({
          value: node.id,
          label: node.hostname,
          meta: node.state,
        }));
      case 'cluster':
        return targets.clusters.map((cluster) => ({
          value: cluster.id,
          label: cluster.name,
          meta: cluster.state,
        }));
      case 'hypervisor_host':
        return targets.hypervisors.map((host) => ({
          value: host.id,
          label: host.name,
          meta: host.provider,
        }));
      case 'enrollment_token':
        return targets.tokens.map((token) => ({
          value: token.id,
          label: token.name,
          meta: token.revoked_at ? 'revoked' : `${token.nodes_enrolled}/${token.max_nodes}`,
        }));
      default:
        return [];
    }
  }, [targets.clusters, targets.hypervisors, targets.nodes, targets.tokens, value.scope_type]);

  const selectorText = value.selectorText ?? selectorToText(value.selector);
  const selectorError =
    value.scope_type === 'label_selector' && selectorText.trim()
      ? parseSelectorText(selectorText).error
      : null;
  const needsTarget = TARGET_SCOPES.has(value.scope_type);
  const targetLabel = scopeTypeLabel(value.scope_type);

  return (
    <div className={cn('grid grid-cols-1 gap-3 sm:grid-cols-2', className)}>
      <SelectField
        id={`${resolvedId}-scope-type`}
        label="Assignment scope"
        value={value.scope_type}
        disabled={disabled}
        onChange={(event) => {
          const nextScope = event.target.value as AssignmentScopeType;
          onChange({
            scope_type: nextScope,
            selectorText: nextScope === 'label_selector' ? selectorText : undefined,
          });
        }}
      >
        {SCOPE_OPTIONS.map((option) => (
          <option key={option.value} value={option.value}>
            {option.label}
          </option>
        ))}
      </SelectField>

      {needsTarget ? (
        <SelectField
          id={`${resolvedId}-scope-target`}
          label={targetLabel}
          value={value.scope_id ?? ''}
          disabled={disabled || !tenantId || targets.loading || targetOptions.length === 0}
          onChange={(event) => onChange({ scope_type: value.scope_type, scope_id: event.target.value })}
        >
          <option value="" disabled>
            {!tenantId
              ? 'Select tenant first'
              : targets.loading
                ? 'Loading targets'
                : targetOptions.length === 0
                  ? 'No targets'
                  : `Select ${targetLabel.toLowerCase()}`}
          </option>
          {targetOptions.map((option) => (
            <option key={option.value} value={option.value}>
              {option.meta ? `${option.label} (${option.meta})` : option.label}
            </option>
          ))}
        </SelectField>
      ) : null}

      {value.scope_type === 'label_selector' ? (
        <div className="flex flex-col gap-1.5 sm:col-span-2">
          <label
            htmlFor={`${resolvedId}-selector`}
            className="text-sm font-medium leading-none text-foreground"
          >
            Label selector
          </label>
          <Input
            id={`${resolvedId}-selector`}
            value={selectorText}
            disabled={disabled}
            placeholder="role=db, env=prod"
            onChange={(event) => {
              const nextText = event.target.value;
              const parsed = parseSelectorText(nextText);
              onChange({
                scope_type: 'label_selector',
                selectorText: nextText,
                selector: parsed.selector,
              });
            }}
          />
          {selectorError ? <p className="text-xs text-state-critical">{selectorError}</p> : null}
        </div>
      ) : null}

      {targets.error ? (
        <p className="text-xs text-state-warning sm:col-span-2">{targets.error}</p>
      ) : null}
    </div>
  );
}
