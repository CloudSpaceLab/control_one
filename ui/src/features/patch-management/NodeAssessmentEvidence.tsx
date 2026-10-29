import { Alert, Panel } from '@/components/kit';
import type { NodeAssessment } from '@/lib/api';
import { formatTs } from '@/lib/format';

export function NodeAssessmentEvidence({ assessment, loadFailed = false }: { assessment?: NodeAssessment | null; loadFailed?: boolean }) {
  if (loadFailed || !assessment) return (
    <Alert variant="warning" title="Assessment evidence unavailable">
      {loadFailed ? 'Assessment evidence could not be loaded.' : 'No saved assessment attempt is available for this node.'}
      {' '}Package inventory and zero known findings do not establish that the machine is free of vulnerabilities.
    </Alert>
  );
  const evidence = assessment.evidence;
  const reasons = [...new Set([...(evidence.reasons ?? []), ...(assessment.current_reasons ?? [])])];
  return (
    <Panel padding="md" eyebrow="ASSESSMENT EVIDENCE" title={`Assessment: ${assessment.current_status}`}>
      <p className="text-sm text-text-secondary">Saved {formatTs(assessment.completed_at)} · attempt {assessment.id}</p>
      <p className="mt-2 text-sm">{evidence.package_count} package inputs · {evidence.findings_updated ? `${evidence.match_count} known matches` : 'match result unavailable'} · inventory completeness: {evidence.inventory_completeness}.</p>
      <p className="mt-1 text-sm">{evidence.findings_updated ? 'Findings were updated for this attempt.' : 'This attempt did not complete finding updates. Earlier findings are retained.'}</p>
      {evidence.native_snapshot && <p className="mt-1 text-sm">Complete native scope: {evidence.native_snapshot.scope} · observed {formatTs(evidence.native_snapshot.observed_at)} · {evidence.removed_findings ?? 0} findings reconciled as removed in this attempt.</p>}
      <p className="mt-2 text-sm text-text-secondary">Coverage is {evidence.coverage.replaceAll('_', ' ')}. Installation success alone does not verify a security fix.</p>
      <p className="mt-1 text-sm text-text-secondary">A package missing from incomplete inventory does not prove removal or resolve earlier findings. Unknown co-installed versions or conflicting advisory evidence block automatic resolution.</p>
      <details className="mt-3 text-sm">
        <summary className="cursor-pointer">Evidence and limitations</summary>
        <p className="mt-2 break-all font-mono text-xs">Inventory generation: {assessment.inventory_generation || 'unavailable'}</p>
        <p className="break-all font-mono text-xs">Feed generation: {evidence.feed_generation || 'unavailable'}</p>
        <p>Saved outcome: {assessment.status}. Current applicability: {assessment.current_status}.</p>
        <ul className="mt-2 list-disc pl-5">{reasons.map(reason => <li key={reason}>{reason.replaceAll('_', ' ')}</li>)}</ul>
        <ul className="mt-2 list-disc pl-5">{Object.entries(evidence.exclusions ?? {}).map(([reason, count]) => <li key={reason}>{reason.replaceAll('_', ' ')}: {count}</li>)}</ul>
        <ul className="mt-2 list-disc pl-5">{(evidence.feeds ?? []).map(feed => <li key={`${feed.name}:${feed.sha256}`}>{feed.name} · bundle {feed.bundle_id} · sequence {feed.sequence} · expires {feed.expires_at ? formatTs(feed.expires_at) : 'not recorded'}</li>)}</ul>
      </details>
    </Panel>
  );
}
