@Tags(['golden'])
library;

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/ui/theme.dart';

import 'app_test.dart' show daysAgo, signedInPhone, startApp, today;
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// Each theme on three screens, to compare them side by side:
///   flutter test --run-skipped --tags golden --update-goldens test/themes_golden_test.dart
void main() {
  setUpAll(loadFonts);

  Future<void> shot(WidgetTester tester, String name) async {
    await tester.pumpAndSettle();
    await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/themes/$name.png'));
  }

  Future<void> at(Future<void> Function() body) => withClock(Clock.fixed(DateTime(2026, 9, 30, 10, 4)), body);

  FakeServer server() => (FakeServer()..me = FakeServer.adminMe)
    ..addDay(today(), 6)
    ..addDay(daysAgo(1), 11, startId: 100);

  for (final theme in AppTheme.values) {
    testWidgets('${theme.name}: library, settings, sending', (tester) => at(() async {
          final platform = signedInPhone()
            ..secrets['theme'] = theme.name
            ..current = const RouteStatus(ServerRoute.local, millis: 12);
          await startApp(tester, platform, server());
          await shot(tester, '${theme.name}-library');

          await tester.tap(find.text('Send').last);
          await tester.pumpAndSettle();
          platform.uploadEvents.add(const UploadState(
            batch: 'up-1', auth: SendAuth.device, running: true, total: 40, done: 11,
            bytesTotal: 180000000, bytesDone: 54000000, etaSeconds: 130, local: true,
            items: [
              UploadItemState(seq: 10, name: '20260930_094512.jpg', size: 3100000, kind: FileKind.photo, state: 'done', bytes: 3100000),
              UploadItemState(seq: 11, name: '20260930_094530.mp4', size: 52000000, kind: FileKind.video, state: 'queued', bytes: 33280000),
              UploadItemState(seq: 12, name: 'Kindergarten_form.pdf', size: 480000, kind: FileKind.document, state: 'queued'),
            ],
          ));
          await shot(tester, '${theme.name}-send');

          await tester.tap(find.text('Settings').last);
          await shot(tester, '${theme.name}-settings');
        }));
  }

  testWidgets('the picker', (tester) => at(() async {
        await startApp(tester, signedInPhone(), server());
        await tester.tap(find.text('Settings').last);
        await tester.pumpAndSettle();
        await tester.tap(find.text('Theme'));
        await shot(tester, 'picker-ember');
        await tester.ensureVisible(find.text('Linen'));
        await tester.pumpAndSettle();
        await tester.tap(find.text('Linen'));
        await shot(tester, 'picker-linen');
      }));
}
