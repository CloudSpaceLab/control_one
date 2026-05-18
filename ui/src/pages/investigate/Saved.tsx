import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Bookmark, Trash2 } from 'lucide-react';
import { Link } from 'react-router-dom';
import { Button } from '@/components/ui/button';
import { DataTable, EmptyState, Panel, SectionHeader, StatusTag } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { toast } from 'sonner';
import type { ColumnDef } from '@tanstack/react-table';
import type { SavedSearch } from '@/lib/api';

export function SavedSearches(): JSX.Element {
  const client = useApiClient();
  const qc = useQueryClient();
  const { currentTenantId } = useTenant();

  const savedQ = useQuery({
    queryKey: ['saved-searches', currentTenantId],
    queryFn: () => client.listSavedSearches({ tenantId: currentTenantId }),
    enabled: !!currentTenantId,
  });

  const del = useMutation({
    mutationFn: (id: string) => client.deleteSavedSearch(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['saved-searches', currentTenantId] });
      toast.success('Saved search deleted');
    },
    onError: (err) => toast.error(err instanceof Error ? err.message : 'Delete failed'),
  });

  const columns: ColumnDef<SavedSearch>[] = [
    {
      accessorKey: 'name',
      header: 'Name',
      cell: ({ row }) => (
        <Link
          to={`/search?q=${encodeURIComponent(row.original.query)}`}
          className="inline-flex items-center gap-2 text-sm text-foreground hover:text-brand-400"
        >
          <Bookmark className="h-4 w-4 text-accent-400" />
          {row.original.name}
        </Link>
      ),
    },
    {
      accessorKey: 'query',
      header: 'Query',
      cell: ({ getValue }) => (
        <span className="font-mono text-xs text-text-secondary">{getValue() as string}</span>
      ),
    },
    {
      accessorKey: 'entity_type',
      header: 'Type',
      cell: ({ getValue }) =>
        getValue() ? <StatusTag tone="info">{(getValue() as string).toUpperCase()}</StatusTag> : null,
    },
    {
      accessorKey: 'shared',
      header: 'Visibility',
      cell: ({ getValue }) => (
        <StatusTag tone={getValue() ? 'healthy' : 'unknown'}>{getValue() ? 'Shared' : 'Private'}</StatusTag>
      ),
    },
    {
      accessorKey: 'updated_at',
      header: 'Updated',
      cell: ({ getValue }) => (
        <span className="text-xs text-text-secondary">
          {new Date(getValue() as string).toLocaleString()}
        </span>
      ),
    },
    {
      id: 'actions',
      header: '',
      cell: ({ row }) => (
        <Button
          variant="ghost"
          size="icon"
          aria-label="Delete saved search"
          onClick={() => del.mutate(row.original.id)}
          disabled={del.isPending}
        >
          <Trash2 className="h-4 w-4 text-state-critical" />
        </Button>
      ),
    },
  ];

  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="INVESTIGATE"
        title="Saved searches"
        description="Persisted queries and entity investigations."
      />
      <Panel padding="sm" tone="inset">
        <DataTable
          columns={columns}
          rows={savedQ.data?.items ?? []}
          rowKey={(r) => r.id}
          loading={savedQ.isLoading}
          empty={<EmptyState title="No saved searches" description="Save a search from the search results page." />}
        />
      </Panel>
    </div>
  );
}
