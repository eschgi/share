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

/** Stops using the PIN in this browser: the server ends the session and drops its cookie. */
export const endSession = () => request<void>('POST', '/api/session/end', {});

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

/** The server's version, for the About dialog; only for people with an account. */
export const getAbout = () => request<{ version: string }>('GET', '/api/about');

export const signOutDevice = (id: string) => request<void>('DELETE', `/api/devices/${encodeURIComponent(id)}`);

// The library: everything that was sent, by upload day, newest first.

export type FileKind = 'photo' | 'video' | 'document';

export interface FileInfo {
  id: string;
  name: string;
  size: number;
  mime: string;
  kind: FileKind;
  /** The upload day, in the server's time zone: 2026-09-27. */
  day: string;
  uploaded_at: string;
  /** Changes with the thumbnail. */
  updated_at: string;
  width: number | null;
  height: number | null;
  duration_ms: number | null;
  has_thumb: boolean;
  /** Who sent it, if they have an account; null for a PIN. */
  from: string | null;
}

export interface LibraryDay {
  day: string;
  count: number;
  bytes: number;
}

/** The days that have files; version grows with every change to the library. */
export interface LibraryOverview {
  version: number;
  days: LibraryDay[];
}

export interface FilePage {
  files: FileInfo[];
  next_cursor: string | null;
}

/** What the library shows: one kind or all, and part of a file name. */
export interface LibraryFilter {
  kind: FileKind | null;
  q: string;
}

function libraryQuery(f: LibraryFilter, extra: Record<string, string | undefined> = {}): string {
  const p = new URLSearchParams();
  if (f.kind) p.set('kind', f.kind);
  if (f.q.trim()) p.set('q', f.q.trim());
  for (const [k, v] of Object.entries(extra)) if (v !== undefined) p.set(k, v);
  const s = p.toString();
  return s ? '?' + s : '';
}

export const getLibrary = (f: LibraryFilter) => request<LibraryOverview>('GET', '/api/library' + libraryQuery(f));

export const getFiles = (f: LibraryFilter, cursor: string | null, limit: number) =>
  request<FilePage>('GET', '/api/files' + libraryQuery(f, { limit: String(limit), cursor: cursor ?? undefined }));

/** A file as it was sent, as an attachment; it resumes with Range. */
export const contentUrl = (f: FileInfo) => `/api/files/${encodeURIComponent(f.id)}/content`;

/** A file's thumbnail; its address changes when a better one arrives, so it can be cached. */
export const thumbUrl = (f: FileInfo) => `/api/files/${encodeURIComponent(f.id)}/thumb?v=${Date.parse(f.updated_at).toString(36)}`;

export const getFile = (id: string) => request<FileInfo>('GET', `/api/files/${encodeURIComponent(id)}`);

/** Every file of a day that the filter shows, for selecting the day as a whole. */
export const getFileIds = (f: LibraryFilter, day: string) =>
  request<{ ids: string[]; bytes: number }>('GET', '/api/files/ids' + libraryQuery(f, { day }));

/** Several files as one ZIP: the archive's name, its exact size, and each file's path in it. */
export interface ZipDownload {
  id: string;
  name: string;
  size: number;
  count: number;
  files: { id: string; path: string; size: number }[];
}

export const createDownload = (ids: string[]) => request<ZipDownload>('POST', '/api/downloads', { ids });

export const zipUrl = (d: ZipDownload) => `/api/downloads/${encodeURIComponent(d.id)}`;

/** Admins: files go to Recently deleted, and come back from there. At most 1000 ids at once. */
export const deleteFiles = (ids: string[]) => request<{ changed: number }>('POST', '/api/files/delete', { ids });

export const restoreFiles = (ids: string[]) => request<{ changed: number }>('POST', '/api/trash/restore', { ids });

/** Admins: the drive, the library and Recently deleted. */
export interface Storage {
  storage_dir: string;
  fs_type: string;
  total_bytes: number;
  free_bytes: number;
  files: number;
  bytes: number;
  trash_files: number;
  trash_bytes: number;
  trash_days: number;
  /** What share check finds about the drive, problems first (contract/storage_warnings.json). */
  warnings?: StorageWarning[];
}

export interface StorageWarning {
  code: string;
  /** problem: Share won't work well until it's fixed. */
  level: 'problem' | 'warning';
  /** The server console's English words, for codes the website doesn't know. */
  message: string;
}

export const getStorage = () => request<Storage>('GET', '/api/admin/storage');

// Admins: upload PINs, people and invites, Recently deleted.

export interface PinInfo {
  id: string;
  code: string;
  kind: 'permanent' | 'day';
  created_at: string;
  expires_at: string | null;
  /** The website with the PIN filled in, for sharing. */
  link: string;
  /** Files sent with it that are still in the library. */
  files: number;
  /** Phones and browsers that unlocked it in the last 30 days. */
  phones: number;
}

export const getPins = () => request<{ pins: PinInfo[] }>('GET', '/api/pins');
export const suggestPin = () => request<{ code: string }>('GET', '/api/pins/suggest');
export const createPin = (kind: PinInfo['kind'], code: string) => request<PinInfo>('POST', '/api/pins', { kind, code });
export const newPinCode = (id: string) => request<PinInfo>('POST', `/api/pins/${encodeURIComponent(id)}/new-code`, {});
export const endPin = (id: string) => request<void>('POST', `/api/pins/${encodeURIComponent(id)}/end`, {});

/** Someone with an account, with their signed-in phones and browsers, most recently used first. */
export interface Person extends User {
  /** The admin asking. */
  me: boolean;
  created_at: string;
  last_seen_at: string | null;
  phones: ListedDevice[];
}

/** An invite nobody has used yet; with user_id it adds a phone or browser for that person. */
export interface OpenInvite {
  id: string;
  name: string;
  role: Role;
  user_id: string | null;
  created_at: string;
  expires_at: string;
}

export interface People {
  users: Person[];
  invites: OpenInvite[];
}

/** A new invite: the link is only in this answer. */
export interface NewInvite {
  token: string;
  link: string;
  invite: OpenInvite;
}

export const getPeople = () => request<People>('GET', '/api/users');
export const setRole = (id: string, role: Role) => request<void>('PATCH', `/api/users/${encodeURIComponent(id)}`, { role });
export const removePerson = (id: string) => request<void>('DELETE', `/api/users/${encodeURIComponent(id)}`);
/** Makes up a new password for someone else, shown once; username is needed if they have none. */
export const newPassword = (id: string, username?: string) =>
  request<{ username: string; password: string }>('POST', `/api/users/${encodeURIComponent(id)}/password`, { username });
export const createInvite = (name: string, role: Role) => request<NewInvite>('POST', '/api/invites', { name, role });
export const inviteDevice = (userId: string) => request<NewInvite>('POST', `/api/users/${encodeURIComponent(userId)}/invites`, {});
export const withdrawInvite = (id: string) => request<void>('DELETE', `/api/invites/${encodeURIComponent(id)}`);

export interface TrashedFile extends FileInfo {
  deleted_at: string;
  /** The admin who deleted it, or null if they are gone. */
  deleted_by: string | null;
  purge_at: string;
}

export const getTrash = () => request<{ files: TrashedFile[]; trash_days: number }>('GET', '/api/trash');
export const purgeFiles = (ids: string[]) => request<{ changed: number }>('POST', '/api/trash/purge', { ids });
