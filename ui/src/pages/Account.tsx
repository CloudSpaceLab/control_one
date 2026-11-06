import { useAuth } from '../providers/AuthProvider';
import { Panel, SectionHeader, StatusTag } from '../components/kit';
import { Button } from '../components/ui/button';

export function Account(): JSX.Element {
  const { profile, loading, error, refreshProfile } = useAuth();
  const roles = [...new Set(profile?.roles ?? [])];
  return (
    <div className="flex flex-col gap-5">
      <SectionHeader
        eyebrow="IDENTITY · MY ACCOUNT"
        title="My account"
        description="Review the identity and access reported for your signed-in account."
        actions={<Button variant="secondary" loading={loading} onClick={() => { void refreshProfile(); }}>Refresh account</Button>}
      />
      {error ? (
        <Panel padding="md" title="Account information unavailable">
          <p role="alert">{error}</p>
        </Panel>
      ) : loading || !profile ? (
        <Panel padding="md" title="Loading account">
          <p role="status">Checking current account information…</p>
        </Panel>
      ) : (
        <>
          <Panel padding="lg" title="Signed-in identity">
            <dl className="grid gap-5 sm:grid-cols-2">
              <div><dt className="text-sm text-text-muted">Name</dt><dd className="mt-1 break-words font-medium">{profile.name || 'Not provided'}</dd></div>
              <div><dt className="text-sm text-text-muted">Email</dt><dd className="mt-1 break-words font-medium">{profile.email || 'Not provided'}</dd></div>
            </dl>
          </Panel>
          <Panel padding="lg" title="Current roles">
            {roles.length ? <ul aria-label="Current account roles" className="flex flex-wrap gap-2">{roles.map(role => <li key={role}><StatusTag tone="info">{role}</StatusTag></li>)}</ul> : <p>No current roles were reported.</p>}
            <p className="mt-4 text-sm text-text-secondary">Roles reflect the latest account check. Tenant access and permissions for each action are checked separately. Contact your administrator to request access changes.</p>
          </Panel>
        </>
      )}
    </div>
  );
}
