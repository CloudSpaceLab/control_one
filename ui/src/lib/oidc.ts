import { oidcConfig } from '../config/oidc';
import { generateCodeChallenge, generateCodeVerifier, generateRandomState } from '../utils/pkce';

const PKCE_SESSION_KEY = 'control-one-oidc';

interface PKCESession {
  state: string;
  codeVerifier: string;
  createdAt: number;
  returnTo?: string;
}

interface TokenResponse {
  access_token?: string;
  id_token?: string;
  token_type?: string;
  expires_in?: number;
  refresh_token?: string;
  scope?: string;
}

function persistSession(data: PKCESession): void {
  if (typeof window === 'undefined' || !window.sessionStorage) {
    throw new Error('Session storage unavailable');
  }
  window.sessionStorage.setItem(PKCE_SESSION_KEY, JSON.stringify(data));
}

function readSession(): PKCESession | null {
  if (typeof window === 'undefined' || !window.sessionStorage) {
    return null;
  }
  const raw = window.sessionStorage.getItem(PKCE_SESSION_KEY);
  if (!raw) {
    return null;
  }
  try {
    return JSON.parse(raw) as PKCESession;
  } catch {
    return null;
  }
}

export function clearOidcSession(): void {
  if (typeof window === 'undefined' || !window.sessionStorage) {
    return;
  }
  window.sessionStorage.removeItem(PKCE_SESSION_KEY);
}

export async function buildAuthorizationUrl(returnTo?: string): Promise<string> {
  if (!oidcConfig.enabled) {
    throw new Error('OIDC is not configured');
  }

  const state = generateRandomState();
  const codeVerifier = await generateCodeVerifier();
  const codeChallenge = await generateCodeChallenge(codeVerifier);

  persistSession({
    state,
    codeVerifier,
    createdAt: Date.now(),
    returnTo,
  });

  const params = new URLSearchParams({
    response_type: 'code',
    client_id: oidcConfig.clientId,
    redirect_uri: oidcConfig.redirectUri,
    scope: oidcConfig.scope,
    state,
    code_challenge: codeChallenge,
    code_challenge_method: 'S256',
  });

  if (oidcConfig.audience) {
    params.set('audience', oidcConfig.audience);
  }

  return `${oidcConfig.authorizationUrl}?${params.toString()}`;
}

export async function exchangeCodeForToken(code: string, state: string): Promise<{
  token: string;
  returnTo?: string;
}> {
  if (!oidcConfig.enabled) {
    throw new Error('OIDC is not configured');
  }

  const session = readSession();
  clearOidcSession();
  if (!session) {
    throw new Error('OIDC session not found or expired');
  }
  if (session.state !== state) {
    throw new Error('OIDC state mismatch');
  }

  const body = new URLSearchParams({
    grant_type: 'authorization_code',
    client_id: oidcConfig.clientId,
    redirect_uri: oidcConfig.redirectUri,
    code,
    code_verifier: session.codeVerifier,
  });
  if (oidcConfig.audience) {
    body.set('audience', oidcConfig.audience);
  }

  const response = await fetch(oidcConfig.tokenUrl, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/x-www-form-urlencoded',
    },
    body: body.toString(),
  });

  if (!response.ok) {
    const text = await response.text();
    throw new Error(`OIDC token exchange failed: ${text || response.statusText}`);
  }

  const payload = (await response.json()) as TokenResponse;
  const token = payload.id_token || payload.access_token;
  if (!token) {
    throw new Error('OIDC token response missing id_token or access_token');
  }

  return { token, returnTo: session.returnTo };
}
