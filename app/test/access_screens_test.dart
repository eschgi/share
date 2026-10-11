import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/access.dart';
import 'package:share_app/ui/widgets.dart';

import 'admin_test.dart' show adminServer, openSettings, tapInList;
import 'app_test.dart' show inviteToken, signedInPhone, startApp;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

const id = '0123456789abcdef0123456789abcdef.access';
final secret = 'f' * 64;
String stored(String id, String secret, {bool on = true}) =>
    jsonEncode(CloudflareAccess(host: 'share.example.com', clientId: id, clientSecret: secret, on: on).toJson());

void main() {
  setUpAll(loadFonts);

  testWidgets('signing in behind Cloudflare Access: the app says what\'s in the way, takes the token and goes on', (tester) async {
    final server = FakeServer()
      ..accessToken = '$id:$secret'
      ..routes['POST /api/auth/login'] = (_) => FakeServer().json(contractResponse('api/login.json'));
    final platform = FakePlatform();
    await startApp(tester, platform, server);
    await tester.tap(find.text('See & download'));
    await tester.pumpAndSettle();
    expect(find.text('Behind Cloudflare Access'), findsOneWidget);
    await tester.enterText(find.byType(TextField).at(0), 'share.example.com');
    await tester.enterText(find.byType(TextField).at(1), 'maria');
    await tester.enterText(find.byType(TextField).at(2), 'correct horse');
    await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
    await tester.pumpAndSettle();

    expect(find.text('Cloudflare Access is in the way'), findsOneWidget);
    await tester.tap(find.text('Enter the token'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).at(0), id);
    await tester.enterText(find.byType(TextField).at(1), 'wrongsecretwrongsecret');
    await tester.tap(find.widgetWithText(FilledButton, 'Continue'));
    await tester.pumpAndSettle();
    expect(find.text('Cloudflare turned this token away. Check both values, or ask for a new token.'), findsOneWidget);
    expect(platform.secrets[AccessRepository.key], isNull, reason: 'kept only once Cloudflare takes it');

    await tester.enterText(find.byType(TextField).at(1), secret);
    await tester.tap(find.widgetWithText(FilledButton, 'Continue'));
    await tester.pumpAndSettle();
    expect(find.text('Library'), findsWidgets, reason: 'the sign-in went on by itself');
    expect(jsonDecode(platform.secrets[AccessRepository.key]!), {'host': 'share.example.com', 'client_id': id, 'client_secret': secret, 'on': true});
    expect(server.requests.last.headers['CF-Access-Client-Secret'], secret);
  });

  testWidgets('when Cloudflare turns the token away, the library says so and leads to the fix', (tester) async {
    final server = FakeServer()..accessToken = '$id:$secret';
    // Who's signed in is kept on the phone, so the library opens without the server.
    final platform = signedInPhone()
      ..secrets['user'] = jsonEncode(contractResponse('api/me.json')['user'])
      ..secrets[AccessRepository.key] = stored(id, 'e' * 64);
    await startApp(tester, platform, server);
    expect(find.text('Cloudflare turned the token away'), findsOneWidget);
    await tester.tap(find.text('Update the token'));
    await tester.pumpAndSettle();
    expect(find.text(id), findsOneWidget, reason: 'the stored ID is filled in');
    await tester.enterText(find.byType(TextField).at(1), secret);
    await tester.tap(find.widgetWithText(FilledButton, 'Continue'));
    await tester.pumpAndSettle();
    expect(find.text('Cloudflare turned the token away'), findsNothing);
  });

  testWidgets('the server settings show the token, and switch it off without forgetting it', (tester) async {
    final server = FakeServer()..accessToken = '$id:$secret';
    final platform = signedInPhone()..secrets[AccessRepository.key] = stored(id, secret);
    await startApp(tester, platform, server);
    await openSettings(tester);
    expect(find.text('Public address in use · Cloudflare Access'), findsOneWidget);
    await tapInList(tester, find.text('Server'));
    expect(find.text(id), findsOneWidget);
    expect(find.text(secret), findsNothing, reason: 'the secret stays hidden');
    expect(find.text('Cloudflare lets this phone in'), findsOneWidget);
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(find.text(id), findsNothing);
    expect(jsonDecode(platform.secrets[AccessRepository.key]!)['on'], false);
  });

  testWidgets('an admin\'s invite brings the token in the QR code, never in the link', (tester) async {
    final server = adminServer()..accessToken = '$id:$secret';
    final platform = signedInPhone()..secrets[AccessRepository.key] = stored(id, secret);
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Peter');
    await tester.pump();
    expect(find.text('Bring the Cloudflare Access token'), findsOneWidget);
    expect(find.text("Peter's phone then gets past Cloudflare by itself"), findsOneWidget);
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    expect(tester.widget<QrCodeView>(find.byType(QrCodeView)).data, endsWith('&cfa=$id:$secret'));
    await tester.tap(find.text('Send as a link, without the token'));
    expect(platform.sharedTexts.single, isNot(contains('cfa=')));

    await tester.tap(find.byType(Switch));
    await tester.pump();
    expect(tester.widget<QrCodeView>(find.byType(QrCodeView)).data, isNot(contains('cfa=')));
    expect(find.text('Send as a link instead'), findsOneWidget);
  });

  testWidgets('an invite that brings the token gets through, and says so', (tester) async {
    final server = FakeServer()..accessToken = '$id:$secret';
    final platform = FakePlatform()..launchLink = 'https://share.example.com/join#$inviteToken&cfa=$id:$secret';
    await startApp(tester, platform, server);
    expect(find.text('Cloudflare token included'), findsOneWidget);
    expect(jsonDecode(platform.secrets[AccessRepository.key]!)['client_secret'], secret);
  });

  testWidgets('a token from an invite that Cloudflare turns away is replaced, not brought back', (tester) async {
    final server = FakeServer()..accessToken = '$id:$secret';
    final platform = FakePlatform()..launchLink = 'https://share.example.com/join#$inviteToken&cfa=$id:${'e' * 64}';
    await startApp(tester, platform, server);
    expect(find.text('Cloudflare turned this token away. Check both values, or ask for a new token.'), findsOneWidget);
    await tester.enterText(find.byType(TextField).at(1), secret);
    await tester.tap(find.widgetWithText(FilledButton, 'Continue'));
    await tester.pumpAndSettle();
    expect(find.text('Cloudflare token included'), findsOneWidget);
    expect(jsonDecode(platform.secrets[AccessRepository.key]!)['client_secret'], secret);
  });
}
