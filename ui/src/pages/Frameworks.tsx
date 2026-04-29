import { useEffect, useState } from 'react';
import { Shield } from 'lucide-react';
import {
  DataTable,
  EmptyState,
  Panel,
  SectionHeader,
} from '../components/kit';
import { useApiClient } from '../hooks/useApiClient';
import type { FrameworkControl } from '../lib/api';
import type { ColumnDef } from '@tanstack/react-table';

const columns: ColumnDef<FrameworkControl, unknown>[] = [
  {
    accessorKey: 'control_id',
    header: 'Control ID',
    cell: ({ getValue }) => (
      <span className="font-mono text-xs bg-muted px-1.5 py-0.5 rounded">
        {getValue() as string}
      </span>
    ),
  },
  { accessorKey: 'title', header: 'Title' },
  {
    accessorKey: 'description',
    header: 'Description',
    cell: ({ getValue }) => (
      <span className="text-muted-foreground text-xs">{getValue() as string}</span>
    ),
  },
];

export function Frameworks(): JSX.Element {
  const client = useApiClient();

  const [frameworks, setFrameworks] = useState<string[]>([]);
  const [controls, setControls] = useState<Record<string, FrameworkControl[]>>({});
  const [selected, setSelected] = useState<string>('');
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    client
      .listComplianceFrameworks()
      .then((res) => {
        if (cancelled) return;
        setFrameworks(res.frameworks);
        setControls(res.controls);
        if (res.frameworks.length > 0) {
          setSelected(res.frameworks[0]);
        }
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(err instanceof Error ? err.message : 'Failed to load frameworks');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [client]);

  const activeControls: FrameworkControl[] = selected ? (controls[selected] ?? []) : [];

  return (
    <div className="flex flex-col gap-4">
      <SectionHeader
        title="Compliance Frameworks"
        description="Browse controls for each supported compliance framework."
      />

      {loading && <div className="text-muted-foreground text-sm py-4">Loading frameworks...</div>}

      {error && <p className="text-sm text-destructive">{error}</p>}

      {!loading && frameworks.length > 0 && (
        <>
          <div className="flex gap-2 flex-wrap">
            {frameworks.map((fw) => (
              <button
                key={fw}
                onClick={() => setSelected(fw)}
                className={`flex items-center gap-1.5 px-4 py-2 rounded-full text-sm font-medium border transition-colors ${
                  selected === fw
                    ? 'bg-primary text-primary-foreground border-primary'
                    : 'bg-background border-border text-foreground hover:bg-muted'
                }`}
              >
                <Shield className="w-3.5 h-3.5" />
                {fw}
              </button>
            ))}
          </div>

          {selected && (
            <Panel>
              <div className="p-4">
                <h3 className="font-semibold text-sm mb-3">
                  {selected} — {activeControls.length} controls
                </h3>
                {activeControls.length === 0 ? (
                  <EmptyState title="No controls" description="No controls defined for this framework." />
                ) : (
                  <DataTable columns={columns} rows={activeControls} />
                )}
              </div>
            </Panel>
          )}
        </>
      )}

      {!loading && frameworks.length === 0 && !error && (
        <EmptyState title="No frameworks available" description="Framework data could not be loaded." />
      )}
    </div>
  );
}
