import { Globe, Shield, ShieldAlert } from 'lucide-react';
import { Panel, PostureBar, StatusTag } from '@/components/kit';
import { Skeleton } from '@/components/ui/skeleton';
import type { IpEnrichment } from '@/lib/api';

export interface IpEnrichmentCardProps {
  enrichment?: IpEnrichment;
  loading?: boolean;
}

export function IpEnrichmentCard({ enrichment, loading }: IpEnrichmentCardProps) {
  const score = enrichment?.reputation_score ?? 0;
  const hasThreatFeeds = Boolean(enrichment?.threat_feeds?.length);
  const confidenceTone = score >= 90 || hasThreatFeeds ? 'critical' : score >= 50 ? 'warning' : 'info';

  return (
    <Panel padding="md" eyebrow="ENRICHMENT" title="IP intelligence" toneAccent="accent">
      {loading ? (
        <Skeleton className="h-32 w-full" />
      ) : !enrichment ? (
        <p className="text-sm text-text-muted">No enrichment yet.</p>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="rounded-md border border-border-subtle bg-surface px-3 py-3">
            <div className="flex items-start justify-between gap-3">
              <div className="min-w-0">
                <div className="flex items-center gap-1.5 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                  <ShieldAlert className="h-3.5 w-3.5" /> Blacklist confidence
                </div>
                <div className="mt-1 flex items-baseline gap-2">
                  <span className="font-mono text-2xl font-semibold tabular-nums text-foreground">
                    {score.toFixed(0)}
                  </span>
                  <span className="font-mono text-xs uppercase tracking-wider text-text-muted">/ 100</span>
                </div>
              </div>
              <StatusTag tone={confidenceTone}>
                {hasThreatFeeds ? 'Listed' : score > 0 ? 'Risk signal' : 'No feed hit'}
              </StatusTag>
            </div>
            <div className="mt-2">
              <PostureBar score={score} ariaLabel="Blacklist confidence score" />
            </div>
            {enrichment.source && (
              <div className="mt-2 font-mono text-[0.65rem] uppercase tracking-wider text-text-muted">
                Source: {enrichment.source}
              </div>
            )}
          </div>

          {hasThreatFeeds && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-1.5 font-mono text-[0.65rem] uppercase tracking-wider text-state-critical">
                <Shield className="h-3.5 w-3.5" /> Threat feed matches
              </div>
              <ul className="flex flex-wrap gap-1.5">
                {enrichment.threat_feeds?.map((tf, i) => (
                  <li key={`${tf.feed}-${i}`}>
                    <StatusTag tone="critical">
                      {tf.feed}
                      {tf.severity ? ` - ${tf.severity}` : ''}
                    </StatusTag>
                  </li>
                ))}
              </ul>
            </div>
          )}

          <div className="grid grid-cols-2 gap-3">
            <Field label="Country" value={enrichment.geo?.country ?? '-'} icon={<Globe className="h-3.5 w-3.5" />} />
            <Field label="City" value={enrichment.geo?.city ?? '-'} />
            <Field label="ASN" value={formatAsn(enrichment.geo?.asn)} mono />
            <Field label="Org" value={enrichment.geo?.org ?? '-'} />
          </div>
        </div>
      )}
    </Panel>
  );
}

function formatAsn(asn?: string): string {
  const trimmed = asn?.trim();
  if (!trimmed) return '-';
  return trimmed.toUpperCase().startsWith('AS') ? trimmed : `AS${trimmed}`;
}

function Field({ label, value, icon, mono = false }: { label: string; value: string; icon?: JSX.Element; mono?: boolean }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md border border-border-subtle bg-surface px-3 py-2">
      <span className="inline-flex items-center gap-1.5 font-mono text-[0.6rem] uppercase tracking-wider text-text-muted">
        {icon}
        {label}
      </span>
      <span className={mono ? 'font-mono text-sm tabular-nums' : 'text-sm'}>{value}</span>
    </div>
  );
}
