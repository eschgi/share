import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/server.dart';

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
      expect(normalizePublicAddress('ftp://x'), isNull);
      expect(normalizePublicAddress('not an address'), isNull);
    });
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
    test('ignores anything else', () {
      expect(parseLink('https://example.com/'), isNull);
      expect(parseLink('https://share.example.com/join#nope'), isNull);
      expect(parseLink('com.eschgi.share://join?server=http%3A%2F%2Fx&token=$token'), isNull);
      expect(parseLink('hello'), isNull);
    });
  });
}
