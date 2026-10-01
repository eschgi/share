// The server's JSON API (see contract/api in the repository).

export interface Info {
  server_id: string;
  name: string;
  api_version: number;
  languages: string[];
  default_language: string;
  chunk_size_bytes: number;
  max_file_size_bytes: number;
}

export interface Session {
  kind: 'pin';
  pin_kind: 'permanent' | 'day';
  expires_at: string | null;
}

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly retryAfterSeconds?: number,
    readonly attemptsLeft?: number,
  ) {
    super(message);
  }
}

interface ErrorBody {
  error?: { code?: string; message?: string; retry_after_seconds?: number; attempts_left?: number };
}

/** The code in an error body such as tus answers with ({"error": {"code": …}}), if there is one. */
export function errorCode(body: string | undefined): string | undefined {
  try {
    const code = (JSON.parse(body ?? '') as ErrorBody | null)?.error?.code;
    return typeof code === 'string' ? code : undefined;
  } catch {
    return undefined;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    });
  } catch {
    throw new ApiError(0, 'network', 'The server can’t be reached.');
  }
  if (res.status === 204) return undefined as T;
  let data: unknown = null;
  try {
    data = await res.json();
  } catch {
    // Cloudflare error pages are HTML; treat them as a server error below.
  }
  if (!res.ok) {
    const e = (data as ErrorBody | null)?.error ?? {};
    throw new ApiError(res.status, e.code ?? 'internal', e.message ?? res.statusText, e.retry_after_seconds, e.attempts_left);
  }
  return data as T;
}

export const getInfo = () => request<Info>('GET', '/api/info');

/** The current PIN session, or null when there is none (or it has ended). */
export async function getSession(): Promise<{ session: Session | null; ended: boolean }> {
  try {
    return { session: await request<Session>('GET', '/api/session'), ended: false };
  } catch (e) {
    if (e instanceof ApiError && e.status === 401) return { session: null, ended: e.code === 'session_ended' };
    throw e;
  }
}

export interface UnlockResult {
  session: Session;
  moved_uploads: number;
}

export const unlock = (code: string) => request<UnlockResult>('POST', '/api/pin/unlock', { code, client: 'web' });

export interface InvitePeek {
  inviter: string | null; // null for invites made on the server's console
  name: string;
  role: 'admin' | 'member';
  expires_at: string;
  adds_phone: boolean;
}

export const peekInvite = (token: string) => request<InvitePeek>('POST', '/api/invites/peek', { token });

export interface AppInfo {
  android_package: string;
  link_scheme: string;
  apk: { version_code: number; version_name: string; size: number; sha256: string } | null;
  play_store_url: string | null;
}

export const getApp = () => request<AppInfo>('GET', '/api/app');
