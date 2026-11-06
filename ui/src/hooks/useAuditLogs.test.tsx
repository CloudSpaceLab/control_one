import {renderHook,waitFor} from '@testing-library/react';
import {beforeEach,expect,it,vi} from 'vitest';
import {APIError} from '../lib/api';
import {useAuditLogs} from './useAuditLogs';
const mocks=vi.hoisted(()=>({api:{listAuditLogs:vi.fn()},error:vi.fn((e:Error)=>e.message)}));
vi.mock('./useApiClient',()=>({useApiClient:()=>mocks.api}));
vi.mock('./useApiErrorHandler',()=>({useApiErrorHandler:()=>mocks.error}));
const pagination={total:1,count:1,limit:100,offset:0,nextOffset:null,prevOffset:null};
beforeEach(()=>vi.resetAllMocks());
it('loads explicitly requested global identity evidence without a tenant and clears rows on denial',async()=>{
 mocks.error.mockImplementation((e:Error)=>e.message);
 mocks.api.listAuditLogs.mockResolvedValueOnce({data:[{id:'global-record'}],pagination}).mockRejectedValueOnce(new APIError('Forbidden',403));
 const {result,rerender}=renderHook(({scope,tenant_id}:{scope:'global_identity'|'tenant';tenant_id?:string})=>useAuditLogs({scope,tenant_id}),{initialProps:{scope:'global_identity',tenant_id:undefined}});
 await waitFor(()=>expect(result.current.data).toHaveLength(1));
 expect(mocks.api.listAuditLogs).toHaveBeenCalledWith(expect.objectContaining({scope:'global_identity',tenant_id:undefined}));
 rerender({scope:'tenant',tenant_id:'tenant-a'});
 await waitFor(()=>expect(result.current.errorStatus).toBe(403));expect(result.current.data).toEqual([]);
});
it('does not turn an unspecified tenant request into global access',async()=>{
 const {result}=renderHook(()=>useAuditLogs());await waitFor(()=>expect(result.current.loading).toBe(false));expect(mocks.api.listAuditLogs).not.toHaveBeenCalled();
});
