import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:share_app/data/api.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';

import 'support/fake_platform.dart';

final config = ServerConfig(
  publicUrl: Uri.parse('https://share.example.com'),
  localUrl: Uri.parse('https://192.168.8.1:8443'),
  pins: ['ab' * 32],
  serverId: 'srv',
);

void main() {
  late FakePlatform platform;
  late List<String> calls;

  http.Client client(String name, {bool fail = false, int status = 200, Object body = const {'ok': true}}) =>
      MockClient((req) async {
        calls.add('$name ${req.method} ${req.url}');
        if (fail) throw http.ClientException('no route to host');
        return http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json'});
      });

  setUp(() {
    platform = FakePlatform();
    calls = [];
  });

  test('uses the public address unless the route says local', () async {
    final api = Api(platform: platform, publicClient: client('public'), localClient: (_) => client('local'))..config = config;
    await api.get('/api/library');
    platform.current = const RouteStatus(ServerRoute.local);
    await api.get('/api/library', query: {'kind': 'photo'});
    expect(calls, [
      'public GET https://share.example.com/api/library',
      'local GET https://192.168.8.1:8443/api/library?kind=photo',
    ]);
  });

  test('while the route is being checked, a request waits for it instead of going public', () async {
    // Right after signing in: the check of the address at home is still running.
    platform
      ..current = const RouteStatus(ServerRoute.public, checking: true)
      ..afterCheck = const RouteStatus(ServerRoute.local);
    final api = Api(platform: platform, publicClient: client('public'), localClient: (_) => client('local'))..config = config;
    await api.get('/api/library');
    expect(calls, ['local GET https://192.168.8.1:8443/api/library']);
    expect(platform.routeChecks, 1);
  });

  test('a read that fails over the local address is tried over the public one', () async {
    platform.current = const RouteStatus(ServerRoute.local);
    final api = Api(platform: platform, publicClient: client('public'), localClient: (_) => client('local', fail: true))
      ..config = config;
    expect(await api.get('/api/me'), {'ok': true});
    expect(calls, ['local GET https://192.168.8.1:8443/api/me', 'public GET https://share.example.com/api/me']);
    expect(platform.routeChecks, 1, reason: 'the route is checked again');
  });

  test('a write is not repeated: it may have arrived', () async {
    platform.current = const RouteStatus(ServerRoute.local);
    final api = Api(platform: platform, publicClient: client('public'), localClient: (_) => client('local', fail: true))
      ..config = config;
    await expectLater(api.post('/api/auth/logout'), throwsA(isA<NetworkException>()));
    expect(calls, hasLength(1));
  });

  group('a server only at home, over plain http', () {
    final home = ServerConfig(publicUrl: Uri.parse('http://192.168.8.52:8080'), serverId: 'srv', deviceId: 'dv1');

    test('the key goes there once the server proved here to be this phone\'s', () async {
      platform.current = const RouteStatus(ServerRoute.public, reason: RouteReason.noLocal, publicVerified: true);
      final api = Api(platform: platform, publicClient: client('public'))
        ..config = home
        ..token = 'shd_x';
      await api.get('/api/library');
      expect(calls, ['public GET http://192.168.8.52:8080/api/library']);
    });

    test('on a network it wasn\'t checked on, the check comes first; without the proof nothing goes', () async {
      platform.current = const RouteStatus(ServerRoute.public, reason: RouteReason.noLocal, checking: true);
      final api = Api(platform: platform, publicClient: client('public'))
        ..config = home
        ..token = 'shd_x';
      await expectLater(api.get('/api/library'), throwsA(isA<NetworkException>()));
      expect(platform.routeChecks, 1, reason: 'it waited for the check');
      expect(calls, isEmpty, reason: 'the key went nowhere');
    });

    test('an address at home over http is used like a pinned one, by the route', () async {
      platform.current = const RouteStatus(ServerRoute.local);
      final api = Api(platform: platform, publicClient: client('public'), localClient: (_) => client('local'))
        ..config = ServerConfig(publicUrl: Uri.parse('https://share.example.com'), localUrl: Uri.parse('http://192.168.8.52:8080'), deviceId: 'dv1')
        ..token = 'shd_x';
      await api.get('/api/library');
      expect(calls, ['local GET http://192.168.8.52:8080/api/library']);
    });
  });

  test('the bearer token goes along, and signed_out ends the session', () async {
    var signedOut = 0;
    late http.Request seen;
    final api = Api(
      platform: platform,
      publicClient: MockClient((req) async {
        seen = req;
        return http.Response(jsonEncode({'error': {'code': 'signed_out', 'message': 'no'}}), 401);
      }),
    )
      ..config = config
      ..token = 'shd_x'
      ..onSignedOut = () => signedOut++;
    await expectLater(api.get('/api/me'), throwsA(isA<ApiException>().having((e) => e.signedOut, 'signedOut', true)));
    expect(seen.headers['Authorization'], 'Bearer shd_x');
    expect(signedOut, 1);
  });

  test('error details and pages that are not ours', () async {
    final api = Api(platform: platform, publicClient: MockClient((req) async {
      if (req.url.path == '/api/auth/login') {
        return http.Response(jsonEncode({'error': {'code': 'login_locked', 'message': '', 'retry_after_seconds': 90}}), 429);
      }
      return http.Response('<html>Bad gateway</html>', 502);
    }))
      ..config = config;
    await expectLater(api.post('/api/auth/login', {}),
        throwsA(isA<ApiException>().having((e) => e.retryAfter, 'retryAfter', const Duration(seconds: 90))));
    await expectLater(api.get('/api/library'), throwsA(isA<ApiException>().having((e) => e.code, 'code', 'unavailable')));
  });

  test('asks each server once where it keeps its files', () async {
    var asked = 0;
    final info = MockClient((req) async {
      calls.add('${req.method} ${req.url}');
      if (req.url.host == 'down.example.com' && asked++ == 0) return http.Response('', 503);
      final storage = req.url.host == 'pin.example.com' ? 'disk' : 's3';
      return http.Response(jsonEncode({'server_id': 'x', 'storage': storage}), 200, headers: {'content-type': 'application/json'});
    });
    final api = Api(platform: platform, publicClient: info)..config = config;
    expect(await api.storage(), Storage.s3);
    expect(await api.storage(), Storage.s3);
    expect(await api.storage(server: Uri.parse('https://pin.example.com')), Storage.disk);
    expect(calls, ['GET https://share.example.com/api/info', 'GET https://pin.example.com/api/info']);
    final down = Uri.parse('https://down.example.com');
    await expectLater(api.storage(server: down), throwsA(isA<ApiException>()));
    expect(await api.storage(server: down), Storage.s3); // a failure isn't kept
  });

  test('a link to the bucket is fetched as it is, never with the key', () async {
    String? seen;
    var keys = 0;
    var signedOut = 0;
    final bucket = MockClient((req) async {
      seen = req.url.toString();
      if (req.headers.containsKey('Authorization')) keys++;
      return http.Response.bytes([1, 2, 3], req.url.host == 'deny.example.com' ? 403 : 200);
    });
    final api = Api(platform: platform, publicClient: bucket, localClient: (_) => client('local'))
      ..config = config
      ..token = 'shd_x'
      ..onSignedOut = () => signedOut++;
    platform.current = const RouteStatus(ServerRoute.local); // at home, still not over the address there
    const link = 'https://acct.r2.cloudflarestorage.com/share/files/b?X-Amz-Credential=key%2F20261005%2Fauto%2Fs3%2Faws4_request&response-content-disposition=attachment%3B%20filename%3D%22a%20b.jpg%22&X-Amz-Signature=ab';
    expect(await api.s3Bytes(link), [1, 2, 3]);
    expect(seen, link);
    expect(keys, 0);
    expect(calls, isEmpty);
    await expectLater(api.s3Bytes('https://deny.example.com/x'), throwsA(isA<ApiException>().having((e) => e.status, 'status', 403)));
    expect(signedOut, 0);
    await expectLater(api.s3Bytes('http://bucket.example.com/x'), throwsA(isA<ApiException>().having((e) => e.code, 'code', 'insecure_link')));
    expect(await api.s3Bytes('http://192.168.8.52:9000/share/files/b?X-Amz-Signature=cd'), [1, 2, 3]);
  });
}
