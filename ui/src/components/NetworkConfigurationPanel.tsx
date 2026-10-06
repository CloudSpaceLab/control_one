import { useQuery } from '@tanstack/react-query';
import { useApiClient } from '../hooks/useApiClient';

const time = (value: string) => value ? new Date(value).toLocaleString() : 'Unknown';

export function NetworkConfigurationPanel({ targetId }: { targetId: string }): JSX.Element {
  const api = useApiClient();
  const query = useQuery({ queryKey: ['network-configuration', targetId], queryFn: () => api.getNetworkConfiguration(targetId) });
  if (query.isLoading) return <section aria-label="Network configuration snapshots"><h3 className="font-semibold">Configuration snapshots and posture</h3><p>Loading configuration evidence…</p></section>;
  if (query.error) return <section aria-label="Network configuration snapshots"><h3 className="font-semibold">Configuration snapshots and posture</h3><p role="alert">Configuration evidence is unavailable.</p></section>;
  if (!query.data || query.data.state === 'not_collected' || query.data.snapshots.length === 0) return <section aria-label="Network configuration snapshots" className="flex flex-col gap-2"><h3 className="font-semibold">Configuration snapshots and posture</h3><p className="text-text-secondary">No configuration snapshot has been reported. Assign a supported read-only edge collector to collect snapshots.</p><p>Write and remediation actions are unavailable.</p></section>;
  return <section aria-label="Network configuration snapshots" className="flex flex-col gap-3">
    <div><h3 className="font-semibold">Configuration snapshots and posture</h3><p className="text-text-secondary">Read-only sanitized evidence from assigned collectors. No device commands or write actions are available here.</p></div>
    {query.data.snapshots.map((snapshot) => <details id={snapshot.id} key={snapshot.id} className="rounded border border-border-subtle p-3">
      <summary className="cursor-pointer font-semibold">Revision {snapshot.revision} · {snapshot.source_type} · {time(snapshot.observed_at)}</summary>
      <p className="mt-2 break-all text-xs text-text-secondary">Adapter: {snapshot.adapter} ({snapshot.adapter_version}) · SHA-256: {snapshot.content_hash}</p>
      <h4 className="mt-3 font-medium">Posture checks</h4>
      <ul className="mt-1 list-disc pl-5 text-sm">{snapshot.findings.map((finding) => <li key={finding.id}>
        <span>{finding.title}: {finding.status.replaceAll('_', ' ')}</span>{finding.severity && <span> · {finding.severity}</span>}
        {finding.evidence?.map((reference) => <a className="ml-2 underline" key={reference} href={`#${snapshot.id}-${reference}`}>{reference}</a>)}
        {finding.evidence_ref && <a className="ml-2 text-xs underline" href={`#${finding.evidence_ref.split(':')[1]}`}>Evidence snapshot</a>}
        {finding.related_evidence_ref && <a className="ml-2 text-xs underline" href={`#${finding.related_evidence_ref.split(':')[1]}`}>Compared snapshot</a>}
      </li>)}</ul>
      <h4 className="mt-3 font-medium">Configuration content (secrets redacted)</h4>
      <pre className="mt-1 max-h-72 overflow-auto rounded bg-surface-subtle p-3 text-xs">{snapshot.content.split('\n').map((line, index) => <span className="block" id={`${snapshot.id}-line:${index + 1}`} key={`${index}-${line}`}>{index + 1}: {line}</span>)}</pre>
      {snapshot.added.length > 0 || snapshot.removed.length > 0 ? <>
        <h4 className="mt-3 font-medium">Changes from previous revision</h4>
        <ul className="mt-1 list-disc pl-5 text-sm">{snapshot.added.map((line, index) => <li key={`added-${index}-${line}`}>Added: {line}</li>)}{snapshot.removed.map((line, index) => <li key={`removed-${index}-${line}`}>Removed: {line}</li>)}</ul>
      </> : snapshot.revision > 1 ? <p className="mt-2 text-sm text-text-secondary">No configuration changes from the previous revision.</p> : null}
    </details>)}
    <p className="text-xs text-text-secondary">Checks not supported by the available evidence remain marked Unsupported. Findings link to the exact sanitized snapshot and line reference.</p>
  </section>;
}
