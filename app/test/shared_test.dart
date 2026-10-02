import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';

import 'app_test.dart' show signedInPhone, startApp, today;
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// "Send with Share" in another app's share sheet.
void main() {
  setUpAll(loadFonts);

  testWidgets('signed in, shared files go out at once, on the Send tab', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer()..addDay(today(), 2));
    expect(find.text('Library'), findsWidgets);
    platform.share(3);
    await tester.pumpAndSettle();
    expect(platform.sharedSent, [SendAuth.device]);
    final nav = tester.widget<NavigationBar>(find.byType(NavigationBar).first);
    expect(nav.selectedIndex, 1, reason: 'the Send tab');
  });

  testWidgets('files shared before the app started go out once it has', (tester) async {
    final platform = signedInPhone()..sharedWaiting = 2;
    await startApp(tester, platform, FakeServer());
    expect(platform.sharedSent, [SendAuth.device]);
    expect(platform.sharedWaiting, 0);
  });

  testWidgets('with a PIN they go with the PIN', (tester) async {
    final platform = FakePlatform()
      ..secrets['pin_token'] = 'shp_x'
      ..secrets['pin_session'] = jsonEncode({'server': 'https://share.example.com', 'pin_kind': 'day', 'expires_at': null});
    await startApp(tester, platform, FakeServer());
    platform.share(1);
    await tester.pumpAndSettle();
    expect(platform.sharedSent, [SendAuth.pin]);
  });

  testWidgets('without either they wait on the first screen, or go', (tester) async {
    final platform = FakePlatform()..sharedWaiting = 2;
    await startApp(tester, platform, FakeServer());
    expect(find.text('2 shared files are waiting. Sign in or enter a PIN to send them.'), findsOneWidget);
    expect(platform.sharedSent, isEmpty);
    await tester.tap(find.text("Don't send"));
    await tester.pumpAndSettle();
    expect(platform.sharedDropped, 1);
    expect(find.textContaining('shared files are waiting'), findsNothing);
  });

  testWidgets('files that couldn\'t be taken are mentioned', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    platform.share(1, skipped: 2);
    await tester.pumpAndSettle();
    expect(find.text("2 shared files couldn't be taken. There may be no room for them on this phone."), findsOneWidget);
  });
}
