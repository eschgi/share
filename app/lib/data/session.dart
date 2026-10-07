/// Whether this phone is signed in, and as whom.
library;

import 'dart:async';
import 'dart:convert';

import 'package:clock/clock.dart';

import 'api.dart';
import 'models.dart';
import 'platform.dart';
import 'server.dart';

sealed class SessionState {
  const SessionState();
}

class SessionLoading extends SessionState {
  const SessionLoading();
}

class SignedOutState extends SessionState {
  const SignedOutState({this.byServer = false});

  /// The server ended this phone's session (revoked, or the account deleted).
  final bool byServer;
}

class SignedInState extends SessionState {
  const SignedInState(this.user);
  final User user;
}

/// The server has no Share at this address.
class NotShareServer implements Exception {
  const NotShareServer();
}

const _tokenKey = 'device_token';
const _userKey = 'user';

class SessionRepository {
  SessionRepository({required this.api, required this.platform}) {
    api.onSignedOut = () => unawaited(_clear(byServer: true));
    // The local address shows a certificate that isn't pinned: maybe the server has a new
    // one (`share cert regenerate`), which /api/server tells over the public address.
    platform.routes.listen((r) {
      if (r.reason != RouteReason.wrongCertificate || _current is! SignedInState) return;
      final now = clock.now();
      if (_certCheckedAt != null && now.difference(_certCheckedAt!) < const Duration(minutes: 5)) return;
      _certCheckedAt = now;
      unawaited(refreshServer());
    });
  }

  final Api api;
  final Platform platform;
  final _states = StreamController<SessionState>.broadcast();
  SessionState _current = const SessionLoading();
  DateTime? _certCheckedAt;

  SessionState get current => _current;
  Stream<SessionState> get states => _states.stream;

  void _set(SessionState s) {
    _current = s;
    _states.add(s);
  }

  /// At start: the stored key and server, then who the server says we are. Without a
  /// connection the stored person is used, so the library opens (from its cache) offline.
  Future<void> restore() async {
    final token = await platform.readSecret(_tokenKey);
    final config = await platform.loadServer();
    if (token == null || config == null) return _set(const SignedOutState());
    api
      ..config = config
      ..token = token;
    User? cached;
    final raw = await platform.readSecret(_userKey);
    if (raw != null) cached = User.fromJson(jsonDecode(raw) as Json);
    try {
      final me = await api.get('/api/me');
      final user = User.fromJson(me['user'] as Json? ?? const {});
      // Phones signed in before the id was kept: it's needed for the proof over plain http.
      final device = (me['device'] as Json?)?['id'];
      if (device is String && config.deviceId != device) {
        final updated = config.copyWith(deviceId: device);
        await platform.saveServer(updated);
        api.config = updated;
      }
      await platform.writeSecret(_userKey, jsonEncode(_userJson(user)));
      _set(SignedInState(user));
      unawaited(refreshServer());
      unawaited(_syncKeys());
    } on ApiException catch (e) {
      if (e.signedOut) return; // onSignedOut cleared it
      _set(cached != null ? SignedInState(cached) : const SignedOutState());
    } on NetworkException {
      _set(cached != null ? SignedInState(cached) : const SignedOutState());
    }
  }

  /// The keys for encrypted folders, opened again (docs/e2ee-plan.md); with [password] right
  /// after signing in with it, which opens them on a new phone. Never fails the session.
  Future<void> _syncKeys({String? password}) async {
    try {
      await platform.syncKeys(password: password);
    } on KeysException {
      // the keys' state says so
    }
  }

  /// Checks that there is a Share server at [server], and returns what it says of itself.
  Future<ServerIdentity> checkServer(Uri server) async {
    try {
      final info = await api.getFrom(server, '/api/info');
      if (info['server_id'] is! String) throw const NotShareServer();
      return ServerIdentity.fromJson(info);
    } on FormatException {
      throw const NotShareServer();
    } on ApiException {
      throw const NotShareServer();
    }
  }

  Future<void> signIn(Uri server, String username, String password) async {
    final identity = await checkServer(server);
    final res = await api.postTo(server, '/api/auth/login', {
      'username': username.trim(),
      'password': password,
      'device_name': await platform.deviceName(),
    });
    await _signedIn(server, identity, SignedIn.fromJson(res));
    unawaited(_syncKeys(password: password));
  }

  Future<InvitePeek> peekInvite(InviteLink link) async =>
      InvitePeek.fromJson(await api.postTo(link.server, '/api/invites/peek', {'token': link.token}));

  Future<void> acceptInvite(InviteLink link) async {
    final identity = await checkServer(link.server);
    final res = await api.postTo(link.server, '/api/invites/accept', {
      'token': link.token,
      'device_name': await platform.deviceName(),
    });
    final signedIn = SignedIn.fromJson(res);
    await _signedIn(link.server, identity, signedIn);
    // The keys the link's secret opens: the folders', or the person's own for a new phone.
    unawaited(platform.keysFromInvite(link.secret, signedIn.keys, signedIn.root).then<void>((_) {}, onError: (Object _) => _syncKeys()));
  }

  Future<void> _signedIn(Uri server, ServerIdentity identity, SignedIn s) async {
    final config = ServerConfig(publicUrl: server, serverId: identity.serverId, deviceId: s.deviceId).withInfo(s.server);
    await platform.writeSecret(_tokenKey, s.token);
    await platform.writeSecret(_userKey, jsonEncode(_userJson(s.user)));
    await platform.saveServer(config);
    api
      ..config = config
      ..token = s.token;
    unawaited(platform.route(check: true));
    _set(SignedInState(s.user));
  }

  /// Picks up a changed local address or certificate, e.g. after `share cert regenerate`.
  Future<void> refreshServer() async {
    final c = api.config;
    if (c == null) return;
    try {
      final updated = c.withInfo(ServerInfo.fromJson(await api.get('/api/server')));
      if (jsonEncode(updated.toJson()) == jsonEncode(c.toJson())) return;
      await platform.saveServer(updated);
      api.config = updated;
      unawaited(platform.route(check: true));
    } on Exception {
      // Next time.
    }
  }

  /// Changes the local address by hand (screen 16).
  Future<void> setLocalAddress(Uri? local) async {
    final c = api.config;
    if (c == null) return;
    final updated = local == null ? c.copyWith(clearLocal: true) : c.copyWith(localUrl: local);
    await platform.saveServer(updated);
    api.config = updated;
    if (local != null) await refreshServer();
    await platform.route(check: true);
  }

  Future<void> setPassword({required String username, required String password, String? current}) async {
    // With the person's key open here, it stays locked with the new password, so the password
    // opens it on a new phone or browser too.
    String? lock;
    try {
      lock = await platform.passwordLock(password);
    } on KeysException {
      lock = null;
    }
    await api.put('/api/me/password', {
      'username': username,
      'password': password,
      if (current != null && current.isNotEmpty) 'current_password': current,
      'password_lock': ?lock,
    });
    final me = await api.get('/api/me');
    final user = User.fromJson(me['user'] as Json? ?? const {});
    await platform.writeSecret(_userKey, jsonEncode(_userJson(user)));
    _set(SignedInState(user));
  }

  /// The phones and browsers signed in as oneself, most recently used first.
  Future<List<Phone>> myDevices() async =>
      [for (final d in (await api.get('/api/me/devices'))['devices'] as List? ?? const []) Phone.fromJson((d as Map).cast())];

  /// Signs out one of one's own phones or browsers, such as a lost one.
  Future<void> signOutDevice(String id) => api.delete('/api/devices/$id');

  Future<void> signOut() async {
    try {
      await api.post('/api/auth/logout');
    } on Exception {
      // Signed out here either way; the server forgets the key when it's revoked or unused.
    }
    await _clear();
  }

  /// Deletes the account on the server. Throws ApiException(last_admin) for the only admin.
  Future<void> deleteAccount() async {
    await api.post('/api/me/delete');
    await _clear();
  }

  Future<void> _clear({bool byServer = false}) async {
    await platform.writeSecret(_tokenKey, null);
    await platform.writeSecret(_userKey, null);
    api.token = null;
    _set(SignedOutState(byServer: byServer));
    unawaited(_syncKeys()); // signed out: the keys go
  }

  static Json _userJson(User u) => {
        'id': u.id,
        'name': u.name,
        'role': u.role.name,
        'username': u.username,
        'has_password': u.hasPassword,
      };
}
