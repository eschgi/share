import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:share_app/data/api.dart';
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
}
