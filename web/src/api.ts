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

/** A PIN session; GET /api/session is only about those (a signed-in browser is /api/me). */
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

let whenSignedOut: (() => void) | null = null;

/** What to do when the server says this browser was signed out, on any request. */
export function onSignedOut(f: (() => void) | null): void {
  whenSignedOut = f;
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
    if (res.status === 401 && e.code === 'signed_out') whenSignedOut?.();
    throw new ApiError(res.status, e.code ?? 'internal', e.message ?? res.statusText, e.retry_after_seconds, e.attempts_left);
  }
  return data as T;
}

let info: Promise<Info> | null = null;

/** The server's name, languages and limits: asked once per page load, again only after a failure. */
export function getInfo(): Promise<Info> {
  info ??= request<Info>('GET', '/api/info').catch((e: unknown) => {
    info = null;
    throw e;
  });
  return info;
}

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

// People with an account. A browser signs in with "client": "web": its key comes back as a
// cookie the page can't read, never in the body. POSTs send {} at least: the server wants JSON
// from browsers, which no form on another site can send.

export type Role = 'admin' | 'member';

export interface User {
  id: string;
  name: string;
  role: Role;
  username: string | null;
  has_password: boolean;
}

/** A signed-in phone (the app) or browser; home_only: a browser that signed in at home, where
 * alone its session works. */
export interface Device {
  id: string;
  name: string;
  client: 'app' | 'web';
  home_only: boolean;
}

export interface Me {
  user: User;
  device: Device;
}

/** A device in a list: when it came and was last used, and whether it is the one asking. */
export interface ListedDevice extends Device {
  created_at: string;
  last_seen_at: string;
  this: boolean;
}

export const getMe = () => request<Me>('GET', '/api/me');

export const login = (username: string, password: string, deviceName: string) =>
  request<Me>('POST', '/api/auth/login', { username, password, device_name: deviceName, client: 'web' });

export const acceptInvite = (token: string, deviceName: string) =>
  request<Me>('POST', '/api/invites/accept', { token, device_name: deviceName, client: 'web' });

export const logout = () => request<void>('POST', '/api/auth/logout', {});

export const setPassword = (username: string, password: string, current?: string) =>
  request<void>('PUT', '/api/me/password', { username, password, current_password: current || undefined });

export const deleteMe = () => request<void>('POST', '/api/me/delete', {});

export const getMyDevices = () => request<{ devices: ListedDevice[] }>('GET', '/api/me/devices');

export const signOutDevice = (id: string) => request<void>('DELETE', `/api/devices/${encodeURIComponent(id)}`);
