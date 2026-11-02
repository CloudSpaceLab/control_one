import {act,fireEvent,render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {afterEach,beforeEach,expect,it,vi} from 'vitest';
import {onlineNode} from '@/lib/onlinePatchPlan.testFixtures';
import {OnlinePlanPreparation} from './OnlinePlanPreparation';

const client=vi.hoisted(()=>({getOnlinePreparation:vi.fn(),prepareOnlinePlan:vi.fn()}));
vi.mock('@/hooks/useApiClient',()=>({useApiClient:()=>client}));
beforeEach(()=>{vi.clearAllMocks();client.getOnlinePreparation.mockResolvedValue({status:'idle'});client.prepareOnlinePlan.mockResolvedValue({job_id:'prepare-1',status:'running'});});
afterEach(()=>vi.useRealTimers());

it('queues bounded sorted scope for one target and the explicit transport without installing',async()=>{
  const user=userEvent.setup();const node=onlineNode();const selected=vi.fn();render(<OnlinePlanPreparation nodes={[node]} mode="proxy" onPrepared={vi.fn()} onSelected={selected}/>);
  await user.selectOptions(screen.getByRole('combobox',{name:'Online preparation target'}),node.id);
  await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare exact online plan'})).toBeEnabled());
  await user.type(screen.getByRole('textbox',{name:'Installed package names'}),'libb, liba liba');
  await user.click(screen.getByRole('button',{name:'Prepare exact online plan'}));
  expect(client.prepareOnlinePlan).toHaveBeenCalledWith(node.id,'proxy',['liba','libb']);expect(selected).toHaveBeenLastCalledWith(null);expect(await screen.findByText(/Preparation: running/)).toBeVisible();
});

it('keeps the published scope explicit and prevents selecting it during replacement preparation',async()=>{
  const user=userEvent.setup();const node=onlineNode();render(<OnlinePlanPreparation nodes={[node]} mode="direct" onPrepared={vi.fn()} onSelected={vi.fn()}/>);
  await user.selectOptions(screen.getByRole('combobox',{name:'Online preparation target'}),node.id);
  await waitFor(()=>expect(screen.getByRole('button',{name:'Prepare exact online plan'})).toBeEnabled());
  expect(screen.getByText(/Available prepared scope:/)).toHaveTextContent('liba');
  await user.type(screen.getByRole('textbox',{name:'Installed package names'}),'libb');
  await user.click(screen.getByRole('button',{name:'Prepare exact online plan'}));
  expect(screen.getByRole('button',{name:'Use prepared direct plan for online-lab'})).toBeDisabled();
});

it('keeps a polling failure visible and retries without reporting success',async()=>{
  vi.useFakeTimers();const node=onlineNode();const prepared=vi.fn();client.getOnlinePreparation.mockResolvedValueOnce({job_id:'prepare-1',status:'running'}).mockRejectedValue(new Error('connection lost'));
  render(<OnlinePlanPreparation nodes={[node]} mode="direct" onPrepared={prepared} onSelected={vi.fn()}/>);
  await act(async()=>{fireEvent.change(screen.getByRole('combobox',{name:'Online preparation target'}),{target:{value:node.id}});});
  await act(async()=>{await vi.advanceTimersByTimeAsync(3000);});
  expect(screen.getByRole('alert')).toHaveTextContent('Retrying automatically');expect(screen.getByText(/Preparation: running/)).toBeVisible();expect(prepared).not.toHaveBeenCalled();
});

it('ignores a late response from the previous mode',async()=>{
  let finish!:(v:unknown)=>void;client.getOnlinePreparation.mockReturnValueOnce(new Promise(resolve=>{finish=resolve;})).mockResolvedValue({status:'idle'});
  const node=onlineNode();const prepared=vi.fn();const select=vi.fn();const view=render(<OnlinePlanPreparation nodes={[node]} mode="direct" onPrepared={prepared} onSelected={select}/>);
  await act(async()=>{fireEvent.change(screen.getByRole('combobox',{name:'Online preparation target'}),{target:{value:node.id}});});
  view.rerender(<OnlinePlanPreparation nodes={[node]} mode="proxy" onPrepared={prepared} onSelected={select}/>);
  await act(async()=>{finish({status:'succeeded',job_id:'old-mode'});});expect(prepared).not.toHaveBeenCalled();expect(screen.queryByText(/old-mode/)).not.toBeInTheDocument();
});
