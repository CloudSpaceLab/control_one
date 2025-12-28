import { jsx as _jsx } from "react/jsx-runtime";
import { createContext, useContext, useEffect, useMemo, useState } from 'react';
const STORAGE_KEY = 'control-one-token';
function readStoredToken() {
    if (typeof window === 'undefined') {
        return null;
    }
    const stored = window.localStorage.getItem(STORAGE_KEY);
    return stored && stored.trim() !== '' ? stored : null;
}
const AuthContext = createContext(undefined);
export function AuthProvider({ children }) {
    const [token, setToken] = useState(() => readStoredToken());
    useEffect(() => {
        if (typeof window === 'undefined') {
            return;
        }
        if (token && token.trim() !== '') {
            window.localStorage.setItem(STORAGE_KEY, token);
        }
        else {
            window.localStorage.removeItem(STORAGE_KEY);
        }
    }, [token]);
    const value = useMemo(() => ({
        token,
        isAuthenticated: Boolean(token && token.trim() !== ''),
        signIn: (nextToken) => {
            setToken(nextToken.trim());
        },
        signOut: () => {
            setToken(null);
        },
    }), [token]);
    return _jsx(AuthContext.Provider, { value: value, children: children });
}
export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return ctx;
}
