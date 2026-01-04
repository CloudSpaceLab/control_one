function ensureCrypto(): Crypto {
  const cryptoObj = typeof window !== 'undefined' ? window.crypto : undefined;
  if (!cryptoObj || !cryptoObj.getRandomValues || !cryptoObj.subtle) {
    throw new Error('Web Crypto API is not available in this environment');
  }
  return cryptoObj;
}

function base64UrlEncode(buffer: ArrayBuffer): string {
  const bytes = new Uint8Array(buffer);
  let binary = '';
  bytes.forEach((b) => {
    binary += String.fromCharCode(b);
  });
  return btoa(binary).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
}

export function generateRandomState(length = 32): string {
  const cryptoObj = ensureCrypto();
  const charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789';
  const randomValues = new Uint8Array(length);
  cryptoObj.getRandomValues(randomValues);
  return Array.from(randomValues, (value) => charset[value % charset.length]).join('');
}

export async function generateCodeVerifier(length = 64): Promise<string> {
  const cryptoObj = ensureCrypto();
  const charset = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~';
  const randomValues = new Uint8Array(length);
  cryptoObj.getRandomValues(randomValues);
  return Array.from(randomValues, (value) => charset[value % charset.length]).join('');
}

export async function generateCodeChallenge(verifier: string): Promise<string> {
  const cryptoObj = ensureCrypto();
  const encoder = new TextEncoder();
  const data = encoder.encode(verifier);
  const digest = await cryptoObj.subtle.digest('SHA-256', data);
  return base64UrlEncode(digest);
}
