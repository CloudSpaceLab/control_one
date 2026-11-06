import {act, render, screen, waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {beforeEach, expect, it, vi} from 'vitest';
import {GlobalRoleSources} from './GlobalRoleSources';
const api=vi.hoisted(()=>({getGlobalRoleSources:vi.fn(),reviewGlobalRoleSource:vi.fn(),changeAdministratorRoleSource:vi.fn(),migrateAuthenticationIdentity:vi.fn()}));
vi.mock('../../hooks/useApiClient',()=>({useApiClient:()=>api}));
const fixture={revision:'reviewed-snapshot',effective_roles:['viewer'],grants:[{assignment_id:'assignment',role:'viewer',source:'legacy_unknown',expires_at:null}]};
beforeEach(()=>{vi.resetAllMocks();api.getGlobalRoleSources.mockResolvedValue(fixture);});
it('requires a reason and submits the exact reviewed assignment and snapshot',async()=>{
 const user=userEvent.setup();const changed=vi.fn();api.reviewGlobalRoleSource.mockResolvedValue({...fixture,revision:'next',grants:[{...fixture.grants[0],source:'administrator'}]});
 render(<GlobalRoleSources userID="existing-user" onReviewed={changed}/>);
 const action=await screen.findByRole('button',{name:'Record administrator ownership of viewer'});expect(action).toBeDisabled();
 await user.type(screen.getByLabelText('Role source review reason'),'Reviewed existing change authorization');await user.click(action);
 await waitFor(()=>expect(api.reviewGlobalRoleSource).toHaveBeenCalledWith('existing-user',{assignment_id:'assignment',expected_revision:'reviewed-snapshot',reason:'Reviewed existing change authorization'}));
 await waitFor(()=>expect(changed).toHaveBeenCalledOnce());expect(screen.queryByRole('button',{name:'Record administrator ownership of viewer'})).not.toBeInTheDocument();
});
it('removes the reviewed draft after a conflict until refreshed',async()=>{
 const user=userEvent.setup();api.reviewGlobalRoleSource.mockRejectedValue(new Error('Role evidence changed'));
 render(<GlobalRoleSources userID="existing-user" onReviewed={vi.fn()}/>);
 await screen.findByRole('button',{name:'Record administrator ownership of viewer'});await user.type(screen.getByLabelText('Role source review reason'),'Reviewed existing change authorization');
 await user.click(screen.getByRole('button',{name:'Record administrator ownership of viewer'}));expect(await screen.findByRole('alert')).toHaveTextContent('Role evidence changed');expect(screen.queryByRole('button',{name:'Record administrator ownership of viewer'})).not.toBeInTheDocument();
});

it('requires a reviewed administrator-only change and preserves the returned directory grant',async()=>{
 const user=userEvent.setup();const changed=vi.fn();
 api.getGlobalRoleSources.mockResolvedValue({...fixture,administrator_changes_available:true,grants:[{...fixture.grants[0],source:'administrator'},{...fixture.grants[0],source:'directory'}]});
 api.changeAdministratorRoleSource.mockResolvedValue({...fixture,revision:'next',grants:[{...fixture.grants[0],source:'directory'}]});
 render(<GlobalRoleSources userID="existing-user" onReviewed={changed}/>);
 await screen.findByRole('button',{name:'Review revoke viewer'});
 await user.type(screen.getByLabelText('Role source review reason'),'Remove only the administrator viewer grant');
 await user.click(screen.getByRole('button',{name:'Review revoke viewer'}));
 expect(api.changeAdministratorRoleSource).not.toHaveBeenCalled();
 expect(screen.getByRole('group',{name:'Review administrator grant change'})).toHaveTextContent('Revoke administrator grant: viewer');
 await user.click(screen.getByRole('button',{name:'Apply reviewed administrator grant change'}));
 await waitFor(()=>expect(api.changeAdministratorRoleSource).toHaveBeenCalledWith('existing-user',expect.objectContaining({assignment_id:'assignment',expected_revision:'reviewed-snapshot',action:'revoke',reason:'Remove only the administrator viewer grant'})));
 await waitFor(()=>expect(changed).toHaveBeenCalledOnce());
 expect(screen.queryByRole('button',{name:'Review revoke viewer'})).not.toBeInTheDocument();
 expect(screen.getByRole('region',{name:'Global role sources'})).toHaveTextContent('directory');
});

it('validates text-entry deadlines and binds the exact UTC time to the reviewed change',async()=>{
 const user=userEvent.setup();
 api.getGlobalRoleSources.mockResolvedValue({...fixture,administrator_changes_available:true,grants:[{...fixture.grants[0],source:'administrator'}]});
 api.changeAdministratorRoleSource.mockResolvedValue({...fixture,grants:[]});
 render(<GlobalRoleSources userID="existing-user" onReviewed={vi.fn()}/>);
 await screen.findByRole('button',{name:'Enter deadline as text'});await user.click(screen.getByRole('button',{name:'Enter deadline as text'}));
 await user.type(screen.getByLabelText('Role source review reason'),'Bounded administrator deadline review');
 await user.type(screen.getByLabelText('Administrator grant expiry'),'invalid');await user.click(screen.getByRole('button',{name:'Review expiry for viewer'}));
 expect(await screen.findByRole('alert')).toHaveTextContent('future local deadline');expect(api.changeAdministratorRoleSource).not.toHaveBeenCalled();
 await user.clear(screen.getByLabelText('Administrator grant expiry'));await user.type(screen.getByLabelText('Administrator grant expiry'),'2099-01-02T10:30');
 await user.click(screen.getByRole('button',{name:'Review expiry for viewer'}));expect(api.changeAdministratorRoleSource).not.toHaveBeenCalled();
 await user.click(screen.getByRole('button',{name:'Apply reviewed administrator grant change'}));
 await waitFor(()=>expect(api.changeAdministratorRoleSource).toHaveBeenCalledWith('existing-user',expect.objectContaining({action:'set_expiry',expires_at:new Date('2099-01-02T10:30').toISOString(),expected_revision:'reviewed-snapshot'})));
});

it('labels expired source grants while showing the server-authorized effective roles',async()=>{
 api.getGlobalRoleSources.mockResolvedValue({...fixture,effective_roles:[],grants:[{...fixture.grants[0],source:'administrator',expires_at:'2020-01-01T00:00:00Z'}]});
 render(<GlobalRoleSources userID="existing-user" onReviewed={vi.fn()}/>);
 expect(await screen.findByText('Expired')).toBeVisible();
 expect(screen.getByText('None')).toBeVisible();
 expect(api.reviewGlobalRoleSource).not.toHaveBeenCalled();expect(api.changeAdministratorRoleSource).not.toHaveBeenCalled();
});

it('reloads effective authority when the displayed grant reaches its deadline',async()=>{
 vi.useFakeTimers();
 try {
  const deadline=new Date(Date.now()+1000).toISOString();
  const grants=[{...fixture.grants[0],source:'administrator',expires_at:deadline}];
  api.getGlobalRoleSources.mockResolvedValueOnce({...fixture,grants}).mockResolvedValue({...fixture,grants,effective_roles:[]});
  render(<GlobalRoleSources userID="existing-user" onReviewed={vi.fn()}/>);
  await act(async()=>{});
  expect(api.getGlobalRoleSources).toHaveBeenCalledTimes(1);
  await act(async()=>{await vi.advanceTimersByTimeAsync(1100);});
  expect(api.getGlobalRoleSources).toHaveBeenCalledTimes(2);
  expect(screen.getByText('Expired')).toBeVisible();expect(screen.getByText('None')).toBeVisible();
  expect(api.changeAdministratorRoleSource).not.toHaveBeenCalled();
 } finally {vi.useRealTimers();}
});

it('binds explicit migration confirmation to the reviewed authority, subject and grant preservation',async()=>{
 const user=userEvent.setup();api.getGlobalRoleSources.mockResolvedValue({...fixture,authentication_migration_available:true,authentication_authorities:['static']});
 api.migrateAuthenticationIdentity.mockResolvedValue({...fixture,authentication_migration_available:false});
 render(<GlobalRoleSources userID="existing-user" onReviewed={vi.fn()}/>);
 await screen.findByRole('button',{name:'Review authentication migration'});
 await user.type(screen.getByLabelText('Role source review reason'),'Approved legacy identity binding');
 await user.type(screen.getByLabelText('Migration verified subject'),'legacy-reviewer');
 expect(screen.getByRole('button',{name:'Review authentication migration'})).toBeDisabled();
 await user.click(screen.getByRole('checkbox',{name:/I authorize retaining/}));await user.click(screen.getByRole('button',{name:'Review authentication migration'}));
 await user.clear(screen.getByLabelText('Migration verified subject'));await user.type(screen.getByLabelText('Migration verified subject'),'changed-after-review');
 expect(api.migrateAuthenticationIdentity).not.toHaveBeenCalled();
 await user.click(screen.getByRole('button',{name:'Apply reviewed authentication migration'}));
 await waitFor(()=>expect(api.migrateAuthenticationIdentity).toHaveBeenCalledWith('existing-user',{expected_revision:'reviewed-snapshot',authority:'static',subject:'legacy-reviewer',reason:'Approved legacy identity binding',preserve_as_administrator:true}));
});

it('shows the backend authority block without offering an execution override',async()=>{
 api.getGlobalRoleSources.mockResolvedValue({...fixture,execution_authority:{authentication_backed:true,configured:false,binding_present:true,detail:'The current-provider authority connection is not configured. SSO-backed unattended execution remains blocked.'}});
 render(<GlobalRoleSources userID="sso-user" onReviewed={vi.fn()}/>);
 expect(await screen.findByRole('heading',{name:'Unattended patch authority'})).toBeInTheDocument();
 expect(screen.getByRole('status')).toHaveTextContent('SSO-backed unattended execution remains blocked');
 expect(screen.getByRole('status')).toHaveTextContent('Provider binding: retained');
 expect(screen.getByRole('status')).toHaveTextContent('This status does not authorize a patch');
 expect(screen.queryByRole('button',{name:/enable.*execution/i})).not.toBeInTheDocument();
});
