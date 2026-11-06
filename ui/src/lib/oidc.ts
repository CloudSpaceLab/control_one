import { oidcConfig } from '../config/oidc';
import { generateCodeChallenge, generateCodeVerifier, generateRandomState } from '../utils/pkce';

const PKCE_SESSION_KEY = 'control-one-oidc';
const PKCE_SESSION_LIFETIME_MS = 10 * 60 * 1000;
const TOKEN_EXCHANGE_TIMEOUT_MS = 15_000;
const MAX_TOKEN_RESPONSE_BYTES = 65_536;

class OidcExchangeError extends Error {}

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

// Bound both the initial request and a provider that stalls while streaming its
// response. Only messages constructed here may reach the callback screen.
async function requestApiToken(body: URLSearchParams): Promise<TokenResponse> {
  const controller = new AbortController();
  let reader: ReadableStreamDefaultReader<Uint8Array> | undefined;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      controller.abort();
      reject(new OidcExchangeError('Organization sign-in timed out. Start a new sign-in or contact your administrator.'));
    }, TOKEN_EXCHANGE_TIMEOUT_MS);
  });
  const exchange = async (): Promise<TokenResponse> => {
    const response = await fetch(oidcConfig.tokenUrl, {
      method: 'POST',
      headers: { 'Content-Type': 'application/x-www-form-urlencoded' },
      body: body.toString(),
      signal: controller.signal,
      redirect: 'error',
      credentials: 'omit',
      cache: 'no-store',
      referrerPolicy: 'no-referrer',
    });
    if (!response.ok) {
      throw new OidcExchangeError(`OIDC token exchange failed (HTTP ${response.status}). Start a fresh sign-in or contact your administrator.`);
    }
    const invalid = () => new OidcExchangeError('Provider returned an invalid token response. Start a fresh sign-in or contact your administrator.');
    if (!/^application\/json(?:\s*;|$)/i.test(response.headers.get('content-type') ?? '') || !response.body) throw invalid();
    const declaredSize = response.headers.get('content-length');
    if (declaredSize && (!/^\d+$/.test(declaredSize) || Number(declaredSize) > MAX_TOKEN_RESPONSE_BYTES)) throw invalid();
    reader = response.body.getReader();
    const chunks: Uint8Array[] = [];
    let size = 0;
    try {
      while (true) {
        const chunk = await reader.read();
        if (chunk.done) break;
        size += chunk.value.byteLength;
        if (size > MAX_TOKEN_RESPONSE_BYTES) throw invalid();
        chunks.push(chunk.value);
      }
    } finally {
      // Cancellation also covers oversized/error streams. Never await a
      // provider-controlled cancellation handler beyond the exchange deadline.
      void reader.cancel().catch(() => undefined);
    }
    const bytes = new Uint8Array(size);
    let offset = 0;
    for (const chunk of chunks) { bytes.set(chunk, offset); offset += chunk.byteLength; }
    try {
      const payload: unknown = JSON.parse(new TextDecoder('utf-8', { fatal: true }).decode(bytes));
      if (!payload || typeof payload !== 'object' || Array.isArray(payload)) throw invalid();
      return payload as TokenResponse;
    } catch { throw invalid(); }
  };
  try {
    return await Promise.race([exchange(), deadline]);
  } catch (error) {
    if (error instanceof OidcExchangeError) throw error;
    throw new OidcExchangeError('Organization sign-in could not reach the identity provider. Start a new sign-in or contact your administrator.');
  } finally {
    clearTimeout(timer);
    controller.abort();
    if (reader) void reader.cancel().catch(() => undefined);
  }
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
  if (
    !Number.isFinite(session.createdAt) ||
    session.createdAt > Date.now() ||
    Date.now() - session.createdAt >= PKCE_SESSION_LIFETIME_MS ||
    typeof session.state !== 'string' || !session.state ||
    typeof session.codeVerifier !== 'string' ||
    !/^[A-Za-z0-9._~-]{43,128}$/.test(session.codeVerifier)
  ) {
    throw new Error('OIDC session not found or expired');
  }
  if (session.state !== state) {
    throw new Error('OIDC state mismatch');
  }
  if (typeof code !== 'string' || !code || code.length > 4096 || Array.from(code).some(character => character.charCodeAt(0) <= 32 || character.charCodeAt(0) === 127) || state.length > 512) {
    throw new Error('Invalid authorization parameters. Start a new sign-in.');
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

  const payload = await requestApiToken(body);
  const token = payload.access_token;
  if (typeof token !== 'string' || !token || /[^\x21-\x7e]/.test(token) || typeof payload.token_type !== 'string' || payload.token_type.toLowerCase() !== 'bearer') {
    throw new Error('Provider must issue a Bearer access token for the Control One API');
  }

  return { token, returnTo: session.returnTo };
}
