import { useEffect, useMemo, useState, type FormEvent } from 'react';
import { Download, Eye, FileText, RefreshCw } from 'lucide-react';
import { Panel, StatusTag, type StateTone } from '@/components/kit';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { useApiClient } from '@/hooks/useApiClient';
import { useNodes } from '@/hooks/useNodes';
import { useRolePick } from '@/hooks/useRolePick';
import type {
  LogDump,
  LogDumpPreview,
  LogDumpSource,
} from '@/lib/api';
import { useLogDumps } from './useLogDumps';

const RETENTION_DAYS = [1, 3, 7, 14, 30] as const;
const WINDOWS = [
  { label: '15 min', minutes: 15 },
  { label: '1 hour', minutes: 60 },
  { label: '6 hours', minutes: 360 },
  { label: '24 hours', minutes: 1440 },
] as const;

const SELECT_CLASS =
  'h-9 w-full rounded-md border border-border-subtle bg-surface px-3 text-sm text-foreground outline-none focus:border-brand-500';

interface LogDumpPanelProps {
  tenantId: string;
  fixedNodeId?: string;
  entityFilter?: Record<string, string>;
}

function statusTone(status: LogDump['status']): StateTone {
  switch (status) {
    case 'captured':
      return 'healthy';
    case 'failed':
    case 'expired':
      return 'critical';
    case 'capturing':
      return 'warning';
    default:
      return 'info';
  }
}

function formatSize(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes <= 0) return '0 B';
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KiB`;
  return `${(bytes / (1024 * 1024)).toFixed(1)} MiB`;
}

function sourceState(dump: LogDump): string {
  if (dump.source_available === false) return dump.source_reason || 'Unavailable';
  if (dump.source_available === true && dump.row_count === 0) return 'Available · no rows';
  if (dump.source_available === true) return 'Available';
  return 'Pending';
}

export function LogDumpPanel({
  tenantId,
  fixedNodeId,
  entityFilter,
}: LogDumpPanelProps): JSX.Element {
  const client = useApiClient();
  const { isAdmin, isOperator } = useRolePick();
  const canRequest = isAdmin || isOperator;
  const nodes = useNodes({
    tenantId,
    limit: fixedNodeId ? 1 : 200,
    offset: 0,
    pollIntervalMs: 30_000,
  });
  const [selectedNodeId, setSelectedNodeId] = useState(fixedNodeId ?? '');
  const [source, setSource] = useState<LogDumpSource>('control_plane');
  const [windowMinutes, setWindowMinutes] = useState(60);
  const [retention, setRetention] = useState<(typeof RETENTION_DAYS)[number]>(7);
  const [submitting, setSubmitting] = useState(false);
  const [actionError, setActionError] = useState<string | null>(null);
  const [previewDump, setPreviewDump] = useState<LogDump | null>(null);
  const [preview, setPreview] = useState<LogDumpPreview | null>(null);
  const [previewLoading, setPreviewLoading] = useState(false);
  const [downloadingId, setDownloadingId] = useState<string | null>(null);

  useEffect(() => {
    if (fixedNodeId) {
      setSelectedNodeId(fixedNodeId);
      return;
    }
    if (!selectedNodeId && nodes.data.length > 0) {
      setSelectedNodeId(nodes.data[0].id);
    }
  }, [fixedNodeId, nodes.data, selectedNodeId]);

  const selectedNode = useMemo(
    () => nodes.data.find((node) => node.id === selectedNodeId),
    [nodes.data, selectedNodeId],
  );
  const dumps = useLogDumps(tenantId, selectedNodeId || undefined);
  const filterText = entityFilter
    ? Object.entries(entityFilter)
        .map(([key, value]) => `${key}=${value}`)
        .join(', ')
    : '';

  const submit = async (event: FormEvent) => {
    event.preventDefault();
    if (!selectedNodeId || !canRequest) return;
    setSubmitting(true);
    setActionError(null);
    try {
      const end = new Date();
      const start = new Date(end.getTime() - windowMinutes * 60_000);
      await client.createLogDump({
        tenant_id: tenantId,
        node_id: selectedNodeId,
        source,
        window_start: start.toISOString(),
        window_end: end.toISOString(),
        entity_filter: entityFilter,
        retention_days: retention,
      });
      dumps.reload();
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Raw log request failed');
    } finally {
      setSubmitting(false);
    }
  };

  const openPreview = async (dump: LogDump) => {
    setPreviewDump(dump);
    setPreview(null);
    setPreviewLoading(true);
    setActionError(null);
    try {
      setPreview(await client.getLogDumpPreview(tenantId, dump.id));
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Preview failed');
    } finally {
      setPreviewLoading(false);
    }
  };

  const download = async (dump: LogDump) => {
    setDownloadingId(dump.id);
    setActionError(null);
    try {
      const file = await client.downloadLogDump(tenantId, dump.id);
      const url = URL.createObjectURL(file.blob);
      const anchor = document.createElement('a');
      anchor.href = url;
      anchor.download = file.filename || `control-one-log-dump-${dump.id}.ndjson`;
      document.body.appendChild(anchor);
      anchor.click();
      anchor.remove();
      URL.revokeObjectURL(url);
    } catch (error) {
      setActionError(error instanceof Error ? error.message : 'Download failed');
    } finally {
      setDownloadingId(null);
    }
  };

  return (
    <>
      <Panel
        eyebrow="DIAGNOSTICS"
        title="Raw logs"
        actions={
          <Button variant="ghost" size="sm" onClick={dumps.reload} disabled={!selectedNodeId}>
            <RefreshCw className="h-3.5 w-3.5" /> Refresh
          </Button>
        }
      >
        <form className="grid gap-3 md:grid-cols-5" onSubmit={submit}>
          {!fixedNodeId && (
            <div className="md:col-span-2">
              <Label htmlFor="log-dump-node">Node</Label>
              <select
                id="log-dump-node"
                className={SELECT_CLASS}
                value={selectedNodeId}
                onChange={(event) => setSelectedNodeId(event.target.value)}
              >
                <option value="">Select node</option>
                {nodes.data.map((node) => (
                  <option key={node.id} value={node.id}>
                    {node.hostname || node.id}
                  </option>
                ))}
              </select>
            </div>
          )}
          <div>
            <Label htmlFor="log-dump-source">Source</Label>
            <select
              id="log-dump-source"
              className={SELECT_CLASS}
              value={source}
              onChange={(event) => setSource(event.target.value as LogDumpSource)}
            >
              <option value="control_plane">Control plane</option>
              <option value="node_agent">Node agent</option>
            </select>
          </div>
          <div>
            <Label htmlFor="log-dump-window">Window</Label>
            <select
              id="log-dump-window"
              className={SELECT_CLASS}
              value={windowMinutes}
              onChange={(event) => setWindowMinutes(Number(event.target.value))}
            >
              {WINDOWS.map((window) => (
                <option key={window.minutes} value={window.minutes}>
                  {window.label}
                </option>
              ))}
            </select>
          </div>
          <div>
            <Label htmlFor="log-dump-retention">Retention</Label>
            <select
              id="log-dump-retention"
              className={SELECT_CLASS}
              value={retention}
              onChange={(event) =>
                setRetention(Number(event.target.value) as (typeof RETENTION_DAYS)[number])
              }
            >
              {RETENTION_DAYS.map((days) => (
                <option key={days} value={days}>
                  {days} day{days === 1 ? '' : 's'}
                </option>
              ))}
            </select>
          </div>
          <div className="flex items-end">
            <Button
              className="w-full"
              type="submit"
              size="md"
              loading={submitting}
              disabled={!selectedNodeId || !canRequest}
            >
              <FileText className="h-4 w-4" /> Request
            </Button>
          </div>
        </form>

        <div className="flex flex-wrap items-center gap-2 text-xs text-text-muted">
          <span>Node: {selectedNode?.hostname || selectedNodeId || '—'}</span>
          {filterText && <span className="font-mono">Entity: {filterText}</span>}
          {!canRequest && <span>Operator or admin role required to request.</span>}
        </div>

        {(actionError || dumps.error || nodes.error) && (
          <div className="rounded-md border border-state-critical/30 bg-state-critical/10 px-3 py-2 text-xs text-state-critical">
            {actionError || dumps.error || nodes.error}
          </div>
        )}

        <div className="overflow-x-auto rounded-md border border-border-subtle">
          <table className="min-w-full divide-y divide-border-subtle text-left text-xs">
            <thead className="bg-surface text-[0.65rem] uppercase text-text-muted">
              <tr>
                <th className="px-3 py-2 font-medium">Created</th>
                <th className="px-3 py-2 font-medium">Source</th>
                <th className="px-3 py-2 font-medium">Status</th>
                <th className="px-3 py-2 font-medium">Rows</th>
                <th className="px-3 py-2 font-medium">Size</th>
                <th className="px-3 py-2 font-medium">Availability</th>
                <th className="px-3 py-2 font-medium">Expires</th>
                <th className="px-3 py-2 text-right font-medium">Actions</th>
              </tr>
            </thead>
            <tbody className="divide-y divide-border-subtle">
              {dumps.data.map((dump) => (
                <tr key={dump.id} className="align-top">
                  <td className="whitespace-nowrap px-3 py-2 font-mono text-text-secondary">
                    {new Date(dump.created_at).toLocaleString()}
                  </td>
                  <td className="whitespace-nowrap px-3 py-2">{dump.source.replace('_', ' ')}</td>
                  <td className="whitespace-nowrap px-3 py-2">
                    <div className="flex flex-wrap items-center gap-1">
                      <StatusTag tone={statusTone(dump.status)}>{dump.status}</StatusTag>
                      {dump.truncated && <StatusTag tone="warning">truncated</StatusTag>}
                    </div>
                    {dump.error && <div className="mt-1 max-w-56 text-state-critical">{dump.error}</div>}
                  </td>
                  <td className="px-3 py-2 font-mono tabular-nums">{dump.row_count}</td>
                  <td className="whitespace-nowrap px-3 py-2 font-mono">{formatSize(dump.size_bytes)}</td>
                  <td className="max-w-56 px-3 py-2">{sourceState(dump)}</td>
                  <td className="whitespace-nowrap px-3 py-2 font-mono">
                    {new Date(dump.expires_at).toLocaleString()}
                  </td>
                  <td className="px-3 py-2">
                    <div className="flex justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="sm"
                        disabled={dump.status !== 'captured' || dump.expired}
                        onClick={() => void openPreview(dump)}
                      >
                        <Eye className="h-3.5 w-3.5" /> Preview
                      </Button>
                      <Button
                        variant="ghost"
                        size="sm"
                        loading={downloadingId === dump.id}
                        disabled={dump.status !== 'captured' || dump.expired}
                        onClick={() => void download(dump)}
                      >
                        <Download className="h-3.5 w-3.5" /> Download
                      </Button>
                    </div>
                  </td>
                </tr>
              ))}
              {!dumps.loading && dumps.data.length === 0 && (
                <tr>
                  <td className="px-3 py-6 text-center text-text-muted" colSpan={8}>
                    No raw log requests for this node.
                  </td>
                </tr>
              )}
              {dumps.loading && dumps.data.length === 0 && (
                <tr>
                  <td className="px-3 py-6 text-center text-text-muted" colSpan={8}>
                    Loading raw logs…
                  </td>
                </tr>
              )}
            </tbody>
          </table>
        </div>
      </Panel>

      <Dialog
        open={previewDump !== null}
        onOpenChange={(open) => {
          if (!open) {
            setPreviewDump(null);
            setPreview(null);
          }
        }}
      >
        <DialogContent className="max-w-4xl">
          <DialogHeader>
            <DialogTitle>Raw log preview</DialogTitle>
          </DialogHeader>
          {previewLoading ? (
            <p className="text-sm text-text-muted">Loading preview…</p>
          ) : (
            <pre className="max-h-[65vh] overflow-auto rounded-md border border-border-subtle bg-surface p-3 font-mono text-[0.7rem] leading-5 text-text-secondary">
              {preview?.lines.join('\n') || 'No rows.'}
            </pre>
          )}
          {preview?.truncated && (
            <p className="text-xs text-state-warning">Preview or capture is truncated.</p>
          )}
        </DialogContent>
      </Dialog>
    </>
  );
}
