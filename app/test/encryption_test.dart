import 'dart:convert';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/widgets.dart';

import 'admin_test.dart' show openSettings, settingsList, tapInList;
import 'app_test.dart' show signedInPhone, startApp;
import 'folders_test.dart' show adminFolders, family, taxes, wedding;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// End-to-end encryption in the app (docs/e2ee-plan.md): the keys live in Kotlin, which the
/// fake platform stands in for; these are the screens and what they hand Kotlin and the server.
void main() {
  setUpAll(loadFonts);

  final secret = 'A' * 42 + 'w';

  test('links carry the secret of the keys they bring', () {
    final invite = parseLink('https://share.example.com/join#shi_${'x' * 40}.$secret') as InviteLink;
    expect(invite.token, 'shi_${'x' * 40}');
    expect(invite.secret, secret);
    final fromApp = parseLink('com.eschgi.share://join?server=https%3A%2F%2Fshare.example.com&token=shi_${'x' * 40}&key=$secret') as InviteLink;
    expect(fromApp.secret, secret);
    expect((parseLink('https://share.example.com/join#shi_${'x' * 40}') as InviteLink).secret, isNull);
    final pin = parseLink('https://share.example.com/#k7m2q.$secret') as PinLink;
    expect([pin.code, pin.secret], ['K7M2Q', secret]);
    expect((parseLink('https://share.example.com/#K7M2Q.short') as PinLink).secret, isNull, reason: 'not a secret: the PIN works without');
    expect(parseLink('https://share.example.com/#K7M2.$secret'), isNull);
  });

  test('encrypted files, folders, PINs and invites as the API says them', () {
    final f = FileInfo.fromJson(contractResponse('api/file_encrypted.json'));
    expect(f.enc, isNotNull);
    expect(f.enc!.version, 1);
    // The API says the plain size as the file's size; Kotlin decrypts with it (contract/app/platform.json).
    expect(f.toJson()['enc'], {...f.enc!.toJson(), 'plain_size': 3145728});
    expect(f.toJson()['folder'], f.folder);
    final plain = FileInfo.fromJson({'id': 'x', 'size': 1});
    expect(plain.enc, isNull);

    final folder = FolderInfo.fromJson(((contractResponse('api/folder_encryption.json'))));
    expect(folder.encrypted, isTrue);
    expect(folder.keyVersion, 1);

    final pin = PinInfo.fromJson({'id': 'p', 'code': 'R8D4W', 'link': 'https://s/#R8D4W', 'secret': {'sealed': 'x', 'version': 2}});
    expect(pin.secret, (sealed: 'x', version: 2));
    expect(pin.withLink('https://s/#R8D4W.$secret').secret, isNull, reason: 'the whole link needs no opening');

    final joined = SignedIn.fromJson(contractResponse('api/invite_accept.json'));
    expect(joined.keys, hasLength(1));
    expect(joined.keys.single['folder'], 'f4mily5x2k7mbqz4bwdbyj6qsq');
  });

  testWidgets('signing in with the password opens the keys with it', (tester) async {
    final server = FakeServer()..routes['POST /api/auth/login'] = (_) => FakeServer().json(contractResponse('api/login.json'));
    final platform = FakePlatform();
    await startApp(tester, platform, server);
    await tester.tap(find.text('See & download'));
    await tester.pumpAndSettle();
    final fields = find.byType(TextField);
    await tester.enterText(fields.at(0), 'share.example.com');
    await tester.enterText(fields.at(1), 'maria');
    await tester.enterText(fields.at(2), 'correct horse');
    await tester.tap(find.widgetWithText(FilledButton, 'Sign in'));
    await tester.pumpAndSettle();
    expect(platform.keySyncs, contains('correct horse'));
  });

  testWidgets('a phone checks in with the keys every half minute while in front, and on coming back', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, adminFolders());
    tester.binding.handleAppLifecycleStateChanged(AppLifecycleState.resumed);
    final first = platform.keyCheckIns;
    await tester.pump(const Duration(seconds: 30));
    expect(platform.keyCheckIns, first + 1);
    // Into the background and back, the way Android goes.
    for (final s in [AppLifecycleState.inactive, AppLifecycleState.hidden, AppLifecycleState.paused]) {
      tester.binding.handleAppLifecycleStateChanged(s);
    }
    await tester.pump(const Duration(minutes: 2));
    expect(platform.keyCheckIns, first + 1, reason: 'none in the background');
    for (final s in [AppLifecycleState.hidden, AppLifecycleState.inactive, AppLifecycleState.resumed]) {
      tester.binding.handleAppLifecycleStateChanged(s);
    }
    await tester.pump();
    expect(platform.keyCheckIns, first + 2, reason: 'one at once on coming back');
  });

  testWidgets('joining with an invite link opens the keys its secret brings', (tester) async {
    final platform = FakePlatform();
    await startApp(tester, platform, FakeServer());
    platform.linkEvents.add('https://share.example.com/join#shi_${'x' * 40}.$secret');
    await tester.pumpAndSettle();
    await tester.tap(find.text('Join as Maria'));
    await tester.pumpAndSettle();
    expect(platform.invitesOpened, hasLength(1));
    expect(platform.invitesOpened.single.$1, secret);
    expect(platform.invitesOpened.single.$2.single['folder'], 'f4mily5x2k7mbqz4bwdbyj6qsq');
  });

  testWidgets('the first encrypted folder makes the recovery code, shown once, then the folder key', (tester) async {
    final server = adminFolders();
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tapInList(tester, find.text('Folders'));
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pumpAndSettle();
    expect(find.text('Encrypt new files'), findsOneWidget);
    expect(find.text('New files are stored as they are'), findsOneWidget);

    await tester.tap(find.descendant(of: find.widgetWithText(SettingsRow, 'Encrypt new files'), matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    expect(find.text('Encrypt new files in Wedding Anna & Marco?'), findsOneWidget);
    expect(find.textContaining('First comes the recovery code'), findsOneWidget);
    await tester.tap(find.text('Encrypt'));
    await tester.pumpAndSettle();

    expect(find.text('Recovery code'), findsOneWidget);
    expect(find.text('7SEN-4M38-3QV2-Z38M\nJEGM-QXC8-FHPD-HJ4M'), findsOneWidget);
    final done = find.widgetWithText(TextButton, 'Done');
    expect(tester.widget<TextButton>(done).onPressed, isNull, reason: 'not before it is kept');
    await tester.tap(find.text('I wrote it down'));
    await tester.pumpAndSettle();
    await tester.tap(done);
    await tester.pumpAndSettle();
    expect(platform.keyCalls, ['recovery', 'encrypt $wedding']);
    expect(find.text('New files in Wedding Anna & Marco are now encrypted'), findsOneWidget);
  });

  testWidgets('a new folder can be encrypted from the start, as the admins set', (tester) async {
    final server = adminFolders()..newFoldersEncrypted = true;
    final platform = signedInPhone()..keysState = const KeysState(status: KeysStatus.ready, hasRecovery: true);
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tester.scrollUntilVisible(find.text('Encrypt new folders'), 200, scrollable: settingsList);
    expect(find.text('Made. A new one replaces it'), findsOneWidget);
    await tester.scrollUntilVisible(find.text('Folders'), -200, scrollable: settingsList);
    await tapInList(tester, find.text('Folders'));
    await tester.tap(find.text('New folder'));
    await tester.pumpAndSettle();
    expect(tester.widget<CheckboxListTile>(find.byType(CheckboxListTile)).value, isTrue);
    await tester.enterText(find.byType(TextField), 'Holidays');
    await tester.pump();
    await tester.tap(find.widgetWithText(TextButton, 'Create'));
    await tester.pumpAndSettle();
    expect(find.text('Encrypt new files in Holidays?'), findsOneWidget, reason: 'asked at once');
    expect(find.textContaining('First comes the recovery code'), findsNothing, reason: 'there is one');
    await tester.tap(find.text('Encrypt'));
    await tester.pumpAndSettle();
    expect(platform.keyCalls.single, startsWith('encrypt '));
  });

  testWidgets('a phone without its keys shows locks and waits; the recovery code opens them', (tester) async {
    final server = adminFolders()..thumb = Uint8List.fromList([1, 2, 3]);
    for (final f in server.files.where((f) => f['folder'] == family)) {
      f['has_thumb'] = true;
      f['enc'] = {'version': 1, 'key': 'sealed-key', 'header': 'U0hFMQABAAA9p6lLxQCBAA'};
    }
    final platform = signedInPhone()
      ..secrets['folder'] = family
      ..sealedFolders = {family}
      ..keysState = const KeysState(status: KeysStatus.waiting, hasRecovery: true, encryptedFolders: 1);
    await startApp(tester, platform, server);
    expect(find.text('Waiting for another phone or browser'), findsOneWidget);
    expect(find.descendant(of: find.byType(LibraryTile).first, matching: find.byIcon(AppIcons.lock)), findsOneWidget);

    await tester.tap(find.text('Use the recovery code'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).last, '0000-0000-0000-0000-0000-0000-0000-0000');
    await tester.pump();
    final use = find.descendant(of: find.byType(AlertDialog), matching: find.widgetWithText(TextButton, 'Use the recovery code'));
    await tester.tap(use);
    await tester.pumpAndSettle();
    expect(find.text("That isn't the recovery code."), findsOneWidget);
    await tester.enterText(find.byType(TextField).last, platform.recoveryCode.toLowerCase());
    await tester.pump();
    await tester.tap(use);
    await tester.pumpAndSettle();
    expect(find.text('2 encrypted folders opened'), findsOneWidget);

    // The keys came: the banner goes, and the thumbnails open.
    platform.sealedFolders = {};
    platform.setKeys(const KeysState(status: KeysStatus.ready, hasRecovery: true, encryptedFolders: 1, open: {'$family:1'}));
    await tester.pumpAndSettle();
    expect(find.text('Waiting for another phone or browser'), findsNothing);
    expect(find.descendant(of: find.byType(LibraryTile).first, matching: find.byIcon(AppIcons.lock)), findsNothing);
    expect(find.descendant(of: find.byType(LibraryTile).first, matching: find.byType(Image)), findsOneWidget);
  });

  testWidgets('a PIN that shows an encrypted folder brings its keys, and its link the secret', (tester) async {
    final server = adminFolders();
    final platform = signedInPhone()..encryptedFolders = {wedding};
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tapInList(tester, find.text('Folders'));
    await tester.tap(find.text('Wedding Anna & Marco'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('New PIN for this folder'));
    await tester.pumpAndSettle();
    await tester.tap(find.descendant(of: find.byType(BottomSheet), matching: find.byType(Switch)));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Create & share'));
    await tester.pumpAndSettle();
    final made = jsonDecode(server.requests.lastWhere((r) => r.method == 'POST' && r.url.path == '/api/pins').body) as Map;
    expect(made['secret'], {'sealed': 'sealed-secret', 'version': 1, 'keys': []});
    expect(platform.sharedTexts.single, endsWith('#R8D4W.${platform.linkSecret}'));
  });

  testWidgets('an invite into an encrypted folder brings its keys, locked with its link\'s secret', (tester) async {
    final server = adminFolders();
    server.folders = [for (final f in server.folders) f['id'] == family ? {...f, 'encrypted': true, 'key_version': 1} : f];
    final platform = signedInPhone()
      ..secrets['folder'] = family
      ..encryptedFolders = {family};
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).first, 'Oma Rosa');
    await tester.pumpAndSettle();
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    final made = jsonDecode(server.requests.lastWhere((r) => r.method == 'POST' && r.url.path == '/api/invites').body) as Map;
    expect(made['keys'], [
      {'folder': family, 'version': 1, 'locked': 'locked-$family'},
    ]);
    expect(find.byWidgetPredicate((w) => w is QrCodeView && w.data.endsWith('.${platform.linkSecret}')), findsOneWidget);
  });

  testWidgets('encrypted files move with their keys, sealed for where they go', (tester) async {
    final server = adminFolders();
    final first = server.files.firstWhere((f) => f['folder'] == family);
    first['enc'] = {'version': 1, 'key': 'sealed-key', 'header': 'U0hFMQABAAA9p6lLxQCBAA'};
    final platform = signedInPhone()..secrets['folder'] = family;
    await startApp(tester, platform, server);
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Move to another folder'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Taxes 2026'));
    await tester.pumpAndSettle();
    await tester.tap(find.widgetWithText(FilledButton, 'Move 6 files'));
    await tester.pumpAndSettle();
    final moved = jsonDecode(server.requests.lastWhere((r) => r.url.path == '/api/files/move').body) as Map;
    expect(moved['keys'], [
      {'id': first['id'], 'version': 1, 'key': 'moved-${first['id']}'},
    ]);
    expect(platform.keyCalls, ['move ${first['id']} $taxes']);
  });

  testWidgets('a new password keeps the keys locked with it', (tester) async {
    final server = FakeServer()..routes['PUT /api/me/password'] = (_) => FakeServer().json({}, 204);
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await openSettings(tester);
    await tapInList(tester, find.text('Password'));
    // Maria has no password yet: her username, then the new one.
    final fields = find.byType(TextField);
    await tester.enterText(fields.at(0), 'maria');
    await tester.enterText(fields.at(1), 'staple battery');
    await tester.tap(find.widgetWithText(TextButton, 'Save'));
    await tester.pumpAndSettle();
    final put = jsonDecode(server.requests.lastWhere((r) => r.method == 'PUT' && r.url.path == '/api/me/password').body) as Map;
    expect(put['password_lock'], 'lock-staple battery');
  });
}
