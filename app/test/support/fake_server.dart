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
      'GET /api/info': (_) => json({...contractResponse('api/info.json'), 'storage': s3 ? 's3' : 'disk'}),
      'GET /api/me': (_) => json(me),
      'POST /api/invites/peek': (_) => json(contractResponse('api/invite_peek.json')),
      'POST /api/invites/accept': (_) => json(contractResponse('api/invite_accept.json')),
      'POST /api/auth/logout': (_) => http.Response('', 204),
      'GET /api/server': (_) => json(contractResponse('api/server.json')),
      'GET /api/about': (_) => json(contractResponse('api/about.json')),
      'GET /api/me/devices': (_) => json({'devices': myDevices}),
      'GET /api/folders': (_) => json({'folders': [for (final f in folders) _withCounts(f)]}),
      'GET /api/library': (req) => _seen(req) ?? json(_library(req.url.queryParameters)),
      'GET /api/files': (req) => _seen(req) ?? json(_files(req.url.queryParameters)),
      'GET /api/files/ids': (req) {
        final list = _filtered(req.url.queryParameters);
        return _seen(req) ?? json({'ids': [for (final f in list) f['id']], 'bytes': list.fold<int>(0, (s, f) => s + (f['size'] as int))});
      },
      ..._adminRoutes(),
      'POST /api/pin/unlock': (req) {
        final code = (_body(req)['code'] as String? ?? '').toUpperCase();
        if (code != pinCode) return json({'error': {'code': 'pin_wrong', 'message': '', 'attempts_left': 4}}, 401);
        final res = contractResponse('api/pin_unlock_app.json');
        res['session'] = {...(res['session'] as Map).cast<String, dynamic>(), ..._pinSession};
        return json(res);
      },
      'GET /api/session': (_) => pinEnded ? json({'error': {'code': 'session_ended', 'message': ''}}, 401) : json({'kind': 'pin', ..._pinSession}),
    };
  }

  /// The PIN that unlocks, and whether the stored session's PIN has ended.
  String pinCode = 'K7M2Q';
  bool pinEnded = false;

  /// The PIN's folder, named only where there are several, and whether the PIN shows it.
  String? pinFolderName;
  bool pinShowsFolder = false;

  Map<String, dynamic> get _pinSession => {
        'pin_kind': 'day',
        'expires_at': clock.now().add(const Duration(hours: 20)).toUtc().toIso8601String(),
        'folder_name': pinFolderName,
        'shows_folder': pinShowsFolder,
      };

  /// The folders the person sees: one, as on most servers, unless a test adds more.
  List<Map<String, dynamic>> folders = [folder('f4mily5x2k7mbqz4bwdbyj6qsq', 'Family')];
  String get firstFolder => folders.first['id'] as String;

  /// A folder like the contract's; what it holds is counted from [files] unless [files] and
  /// [bytes] say.
  static Map<String, dynamic> folder(String id, String name, {int? files, int? bytes, int people = 4, Map<String, dynamic>? cover}) {
    final example = ((contractResponse('api/folders.json')['folders'] as List).first as Map).cast<String, dynamic>()
      ..remove('files')
      ..remove('bytes');
    return {
      ...example,
      'id': id,
      'name': name,
      'files': ?files,
      'bytes': ?bytes,
      'people': people,
      'cover': cover,
    };
  }

  /// A folder as the server lists it.
  Map<String, dynamic> _withCounts(Map<String, dynamic> f) {
    final mine = files.where((x) => x['folder'] == f['id']);
    return {'files': mine.length, 'bytes': mine.fold<int>(0, (s, x) => s + (x['size'] as int)), ...f};
  }

  /// A folder the person doesn't see answers 404, as if it didn't exist.
  http.Response? _seen(http.Request req) {
    final folder = req.url.queryParameters['folder'];
    return folder == null || folders.any((f) => f['id'] == folder) ? null : _error(404, 'not_found');
  }

  /// Grows with every change to the library, as the server's.
  int version = 1;

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

  Map<String, dynamic> _pin(String kind, String code, {String? folder, bool showsFolder = false}) {
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
      'folder': folder ?? firstFolder,
      'shows_folder': showsFolder,
    };
  }

  /// What's wrong with a folder name, as the server says it; null if nothing.
  http.Response? _folderName(String name, {String? except}) {
    if (name.isEmpty || name.length > 60 || RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(name)) return _error(400, 'bad_request');
    final taken = folders.any((f) => f['id'] != except && (f['name'] as String).toLowerCase() == name.toLowerCase());
    return taken ? _error(409, 'folder_name_taken') : null;
  }

  /// Files gone to Recently deleted, as an admin deleted them.
  void _toTrash(Iterable<Map<String, dynamic>> gone) {
    final now = clock.now().toUtc();
    trash.insertAll(0, [
      for (final f in gone)
        {...f, 'deleted_at': now.toIso8601String(), 'deleted_by': 'Stefan', 'purge_at': now.add(const Duration(days: 30)).toIso8601String()},
    ]);
  }

  Map<String, http.Response Function(http.Request)> _adminRoutes() => {
        'GET /api/pins': (_) => json({'pins': pins}),
        'GET /api/pins/suggest': (_) => json({'code': 'R8D4W'}),
        'POST /api/pins': (req) {
          final b = _body(req);
          if (b['folder'] == null) return _error(400, 'bad_request'); // as the server: every PIN says its folder
          final code = (b['code'] as String? ?? 'Z${_made}XYZ').toUpperCase();
          if (usedCodes.contains(code)) return _error(409, 'pin_taken');
          usedCodes.add(code);
          final pin = _pin(b['kind'] as String, code, folder: b['folder'] as String?, showsFolder: b['shows_folder'] == true);
          pins.add(pin);
          return json(pin, 201);
        },
        'POST /api/files/move': (req) {
          final b = _body(req);
          final to = b['folder'] as String;
          if (!folders.any((f) => f['id'] == to)) return _error(404, 'not_found');
          final ids = (b['ids'] as List).cast<String>().toSet();
          var changed = 0;
          for (final f in files) {
            if (ids.contains(f['id']) && f['folder'] != to) {
              f['folder'] = to;
              changed++;
            }
          }
          if (changed > 0) version++;
          return json({'changed': changed});
        },
        'POST /api/folders': (req) {
          final name = (_body(req)['name'] as String? ?? '').trim();
          final problem = _folderName(name);
          if (problem != null) return problem;
          _made++;
          final f = {...folder('f${_made}new${'a' * 20}', name, people: 1), 'admins_only': true, 'created_at': clock.now().toUtc().toIso8601String()};
          folders.add(f);
          version++;
          return json(_withCounts(f), 201);
        },
        'GET /api/users': (_) => json(people),
        'POST /api/invites': (req) {
          final b = _body(req);
          if (b['role'] == 'member' && b['folders'] == null) return _error(400, 'bad_request'); // a member's invite says the folders
          return _newInvite(b['name'] as String, b['role'] as String, null, folders: (b['folders'] as List?)?.cast<String>() ?? const []);
        },
        'POST /api/files/delete': (req) {
          final ids = (_body(req)['ids'] as List).cast<String>().toSet();
          final gone = files.where((f) => ids.contains(f['id'])).toList();
          files.removeWhere((f) => ids.contains(f['id']));
          _toTrash(gone);
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

  http.Response _newInvite(String name, String role, String? userId, {List<String> folders = const []}) {
    _made++;
    final token = 'shi_${'x' * 40}$_made';
    final invite = {
      'id': 'i${_made}new${'a' * 20}',
      'name': name,
      'role': role,
      'user_id': userId,
      'folders': role == 'admin' ? [for (final f in this.folders) f['id']] : folders,
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
    final folder = RegExp(r'^/api/folders/([^/]+)(?:/(people|invites)/([^/]+))?$').firstMatch(path);
    if (folder != null) {
      final id = folder.group(1)!;
      final f = folders.where((f) => f['id'] == id).firstOrNull;
      if (f == null) return _error(404, 'not_found');
      final who = folder.group(3);
      if (who != null && (req.method == 'PUT' || req.method == 'DELETE')) {
        final key = folder.group(2) == 'people' ? 'users' : 'invites';
        final list = [for (final x in people[key] as List) (x as Map).cast<String, dynamic>()];
        if (!list.any((x) => x['id'] == who)) return _error(404, 'not_found');
        List<Object?> given(Map<String, dynamic> x) => [for (final g in x['folders'] as List? ?? const []) if (g != id) g];
        people[key] = [for (final x in list) x['id'] != who ? x : {...x, 'folders': [...given(x), if (req.method == 'PUT') id]}];
        version++;
        return _noContent;
      }
      if (req.method == 'PATCH') {
        final name = (_body(req)['name'] as String? ?? '').trim();
        final problem = _folderName(name, except: id);
        if (problem != null) return problem;
        f['name'] = name;
        version++;
        return json(_withCounts(f));
      }
      if (req.method == 'DELETE') {
        if (folders.length <= 1) return _error(409, 'last_folder');
        folders.remove(f);
        final gone = files.where((x) => x['folder'] == id).toList();
        files.removeWhere((x) => x['folder'] == id);
        _toTrash(gone);
        pins.removeWhere((p) => p['folder'] == id);
        version++;
        return json({'changed': gone.length});
      }
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

  /// The files are in a bucket: the app fetches them by link from [bucketHost].
  bool s3 = false;

  static const bucketHost = 'bucket.example.com';

  /// A file's link in the bucket, signed the way S3 does it.
  static String bucketLink(String id) =>
      'https://$bucketHost/share/files/$id?X-Amz-Credential=AKIA%2F20261005%2Fauto%2Fs3%2Faws4_request&response-content-disposition=attachment%3B%20filename%3D%22x.jpg%22&X-Amz-Signature=sig';

  /// What a file holds, from the server or the bucket.
  static Uint8List contentOf(String id) => Uint8List.fromList(utf8.encode('the bytes of $id'));

  http.Client get client => MockClient((req) async {
        requests.add(req);
        final path = req.url.path;
        if (req.url.host == bucketHost) {
          if (req.headers.containsKey('Authorization')) return http.Response('a key at the bucket', 400);
          final id = path.split('/').last;
          return req.url.toString() == bucketLink(id) ? http.Response.bytes(contentOf(id), 200) : http.Response('a changed link', 403);
        }
        final link = RegExp(r'^/api/s3/files/([a-z2-7]+)/url$').firstMatch(path);
        if (link != null) {
          return s3 ? json({'url': bucketLink(link.group(1)!), 'expires_at': '2026-10-05T22:00:00Z'}) : json({'error': {'code': 'not_found', 'message': ''}}, 404);
        }
        final content = RegExp(r'^/api/files/([a-z2-7]+)/content$').firstMatch(path);
        if (content != null) {
          if (s3 && req.headers.containsKey('Authorization')) return json({'error': {'code': 's3_use_url', 'message': ''}}, 409);
          return http.Response.bytes(contentOf(content.group(1)!), 200);
        }
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
          if ((q['folder'] == null || f['folder'] == q['folder']) &&
              (q['kind'] == null || f['kind'] == q['kind']) &&
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
    return {'version': version, 'days': days.values.toList()};
  }

  Map<String, dynamic> _files(Map<String, String> q) {
    final list = _filtered(q);
    final limit = int.tryParse(q['limit'] ?? '') ?? 200;
    final start = int.tryParse(q['cursor'] ?? '') ?? 0;
    final page = list.skip(start).take(limit).toList();
    final next = start + limit < list.length ? '${start + limit}' : null;
    return {'files': page, 'next_cursor': next};
  }

  /// Adds [count] files on [day]: photos, a video now and then, and a document; into [folder],
  /// the first folder unless said.
  void addDay(String day, int count, {int startId = 0, String? folder}) {
    for (var i = 0; i < count; i++) {
      final n = startId + i;
      final kind = i == 2 ? 'video' : (i == 3 ? 'document' : 'photo');
      files.add({
        'id': _id(n),
        'folder': folder ?? firstFolder,
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
