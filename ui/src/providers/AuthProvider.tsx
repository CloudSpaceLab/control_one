import { createContext, ReactNode, useContext, useEffect, useMemo, useState } from 'react';

const STORAGE_KEY = 'control-one-token';

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
  signIn: (token: string) => void;
  signOut: () => void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

export function AuthProvider({ children }: { children: ReactNode }): JSX.Element {
  const [token, setToken] = useState<string | null>(() => readStoredToken());

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

  const value = useMemo<AuthContextValue>(
    () => ({
      token,
      isAuthenticated: Boolean(token && token.trim() !== ''),
      signIn: (nextToken: string) => {
        setToken(nextToken.trim());
      },
      signOut: () => {
        setToken(null);
      },
    }),
    [token],
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
