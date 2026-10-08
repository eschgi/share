import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';

import 'app_test.dart' show startApp;
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// Scanning an invite's QR code (issue #5): what the scanner reads opens, and when it can't scan,
/// the link is asked for, saying why, instead of nothing happening.
void main() {
  setUpAll(loadFonts);

  final invite = 'https://share.example.com/join#shi_${'x' * 40}';

  testWidgets('a scanned invite opens', (tester) async {
    final platform = FakePlatform()..scanned = invite;
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pumpAndSettle();
    expect(find.text('Join as Maria'), findsOneWidget);
  });

  testWidgets('closing the scanner leaves things as they were', (tester) async {
    final platform = FakePlatform();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pumpAndSettle();
    expect(platform.scans, 1);
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Got an invite? Scan it'), findsOneWidget);
  });

  for (final (problem, why) in [
    (ScanProblem.noPermission, 'Scanning needs the camera. Allow Share to use it, or paste the invite link.'),
    (ScanProblem.noCamera, 'This phone has no camera to scan with. Paste the invite link instead.'),
    (ScanProblem.failed, "The camera didn't open. Paste the invite link instead."),
  ]) {
    testWidgets('a scanner that cannot scan asks for the link: ${problem.name}', (tester) async {
      final platform = FakePlatform()..scanProblem = problem;
      await startApp(tester, platform, FakeServer());
      await tester.tap(find.text('Got an invite? Scan it'));
      await tester.pumpAndSettle();
      expect(find.text('Paste invite link'), findsOneWidget);
      expect(find.text(why), findsOneWidget);
      expect(find.text('Open settings'), problem == ScanProblem.noPermission ? findsOneWidget : findsNothing);
      await tester.enterText(find.byType(TextField), invite);
      await tester.tap(find.widgetWithText(TextButton, 'Save'));
      await tester.pumpAndSettle();
      expect(find.text('Join as Maria'), findsOneWidget);
    });
  }

  testWidgets('without the camera, its permission is a tap away in the settings', (tester) async {
    final platform = FakePlatform()..scanProblem = ScanProblem.noPermission;
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Open settings'));
    await tester.pumpAndSettle();
    expect(platform.settingsOpened, 1);
    expect(find.byType(AlertDialog), findsNothing);
  });

  testWidgets('a second tap while the scanner opens opens no second one', (tester) async {
    final closed = Completer<void>();
    final platform = FakePlatform()
      ..scanned = invite
      ..scanWait = closed.future;
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pump();
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pump();
    expect(platform.scans, 1);

    closed.complete();
    await tester.pumpAndSettle();
    expect(find.text('Join as Maria'), findsOneWidget);
  });
}
