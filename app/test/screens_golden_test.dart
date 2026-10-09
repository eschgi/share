@Tags(['golden'])
library;

import 'dart:convert';
import 'dart:typed_data';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/zip.dart';
import 'package:share_app/l10n/app_localizations.dart';
import 'package:share_app/ui/admin/print_sheet.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/settings_screen.dart';

import 'app_test.dart' show daysAgo, inviteToken, signedInPhone, startApp, today;
import 'folders_test.dart' show family, kindergarten, taxes, wedding;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';
import 'zip_test.dart' show dolomitesPart3, sundayFiles, sundayZip;

/// Screenshots of the app's screens, for comparing with `docs/share-mockup/<lang>/*.png`.
///   flutter test --run-skipped --tags golden --update-goldens

/// The screenshots show times, so they're taken at a fixed one: the mockups' morning.
Future<void> atTen(Future<void> Function() body) => withClock(Clock.fixed(DateTime(2026, 9, 30, 10, 4)), body);

void main() {
  setUpAll(loadFonts);

  Future<void> shot(WidgetTester tester, String name) async {
    await tester.pumpAndSettle();
    await expectLater(find.byType(MaterialApp), matchesGoldenFile('goldens/$name.png'));
  }

  FakeServer library() => FakeServer()
    ..addDay(today(), 6)
    ..addDay(daysAgo(1), 11, startId: 100)
    ..addDay(daysAgo(3), 8, startId: 200);

  for (final lang in ['en', 'de', 'it']) {
    testWidgets('first start, $lang', (tester) => atTen(() async {
      await startApp(tester, FakePlatform()..secrets['language'] = lang, FakeServer());
      await shot(tester, '$lang/07-first-start');
    }));

    testWidgets('invite, $lang', (tester) => atTen(() async {
      final platform = FakePlatform()..secrets['language'] = lang;
      await startApp(tester, platform, FakeServer());
      platform.linkEvents.add('https://share.example.com/join#$inviteToken');
      await shot(tester, '$lang/10-invite');
    }));

    testWidgets('library, $lang', (tester) => atTen(() async {
      await startApp(tester, signedInPhone()..secrets['language'] = lang, library());
      await shot(tester, '$lang/11-library');
    }));
  }

  // The mockups' folders: Maria's three (38, 39, 48), and the taxes only admins see (40, 41).
  List<Map<String, dynamic>> mockupFolders(FakeServer server, {bool admin = false}) {
    Map<String, dynamic> cover(int i) => {...server.files[i], 'has_thumb': true};
    return [
      FakeServer.folder(family, 'Family', files: 2340, bytes: 41000000000, cover: cover(0)),
      {...FakeServer.folder(wedding, 'Wedding Anna & Marco', files: 412, bytes: 9800000000, people: 2, cover: cover(7)), 'created_at': '2026-09-26T08:00:00Z'},
      FakeServer.folder(kindergarten, 'Kindergarten', files: 86, bytes: 340000000, people: 2),
      if (admin) {...FakeServer.folder(taxes, 'Taxes 2026', files: 37, bytes: 120000000, people: 1), 'admins_only': true},
    ];
  }

  // As Maria, with the library showing Family.
  FakeServer folders() {
    final server = library();
    return server..folders = mockupFolders(server);
  }

  testWidgets('a folder', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..secrets['folder'] = family, folders());
    await shot(tester, 'en/38-library-folder');
    await tester.tap(find.text('Family'));
    await shot(tester, 'en/39-choose-folder');
  }));

  // Keys passed on only with an OK (50 to 52): a phone that waits for its keys, with the code its
  // other phone or browser shows; and the sheets that ask before passing them on.
  testWidgets('keys: a phone waits for approval', (tester) => atTen(() async {
    final server = folders()..thumb = Uint8List.fromList([1, 2, 3]);
    for (final f in server.files.take(6)) {
      f['has_thumb'] = true;
      f['enc'] = {'version': 1, 'key': 'sealed-key', 'header': 'U0hFMQABAAA9p6lLxQCBAA'};
    }
    final platform = signedInPhone()
      ..secrets['folder'] = family
      ..sealedFolders = {for (final f in server.files) f['folder'] as String}
      ..keysState = const KeysState(status: KeysStatus.waiting, encryptedFolders: 1, codes: [ShownCode(kind: 'device', from: 'Chrome · Windows', code: '482197')]);
    await startApp(tester, platform, server);
    await shot(tester, 'en/50-keys-waiting');
  }));

  testWidgets('keys: allowing a new browser', (tester) => atTen(() async {
    final platform = signedInPhone()
      ..secrets['folder'] = family
      ..keysState = KeysState(status: KeysStatus.ready, encryptedFolders: 1, asks: [
        KeyAsk(kind: 'device', id: 'd1', name: 'Chrome · Windows', client: 'web', since: DateTime(2026, 9, 30, 10, 3), code: '735041'),
      ]);
    await startApp(tester, platform, folders());
    // Listed in the library; Show opens the sheet.
    await shot(tester, 'en/51-keys-waiting-list');
    await tester.tap(find.text('Show'));
    await shot(tester, 'en/51-keys-allow-browser');
  }));

  testWidgets('keys: several wait for an OK', (tester) => atTen(() async {
    final platform = signedInPhone()
      ..secrets['folder'] = family
      ..keysState = KeysState(status: KeysStatus.ready, encryptedFolders: 3, asks: [
        KeyAsk(kind: 'device', id: 'd1', name: 'Chrome · Windows', client: 'web', since: DateTime(2026, 9, 30, 10, 3)),
        const KeyAsk(kind: 'person', id: 'u1', name: 'Maria', folders: [family]),
        const KeyAsk(kind: 'person', id: 'u2', name: 'Peter', folders: [family, wedding]),
      ]);
    await startApp(tester, platform, folders());
    await shot(tester, 'en/51-keys-several');
  }));

  testWidgets('keys: allowing someone a new key', (tester) => atTen(() async {
    final platform = signedInPhone()
      ..secrets['folder'] = family
      ..keysState = const KeysState(status: KeysStatus.ready, encryptedFolders: 3, asks: [
        KeyAsk(kind: 'person', id: 'u1', name: 'Maria', folders: [family, wedding, kindergarten], code: '813552', keyChanged: true),
      ]);
    await startApp(tester, platform, folders());
    await tester.tap(find.text('Show'));
    await shot(tester, 'en/52-keys-allow-person');
  }));

  testWidgets('sign in', (tester) => atTen(() async {
    await startApp(tester, FakePlatform(), FakeServer());
    await tester.tap(find.text('See & download'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField).at(0), 'share.example.com');
    await tester.enterText(find.byType(TextField).at(1), 'maria');
    await tester.enterText(find.byType(TextField).at(2), 'correct horse');
    await shot(tester, 'en/08-sign-in');
  }));

  testWidgets('selecting', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), library());
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.longPress(find.byType(GestureDetector).at(12));
    await shot(tester, 'en/12-select');
  }));

  testWidgets('downloading', (tester) => atTen(() async {
    final platform = signedInPhone();
    await startApp(tester, platform, library());
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Download 6'));
    await tester.pumpAndSettle();
    platform.transferEvents.add(const TransferState(
      batch: 'batch-1', running: true, total: 6, done: 3, failed: 0, skipped: 1,
      bytesTotal: 18000000, bytesDone: 9100000, media: 5, documents: 1, local: true));
    await shot(tester, 'en/13-downloading');
  }));

  testWidgets('server', (tester) => atTen(() async {
    final platform = signedInPhone()..current = const RouteStatus(ServerRoute.local, millis: 12);
    platform.server = platform.server!.copyWith(localUrl: Uri.parse('https://192.168.8.1:8443'), pins: ['a' * 64]);
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Server'));
    await shot(tester, 'en/16-server');
  }));

  // The admin screens, with the mockups' people and PINs, at times that read the same every day.
  FakeServer admin() {
    final now = clock.now();
    final today = DateTime(now.year, now.month, now.day);
    String at(DateTime t) => t.toUtc().toIso8601String();
    final server = FakeServer()..me = FakeServer.adminMe;
    final users = [for (final u in server.people['users'] as List) (u as Map).cast<String, dynamic>()];
    for (final u in users) {
      for (final p in u['phones'] as List) {
        (p as Map)['last_seen_at'] = at(now.subtract(const Duration(minutes: 5)));
      }
      u['last_seen_at'] = at(now.subtract(const Duration(minutes: 5)));
    }
    users.first['phones'] = [(users.first['phones'] as List).first];
    users.add({
      'id': 'u9ld5x2k7mbqz4bwdbyj6qsqxa', 'name': 'Peter', 'role': 'member', 'username': null, 'has_password': false, 'me': false,
      'created_at': at(today), 'last_seen_at': at(now),
      'phones': [
        {'id': 'd9ld5x2k7mbqz4bwdbyj6qsqxa', 'name': 'Moto g', 'created_at': at(today), 'last_seen_at': at(now), 'this': false},
      ],
    });
    server.people = {
      'users': users,
      'invites': [
        {...((server.people['invites'] as List).first as Map).cast<String, dynamic>(), 'expires_at': at(today.add(const Duration(hours: 21)))},
      ],
    };
    server.pins[0]['created_at'] = at(DateTime(now.year, 9, 12, 10));
    server.pins[1]['expires_at'] = at(today.add(const Duration(hours: 23, minutes: 30)));
    return server;
  }

  // As Stefan: Maria was given Kindergarten too and Peter Family; the 24-hour PIN sends into
  // the wedding's folder.
  FakeServer adminFolders() {
    final server = admin()..addDay(today(), 6)..addDay(daysAgo(1), 11, startId: 100);
    server.folders = mockupFolders(server, admin: true);
    server.people['users'] = [
      for (final u in server.people['users'] as List)
        switch ((u as Map)['name']) {
          'Maria' => {...u, 'folders': [family, wedding, kindergarten]},
          'Peter' => {...u, 'folders': [family]},
          _ => u,
        },
    ];
    server.pins[1]['folder'] = wedding;
    return server;
  }

  testWidgets('folders', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), adminFolders());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Folders'));
    await shot(tester, 'en/40-folders');
    await tester.tap(find.text('Wedding Anna & Marco'));
    await shot(tester, 'en/41-folder');
  }));

  testWidgets('a new PIN, into a folder', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..secrets['folder'] = wedding, adminFolders());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Upload PINs'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('New PIN'));
    await shot(tester, 'en/43-new-pin-folder');
  }));

  testWidgets('moving files to another folder', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..secrets['folder'] = family, adminFolders());
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.byType(LibraryTile).first); // five of the day's six
    await tester.pumpAndSettle();
    await tester.tap(find.bySemanticsLabel('Move to another folder'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Taxes 2026'));
    await shot(tester, 'en/49-move');
  }));

  testWidgets("a person's folders", (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), adminFolders());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Maria'));
    await shot(tester, 'en/42-person-folders');
  }));

  testWidgets('invite someone, with folders', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), adminFolders());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Oma Rosa');
    FocusManager.instance.primaryFocus?.unfocus();
    await shot(tester, 'en/47-invite-folders');
  }));

  testWidgets('settings', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..current = const RouteStatus(ServerRoute.local, millis: 12), admin());
    await tester.tap(find.text('Settings').last);
    await shot(tester, 'en/17-settings');
  }));

  testWidgets('upload PINs', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), admin());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Upload PINs'));
    await shot(tester, 'en/18-pins');
    await tester.tap(find.text('New PIN'));
    await shot(tester, 'en/19-new-pin');
  }));

  testWidgets('print a PIN', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), admin());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Upload PINs'));
    await tester.pumpAndSettle();
    await tester.tap(find.byType(PopupMenuButton<VoidCallback>));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Print'));
    await tester.pumpAndSettle();
    await tester.enterText(find.widgetWithText(TextField, 'Family'), 'Anna & Marco');
    FocusManager.instance.primaryFocus?.unfocus();
    await shot(tester, 'en/69-print');
  }));

  // The page itself, as it is printed (71, 72).
  testWidgets('a printed page', (tester) => atTen(() async {
    tester.view.physicalSize = PrintPage.size;
    tester.view.devicePixelRatio = 1;
    addTearDown(tester.view.reset);
    final texts = lookupAppLocalizations(const Locale('en'));
    Widget page(PrintKind kind) => MaterialApp(
          debugShowCheckedModeBanner: false,
          home: PrintPage(Printed(
            kind: kind,
            title: 'Anna & Marco',
            line: texts.printLine,
            link: 'https://share.example.com/#ANNA5',
            brand: 'Share',
            texts: texts,
            locale: 'en',
            day: DateTime(2026, 10, 10),
            code: 'ANNA5',
            showsFolder: true,
          )),
        );
    await tester.pumpWidget(page(PrintKind.poster));
    await shot(tester, 'en/71-print-poster');
    await tester.pumpWidget(page(PrintKind.cards));
    await shot(tester, 'en/72-print-cards');
  }));

  testWidgets('invite someone', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), admin());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Invite'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'Oma Rosa');
    await tester.pump();
    await tester.tap(find.text('Show the QR code'));
    await tester.pumpAndSettle();
    FocusManager.instance.primaryFocus?.unfocus();
    await shot(tester, 'en/20-invite');
  }));

  testWidgets('deleting', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), library()..me = FakeServer.adminMe);
    await tester.longPress(find.byType(GestureDetector).at(8));
    await tester.pumpAndSettle();
    for (final i in [9, 10, 12, 13]) {
      await tester.tap(find.byType(GestureDetector).at(i));
      await tester.pump();
    }
    await tester.tap(find.bySemanticsLabel('Delete'));
    await shot(tester, 'en/21-delete');
  }));

  testWidgets('recently deleted', (tester) => atTen(() async {
    final server = admin();
    final now = clock.now().toUtc();
    final example = ((contractResponse('api/trash.json')['files'] as List).first as Map).cast<String, dynamic>();
    server.trash = [
      for (final (i, name) in ['IMG_2041.jpg', 'IMG_2042.jpg', 'Car_insurance.pdf'].indexed)
        {
          ...example,
          'id': 'abc'[i] * 26,
          'name': name,
          'kind': name.endsWith('.pdf') ? 'document' : 'photo',
          'mime': name.endsWith('.pdf') ? 'application/pdf' : 'image/jpeg',
          'has_thumb': false,
          'deleted_at': now.subtract(Duration(days: i * 4)).toIso8601String(),
          'purge_at': now.add(Duration(days: 30 - i * 4, hours: 1)).toIso8601String(),
        },
    ];
    await startApp(tester, signedInPhone(), server);
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    final list = find.descendant(of: find.byType(SettingsScreen), matching: find.byType(Scrollable)).first;
    await tester.scrollUntilVisible(find.text('Recently deleted'), 200, scrollable: list);
    await tester.ensureVisible(find.text('Recently deleted'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Recently deleted'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('IMG_2042.jpg'));
    await shot(tester, 'en/22-recently-deleted');
  }));

  testWidgets('sending', (tester) => atTen(() async {
    final platform = signedInPhone()..current = const RouteStatus(ServerRoute.local, millis: 12);
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Send').last);
    await tester.pumpAndSettle();
    platform.uploadEvents.add(const UploadState(
      batch: 'up-1', auth: SendAuth.device, running: true, total: 40, done: 11,
      bytesTotal: 180000000, bytesDone: 54000000, etaSeconds: 130, local: true,
      items: [
        UploadItemState(seq: 10, name: '20260930_094512.jpg', size: 3100000, kind: FileKind.photo, state: 'done', bytes: 3100000),
        UploadItemState(seq: 11, name: '20260930_094530.mp4', size: 52000000, kind: FileKind.video, state: 'queued', bytes: 33280000),
        UploadItemState(seq: 12, name: 'Kindergarten_form.pdf', size: 480000, kind: FileKind.document, state: 'queued'),
        UploadItemState(seq: 13, name: '20260930_094602.jpg', size: 2800000, kind: FileKind.photo, state: 'queued'),
      ],
    ));
    await shot(tester, 'en/15-send');
  }));

  testWidgets('sending into a folder', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..secrets['folder'] = family, folders());
    await tester.tap(find.text('Send').last);
    await shot(tester, 'en/48-send-folder');
  }));

  testWidgets('a PIN into a folder', (tester) => atTen(() async {
    await startApp(tester, FakePlatform(), FakeServer()..pinFolderName = 'Wedding Anna & Marco');
    await tester.tap(find.text('Send files'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'share.example.com');
    await tester.tap(find.text('Next'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'K7M2Q');
    await tester.tap(find.text('Unlock'));
    await shot(tester, 'en/45-pin-into-folder');
  }));

  testWidgets('a PIN that shows its folder', (tester) => atTen(() async {
    final server = library()
      ..pinFolderName = 'Wedding Anna & Marco'
      ..pinShowsFolder = true;
    server.folders = [{...FakeServer.folder(wedding, 'Wedding Anna & Marco', files: 412, bytes: 9800000000, people: 2), 'senders': 23}];
    final platform = FakePlatform()
      ..secrets['pin_token'] = 'shp_x'
      ..secrets['pin_session'] = jsonEncode({'server': 'https://share.example.com', 'pin_kind': 'day', 'expires_at': null});
    await startApp(tester, platform, server);
    await tester.tap(find.text('See'));
    await shot(tester, 'en/46-pin-sees-folder');
    await tester.tap(find.text('Send').last);
    await shot(tester, 'en/46-pin-send-beside-see');
  }));

  testWidgets('a PIN, without an account', (tester) => atTen(() async {
    await startApp(tester, FakePlatform(), FakeServer());
    await tester.tap(find.text('Send files'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'share.example.com');
    await tester.tap(find.text('Next'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'K7M2');
    await shot(tester, 'en/23-pin');
    await tester.enterText(find.byType(TextField), 'K7M2Q');
    await tester.tap(find.text('Unlock'));
    await shot(tester, 'en/24-pin-send');
  }));

  // Sending photos as ZIPs, and opening them (docs/zip-plan.md, screens 98 to 110).
  FakePlatform sunday() => FakePlatform()
    ..zipArrival = ZipToPack(sundayFiles())
    ..zipSizes = [for (final f in sundayFiles()) f.size];

  testWidgets('a ZIP: name, and where it goes', (tester) => atTen(() async {
    await startApp(tester, sunday(), FakeServer());
    await shot(tester, 'en/98-zip-setup');
  }));

  testWidgets('a ZIP: packing, and ready to send', (tester) => atTen(() async {
    final platform = sunday();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Pack ZIP'));
    await tester.pump();
    platform.zipPackingEvents.add(const ZipPackState(stage: ZipPackStage.packing, filesDone: 8, files: 14, bytesDone: 200000000, bytesTotal: 312000000, part: 1, parts: 1));
    await shot(tester, 'en/99-zip-packing');
    platform.zipPackingEvents.add(const ZipPackState(stage: ZipPackStage.ready, plan: ZipPlanInfo(parts: [ZipPartInfo(number: 1, name: 'Photos 4 Oct 2026.zip', bytes: 312004000, files: 14)])));
    await shot(tester, 'en/100-zip-ready');
  }));

  testWidgets('a ZIP from the library', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..secrets['folder'] = family, folders());
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.share));
    await shot(tester, 'en/101-zip-library');
  }));

  testWidgets('a ZIP on the Send tab', (tester) => atTen(() async {
    await startApp(tester, signedInPhone(), FakeServer());
    await tester.tap(find.text('Send').last);
    await tester.pumpAndSettle();
    await tester.drag(find.byType(ListView).last, const Offset(0, -400));
    await shot(tester, 'en/102-zip-send-tab');
  }));

  testWidgets('a ZIP: where it goes, and four of them', (tester) => atTen(() async {
    final files = [
      for (var i = 0; i < 49; i++) ZipFileInfo(name: 'IMG_$i.jpg', size: 90000000, type: 'image/jpeg', kind: FileKind.photo, taken: DateTime(2026, 10, 15 + i % 4, 10)),
      ZipFileInfo(name: 'VID_20261017_141502.mp4', size: 2590000000, type: 'video/mp4', kind: FileKind.video, taken: DateTime(2026, 10, 17, 14, 15)),
    ];
    final platform = FakePlatform()
      ..zipArrival = ZipToPack(files)
      ..zipSizes = [for (final f in files) f.size];
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Change'));
    await shot(tester, 'en/103-zip-where');
    await tester.tap(find.text('WhatsApp, Telegram').last);
    await tester.pumpAndSettle();
    await tester.tap(find.text('Pack 4 ZIPs'));
    await tester.pump();
    platform.zipPackingEvents.add(const ZipPackState(
      stage: ZipPackStage.ready,
      plan: ZipPlanInfo(parts: [
        ZipPartInfo(number: 1, bytes: 1900000000, files: 18),
        ZipPartInfo(number: 2, bytes: 1900000000, files: 22),
        ZipPartInfo(number: 3, bytes: 1900000000, files: 11, pieces: [ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 1, pieces: 2)]),
        ZipPartInfo(number: 4, bytes: 1300000000, files: 0, pieces: [ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 2, pieces: 2)]),
      ], cut: [ZipCutInfo(name: 'VID_20261017_141502.mp4', size: 2400000000, parts: [3, 4])]),
    ));
    await shot(tester, 'en/104-zip-parts');
  }));

  testWidgets('a ZIP opened: inside, and saved', (tester) => atTen(() async {
    final platform = signedInPhone()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = sundayZip();
    await startApp(tester, platform, FakeServer());
    await shot(tester, 'en/106-zip-inside');
    await tester.tap(find.text('Save 14 to this phone'));
    await tester.pump();
    platform.zipOpened = sundayZip(saved: true);
    platform.zipSavingEvents.add(const ZipSaveState(stage: ZipSaveStage.done, saved: 14, to: 'phone'));
    await shot(tester, 'en/107-zip-saved');
  }));

  testWidgets('ZIPs opened: all parts at once', (tester) => atTen(() async {
    final platform = signedInPhone()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = ZipContents(
        name: 'Dolomites 15–18 Oct 2026',
        zips: 4,
        bytes: 7000000000,
        set: const ZipSetInfo(parts: 4, here: [1, 2, 3, 4]),
        files: [
          for (var i = 0; i < 51; i++) ZipEntryInfo(index: i, name: 'IMG_$i.jpg', kind: FileKind.photo, size: 110000000),
          const ZipEntryInfo(index: 51, name: 'VID_20261017_141502.mp4.001', kind: FileKind.video, size: 1105000000, piece: ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 1, pieces: 2)),
          const ZipEntryInfo(index: 52, name: 'VID_20261017_141502.mp4.002', kind: FileKind.video, size: 1307345678, piece: ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 2, pieces: 2)),
        ],
        joins: const [ZipJoinInfo(file: 'VID_20261017_141502.mp4', kind: FileKind.video, total: 2412345678, pieces: 2, have: [1, 2], parts: [3, 4])],
      );
    await startApp(tester, platform, FakeServer());
    await shot(tester, 'en/108-zip-set');
  }));

  testWidgets('ZIPs opened: one part at a time', (tester) => atTen(() async {
    final platform = FakePlatform()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = dolomitesPart3();
    await startApp(tester, platform, FakeServer());
    platform.zipOpened = dolomitesPart3(saved: true);
    platform.zipSavingEvents.add(const ZipSaveState(stage: ZipSaveStage.done, saved: 11, to: 'phone'));
    await shot(tester, 'en/109-zip-part');
  }));

  testWidgets('ZIPs opened: the video back together', (tester) => atTen(() async {
    final joined = FakePlatform()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = const ZipContents(
        name: 'Dolomites 15–18 Oct 2026',
        bytes: 1300000000,
        set: ZipSetInfo(parts: 4, here: [4], saved: [1, 2, 3], to: 'phone'),
        files: [ZipEntryInfo(index: 0, name: 'VID_20261017_141502.mp4.002', kind: FileKind.video, size: 1307345678, piece: ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 2, pieces: 2))],
        joins: [ZipJoinInfo(file: 'VID_20261017_141502.mp4', kind: FileKind.video, total: 2412345678, pieces: 2, have: [1, 2], parts: [3, 4])],
      );
    await startApp(tester, joined, FakeServer());
    joined.zipSavingEvents.add(const ZipSaveState(
      stage: ZipSaveStage.done,
      saved: 1,
      to: 'phone',
      joined: [(file: 'VID_20261017_141502.mp4', kind: FileKind.video, total: 2412345678, parts: [3, 4])],
    ));
    await shot(tester, 'en/110-zip-joined');
  }));
}
