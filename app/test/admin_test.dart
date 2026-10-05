import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/admin/invite_person_screen.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/settings_screen.dart';
import 'package:share_app/ui/widgets.dart';

import 'app_test.dart' show signedInPhone, startApp, today;
import 'support/contract.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

FakeServer adminServer() => FakeServer()..me = FakeServer.adminMe;

Future<void> openSettings(WidgetTester tester) async {
  await tester.tap(find.text('Settings').last);
  await tester.pumpAndSettle();
}

final settingsList = find.descendant(of: find.byType(SettingsScreen), matching: find.byType(Scrollable)).first;

/// Scrolls the settings until [finder] is on screen, then taps it.
Future<void> tapInList(WidgetTester tester, Finder finder) async {
  // Built is not yet visible: the row may still be behind the navigation bar.
  await tester.scrollUntilVisible(finder, 200, scrollable: settingsList);
  await tester.ensureVisible(finder);
  await tester.pumpAndSettle();
  await tester.tap(finder);
  await tester.pumpAndSettle();
}

void main() {
  setUpAll(loadFonts);

  testWidgets('admins get PINs, people and storage in the settings; members don\'t', (tester) async {
    await startApp(tester, signedInPhone(), adminServer());
    await openSettings(tester);
    expect(find.text('Upload PINs'), findsOneWidget);
    expect(find.text('1 permanent · 1 for 24 hours'), findsOneWidget);
    expect(find.text('Maria'), findsOneWidget);
    expect(find.textContaining('1 phone, 1 browser'), findsOneWidget, reason: "Maria's phone and browser");
    expect(find.text('Oma Rosa'), findsOneWidget);
    expect(find.text('Invited'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('/mnt/usb/share'), 200, scrollable: settingsList);
    expect(find.text('Recently deleted'), findsOneWidget);
    expect(find.text('The drive is exFAT or NTFS: it ignores capitals in names and is slower than ext4.'), findsOneWidget,
        reason: "the storage fixture's warning");

    await tester.pumpWidget(const SizedBox());
    await startApp(tester, signedInPhone(), FakeServer());
    await openSettings(tester);
    expect(find.text('Upload PINs'), findsNothing);
    expect(find.text('People'.toUpperCase()), findsNothing);
  });

  testWidgets('with a bucket, the settings name it instead of a drive', (tester) async {
    final server = adminServer()
      ..storage = {
        ...contractResponse('api/storage.json'),
        'storage': 's3',
        's3_bucket': 'family-photos',
        's3_endpoint': 'https://s3.eu-central-003.backblazeb2.com',
        'storage_dir': '',
        'fs_type': '',
        'total_bytes': 0,
        'free_bytes': 0,
        'warnings': [
          {'code': 's3_cors', 'level': 'problem', 'message': "the bucket's CORS rules don't let Share's pages in"},
        ],
      };
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.scrollUntilVisible(find.text('family-photos'), 200, scrollable: settingsList);
    expect(find.text('s3.eu-central-003.backblazeb2.com\nSet in config.json on the server\n4210 files · 311 GB'), findsOneWidget);
    expect(find.text('/mnt/usb/share'), findsNothing);
    expect(find.textContaining('free of'), findsNothing);
    expect(find.text("The bucket doesn't let Share's pages send and fetch files. Set its CORS rules; share check prints them."), findsOneWidget);
  });

  testWidgets('a new PIN, with a code that was taken first', (tester) async {
    final server = adminServer()..usedCodes.add('R8D4W');
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tester.tap(find.text('Upload PINs'));
    await tester.pumpAndSettle();
    expect(find.bySemanticsLabel('K 7 M 2 Q'), findsOneWidget);
    expect(find.bySemanticsLabel('4 H X 9 T'), findsOneWidget);

    await tester.tap(find.text('New PIN'));
    await tester.pumpAndSettle();
    expect(find.bySemanticsLabel('R 8 D 4 W'), findsOneWidget, reason: 'the suggestion');
    await tester.tap(find.text('Create & share'));
    await tester.pumpAndSettle();
    expect(find.text('That PIN was used before. Pick another one.'), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'm9c-q3');
    await tester.pump();
    expect(find.bySemanticsLabel('M 9 C Q 3'), findsOneWidget, reason: 'typed, in capitals, without the dash');
    await tester.tap(find.text('Create & share'));
    await tester.pumpAndSettle();
    expect(platform.sharedTexts.single, 'Send me photos and files with Share: https://share.example.com/#M9CQ3');
    expect(find.bySemanticsLabel('M 9 C Q 3'), findsOneWidget, reason: 'in the list now');
  });

  testWidgets('ending a PIN asks first', (tester) async {
    final server = adminServer();
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.tap(find.text('Upload PINs'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('End now'));
    await tester.pumpAndSettle();
    expect(find.text('End 4HX9T now?'), findsOneWidget);
    await tester.tap(find.text('End now').last);
    await tester.pumpAndSettle();
    expect(find.bySemanticsLabel('4 H X 9 T'), findsNothing);
    expect(server.pins, hasLength(1));
  });

  testWidgets('inviting someone: a QR code, or the link', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, adminServer());
    await openSettings(tester);
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Peter');
    await tester.tap(find.descendant(of: find.byType(InvitePersonScreen), matching: find.text('Admin')));
    await tester.pump();
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    expect(find.byType(QrCodeView), findsOneWidget);
    expect(find.text('Let Peter scan this with the phone camera'), findsOneWidget);
    await tester.tap(find.text('Send as a link instead'));
    expect(platform.sharedTexts.single, startsWith('Peter, this invite signs you in to Share.'));
    expect(platform.sharedTexts.single, contains('https://share.example.com/join#shi_'));
  });

  testWidgets('a person: signing a phone out, the role, removing them', (tester) async {
    final server = adminServer();
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.tap(find.text('Maria'));
    await tester.pumpAndSettle();
    expect(find.text('Pixel 8'), findsOneWidget);
    expect(find.text('Phones and browsers'.toUpperCase()), findsOneWidget);
    expect(find.textContaining('only at home'), findsOneWidget, reason: 'a browser signed in at home');
    expect(find.text('Add a phone or browser'), findsOneWidget);
    await tester.tap(find.text('Sign out').last);
    await tester.pumpAndSettle();
    expect(find.text('Sign out Chrome · Windows?'), findsOneWidget);
    await tester.tap(find.text('Sign out').last);
    await tester.pumpAndSettle();
    expect(find.text('Chrome · Windows'), findsNothing);

    await tester.tap(find.text('Make admin'));
    await tester.pumpAndSettle();
    expect(find.text('Make member'), findsOneWidget);

    await tester.tap(find.text('Remove Maria'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Remove Maria').last);
    await tester.pumpAndSettle();
    expect(find.text('Maria'), findsNothing);
  });

  testWidgets('everyone signs out their own phones and browsers from the profile', (tester) async {
    final server = FakeServer(); // a member
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tester.tap(find.text('Signed in on this phone'));
    await tester.pumpAndSettle();
    expect(find.text('Your phones and browsers'), findsOneWidget);
    expect(find.text('This phone'), findsOneWidget);
    expect(find.textContaining('only at home'), findsOneWidget);
    await tester.tap(find.text('Sign out').last);
    await tester.pumpAndSettle();
    expect(find.text('Sign out Chrome · Windows?'), findsOneWidget);
    await tester.tap(find.text('Sign out').last);
    await tester.pumpAndSettle();
    expect(find.text('Chrome · Windows'), findsNothing);
    expect(server.myDevices.map((d) => d['name']), ['Pixel 8']);
  });

  testWidgets('a new password for someone: the username first, then shown once', (tester) async {
    final server = adminServer();
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tester.tap(find.text('Maria'));
    await tester.pumpAndSettle();
    expect(find.text('For signing in on other phones and in browsers'), findsOneWidget, reason: 'Maria has no password yet');
    await tester.tap(find.text('Set a new password'));
    await tester.pumpAndSettle();
    expect(find.widgetWithText(TextField, 'maria'), findsOneWidget, reason: 'suggested from the name');

    await tester.enterText(find.byType(TextField), 'stefan');
    await tester.tap(find.text('Make a password'));
    await tester.pumpAndSettle();
    expect(find.text('Someone else has that username.'), findsOneWidget);
    await tester.enterText(find.byType(TextField), 'maria.rossi');
    await tester.tap(find.text('Make a password'));
    await tester.pumpAndSettle();
    final password = contractResponse('api/user_password.json')['password'] as String;
    expect(find.text('maria.rossi'), findsOneWidget);
    expect(find.text(password), findsOneWidget);
    await tester.tap(find.byTooltip('Password'));
    await tester.pumpAndSettle();
    expect(platform.copiedSecrets, [password], reason: 'copied as a secret, hidden in the clipboard preview');
    await tester.tap(find.text('Send in a message'));
    expect(platform.sharedTexts.single, 'Your sign-in for share.example.com: username maria.rossi, password $password');
    await tester.tap(find.text('Close'));
    await tester.pumpAndSettle();
    expect(find.text('@maria.rossi'), findsOneWidget);
    expect(find.text('If Maria forgot it'), findsOneWidget);

    // Not for oneself.
    await tester.tapAt(const Offset(10, 10)); // the sheet closes
    await tester.pumpAndSettle();
    await tester.tap(find.text('Stefan').first);
    await tester.pumpAndSettle();
    expect(find.text('Set a new password'), findsNothing);
  });

  testWidgets('the only admin stays one', (tester) async {
    await startApp(tester, signedInPhone(), adminServer());
    await openSettings(tester);
    await tester.tap(find.text('Stefan').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Make member'));
    await tester.pumpAndSettle();
    expect(find.text('This is the only admin. Make someone else admin first.'), findsOneWidget);
  });

  testWidgets('deleting from the library, and taking it back', (tester) async {
    final server = adminServer()..addDay(today(), 6);
    await startApp(tester, signedInPhone(), server);
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Delete'));
    await tester.pumpAndSettle();
    expect(find.text('Delete 6 files?'), findsOneWidget);
    await tester.tap(find.text('Delete 6 files'));
    await tester.pumpAndSettle();
    expect(server.files, isEmpty);
    expect(find.text('6 files deleted'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNothing);

    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(server.files, hasLength(6));
    expect(find.byType(LibraryTile), findsNWidgets(6));
  });

  testWidgets('deleting in the viewer goes on to the next file, and Undo brings it back', (tester) async {
    final server = adminServer()..addDay(today(), 3);
    await startApp(tester, signedInPhone(), server);
    await tester.tap(find.byType(LibraryTile).at(1));
    await tester.pumpAndSettle();
    expect(find.textContaining(RegExp(r'^IMG_1\.jpg · ')), findsOneWidget);
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    expect(find.text('Delete 1 file?'), findsOneWidget);
    await tester.tap(find.text('Delete 1 file'));
    await tester.pumpAndSettle();
    expect(server.files.map((f) => f['name']), ['IMG_0.jpg', 'VID_2.mp4']);
    expect(find.text('1 file deleted'), findsOneWidget);
    expect(find.textContaining(RegExp(r'^VID_2\.mp4 · ')), findsOneWidget, reason: 'the next file shows in its place');
    await tester.pump(const Duration(seconds: 7));
    await tester.pumpAndSettle();
    expect(find.text('1 file deleted'), findsNothing, reason: 'it goes by itself, uncovering the buttons');

    // The last one: the one before it shows.
    await tester.tap(find.text('Delete'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete 1 file'));
    await tester.pumpAndSettle();
    expect(find.textContaining(RegExp(r'^IMG_0\.jpg · ')), findsOneWidget);

    await tester.tap(find.byType(ShareBackButton));
    await tester.pumpAndSettle();
    expect(find.byType(LibraryTile), findsOneWidget, reason: 'the library dropped both');
    await tester.tap(find.text('Undo'));
    await tester.pumpAndSettle();
    expect(server.files.map((f) => f['name']), contains('VID_2.mp4'));
    expect(find.byType(LibraryTile), findsNWidgets(2));
  });

  testWidgets('members can\'t delete', (tester) async {
    await startApp(tester, signedInPhone(), FakeServer()..addDay(today(), 3));
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    expect(find.byIcon(AppIcons.trash), findsNothing);
    await tester.tap(find.byIcon(AppIcons.x));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.text('Delete'), findsNothing, reason: 'nor in the viewer');
  });

  testWidgets('recently deleted: bringing back, and deleting for good', (tester) async {
    final server = adminServer();
    final example = (contractResponse('api/trash.json')['files'] as List).first as Map;
    server.trash = [
      {...example.cast<String, dynamic>(), 'id': 'aaaaaaaaaaaaaaaaaaaaaaaaaa', 'name': 'Back.jpg'},
      {...example.cast<String, dynamic>(), 'id': 'bbbbbbbbbbbbbbbbbbbbbbbbbb', 'name': 'Gone.jpg'},
    ];
    await startApp(tester, signedInPhone(), server);
    await openSettings(tester);
    await tapInList(tester, find.text('Recently deleted'));
    expect(find.text('Back.jpg'), findsOneWidget);

    await tester.tap(find.text('Back.jpg'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Restore'));
    await tester.pumpAndSettle();
    expect(find.text('1 file is back'), findsOneWidget);
    expect(server.files.single['name'], 'Back.jpg');

    await tester.tap(find.text('Gone.jpg'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete now'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Delete now').last);
    await tester.pumpAndSettle();
    expect(server.trash, isEmpty);
    expect(find.text('Nothing was deleted.'), findsOneWidget);
  });
}
