import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/server.dart';

import 'support/contract.dart';

void main() {
  group('normalizePublicAddress', () {
    test('adds https and keeps only the origin', () {
      expect(normalizePublicAddress(' Share.Example.com ').toString(), 'https://share.example.com');
      expect(normalizePublicAddress('https://share.example.com/join#x').toString(), 'https://share.example.com');
      expect(normalizePublicAddress('share.example.com:8443').toString(), 'https://share.example.com:8443');
    });
    test('refuses what can not be a public address', () {
      expect(normalizePublicAddress(''), isNull);
      expect(normalizePublicAddress('http://share.example.com'), isNull);
      expect(normalizePublicAddress('http://8.8.8.8:8080'), isNull);
      expect(normalizePublicAddress('ftp://x'), isNull);
      expect(normalizePublicAddress('not an address'), isNull);
    });
    test('takes plain http for an address at home, also without the http://', () {
      expect(normalizePublicAddress('192.168.8.52:8080').toString(), 'http://192.168.8.52:8080');
      expect(normalizePublicAddress('http://192.168.8.52:8080/').toString(), 'http://192.168.8.52:8080');
      expect(normalizePublicAddress('https://192.168.8.52:8443').toString(), 'https://192.168.8.52:8443');
      expect(normalizePublicAddress('Share.local:8080').toString(), 'http://share.local:8080');
      expect(normalizeLocalAddress('http://10.0.0.2:8080').toString(), 'http://10.0.0.2:8080');
    });
  });

  test('isHomeHost agrees with the server (contract/home_hosts.json)', () {
    for (final h in contract('home_hosts.json')['hosts'] as List) {
      final host = (h as Map)['host'] as String;
      expect(isHomeHost(host), h['home'], reason: host);
    }
  });

  group('parseLink', () {
    const token = 'shi_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG';
    test('reads the invite link people get', () {
      final link = parseLink('https://share.example.com/join#$token') as InviteLink;
      expect(link.server.toString(), 'https://share.example.com');
      expect(link.token, token);
    });
    test('reads the link the invite page hands to the app', () {
      final link = parseLink('com.eschgi.share://join?server=https%3A%2F%2Fshare.example.com&token=$token') as InviteLink;
      expect(link.server.toString(), 'https://share.example.com');
      expect(link.token, token);
    });
    test('reads PIN links', () {
      final link = parseLink('https://share.example.com/#k7m2q') as PinLink;
      expect(link.code, 'K7M2Q');
    });
    test('reads links to a server at home over plain http', () {
      final link = parseLink('http://192.168.8.52:8080/join#$token') as InviteLink;
      expect(link.server.toString(), 'http://192.168.8.52:8080');
      final handed = parseLink('com.eschgi.share://join?server=http%3A%2F%2F192.168.8.52%3A8080&token=$token') as InviteLink;
      expect(handed.server.toString(), 'http://192.168.8.52:8080');
      expect(parseLink('http://share.example.com/join#$token'), isNull);
    });
    test('ignores anything else', () {
      expect(parseLink('https://example.com/'), isNull);
      expect(parseLink('https://share.example.com/join#nope'), isNull);
      expect(parseLink('com.eschgi.share://join?server=http%3A%2F%2Fx&token=$token'), isNull);
      expect(parseLink('hello'), isNull);
    });
  });
}
