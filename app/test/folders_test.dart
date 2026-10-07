import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/folders.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/widgets.dart';

import 'admin_test.dart' show adminServer, openSettings, tapInList;
import 'app_test.dart' show daysAgo, signedInPhone, startApp, today;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

const family = 'f4mily5x2k7mbqz4bwdbyj6qsq';
const wedding = 'w3dding5x2k7mbqz4bwdbyj6qs';
const kindergarten = 'k1nd3rg4rt3nmbqz4bwdbyj6qs';
const taxes = 't4xes5x2k7mbqz4bwdbyj6qsqx';
const maria = 'u7ld5x2k7mbqz4bwdbyj6qsqxa';
const omaRosa = 'i1ld5x2k7mbqz4bwdbyj6qsqxa';

/// Stefan's server, as in the people fixture: Maria sees Family and the wedding, the invite for
/// Oma Rosa gives Family, and only admins see the taxes.
FakeServer adminFolders() => adminServer()
  ..folders = [
    FakeServer.folder(family, 'Family'),
    FakeServer.folder(wedding, 'Wedding Anna & Marco', people: 2),
    {...FakeServer.folder(taxes, 'Taxes 2026', people: 1), 'admins_only': true},
  ]
  ..addDay(today(), 6)
  ..addDay(daysAgo(1), 4, startId: 100, folder: wedding);

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

  testWidgets('admins find every folder in the settings, with who sees it', (tester) async {
    await startApp(tester, signedInPhone(), adminFolders());
    await openSettings(tester);
    expect(find.text('3 folders'), findsOneWidget);
    await tapInList(tester, find.text('Folders'));

    expect(find.text('Everyone sees only the folders they were given; admins see all of them.'), findsOneWidget);
    expect(find.text('6 files · 2 PINs'), findsOneWidget, reason: 'both PINs of the fixture send into Family');
    expect(find.text('4 files'), findsOneWidget);
    expect(find.text('Empty · only admins'), findsOneWidget);
    final stacks = tester.widgetList<AvatarStack>(find.byType(AvatarStack)).map((s) => s.people.map((p) => p.name).toList()).toList();
    expect(stacks, [
      ['Stefan', 'Maria', 'Oma Rosa'],
      ['Stefan', 'Maria'],
      ['Stefan'],
    ]);
  });

  testWidgets('a folder: who sees it, a PIN for it, renaming and deleting it', (tester) async {
    final server = adminFolders();
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tapInList(tester, find.text('Folders'));
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pumpAndSettle();
    expect(find.text('4 files · 12 MB'), findsOneWidget);
    expect(find.text('Admin · sees all folders'), findsOneWidget);

    Switch switchOf(String name) => tester.widget<Switch>(find.descendant(of: find.widgetWithText(SettingsRow, name), matching: find.byType(Switch)));
    expect([switchOf('Stefan').value, switchOf('Stefan').onChanged], [true, null], reason: 'admins see every folder');
    expect(switchOf('Maria').value, isTrue);
    expect(switchOf('Oma Rosa').value, isFalse);

    await tester.tap(find.descendant(of: find.widgetWithText(SettingsRow, 'Oma Rosa'), matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    expect(server.requests.last.method, 'GET', reason: 'reloaded after the change');
    expect(server.requests.any((r) => r.method == 'PUT' && r.url.path == '/api/folders/$wedding/invites/$omaRosa'), isTrue);
    expect(switchOf('Oma Rosa').value, isTrue);
    await tester.tap(find.descendant(of: find.widgetWithText(SettingsRow, 'Maria'), matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    expect(server.requests.any((r) => r.method == 'DELETE' && r.url.path == '/api/folders/$wedding/people/$maria'), isTrue);
    expect(switchOf('Maria').value, isFalse);

    await tester.tap(find.text('New PIN for this folder'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Create & share'));
    await tester.pumpAndSettle();
    final made = server.requests.lastWhere((r) => r.method == 'POST' && r.url.path == '/api/pins');
    expect(jsonDecode(made.body), containsPair('folder', wedding));
    expect(platform.sharedTexts.single, endsWith('#R8D4W'));
    expect(find.text('R8D4W'), findsOneWidget, reason: 'now among the PINs that send into it');

    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Rename'));
    await tester.pumpAndSettle();
    expect(find.text("Its folder on the server's drive gets the new name too."), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'family');
    await tester.tap(find.widgetWithText(TextButton, 'Rename'));
    await tester.pumpAndSettle();
    expect(find.text('Another folder has that name.'), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'Hochzeit Anna & Marco');
    await tester.tap(find.widgetWithText(TextButton, 'Rename'));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(AppBar, 'Hochzeit Anna & Marco'), findsOneWidget);

    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete the folder'));
    await tester.pumpAndSettle();
    expect(find.text('Delete “Hochzeit Anna & Marco”?'), findsOneWidget);
    await tester.tap(find.widgetWithText(TextButton, 'Delete the folder'));
    await tester.pumpAndSettle();
    expect(find.text('“Hochzeit Anna & Marco” is deleted.'), findsOneWidget);
    expect(find.text('Hochzeit Anna & Marco'), findsNothing, reason: 'back among the folders, without it');
    expect(server.trash, hasLength(4));
    expect(server.pins.where((p) => p['folder'] == wedding), isEmpty, reason: 'its PINs ended');
  });

  testWidgets('in a bucket, renaming a folder renames nothing on a drive', (tester) async {
    final server = adminServer()
      ..storage = {
        ...contractResponse('api/storage.json'),
        'storage': 's3',
        's3_bucket': 'family-photos',
        's3_endpoint': 'https://s3.eu-central-003.backblazeb2.com',
        'storage_dir': '',
      };
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tapInList(tester, find.text('Folders'));
    await tester.tap(find.text('Family'));
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Rename'));
    await tester.pumpAndSettle();
    expect(find.text('Rename the folder'), findsOneWidget);
    expect(find.text("Its folder on the server's drive gets the new name too."), findsNothing);
  });

  testWidgets('a new folder opens at once; the last one can\'t be deleted', (tester) async {
    final server = adminServer();
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await openSettings(tester);
    expect(find.text('1 folder'), findsOneWidget);
    await tapInList(tester, find.text('Folders'));
    await tester.tap(find.text('Family'));
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    expect(find.text('Rename'), findsOneWidget);
    expect(find.text('Delete the folder'), findsNothing);
    await tester.tapAt(const Offset(200, 600)); // away from the menu
    await tester.pumpAndSettle();
    await tester.tap(find.byType(ShareBackButton));
    await tester.pumpAndSettle();

    await tester.tap(find.text('New folder'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), '2026-09-27');
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Create'));
    await tester.pumpAndSettle();
    expect(find.text("A folder needs a name, and it can't be a date like 2026-09-27."), findsOneWidget);
    // Once there is a recovery key, a phone that doesn't hold it can't sign a new folder.
    platform.lacksRoot = true;
    await tester.enterText(find.byType(TextField), 'Kindergarten');
    await tester.tap(find.widgetWithText(TextButton, 'Create'));
    await tester.pumpAndSettle();
    expect(find.textContaining('it needs the recovery key'), findsOneWidget);
    platform.lacksRoot = false;
    await tester.tap(find.widgetWithText(TextButton, 'Create'));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(AppBar, 'Kindergarten'), findsOneWidget);
    expect(find.textContaining('Empty · '), findsNothing, reason: 'the header says how much it holds');
    expect(server.folders.map((f) => f['name']), ['Family', 'Kindergarten']);
  });

  testWidgets("a person's folders, switched from the person", (tester) async {
    final server = adminFolders();
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.tap(find.text('Maria'));
    await tester.pumpAndSettle();
    expect(find.text('Maria sees only the folders switched on. Admins see every folder.'), findsOneWidget);
    expect(find.text('Empty · only admins'), findsOneWidget);
    Switch switchOf(String name) => tester.widget<Switch>(find.descendant(of: find.widgetWithText(SettingsRow, name), matching: find.byType(Switch)));
    expect([for (final f in ['Family', 'Wedding Anna & Marco', 'Taxes 2026']) switchOf(f).value], [true, true, false]);

    await tester.tap(find.descendant(of: find.widgetWithText(SettingsRow, 'Taxes 2026'), matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    expect(server.requests.any((r) => r.method == 'PUT' && r.url.path == '/api/folders/$taxes/people/$maria'), isTrue);
    expect(switchOf('Taxes 2026').value, isTrue);
    await tester.tap(find.descendant(of: find.widgetWithText(SettingsRow, 'Family'), matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    expect(server.requests.any((r) => r.method == 'DELETE' && r.url.path == '/api/folders/$family/people/$maria'), isTrue);
    expect(switchOf('Family').value, isFalse);
    await tester.binding.handlePopRoute(); // the phone's back
    await tester.pumpAndSettle();

    // An admin sees every folder, whatever the switches said.
    await tester.tap(find.text('Stefan').last);
    await tester.pumpAndSettle();
    expect(find.text('Admins see every folder.'), findsOneWidget);
    expect(switchOf('Taxes 2026').value, isTrue);
    expect(switchOf('Taxes 2026').onChanged, isNull);
  });

  testWidgets('with one folder a member has, there is nothing to switch', (tester) async {
    await startApp(tester, signedInPhone(), adminServer());
    await openSettings(tester);
    await tester.tap(find.text('Maria'));
    await tester.pumpAndSettle();
    expect(find.text('Folders'.toUpperCase()), findsNothing);
    expect(find.byType(Switch), findsNothing);
  });

  testWidgets('an invite gives the folders ticked; the one the library shows to begin with', (tester) async {
    final server = adminFolders();
    await startApp(tester, signedInPhone()..secrets['folder'] = wedding, server);
    await openSettings(tester);
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Oma Rosa');
    await tester.pump();
    expect(find.text('Admins see every folder.'), findsOneWidget);
    Checkbox boxOf(String name) => tester.widget<Checkbox>(find.descendant(of: find.widgetWithText(SettingsRow, name), matching: find.byType(Checkbox)));
    expect([for (final f in ['Family', 'Wedding Anna & Marco', 'Taxes 2026']) boxOf(f).value], [false, true, false]);
    BusyButton show() => tester.widget<BusyButton>(find.widgetWithText(BusyButton, 'Show the QR code'));
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pump();
    expect(show().onPressed, isNull, reason: 'a member gets at least one folder');
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.tap(find.text('Family'));
    await tester.pump();
    expect(show().onPressed, isNotNull);
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    final made = server.requests.lastWhere((r) => r.url.path == '/api/invites');
    // The root this phone trusts goes along, locked with the link's secret.
    expect(jsonDecode(made.body), {'name': 'Oma Rosa', 'role': 'member', 'folders': [wedding, family], 'root': 'locked-root'});
    expect(boxOf('Family').onChanged, isNull, reason: 'made already');

    // An admin sees every folder: nothing to tick.
    await tester.tap(find.text('Invite someone else'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Anna');
    await tester.tap(find.text('Admin'));
    await tester.pump();
    expect(find.byType(Checkbox), findsNothing);
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    expect(jsonDecode(server.requests.lastWhere((r) => r.url.path == '/api/invites').body), {'name': 'Anna', 'role': 'admin', 'root': 'locked-root'});
  });

  testWidgets('with one folder an invite gives it without asking', (tester) async {
    final server = adminServer();
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Oma Rosa');
    await tester.pump();
    expect(find.byType(Checkbox), findsNothing);
    expect(find.text('Admins see every folder.'), findsNothing);
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    expect(jsonDecode(server.requests.lastWhere((r) => r.url.path == '/api/invites').body)['folders'], [family]);
  });

  testWidgets('a new PIN sends into the folder chosen, and may show it to guests', (tester) async {
    final server = adminFolders();
    final platform = signedInPhone()..secrets['folder'] = wedding;
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tester.tap(find.text('Upload PINs'));
    await tester.pumpAndSettle();
    expect(find.text('Family'), findsNWidgets(2), reason: "both PINs' cards say their folder");
    expect(find.text('Guests see the folder'), findsNothing);

    await tester.tap(find.text('New PIN'));
    await tester.pumpAndSettle();
    expect(find.text('Sends into'), findsOneWidget);
    expect(find.text('Wedding Anna & Marco'), findsOneWidget, reason: 'the folder the library shows');
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Taxes 2026'));
    await tester.pumpAndSettle();
    await tester.tap(find.descendant(of: find.byType(BottomSheet), matching: find.byType(Switch)));
    await tester.pump();
    await tester.tap(find.text('Create & share'));
    await tester.pumpAndSettle();
    final made = jsonDecode(server.requests.lastWhere((r) => r.method == 'POST' && r.url.path == '/api/pins').body);
    expect(made, {'kind': 'day', 'code': 'R8D4W', 'folder': taxes, 'shows_folder': true});
    expect(find.text('Taxes 2026'), findsOneWidget, reason: "the new PIN's card");
    expect(find.text('Guests see the folder'), findsOneWidget);
    expect(find.textContaining('Only PINs made to show their folder let guests see it too.'), findsOneWidget);
  });

  testWidgets('with one folder a PIN card says no folder, and a new PIN goes into it', (tester) async {
    final server = adminServer();
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.tap(find.text('Upload PINs'));
    await tester.pumpAndSettle();
    expect(find.text('Family'), findsNothing);
    await tester.tap(find.text('New PIN'));
    await tester.pumpAndSettle();
    expect(find.text('Sends into'), findsNothing);
    expect(find.text('Guests also see this folder'), findsOneWidget);
    await tester.tap(find.text('Create & share'));
    await tester.pumpAndSettle();
    expect(jsonDecode(server.requests.lastWhere((r) => r.method == 'POST' && r.url.path == '/api/pins').body), {'kind': 'day', 'code': 'R8D4W', 'folder': family});
  });

  testWidgets('sending with a PIN says which folder, and whether everyone sees it', (tester) async {
    final server = FakeServer()
      ..pinFolderName = 'Wedding Anna & Marco'
      ..pinShowsFolder = true;
    final platform = FakePlatform();
    await startApp(tester, platform, server);
    await tester.tap(find.text('Send files'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'share.example.com');
    await tester.tap(find.text('Next'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'K7M2Q');
    await tester.tap(find.text('Unlock'));
    await tester.pumpAndSettle();
    expect(find.text('Wedding Anna & Marco'), findsOneWidget);
    expect(find.text('Everyone with this PIN sees what you send.'), findsOneWidget);
    expect(jsonDecode(platform.secrets['pin_session']!), containsPair('folder_name', 'Wedding Anna & Marco'));

    // At the next start the server's word counts: the folder has a new name, and shows no more.
    server
      ..pinFolderName = 'Hochzeit'
      ..pinShowsFolder = false;
    await tester.pumpWidget(const SizedBox());
    await startApp(tester, platform, server);
    expect(find.text('Hochzeit'), findsOneWidget);
    expect(find.text("Others with this PIN can't see what you send."), findsOneWidget);
  });

  testWidgets('a PIN of a server with one folder names no folder', (tester) async {
    final platform = FakePlatform()
      ..secrets['pin_token'] = 'shp_x'
      ..secrets['pin_session'] = jsonEncode({'server': 'https://share.example.com', 'pin_kind': 'day', 'expires_at': null});
    await startApp(tester, platform, FakeServer());
    expect(find.text('To share.example.com'), findsOneWidget);
    expect(find.byIcon(AppIcons.folder), findsNothing);
    expect(find.text("Others with this PIN can't see what you send."), findsOneWidget);
  });

  testWidgets('admins move files to another folder, and can take it back', (tester) async {
    final server = adminFolders();
    await startApp(tester, signedInPhone()..secrets['folder'] = family, server);
    expect(find.byType(LibraryTile), findsNWidgets(6));
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(LibraryTile).first); // five of six
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Move to another folder'));
    await tester.pumpAndSettle();

    expect(find.text('Who sees them changes with the folder.'), findsOneWidget);
    expect(find.text('Here now · 4 people'), findsOneWidget);
    await tester.tap(find.text('Taxes 2026'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Move 5 files'));
    await tester.pumpAndSettle();
    final moved = jsonDecode(server.requests.lastWhere((r) => r.url.path == '/api/files/move').body) as Map;
    expect(moved['folder'], taxes);
    expect(moved['ids'], hasLength(5));
    expect(find.text('5 files are in “Taxes 2026” now.'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNWidgets(1), reason: 'they left the folder shown');
    expect(server.files.where((f) => f['folder'] == taxes), hasLength(5));

    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(server.files.where((f) => f['folder'] == family), hasLength(6));
    expect(find.byType(LibraryTile), findsNWidgets(6));
  });

  testWidgets('with all folders shown, moved files stay in view; from one folder they can go back', (tester) async {
    final server = adminFolders();
    await startApp(tester, signedInPhone(), server);
    await tester.longPress(find.byType(LibraryTile).first); // one of Family's
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Move to another folder'));
    await tester.pumpAndSettle();
    expect(find.text('Here now · 4 people'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, 'Move 1 file'));
    await tester.pumpAndSettle();
    expect(find.text('1 file is in “Wedding Anna & Marco” now.'), findsOneWidget, reason: 'the first other folder, at first');
    expect(find.byType(LibraryTile), findsNWidgets(10));
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(server.files.where((f) => f['folder'] == family), hasLength(6));

    // Files from two folders have no "here", and no way back.
    await tester.longPress(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(LibraryTile).last);
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Move to another folder'));
    await tester.pumpAndSettle();
    expect(find.textContaining('Here now'), findsNothing);
    await tester.tap(find.text('Taxes 2026'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Move 2 files'));
    await tester.pumpAndSettle();
    expect(find.text('2 files are in “Taxes 2026” now.'), findsOneWidget);
    expect(find.text('Undo'), findsNothing);
  });

  testWidgets('members, and admins with one folder, have no Move', (tester) async {
    await startApp(tester, signedInPhone(), threeFolders()..addDay(today(), 1, startId: 300));
    await tester.longPress(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.bySemanticsLabel('Move to another folder'), findsNothing);
    await tester.pumpWidget(const SizedBox());

    await startApp(tester, signedInPhone(), adminServer()..addDay(today(), 2));
    await tester.longPress(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.bySemanticsLabel('Move to another folder'), findsNothing);
    expect(find.bySemanticsLabel('Delete'), findsOneWidget);
  });
}
