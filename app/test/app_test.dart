import 'dart:convert';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:share_app/app.dart';
import 'package:share_app/data/api.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/player.dart';

import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_player.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

String today() => clock.now().toIso8601String().substring(0, 10);
String daysAgo(int n) => clock.now().subtract(Duration(days: n)).toIso8601String().substring(0, 10);

const inviteToken = 'shi_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG';

/// The app over [server]; with [public], the public address goes there instead.
Future<AppServices> startApp(WidgetTester tester, FakePlatform platform, FakeServer server, {MediaPlayerFactory? player, http.Client? public}) async {
  tester.view.physicalSize = const Size(780, 1688);
  tester.view.devicePixelRatio = 2;
  addTearDown(tester.view.reset);
  final services = AppServices(
    platform: platform,
    api: Api(platform: platform, publicClient: public ?? server.client, localClient: (_) => server.client),
    player: player ?? FakeMediaPlayer.new,
  );
  await tester.pumpWidget(ShareApp(services: services));
  await tester.pumpAndSettle();
  return services;
}

/// A phone that is signed in already.
FakePlatform signedInPhone() => FakePlatform()
  ..secrets['device_token'] = 'shd_x'
  ..server = ServerConfig(publicUrl: Uri.parse('https://share.example.com'), serverId: 'srv');

void main() {
  setUpAll(loadFonts);

  testWidgets('signed out: the first screen, signing in, then the library', (tester) async {
    final server = FakeServer()..routes['POST /api/auth/login'] = (_) => FakeServer().json(contractResponse('api/login.json'));
    final platform = FakePlatform();
    await startApp(tester, platform, server);
    expect(find.text('What would you like to do?'), findsOneWidget);

    await tester.tap(find.text('See & download'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).at(0), 'share.example.com');
    await tester.enterText(find.byType(TextField).at(1), 'maria');
    await tester.enterText(find.byType(TextField).at(2), 'correct horse');
    await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
    await tester.pumpAndSettle();

    expect(find.text('Library'), findsWidgets);
    expect(platform.secrets['device_token'], startsWith('shd_'));
    final login = server.requests.firstWhere((r) => r.url.path == '/api/auth/login');
    expect(login.url.origin, 'https://share.example.com');
    expect(login.body, contains('"device_name":"Pixel 8"'));
  });

  testWidgets('a wrong password is shown with the tries left', (tester) async {
    final server = FakeServer()
      ..routes['POST /api/auth/login'] =
          (_) => FakeServer().json({'error': {'code': 'login_wrong', 'message': '', 'attempts_left': 3}}, 401);
    await startApp(tester, FakePlatform(), server);
    await tester.tap(find.text('See & download'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).at(0), 'share.example.com');
    await tester.enterText(find.byType(TextField).at(1), 'maria');
    await tester.enterText(find.byType(TextField).at(2), 'nope');
    await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
    await tester.pumpAndSettle();
    expect(find.text('Wrong username or password. 3 tries left.'), findsOneWidget);
  });

  testWidgets('an invite link opens the invite, and joining signs in', (tester) async {
    final platform = FakePlatform();
    await startApp(tester, platform, FakeServer());
    platform.linkEvents.add('com.eschgi.share://join?server=https%3A%2F%2Fshare.example.com&token=$inviteToken');
    await tester.pumpAndSettle();
    expect(find.text('Stefan invited you'), findsOneWidget);
    expect(find.text('Member: see, download, send'), findsOneWidget);

    await tester.tap(find.text('Join as Maria'));
    await tester.pumpAndSettle();
    expect(find.text('Library'), findsWidgets);
    expect(platform.server!.localUrl.toString(), 'https://192.168.8.1:8443');
  });

  testWidgets('the library groups by day, and a day is selected and saved at once', (tester) async {
    final server = FakeServer()
      ..addDay(today(), 6)
      ..addDay(daysAgo(1), 5, startId: 100);
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    expect(find.text('Today'), findsOneWidget);
    expect(find.text('Yesterday'), findsOneWidget);
    expect(find.textContaining('6 files · '), findsOneWidget);

    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    expect(find.text('6 selected'), findsOneWidget);
    // A whole day keeps its usual line (mockup 12); only part of one says how much.
    expect(find.textContaining('6 files · '), findsOneWidget);
    await tester.tap(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.text('5 of 6 selected'), findsOneWidget);
    await tester.tap(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.text('6 selected'), findsOneWidget);

    await tester.tap(find.text('Download 6'));
    await tester.pumpAndSettle();
    expect(platform.downloads.single.map((f) => f.day).toSet(), {today()});
    expect(find.text('Saving 6 files'), findsOneWidget);
    expect(find.text('5 photos & videos'), findsOneWidget);
    expect(find.text('1 document'), findsOneWidget);
  });

  testWidgets('at home, with the public address not set up yet, the library comes back by itself', (tester) async {
    // A check found nothing at home, e.g. while the server restarted; the public address
    // doesn't lead anywhere.
    final server = FakeServer()..addDay(today(), 3);
    final platform = signedInPhone()
      ..secrets['user'] = jsonEncode(contractResponse('api/me.json')['user'])
      ..server = ServerConfig(
        publicUrl: Uri.parse('https://share.example.com'),
        localUrl: Uri.parse('http://192.168.8.248:8080'),
        serverId: 'srv',
        deviceId: 'dv1',
      )
      ..current = const RouteStatus(ServerRoute.public, reason: RouteReason.unreachable);
    final nowhere = MockClient((_) async => throw http.ClientException('Failed host lookup: share.example.com'));
    await startApp(tester, platform, server, public: nowhere);
    expect(find.text('No connection to the server. Check your internet and try again.'), findsOneWidget);

    // The server is back: at the next look the library is there, without "Try again".
    platform.afterCheck = const RouteStatus(ServerRoute.local);
    await tester.pump(const Duration(seconds: 30));
    await tester.pumpAndSettle();
    expect(find.textContaining('3 files · '), findsOneWidget);
  });

  testWidgets('sharing the selection, and a file no app opens', (tester) async {
    final server = FakeServer()..addDay(today(), 6);
    final platform = signedInPhone();
    await startApp(tester, platform, server);

    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.share));
    await tester.pumpAndSettle();
    expect(platform.shared.single, hasLength(6));
    await tester.tap(find.byIcon(AppIcons.x));
    await tester.pumpAndSettle();

    // The day's fourth file is a PDF; the viewer offers to open it in another app.
    platform.openError = const OpenFailed(noApp: true);
    await tester.tap(find.byType(LibraryTile).at(3));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Car_insurance.pdf'));
    await tester.pumpAndSettle();
    expect(find.text('No app on this phone can open this file.'), findsOneWidget);
  });

  testWidgets('the viewer says how long a video is', (tester) async {
    final server = FakeServer()..addDay(today(), 6);
    await startApp(tester, signedInPhone(), server);
    await tester.tap(find.byType(LibraryTile).at(2)); // the day's third file is a video of 18 seconds
    await tester.pumpAndSettle();
    expect(find.textContaining(RegExp(r'^VID_2\.mp4 · .+ · 0:18$')), findsOneWidget);
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Details').last);
    await tester.pumpAndSettle();
    expect(find.textContaining(RegExp(r' · 0:18 · video/mp4$')), findsOneWidget);
  });

  testWidgets('signing out goes back to the first screen', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Sign out'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Sign out'));
    await tester.pumpAndSettle();
    expect(find.text('What would you like to do?'), findsOneWidget);
    expect(platform.secrets['device_token'], isNull);
  });
}
