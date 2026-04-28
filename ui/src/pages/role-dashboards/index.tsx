import { useSearchParams } from 'react-router-dom';
import { useRolePick } from '@/hooks/useRolePick';
import { AdminDashboard } from './AdminDashboard';
import { OperatorDashboard } from './OperatorDashboard';
import { ViewerDashboard } from './ViewerDashboard';

export function DashboardRouter(): JSX.Element {
  const { role } = useRolePick();
  const [params] = useSearchParams();
  const view = params.get('view');

  if (view === 'executive') return <ViewerDashboard readonly />;
  if (role === 'admin') return <AdminDashboard />;
  if (role === 'operator') return <OperatorDashboard />;
  return <ViewerDashboard />;
}
