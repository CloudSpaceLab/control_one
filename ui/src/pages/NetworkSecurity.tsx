import { lazy, Suspense, useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '../components/ui/tabs';
import { SectionHeader, EmptyState, StatusTag, KpiTile, Panel, SelectField, type StateTone } from '../components/kit';
import { Skeleton } from '../components/ui/skeleton';
import { Button } from '../components/ui/button';
import { Input } from '../components/ui/input';
import { ConfirmModal } from '../components/ConfirmModal';
import { useApiClient } from '../hooks/useApiClient';
import { useTenant } from '../providers/TenantProvider';
import { ArrowRight, Ban, Download, Filter, Globe2, Network, RefreshCw, Search, ShieldAlert, ShieldCheck, Sparkles, XCircle } from 'lucide-react';
import type {
  ActiveBlock,
  BehavioralAnomaly,
  ControlRoomFirewallNode,
  ControlRoomOverview,
  IPBehaviorBaseline,
  IPBehaviorCountrySummary,
  IPBehaviorIPProfile,
  IPBlockProposal,
  IpEnrichment,
  NodeFirewallRule,
  WebserverInstance,
} from '../lib/api';

// NetworkSecurity is the consolidated /security/network surface introduced in
// PR 3. It hosts the previously-separate Threat Feeds and Connections pages
// as tabs (lazy-loaded — they keep their own data hooks) and adds two new
// tabs: IP Behavior, Active Blocks (rolled-up view of operator-driven IP
// blocks across the fleet), and Firewall Management (per-node firewall_state
// inspection).
//
// Tab state is mirrored to ?tab=… so deep links survive reloads, and the
// legacy /threat-feeds and /connections routes redirect here preserving
// their tab choice.
const ThreatFeeds = lazy(() => import('./ThreatFeeds').then((m) => ({ default: m.ThreatFeeds })));
const Connections = lazy(() => import('./Connections').then((m) => ({ default: m.Connections })));

const VALID_TABS = ['threats', 'connections', 'ip-behavior', 'blocks', 'firewall'] as const;
type TabKey = (typeof VALID_TABS)[number];

function isValidTab(s: string | null): s is TabKey {
  return !!s && (VALID_TABS as readonly string[]).includes(s);
}

export function NetworkSecurity(): JSX.Element {
  const [params, setParams] = useSearchParams();
  const initial = isValidTab(params.get('tab')) ? (params.get('tab') as TabKey) : 'threats';
  const [tab, setTab] = useState<TabKey>(initial);

  const onTabChange = (next: string) => {
    if (!isValidTab(next)) return;
    setTab(next);
    const updated = new URLSearchParams(params);
    updated.set('tab', next);
    setParams(updated, { replace: true });
  };

  return (
    <div className="space-y-6 p-6">
      <SectionHeader
        title="Network security"
        description="Threat feeds, live connections, active blocks, IP behavior, and firewall state."
      />
      <Tabs value={tab} onValueChange={onTabChange} className="w-full">
        <TabsList>
          <TabsTrigger value="threats">Threat feeds</TabsTrigger>
          <TabsTrigger value="connections">Connections</TabsTrigger>
          <TabsTrigger value="ip-behavior">IP behavior</TabsTrigger>
          <TabsTrigger value="blocks">Active blocks</TabsTrigger>
          <TabsTrigger value="firewall">Firewall</TabsTrigger>
        </TabsList>

        <TabsContent value="threats" className="pt-4">
          <Suspense fallback={<Skeleton className="h-96 w-full" />}>
            {tab === 'threats' && <ThreatFeeds />}
          </Suspense>
        </TabsContent>

        <TabsContent value="connections" className="pt-4">
          <Suspense fallback={<Skeleton className="h-96 w-full" />}>
            {tab === 'connections' && <Connections />}
          </Suspense>
        </TabsContent>

        <TabsContent value="ip-behavior" className="pt-4">
          <IPBehaviorPanel />
        </TabsContent>

        <TabsContent value="blocks" className="pt-4">
          <ActiveBlocksPanel />
        </TabsContent>

        <TabsContent value="firewall" className="pt-4">
          <FirewallManagementPanel />
        </TabsContent>
      </Tabs>
    </div>
  );
}

// ── Active Blocks ────────────────────────────────────────────────────────

type TimeWindowKey = '1h' | '6h' | '24h' | '7d';
type SeverityFilter = 'all' | 'watch' | 'suspicious' | 'high' | 'critical';
type EnforcementTarget = 'firewall' | 'webserver' | 'combined';

const WINDOW_HOURS: Record<TimeWindowKey, number> = {
  '1h': 1,
  '6h': 6,
  '24h': 24,
  '7d': 168,
};

interface IPBehaviorFilters {
  timeWindow: TimeWindowKey;
  severity: SeverityFilter;
  environment: string;
  criticality: string;
  serverGroup: string;
  app: string;
  vhost: string;
}

interface ConfirmState {
  title: string;
  body?: string;
  confirmLabel?: string;
  variant?: 'default' | 'danger';
  run: () => Promise<void>;
}

function IPBehaviorPanel(): JSX.Element {
  const client = useApiClient();
  const navigate = useNavigate();
  const { currentTenantId } = useTenant();
  const [countries, setCountries] = useState<IPBehaviorCountrySummary[]>([]);
  const [webservers, setWebservers] = useState<WebserverInstance[]>([]);
  const [baselines, setBaselines] = useState<IPBehaviorBaseline[]>([]);
  const [findings, setFindings] = useState<BehavioralAnomaly[]>([]);
  const [overview, setOverview] = useState<{ request_count: number; bytes_out: number; status_counts: Record<string, number>; generated_at?: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [filters, setFilters] = useState<IPBehaviorFilters>({
    timeWindow: '1h',
    severity: 'all',
    environment: 'all',
    criticality: 'all',
    serverGroup: '',
    app: '',
    vhost: '',
  });
  const [selectedCountryCode, setSelectedCountryCode] = useState('');
  const [selectedCountryDetail, setSelectedCountryDetail] = useState<IPBehaviorCountrySummary | null>(null);
  const [ipQuery, setIpQuery] = useState('');
  const [profile, setProfile] = useState<IPBehaviorIPProfile | null>(null);
  const [profileBlocks, setProfileBlocks] = useState<IPBlockProposal[]>([]);
  const [profileFindings, setProfileFindings] = useState<BehavioralAnomaly[]>([]);
  const [ipEnrichment, setIpEnrichment] = useState<IpEnrichment | null>(null);
  const [profileError, setProfileError] = useState<string | null>(null);
  const [proposalState, setProposalState] = useState<string | null>(null);
  const [enforcement, setEnforcement] = useState<EnforcementTarget>('firewall');
  const [confirm, setConfirm] = useState<ConfirmState | null>(null);
  const [confirming, setConfirming] = useState(false);

  const since = useMemo(() => windowSince(filters.timeWindow), [filters.timeWindow]);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const [overviewResp, countryResp, webserverResp, baselineResp, findingResp] = await Promise.all([
        client.getIPBehaviorOverview({ tenantId: currentTenantId, since }),
        client.listIPBehaviorCountries({ tenantId: currentTenantId, since }),
        client.listWebserverInstances({ tenantId: currentTenantId, limit: 100 }),
        client.listIPBehaviorBaselines({ tenantId: currentTenantId, limit: 100 }),
        client.listAnomalies({ tenantId: currentTenantId, resolved: false, limit: 100 }),
      ]);
      const nextCountries = countryResp.countries ?? [];
      setOverview(overviewResp);
      setCountries(nextCountries);
      setWebservers(webserverResp.data ?? []);
      setBaselines(baselineResp.data ?? []);
      setFindings(findingResp.data ?? []);
      setSelectedCountryCode((current) => current || nextCountries[0]?.country_code || '');
      setSelectedCountryDetail((current) => current ?? nextCountries[0] ?? null);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Network behavior data failed to load');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId, since]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const visibleCountries = countries.filter((country) => {
    if (filters.serverGroup && !country.server_groups?.some((g) => includesFold(g, filters.serverGroup))) return false;
    if (filters.app && !country.top_apps?.some((app) => includesFold(app, filters.app))) return false;
    const score = countryBackendScore(country, findings);
    if (filters.severity === 'critical') return score >= 85;
    if (filters.severity === 'high') return score >= 70;
    if (filters.severity === 'suspicious') return score >= 50;
    if (filters.severity === 'watch') return score > 0;
    return true;
  });

  const rankedCountries = [...visibleCountries]
    .map((country) => ({
      country,
      baseline: findCountryBaseline(country, baselines, filters),
      score: countryBackendScore(country, findings),
      finding: countryTopFinding(country, findings),
    }))
    .sort((a, b) => b.score - a.score || b.country.request_count - a.country.request_count);
  const selectedCountry = selectedCountryDetail ?? visibleCountries.find((c) => c.country_code === selectedCountryCode) ?? visibleCountries[0] ?? null;
  const selectedCountryBaseline = selectedCountry ? findCountryBaseline(selectedCountry, baselines, filters) : null;
  const selectedCountryInsight = selectedCountry ? countryBaselineInsight(selectedCountry, selectedCountryBaseline, filters.timeWindow) : null;
  const status = overview?.status_counts ?? {};
  const authFailures = (status['401'] ?? 0) + (status['403'] ?? 0);
  const serverErrors = (status['500'] ?? 0) + (status['502'] ?? 0) + (status['503'] ?? 0) + (status['5xx'] ?? 0);
  const profileBaseline = profile ? findIPBaseline(profile, baselines, filters) : null;
  const profileInsight = profile ? profileBaselineInsight(profile, profileBaseline) : null;
  const profileScore = profile ? maxBackendScore(profileFindings) : 0;

  const selectCountry = useCallback(async (country: IPBehaviorCountrySummary) => {
    setSelectedCountryCode(country.country_code);
    setSelectedCountryDetail(country);
    if (!currentTenantId || !country.country_code) return;
    try {
      const detail = await client.getIPBehaviorCountryDetail({ tenantId: currentTenantId, code: country.country_code, since });
      setSelectedCountryDetail(detail);
    } catch {
      setSelectedCountryDetail(country);
    }
  }, [client, currentTenantId, since]);

  const refreshProfileBlocks = useCallback(async (sourceIP: string) => {
    if (!currentTenantId || !sourceIP) return;
    const cidr = ipv4Cidr24(sourceIP);
    const exact = exactIPCIDR(sourceIP);
    const targets = Array.from(new Set([sourceIP, exact, cidr].filter((value): value is string => !!value)));
    const pages = await Promise.all(
      targets.map((ipCidr) => client.listBlockProposals({ tenantId: currentTenantId, ipCidr, limit: 20 })),
    );
    const merged = new Map<string, IPBlockProposal>();
    for (const page of pages) {
      for (const proposal of page.data ?? []) merged.set(proposal.id, proposal);
    }
    setProfileBlocks([...merged.values()]);
  }, [client, currentTenantId]);

  const inspectIP = useCallback(async (overrideIP?: string) => {
    const target = (overrideIP ?? ipQuery).trim();
    if (!currentTenantId || !target) return;
    setIpQuery(target);
    setProfile(null);
    setProfileBlocks([]);
    setProfileFindings([]);
    setIpEnrichment(null);
    setProfileError(null);
    setProposalState(null);
    try {
      const [nextProfile, enrichment] = await Promise.all([
        client.getIPBehaviorIPProfile({ tenantId: currentTenantId, ip: target, since }),
        client.enrichIp(target, currentTenantId).catch(() => null),
      ]);
      setProfile(nextProfile);
      setIpEnrichment(enrichment);
      const [, findings] = await Promise.all([
        refreshProfileBlocks(nextProfile.source_ip),
        client.listAnomalies({ tenantId: currentTenantId, sourceIp: nextProfile.source_ip, resolved: false, limit: 10 }),
      ]);
      setProfileFindings(findings.data ?? []);
    } catch (err) {
      setProfileError(err instanceof Error ? err.message : 'IP profile failed to load');
    }
  }, [client, currentTenantId, ipQuery, refreshProfileBlocks, since]);

  const queueBlockProposal = useCallback((target: 'ip' | 'cidr' | 'vhost') => {
    if (!currentTenantId || !profile?.source_ip) return;
    const cidr = target === 'cidr' ? ipv4Cidr24(profile.source_ip) : profile.source_ip;
    if (!cidr) {
      setProposalState('CIDR proposal requires an IPv4 source address');
      return;
    }
    const scopedToVhost = target === 'vhost';
    const label = target === 'cidr' ? 'Block /24 CIDR' : scopedToVhost ? 'Limit to vhost' : 'Block IP';
    setConfirm({
      title: label,
      body: `${cidr} will be proposed for ${enforcement} enforcement with a 1 hour TTL.`,
      confirmLabel: 'Create proposal',
      variant: 'danger',
      run: async () => {
        setProposalState('Creating proposal...');
        const reason = scopedBlockReason(profile, selectedCountry, profileBaseline, filters, target);
        await client.createBlockProposal({
          tenant_id: currentTenantId,
          ip_cidr: cidr,
          reason,
          score: profileScore || undefined,
          ttl_seconds: 3600,
          scope: scopedToVhost ? 'app' : 'tenant',
          target_type: 'tenant',
          server_group: filters.serverGroup,
          app: filters.app,
          vhost: scopedToVhost ? filters.vhost : '',
          enforcement,
        });
        setProposalState('Proposal queued for approval');
        await refreshProfileBlocks(profile.source_ip);
      },
    });
  }, [client, currentTenantId, enforcement, filters, profile, profileBaseline, profileScore, refreshProfileBlocks, selectedCountry]);

  const queueASNBlockProposal = useCallback(() => {
    if (!currentTenantId || !profile?.source_ip) return;
    const asn = profile.asns?.[0];
    if (!asn) {
      setProposalState('ASN proposal requires ASN data for this IP');
      return;
    }
    setConfirm({
      title: 'Block observed ASN sources',
      body: `${asn} will create capped per-IP block proposals for observed sources in the selected window. Approval is still required before enforcement.`,
      confirmLabel: 'Create proposals',
      variant: 'danger',
      run: async () => {
        setProposalState('Creating ASN proposals...');
        const response = await client.createASNBlockProposals({
          tenant_id: currentTenantId,
          asn,
          since,
          limit: 25,
          reason: asnBlockReason(profile, selectedCountry, profileInsight?.description),
          score: profileScore || undefined,
          ttl_seconds: 3600,
          scope: filters.vhost ? 'app' : 'tenant',
          target_type: 'tenant',
          server_group: filters.serverGroup,
          app: filters.app,
          vhost: filters.vhost,
          enforcement,
        });
        const created = response.created?.length ?? 0;
        const skipped = response.skipped?.length ?? 0;
        setProposalState(`${created} ASN source proposal${created === 1 ? '' : 's'} queued${skipped > 0 ? `, ${skipped} skipped by safety checks` : ''}`);
        await refreshProfileBlocks(profile.source_ip);
      },
    });
  }, [client, currentTenantId, enforcement, filters, profile, profileInsight?.description, profileScore, refreshProfileBlocks, selectedCountry, since]);

  const queueProposalLifecycle = useCallback((proposal: IPBlockProposal, action: 'approve' | 'promote' | 'reject' | 'rollback') => {
    const title = action === 'approve' ? 'Approve block proposal' : action === 'promote' ? 'Promote canary block' : action === 'rollback' ? 'Rollback block' : 'Reject block proposal';
    setConfirm({
      title,
      body: `${proposal.ip_cidr} is currently ${proposal.status}.`,
      confirmLabel: action === 'reject' ? 'Reject' : action === 'rollback' ? 'Rollback' : 'Confirm',
      variant: action === 'rollback' || action === 'reject' ? 'danger' : 'default',
      run: async () => {
        setProposalState(`${title}...`);
        if (action === 'approve') await client.approveBlockProposal(proposal.id);
        if (action === 'promote') await client.promoteBlockProposal(proposal.id);
        if (action === 'reject') await client.rejectBlockProposal(proposal.id, 'Rejected from IP behavior profile');
        if (action === 'rollback') await client.rollbackBlockProposal(proposal.id, 'Rollback requested from IP behavior profile');
        setProposalState('Enforcement workflow updated');
        if (profile?.source_ip) await refreshProfileBlocks(profile.source_ip);
      },
    });
  }, [client, profile?.source_ip, refreshProfileBlocks]);

  const runConfirmed = useCallback(async () => {
    if (!confirm) return;
    setConfirming(true);
    try {
      await confirm.run();
    } catch (err) {
      setProposalState(err instanceof Error ? err.message : 'action failed');
    } finally {
      setConfirming(false);
      setConfirm(null);
    }
  }, [confirm]);

  const collectEvidence = useCallback(() => {
    if (!profile) return;
    const blob = new Blob([JSON.stringify({ profile, enrichment: ipEnrichment, baseline: profileBaseline, blocks: profileBlocks, findings: profileFindings }, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = `control-one-ip-${profile.source_ip}-evidence.json`;
    link.click();
    URL.revokeObjectURL(url);
    setProposalState('Evidence pack generated');
  }, [ipEnrichment, profile, profileBaseline, profileBlocks, profileFindings]);

  const suppressProfileFindings = useCallback(() => {
    if (!profile?.source_ip) return;
    if (profileFindings.length === 0) {
      setProposalState('No open findings for this IP');
      return;
    }
    setConfirm({
      title: 'Suppress IP findings',
      body: `${profileFindings.length} open finding${profileFindings.length === 1 ? '' : 's'} for ${profile.source_ip} will be marked suppressed.`,
      confirmLabel: 'Suppress',
      variant: 'danger',
      run: async () => {
        setProposalState('Suppressing findings...');
        await Promise.all(profileFindings.map((finding) => client.suppressAnomaly(finding.id)));
        setProfileFindings([]);
        setFindings((current) => current.filter((finding) => !profileFindings.some((profileFinding) => profileFinding.id === finding.id)));
        setProposalState('Findings suppressed');
      },
    });
  }, [client, profile?.source_ip, profileFindings]);

  const allowlistPartner = useCallback(() => {
    if (!currentTenantId || !profile?.source_ip) return;
    const cidr = exactIPCIDR(profile.source_ip);
    setConfirm({
      title: 'Allowlist partner IP',
      body: `${cidr} will be added to the tenant allowlist used by capture and enforcement safety checks.`,
      confirmLabel: 'Allowlist',
      run: async () => {
        setProposalState('Updating tenant allowlist...');
        const filters = await client.getTenantEventFilters(currentTenantId);
        const allowlist = Array.from(new Set([...(filters.allowlist_cidrs ?? []), cidr]));
        await client.updateTenantEventFilters(currentTenantId, { allowlist_cidrs: allowlist });
        setProposalState('Tenant allowlist updated');
      },
    });
  }, [client, currentTenantId, profile?.source_ip]);

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant from the header to view IP behavior." />;
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-5">
        <KpiTile label={`Requests, ${filters.timeWindow}`} value={formatNumber(overview?.request_count ?? 0)} loading={loading} />
        <KpiTile label="Bytes out" value={formatBytes(overview?.bytes_out ?? 0)} tone={overview?.bytes_out ? 'info' : 'unknown'} loading={loading} />
        <KpiTile label="401/403" value={formatNumber(authFailures)} tone={authFailures > 0 ? 'warning' : 'healthy'} loading={loading} />
        <KpiTile label="5xx" value={formatNumber(serverErrors)} tone={serverErrors > 0 ? 'critical' : 'healthy'} loading={loading} />
        <KpiTile label="Baselines" value={formatNumber(baselines.length)} tone={baselines.length > 0 ? 'healthy' : 'unknown'} loading={loading} />
      </div>

      <div className="rounded border border-border p-3">
        <div className="mb-3 flex items-center gap-2 text-sm font-medium">
          <Filter className="h-4 w-4" />
          Filters
        </div>
        <div className="grid grid-cols-1 gap-3 md:grid-cols-3 xl:grid-cols-6">
          <SelectField id="ip-window" label="Window" value={filters.timeWindow} onChange={(e) => setFilters({ ...filters, timeWindow: e.target.value as TimeWindowKey })}>
            <option value="1h">1 hour</option>
            <option value="6h">6 hours</option>
            <option value="24h">24 hours</option>
            <option value="7d">7 days</option>
          </SelectField>
          <SelectField id="ip-severity" label="Severity" value={filters.severity} onChange={(e) => setFilters({ ...filters, severity: e.target.value as SeverityFilter })}>
            <option value="all">All</option>
            <option value="watch">Watch+</option>
            <option value="suspicious">Suspicious+</option>
            <option value="high">High+</option>
            <option value="critical">Critical</option>
          </SelectField>
          <SelectField id="ip-env" label="Environment" value={filters.environment} onChange={(e) => setFilters({ ...filters, environment: e.target.value })}>
            <option value="all">All</option>
            <option value="prod">Production</option>
            <option value="dr">DR</option>
            <option value="uat">UAT</option>
          </SelectField>
          <SelectField id="ip-criticality" label="Criticality" value={filters.criticality} onChange={(e) => setFilters({ ...filters, criticality: e.target.value })}>
            <option value="all">All</option>
            <option value="core-banking">Core banking</option>
            <option value="payment-switch">Payment switch</option>
            <option value="atm">ATM</option>
            <option value="dmz">DMZ</option>
            <option value="domain-controllers">Domain controllers</option>
          </SelectField>
          <Input value={filters.serverGroup} onChange={(e) => setFilters({ ...filters, serverGroup: e.target.value })} placeholder="Server group" />
          <Input value={filters.app} onChange={(e) => setFilters({ ...filters, app: e.target.value })} placeholder="App or vhost" />
        </div>
      </div>

      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <div className="flex min-w-0 items-center gap-2 text-sm text-text-secondary">
          <Globe2 className="h-4 w-4 shrink-0" />
          <span className="truncate">
            {rankedCountries.length > 0
              ? rankedCountries[0].score > 0
                ? `${countryLabel(rankedCountries[0].country)} has the top backend IP finding`
                : `${countryLabel(rankedCountries[0].country)} has the most requests; no backend IP finding`
              : 'No web.request rollups in this window'}
          </span>
        </div>
        <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-4">
        {rankedCountries.slice(0, 4).map(({ country, baseline, score }) => (
          <button
            key={country.country_code || country.country || 'unknown'}
            type="button"
            className="rounded border border-border bg-elevated p-3 text-left hover:border-border-strong hover:bg-hover"
            onClick={() => selectCountry(country)}
          >
            <div className="mb-2 flex items-start justify-between gap-2">
              <div className="min-w-0">
                <div className="truncate text-sm font-semibold">{countryLabel(country)}</div>
                <div className="text-xs text-text-secondary">{country.country_code || 'unmapped'}</div>
              </div>
              <StatusTag tone={riskTone(score)}>score {score}</StatusTag>
            </div>
            <div className="grid grid-cols-2 gap-2 text-xs text-text-secondary">
              <span>{formatNumber(country.request_count)} req</span>
              <span>{formatBytes(country.bytes_out)} out</span>
              <span>{formatNumber(country.unique_source_ips)} IPs</span>
              <span>{formatNumber(authCount(country.status_counts))} auth</span>
              <span>{formatNumber(country.status_counts['301'] ?? 0)} 301</span>
              <span>{formatNumber(serverErrorCount(country.status_counts))} 5xx</span>
            </div>
            <div className="mt-2 text-xs text-text-muted">{countryBaselineInsight(country, baseline, filters.timeWindow).label}</div>
          </button>
        ))}
      </div>

      <div className="grid grid-cols-1 gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(340px,0.65fr)]">
        <div className="rounded border border-border">
          <div className="border-b border-border px-3 py-2 text-sm font-medium">Country behavior</div>
          {loading ? (
            <Skeleton className="h-72 w-full" />
          ) : visibleCountries.length === 0 ? (
            <EmptyState title="No country rollups" description="No web.request rollups match these filters." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
                  <tr>
                    <th className="px-3 py-2">Country</th>
                    <th className="px-3 py-2">Risk</th>
                    <th className="px-3 py-2">Unique IPs</th>
                    <th className="px-3 py-2">Requests</th>
                    <th className="px-3 py-2">Bytes out</th>
                    <th className="px-3 py-2">301</th>
                    <th className="px-3 py-2">401/403</th>
                    <th className="px-3 py-2">5xx</th>
                    <th className="px-3 py-2">Top ASN/App</th>
                    <th className="px-3 py-2">Last seen</th>
                  </tr>
                </thead>
                <tbody>
                  {visibleCountries.map((country) => {
                    const score = countryBackendScore(country, findings);
                    const finding = countryTopFinding(country, findings);
                    return (
                      <tr
                        key={country.country_code || country.country || 'unknown'}
                        className="cursor-pointer border-t border-border hover:bg-hover"
                        onClick={() => selectCountry(country)}
                      >
                        <td className="px-3 py-2">
                          <div className="font-medium">{countryLabel(country)}</div>
                          <div className="text-xs text-text-secondary">{country.country_code || 'unmapped'}</div>
                        </td>
                        <td className="px-3 py-2"><StatusTag tone={riskTone(score)}>{severityLabel(score, finding)}</StatusTag></td>
                        <td className="px-3 py-2">{formatNumber(country.unique_source_ips)}</td>
                        <td className="px-3 py-2">{formatNumber(country.request_count)}</td>
                        <td className="px-3 py-2">{formatBytes(country.bytes_out)}</td>
                        <td className="px-3 py-2">{formatNumber(country.status_counts['301'] ?? 0)}</td>
                        <td className="px-3 py-2">{formatNumber(authCount(country.status_counts))}</td>
                        <td className="px-3 py-2">{formatNumber(serverErrorCount(country.status_counts))}</td>
                        <td className="px-3 py-2 text-xs text-text-secondary">{compactList([country.top_asns?.[0], country.top_apps?.[0]])}</td>
                        <td className="px-3 py-2 text-text-secondary">{formatDateTime(country.last_seen_at)}</td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          )}
        </div>

        <div className="space-y-4">
          <div className="rounded border border-border p-3">
            <div className="mb-3 text-sm font-medium">Unusual now</div>
            {rankedCountries.filter((row) => row.score >= 50).length === 0 ? (
              <p className="text-sm text-text-secondary">Current traffic has no suspicious baseline, status, or bytes deviation.</p>
            ) : (
              <div className="space-y-2">
                {rankedCountries.filter((row) => row.score >= 50).slice(0, 5).map(({ country, finding, score }) => (
                  <button
                    key={`${country.country_code}-${country.last_seen_at}`}
                    type="button"
                    className="w-full rounded border border-border p-2 text-left hover:bg-hover"
                    onClick={() => selectCountry(country)}
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="font-medium">{countryLabel(country)}</span>
                      <StatusTag tone={riskTone(score)}>{severityLabel(score, finding)}</StatusTag>
                    </div>
                    <p className="mt-1 text-xs text-text-secondary">{finding?.reason || countryBackendReason(country, findings)}</p>
                  </button>
                ))}
              </div>
            )}
          </div>

          {selectedCountry && selectedCountryInsight && (
            <div className="rounded border border-border p-3">
              <div className="mb-3 flex items-center justify-between gap-2">
                <div className="text-sm font-medium">{countryLabel(selectedCountry)}</div>
                <StatusTag tone={selectedCountryInsight.tone}>{selectedCountryInsight.label}</StatusTag>
              </div>
              <p className="text-sm text-text-secondary">{selectedCountryInsight.description}</p>
              <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-text-secondary">
                <span>{formatNumber(selectedCountry.request_count)} requests</span>
                <span>{formatBytes(selectedCountry.bytes_out)} out</span>
                <span>{compactList(selectedCountry.top_asns)}</span>
                <span>{compactList(selectedCountry.top_apps)}</span>
                <span className="col-span-2">{compactList(selectedCountry.server_groups)}</span>
              </div>
            </div>
          )}

          <div className="rounded border border-border p-3">
            <div className="mb-3 text-sm font-medium">IP profile</div>
            <div className="flex gap-2">
              <Input value={ipQuery} onChange={(e) => setIpQuery(e.target.value)} placeholder="203.0.113.10" onKeyDown={(e) => { if (e.key === 'Enter') void inspectIP(); }} />
              <Button variant="outline" size="icon" onClick={() => void inspectIP()} aria-label="Inspect IP">
                <Search className="h-4 w-4" />
              </Button>
            </div>
            {profileError && <p className="mt-2 text-sm text-destructive">{profileError}</p>}
            {profile && profileInsight && (
              <div className="mt-3 space-y-3 text-sm">
                <div className="flex items-center justify-between gap-2">
                  <span className="font-mono">{profile.source_ip}</span>
                  <StatusTag tone={riskTone(profileScore)}>score {profileScore}</StatusTag>
                </div>
                <div className="rounded border border-border p-2">
                  <div className="mb-1 flex items-center justify-between gap-2">
                    <span className="font-medium">Baseline explanation</span>
                    <StatusTag tone={profileInsight.tone}>{profileInsight.label}</StatusTag>
                  </div>
                  <p className="text-xs text-text-secondary">{profileInsight.description}</p>
                </div>
                <div className="grid grid-cols-2 gap-2 text-text-secondary">
                  <span>{formatNumber(profile.request_count)} requests</span>
                  <span>{formatBytes(profile.bytes_out)} out</span>
                  <span>{formatNumber(authCount(profile.status_counts))} auth failures</span>
                  <span>{formatNumber(serverErrorCount(profile.status_counts))} 5xx</span>
                  <span>{compactList(profile.countries) || 'country unknown'}</span>
                  <span>{compactList(profile.asns) || 'ASN unknown'}</span>
                  <span>{compactList(profile.isps) || ipEnrichment?.geo?.isp || 'ISP unknown'}</span>
                  <span>{ipEnrichment?.reputation_score !== undefined ? `reputation ${ipEnrichment.reputation_score}/100` : 'reputation unknown'}</span>
                  <span>{compactList(profile.server_groups) || 'groups unknown'}</span>
                  <span>{formatNumber(profile.node_ids?.length ?? 0)} servers</span>
                  <span>{formatDateTime(profile.first_seen_at)} first seen</span>
                  <span>{formatDateTime(profile.last_seen_at)} last seen</span>
                </div>
                <div className="rounded border border-border p-2">
                  <div className="mb-2 text-xs font-medium uppercase tracking-wider text-text-secondary">Status mix</div>
                  <div className="grid grid-cols-4 gap-2 text-xs text-text-secondary">
                    {['2xx', '301', '401', '403', '404', '429', '500', '5xx'].map((code) => (
                      <div key={code} className="rounded bg-surface-2 px-2 py-1">
                        <span className="font-mono text-foreground">{formatNumber(profile.status_counts?.[code] ?? 0)}</span> {code}
                      </div>
                    ))}
                  </div>
                </div>
                {profile.history && profile.history.length > 0 && (
                  <div className="rounded border border-border p-2">
                    <div className="mb-2 text-xs font-medium uppercase tracking-wider text-text-secondary">Request and bytes trend</div>
                    <div className="flex h-20 items-end gap-1">
                      {profile.history.slice(-24).map((point) => {
                        const maxReq = Math.max(...(profile.history ?? []).map((row) => row.request_count), 1);
                        const height = Math.max(8, Math.round((point.request_count / maxReq) * 72));
                        return (
                          <div key={point.hour_ts} className="flex min-w-0 flex-1 flex-col items-center gap-1">
                            <div
                              className="w-full rounded-t bg-brand-500/70"
                              style={{ height }}
                              title={`${formatDateTime(point.hour_ts)}: ${formatNumber(point.request_count)} requests, ${formatBytes(point.bytes_out)} out`}
                            />
                            <span className="hidden text-[10px] text-text-muted sm:block">{new Date(point.hour_ts).getHours()}</span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                )}
                <div className="grid gap-2 sm:grid-cols-2">
                  <SelectField
                    id="ip-behavior-enforcement"
                    label="Enforcement"
                    value={enforcement}
                    onChange={(e) => setEnforcement(e.target.value as EnforcementTarget)}
                  >
                    <option value="firewall">Firewall</option>
                    <option value="webserver">Webserver</option>
                    <option value="combined">Firewall + webserver</option>
                  </SelectField>
                  <Input value={filters.vhost} onChange={(e) => setFilters({ ...filters, vhost: e.target.value })} placeholder="Vhost scope" />
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button variant="outline" size="sm" onClick={() => queueBlockProposal('ip')}><Ban className="h-4 w-4" />Block IP</Button>
                  <Button variant="outline" size="sm" onClick={() => queueBlockProposal('cidr')}><ShieldAlert className="h-4 w-4" />Block /24</Button>
                  <Button variant="outline" size="sm" onClick={queueASNBlockProposal} disabled={!profile.asns?.length}><Network className="h-4 w-4" />Block ASN</Button>
                  <Button variant="outline" size="sm" onClick={() => queueBlockProposal('vhost')} disabled={!filters.vhost.trim()}><ShieldCheck className="h-4 w-4" />Limit to vhost</Button>
                  <Button variant="outline" size="sm" onClick={collectEvidence}><Download className="h-4 w-4" />Evidence</Button>
                  <Button variant="outline" size="sm" onClick={() => navigate(`/ask?q=${encodeURIComponent(askAIPrompt(profile, profileInsight.description))}`)}><Sparkles className="h-4 w-4" />Ask AI</Button>
                </div>
                <div className="flex flex-wrap gap-2">
                  <Button variant="ghost" size="sm" onClick={suppressProfileFindings} disabled={profileFindings.length === 0}><XCircle className="h-4 w-4" />Suppress</Button>
                  <Button variant="ghost" size="sm" onClick={allowlistPartner}><ShieldCheck className="h-4 w-4" />Allowlist partner</Button>
                </div>
                {profileFindings.length > 0 && (
                  <div className="rounded border border-border p-2">
                    <div className="mb-2 text-xs font-medium uppercase tracking-wider text-text-secondary">Open findings</div>
                    <div className="space-y-2">
                      {profileFindings.map((finding) => (
                        <div key={finding.id} className="flex items-start justify-between gap-2 text-xs">
                          <div className="min-w-0">
                            <div className="truncate font-medium">{finding.reason || finding.metric}</div>
                            <div className="text-text-secondary">{formatDateTime(finding.last_seen_at ?? finding.created_at)}</div>
                          </div>
                          <StatusTag tone={findingSeverityTone(finding.severity)}>{finding.severity || finding.status || 'open'}</StatusTag>
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                {proposalState && <p className="text-xs text-text-secondary">{proposalState}</p>}
              </div>
            )}
          </div>
        </div>
      </div>

      {profile && (
        <div className="rounded border border-border">
          <div className="border-b border-border px-3 py-2 text-sm font-medium">Enforcement status for {profile.source_ip}</div>
          {profileBlocks.length === 0 ? (
            <EmptyState title="No block proposals" description="No block proposals target this IP." />
          ) : (
            <div className="overflow-x-auto">
              <table className="w-full text-sm">
                <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
                  <tr>
                    <th className="px-3 py-2">Target</th>
                    <th className="px-3 py-2">Enforcement</th>
                    <th className="px-3 py-2">Status</th>
                    <th className="px-3 py-2">Scope</th>
                    <th className="px-3 py-2">Expires</th>
                    <th className="px-3 py-2">Actions</th>
                  </tr>
                </thead>
                <tbody>
                  {profileBlocks.map((proposal) => (
                    <tr key={proposal.id} className="border-t border-border">
                      <td className="px-3 py-2 font-mono text-xs">{proposal.ip_cidr}</td>
                      <td className="px-3 py-2">{proposal.enforcement}</td>
                      <td className="px-3 py-2"><StatusTag tone={blockStatusTone(proposal.status)}>{proposal.status}</StatusTag></td>
                      <td className="px-3 py-2 text-text-secondary">{compactList([proposal.server_group, proposal.app, proposal.vhost]) || proposal.scope}</td>
                      <td className="px-3 py-2 text-text-secondary">{proposal.expires_at ? formatDateTime(proposal.expires_at) : 'manual'}</td>
                      <td className="px-3 py-2">
                        <div className="flex flex-wrap gap-2">
                          {proposal.status === 'proposed' && <Button variant="outline" size="sm" onClick={() => queueProposalLifecycle(proposal, 'approve')}>Approve</Button>}
                          {proposal.status === 'proposed' && <Button variant="ghost" size="sm" onClick={() => queueProposalLifecycle(proposal, 'reject')}>Reject</Button>}
                          {proposal.status === 'canary' && <Button variant="outline" size="sm" onClick={() => queueProposalLifecycle(proposal, 'promote')}>Promote</Button>}
                          {['canary', 'dispatching', 'active', 'failed'].includes(proposal.status) && <Button variant="ghost" size="sm" onClick={() => queueProposalLifecycle(proposal, 'rollback')}>Rollback</Button>}
                        </div>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </div>
      )}

      {webservers.length > 0 && (
        <div className="rounded border border-border">
          <div className="flex items-center justify-between gap-3 border-b border-border px-3 py-2">
            <div className="text-sm font-medium">Detected webservers</div>
            <Button asChild variant="ghost" size="sm">
              <Link to="/security/webservers">
                Open control
                <ArrowRight className="ml-1 h-3.5 w-3.5" />
              </Link>
            </Button>
          </div>
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">Kind</th>
                <th className="px-3 py-2">Service</th>
                <th className="px-3 py-2">Config</th>
                <th className="px-3 py-2">Access log</th>
                <th className="px-3 py-2">Observed</th>
              </tr>
            </thead>
            <tbody>
              {webservers.map((w) => (
                <tr key={w.ID} className="border-t border-border">
                  <td className="px-3 py-2 font-medium">{w.Kind}</td>
                  <td className="px-3 py-2">{w.ServiceName || 'default'}</td>
                  <td className="px-3 py-2 font-mono text-xs text-text-secondary">{w.ConfigPath || 'unknown'}</td>
                  <td className="px-3 py-2 font-mono text-xs text-text-secondary">{w.AccessLogPath || 'unknown'}</td>
                  <td className="px-3 py-2 text-text-secondary">{formatDateTime(w.ObservedAt)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      <ConfirmModal
        open={!!confirm}
        title={confirm?.title ?? ''}
        body={confirm?.body}
        confirmLabel={confirming ? 'Working...' : confirm?.confirmLabel}
        variant={confirm?.variant}
        onConfirm={() => void runConfirmed()}
        onCancel={() => confirming ? undefined : setConfirm(null)}
      />
    </div>
  );
}

function countryBackendScore(country: IPBehaviorCountrySummary, findings: BehavioralAnomaly[]): number {
  return findingScore(countryTopFinding(country, findings));
}

function countryTopFinding(country: IPBehaviorCountrySummary, findings: BehavioralAnomaly[]): BehavioralAnomaly | undefined {
  const code = (country.country_code || '').toUpperCase();
  const asns = new Set((country.top_asns ?? []).map((asn) => asn.toUpperCase()));
  return findings
    .filter((finding) => {
      if (code && (finding.country_code || '').toUpperCase() === code) return true;
      if (finding.asn && asns.has(finding.asn.toUpperCase())) return true;
      return false;
    })
    .sort((a, b) => findingScore(b) - findingScore(a))[0];
}

function countryBackendReason(country: IPBehaviorCountrySummary, findings: BehavioralAnomaly[]): string {
  const finding = countryTopFinding(country, findings);
  if (finding?.reason) return finding.reason;
  return `${countryLabel(country)} has no open backend anomaly finding.`;
}

function maxBackendScore(findings: BehavioralAnomaly[]): number {
  return Math.max(0, ...findings.map((finding) => findingScore(finding)));
}

function findingScore(finding?: BehavioralAnomaly): number {
  if (!finding) return 0;
  if (Number.isFinite(finding.observed_value)) {
    return Math.max(0, Math.min(100, Math.round(finding.observed_value)));
  }
  if (Number.isFinite(finding.z_score)) {
    return Math.max(0, Math.min(100, Math.round(finding.z_score * 20)));
  }
  return 0;
}

function countryBaselineInsight(country: IPBehaviorCountrySummary, baseline?: IPBehaviorBaseline | null, windowKey: TimeWindowKey = '1h'): { tone: StateTone; label: string; description: string } {
  const hours = WINDOW_HOURS[windowKey] || 1;
  const hourlyRequests = country.request_count / hours;
  const hourlyBytes = country.bytes_out / hours;
  const samples = baseline ? baselineSampleCount(baseline) : 0;
  if (!baseline || samples < 5) {
    return {
      tone: 'unknown',
      label: 'Insufficient baseline',
      description: `${countryLabel(country)} has ${formatNumber(country.request_count)} requests and ${formatBytes(country.bytes_out)} out in this window, but only ${formatNumber(samples)} baseline samples are available.`,
    };
  }
  const reqP95 = baselineMetric(baseline, 'request_count', 'p95');
  const reqPeak = baselineMetric(baseline, 'request_count', 'peak');
  const bytesP99 = baselineMetric(baseline, 'bytes_out', 'p99');
  const authBaseline = baselineStatusCount(baseline, '401') + baselineStatusCount(baseline, '403');
  const auth = authCount(country.status_counts);
  const overReq = reqP95 > 0 && hourlyRequests > reqP95;
  const overBytes = bytesP99 > 0 && hourlyBytes > bytesP99;
  const authSpike = authBaseline > 0 ? auth > authBaseline * 2 : auth >= 10;
  const tone: StateTone = overBytes || (overReq && authSpike) ? 'critical' : overReq || authSpike ? 'warning' : 'healthy';
  const label = tone === 'healthy' ? 'Inside baseline' : tone === 'critical' ? 'Critical deviation' : 'Baseline deviation';
  return {
    tone,
    label,
    description: `${countryLabel(country)} is running at ${formatNumber(Math.round(hourlyRequests))} requests/hour against p95 ${formatNumber(Math.round(reqP95))} and peak ${formatNumber(Math.round(reqPeak))}; bytes out is ${formatBytes(Math.round(hourlyBytes))}/hour against p99 ${formatBytes(Math.round(bytesP99))}; auth failures are ${formatNumber(auth)}.`,
  };
}

function profileBaselineInsight(profile: IPBehaviorIPProfile, baseline?: IPBehaviorBaseline | null): { tone: StateTone; label: string; description: string } {
  const samples = baseline ? baselineSampleCount(baseline) : 0;
  if (!baseline || samples < 5) {
    return {
      tone: 'unknown',
      label: 'Insufficient baseline',
      description: `${profile.source_ip} has ${formatNumber(profile.request_count)} requests and ${formatBytes(profile.bytes_out)} out, but only ${formatNumber(samples)} matching source-IP baseline samples are available.`,
    };
  }
  const reqP99 = baselineMetric(baseline, 'request_count', 'p99');
  const bytesP99 = baselineMetric(baseline, 'bytes_out', 'p99');
  const overReq = reqP99 > 0 && profile.request_count > reqP99;
  const overBytes = bytesP99 > 0 && profile.bytes_out > bytesP99;
  const tone: StateTone = overBytes ? 'critical' : overReq || authCount(profile.status_counts) > 0 ? 'warning' : 'healthy';
  const label = tone === 'healthy' ? 'Inside baseline' : tone === 'critical' ? 'Exfiltration risk' : 'Behavior shift';
  return {
    tone,
    label,
    description: `${profile.source_ip} has ${formatNumber(profile.request_count)} requests against source/app p99 ${formatNumber(Math.round(reqP99))}; bytes out is ${formatBytes(profile.bytes_out)} against p99 ${formatBytes(Math.round(bytesP99))}; affected servers ${formatNumber(profile.node_ids?.length ?? 0)}.`,
  };
}

function findCountryBaseline(country: IPBehaviorCountrySummary, baselines: IPBehaviorBaseline[], filters: IPBehaviorFilters): IPBehaviorBaseline | null {
  const code = (country.country_code || '').toUpperCase();
  if (!code) return null;
  return bestBaseline(baselines.filter((row) => {
    const dim = baselineDimension(row);
    const key = baselineDimensionKey(row);
    if (!dim.includes('country_app') || !key.toUpperCase().endsWith(`|${code}`)) return false;
    if (filters.serverGroup && !includesFold(key, filters.serverGroup)) return false;
    if (filters.app && !includesFold(key, filters.app)) return false;
    return true;
  }));
}

function findIPBaseline(profile: IPBehaviorIPProfile, baselines: IPBehaviorBaseline[], filters: IPBehaviorFilters): IPBehaviorBaseline | null {
  const ip = profile.source_ip;
  return bestBaseline(baselines.filter((row) => {
    const dim = baselineDimension(row);
    const key = baselineDimensionKey(row);
    if (!dim.includes('source_ip_app') || !key.endsWith(`|${ip}`)) return false;
    if (filters.app && !includesFold(key, filters.app)) return false;
    return true;
  }));
}

function bestBaseline(rows: IPBehaviorBaseline[]): IPBehaviorBaseline | null {
  if (rows.length === 0) return null;
  return [...rows].sort((a, b) => baselineSampleCount(b) - baselineSampleCount(a))[0];
}

function baselinePayload(row: IPBehaviorBaseline): Record<string, unknown> {
  return row.baseline ?? row.Baseline ?? {};
}

function baselineDimension(row: IPBehaviorBaseline): string {
  return String(row.dimension ?? row.Dimension ?? '');
}

function baselineDimensionKey(row: IPBehaviorBaseline): string {
  return String(row.dimension_key ?? row.DimensionKey ?? '');
}

function baselineSampleCount(row: IPBehaviorBaseline): number {
  return Number(row.sample_count ?? row.SampleCount ?? baselinePayload(row).sample_count ?? 0);
}

function baselineMetric(row: IPBehaviorBaseline, metric: 'request_count' | 'bytes_out', field: 'avg' | 'p95' | 'p99' | 'peak'): number {
  const payload = baselinePayload(row);
  const value = payload[metric];
  if (!value || typeof value !== 'object') return 0;
  const raw = (value as Record<string, unknown>)[field];
  return Number(raw ?? 0);
}

function baselineStatusCount(row: IPBehaviorBaseline, status: string): number {
  const counts = baselinePayload(row).status_counts;
  if (!counts || typeof counts !== 'object') return 0;
  return Number((counts as Record<string, unknown>)[status] ?? 0);
}

function authCount(statusCounts: Record<string, number> = {}): number {
  return (statusCounts['401'] ?? 0) + (statusCounts['403'] ?? 0);
}

function serverErrorCount(statusCounts: Record<string, number> = {}): number {
  return (statusCounts['500'] ?? 0) + (statusCounts['502'] ?? 0) + (statusCounts['503'] ?? 0) + (statusCounts['5xx'] ?? 0);
}

function riskTone(score: number): StateTone {
  if (score >= 85) return 'critical';
  if (score >= 70) return 'degraded';
  if (score >= 50) return 'warning';
  if (score > 0) return 'unknown';
  return 'healthy';
}

function blockStatusTone(status: IPBlockProposal['status']): StateTone {
  if (status === 'active') return 'healthy';
  if (status === 'failed' || status === 'denied') return 'critical';
  if (status === 'dispatching' || status === 'canary' || status === 'approved') return 'warning';
  if (status === 'expired' || status === 'removed' || status === 'rolled_back' || status === 'rejected') return 'unknown';
  return 'info';
}

function findingSeverityTone(severity?: string): StateTone {
  if (severity === 'critical') return 'critical';
  if (severity === 'high') return 'degraded';
  if (severity === 'medium') return 'warning';
  if (severity === 'low') return 'info';
  return 'unknown';
}

function severityLabel(score: number, finding?: BehavioralAnomaly): string {
  if (finding?.severity) return finding.severity;
  if (score <= 0) return 'normal';
  if (score >= 85) return 'critical';
  if (score >= 70) return 'high';
  if (score >= 50) return 'suspicious';
  return 'watch';
}

function countryLabel(country: IPBehaviorCountrySummary): string {
  return country.country || country.country_code || 'Unknown';
}

function compactList(values?: Array<string | undefined | null>): string {
  return (values ?? []).filter((value): value is string => !!value && value.trim().length > 0).slice(0, 3).join(', ');
}

function includesFold(value: string, needle: string): boolean {
  return value.toLowerCase().includes(needle.trim().toLowerCase());
}

function windowSince(windowKey: TimeWindowKey): string {
  return new Date(Date.now() - (WINDOW_HOURS[windowKey] || 1) * 60 * 60 * 1000).toISOString();
}

function ipv4Cidr24(ip: string): string | null {
  const parts = ip.split('.');
  if (parts.length !== 4 || parts.some((part) => !/^\d+$/.test(part) || Number(part) < 0 || Number(part) > 255)) return null;
  return `${parts[0]}.${parts[1]}.${parts[2]}.0/24`;
}

function exactIPCIDR(ip: string): string {
  return ip.includes(':') ? `${ip}/128` : `${ip}/32`;
}

function scopedBlockReason(
  profile: IPBehaviorIPProfile,
  country: IPBehaviorCountrySummary | null,
  baseline: IPBehaviorBaseline | null,
  filters: IPBehaviorFilters,
  target: 'ip' | 'cidr' | 'vhost',
): string {
  const insight = profileBaselineInsight(profile, baseline).description;
  const scope = target === 'vhost' ? ` scoped to vhost ${filters.vhost}` : target === 'cidr' ? ' for source /24' : '';
  return `IP behavior proposal${scope}: ${profile.source_ip}; ${insight}; country=${country ? countryLabel(country) : compactList(profile.countries)}; app=${filters.app || compactList(profile.apps)}; server_group=${filters.serverGroup || compactList(profile.server_groups)}.`;
}

function asnBlockReason(profile: IPBehaviorIPProfile, country: IPBehaviorCountrySummary | null, explanation?: string): string {
  const asn = profile.asns?.[0] || 'unknown ASN';
  const context = explanation || `${formatNumber(profile.request_count)} requests and ${formatBytes(profile.bytes_out)} out`;
  return `IP behavior ASN proposal: ${asn}; seed_ip=${profile.source_ip}; ${context}; country=${country ? countryLabel(country) : compactList(profile.countries)}; apps=${compactList(profile.apps)}; server_groups=${compactList(profile.server_groups)}.`;
}

function askAIPrompt(profile: IPBehaviorIPProfile, explanation: string): string {
  return `Analyze IP behavior for ${profile.source_ip}. ${explanation} Status counts: ${JSON.stringify(profile.status_counts)}. Recommend evidence to review before containment.`;
}

function formatBytes(value: number): string {
  if (!Number.isFinite(value) || value <= 0) return '0 B';
  const units = ['B', 'KiB', 'MiB', 'GiB', 'TiB'];
  let n = value;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i += 1;
  }
  return `${n >= 10 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`;
}

function formatNumber(value: number): string {
  return new Intl.NumberFormat().format(value || 0);
}

function formatDateTime(value?: string): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '-';
  return date.toLocaleString();
}

function ActiveBlocksPanel(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [blocks, setBlocks] = useState<ActiveBlock[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [selected, setSelected] = useState<ActiveBlock | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const resp = await client.listActiveBlocks({ tenantId: currentTenantId, limit: 100 });
      setBlocks(resp.blocks ?? []);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Active block data failed to load');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    refresh();
  }, [refresh]);

  const totals = blocks.reduce(
    (acc, b) => {
      acc.applied += b.NodesApplied;
      acc.failed += b.NodesFailed;
      acc.pending += b.NodesPending;
      return acc;
    },
    { applied: 0, failed: 0, pending: 0 },
  );

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant from the header to view active blocks." />;
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
        <KpiTile label="Active blocks" value={String(blocks.length)} />
        <KpiTile label="Nodes applied" value={String(totals.applied)} tone="healthy" />
        <KpiTile label="Nodes pending" value={String(totals.pending)} tone="warning" />
        <KpiTile label="Nodes failed" value={String(totals.failed)} tone={totals.failed > 0 ? 'critical' : 'unknown'} />
      </div>

      <div className="flex items-center justify-end gap-2">
        <Button variant="outline" size="sm" onClick={refresh} disabled={loading}>
          <RefreshCw className={`mr-2 h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
          Refresh
        </Button>
      </div>

      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}

      {!loading && blocks.length === 0 ? (
        <EmptyState
          title="No active blocks"
          description="No active IP blocks are reported by agents."
        />
      ) : (
        <div className="rounded border border-border">
          <table className="w-full text-sm">
            <thead className="bg-surface-2 text-left text-xs uppercase tracking-wider text-text-secondary">
              <tr>
                <th className="px-3 py-2">IP</th>
                <th className="px-3 py-2">Action</th>
                <th className="px-3 py-2">Status</th>
                <th className="px-3 py-2">Applied / Total</th>
                <th className="px-3 py-2">Reason</th>
                <th className="px-3 py-2">Created</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {blocks.map((b) => {
                const tone = b.NodesFailed > 0 ? 'critical' : b.NodesPending > 0 ? 'warning' : 'healthy';
                const status = b.NodesFailed > 0 ? 'partial' : b.NodesPending > 0 ? 'pending' : 'applied';
                return (
                  <tr key={b.EntityActionID} className="border-t border-border hover:bg-hover">
                    <td className="px-3 py-2 font-mono text-xs">{b.EntityID}</td>
                    <td className="px-3 py-2">{b.Action}</td>
                    <td className="px-3 py-2">
                      <StatusTag tone={tone}>{status}</StatusTag>
                    </td>
                    <td className="px-3 py-2">
                      {b.NodesApplied}/{b.TotalNodes}
                    </td>
                    <td className="px-3 py-2 text-text-secondary">{b.Reason ?? '—'}</td>
                    <td className="px-3 py-2 text-text-secondary">{new Date(b.CreatedAt).toLocaleString()}</td>
                    <td className="px-3 py-2 text-right">
                      <Button variant="ghost" size="sm" onClick={() => setSelected(b)}>
                        Per-node
                      </Button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {selected && <BlockNodeDetail block={selected} onClose={() => setSelected(null)} />}
    </div>
  );
}

function BlockNodeDetail({ block, onClose }: { block: ActiveBlock; onClose: () => void }): JSX.Element {
  const client = useApiClient();
  const [rules, setRules] = useState<NodeFirewallRule[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let cancel = false;
    (async () => {
      setLoading(true);
      try {
        const resp = await client.listBlockNodes(block.EntityActionID);
        if (!cancel) setRules(resp.rules ?? []);
      } catch (err) {
        if (!cancel) setError(err instanceof Error ? err.message : 'Firewall data failed to load');
      } finally {
        if (!cancel) setLoading(false);
      }
    })();
    return () => {
      cancel = true;
    };
  }, [block.EntityActionID, client]);

  return (
    <aside className="fixed right-0 top-0 z-40 h-full w-full max-w-xl overflow-y-auto border-l border-border bg-elevated p-6 shadow-2xl">
      <div className="mb-4 flex items-start justify-between">
        <div>
          <p className="text-xs uppercase tracking-wider text-text-secondary">Per-node fan-out</p>
          <h3 className="font-mono text-lg">{block.EntityID}</h3>
        </div>
        <Button variant="ghost" size="sm" onClick={onClose}>
          Close
        </Button>
      </div>
      {loading && <Skeleton className="h-32 w-full" />}
      {error && <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div>}
      {!loading && rules.length === 0 && <EmptyState title="No nodes" description="No nodes received this block." />}
      {rules.length > 0 && (
        <table className="w-full text-sm">
          <thead className="text-left text-xs uppercase tracking-wider text-text-secondary">
            <tr>
              <th className="px-2 py-1">Node</th>
              <th className="px-2 py-1">Status</th>
              <th className="px-2 py-1">Error</th>
              <th className="px-2 py-1">Applied</th>
            </tr>
          </thead>
          <tbody>
            {rules.map((r) => (
              <tr key={r.ID} className="border-t border-border">
                <td className="px-2 py-1 font-mono text-xs">{r.NodeID.slice(0, 8)}</td>
                <td className="px-2 py-1">
                  <StatusTag tone={statusTone(r.Status)}>{r.Status}</StatusTag>
                </td>
                <td className="px-2 py-1 text-xs text-text-secondary">{r.Error ?? '—'}</td>
                <td className="px-2 py-1 text-xs text-text-secondary">{r.AppliedAt ? new Date(r.AppliedAt).toLocaleString() : '—'}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </aside>
  );
}

function statusTone(s: NodeFirewallRule['Status']): 'healthy' | 'warning' | 'critical' | 'unknown' {
  switch (s) {
    case 'applied':
      return 'healthy';
    case 'pending':
      return 'warning';
    case 'failed':
      return 'critical';
    case 'removed':
    default:
      return 'unknown';
  }
}

// ── Firewall Management ──────────────────────────────────────────────────

function FirewallManagementPanel(): JSX.Element {
  const client = useApiClient();
  const { currentTenantId } = useTenant();
  const [overview, setOverview] = useState<ControlRoomOverview | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const refresh = useCallback(async () => {
    if (!currentTenantId) return;
    setLoading(true);
    setError(null);
    try {
      const next = await client.getControlRoomOverview(currentTenantId, '24h');
      setOverview(next);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Firewall posture failed to load');
    } finally {
      setLoading(false);
    }
  }, [client, currentTenantId]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  if (!currentTenantId) {
    return <EmptyState title="Select a tenant" description="Choose a tenant to view firewall posture." />;
  }

  const firewall = overview?.firewall;
  const nodes = firewall?.nodes ?? [];
  const unknownOrOff = (firewall?.unknown ?? 0) + (firewall?.disabled ?? 0);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="text-base font-semibold text-foreground">Host firewall posture</h3>
          <p className="mt-1 max-w-3xl text-sm text-text-secondary">
            Default-deny firewalls reduce current exposure. Unknown, off, and stale states stay visible until the agent reports protection.
          </p>
        </div>
        <div className="flex flex-wrap gap-2">
          <Button asChild variant="outline" size="sm">
            <Link to="/control-room/exposure">
              Control Room exposure
              <ArrowRight className="h-4 w-4" />
            </Link>
          </Button>
          <Button variant="outline" size="sm" onClick={() => void refresh()} disabled={loading}>
            <RefreshCw className={`h-4 w-4 ${loading ? 'animate-spin' : ''}`} />
            Refresh
          </Button>
        </div>
      </div>

      {error ? <div className="rounded border border-destructive/50 bg-destructive/10 p-3 text-sm">{error}</div> : null}

      {loading && !overview ? (
        <Skeleton className="h-64 w-full" />
      ) : (
        <>
          <div className="grid grid-cols-1 gap-3 md:grid-cols-4">
            <KpiTile label="Firewall active" value={formatNumber(firewall?.enabled ?? 0)} tone={(firewall?.enabled ?? 0) > 0 ? 'healthy' : 'unknown'} />
            <KpiTile label="Default deny" value={formatNumber(firewall?.default_deny ?? 0)} tone={(firewall?.default_deny ?? 0) > 0 ? 'healthy' : 'warning'} />
            <KpiTile label="Unknown/off" value={formatNumber(unknownOrOff)} tone={unknownOrOff > 0 ? 'warning' : 'healthy'} />
            <KpiTile label="Stale reports" value={formatNumber(firewall?.stale ?? 0)} tone={(firewall?.stale ?? 0) > 0 ? 'warning' : 'healthy'} />
          </div>

          <Panel eyebrow="NODE POSTURE" title="Firewall state by node" toneAccent={unknownOrOff > 0 || (firewall?.stale ?? 0) > 0 ? 'warning' : 'healthy'}>
            {nodes.length === 0 ? (
              <EmptyState
                icon={<ShieldAlert className="h-8 w-8" />}
                title="No firewall reports"
                description="No node agent has reported host firewall state in the selected window."
              />
            ) : (
              <div className="overflow-x-auto rounded-lg border border-border-subtle">
                <table className="w-full text-sm">
                  <thead className="bg-surface-2 text-left text-xs uppercase tracking-wide text-text-secondary">
                    <tr>
                      <th className="px-3 py-2">Node</th>
                      <th className="px-3 py-2">Firewall</th>
                      <th className="px-3 py-2">State</th>
                      <th className="px-3 py-2">Exposure effect</th>
                      <th className="px-3 py-2">Observed</th>
                    </tr>
                  </thead>
                  <tbody>
                    {nodes.map((node) => (
                      <tr key={node.node_id} className="border-t border-border-subtle hover:bg-hover">
                        <td className="px-3 py-2">
                          <Link to={`/nodes/${node.node_id}`} className="font-medium text-brand-400 hover:underline">
                            {node.hostname || node.node_id}
                          </Link>
                        </td>
                        <td className="px-3 py-2 text-text-secondary">{node.firewall_type || 'not reported'}</td>
                        <td className="px-3 py-2">
                          <StatusTag tone={firewallPostureTone(node)}>{firewallPostureLabel(node)}</StatusTag>
                        </td>
                        <td className="px-3 py-2 text-text-secondary">{firewallExposureEffect(node)}</td>
                        <td className="px-3 py-2 text-text-secondary">{formatDateTime(node.observed_at)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            )}
          </Panel>
        </>
      )}
    </div>
  );
}

function firewallPostureTone(node: ControlRoomFirewallNode): StateTone {
  if (!node.known || !node.enabled || node.stale) return 'warning';
  if (node.default_deny) return 'healthy';
  return 'info';
}

function firewallPostureLabel(node: ControlRoomFirewallNode): string {
  if (!node.known) return 'unknown';
  if (!node.enabled) return 'off';
  if (node.stale) return 'stale';
  if (node.default_deny) return 'default deny';
  return 'enabled';
}

function firewallExposureEffect(node: ControlRoomFirewallNode): string {
  if (!node.known) return 'firewall state unknown';
  if (!node.enabled) return 'not reducing exposure';
  if (node.stale) return 'needs fresh agent report';
  if (node.default_deny) return 'counts as protected';
  return 'rules active, default allow';
}

export default NetworkSecurity;
