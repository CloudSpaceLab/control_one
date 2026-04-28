import { FormEvent, useMemo, useState } from 'react';
import { Building2, RefreshCw } from 'lucide-react';
import { useTenants } from '../hooks/useTenants';
import { useApiClient } from '../hooks/useApiClient';
import { useFormFeedback } from '../hooks/useFormFeedback';
import { useToast } from '../providers/ToastProvider';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { Label } from '../components/ui/label';
import {
  DataTable,
  EmptyState,
  EntityChip,
  KpiTile,
  Panel,
  SectionHeader,
} from '../components/kit';
import type { ColumnDef } from '@tanstack/react-table';
import type { Tenant } from '../lib/api';

function formatDate(value: string): string {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return value;
  }
  return date.toLocaleString();
}

export function Tenants(): JSX.Element {
  const api = useApiClient();
  const [offset, setOffset] = useState(0);
  const [limit] = useState(20);
  const [nameFilter, setNameFilter] = useState('');
  const { data, pagination, loading, error, reload } = useTenants({
    limit,
    offset,
    namePrefix: nameFilter.trim() || undefined,
  });
  const [tenantName, setTenantName] = useState('');
  const { error: formError, success: formSuccess, showError, showSuccess, reset } = useFormFeedback();
  const { showToast } = useToast();
  const [submitting, setSubmitting] = useState(false);
  const [selectedTenantId, setSelectedTenantId] = useState<string | null>(null);
  const [renameValue, setRenameValue] = useState('');
  const [renaming, setRenaming] = useState(false);
  const [deleting, setDeleting] = useState(false);

  const rows = useMemo(() => data, [data]);
  const selectedTenant = useMemo(
    () => rows.find((tenant) => tenant.id === selectedTenantId) ?? null,
    [rows, selectedTenantId],
  );

  const summary = useMemo(() => {
    const total = pagination.total;
    const newest = rows[0];
    return {
      total,
      newestName: newest?.name ?? '—',
      newestDate: newest ? formatDate(newest.created_at) : '—',
    };
  }, [pagination.total, rows]);

  const handleCreateTenant = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const name = tenantName.trim();
    if (!name) {
      showError('Tenant name is required');
      return;
    }

    setSubmitting(true);
    reset();
    try {
      await api.createTenant({ name });
      setTenantName('');
      setOffset(0);
      reload();
      const successMessage = 'Tenant created successfully.';
      showSuccess(successMessage);
      showToast(successMessage, 'success');
    } catch (err) {
      if (err instanceof Error) {
        showError(err.message);
        showToast(err.message, 'error');
      } else {
        const fallback = 'Failed to create tenant';
        showError(fallback);
        showToast(fallback, 'error');
      }
    } finally {
      setSubmitting(false);
    }
  };

  const openTenantDetails = (tenantId: string) => {
    setSelectedTenantId((current) => (current === tenantId ? null : tenantId));
    const tenant = rows.find((t) => t.id === tenantId);
    setRenameValue(tenant?.name ?? '');
  };

  const handleRenameTenant = async () => {
    if (!selectedTenant) {
      return;
    }
    const next = renameValue.trim();
    if (!next) {
      showToast('Tenant name cannot be empty.', 'error');
      return;
    }
    if (next === selectedTenant.name) {
      showToast('No changes detected.', 'info');
      return;
    }
    setRenaming(true);
    try {
      await api.updateTenant(selectedTenant.id, { name: next });
      showToast('Tenant renamed.', 'success');
      reload();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to rename tenant.';
      showToast(message, 'error');
    } finally {
      setRenaming(false);
    }
  };

  const handleDeleteTenant = async () => {
    if (!selectedTenant) {
      return;
    }
    const confirmed = window.confirm(
      `Delete tenant “${selectedTenant.name}”? Nodes and jobs referencing this tenant may become orphaned.`,
    );
    if (!confirmed) {
      return;
    }
    setDeleting(true);
    try {
      await api.deleteTenant(selectedTenant.id);
      showToast('Tenant deleted.', 'success');
      setSelectedTenantId(null);
      reload();
    } catch (error) {
      const message = error instanceof Error ? error.message : 'Failed to delete tenant.';
      showToast(message, 'error');
    } finally {
      setDeleting(false);
    }
  };

  const columns = useMemo<ColumnDef<Tenant>[]>(() => [
    {
      accessorKey: 'name',
      header: 'Name',
      cell: ({ getValue }) => <span className="font-medium">{getValue() as string}</span>,
    },
    {
      accessorKey: 'id',
      header: 'Tenant ID',
      cell: ({ getValue }) => <EntityChip type="tenant" value={getValue() as string} />,
    },
    {
      accessorKey: 'created_at',
      header: 'Created',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs tabular-nums">{formatDate(getValue() as string)}</span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          variant="ghost"
          size="sm"
          onClick={() => openTenantDetails(row.original.id)}
        >
          {selectedTenantId === row.original.id ? 'Hide' : 'View'}
        </Button>
      ),
    },
  ], [selectedTenantId, rows]);

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="GOVERNANCE · TENANTS"
        title="Tenants"
        description="Tenants represent isolation boundaries for infrastructure, policy, and compliance scope."
        actions={
          <Button variant="secondary" size="md" onClick={reload} disabled={loading}>
            <RefreshCw className="h-4 w-4" /> {loading ? 'Refreshing…' : 'Refresh'}
          </Button>
        }
      />

      <div className="grid grid-cols-1 gap-3 sm:grid-cols-3">
        <KpiTile label="TOTAL TENANTS" value={summary.total} tone="brand" />
        <KpiTile label="MOST RECENT" value={summary.newestName} tone="info" hint={summary.newestDate} />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-[minmax(0,360px),1fr,minmax(0,360px)]">
        <Panel padding="md" eyebrow="CREATE" title="Create tenant">
          <form className="flex flex-col gap-3" onSubmit={handleCreateTenant}>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="tenant-name">Name</Label>
              <Input
                id="tenant-name"
                name="tenant-name"
                type="text"
                value={tenantName}
                onChange={(event) => setTenantName(event.target.value)}
                placeholder="e.g. Production Cluster"
                disabled={submitting}
                required
              />
            </div>
            {formError ? <p className="text-xs text-state-critical">{formError}</p> : null}
            {formSuccess ? <p className="text-xs text-state-healthy">{formSuccess}</p> : null}
            <Button type="submit" variant="primary" size="md" disabled={submitting}>
              {submitting ? 'Creating…' : 'Create tenant'}
            </Button>
          </form>
        </Panel>

        <Panel padding="sm" tone="inset" eyebrow={`TENANTS · ${rows.length} of ${pagination.total}`} title="Directory">
          <div className="flex flex-col gap-3 px-1 pt-1">
            <div className="flex flex-wrap items-end gap-2">
              <div className="flex flex-1 flex-col gap-1.5 min-w-[200px]">
                <Label htmlFor="tenant-search">Filter</Label>
                <Input
                  id="tenant-search"
                  type="search"
                  placeholder="Search by name"
                  value={nameFilter}
                  onChange={(event) => {
                    setNameFilter(event.target.value);
                    setOffset(0);
                  }}
                />
              </div>
            </div>

            {error ? (
              <Panel padding="sm" tone="inset" toneAccent="critical" eyebrow="ERROR" title="Failed to load tenants">
                <p className="text-sm text-state-critical">{error}</p>
              </Panel>
            ) : null}

            <DataTable
              columns={columns}
              rows={rows}
              rowKey={(r) => r.id}
              loading={loading}
              compact
              empty={
                <EmptyState
                  icon={<Building2 />}
                  title="No tenants"
                  description="No tenants match the current filters."
                />
              }
            />
            <div className="flex items-center justify-between gap-2 border-t border-border-subtle p-2">
              <Button
                variant="secondary"
                size="sm"
                disabled={pagination.prevOffset === null || pagination.prevOffset === undefined}
                onClick={() => setOffset(pagination.prevOffset ?? 0)}
              >
                Previous
              </Button>
              <span className="font-mono text-xs text-text-muted">
                Showing {rows.length} of {pagination.total} tenants
              </span>
              <Button
                variant="secondary"
                size="sm"
                disabled={pagination.nextOffset === null || pagination.nextOffset === undefined}
                onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}
              >
                Next
              </Button>
            </div>
          </div>
        </Panel>

        {selectedTenant ? (
          <Panel padding="md" eyebrow="DETAILS" title={selectedTenant.name}>
            <dl className="grid grid-cols-1 gap-2 text-sm">
              <div className="flex flex-col gap-0.5">
                <dt className="text-xs text-text-muted uppercase tracking-wider">Tenant ID</dt>
                <dd className="font-mono text-xs text-text-secondary break-all">{selectedTenant.id}</dd>
              </div>
              <div className="flex flex-col gap-0.5">
                <dt className="text-xs text-text-muted uppercase tracking-wider">Created</dt>
                <dd>{formatDate(selectedTenant.created_at)}</dd>
              </div>
            </dl>
            <div className="flex flex-col gap-2 pt-2 border-t border-border-subtle">
              <Label htmlFor="rename-tenant">Rename tenant</Label>
              <Input
                id="rename-tenant"
                type="text"
                value={renameValue}
                onChange={(event) => setRenameValue(event.target.value)}
              />
              <div className="flex flex-wrap gap-2">
                <Button variant="ghost" size="sm" onClick={() => setSelectedTenantId(null)}>
                  Close
                </Button>
                <Button variant="primary" size="sm" onClick={handleRenameTenant} disabled={renaming}>
                  {renaming ? 'Saving…' : 'Save changes'}
                </Button>
                <Button variant="danger" size="sm" onClick={handleDeleteTenant} disabled={deleting}>
                  {deleting ? 'Deleting…' : 'Delete tenant'}
                </Button>
              </div>
            </div>
          </Panel>
        ) : null}
      </div>
    </div>
  );
}
