import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';

import 'app_test.dart' show startApp;
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// Scanning an invite's QR code (issue #5): what the scanner gives opens, and when the scanner
/// doesn't open, the link is asked for, saying why, instead of nothing happening.
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

  testWidgets('backing out of the scanner leaves things as they were', (tester) async {
    final platform = FakePlatform();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pumpAndSettle();
    expect(platform.scans, 1);
    expect(find.byType(AlertDialog), findsNothing);
    expect(find.text('Got an invite? Scan it'), findsOneWidget);
  });

  for (final (problem, why) in [
    (ScanProblem.noPlayServices, 'Scanning needs Google Play services. Paste the invite link instead.'),
    (ScanProblem.installing, 'The scanner is still being installed on this phone. Try again in a minute, or paste the invite link.'),
    (ScanProblem.outdated, 'Scanning needs a newer version of Google Play services. Paste the invite link instead.'),
    (ScanProblem.failed, "The scanner didn't open. Paste the invite link instead."),
  ]) {
    testWidgets('a scanner that does not open asks for the link: ${problem.name}', (tester) async {
      final platform = FakePlatform()..scanProblem = problem;
      await startApp(tester, platform, FakeServer());
      await tester.tap(find.text('Got an invite? Scan it'));
      await tester.pumpAndSettle();
      expect(find.text('Paste invite link'), findsOneWidget);
      expect(find.text(why), findsOneWidget);
      await tester.enterText(find.byType(TextField), invite);
      await tester.tap(find.widgetWithText(TextButton, 'Save'));
      await tester.pumpAndSettle();
      expect(find.text('Join as Maria'), findsOneWidget);
    });
  }

  testWidgets('a slow first scan says the scanner is getting ready, and a second tap waits for it', (tester) async {
    final ready = Completer<void>();
    final platform = FakePlatform()
      ..scanned = invite
      ..scanWait = ready.future;
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pump(const Duration(milliseconds: 500));
    expect(find.text('Getting the scanner ready…'), findsNothing, reason: 'not for a scanner that opens at once');
    await tester.pump(const Duration(seconds: 1));
    expect(find.text('Getting the scanner ready…'), findsOneWidget);
    await tester.tap(find.text('Got an invite? Scan it'));
    await tester.pump();
    expect(platform.scans, 1);

    ready.complete();
    await tester.pumpAndSettle();
    expect(find.text('Getting the scanner ready…'), findsNothing);
    expect(find.text('Join as Maria'), findsOneWidget);
  });
}
