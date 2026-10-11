import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:share_app/data/access.dart';
import 'package:share_app/data/api.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';
import 'package:share_app/data/session.dart';

import 'support/fake_platform.dart';

const id = '0123456789abcdef0123456789abcdef.access';
final secret = 'f' * 64;
final access = CloudflareAccess(host: 'share.example.com', clientId: id, clientSecret: secret);

final config = ServerConfig(
  publicUrl: Uri.parse('https://share.example.com'),
  localUrl: Uri.parse('https://192.168.8.1:8443'),
  pins: ['ab' * 32],
  serverId: 'srv',
);

void main() {
  group('the token', () {
    test('goes only to its public address, over https', () {
      expect(access.appliesTo(Uri.parse('https://share.example.com')), isTrue);
      expect(access.appliesTo(Uri.parse('https://SHARE.example.com')), isTrue);
      expect(access.appliesTo(Uri.parse('https://192.168.8.1:8443')), isFalse);
      expect(access.appliesTo(Uri.parse('http://share.example.com')), isFalse);
      expect(access.copyWith(on: false).appliesTo(Uri.parse('https://share.example.com')), isFalse);
    });

    test('comes from an invite\'s link as <client id>:<client secret>', () {
      final a = CloudflareAccess.fromLink('Share.Example.com', '$id:$secret')!;
      expect((a.host, a.clientId, a.clientSecret, a.on), ('share.example.com', id, secret, true));
      expect(a.linkValue, '$id:$secret');
      expect(CloudflareAccess.fromLink('share.example.com', 'nodot:$secret'), isNull);
      expect(CloudflareAccess.fromLink('share.example.com', '$id:short'), isNull);
      expect(CloudflareAccess.fromLink('share.example.com', id), isNull);
      expect(CloudflareAccess.fromLink('share.example.com', null), isNull);
    });

    test('is found in what Cloudflare copies, or in a curl command or JSON', () {
      expect(accessFromClipboard('CF-Access-Client-Id: $id\nCF-Access-Client-Secret: $secret'), (id: id, secret: secret));
      expect(accessFromClipboard('curl -H "CF-Access-Client-Id: $id" -H "CF-Access-Client-Secret: $secret" https://share.example.com'),
          (id: id, secret: secret));
      expect(accessFromClipboard('{"cf-access-client-id": "$id", "cf-access-client-secret": "$secret"}'), (id: id, secret: secret));
      expect(accessFromClipboard('  $id  '), (id: id, secret: null));
      expect(accessFromClipboard(secret), (id: null, secret: secret));
      expect(accessFromClipboard('hello'), (id: null, secret: null));
    });
  });

  group('invite links', () {
    const token = 'shi_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG';
    final keySecret = '${'B' * 42}w';

    test('carry the token after &cfa=, apart from the invite\'s secret', () {
      final link = parseLink('https://share.example.com/join#$token.$keySecret&cfa=$id:$secret') as InviteLink;
      expect((link.token, link.secret, link.access?.clientId, link.access?.clientSecret, link.access?.host),
          (token, keySecret, id, secret, 'share.example.com'));
      final bare = parseLink('https://share.example.com/join#$token&cfa=$id:$secret') as InviteLink;
      expect((bare.secret, bare.access?.clientId), (null, id));
      expect((parseLink('https://share.example.com/join#$token.$keySecret') as InviteLink).access, isNull);
      expect((parseLink('https://share.example.com/join#$token&cfa=garbage') as InviteLink).access, isNull);
    });

    test('carry it in the form the invite page hands to the app', () {
      final q = Uri(queryParameters: {'server': 'https://share.example.com', 'token': token, 'cfa': '$id:$secret'}).query;
      final link = parseLink('com.eschgi.share://join?$q') as InviteLink;
      expect(link.access?.clientSecret, secret);
    });

    test('the QR code\'s link has it, and reads back', () {
      final qr = inviteLinkWithAccess('https://share.example.com/join#$token.$keySecret', access);
      expect((parseLink(qr) as InviteLink).access?.linkValue, access.linkValue);
    });
  });

  group('the API', () {
    late FakePlatform platform;
    late List<http.BaseRequest> sent;

    http.Client answering(http.Response Function(http.BaseRequest req) answer) => MockClient((req) async {
          sent.add(req);
          return answer(req);
        });

    http.Response json(Object body, [int status = 200, Map<String, String> headers = const {}]) =>
        http.Response(jsonEncode(body), status, headers: {'content-type': 'application/json', ...headers});

    setUp(() {
      platform = FakePlatform();
      sent = [];
    });

    test('sends the token to the public address, never at home, and follows no redirects', () async {
      final api = Api(platform: platform, publicClient: answering((_) => json({'ok': true})), localClient: (_) => answering((_) => json({'ok': true})))
        ..config = config
        ..access = access;
      await api.get('/api/library');
      platform.current = const RouteStatus(ServerRoute.local);
      await api.get('/api/library');
      expect(sent[0].headers['CF-Access-Client-Id'], id);
      expect(sent[0].headers['CF-Access-Client-Secret'], secret);
      expect(sent[0].followRedirects, isFalse);
      expect(sent[1].url.host, '192.168.8.1');
      expect(sent[1].headers.containsKey('CF-Access-Client-Id'), isFalse);
      await api.getFrom(Uri.parse('https://other.example.org'), '/api/info');
      expect(sent[2].headers.containsKey('CF-Access-Client-Id'), isFalse, reason: 'another server');
    });

    test('Cloudflare\'s login instead of Share is CloudflareAccessException, and the library hears of it', () async {
      var answer = http.Response('', 302, headers: {'location': 'https://family.cloudflareaccess.com/cdn-cgi/access/login/share.example.com'});
      final api = Api(platform: platform, publicClient: answering((_) => answer))..config = config;
      final problems = <AccessProblem?>[];
      api.accessProblems.listen(problems.add);

      await expectLater(api.get('/api/library'), throwsA(isA<CloudflareAccessException>().having((e) => e.tokenSent, 'token sent', false)));
      expect(api.accessProblem, AccessProblem.missing);

      // Strict service token authentication: Cloudflare's own 403, not Share's JSON.
      api.access = access;
      answer = http.Response('<html>Forbidden</html>', 403, headers: {'cf-ray': '8c1f-FRA', 'server': 'cloudflare'});
      await expectLater(api.get('/api/library'), throwsA(isA<CloudflareAccessException>().having((e) => e.tokenSent, 'token sent', true)));
      expect(api.accessProblem, AccessProblem.refused);

      answer = json({'ok': true}, 200, {'cf-ray': '8c1f-FRA'});
      await api.get('/api/library');
      expect(api.accessProblem, isNull);
      await Future<void>.delayed(Duration.zero);
      expect(problems, [AccessProblem.missing, AccessProblem.refused, null]);
    });

    test('Share\'s own refusals through Cloudflare stay Share\'s', () async {
      final api = Api(
        platform: platform,
        publicClient: answering((_) => json({'error': {'code': 'signed_out', 'message': 'Signed out.'}}, 401, {'cf-ray': '8c1f-FRA'})),
      )..config = config;
      await expectLater(api.get('/api/me'), throwsA(isA<ApiException>().having((e) => e is CloudflareAccessException, 'from Cloudflare Access', false)));
      expect(api.accessProblem, isNull);
    });

    test('checking a server behind Cloudflare Access doesn\'t say it isn\'t Share', () async {
      final api = Api(platform: platform, publicClient: answering((_) => http.Response('', 302, headers: {'location': 'https://family.cloudflareaccess.com/'})));
      final session = SessionRepository(api: api, platform: platform);
      await expectLater(session.checkServer(Uri.parse('https://share.example.com')), throwsA(isA<CloudflareAccessException>()));
    });
  });

  group('the repository', () {
    test('keeps the token with the secrets, for Kotlin too', () async {
      final platform = FakePlatform();
      final api = Api(platform: platform);
      final repo = AccessRepository(api: api, platform: platform);
      await repo.save(access);
      expect(jsonDecode((await platform.readSecret(AccessRepository.key))!), access.toJson());
      api.access = null;
      await repo.restore();
      expect(repo.current?.clientSecret, secret);
      expect(repo.forServer(Uri.parse('https://share.example.com'))?.clientId, id);
      expect(repo.forServer(Uri.parse('https://other.example.org')), isNull);
      await repo.save(null);
      expect(await platform.readSecret(AccessRepository.key), isNull);
    });
  });
}
