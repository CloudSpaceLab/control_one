import { Globe, Shield } from 'lucide-react';
import { Panel, PostureBar, StatusTag } from '@/components/kit';
import { Skeleton } from '@/components/ui/skeleton';
import type { IpEnrichment } from '@/lib/api';

export interface IpEnrichmentCardProps {
  enrichment?: IpEnrichment;
  loading?: boolean;
}

export function IpEnrichmentCard({ enrichment, loading }: IpEnrichmentCardProps) {
  return (
    <Panel padding="md" eyebrow="ENRICHMENT" title="IP intelligence" toneAccent="accent">
      {loading ? (
        <Skeleton className="h-32 w-full" />
      ) : !enrichment ? (
        <p className="text-sm text-text-muted">No enrichment yet.</p>
      ) : (
        <div className="flex flex-col gap-4">
          <div className="grid grid-cols-2 gap-3">
            <Field label="Country" value={enrichment.geo?.country ?? '—'} icon={<Globe className="h-3.5 w-3.5" />} />
            <Field label="City" value={enrichment.geo?.city ?? '—'} />
            <Field label="ASN" value={enrichment.geo?.asn ? `AS${enrichment.geo.asn}` : '—'} mono />
            <Field label="Org" value={enrichment.geo?.org ?? '—'} />
          </div>

          {enrichment.reputation_score !== undefined && (
            <div className="flex flex-col gap-1">
              <div className="flex items-center justify-between text-xs">
                <span className="font-mono uppercase tracking-wider text-text-muted">Reputation</span>
                <span className="font-mono tabular-nums text-foreground">
                  {enrichment.reputation_score.toFixed(0)} / 100
                </span>
              </div>
              <PostureBar score={enrichment.reputation_score} ariaLabel="Reputation score" />
            </div>
          )}

          {enrichment.threat_feeds && enrichment.threat_feeds.length > 0 && (
            <div className="flex flex-col gap-2">
              <div className="flex items-center gap-1.5 font-mono text-[0.65rem] uppercase tracking-wider text-state-critical">
                <Shield className="h-3.5 w-3.5" /> Threat feed matches
              </div>
              <ul className="flex flex-wrap gap-1.5">
                {enrichment.threat_feeds.map((tf, i) => (
                  <li key={i}>
                    <StatusTag tone="critical">{tf.feed}{tf.severity ? ` · ${tf.severity}` : ''}</StatusTag>
                  </li>
                ))}
              </ul>
            </div>
          )}
        </div>
      )}
    </Panel>
  );
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
