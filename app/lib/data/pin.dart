/// Sending without an account: a PIN session on this phone, like the website's.
library;

import 'dart:async';
import 'dart:convert';

import 'api.dart';
import 'library.dart';
import 'models.dart';
import 'platform.dart';
import 'server.dart';

/// The PIN this phone sends with.
class PinSession {
  const PinSession({required this.server, required this.kind, this.expiresAt, this.folderName, this.showsFolder = false, this.ended = false});

  /// As kept on the phone, or as the server says it (GET /api/session) with [server] added.
  factory PinSession.fromJson(Json j) => PinSession(
        server: Uri.parse(j['server'] as String? ?? ''),
        kind: PinKind.parse(j['pin_kind']),
        expiresAt: j['expires_at'] is String ? DateTime.tryParse(j['expires_at'] as String)?.toLocal() : null,
        folderName: j['folder_name'] is String ? j['folder_name'] as String : null,
        showsFolder: j['shows_folder'] == true,
      );

  final Uri server;
  final PinKind kind;
  final DateTime? expiresAt; // a 24-hour PIN
  final String? folderName; // the folder it sends into; null while the server has one folder
  final bool showsFolder; // everyone with the PIN also sees its folder
  final bool ended; // the PIN stopped working; a new one takes over what's unfinished

  Json toJson() => {
        'server': server.toString(),
        'pin_kind': kind.wire,
        'expires_at': expiresAt?.toUtc().toIso8601String(),
        'folder_name': folderName,
        'shows_folder': showsFolder,
      };

  PinSession copyWith({bool? ended}) =>
      PinSession(server: server, kind: kind, expiresAt: expiresAt, folderName: folderName, showsFolder: showsFolder, ended: ended ?? this.ended);
}

const _tokenKey = 'pin_token';
const _sessionKey = 'pin_session';

class PinRepository {
  PinRepository({required this.api, required this.platform});

  final Api api;
  final Platform platform;
  final _states = StreamController<PinSession?>.broadcast();
  PinSession? _current;

  PinSession? get current => _current;
  Stream<PinSession?> get states => _states.stream;

  void _set(PinSession? s) {
    _current = s;
    _states.add(s);
  }

  /// At start: the stored PIN, and whether it still works.
  Future<void> restore() async {
    final raw = await platform.readSecret(_sessionKey);
    final token = await platform.readSecret(_tokenKey);
    if (raw == null || token == null) return _set(null);
    final saved = PinSession.fromJson(jsonDecode(raw) as Json);
    _set(saved);
    try {
      // The folder may have a new name, or show itself now.
      final now = PinSession.fromJson({...await api.getFrom(saved.server, '/api/session', bearer: token), 'server': saved.server.toString()});
      await platform.writeSecret(_sessionKey, jsonEncode(now.toJson()));
      _set(now);
    } on ApiException catch (e) {
      if (e.status == 401) _set(saved.copyWith(ended: true));
    } on NetworkException {
      // Offline: it may well still work.
    }
  }

  /// Unlocks sending with [code]. The unfinished uploads of an earlier PIN move to this one on
  /// the server, so they go on. Throws ApiException pin_wrong (attemptsLeft), pin_ended,
  /// pin_locked (retryAfter) or pin_format. The [secret] of a PIN link that shows an encrypted
  /// folder opens it here, and with the [root]'s fingerprint of a PIN that only sends into a
  /// folder with keys, both name the root its uploads check the folder's key with
  /// (docs/e2ee-plan.md); a typed code has neither.
  Future<void> unlock(Uri server, String code, {String? secret, String? root}) async {
    final old = await platform.readSecret(_tokenKey);
    final res = await api.postTo(server, '/api/pin/unlock', {'code': code, 'client': 'app'}, bearer: old);
    final info = PinSession.fromJson({...(res['session'] as Map? ?? const {}).cast<String, dynamic>(), 'server': server.toString()});
    final config = ServerConfig(publicUrl: server).withInfo(ServerInfo.fromJson((res['server'] as Map? ?? const {}).cast()));
    await platform.writeSecret(_tokenKey, res['token'] as String?);
    await platform.writeSecret(_sessionKey, jsonEncode(info.toJson()));
    await platform.savePinServer(config);
    _set(info);
    try {
      await platform.pinLink(secret: secret, root: root);
    } on KeysException {
      // its files show locked; opening the link again tries once more
    }
    await platform.resumeUploads(SendAuth.pin);
  }

  /// What the PIN's folder is fetched with, while the PIN shows it (screen 46).
  Future<PinAccess?> access() async {
    final s = _current;
    final token = await platform.readSecret(_tokenKey);
    return s == null || token == null || !s.showsFolder ? null : (server: s.server, token: token);
  }

  /// The server said the PIN ended: a new one takes over what's unfinished.
  void ended() {
    final s = _current;
    if (s != null && !s.ended) _set(s.copyWith(ended: true));
  }

  /// Stops sending with a PIN on this phone.
  Future<void> forget() async {
    final token = await platform.readSecret(_tokenKey);
    final server = _current?.server;
    if (token != null && server != null) {
      try {
        await api.postTo(server, '/api/session/end', const {}, bearer: token);
      } on Exception {
        // Forgotten here either way.
      }
    }
    await platform.writeSecret(_tokenKey, null);
    await platform.writeSecret(_sessionKey, null);
    await platform.savePinServer(null);
    try {
      await platform.forgetPinKeys();
    } on KeysException {
      // nothing to forget
    }
    _set(null);
  }
}
