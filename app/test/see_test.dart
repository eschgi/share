import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';

import 'app_test.dart' show startApp, today;
import 'folders_test.dart' show wedding;
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// A phone that sends with a PIN; [shows]: the PIN also shows its folder (screen 46).
FakePlatform pinPhone({bool shows = true}) => FakePlatform()
  ..secrets['pin_token'] = 'shp_x'
  ..secrets['pin_session'] = jsonEncode({
    'server': 'https://share.example.com',
    'pin_kind': 'day',
    'expires_at': null,
    'folder_name': 'Wedding Anna & Marco',
    'shows_folder': shows,
  });

/// The wedding's server, where the PIN sends into the wedding's folder and shows it.
FakeServer weddingServer({bool shows = true}) => FakeServer()
  ..pinFolderName = 'Wedding Anna & Marco'
  ..pinShowsFolder = shows
  ..folders = [
    {...FakeServer.folder(wedding, 'Wedding Anna & Marco'), 'senders': 23},
  ]
  ..addDay(today(), 6, folder: wedding);

void main() {
  setUpAll(loadFonts);

  testWidgets("a PIN that shows its folder: See, with the PIN's key over the public address", (tester) async {
    final server = weddingServer();
    final platform = pinPhone();
    await startApp(tester, platform, server);
    expect(find.text('Send'), findsWidgets);
    await tester.tap(find.text('See'));
    await tester.pumpAndSettle();

    expect(find.widgetWithText(AppBar, 'Wedding Anna & Marco'), findsOneWidget);
    expect(find.text('6 files from 23 people'), findsOneWidget);
    expect(find.textContaining('Download all · '), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(6));
    final bar = tester.widget<NavigationBar>(find.byType(NavigationBar));
    expect([bar.selectedIndex, bar.destinations.length], [1, 2], reason: 'See, beside Send, in the bar at the bottom');
    final asked = server.requests.where((r) => r.url.path == '/api/library' || r.url.path == '/api/files' || r.url.path == '/api/folders');
    expect(asked, isNotEmpty);
    for (final r in asked) {
      expect(r.headers['Authorization'], 'Bearer shp_x');
      expect(r.url.origin, 'https://share.example.com');
    }
    expect(find.byIcon(AppIcons.circle), findsNothing, reason: 'nothing to select');

    // The viewer: no Delete, and nobody's name.
    await tester.tap(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.text('Delete'), findsNothing);
    await tester.tap(find.byIcon(AppIcons.share));
    await tester.pumpAndSettle();
    expect(platform.auths.last, SendAuth.pin);
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Details').last);
    await tester.pumpAndSettle();
    expect(find.text('Sent with a PIN'), findsNothing);
    expect(find.textContaining('From '), findsNothing);
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();
    await tester.binding.handlePopRoute();
    await tester.pumpAndSettle();

    await tester.tap(find.textContaining('Download all · '));
    await tester.pumpAndSettle();
    expect(platform.downloads.single, hasLength(6));
    expect(platform.auths.last, SendAuth.pin);
  });

  testWidgets('a PIN that only sends has no See, and no bar at the bottom', (tester) async {
    await startApp(tester, pinPhone(shows: false), weddingServer(shows: false));
    expect(find.text('Wedding Anna & Marco'), findsOneWidget, reason: 'where the files go');
    expect(find.text('See'), findsNothing);
    expect(find.byType(NavigationBar), findsNothing);
    expect(find.text("Others with this PIN can't see what you send."), findsOneWidget);
  });

  testWidgets('a PIN that ends while seeing goes back to sending, with a new PIN to enter', (tester) async {
    final server = weddingServer();
    await startApp(tester, pinPhone(), server);
    await tester.tap(find.text('See'));
    await tester.pumpAndSettle();
    expect(find.byType(LibraryTile), findsNWidgets(6));

    server.routes['GET /api/library'] = (_) => server.json({'error': {'code': 'session_ended', 'message': ''}}, 401);
    await tester.drag(find.byType(CustomScrollView), const Offset(0, 300)); // pull to refresh
    await tester.pumpAndSettle();
    expect(find.text('The PIN you used has ended. Enter a new PIN to finish sending.'), findsOneWidget);
    expect(find.text('See'), findsNothing);
  });
}
