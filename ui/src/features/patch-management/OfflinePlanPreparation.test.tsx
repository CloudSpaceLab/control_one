import {act,fireEvent,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach,describe,expect,it,vi} from 'vitest';
import {OfflinePlanPreparation} from './OfflinePlanPreparation';
import type {NodeSummary} from '../../lib/api';

const mocks=vi.hoisted(()=>({getOfflineRepository:vi.fn(),getOfflinePreparation:vi.fn(),saveOfflineRepository:vi.fn(),prepareOfflinePlan:vi.fn()}));
vi.mock('../../hooks/useApiClient',()=>({useApiClient:()=>mocks}));
const nodes=[{id:'lab',hostname:'ubuntu-lab',state:'active'}] as NodeSummary[];
const repo={id:'repo',name:'Approved Ubuntu',keyring_sha256:'a'.repeat(64),suite:'noble-updates',component:'main',enabled:true};

describe('app-driven offline preparation',()=>{
  beforeEach(()=>{
    vi.resetAllMocks();
    mocks.getOfflineRepository.mockResolvedValue({repository:repo,repository_path:'/var/cache/control-one/offline-sources/repo/repository'});
    mocks.getOfflinePreparation.mockResolvedValue({status:'idle'});
    mocks.prepareOfflinePlan.mockResolvedValue({job_id:'job',status:'succeeded',message:'Prepared without installing'});
  });

  it('prepares only an exact scope using the registered repository and refreshes published plans',async()=>{
    const user=userEvent.setup();const done=vi.fn();render(<OfflinePlanPreparation nodes={nodes} onPrepared={done}/>);
    await user.click(screen.getByText('Prepare a new offline plan'));
    await user.selectOptions(screen.getByRole('combobox',{name:'Preparation target'}),'lab');
    await screen.findByText('Approved repository: Approved Ubuntu');
    await user.type(screen.getByRole('textbox',{name:'Exact packages to prepare'}),'libudev1=255.4-1ubuntu8.18');
    await user.click(screen.getByRole('button',{name:'Prepare offline plan'}));
    await waitFor(()=>expect(mocks.prepareOfflinePlan).toHaveBeenCalledWith('lab','repo',[{name:'libudev1',version:'255.4-1ubuntu8.18'}]));
    await waitFor(()=>expect(done).toHaveBeenCalledTimes(1));
    expect(screen.getByText(/does not install packages/)).toBeInTheDocument();
  });

  it('keeps preparation disabled until a repository is registered, and supports administrator registration',async()=>{
    mocks.getOfflineRepository.mockResolvedValue({repository:null});mocks.saveOfflineRepository.mockResolvedValue({repository:repo,repository_path:'/approved/content'});
    const user=userEvent.setup();render(<OfflinePlanPreparation nodes={nodes} onPrepared={vi.fn()}/>);
    await user.click(screen.getByText('Prepare a new offline plan'));
    await user.selectOptions(screen.getByRole('combobox',{name:'Preparation target'}),'lab');
    await screen.findByText('Approved repository: Not registered');
    expect(screen.getByRole('button',{name:'Prepare offline plan'})).toBeDisabled();
    await user.click(screen.getByText('Administrator: register approved repository'));
    await user.type(screen.getByRole('textbox',{name:'Repository name'}),'Approved Ubuntu');
    await user.type(screen.getByRole('textbox',{name:'Trusted keyring SHA-256'}),'a'.repeat(64));
    await user.click(screen.getByRole('button',{name:'Save approved repository'}));
    await waitFor(()=>expect(mocks.saveOfflineRepository).toHaveBeenCalledWith('lab',expect.objectContaining({name:'Approved Ubuntu',keyring_sha256:'a'.repeat(64),enabled:true})));
    expect(await screen.findByText('/approved/content')).toBeInTheDocument();
  });

  it('keeps a failed preparation visible without refreshing or claiming a new plan',async()=>{
    mocks.prepareOfflinePlan.mockResolvedValue({job_id:'failed',status:'failed',message:'Signed repository content missing'});
    const user=userEvent.setup();const done=vi.fn();render(<OfflinePlanPreparation nodes={nodes} onPrepared={done}/>);
    await user.click(screen.getByText('Prepare a new offline plan'));
    await user.selectOptions(screen.getByRole('combobox',{name:'Preparation target'}),'lab');
    await screen.findByText('Approved repository: Approved Ubuntu');
    await user.type(screen.getByRole('textbox',{name:'Exact packages to prepare'}),'libudev1=1.2');
    await user.click(screen.getByRole('button',{name:'Prepare offline plan'}));
    expect(await screen.findByText(/Signed repository content missing/)).toBeInTheDocument();
    expect(done).not.toHaveBeenCalled();
  });

  it('clears a transient polling error after reconnection and keeps the terminal failure',async()=>{
    mocks.prepareOfflinePlan.mockResolvedValue({job_id:'waiting',status:'running'});
    const user=userEvent.setup();const done=vi.fn();render(<OfflinePlanPreparation nodes={nodes} onPrepared={done}/>);
    await user.click(screen.getByText('Prepare a new offline plan'));
    await user.selectOptions(screen.getByRole('combobox',{name:'Preparation target'}),'lab');
    await screen.findByText('Approved repository: Approved Ubuntu');
    await user.type(screen.getByRole('textbox',{name:'Exact packages to prepare'}),'libudev1=1.2');
    vi.useFakeTimers();
    try {
      await act(async()=>{fireEvent.click(screen.getByRole('button',{name:'Prepare offline plan'}));});
      mocks.getOfflinePreparation.mockRejectedValueOnce(new Error('Service restarting')).mockResolvedValue({job_id:'waiting',status:'failed',message:'Preparation timed out'});
      await act(async()=>{await vi.advanceTimersByTimeAsync(3000);});
      expect(screen.getByRole('alert')).toHaveTextContent('Retrying automatically');
      await act(async()=>{await vi.advanceTimersByTimeAsync(3000);});
      expect(screen.queryByRole('alert')).not.toBeInTheDocument();
      expect(screen.getByRole('status')).toHaveTextContent('Preparation timed out');
      expect(done).not.toHaveBeenCalled();
    } finally {vi.useRealTimers();}
  });
});
