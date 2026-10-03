import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/folders.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';

import 'app_test.dart' show daysAgo, signedInPhone, startApp, today;
import 'support/fake_server.dart';
import 'support/fonts.dart';

const family = 'f4mily5x2k7mbqz4bwdbyj6qsq';
const wedding = 'w3dd1ng5x2k7mbqz4bwdbyj6qs';
const kindergarten = 'k1nd3rg4rt3nmbqz4bwdbyj6qs';

/// Maria's server: three folders she was given, with files in each.
FakeServer threeFolders() => FakeServer()
  ..folders = [FakeServer.folder(family, 'Family'), FakeServer.folder(wedding, 'Wedding Anna & Marco'), FakeServer.folder(kindergarten, 'Kindergarten')]
  ..addDay(today(), 6)
  ..addDay(daysAgo(1), 4, startId: 100, folder: wedding)
  ..addDay(daysAgo(2), 2, startId: 200, folder: kindergarten);

/// The folders the library asked for, one per request.
Iterable<String?> asked(FakeServer server) =>
    server.requests.where((r) => r.url.path == '/api/library' || r.url.path == '/api/files').map((r) => r.url.queryParameters['folder']);

void main() {
  setUpAll(loadFonts);

  testWidgets('with one folder the library looks as it always did', (tester) async {
    final server = FakeServer()..addDay(today(), 6);
    await startApp(tester, signedInPhone(), server);
    expect(find.text('Library'), findsWidgets);
    expect(find.byType(FolderTitle), findsNothing);
    expect(asked(server), everyElement(isNull));
  });

  testWidgets('with more folders the title switches between them, and the choice stays', (tester) async {
    final server = threeFolders();
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    expect(find.widgetWithText(FolderTitle, 'All folders'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(12));

    await tester.tap(find.byType(FolderTitle));
    await tester.pumpAndSettle();
    Finder inSheet(Finder f) => find.descendant(of: find.byType(BottomSheet), matching: f);
    expect(find.text('Folders'), findsOneWidget);
    expect(inSheet(find.textContaining('12 files · ')), findsOneWidget);
    expect(inSheet(find.textContaining('4 files · ')), findsOneWidget);
    expect(inSheet(find.byIcon(AppIcons.check)), findsOneWidget); // on All folders
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pumpAndSettle();

    expect(find.text('Folders'), findsNothing);
    expect(find.widgetWithText(FolderTitle, 'Wedding Anna & Marco'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(4));
    expect(find.text('Yesterday'), findsOneWidget);
    expect(find.text('Today'), findsNothing);
    expect(asked(server).last, wedding);
    expect(platform.secrets['folder'], wedding);

    // The kind chips keep the folder.
    await tester.tap(find.text('Photos'));
    await tester.pumpAndSettle();
    expect(server.requests.last.url.queryParameters, containsPair('folder', wedding));
  });

  testWidgets('the folder shown last time is shown again at once', (tester) async {
    final server = threeFolders();
    await startApp(tester, signedInPhone()..secrets['folder'] = kindergarten, server);
    expect(find.widgetWithText(FolderTitle, 'Kindergarten'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(2));
    expect(asked(server), everyElement(kindergarten)); // the first page waited for the folders
  });

  testWidgets('a folder stored but no longer seen shows all folders', (tester) async {
    final server = threeFolders();
    await startApp(tester, signedInPhone()..secrets['folder'] = 'g0n35x2k7mbqz4bwdbyj6qsqxa', server);
    expect(find.widgetWithText(FolderTitle, 'All folders'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(12));
    expect(asked(server), everyElement(isNull));
  });

  testWidgets('a folder taken away while shown goes back to all folders', (tester) async {
    final server = threeFolders();
    final platform = signedInPhone()..secrets['folder'] = wedding;
    await startApp(tester, platform, server);
    expect(find.byType(LibraryTile), findsNWidgets(4));

    server
      ..folders.removeWhere((f) => f['id'] == wedding)
      ..files.removeWhere((f) => f['folder'] == wedding)
      ..version += 1;
    await tester.pump(const Duration(seconds: 30));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(FolderTitle, 'All folders'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(8));
    expect(platform.secrets['folder'], isNull);
  });

  testWidgets('files moved out of the folder shown leave it', (tester) async {
    final server = threeFolders();
    await startApp(tester, signedInPhone()..secrets['folder'] = family, server);
    expect(find.byType(LibraryTile), findsNWidgets(6));

    for (final f in server.files.take(2)) {
      f['folder'] = kindergarten;
    }
    server.version += 1;
    await tester.pump(const Duration(seconds: 30));
    await tester.pumpAndSettle();
    expect(find.byType(LibraryTile), findsNWidgets(4));
    expect(find.textContaining('4 files · '), findsOneWidget);
  });

  testWidgets('the details say which folder a file is in, where there are several', (tester) async {
    await startApp(tester, signedInPhone(), threeFolders());
    await tester.tap(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Details').last);
    await tester.pumpAndSettle();
    expect(find.text('In “Family”'), findsOneWidget);
  });

  testWidgets('signing out forgets the folder shown', (tester) async {
    final platform = signedInPhone()..secrets['folder'] = wedding;
    await startApp(tester, platform, threeFolders());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Sign out'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(TextButton, 'Sign out'));
    await tester.pumpAndSettle();
    expect(platform.secrets['folder'], isNull);
  });
}
