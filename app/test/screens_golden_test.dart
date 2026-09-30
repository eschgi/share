@Tags(['golden'])
library;

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';

import 'app_test.dart' show daysAgo, inviteToken, signedInPhone, startApp, today;
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// Screenshots of the app's screens, for comparing with `docs/share-mockup/<lang>/*.png`.
///   flutter test --run-skipped --tags golden --update-goldens
void main() {
  setUpAll(loadFonts);

  Future<void> shot(WidgetTester tester, String name) async {
    await tester.pumpAndSettle();
    await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/$name.png'));
  }

  FakeServer library() => FakeServer()
    ..addDay(today(), 6)
    ..addDay(daysAgo(1), 11, startId: 100)
    ..addDay(daysAgo(3), 8, startId: 200);

  for (final lang in ['en', 'de', 'it']) {
    testWidgets('first start, $lang', (tester) async {
      await startApp(tester, FakePlatform()..secrets['language'] = lang, FakeServer());
      await shot(tester, '$lang/07-first-start');
    });

    testWidgets('invite, $lang', (tester) async {
      final platform = FakePlatform()..secrets['language'] = lang;
      await startApp(tester, platform, FakeServer());
      platform.linkEvents.add('https://share.example.com/join#$inviteToken');
      await shot(tester, '$lang/10-invite');
    });

    testWidgets('library, $lang', (tester) async {
      await startApp(tester, signedInPhone()..secrets['language'] = lang, library());
      await shot(tester, '$lang/11-library');
    });
  }

  testWidgets('sign in', (tester) async {
    await startApp(tester, FakePlatform(), FakeServer());
    await tester.tap(find.text('See & download'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).at(0), 'share.example.com');
    await tester.enterText(find.byType(TextField).at(1), 'maria');
    await tester.enterText(find.byType(TextField).at(2), 'correct horse');
    await shot(tester, 'en/08-sign-in');
  });

  testWidgets('selecting', (tester) async {
    await startApp(tester, signedInPhone(), library());
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.longPress(find.byType(GestureDetector).at(12));
    await shot(tester, 'en/12-select');
  });

  testWidgets('downloading', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, library());
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Download 6'));
    await tester.pumpAndSettle();
    platform.transferEvents.add(const TransferState(
      batch: 'batch-1', running: true, total: 6, done: 3, failed: 0, skipped: 1,
      bytesTotal: 18000000, bytesDone: 9100000, media: 5, documents: 1, local: true));
    await shot(tester, 'en/13-downloading');
  });

  testWidgets('server', (tester) async {
    final platform = signedInPhone()..current = const RouteStatus(ServerRoute.local, millis: 12);
    platform.server = platform.server!.copyWith(localUrl: Uri.parse('https://192.168.8.1:8443'), pins: ['a' * 64]);
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Server'));
    await shot(tester, 'en/16-server');
  });

  testWidgets('settings', (tester) async {
    await startApp(tester, signedInPhone()..current = const RouteStatus(ServerRoute.local, millis: 12), FakeServer());
    await tester.tap(find.text('Settings').last);
    await shot(tester, 'en/17-settings');
  });
}
