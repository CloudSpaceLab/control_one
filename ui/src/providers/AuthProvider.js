import { jsx as _jsx } from "react/jsx-runtime";
import { createContext, useCallback, useContext, useEffect, useMemo, useState, } from 'react';
import { APIClient, APIError } from '../lib/api';
const STORAGE_KEY = 'control-one-token';
const PROFILE_REFRESH_INTERVAL_MS = 5 * 60 * 1000; // 5 minutes
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
    const [profile, setProfile] = useState(null);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState(null);
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
        }
        else {
            window.localStorage.removeItem(STORAGE_KEY);
        }
    }, [token]);
    const handleSessionEnded = useCallback((message) => {
        setToken(null);
        setProfile(null);
        setError(message ?? 'Session has expired. Please sign in again.');
    }, []);
    useEffect(() => {
        apiClient.onUnauthorized(() => handleSessionEnded());
        return () => apiClient.onUnauthorized(undefined);
    }, [apiClient, handleSessionEnded]);
    const refreshProfile = useCallback(async () => {
        if (!token) {
            setProfile(null);
            setError(null);
            return;
        }
        setLoading(true);
        setError(null);
        try {
            const data = await apiClient.getProfile();
            setProfile(data);
            setError(null);
        }
        catch (err) {
            if (err instanceof APIError && err.status === 401) {
                handleSessionEnded();
                return;
            }
            const message = err instanceof Error ? err.message : 'Failed to load profile';
            setError(message);
            setProfile(null);
        }
        finally {
            setLoading(false);
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
            refreshProfile().catch(() => { });
        }, PROFILE_REFRESH_INTERVAL_MS);
        return () => window.clearInterval(id);
    }, [token, refreshProfile]);
    const value = useMemo(() => ({
        token,
        isAuthenticated: Boolean(token && token.trim() !== ''),
        profile,
        loading,
        error,
        apiClient,
        signIn: async (nextToken) => {
            const trimmed = nextToken.trim();
            if (!trimmed) {
                throw new Error('Token is required');
            }
            setToken(trimmed);
            setProfile(null);
            setError(null);
        },
        signOut: () => {
            handleSessionEnded(null);
        },
        refreshProfile,
    }), [token, profile, loading, error, refreshProfile, handleSessionEnded, apiClient]);
    return _jsx(AuthContext.Provider, { value: value, children: children });
}
export function useAuth() {
    const ctx = useContext(AuthContext);
    if (!ctx) {
        throw new Error('useAuth must be used within an AuthProvider');
    }
    return ctx;
}
