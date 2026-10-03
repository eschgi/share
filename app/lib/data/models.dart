/// What the server's API returns (contract/api in the repository), parsed tolerantly: a
/// missing or odd field gives a default instead of an exception.
library;

import 'package:clock/clock.dart';

typedef Json = Map<String, dynamic>;

String _str(Object? v, [String fallback = '']) => v is String ? v : fallback;
int _int(Object? v, [int fallback = 0]) => v is num ? v.toInt() : fallback;
int? _intOrNull(Object? v) => v is num ? v.toInt() : null;
bool _bool(Object? v) => v == true;
DateTime? _time(Object? v) => v is String ? DateTime.tryParse(v)?.toLocal() : null;
Json _obj(Object? v) => v is Map ? v.cast<String, dynamic>() : const {};
List<Object?> _list(Object? v) => v is List ? v : const [];

enum Role {
  admin,
  member;

  static Role parse(Object? v) => v == 'admin' ? admin : member;
}

class User {
  const User({required this.id, required this.name, required this.role, this.username, this.hasPassword = false});

  factory User.fromJson(Json j) => User(
        id: _str(j['id']),
        name: _str(j['name']),
        role: Role.parse(j['role']),
        username: j['username'] is String ? j['username'] as String : null,
        hasPassword: _bool(j['has_password']),
      );

  final String id;
  final String name;
  final Role role;
  final String? username;
  final bool hasPassword;

  bool get isAdmin => role == Role.admin;
}

/// Where the server can be reached: always the public address, and optionally a local one
/// whose self-signed certificate the app trusts only by these fingerprints.
class ServerInfo {
  const ServerInfo({required this.publicUrl, this.localUrl, this.localCertSha256 = const []});

  factory ServerInfo.fromJson(Json j) => ServerInfo(
        publicUrl: _str(j['public_url']),
        localUrl: j['local_url'] is String ? j['local_url'] as String : null,
        localCertSha256: [for (final p in _list(j['local_cert_sha256'])) if (p is String) p.toLowerCase()],
      );

  final String publicUrl;
  final String? localUrl;
  final List<String> localCertSha256;
}

/// A signed-in phone: the token is shown once, at sign-in.
class SignedIn {
  const SignedIn({required this.token, required this.user, required this.deviceId, required this.server});

  factory SignedIn.fromJson(Json j) => SignedIn(
        token: _str(j['token']),
        user: User.fromJson(_obj(j['user'])),
        deviceId: _str(_obj(j['device'])['id']),
        server: ServerInfo.fromJson(_obj(j['server'])),
      );

  final String token;
  final User user;
  final String deviceId;
  final ServerInfo server;
}

class InvitePeek {
  const InvitePeek({required this.name, required this.role, required this.expiresAt, this.inviter, this.addsPhone = false});

  factory InvitePeek.fromJson(Json j) => InvitePeek(
        inviter: j['inviter'] is String ? j['inviter'] as String : null,
        name: _str(j['name']),
        role: Role.parse(j['role']),
        expiresAt: _time(j['expires_at']) ?? clock.now(),
        addsPhone: _bool(j['adds_phone']),
      );

  final String? inviter;
  final String name;
  final Role role;
  final DateTime expiresAt;
  final bool addsPhone;
}

/// /api/info: who the server is. Also the probe of the local address.
class ServerIdentity {
  const ServerIdentity({required this.serverId, required this.name, required this.chunkSize});

  factory ServerIdentity.fromJson(Json j) => ServerIdentity(
        serverId: _str(j['server_id']),
        name: _str(j['name'], 'Share'),
        chunkSize: _int(j['chunk_size_bytes'], 20 << 20),
      );

  final String serverId;
  final String name;
  final int chunkSize;
}

enum FileKind {
  photo,
  video,
  document;

  static FileKind parse(Object? v) => switch (v) { 'photo' => photo, 'video' => video, _ => document };
}

class FileInfo {
  const FileInfo({
    required this.id,
    this.folder = '',
    required this.name,
    required this.size,
    required this.mime,
    required this.kind,
    required this.day,
    required this.uploadedAt,
    required this.updatedAt,
    this.width,
    this.height,
    this.durationMs,
    this.hasThumb = false,
    this.from,
  });

  factory FileInfo.fromJson(Json j) => FileInfo(
        id: _str(j['id']),
        folder: _str(j['folder']),
        name: _str(j['name']),
        size: _int(j['size']),
        mime: _str(j['mime'], 'application/octet-stream'),
        kind: FileKind.parse(j['kind']),
        day: _str(j['day']),
        uploadedAt: _time(j['uploaded_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        updatedAt: _time(j['updated_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        width: _intOrNull(j['width']),
        height: _intOrNull(j['height']),
        durationMs: _intOrNull(j['duration_ms']),
        hasThumb: _bool(j['has_thumb']),
        from: j['from'] is String ? j['from'] as String : null,
      );

  final String id;
  final String folder; // the id of the folder it lies in
  final String name;
  final int size;
  final String mime;
  final FileKind kind;
  final String day; // YYYY-MM-DD, the upload day on the server
  final DateTime uploadedAt;
  final DateTime updatedAt;
  final int? width, height, durationMs;
  final bool hasThumb;
  final String? from; // who sent it, if they have an account

  /// "PDF" for report.pdf; empty without an extension.
  String get ext {
    final dot = name.lastIndexOf('.');
    return dot > 0 && name.length - dot <= 6 ? name.substring(dot + 1).toUpperCase() : '';
  }

  /// Sound, which the viewer plays: by its type, or by its name when the type says nothing.
  bool get isAudio => kind == FileKind.document && (mime.startsWith('audio/') || _audioExt.contains(ext));
  static const _audioExt = {'MP3', 'M4A', 'AAC', 'WAV', 'OGG', 'OGA', 'OPUS', 'FLAC'};

  Json toJson() => {'id': id, 'name': name, 'size': size, 'mime': mime, 'kind': kind.name, 'day': day};
}

/// A folder of the library: what it holds and how many see it.
class FolderInfo {
  const FolderInfo({
    required this.id,
    required this.name,
    this.files = 0,
    this.bytes = 0,
    this.senders = 0,
    this.people = 0,
    this.adminsOnly = false,
    this.cover,
    required this.createdAt,
  });

  factory FolderInfo.fromJson(Json j) => FolderInfo(
        id: _str(j['id']),
        name: _str(j['name']),
        files: _int(j['files']),
        bytes: _int(j['bytes']),
        senders: _int(j['senders']),
        people: _int(j['people']),
        adminsOnly: _bool(j['admins_only']),
        cover: j['cover'] is Map ? FileInfo.fromJson(_obj(j['cover'])) : null,
        createdAt: _time(j['created_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
      );

  /// GET /api/folders: the folders the person sees, the oldest first.
  static List<FolderInfo> listFromJson(Json j) => [for (final f in _list(j['folders'])) FolderInfo.fromJson(_obj(f))];

  final String id;
  final String name;
  final int files, bytes;
  final int senders; // the people and PIN sessions that sent its files
  final int people; // who sees it: the admins, its members and open invites
  final bool adminsOnly; // no member sees it
  final FileInfo? cover; // its newest photo or video with a thumbnail
  final DateTime createdAt;
}

class DaySummary {
  const DaySummary({required this.day, required this.count, required this.bytes});

  factory DaySummary.fromJson(Json j) => DaySummary(day: _str(j['day']), count: _int(j['count']), bytes: _int(j['bytes']));

  final String day;
  final int count;
  final int bytes;
}

class LibraryOverview {
  const LibraryOverview({required this.version, required this.days});

  factory LibraryOverview.fromJson(Json j) => LibraryOverview(
        version: _int(j['version']),
        days: [for (final d in _list(j['days'])) DaySummary.fromJson(_obj(d))],
      );

  final int version;
  final List<DaySummary> days;
}

class FilePage {
  const FilePage({required this.files, this.nextCursor});

  factory FilePage.fromJson(Json j) => FilePage(
        files: [for (final f in _list(j['files'])) FileInfo.fromJson(_obj(f))],
        nextCursor: j['next_cursor'] is String ? j['next_cursor'] as String : null,
      );

  final List<FileInfo> files;
  final String? nextCursor;
}

class FileIds {
  const FileIds({required this.ids, required this.bytes});

  factory FileIds.fromJson(Json j) => FileIds(ids: [for (final i in _list(j['ids'])) if (i is String) i], bytes: _int(j['bytes']));

  final List<String> ids;
  final int bytes;
}

// Admin (screens 17–21): PINs, people, the trash, storage.

enum PinKind {
  permanent,
  day;

  static PinKind parse(Object? v) => v == 'day' ? day : permanent;
  String get wire => this == day ? 'day' : 'permanent';
}

/// An upload PIN that still works.
class PinInfo {
  const PinInfo({
    required this.id,
    required this.code,
    required this.kind,
    required this.createdAt,
    this.expiresAt,
    required this.link,
    this.files = 0,
    this.phones = 0,
    this.folder = '',
    this.showsFolder = false,
  });

  factory PinInfo.fromJson(Json j) => PinInfo(
        id: _str(j['id']),
        code: _str(j['code']),
        kind: PinKind.parse(j['kind']),
        createdAt: _time(j['created_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        expiresAt: _time(j['expires_at']),
        link: _str(j['link']),
        files: _int(j['files']),
        phones: _int(j['phones']),
        folder: _str(j['folder']),
        showsFolder: _bool(j['shows_folder']),
      );

  final String id;
  final String code;
  final PinKind kind;
  final DateTime createdAt;
  final DateTime? expiresAt; // for a 24-hour PIN
  final String link; // the website with the PIN filled in
  final int files; // sent with it, still in the library
  final int phones; // browsers and phones that unlocked it
  final String folder; // the id of the folder it sends into
  final bool showsFolder; // guests with it also see and download the folder
}

/// A signed-in phone of someone.
/// A signed-in phone (the app) or browser (the website).
class Phone {
  const Phone({
    required this.id,
    required this.name,
    required this.createdAt,
    required this.lastSeenAt,
    this.isThis = false,
    this.isBrowser = false,
    this.homeOnly = false,
  });

  factory Phone.fromJson(Json j) => Phone(
        id: _str(j['id']),
        name: _str(j['name']),
        createdAt: _time(j['created_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        lastSeenAt: _time(j['last_seen_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        isThis: _bool(j['this']),
        isBrowser: j['client'] == 'web',
        homeOnly: _bool(j['home_only']),
      );

  final String id;
  final String name;
  final DateTime createdAt;
  final DateTime lastSeenAt;
  final bool isThis; // the phone asking
  final bool isBrowser;
  /// A browser that signed in at home, where alone it stays signed in.
  final bool homeOnly;
}

class Person {
  const Person({
    required this.id,
    required this.name,
    required this.role,
    this.username,
    this.hasPassword = false,
    this.isMe = false,
    this.lastSeenAt,
    this.phones = const [],
    this.folders = const [],
  });

  factory Person.fromJson(Json j) => Person(
        id: _str(j['id']),
        name: _str(j['name']),
        role: Role.parse(j['role']),
        username: j['username'] is String ? j['username'] as String : null,
        hasPassword: _bool(j['has_password']),
        isMe: _bool(j['me']),
        lastSeenAt: _time(j['last_seen_at']),
        phones: [for (final p in _list(j['phones'])) Phone.fromJson(_obj(p))],
        folders: [for (final f in _list(j['folders'])) if (f is String) f],
      );

  final String id;
  final String name;
  final Role role;
  final String? username;
  final bool hasPassword;
  final bool isMe;
  final DateTime? lastSeenAt;
  final List<Phone> phones;
  final List<String> folders; // the folders they see: every folder for an admin

  bool get isAdmin => role == Role.admin;

  Person copyWith({Role? role, List<Phone>? phones, List<String>? folders}) => Person(
        id: id,
        name: name,
        role: role ?? this.role,
        username: username,
        hasPassword: hasPassword,
        isMe: isMe,
        lastSeenAt: lastSeenAt,
        phones: phones ?? this.phones,
        folders: folders ?? this.folders,
      );
}

/// An invite nobody has used yet.
class OpenInvite {
  const OpenInvite({required this.id, required this.name, required this.role, this.userId, required this.expiresAt, this.folders = const []});

  factory OpenInvite.fromJson(Json j) => OpenInvite(
        id: _str(j['id']),
        name: _str(j['name']),
        role: Role.parse(j['role']),
        userId: j['user_id'] is String ? j['user_id'] as String : null,
        expiresAt: _time(j['expires_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        folders: [for (final f in _list(j['folders'])) if (f is String) f],
      );

  final String id;
  final String name;
  final Role role;
  final String? userId; // set: it adds a phone for this person
  final DateTime expiresAt;
  final List<String> folders; // the folders the new person will see
}

class People {
  const People({required this.users, required this.invites});

  factory People.fromJson(Json j) => People(
        users: [for (final u in _list(j['users'])) Person.fromJson(_obj(u))],
        invites: [for (final i in _list(j['invites'])) OpenInvite.fromJson(_obj(i))],
      );

  final List<Person> users;
  final List<OpenInvite> invites;
}

/// A fresh invite: the link is shown only now.
class NewInvite {
  const NewInvite({required this.link, required this.invite});

  factory NewInvite.fromJson(Json j) => NewInvite(link: _str(j['link']), invite: OpenInvite.fromJson(_obj(j['invite'])));

  final String link;
  final OpenInvite invite;
}

class TrashedFile {
  const TrashedFile({required this.file, required this.deletedAt, this.deletedBy, required this.purgeAt});

  factory TrashedFile.fromJson(Json j) => TrashedFile(
        file: FileInfo.fromJson(j),
        deletedAt: _time(j['deleted_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
        deletedBy: j['deleted_by'] is String ? j['deleted_by'] as String : null,
        purgeAt: _time(j['purge_at']) ?? DateTime.fromMillisecondsSinceEpoch(0),
      );

  final FileInfo file;
  final DateTime deletedAt;
  final String? deletedBy;
  final DateTime purgeAt; // when it goes for good
}

class Trash {
  const Trash({required this.files, required this.days, this.folders = const []});

  factory Trash.fromJson(Json j) => Trash(
        files: [for (final f in _list(j['files'])) TrashedFile.fromJson(_obj(f))],
        days: _int(j['trash_days'], 30),
        folders: [for (final f in _list(j['folders'])) TrashedFolder.fromJson(_obj(f))],
      );

  final List<TrashedFile> files;
  final int days;

  /// The folders the files are from; deleted ones aren't in GET /api/folders any more.
  final List<TrashedFolder> folders;
}

class TrashedFolder {
  const TrashedFolder({required this.id, required this.name, this.deleted = false});

  factory TrashedFolder.fromJson(Json j) => TrashedFolder(id: _str(j['id']), name: _str(j['name']), deleted: _bool(j['deleted']));

  final String id;
  final String name;
  final bool deleted; // the folder went too; restoring a file brings it back
}

class StorageInfo {
  const StorageInfo({
    required this.storageDir,
    this.totalBytes = 0,
    this.freeBytes = 0,
    this.files = 0,
    this.bytes = 0,
    this.trashFiles = 0,
    this.trashBytes = 0,
    this.trashDays = 30,
    this.warnings = const [],
  });

  factory StorageInfo.fromJson(Json j) => StorageInfo(
        storageDir: _str(j['storage_dir']),
        totalBytes: _int(j['total_bytes']),
        freeBytes: _int(j['free_bytes']),
        files: _int(j['files']),
        bytes: _int(j['bytes']),
        trashFiles: _int(j['trash_files']),
        trashBytes: _int(j['trash_bytes']),
        trashDays: _int(j['trash_days'], 30),
        warnings: [for (final w in _list(j['warnings'])) StorageWarning.fromJson(_obj(w))],
      );

  final String storageDir;
  final int totalBytes, freeBytes; // 0 if the server couldn't ask the drive
  final int files, bytes; // the library
  final int trashFiles, trashBytes;
  final int trashDays;

  /// What share check finds about the drive, problems first.
  final List<StorageWarning> warnings;
}

/// A problem keeps Share from working well, a warning is worth knowing (contract/storage_warnings.json).
class StorageWarning {
  const StorageWarning({required this.code, this.problem = false});

  factory StorageWarning.fromJson(Json j) => StorageWarning(code: _str(j['code']), problem: j['level'] == 'problem');

  final String code;
  final bool problem;
}
