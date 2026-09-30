/// Sending without an account: a PIN session on this phone, like the website's.
library;

import 'dart:async';
import 'dart:convert';

import 'api.dart';
import 'models.dart';
import 'platform.dart';
import 'server.dart';

/// The PIN this phone sends with.
class PinSession {
  const PinSession({required this.server, required this.kind, this.expiresAt, this.ended = false});

  factory PinSession.fromJson(Json j) => PinSession(
        server: Uri.parse(j['server'] as String? ?? ''),
        kind: PinKind.parse(j['pin_kind']),
        expiresAt: j['expires_at'] is String ? DateTime.tryParse(j['expires_at'] as String)?.toLocal() : null,
      );

  final Uri server;
  final PinKind kind;
  final DateTime? expiresAt; // a 24-hour PIN
  final bool ended; // the PIN stopped working; a new one takes over what's unfinished

  Json toJson() => {'server': server.toString(), 'pin_kind': kind.wire, 'expires_at': expiresAt?.toUtc().toIso8601String()};

  PinSession copyWith({bool? ended}) => PinSession(server: server, kind: kind, expiresAt: expiresAt, ended: ended ?? this.ended);
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
      await api.getFrom(saved.server, '/api/session', bearer: token);
    } on ApiException catch (e) {
      if (e.status == 401) _set(saved.copyWith(ended: true));
    } on NetworkException {
      // Offline: it may well still work.
    }
  }

  /// Unlocks sending with [code]. The unfinished uploads of an earlier PIN move to this one on
  /// the server, so they go on. Throws ApiException pin_wrong (attemptsLeft), pin_ended,
  /// pin_locked (retryAfter) or pin_format.
  Future<void> unlock(Uri server, String code) async {
    final old = await platform.readSecret(_tokenKey);
    final res = await api.postTo(server, '/api/pin/unlock', {'code': code, 'client': 'app'}, bearer: old);
    final session = (res['session'] as Map? ?? const {}).cast<String, dynamic>();
    final info = PinSession(
      server: server,
      kind: PinKind.parse(session['pin_kind']),
      expiresAt: session['expires_at'] is String ? DateTime.tryParse(session['expires_at'] as String)?.toLocal() : null,
    );
    final config = ServerConfig(publicUrl: server).withInfo(ServerInfo.fromJson((res['server'] as Map? ?? const {}).cast()));
    await platform.writeSecret(_tokenKey, res['token'] as String?);
    await platform.writeSecret(_sessionKey, jsonEncode(info.toJson()));
    await platform.savePinServer(config);
    _set(info);
    await platform.resumeUploads(SendAuth.pin);
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
    _set(null);
  }
}
