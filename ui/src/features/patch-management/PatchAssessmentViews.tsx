import { useEffect, useState } from 'react';
import { Loader } from '@/components/kit';
import { useApiClient } from '@/hooks/useApiClient';
import type { NodeAssessment, PatchDeploymentSummary, PatchHealthEvidence, PatchVerification } from '@/lib/api';
import { NodeAssessmentEvidence } from './NodeAssessmentEvidence';
import { PatchAssessmentNotice } from './PatchAssessmentNotice';
import { PatchVerificationEvidence } from './PatchVerificationEvidence';

export function TenantPatchAssessmentNotice({tenantId,refreshKey}: {tenantId: string;refreshKey?:string}) {
  const api = useApiClient();
  const [state,setState] = useState<{scope: string; assessment?: PatchDeploymentSummary['assessment']} | null>(null);
  useEffect(()=>{
    let cancelled=false;
    setState(null);
    api.getPatchSummary(tenantId).then(summary=>{if(!cancelled)setState({scope:tenantId,assessment:summary.assessment});})
      .catch(()=>{if(!cancelled)setState({scope:tenantId});});
    return()=>{cancelled=true;};
  },[api,tenantId,refreshKey]);
  if(state?.scope!==tenantId)return <Loader label="Loading patch assessment coverage..."/>;
  return <PatchAssessmentNotice assessment={state.assessment}/>;
}

export function NodePatchAssessmentNotice({nodeId}: {nodeId: string}) {
  const api=useApiClient();
  const [state,setState]=useState<{scope:string;failed?:boolean;assessment?:NodeAssessment|null;verification?:PatchVerification[]|null;health?:PatchHealthEvidence|null}|null>(null);
  useEffect(()=>{
    let cancelled=false;
    setState(null);
    api.listNodePackages(nodeId).then(result=>{if(!cancelled)setState({scope:nodeId,assessment:result.assessment,verification:result.patch_verifications,health:result.patch_health});})
      .catch(()=>{if(!cancelled)setState({scope:nodeId,failed:true});});
    return()=>{cancelled=true;};
  },[api,nodeId]);
  if(state?.scope!==nodeId)return <Loader label="Loading node patch assessment..."/>;
  return <div className="space-y-3"><NodeAssessmentEvidence assessment={state.assessment} loadFailed={state.failed}/>{!state.failed && <PatchVerificationEvidence records={state.verification??[]} health={state.health??null}/>}</div>;
}
