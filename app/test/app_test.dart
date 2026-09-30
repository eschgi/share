import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/app.dart';
import 'package:share_app/data/api.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';

import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

String today() => DateTime.now().toIso8601String().substring(0, 10);
String daysAgo(int n) => DateTime.now().subtract(Duration(days: n)).toIso8601String().substring(0, 10);

const inviteToken = 'shi_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG';

Future<AppServices> startApp(WidgetTester tester, FakePlatform platform, FakeServer server) async {
  tester.view.physicalSize = const Size(780, 1688);
  tester.view.devicePixelRatio = 2;
  addTearDown(tester.view.reset);
  final services = AppServices(
    platform: platform,
    api: Api(platform: platform, publicClient: server.client, localClient: (_) => server.client),
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
