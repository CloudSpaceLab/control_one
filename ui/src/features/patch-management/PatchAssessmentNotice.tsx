import { Alert } from '@/components/kit';
import type { PatchDeploymentSummary } from '@/lib/api';

export function PatchAssessmentNotice({ assessment }: { assessment?: PatchDeploymentSummary['assessment'] }) {
  return (
    <Alert variant="warning" title={assessment?.status === 'partial'
      ? 'Assessment coverage is partial'
      : assessment?.status === 'expired' ? 'Vulnerability feed expired' : 'Assessment status unavailable'}>
      {assessment?.status === 'partial'
        ? `${assessment.feed_count} current feed(s) passed validation. Coverage of all installed products and versions has not been established. A current feed is not proof of a complete assessment.`
        : 'A current assessment cannot be confirmed. Existing findings are retained; refresh trusted feed content and reassess.'}
      {' '}Zero known findings does not establish that a machine is free of vulnerabilities. Inventory freshness and package installation results are separate evidence.
    </Alert>
  );
}
