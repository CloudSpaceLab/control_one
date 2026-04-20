import { Navigate, Route, Routes } from 'react-router-dom';
import { MainLayout } from './components/MainLayout';
import { Dashboard } from './pages/Dashboard';
import { Jobs } from './pages/Jobs';
import { Templates } from './pages/Templates';
import { Nodes } from './pages/Nodes';
import { Tenants } from './pages/Tenants';
import { Compliance } from './pages/Compliance';
import { Audit } from './pages/Audit';
import { Users } from './pages/Users';
import { Telemetry } from './pages/Telemetry';
import { Settings } from './pages/Settings';
import { Secrets } from './pages/Secrets';
import { FleetEnroll } from './pages/FleetEnroll';
import { OfflineBundle } from './pages/OfflineBundle';
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
        <Route path="fleet-enroll" element={<FleetEnroll />} />
        <Route path="jobs" element={<Jobs />} />
        <Route path="templates" element={<Templates />} />
        <Route path="compliance" element={<Compliance />} />
        <Route path="audit" element={<Audit />} />
        <Route path="users" element={<Users />} />
        <Route path="telemetry" element={<Telemetry />} />
        <Route path="secrets" element={<Secrets />} />
        <Route path="offline-bundle" element={<OfflineBundle />} />
        <Route path="settings" element={<Settings />} />
      </Route>
      <Route path="*" element={<Navigate to={isAuthenticated ? '/' : '/login'} replace />} />
    </Routes>
  );
}
