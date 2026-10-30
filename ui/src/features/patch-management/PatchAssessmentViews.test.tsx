import { act, render, screen } from '@testing-library/react';
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { NodePatchAssessmentNotice, TenantPatchAssessmentNotice } from './PatchAssessmentViews';

const api=vi.hoisted(()=>({getPatchSummary:vi.fn(),listNodePackages:vi.fn()}));
vi.mock('@/hooks/useApiClient',()=>({useApiClient:()=>api}));

describe('assessment evidence across operator views',()=>{
  beforeEach(()=>vi.resetAllMocks());
  it('uses the same tenant coverage evidence as Patch posture',async()=>{
    api.getPatchSummary.mockResolvedValue({assessment:{status:'partial',feed_count:2}});
    render(<TenantPatchAssessmentNotice tenantId="tenant-a"/>);
    expect(await screen.findByText('Assessment coverage is partial')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('2 current feed(s)');
    expect(api.getPatchSummary).toHaveBeenCalledWith('tenant-a');
  });
  it('shows unavailable evidence after a failed coverage request',async()=>{
    api.getPatchSummary.mockRejectedValue(new Error('offline'));
    render(<TenantPatchAssessmentNotice tenantId="tenant-a"/>);
    expect(await screen.findByText('Assessment status unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('Zero known findings does not establish');
  });
  it('reloads coverage when Control Room is refreshed',async()=>{
    api.getPatchSummary.mockResolvedValueOnce({assessment:{status:'partial',feed_count:2}}).mockResolvedValueOnce({assessment:{status:'expired',feed_count:0}});
    const view=render(<TenantPatchAssessmentNotice tenantId="tenant-a" refreshKey="first"/>);
    await screen.findByText('Assessment coverage is partial');
    view.rerender(<TenantPatchAssessmentNotice tenantId="tenant-a" refreshKey="second"/>);
    expect(await screen.findByText('Vulnerability feed expired')).toBeInTheDocument();
    expect(api.getPatchSummary).toHaveBeenCalledTimes(2);
  });
  it('does not show a late response from the previous tenant',async()=>{
    let resolveOld!:(value:unknown)=>void;
    api.getPatchSummary.mockImplementation((id:string)=>id==='old'?new Promise(resolve=>{resolveOld=resolve;}):Promise.resolve({assessment:{status:'partial',feed_count:3}}));
    const view=render(<TenantPatchAssessmentNotice tenantId="old"/>);
    view.rerender(<TenantPatchAssessmentNotice tenantId="new"/>);
    await screen.findByText('Assessment coverage is partial');
    await act(async()=>{resolveOld({assessment:{status:'partial',feed_count:99}});});
    expect(screen.getByRole('alert')).toHaveTextContent('3 current feed(s)');
    expect(screen.getByRole('alert')).not.toHaveTextContent('99 current');
  });
  it('keeps failed node evidence unavailable instead of showing a verified fix',async()=>{
    api.listNodePackages.mockRejectedValue(new Error('offline'));
    render(<NodePatchAssessmentNotice nodeId="node-a"/>);
    expect(await screen.findByText('Assessment evidence unavailable')).toBeInTheDocument();
    expect(screen.getByRole('alert')).toHaveTextContent('could not be loaded');
    expect(screen.queryByText('Execution and verified outcome')).not.toBeInTheDocument();
  });
});
