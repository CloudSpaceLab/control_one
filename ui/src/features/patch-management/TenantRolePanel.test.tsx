import {render,screen,waitFor} from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import {MemoryRouter} from 'react-router-dom';
import {beforeEach,it,expect,vi} from 'vitest';
import {TenantRolePanel} from './TenantRolePanel';
const api=vi.hoisted(()=>({getTenantRoleControls:vi.fn(),saveTenantRoleControl:vi.fn(),refreshProfile:vi.fn()}));
vi.mock('@/hooks/useApiClient',()=>({useApiClient:()=>api}));
vi.mock('@/providers/AuthProvider',()=>({useAuth:()=>({refreshProfile:api.refreshProfile})}));

it('refreshes the profile and removes previously allowed authority when current evidence cannot be loaded',async()=>{
 const user=userEvent.setup();
 render(<MemoryRouter><TenantRolePanel tenantId="lab"/></MemoryRouter>);
 await screen.findByText('Configure organizational policy: Allowed by current role');
 api.getTenantRoleControls.mockRejectedValueOnce(new Error('Current access unavailable'));
 await user.click(screen.getByRole('button',{name:'Refresh tenant access evidence'}));
 expect(await screen.findByRole('alert')).toHaveTextContent('Current access unavailable');
 expect(screen.queryByText('Configure organizational policy: Allowed by current role')).not.toBeInTheDocument();
 expect(api.refreshProfile).toHaveBeenCalledWith({background:true});
});
const fixture=()=>({controls:{revision:4,assignments:[{id:'global',user_id:'viewer',name:'Existing Viewer',role:'viewer',global:true}],history:[]},can_manage:true,current_user_id:'admin',people:[{id:'viewer',name:'Existing Viewer'}],people_total:1,people_limit:100,allowed_roles:['viewer','operator','ciso'],permissions:{configure_policy:true},history_limit:50,has_global_assignment:true,scope:'Existing accounts only'});
beforeEach(()=>{vi.resetAllMocks();api.getTenantRoleControls.mockResolvedValue(fixture());api.saveTenantRoleControl.mockResolvedValue({saved:true,global_assignments_changed:false});});
it('requires an exact reviewed grant and never edits global roles',async()=>{const user=userEvent.setup();render(<MemoryRouter><TenantRolePanel tenantId="lab"/></MemoryRouter>);await user.click(await screen.findByText('Manage existing-account tenant assignments'));await user.selectOptions(screen.getByLabelText('Existing account'),'viewer');await user.selectOptions(screen.getByLabelText('Tenant role'),'ciso');await user.type(screen.getByLabelText('Access change reason'),'Time-limited lab review');await user.click(screen.getByRole('button',{name:'Review tenant role assignment'}));expect(api.saveTenantRoleControl).not.toHaveBeenCalled();expect(screen.getByRole('dialog')).toHaveTextContent('GRANT ciso for Existing Viewer');await user.click(screen.getByRole('button',{name:'Apply reviewed tenant access change'}));await waitFor(()=>expect(api.saveTenantRoleControl).toHaveBeenCalledWith('lab',expect.objectContaining({expected_revision:4,user_id:'viewer',role:'ciso',action:'grant'})));expect(screen.queryByRole('button',{name:/Review revoke Existing Viewer viewer/})).not.toBeInTheDocument();});
it('keeps viewer permissions read only and exposes no grant actions',async()=>{api.getTenantRoleControls.mockResolvedValue({...fixture(),can_manage:false,permissions:{}});render(<MemoryRouter><TenantRolePanel tenantId="lab"/></MemoryRouter>);expect(await screen.findByText(/You cannot grant or revoke assignments/)).toBeVisible();expect(screen.queryByText('Manage existing-account tenant assignments')).not.toBeInTheDocument();expect(api.saveTenantRoleControl).not.toHaveBeenCalled();});
it('ignores a late response from the previous tenant',async()=>{let finish:(v:unknown)=>void=()=>{};api.getTenantRoleControls.mockImplementation((tenant:string)=>tenant==='old'?new Promise(resolve=>{finish=resolve;}):Promise.resolve({...fixture(),scope:'New tenant only'}));const view=render(<MemoryRouter><TenantRolePanel tenantId="old"/></MemoryRouter>);view.rerender(<MemoryRouter><TenantRolePanel tenantId="new"/></MemoryRouter>);expect(await screen.findByText(/New tenant only/)).toBeVisible();finish({...fixture(),scope:'Old tenant scope'});await waitFor(()=>expect(screen.queryByText(/Old tenant scope/)).not.toBeInTheDocument());});
