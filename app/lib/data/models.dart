/// What the server's API returns (contract/api in the repository), parsed tolerantly: a
/// missing or odd field gives a default instead of an exception.
library;

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
        expiresAt: _time(j['expires_at']) ?? DateTime.now(),
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

  Json toJson() => {'id': id, 'name': name, 'size': size, 'mime': mime, 'kind': kind.name, 'day': day};
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
