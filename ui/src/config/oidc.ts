const env = import.meta.env;

const defaultRedirect =
  typeof window !== 'undefined' ? `${window.location.origin}/auth/callback` : '/auth/callback';

export interface OIDCConfig {
  enabled: boolean;
  authorizationUrl: string;
  tokenUrl: string;
  clientId: string;
  scope: string;
  redirectUri: string;
  audience?: string;
}

export const oidcConfig: OIDCConfig = {
  enabled:
    Boolean(env.VITE_OIDC_AUTH_URL) &&
    Boolean(env.VITE_OIDC_TOKEN_URL) &&
    Boolean(env.VITE_OIDC_CLIENT_ID),
  authorizationUrl: env.VITE_OIDC_AUTH_URL ?? '',
  tokenUrl: env.VITE_OIDC_TOKEN_URL ?? '',
  clientId: env.VITE_OIDC_CLIENT_ID ?? '',
  scope: env.VITE_OIDC_SCOPE ?? 'openid profile email',
  redirectUri: env.VITE_OIDC_REDIRECT_URI ?? defaultRedirect,
  audience: env.VITE_OIDC_AUDIENCE,
};

export function isOidcConfigured(): boolean {
  return oidcConfig.enabled;
}
