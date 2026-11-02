import type {UpdateCandidate} from '@/lib/api';
import {formatTs} from '@/lib/format';

export function UpdateReviewExplanation({record, now}: {record: UpdateCandidate; now: number}): JSX.Element {
  const review=record.review;
  if (!review) return <p className="mt-2 text-xs">Update review unavailable. Refresh evidence from a compatible server; no installation recommendation is established.</p>;
  const evaluated=Date.parse(review.evaluated_at);
  const until=Date.parse(review.evidence_valid_until ?? '');
  const expired=review.state!=='evidence_required' && (!Number.isFinite(until) || until<=now || !Number.isFinite(evaluated) || evaluated>now+30_000);
  const labels:Record<string,string>={security_fix_review:'Security fix ready for review',update_review:'Newer update available for review',no_newer_candidate:'No newer repository candidate',evidence_required:'More evidence required'};
  const observation=Date.parse(record.repository_observation?.observed_at ?? '');
  const age=Number.isFinite(observation) && observation<=now ? `${Math.floor((now-observation)/60_000)} minutes old` : 'age unavailable';
  return <section aria-label={`Update review for ${record.name}`} className="mt-3 space-y-2 rounded border border-border p-3">
    <p className="font-semibold">{expired ? 'Review evidence expired — refresh required' : labels[review.state] ?? 'Review evidence unavailable'}</p>
    <p>{expired ? 'Previously evaluated evidence no longer establishes a current update recommendation.' : review.summary}</p>
    <p className="text-xs">Repository observation: {age}. Review evaluated {formatTs(review.evaluated_at)}{review.evidence_valid_until ? `; valid until ${formatTs(review.evidence_valid_until)}` : '; no current validity established'}.</p>
    {!expired && <p className="text-xs">Findings addressed by candidate: {review.findings_addressed.length ? review.findings_addressed.join(', ') : 'Not established in selected advisory coverage'}.</p>}
    <dl className="space-y-2 text-xs"><div><dt className="font-semibold">Dependencies</dt><dd>{review.dependencies}</dd></div><div><dt className="font-semibold">Reboot impact</dt><dd>{review.reboot}</dd></div><div><dt className="font-semibold">Health verification</dt><dd>{review.health}</dd></div><div><dt className="font-semibold">Urgency</dt><dd>{review.urgency}</dd></div></dl>
    <details><summary className="cursor-pointer text-xs">Required operator steps</summary><ol className="ml-5 list-decimal space-y-1 text-xs">{review.required_steps.map(step=><li key={step}>{step}</li>)}</ol></details>
    <p className="text-xs text-text-secondary">Read-only recommendation. This does not create an approval, authorize installation or establish a verified remediation.</p>
  </section>;
}
