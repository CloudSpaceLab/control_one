import { useQuery } from '@tanstack/react-query';
import type { OnboardingStep } from '@/components/kit';
import { useApiClient } from './useApiClient';
import { useTenant } from '@/providers/TenantProvider';
import { useRolePick } from './useRolePick';

/**
 * useOnboardingState detects whether a freshly-provisioned tenant has done
 * the bare-minimum setup. Used by every dashboard / list page to short-
 * circuit into a guided checklist when there's no data yet.
 *
 * Heuristic:
 *   - tenants > 0
 *   - nodes > 0
 *   - any rule trigger or alert in the last 24h (proxy for "wired up")
 *
 * The hook never blocks rendering: while data loads `isReady` is false but
 * `steps` is still well-formed (with all done=false) so the checklist
 * renders skeleton-safe.
 */
export function useOnboardingState(): {
  isReady: boolean;
  isEmpty: boolean;
  steps: OnboardingStep[];
} {
  const client = useApiClient();
  const { tenants, currentTenantId } = useTenant();
  const { isAdmin, isOperator } = useRolePick();

  const overviewQ = useQuery({
    queryKey: ['onboarding.overview', currentTenantId],
    queryFn: () => client.getDashboardOverview(currentTenantId ?? undefined),
    staleTime: 60_000,
  });

  const hasTenant = tenants.length > 0;
  const overview = overviewQ.data;
  const hasNode = (overview?.node_counts?.total ?? 0) > 0;
  const hasRule =
    Object.values(overview?.rule_trigger_counts_24h ?? {}).reduce((a, b) => a + b, 0) > 0;
  const hasComplianceData = (overview?.compliance_summary?.total ?? 0) > 0;

  const steps: OnboardingStep[] = [
    {
      id: 'tenant',
      title: 'Create your first tenant',
      description: 'Tenants isolate data. Most teams start with one for the whole company.',
      to: '/tenants',
      cta: hasTenant ? 'View tenants' : 'Create tenant',
      done: hasTenant,
      required: true,
    },
    {
      id: 'server',
      title: 'Onboard a server',
      description: 'Connect your first host over SSH, WinRM, or RDP. We test credentials before enrolling.',
      to: '/onboard',
      cta: hasNode ? 'Manage nodes' : 'Add server',
      done: hasNode,
      required: true,
    },
    {
      id: 'rules',
      title: 'Enable detection rules',
      description: 'Pick a starter pack or write your first rule. Triggers appear on the dashboard.',
      to: isOperator || isAdmin ? '/rules' : '/recommendations',
      cta: hasRule ? 'Review rules' : 'Pick rules',
      done: hasRule,
    },
    {
      id: 'compliance',
      title: 'Run a compliance scan',
      description: 'CIS, PCI, SOC 2 — pick a framework and run a baseline scan.',
      to: '/compliance',
      cta: hasComplianceData ? 'View posture' : 'Start scan',
      done: hasComplianceData,
    },
    {
      id: 'threatfeeds',
      title: 'Subscribe to threat feeds',
      description: 'AbuseIPDB, Spamhaus, FireHOL — enrich every IP your fleet sees.',
      to: '/threat-feeds',
      cta: 'Configure feeds',
      done: false, // best-effort signal not in overview; we keep this informational
    },
  ];

  const isReady = !overviewQ.isLoading;
  const isEmpty = isReady && !hasTenant && !hasNode;

  return { isReady, isEmpty, steps };
}
