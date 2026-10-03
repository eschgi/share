import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/ui/folders.dart';

import 'app_test.dart' show signedInPhone, startApp, today;
import 'folders_test.dart' show family, threeFolders, wedding;
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
    expect(platform.sharedFolders, [family], reason: 'the one folder');
    final nav = tester.widget<NavigationBar>(find.byType(NavigationBar).first);
    expect(nav.selectedIndex, 1, reason: 'the Send tab');
  });

  testWidgets('signed in with a second folder, they wait on the Send tab for the folder', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, threeFolders());
    platform.share(3);
    await tester.pumpAndSettle();
    expect(platform.sharedSent, isEmpty);
    expect(tester.widget<NavigationBar>(find.byType(NavigationBar).first).selectedIndex, 1);
    expect(find.text('3 files wait to be sent.'), findsOneWidget);
    expect(find.text('Family'), findsOneWidget, reason: 'all folders are shown in the library, so the oldest');

    // Switching folders in the library sends nothing, nor goes back to the Send tab.
    await tester.tap(find.text('Library').last);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(FolderTitle));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Kindergarten'));
    await tester.pumpAndSettle();
    expect(platform.sharedSent, isEmpty);
    expect(tester.widget<NavigationBar>(find.byType(NavigationBar).first).selectedIndex, 0);

    await tester.tap(find.text('Send').last);
    await tester.pumpAndSettle();
    expect(find.text('Kindergarten'), findsOneWidget, reason: 'now the folder open in the library');
    await tester.tap(find.text('Kindergarten'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Send 3 files'));
    await tester.pumpAndSettle();
    expect(platform.sharedSent, [SendAuth.device]);
    expect(platform.sharedFolders, [wedding]);
    expect(find.text('3 files wait to be sent.'), findsNothing);
    expect(find.text('Into the folder'), findsOneWidget, reason: 'the field for picking stays');
  });

  testWidgets('signed in with a second folder, they can still be dropped', (tester) async {
    final platform = signedInPhone()..sharedWaiting = 2;
    await startApp(tester, platform, threeFolders());
    expect(find.text('2 files wait to be sent.'), findsOneWidget);
    await tester.tap(find.text("Don't send"));
    await tester.pumpAndSettle();
    expect(platform.sharedDropped, 1);
    expect(platform.sharedSent, isEmpty);
    expect(find.text('2 files wait to be sent.'), findsNothing);
  });

  testWidgets('files shared before the app started go out once it has', (tester) async {
    final platform = signedInPhone()..sharedWaiting = 2;
    await startApp(tester, platform, FakeServer());
    expect(platform.sharedSent, [SendAuth.device]);
    expect(platform.sharedFolders, [family], reason: 'once the folders were fetched');
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
    expect(platform.sharedFolders, [null], reason: 'a PIN sends into its own');
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
