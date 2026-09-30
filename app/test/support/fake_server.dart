import 'dart:convert';
import 'dart:typed_data';

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
      'GET /api/library': (req) => json(_library(req.url.queryParameters)),
      'GET /api/files': (req) => json(_files(req.url.queryParameters)),
      'GET /api/files/ids': (req) {
        final list = _filtered(req.url.queryParameters);
        return json({'ids': [for (final f in list) f['id']], 'bytes': list.fold<int>(0, (s, f) => s + (f['size'] as int))});
      },
    };
  }

  late final Map<String, http.Response Function(http.Request)> routes;
  final requests = <http.Request>[];

  Map<String, dynamic> me = contractResponse('api/me.json');

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
