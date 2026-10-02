import 'dart:convert';
import 'dart:typed_data';

import 'package:clock/clock.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';

import 'contract.dart';

/// A Share server in memory, answering like the real one (shapes from contract/).
class FakeServer {
  FakeServer() {
    routes = {
      'GET /api/info': (_) => json(contractResponse('api/info.json')),
      'GET /api/me': (_) => json(me),
      'POST /api/invites/peek': (_) => json(contractResponse('api/invite_peek.json')),
      'POST /api/invites/accept': (_) => json(contractResponse('api/invite_accept.json')),
      'POST /api/auth/logout': (_) => http.Response('', 204),
      'GET /api/server': (_) => json(contractResponse('api/server.json')),
      'GET /api/me/devices': (_) => json({'devices': myDevices}),
      'GET /api/library': (req) => json(_library(req.url.queryParameters)),
      'GET /api/files': (req) => json(_files(req.url.queryParameters)),
      'GET /api/files/ids': (req) {
        final list = _filtered(req.url.queryParameters);
        return json({'ids': [for (final f in list) f['id']], 'bytes': list.fold<int>(0, (s, f) => s + (f['size'] as int))});
      },
      ..._adminRoutes(),
      'POST /api/pin/unlock': (req) {
        final code = (_body(req)['code'] as String? ?? '').toUpperCase();
        if (code != pinCode) return json({'error': {'code': 'pin_wrong', 'message': '', 'attempts_left': 4}}, 401);
        final res = contractResponse('api/pin_unlock_app.json');
        res['session'] = {...(res['session'] as Map).cast<String, dynamic>(), 'expires_at': clock.now().add(const Duration(hours: 20)).toUtc().toIso8601String()};
        return json(res);
      },
      'GET /api/session': (_) => pinEnded
          ? json({'error': {'code': 'session_ended', 'message': ''}}, 401)
          : json({'kind': 'pin', 'pin_kind': 'day', 'expires_at': clock.now().add(const Duration(hours: 20)).toUtc().toIso8601String()}),
    };
  }

  /// The PIN that unlocks, and whether the stored session's PIN has ended.
  String pinCode = 'K7M2Q';
  bool pinEnded = false;

  /// A signed-in admin, for the admin screens.
  static Map<String, dynamic> get adminMe => {
        'user': {'id': 'u1ld5x2k7mbqz4bwdbyj6qsqxa', 'name': 'Stefan', 'role': 'admin', 'username': 'stefan', 'has_password': true},
        'device': {'id': 'd1ld5x2k7mbqz4bwdbyj6qsqxa', 'name': 'Pixel 9'},
      };

  // What admins manage, starting from the contract's examples.
  List<Map<String, dynamic>> pins = [for (final p in contractResponse('api/pins.json')['pins'] as List) (p as Map).cast()];
  Map<String, dynamic> people = contractResponse('api/people.json');
  List<Map<String, dynamic>> trash = [];
  Map<String, dynamic> storage = contractResponse('api/storage.json');
  final usedCodes = <String>{'K7M2Q', '4HX9T'};
  var _made = 0;

  List<Map<String, dynamic>> get _users => [for (final u in people['users'] as List) (u as Map).cast()];
  List<Map<String, dynamic>> get _invites => [for (final i in people['invites'] as List) (i as Map).cast()];

  Map<String, dynamic> _body(http.Request req) => req.body.isEmpty ? {} : (jsonDecode(req.body) as Map).cast();

  http.Response _error(int status, String code) => json({'error': {'code': code, 'message': ''}}, status);

  http.Response get _noContent => http.Response('', 204);

  Map<String, dynamic> _pin(String kind, String code) {
    _made++;
    final now = clock.now().toUtc();
    return {
      'id': 'p${_made}new${'a' * 20}',
      'code': code,
      'kind': kind,
      'created_at': now.toIso8601String(),
      'expires_at': kind == 'day' ? now.add(const Duration(days: 1)).toIso8601String() : null,
      'link': 'https://share.example.com/#$code',
      'files': 0,
      'phones': 0,
    };
  }

  Map<String, http.Response Function(http.Request)> _adminRoutes() => {
        'GET /api/pins': (_) => json({'pins': pins}),
        'GET /api/pins/suggest': (_) => json({'code': 'R8D4W'}),
        'POST /api/pins': (req) {
          final b = _body(req);
          final code = (b['code'] as String? ?? 'Z${_made}XYZ').toUpperCase();
          if (usedCodes.contains(code)) return _error(409, 'pin_taken');
          usedCodes.add(code);
          final pin = _pin(b['kind'] as String, code);
          pins.add(pin);
          return json(pin, 201);
        },
        'GET /api/users': (_) => json(people),
        'POST /api/invites': (req) {
          final b = _body(req);
          return _newInvite(b['name'] as String, b['role'] as String, null);
        },
        'POST /api/files/delete': (req) {
          final ids = (_body(req)['ids'] as List).cast<String>().toSet();
          final gone = files.where((f) => ids.contains(f['id'])).toList();
          files.removeWhere((f) => ids.contains(f['id']));
          final now = clock.now().toUtc();
          trash.insertAll(0, [
            for (final f in gone)
              {...f, 'deleted_at': now.toIso8601String(), 'deleted_by': 'Stefan', 'purge_at': now.add(const Duration(days: 30)).toIso8601String()},
          ]);
          return json({'changed': gone.length});
        },
        'GET /api/trash': (_) => json({'files': trash, 'trash_days': 30}),
        'POST /api/trash/restore': (req) {
          final ids = (_body(req)['ids'] as List).cast<String>().toSet();
          final back = trash.where((f) => ids.contains(f['id'])).toList();
          trash.removeWhere((f) => ids.contains(f['id']));
          for (final f in back) {
            files.add(Map.of(f)..removeWhere((k, _) => k == 'deleted_at' || k == 'deleted_by' || k == 'purge_at'));
          }
          files.sort((a, b) => (b['uploaded_at'] as String).compareTo(a['uploaded_at'] as String));
          return json({'changed': back.length});
        },
        'POST /api/trash/purge': (req) {
          final ids = (_body(req)['ids'] as List).cast<String>().toSet();
          final before = trash.length;
          trash.removeWhere((f) => ids.contains(f['id']));
          return json({'changed': before - trash.length});
        },
        'GET /api/admin/storage': (_) => json(storage),
      };

  http.Response _newInvite(String name, String role, String? userId) {
    _made++;
    final token = 'shi_${'x' * 40}$_made';
    final invite = {
      'id': 'i${_made}new${'a' * 20}',
      'name': name,
      'role': role,
      'user_id': userId,
      'created_at': clock.now().toUtc().toIso8601String(),
      'expires_at': clock.now().toUtc().add(const Duration(days: 1)).toIso8601String(),
    };
    people['invites'] = [..._invites, invite];
    return json({'token': token, 'link': 'https://share.example.com/join#$token', 'invite': invite}, 201);
  }

  /// The admin routes with an id in the path.
  http.Response? _adminPath(http.Request req) {
    final path = req.url.path;
    final pinAction = RegExp(r'^/api/pins/([^/]+)/(new-code|end)$').firstMatch(path);
    if (req.method == 'POST' && pinAction != null) {
      final i = pins.indexWhere((p) => p['id'] == pinAction.group(1));
      if (i < 0) return _error(404, 'not_found');
      final old = pins.removeAt(i);
      if (pinAction.group(2) == 'end') return _noContent;
      final pin = _pin(old['kind'] as String, 'N${_made}CDE');
      pins.insert(i, pin);
      return json(pin);
    }
    final password = RegExp(r'^/api/users/([^/]+)/password$').firstMatch(path);
    if (req.method == 'POST' && password != null) {
      final u = _users.where((u) => u['id'] == password.group(1)).firstOrNull;
      if (u == null) return _error(404, 'not_found');
      if (u['me'] == true) return _error(403, 'forbidden');
      final username = (_body(req)['username'] as String?)?.trim() ?? u['username'] as String?;
      if (username == null || username.isEmpty) return _error(400, 'bad_request');
      if (_users.any((x) => x['id'] != u['id'] && x['username'] == username)) return _error(409, 'username_taken');
      people['users'] = [for (final x in _users) x['id'] == u['id'] ? {...x, 'username': username, 'has_password': true} : x];
      return json({...contractResponse('api/user_password.json'), 'username': username});
    }
    final user = RegExp(r'^/api/users/([^/]+)(/invites)?$').firstMatch(path);
    if (user != null) {
      final id = user.group(1);
      final u = _users.where((u) => u['id'] == id).firstOrNull;
      if (u == null) return _error(404, 'not_found');
      if (user.group(2) != null && req.method == 'POST') return _newInvite(u['name'] as String, u['role'] as String, id);
      final admins = _users.where((u) => u['role'] == 'admin').length;
      if (req.method == 'PATCH') {
        final role = _body(req)['role'] as String;
        if (u['role'] == 'admin' && role != 'admin' && admins <= 1) return _error(409, 'last_admin');
        people['users'] = [for (final x in _users) x['id'] == id ? {...x, 'role': role} : x];
        return _noContent;
      }
      if (req.method == 'DELETE') {
        if (u['role'] == 'admin' && admins <= 1) return _error(409, 'last_admin');
        people['users'] = [for (final x in _users) if (x['id'] != id) x];
        return _noContent;
      }
    }
    final device = RegExp(r'^/api/devices/([^/]+)$').firstMatch(path);
    if (req.method == 'DELETE' && device != null) {
      myDevices = [for (final d in myDevices) if (d['id'] != device.group(1)) d];
      people['users'] = [
        for (final u in _users) {...u, 'phones': [for (final p in u['phones'] as List) if ((p as Map)['id'] != device.group(1)) p]},
      ];
      return _noContent;
    }
    final invite = RegExp(r'^/api/invites/([^/]+)$').firstMatch(path);
    if (req.method == 'DELETE' && invite != null) {
      people['invites'] = [for (final i in _invites) if (i['id'] != invite.group(1)) i];
      return _noContent;
    }
    return null;
  }

  late final Map<String, http.Response Function(http.Request)> routes;
  final requests = <http.Request>[];

  Map<String, dynamic> me = contractResponse('api/me.json');

  /// The phones and browsers of the person signed in; the phone asking is the app.
  List<Map<String, dynamic>> myDevices = [
    for (final d in contractResponse('api/me_devices.json')['devices'] as List) {...(d as Map).cast<String, dynamic>(), 'this': d['client'] == 'app'},
  ];

  /// Files as the API returns them, newest first.
  List<Map<String, dynamic>> files = [];

  http.Response json(Object body, [int status = 200]) =>
      http.Response.bytes(utf8.encode(jsonEncode(body)), status, headers: {'content-type': 'application/json'});

  http.Client get client => MockClient((req) async {
        requests.add(req);
        final path = req.url.path;
        if (req.method == 'GET' && path.endsWith('/thumb')) return http.Response.bytes(Uint8List(0), 404);
        final handler = routes['${req.method} $path'];
        if (handler != null) return handler(req);
        final admin = _adminPath(req);
        if (admin != null) return admin;
        final m = RegExp(r'^/api/files/([a-z2-7]+)$').firstMatch(path);
        if (m != null) return json(files.firstWhere((f) => f['id'] == m.group(1)));
        return json({'error': {'code': 'not_found', 'message': ''}}, 404);
      });

  List<Map<String, dynamic>> _filtered(Map<String, String> q) => [
        for (final f in files)
          if ((q['kind'] == null || f['kind'] == q['kind']) &&
              (q['day'] == null || f['day'] == q['day']) &&
              (q['q'] == null || (f['name'] as String).toLowerCase().contains(q['q']!.toLowerCase())))
            f,
      ];

  Map<String, dynamic> _library(Map<String, String> q) {
    final days = <String, Map<String, dynamic>>{};
    for (final f in _filtered(q)) {
      final d = days.putIfAbsent(f['day'] as String, () => {'day': f['day'], 'count': 0, 'bytes': 0});
      d['count'] = (d['count'] as int) + 1;
      d['bytes'] = (d['bytes'] as int) + (f['size'] as int);
    }
    return {'version': 1, 'days': days.values.toList()};
  }

  Map<String, dynamic> _files(Map<String, String> q) {
    final list = _filtered(q);
    final limit = int.tryParse(q['limit'] ?? '') ?? 200;
    final start = int.tryParse(q['cursor'] ?? '') ?? 0;
    final page = list.skip(start).take(limit).toList();
    final next = start + limit < list.length ? '${start + limit}' : null;
    return {'files': page, 'next_cursor': next};
  }

  /// Adds [count] files on [day]: photos, a video now and then, and a document.
  void addDay(String day, int count, {int startId = 0}) {
    for (var i = 0; i < count; i++) {
      final n = startId + i;
      final kind = i == 2 ? 'video' : (i == 3 ? 'document' : 'photo');
      files.add({
        'id': _id(n),
        'name': kind == 'document' ? 'Car_insurance.pdf' : (kind == 'video' ? 'VID_$n.mp4' : 'IMG_$n.jpg'),
        'size': 3000000 + n * 1000,
        'mime': kind == 'document' ? 'application/pdf' : (kind == 'video' ? 'video/mp4' : 'image/jpeg'),
        'kind': kind,
        'day': day,
        'uploaded_at': '${day}T10:${(59 - i).toString().padLeft(2, '0')}:00Z',
        'updated_at': '${day}T10:${(59 - i).toString().padLeft(2, '0')}:00Z',
        'width': kind == 'photo' ? 4000 : null,
        'height': kind == 'photo' ? 3000 : null,
        'duration_ms': kind == 'video' ? 18000 : null,
        'has_thumb': false,
        'from': null,
      });
    }
  }

  static String _id(int n) {
    const alphabet = 'abcdefghijklmnopqrstuvwxyz234567';
    final b = StringBuffer();
    var v = n + 1;
    for (var i = 0; i < 26; i++) {
      b.write(alphabet[v % 32]);
      v = v ~/ 32;
    }
    return b.toString();
  }
}
