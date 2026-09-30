import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:share_app/data/api.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';
import 'package:share_app/data/session.dart';

import 'support/contract.dart';
import 'support/fake_platform.dart';

void main() {
  late FakePlatform platform;
  late Map<String, http.Response Function(http.Request)> routes;
  late SessionRepository session;
  final server = Uri.parse('https://share.example.com');
  const token = 'shi_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG';

  http.Response json(Object body, [int status = 200]) => http.Response(jsonEncode(body), status);

  setUp(() {
    platform = FakePlatform();
    routes = {
      'GET /api/info': (_) => json(contractResponse('api/info.json')),
      'POST /api/invites/accept': (_) => json(contractResponse('api/invite_accept.json')),
      'POST /api/invites/peek': (_) => json(contractResponse('api/invite_peek.json')),
      'GET /api/me': (_) => json(contractResponse('api/me.json')),
      'POST /api/auth/logout': (_) => http.Response('', 204),
    };
    final api = Api(
      platform: platform,
      publicClient: MockClient((req) async {
        final handler = routes['${req.method} ${req.url.path}'];
        return handler == null ? json({'error': {'code': 'not_found', 'message': ''}}, 404) : handler(req);
      }),
    );
    session = SessionRepository(api: api, platform: platform);
  });

  test('accepting an invite signs in and keeps both addresses', () async {
    final link = InviteLink(server, token);
    expect((await session.peekInvite(link)).inviter, 'Stefan');
    await session.acceptInvite(link);
    final state = session.current as SignedInState;
    expect(state.user.name, 'Maria');
    expect(platform.secrets['device_token'], startsWith('shd_'));
    expect(platform.server!.publicUrl.toString(), 'https://share.example.com');
    expect(platform.server!.localUrl.toString(), 'https://192.168.8.1:8443');
    expect(platform.server!.pins.single, hasLength(64));
    expect(platform.server!.serverId, contractResponse('api/info.json')['server_id']);
    expect(platform.routeChecks, 1, reason: 'the local address is tried right away');
  });

  test('a wrong password says how many tries are left', () async {
    routes['POST /api/auth/login'] = (_) => json({'error': {'code': 'login_wrong', 'message': '', 'attempts_left': 9}}, 401);
    await expectLater(session.signIn(server, 'maria', 'nope'),
        throwsA(isA<ApiException>().having((e) => e.attemptsLeft, 'attemptsLeft', 9)));
    expect(platform.secrets, isEmpty);
  });

  test('an address without a Share server is refused', () async {
    routes['GET /api/info'] = (_) => http.Response('<html>hello</html>', 200);
    await expectLater(session.signIn(server, 'maria', 'pw'), throwsA(isA<NotShareServer>()));
  });

  test('restoring: signed in, offline with the stored person, or signed out by the server', () async {
    await session.acceptInvite(InviteLink(server, token));
    final again = SessionRepository(api: Api(platform: platform, publicClient: MockClient((_) async => json(contractResponse('api/me.json')))), platform: platform);
    await again.restore();
    expect((again.current as SignedInState).user.name, 'Maria');

    final offline = SessionRepository(
        api: Api(platform: platform, publicClient: MockClient((_) async => throw http.ClientException('offline'))), platform: platform);
    await offline.restore();
    expect(offline.current, isA<SignedInState>(), reason: 'the library opens offline');

    final revoked = SessionRepository(
        api: Api(platform: platform, publicClient: MockClient((_) async => json({'error': {'code': 'signed_out', 'message': ''}}, 401))),
        platform: platform);
    await revoked.restore();
    await pumpEventQueue();
    expect(revoked.current, isA<SignedOutState>().having((s) => s.byServer, 'byServer', true));
    expect(platform.secrets['device_token'], isNull);
    expect(platform.server, isNotNull, reason: 'the address stays, for signing in again');
  });

  test('signing out forgets the key', () async {
    await session.acceptInvite(InviteLink(server, token));
    await session.signOut();
    expect(session.current, isA<SignedOutState>());
    expect(platform.secrets, isEmpty);
    expect(User.fromJson(const {}).role, Role.member);
  });

  test('an unknown certificate on the local address fetches the pins again, once in a while', () async {
    var fetched = 0;
    final rotated = {...contractResponse('api/server.json'), 'local_cert_sha256': ['ab' * 32]};
    routes['GET /api/server'] = (_) {
      fetched++;
      return json(rotated);
    };
    await session.acceptInvite(InviteLink(server, token));
    await pumpEventQueue();
    fetched = 0;
    final checks = platform.routeChecks;

    platform.routeEvents.add(const RouteStatus(ServerRoute.public, reason: RouteReason.wrongCertificate));
    await pumpEventQueue();
    expect(fetched, 1);
    expect(platform.server!.pins, ['ab' * 32]);
    expect(platform.routeChecks, checks + 1, reason: 'and the local address is tried with the new pin');

    platform.routeEvents.add(const RouteStatus(ServerRoute.public, reason: RouteReason.wrongCertificate));
    platform.routeEvents.add(const RouteStatus(ServerRoute.public, reason: RouteReason.unreachable));
    await pumpEventQueue();
    expect(fetched, 1, reason: 'not again within five minutes, and not for other reasons');
  });
}
