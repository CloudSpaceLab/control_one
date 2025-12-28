import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Dashboard } from './pages/Dashboard';
import { Jobs } from './pages/Jobs';
import { Nodes } from './pages/Nodes';
import { Tenants } from './pages/Tenants';
import { Login } from './pages/Login';
import { useAuth } from './providers/AuthProvider';

export function App(): JSX.Element {
  const { isAuthenticated } = useAuth();

  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route
        path="/"
        element={isAuthenticated ? <MainLayout /> : <Navigate to="/login" replace />}
      >
        <Route index element={<Dashboard />} />
        <Route path="tenants" element={<Tenants />} />
        <Route path="nodes" element={<Nodes />} />
        <Route path="jobs" element={<Jobs />} />
      </Route>
      <Route path="*" element={<Navigate to={isAuthenticated ? '/' : '/login'} replace />} />
    </Routes>
  );
}
