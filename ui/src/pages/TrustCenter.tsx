import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { Shield, CheckCircle, AlertTriangle, FileText, Users, HelpCircle, Clock } from 'lucide-react';
import { Panel, EmptyState, StatusTag } from '../components/kit';
import { Button } from '../components/ui/button';
import { Skeleton } from '../components/ui/skeleton';

// Public API types (no auth required)
interface TrustCenterData {
  tenant_slug: string;
  tenant_name: string;
  subprocessors: Subprocessor[];
  certifications: Certification[];
  faq: FAQItem[];
  incidents: Incident[];
  security_email?: string;
  trust_portal_url?: string;
  last_updated: string;
}

interface Subprocessor {
  name: string;
  purpose: string;
  location: string;
  data_types: string[];
  dpa_in_place: boolean;
  soc2: boolean;
  iso27001: boolean;
}

interface Certification {
  type: string;
  scope: string;
  issued_at: string;
  expires_at: string;
  auditor: string;
  status: string;
}

interface FAQItem {
  question: string;
  answer: string;
  category: string;
}

interface Incident {
  incident_id: string;
  title: string;
  summary: string;
  severity: string;
  status: string;
  started_at: string;
  resolved_at?: string;
  published_at: string;
}

type Tab = 'overview' | 'subprocessors' | 'certifications' | 'incidents' | 'faq';

function formatDate(v?: string): string {
  if (!v) return '—';
  const d = new Date(v);
  return Number.isNaN(d.getTime()) ? v : d.toLocaleDateString();
}

function severityTone(sev: string): 'critical' | 'warning' | 'healthy' | 'info' | 'unknown' {
  switch (sev.toLowerCase()) {
    case 'critical': return 'critical';
    case 'high': return 'critical';
    case 'medium': return 'warning';
    case 'low': return 'info';
    default: return 'unknown';
  }
}

export function TrustCenter(): JSX.Element {
  const { tenantSlug } = useParams<{ tenantSlug: string }>();
  const [data, setData] = useState<TrustCenterData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [activeTab, setActiveTab] = useState<Tab>('overview');

  useEffect(() => {
    let cancelled = false;
    if (!tenantSlug) {
      setError('No tenant specified');
      setLoading(false);
      return;
    }

    setLoading(true);
    fetch(`/api/v1/trust/${tenantSlug}`)
      .then(async (res) => {
        if (!res.ok) {
          const text = await res.text();
          throw new Error(text || `HTTP ${res.status}`);
        }
        return res.json();
      })
      .then((d) => {
        if (!cancelled) setData(d);
      })
      .catch((err) => {
        if (!cancelled) setError(err instanceof Error ? err.message : 'Failed to load trust center');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });

    return () => { cancelled = true; };
  }, [tenantSlug]);

  if (loading) {
    return (
      <div className="min-h-screen bg-surface p-6">
        <div className="mx-auto max-w-5xl">
          <Skeleton className="h-16 w-64 mb-4" />
          <Skeleton className="h-6 w-96 mb-8" />
          <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
            <Skeleton className="h-32" />
            <Skeleton className="h-32" />
            <Skeleton className="h-32" />
          </div>
        </div>
      </div>
    );
  }

  if (error || !data) {
    return (
      <div className="min-h-screen bg-surface flex items-center justify-center p-6">
        <EmptyState
          icon={<AlertTriangle className="h-12 w-12" />}
          title="Trust Center Unavailable"
          description={error || 'No data available for this tenant.'}
          action={
            <Button variant="secondary" asChild>
              <Link to="/">Go Home</Link>
            </Button>
          }
        />
      </div>
    );
  }

  const activeCerts = data.certifications.filter(c => c.status === 'active');
  const totalSubprocessors = data.subprocessors.length;
  const publishedIncidents = data.incidents.length;

  return (
    <div className="min-h-screen bg-surface">
      {/* Header */}
      <header className="border-b border-border-subtle bg-elevated">
        <div className="mx-auto max-w-5xl px-6 py-8">
          <div className="flex items-center gap-3 mb-2">
            <Shield className="h-8 w-8 text-brand-500" />
            <span className="text-sm font-medium uppercase tracking-wider text-text-muted">Trust Center</span>
          </div>
          <h1 className="text-3xl font-semibold text-foreground">{data.tenant_name}</h1>
          <p className="mt-2 text-text-secondary">
            Transparency portal for security, compliance, and data privacy practices.
          </p>
          <p className="mt-1 text-xs text-text-muted">
            Last updated: {formatDate(data.last_updated)}
          </p>
        </div>
      </header>

      {/* Tabs */}
      <div className="border-b border-border-subtle bg-elevated">
        <div className="mx-auto max-w-5xl px-6">
          <nav className="flex gap-1 overflow-x-auto">
            {[
              { id: 'overview', label: 'Overview', icon: Shield },
              { id: 'subprocessors', label: 'Subprocessors', icon: Users },
              { id: 'certifications', label: 'Certifications', icon: CheckCircle },
              { id: 'incidents', label: 'Incidents', icon: AlertTriangle },
              { id: 'faq', label: 'Security FAQ', icon: HelpCircle },
            ].map((tab) => (
              <button
                key={tab.id}
                onClick={() => setActiveTab(tab.id as Tab)}
                className={`flex items-center gap-2 px-4 py-3 text-sm font-medium border-b-2 transition-colors whitespace-nowrap ${
                  activeTab === tab.id
                    ? 'border-brand-500 text-brand-600'
                    : 'border-transparent text-text-secondary hover:text-foreground'
                }`}
              >
                <tab.icon className="h-4 w-4" />
                {tab.label}
              </button>
            ))}
          </nav>
        </div>
      </div>

      {/* Content */}
      <main className="mx-auto max-w-5xl px-6 py-8">
        {activeTab === 'overview' && (
          <div className="flex flex-col gap-6">
            {/* Stats cards */}
            <div className="grid grid-cols-1 md:grid-cols-3 gap-4">
              <Panel padding="md" className="text-center">
                <CheckCircle className="h-8 w-8 text-state-healthy mx-auto mb-2" />
                <div className="text-3xl font-bold text-foreground">{activeCerts.length}</div>
                <div className="text-sm text-text-secondary">Active Certifications</div>
              </Panel>
              <Panel padding="md" className="text-center">
                <Users className="h-8 w-8 text-brand-500 mx-auto mb-2" />
                <div className="text-3xl font-bold text-foreground">{totalSubprocessors}</div>
                <div className="text-sm text-text-secondary">Subprocessors</div>
              </Panel>
              <Panel padding="md" className="text-center">
                <AlertTriangle className="h-8 w-8 text-state-warning mx-auto mb-2" />
                <div className="text-3xl font-bold text-foreground">{publishedIncidents}</div>
                <div className="text-sm text-text-secondary">Published Incidents</div>
              </Panel>
            </div>

            {/* Contact info */}
            <Panel padding="md" eyebrow="CONTACT" title="Security & Privacy">
              <div className="flex flex-col gap-2 text-sm">
                {data.security_email && (
                  <p>
                    <span className="text-text-muted">Security email:</span>{' '}
                    <a href={`mailto:${data.security_email}`} className="text-brand-600 hover:underline">
                      {data.security_email}
                    </a>
                  </p>
                )}
                <p className="text-text-secondary">
                  For security inquiries, vulnerability reports, or data subject requests, please contact our security team.
                </p>
              </div>
            </Panel>

            {/* Quick links to other tabs */}
            <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
              <button
                onClick={() => setActiveTab('subprocessors')}
                className="text-left p-4 rounded-lg border border-border-subtle bg-elevated hover:border-brand-300 transition-colors"
              >
                <Users className="h-5 w-5 text-brand-500 mb-2" />
                <h3 className="font-medium text-foreground">Subprocessors</h3>
                <p className="text-sm text-text-secondary mt-1">View third-party service providers and their compliance status</p>
              </button>
              <button
                onClick={() => setActiveTab('certifications')}
                className="text-left p-4 rounded-lg border border-border-subtle bg-elevated hover:border-brand-300 transition-colors"
              >
                <FileText className="h-5 w-5 text-brand-500 mb-2" />
                <h3 className="font-medium text-foreground">Certifications</h3>
                <p className="text-sm text-text-secondary mt-1">View current compliance certifications and audit reports</p>
              </button>
            </div>
          </div>
        )}

        {activeTab === 'subprocessors' && (
          <Panel
            padding="md"
            eyebrow="SUBPROCESSORS"
            title="Third-Party Service Providers"
          >
            <p className="text-sm text-text-secondary mb-4">
              The following subprocessors may process customer data on our behalf.
            </p>
            {data.subprocessors.length === 0 ? (
              <EmptyState
                icon={<Users className="h-10 w-10" />}
                title="No subprocessors listed"
                description="This tenant has not disclosed any subprocessors."
              />
            ) : (
              <div className="overflow-x-auto">
                <table className="w-full text-sm">
                  <thead>
                    <tr className="border-b border-border-subtle">
                      <th className="text-left py-2 px-3 font-medium text-text-muted">Name</th>
                      <th className="text-left py-2 px-3 font-medium text-text-muted">Purpose</th>
                      <th className="text-left py-2 px-3 font-medium text-text-muted">Location</th>
                      <th className="text-left py-2 px-3 font-medium text-text-muted">Compliance</th>
                    </tr>
                  </thead>
                  <tbody className="divide-y divide-border-subtle">
                    {data.subprocessors.map((sp, idx) => (
                      <tr key={idx} className="hover:bg-surface-2/50">
                        <td className="py-3 px-3 font-medium">{sp.name}</td>
                        <td className="py-3 px-3 text-text-secondary">{sp.purpose}</td>
                        <td className="py-3 px-3 text-text-secondary">{sp.location}</td>
                        <td className="py-3 px-3">
                          <div className="flex flex-wrap gap-1">
                            {sp.soc2 && <StatusTag tone="healthy">SOC 2</StatusTag>}
                            {sp.iso27001 && <StatusTag tone="healthy">ISO 27001</StatusTag>}
                            {sp.dpa_in_place && <StatusTag tone="info">DPA</StatusTag>}
                          </div>
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        )}

        {activeTab === 'certifications' && (
          <Panel
            padding="md"
            eyebrow="CERTIFICATIONS"
            title="Compliance Certifications"
          >
            <p className="text-sm text-text-secondary mb-4">
              Current security and compliance certifications.
            </p>
            {data.certifications.length === 0 ? (
              <EmptyState
                icon={<CheckCircle className="h-10 w-10" />}
                title="No certifications listed"
                description="This tenant has not published any certifications."
              />
            ) : (
              <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
                {data.certifications.map((cert, idx) => (
                  <div key={idx} className="p-4 rounded-lg border border-border-subtle bg-surface">
                    <div className="flex items-start justify-between mb-2">
                      <h3 className="font-semibold text-foreground">{cert.type}</h3>
                      <StatusTag tone={cert.status === 'active' ? 'healthy' : cert.status === 'expired' ? 'critical' : 'warning'}>
                        {cert.status}
                      </StatusTag>
                    </div>
                    <p className="text-sm text-text-secondary mb-3">{cert.scope}</p>
                    <div className="text-xs text-text-muted space-y-1">
                      <p><span className="font-medium">Auditor:</span> {cert.auditor}</p>
                      <p><span className="font-medium">Issued:</span> {formatDate(cert.issued_at)}</p>
                      <p><span className="font-medium">Expires:</span> {formatDate(cert.expires_at)}</p>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Panel>
        )}

        {activeTab === 'incidents' && (
          <Panel
            padding="md"
            eyebrow="INCIDENTS"
            title="Security Incident History"
          >
            <p className="text-sm text-text-secondary mb-4">
              Published security incidents and their resolution status.
            </p>
            {data.incidents.length === 0 ? (
              <EmptyState
                icon={<Shield className="h-10 w-10" />}
                title="No published incidents"
                description="This tenant has not published any security incidents."
              />
            ) : (
              <div className="flex flex-col gap-4">
                {data.incidents.map((inc) => (
                  <div key={inc.incident_id} className="p-4 rounded-lg border border-border-subtle bg-surface">
                    <div className="flex items-start justify-between mb-2">
                      <div>
                        <span className="text-xs font-mono text-text-muted">{inc.incident_id}</span>
                        <h3 className="font-semibold text-foreground">{inc.title}</h3>
                      </div>
                      <div className="flex gap-2">
                        <StatusTag tone={severityTone(inc.severity)}>{inc.severity}</StatusTag>
                        <StatusTag tone={inc.status === 'resolved' ? 'healthy' : inc.status === 'postmortem' ? 'info' : 'warning'}>
                          {inc.status}
                        </StatusTag>
                      </div>
                    </div>
                    <p className="text-sm text-text-secondary mb-3">{inc.summary}</p>
                    <div className="flex items-center gap-4 text-xs text-text-muted">
                      <span className="flex items-center gap-1">
                        <Clock className="h-3 w-3" />
                        Started: {formatDate(inc.started_at)}
                      </span>
                      {inc.resolved_at && (
                        <span className="flex items-center gap-1">
                          <CheckCircle className="h-3 w-3" />
                          Resolved: {formatDate(inc.resolved_at)}
                        </span>
                      )}
                      <span>Published: {formatDate(inc.published_at)}</span>
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Panel>
        )}

        {activeTab === 'faq' && (
          <Panel
            padding="md"
            eyebrow="SECURITY FAQ"
            title="Frequently Asked Questions"
          >
            <p className="text-sm text-text-secondary mb-4">
              Common security and privacy questions answered.
            </p>
            {data.faq.length === 0 ? (
              <EmptyState
                icon={<HelpCircle className="h-10 w-10" />}
                title="No FAQ items"
                description="This tenant has not published any security FAQ items."
              />
            ) : (
              <div className="flex flex-col gap-4">
                {Object.entries(
                  data.faq.reduce((acc, item) => {
                    if (!acc[item.category]) acc[item.category] = [];
                    acc[item.category].push(item);
                    return acc;
                  }, {} as Record<string, FAQItem[]>)
                ).map(([category, items]) => (
                  <div key={category}>
                    <h3 className="text-sm font-semibold uppercase tracking-wider text-text-muted mb-3">
                      {category}
                    </h3>
                    <div className="flex flex-col gap-3">
                      {items.map((item, idx) => (
                        <div key={idx} className="p-4 rounded-lg border border-border-subtle bg-surface">
                          <h4 className="font-medium text-foreground mb-1">{item.question}</h4>
                          <p className="text-sm text-text-secondary">{item.answer}</p>
                        </div>
                      ))}
                    </div>
                  </div>
                ))}
              </div>
            )}
          </Panel>
        )}
      </main>
    </div>
  );
}
