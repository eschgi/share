@Tags(['golden'])
library;

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/ui/settings_screen.dart';

import 'app_test.dart' show daysAgo, inviteToken, signedInPhone, startApp, today;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

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

  // Maria's three folders, as in the mockups, with the library showing Family.
  FakeServer folders() {
    final server = library();
    Map<String, dynamic> cover(int i) => {...server.files[i], 'has_thumb': true};
    return server
      ..folders = [
        FakeServer.folder('f4mily5x2k7mbqz4bwdbyj6qsq', 'Family', files: 2340, bytes: 41000000000, cover: cover(0)),
        FakeServer.folder('w3dd1ng5x2k7mbqz4bwdbyj6qs', 'Wedding Anna & Marco', files: 412, bytes: 9800000000, cover: cover(7)),
        FakeServer.folder('k1nd3rg4rt3nmbqz4bwdbyj6qs', 'Kindergarten', files: 86, bytes: 340000000),
      ];
  }

  testWidgets('a folder', (tester) => atTen(() async {
    await startApp(tester, signedInPhone()..secrets['folder'] = 'f4mily5x2k7mbqz4bwdbyj6qsq', folders());
    await shot(tester, 'en/38-library-folder');
    await tester.tap(find.text('Family'));
    await shot(tester, 'en/39-choose-folder');
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
}
