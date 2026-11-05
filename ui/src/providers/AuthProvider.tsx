import {
  createContext,
  ReactNode,
  useCallback,
  useContext,
  useEffect,
  useMemo,
  useState,
} from 'react';
import { APIClient, APIError, type Profile } from '../lib/api';

const STORAGE_KEY = 'control-one-token';
const DIRECTORY_SESSION_NOTICE = 'Directory access changed or could not be verified. Sign in again or contact your administrator.';
const PROFILE_REFRESH_INTERVAL_MS = 5 * 60 * 1000; // 5 minutes

function readStoredToken(): string | null {
  if (typeof window === 'undefined') {
    return null;
  }
  const stored = window.localStorage.getItem(STORAGE_KEY);
  return stored && stored.trim() !== '' ? stored : null;
}

interface AuthContextValue {
  token: string | null;
  isAuthenticated: boolean;
  profile: Profile | null;
  loading: boolean;
  error: string | null;
  apiClient: APIClient;
  signIn: (token: string) => Promise<void>;
  signOut: () => Promise<void>;
  refreshProfile: (options?: { background?: boolean }) => Promise<void>;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }): JSX.Element {
  const [token, setToken] = useState<string | null>(() => readStoredToken());
  const [profile, setProfile] = useState<Profile | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const apiClient = useMemo(() => new APIClient({ token: readStoredToken() }), []);

  useEffect(() => {
    apiClient.setToken(token);
  }, [apiClient, token]);

  useEffect(() => {
    if (typeof window === 'undefined') {
      return;
    }
    if (token && token.trim() !== '') {
      window.localStorage.setItem(STORAGE_KEY, token);
    } else {
      window.localStorage.removeItem(STORAGE_KEY);
    }
  }, [token]);

  const handleSessionEnded = useCallback((message?: string | null) => {
    // Clear the apiClient token SYNCHRONOUSLY so any in-flight request
    // dispatched by other consumers in the same tick uses null, not the
    // stale token. The useEffect that mirrors token state would otherwise
    // run after the tear-down rerender.
    apiClient.setToken(null);
    setToken(null);
    setProfile(null);
    setError(message ?? 'Session has expired. Please sign in again.');
  }, [apiClient]);

  useEffect(() => {
    apiClient.onUnauthorized(reason => handleSessionEnded(reason === 'directory_verification_required' ? DIRECTORY_SESSION_NOTICE : undefined));
    return () => apiClient.onUnauthorized(undefined);
  }, [apiClient, handleSessionEnded]);

  const refreshProfile = useCallback(async (options?: { background?: boolean }): Promise<void> => {
    if (!token) {
      setProfile(null);
      setError(null);
      return;
    }
    if (!options?.background) setLoading(true);
    setError(null);
    try {
      const data = await apiClient.getProfile();
      setProfile(data);
      setError(null);
    } catch (err) {
      if (err instanceof APIError && err.status === 401) {
        handleSessionEnded(err.authReason === 'directory_verification_required' ? DIRECTORY_SESSION_NOTICE : undefined);
        return;
      }
      const message = err instanceof Error ? err.message : 'Failed to load profile';
      setError(message);
      setProfile(null);
    } finally {
      if (!options?.background) setLoading(false);
    }
  }, [apiClient, token, handleSessionEnded]);

  useEffect(() => {
    if (!token) {
      setProfile(null);
      setError(null);
      return;
    }
    refreshProfile().catch(() => {
      // errors are handled inside refreshProfile
    });
  }, [token, refreshProfile]);

  useEffect(() => {
    if (!token) {
      return;
    }
    const id = window.setInterval(() => {
      refreshProfile().catch(() => {});
    }, PROFILE_REFRESH_INTERVAL_MS);
    return () => window.clearInterval(id);
  }, [token, refreshProfile]);

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      isAuthenticated: Boolean(token && token.trim() !== ''),
      profile,
      loading,
      error,
      apiClient,
      signIn: async (nextToken: string) => {
        const trimmed = nextToken.trim();
        if (!trimmed) {
          throw new Error('Token is required');
        }
        // Sync the apiClient token BEFORE flipping the React state.
        // Other consumers (TenantProvider, dashboards) react to
        // isAuthenticated flipping true and immediately call into
        // apiClient — without this sync those calls would fly without
        // an Authorization header and bounce 401, triggering the
        // unauthorized handler and logging the user back out instantly.
        apiClient.setToken(trimmed);
        setToken(trimmed);
        setProfile(null);
        setError(null);
      },
      signOut: async () => {
        try {
          if (token && token.trim() !== '') {
            await apiClient.logout();
          }
        } catch {
          // A failed logout request must not trap the operator in the UI.
        } finally {
          handleSessionEnded(null);
        }
      },
      refreshProfile,
    }),
    [token, profile, loading, error, refreshProfile, handleSessionEnded, apiClient],
  );

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}

export function useOptionalAuth(): AuthContextValue | null {
  return useContext(AuthContext) ?? null;
}
