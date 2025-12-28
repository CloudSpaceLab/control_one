import { useMemo, useState } from 'react';
import { useTenants } from '../hooks/useTenants';

function formatDate(value: string): string {
  return new Date(value).toLocaleString();
}

export function Tenants(): JSX.Element {
  const [offset, setOffset] = useState(0);
  const [limit] = useState(20);
  const { data, pagination, loading, error } = useTenants();

  const rows = useMemo(() => data, [data]);

  return (
    <section>
      <h2>Tenants</h2>
      <p>Tenants represent isolation boundaries for infrastructure, policy, and compliance scope.</p>

      {loading ? <p className="muted">Loading tenants&hellip;</p> : null}
      {error ? <p className="form-error">Failed to load tenants: {error}</p> : null}

      {!loading && !error && rows.length === 0 ? <p className="muted">No tenants registered yet.</p> : null}

      {!loading && !error && rows.length > 0 ? (
        <>
          <table>
            <thead>
              <tr>
                <th>ID</th>
                <th>Name</th>
                <th>Created</th>
              </tr>
            </thead>
            <tbody>
              {rows.map((tenant) => (
                <tr key={tenant.id}>
                  <td>{tenant.id}</td>
                  <td>{tenant.name}</td>
                  <td>{formatDate(tenant.created_at)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <div className="pagination">
            <button type="button" disabled={!pagination.prevOffset} onClick={() => setOffset(pagination.prevOffset ?? 0)}>
              Previous
            </button>
            <span>
              Showing {rows.length} of {pagination.total} tenants
            </span>
            <button type="button" disabled={!pagination.nextOffset} onClick={() => setOffset(pagination.nextOffset ?? offset + limit)}>
              Next
            </button>
          </div>
        </>
      ) : null}
    </section>
  );
}
