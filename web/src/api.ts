// The server's JSON API (see contract/api in the repository).

export interface Info {
  server_id: string;
  name: string;
  api_version: number;
  languages: string[];
  default_language: string;
  chunk_size_bytes: number;
  max_file_size_bytes: number;
  /** Where the files are: disk (sent over tus) or s3 (a bucket, sent straight there). */
  storage: 'disk' | 's3';
  /** Nobody has an account yet: visitors without a PIN go to /setup. */
  setup: boolean;
}

/** A PIN session; GET /api/session is only about those (a signed-in browser is /api/me). */
export interface Session {
  kind: 'pin';
  pin_kind: 'permanent' | 'day';
  expires_at: string | null;
  /** The folder the PIN sends into; null while the server has only one folder. */
  folder_name: string | null;
  /** The PIN also shows what is in its folder. */
  shows_folder: boolean;
  /** The key new files into the folder are encrypted for; null while it is plain. */
  encrypt: EncryptKey | null;
}

/** The newest version of an encrypted folder's key, which uploads seal their file keys for. */
export interface EncryptKey {
  folder: string;
  version: number;
  public_key: string;
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

async function request<T>(method: string, path: string, body?: unknown, headers?: Record<string, string>): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: { ...(body === undefined ? {} : { 'Content-Type': 'application/json' }), ...headers },
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

// Setting Share up (/setup) while nobody has an account: the storage folder on a drive, then the
// first admin. From outside the home network only in the first minutes after the server starts,
// or with the secret of the link in its log.

export interface SetupStatus {
  /** The folder is set up and Share runs. */
  ready: boolean;
  /** Share runs and nobody has an account yet: the page makes the first admin. */
  needs_admin: boolean;
  name: string;
  languages: string[];
  default_language: string;
  /** Before the setup: the folder, what it holds, and its drive (or the drive of the nearest
   * folder above, when it doesn't exist yet). */
  storage_dir: string;
  exists: boolean;
  empty: boolean;
  fs_type: string;
  total_bytes: number;
  free_bytes: number;
  warnings: StorageWarning[];
}

const setupHeader = (secret: string | null): Record<string, string> => (secret ? { 'X-Share-Setup': secret } : {});

export const getSetup = (secret: string | null) => request<SetupStatus>('GET', '/api/setup', undefined, setupHeader(secret));

/** Sets the folder up; then the server starts for real, and for a moment nothing answers. */
export const startSetup = (secret: string | null) => request<void>('POST', '/api/setup', {}, setupHeader(secret));

export interface FirstAdmin {
  name: string;
  username: string;
  password: string;
  device_name: string;
}

/** Makes the first admin and signs this browser in. */
export const createFirstAdmin = (secret: string | null, admin: FirstAdmin) => request<void>('POST', '/api/setup/admin', admin, setupHeader(secret));

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

/** A key locked with the secret of an invite's link: a version of a folder's key, or the
 * person's own key (folder null). */
export interface InviteKey {
  folder: string | null;
  version: number;
  public_key: string;
  locked: string;
}

export const acceptInvite = (token: string, deviceName: string) =>
  request<Me & { keys: InviteKey[] }>('POST', '/api/invites/accept', { token, device_name: deviceName, client: 'web' });

export const logout = () => request<void>('POST', '/api/auth/logout', {});

/** Sets the username and password; lock is the person's key locked with the new password. */
export const setPassword = (username: string, password: string, current?: string, lock?: string) =>
  request<void>('PUT', '/api/me/password', { username, password, current_password: current || undefined, password_lock: lock });

export const deleteMe = () => request<void>('POST', '/api/me/delete', {});

export const getMyDevices = () => request<{ devices: ListedDevice[] }>('GET', '/api/me/devices');

/** The server's version, for the About dialog; only for people with an account. */
export const getAbout = () => request<{ version: string }>('GET', '/api/about');

export const signOutDevice = (id: string) => request<void>('DELETE', `/api/devices/${encodeURIComponent(id)}`);

// The library: everything that was sent, by upload day, newest first.

export type FileKind = 'photo' | 'video' | 'document';

export interface FileInfo {
  id: string;
  /** The id of the folder it lies in. */
  folder: string;
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
  /** How it is encrypted (docs/e2ee-plan.md); null for a plain file. size is the plain size. */
  enc: FileEnc | null;
}

/** An encrypted file: the version of its folder's key that its file key is sealed for, that
 * sealed key, and the header its stored bytes start with. */
export interface FileEnc {
  version: number;
  key: string;
  header: string;
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

/** What the library shows: one folder or all, one kind or all, and part of a file name. */
export interface LibraryFilter {
  folder: string | null;
  kind: FileKind | null;
  q: string;
}

function libraryQuery(f: LibraryFilter, extra: Record<string, string | undefined> = {}): string {
  const p = new URLSearchParams();
  if (f.folder) p.set('folder', f.folder);
  if (f.kind) p.set('kind', f.kind);
  if (f.q.trim()) p.set('q', f.q.trim());
  for (const [k, v] of Object.entries(extra)) if (v !== undefined) p.set(k, v);
  const s = p.toString();
  return s ? '?' + s : '';
}

export const getLibrary = (f: LibraryFilter) => request<LibraryOverview>('GET', '/api/library' + libraryQuery(f));

export const getFiles = (f: LibraryFilter, cursor: string | null, limit: number) =>
  request<FilePage>('GET', '/api/files' + libraryQuery(f, { limit: String(limit), cursor: cursor ?? undefined }));

/** A file as it was sent, as an attachment; it resumes with Range. From a bucket the server
 * sends the browser on to it there. */
export const contentPath = (id: string) => `/api/files/${encodeURIComponent(id)}/content`;

export const contentUrl = (f: FileInfo) => contentPath(f.id);

/** A file's thumbnail; its address changes when a better one arrives, so it can be cached. */
export const thumbUrl = (f: FileInfo) => `/api/files/${encodeURIComponent(f.id)}/thumb?v=${Date.parse(f.updated_at).toString(36)}`;

export const getFile = (id: string) => request<FileInfo>('GET', `/api/files/${encodeURIComponent(id)}`);

/** Every file of a day that the filter shows, for selecting the day as a whole; without a day,
 * of every day. */
export const getFileIds = (f: LibraryFilter, day?: string) =>
  request<{ ids: string[]; bytes: number }>('GET', '/api/files/ids' + libraryQuery(f, { day }));

/** A folder of the library: what it holds and how many see it. */
export interface FolderInfo {
  id: string;
  name: string;
  files: number;
  bytes: number;
  /** The people and PIN sessions that sent its files. */
  senders: number;
  /** Who sees it: the admins, its members and open invites. */
  people: number;
  /** No member sees it. */
  admins_only: boolean;
  /** Its newest photo or video with a thumbnail. */
  cover: FileInfo | null;
  created_at: string;
  /** New files must be encrypted. */
  encrypted: boolean;
  /** The newest version of its key; null if it was never encrypted. */
  key_version: number | null;
}

/** The folders this person sees, the oldest first. */
export const getFolders = () => request<{ folders: FolderInfo[] }>('GET', '/api/folders');

/** Admins: a new folder, which only admins see until people are given it. */
export const createFolder = (name: string) => request<FolderInfo>('POST', '/api/folders', { name });
/** Admins: renames a folder, and its directory on the server's drive. */
export const renameFolder = (id: string, name: string) => request<FolderInfo>('PATCH', `/api/folders/${encodeURIComponent(id)}`, { name });
/** Admins: deletes a folder; its files go to Recently deleted, and its PINs stop. */
export const deleteFolder = (id: string) => request<{ changed: number }>('DELETE', `/api/folders/${encodeURIComponent(id)}`);
/** Admins: gives someone a folder, or takes it away. */
export const setFolderPerson = (folder: string, user: string, sees: boolean) =>
  request<void>(sees ? 'PUT' : 'DELETE', `/api/folders/${encodeURIComponent(folder)}/people/${encodeURIComponent(user)}`);
/** Admins: lets an open invite for a new member give a folder, or not. */
export const setFolderInvite = (folder: string, invite: string, gets: boolean) =>
  request<void>(gets ? 'PUT' : 'DELETE', `/api/folders/${encodeURIComponent(folder)}/invites/${encodeURIComponent(invite)}`);

/** Several files as one ZIP: the archive's name, its exact size, and each file's path in it. */
export interface ZipDownload {
  id: string;
  name: string;
  size: number;
  count: number;
  /** Encrypted files are listed, with their plain size, but the ZIP leaves them out. */
  files: { id: string; folder: string; path: string; size: number; enc: FileEnc | null }[];
}

export const createDownload = (ids: string[]) => request<ZipDownload>('POST', '/api/downloads', { ids });

export const zipUrl = (d: ZipDownload) => `/api/downloads/${encodeURIComponent(d.id)}`;

/** Admins: files go to Recently deleted, and come back from there. At most 1000 ids at once. */
export const deleteFiles = (ids: string[]) => request<{ changed: number }>('POST', '/api/files/delete', { ids });

export const restoreFiles = (ids: string[]) => request<{ changed: number }>('POST', '/api/trash/restore', { ids });

/** A file key sealed for another folder's key, for moving an encrypted file there. */
export interface MovedKey {
  id: string;
  version: number;
  key: string;
}

/** Admins: files go into another folder, and who sees them with it. At most 1000 ids at once. */
export const moveFiles = (ids: string[], folder: string, keys?: MovedKey[]) =>
  request<{ changed: number }>('POST', '/api/files/move', { ids, folder, keys: keys?.length ? keys : undefined });

/** Admins: the drive or the bucket, the library and Recently deleted. */
export interface Storage {
  storage: 'disk' | 's3';
  /** With the files in a bucket: its name and the service's address; the drive's fields are empty. */
  s3_bucket: string;
  s3_endpoint: string;
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

// Sending into a bucket (storage s3): the parts go straight there, with links from the server.

/** A link for sending one part, exactly size bytes, with a PUT. */
export interface S3PartUrl {
  number: number;
  url: string;
  size: number;
}

/** A new upload into the bucket: how the file is cut into parts, and links for the first ones. */
export interface S3NewUpload {
  id: string;
  part_size: number;
  /** 0 for an empty file, which only needs complete. */
  parts: number;
  urls: S3PartUrl[];
  expires_at: string;
}

/** Fresh links for some parts. */
export interface S3PartUrls {
  urls: S3PartUrl[];
  expires_at: string;
}

/** How far an upload into the bucket is: done_parts are those the bucket has. */
export interface S3UploadStatus {
  id: string;
  state: 'receiving' | 'finishing' | 'complete';
  size: number;
  part_size: number;
  parts: number;
  done_parts: number[];
}

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
  /** The id of the folder it sends into. */
  folder: string;
  /** Guests with it also see and download what is in the folder. */
  shows_folder: boolean;
  /** For a PIN that shows an encrypted folder: its link's secret, sealed for that version of
   * the folder's key, so admins' devices can show the whole link again. */
  secret: { sealed: string; version: number } | null;
}

export const getPins = () => request<{ pins: PinInfo[] }>('GET', '/api/pins');
export const suggestPin = () => request<{ code: string }>('GET', '/api/pins/suggest');
/** What the secret of a PIN link that shows an encrypted folder brings. */
export interface PinSecret {
  sealed: string;
  version: number;
  keys: { version: number; locked: string }[];
}

export const createPin = (kind: PinInfo['kind'], code: string, folder: string, shows_folder = false, secret?: PinSecret) =>
  request<PinInfo>('POST', '/api/pins', { kind, code, folder, shows_folder, secret });
export const newPinCode = (id: string) => request<PinInfo>('POST', `/api/pins/${encodeURIComponent(id)}/new-code`, {});
export const endPin = (id: string) => request<void>('POST', `/api/pins/${encodeURIComponent(id)}/end`, {});

/** Someone with an account, with their signed-in phones and browsers, most recently used first. */
export interface Person extends User {
  /** The admin asking. */
  me: boolean;
  created_at: string;
  last_seen_at: string | null;
  phones: ListedDevice[];
  /** The folders they see: every folder for an admin. */
  folders: string[];
}

/** An invite nobody has used yet; with user_id it adds a phone or browser for that person. */
export interface OpenInvite {
  id: string;
  name: string;
  role: Role;
  user_id: string | null;
  created_at: string;
  expires_at: string;
  /** The folders the new person will see; none for an added phone. */
  folders: string[];
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
/** An invite for someone new; a member sees folders, an admin every folder. */
/** A version of a folder's key locked with the secret of an invite's link. */
export interface LockedFolderKey {
  folder: string;
  version: number;
  locked: string;
}

export const createInvite = (name: string, role: Role, folders: string[], keys?: LockedFolderKey[]) =>
  request<NewInvite>('POST', '/api/invites', { name, role, folders: role === 'member' ? folders : undefined, keys: keys?.length ? keys : undefined });
/** An invite for another phone or browser of someone; personKey, their key locked with the
 * link's secret, when it is the caller's own. */
export const inviteDevice = (userId: string, personKey?: string) =>
  request<NewInvite>('POST', `/api/users/${encodeURIComponent(userId)}/invites`, { person_key: personKey });
export const withdrawInvite = (id: string) => request<void>('DELETE', `/api/invites/${encodeURIComponent(id)}`);

export interface TrashedFile extends FileInfo {
  deleted_at: string;
  /** The admin who deleted it, or null if they are gone. */
  deleted_by: string | null;
  purge_at: string;
}

/** A folder that files in Recently deleted are from; deleted ones aren't in /api/folders. */
export interface TrashedFolder {
  id: string;
  name: string;
  deleted: boolean;
}

export const getTrash = () => request<{ files: TrashedFile[]; folders: TrashedFolder[]; trash_days: number }>('GET', '/api/trash');
export const purgeFiles = (ids: string[]) => request<{ changed: number }>('POST', '/api/trash/purge', { ids });

// End-to-end encryption (docs/e2ee-plan.md): binary values are base64url without padding.

/** What this browser can open, and what it can seal for others. */
export interface KeysAnswer {
  device_key: string | null;
  person: { public_key: string | null; sealed: string | null; password_lock: string | null; held_by: number };
  folders: { folder: string; version: number; public_key: string; sealed: string | null }[];
  recovery_key: string | null;
  todo: {
    /** The person's devices that lack their key: passed on only after a check, which needs the
     * device active (seen in the last 15 minutes) to answer it. */
    devices: { id: string; public_key: string; name: string; client: 'app' | 'web'; created_at: string; active: boolean }[];
    /** People who lack a version of a folder's key: passed on after a check, which needs one of
     * their devices active, or at once for a key checked here before. */
    people: { folder: string; version: number; user: string; name: string; public_key: string; active: boolean }[];
    recovery: { folder: string; version: number }[];
    rekey: string[];
    pins: { pin: string; folder: string; version: number; secret_version: number; secret_sealed: string }[];
  };
  checks: KeyCheck[];
}

/** An open check this browser takes part in (contract/api/keys_check.json): one it asks, or
 * one for it or its person. */
export interface KeyCheck {
  id: string;
  asking: boolean;
  device: string | null;
  user: string | null;
  from: string;
  commitment: string;
  answer: string | null;
  answered: boolean;
  reveal: string | null;
}

export const openCheck = (target: { device: string } | { user: string }, commitment: string) =>
  request<{ id: string }>('POST', '/api/keys/checks', { ...target, commitment });
export const answerCheck = (id: string, nonce: string) => request<void>('PUT', `/api/keys/checks/${encodeURIComponent(id)}/answer`, { nonce });
/** Reveals the nonce committed to, for the answer seen, which the code is made from. */
export const revealCheck = (id: string, nonce: string, answer: string) =>
  request<void>('PUT', `/api/keys/checks/${encodeURIComponent(id)}/reveal`, { nonce, answer });
export const closeCheck = (id: string) => request<void>('DELETE', `/api/keys/checks/${encodeURIComponent(id)}`);

export const getKeys = () => request<KeysAnswer>('GET', '/api/keys');
export const putDeviceKey = (public_key: string) => request<void>('PUT', '/api/keys/device', { public_key });
export const putPersonKey = (public_key: string, sealed: string, start_over = false) =>
  request<void>('PUT', '/api/keys/person', { public_key, sealed, start_over: start_over || undefined });
export const putPasswordLock = (password_lock: string) => request<void>('PUT', '/api/keys/password-lock', { password_lock });

/** What this browser sealed or locked from its to-do list. */
export interface Grants {
  devices: { device: string; sealed: string }[];
  people: { folder: string; version: number; user: string; sealed: string }[];
  recovery: { folder: string; version: number; sealed: string }[];
  pins: { pin: string; version: number; locked: string }[];
}

export const postGrants = (g: Grants) => request<void>('POST', '/api/keys/grants', g);

/** A version of a folder's key, made here: sealed for this person and for the recovery key. */
export interface NewFolderKey {
  public_key: string;
  sealed: string;
  recovery_sealed: string;
}

export const setFolderEncryption = (folder: string, encrypted: boolean, key?: NewFolderKey) =>
  request<FolderInfo>('PUT', `/api/folders/${encodeURIComponent(folder)}/encryption`, { encrypted, key });
export const postFolderKey = (folder: string, version: number, key: NewFolderKey) =>
  request<void>('POST', `/api/folders/${encodeURIComponent(folder)}/keys`, { version, ...key });

/** Admins: the recovery key, and the folder keys sealed for it. */
export interface Recovery {
  public_key: string | null;
  locked: string | null;
  folders: { folder: string; version: number; public_key: string; sealed: string }[];
}

export const getRecovery = () => request<Recovery>('GET', '/api/recovery');
export const putRecovery = (public_key: string, locked: string) => request<void>('PUT', '/api/recovery', { public_key, locked });

/** Admins: the settings for the whole server. */
export interface Settings {
  new_folders_encrypted: boolean;
}

export const getSettings = () => request<Settings>('GET', '/api/admin/settings');
export const putSettings = (s: Settings) => request<Settings>('PUT', '/api/admin/settings', s);

/** A PIN that shows its folder: the folder's keys, locked with its link's secret. */
export interface PinKeys {
  folder: string;
  keys: { version: number; public_key: string; locked: string }[];
}

export const getPinKeys = () => request<PinKeys>('GET', '/api/pin/keys');
