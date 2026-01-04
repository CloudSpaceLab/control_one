import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Dashboard } from './pages/Dashboard';
import { Jobs } from './pages/Jobs';
import { Templates } from './pages/Templates';
import { Nodes } from './pages/Nodes';
import { Tenants } from './pages/Tenants';
import { Login } from './pages/Login';
import { AuthCallback } from './pages/AuthCallback';
import { useAuth } from './providers/AuthProvider';

export function App(): JSX.Element {
  const { isAuthenticated } = useAuth();

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/auth/callback" element={<AuthCallback />} />
      <Route
        path="/"
        element={isAuthenticated ? <MainLayout /> : <Navigate to="/login" replace />}
      >
        <Route index element={<Dashboard />} />
        <Route path="tenants" element={<Tenants />} />
        <Route path="nodes" element={<Nodes />} />
        <Route path="jobs" element={<Jobs />} />
        <Route path="templates" element={<Templates />} />
      </Route>
      <Route path="*" element={<Navigate to={isAuthenticated ? '/' : '/login'} replace />} />
    </Routes>
  );
}
