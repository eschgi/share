import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';

import 'app_test.dart' show signedInPhone, startApp;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

UploadState sample({bool running = true, int lost = 0, String? paused, SendAuth auth = SendAuth.device}) => UploadState(
      batch: 'up-1',
      auth: auth,
      running: running,
      paused: paused,
      total: 5,
      done: running ? 1 : 5 - lost,
      lost: lost,
      bytesTotal: 61280000,
      bytesDone: running ? 36380000 : 61280000,
      etaSeconds: running ? 125 : null,
      local: true,
      items: [
        const UploadItemState(seq: 0, name: '20260930_094512.jpg', size: 3100000, kind: FileKind.photo, state: 'done', bytes: 3100000),
        UploadItemState(seq: 1, name: '20260930_094530.mp4', size: 52000000, kind: FileKind.video, state: running ? 'queued' : 'done', bytes: 33280000),
        UploadItemState(seq: 2, name: 'Kindergarten_form.pdf', size: 480000, kind: FileKind.document, state: lost > 0 ? 'lost' : 'queued'),
      ],
    );

void main() {
  setUpAll(loadFonts);

  testWidgets('sending from the Send tab: picking, progress, stopping', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Send').last);
    await tester.pumpAndSettle();
    expect(find.text("No PIN needed while you're signed in."), findsOneWidget);

    await tester.tap(find.text('Photos & videos'));
    await tester.pumpAndSettle();
    expect(platform.picks.single, (PickWhat.media, SendAuth.device));

    platform.uploadEvents.add(sample());
    await tester.pumpAndSettle();
    expect(find.text('Sending 2 of 5'), findsOneWidget);
    expect(find.text('About 2 min left'), findsOneWidget);
    expect(find.text('64%'), findsOneWidget);
    expect(find.text('Waiting'), findsOneWidget);
    expect(find.text('Direct · local Wi-Fi'), findsOneWidget);

    await tester.tap(find.text('Stop sending'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Stop sending').last);
    await tester.pumpAndSettle();
    expect(platform.cancelledUploads, ['up-1']);
  });

  testWidgets('files the phone didn\'t keep are picked again', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Send').last);
    await tester.pumpAndSettle();
    platform.uploadEvents.add(sample(running: false, lost: 1));
    await tester.pumpAndSettle();
    expect(find.text('4 files sent'), findsOneWidget);
    expect(find.textContaining("Your phone didn't keep 1 file."), findsOneWidget);
    await tester.tap(find.text('Pick them again'));
    await tester.pumpAndSettle();
    expect(platform.picks.single, (PickWhat.documents, SendAuth.device), reason: 'the lost one is a document');
  });

  testWidgets('without an account: the address, a wrong PIN, the right one, sending', (tester) async {
    final platform = FakePlatform();
    final server = FakeServer();
    await startApp(tester, platform, server);
    await tester.tap(find.text('Send files'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'share.example.com');
    await tester.tap(find.text('Next'));
    await tester.pumpAndSettle();
    expect(find.text('Enter your PIN'), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'a2b3c');
    await tester.tap(find.text('Unlock'));
    await tester.pumpAndSettle();
    expect(find.text("That PIN didn't work. 4 tries left."), findsOneWidget);

    await tester.enterText(find.byType(TextField), 'k7m2q');
    await tester.tap(find.text('Unlock'));
    await tester.pumpAndSettle();
    expect(find.text('To share.example.com'), findsOneWidget);
    expect(find.textContaining('This PIN works until'), findsOneWidget);
    expect(platform.secrets['pin_token'], startsWith('shp_'));
    expect(platform.pinServer!.publicUrl.toString(), 'https://share.example.com');
    expect(platform.pinServer!.localUrl.toString(), 'https://192.168.8.1:8443');
    expect(platform.resumed, [SendAuth.pin]);
    final unlock = server.requests.lastWhere((r) => r.url.path == '/api/pin/unlock');
    expect(jsonDecode(unlock.body), {'code': 'K7M2Q', 'client': 'app'});

    await tester.tap(find.text('Other files'));
    await tester.pumpAndSettle();
    expect(platform.picks.single, (PickWhat.documents, SendAuth.pin));
  });

  testWidgets('a PIN that ended: a new one takes over what\'s unfinished', (tester) async {
    final platform = FakePlatform()
      ..secrets['pin_token'] = 'shp_old'
      ..secrets['pin_session'] = jsonEncode({'server': 'https://share.example.com', 'pin_kind': 'day', 'expires_at': null});
    final server = FakeServer()..pinEnded = true;
    await startApp(tester, platform, server);
    expect(find.text('The PIN you used has ended. Enter a new PIN to finish sending.'), findsOneWidget);

    await tester.tap(find.text('Enter a new PIN'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'K7M2Q');
    await tester.tap(find.text('Unlock'));
    await tester.pumpAndSettle();
    final unlock = server.requests.lastWhere((r) => r.url.path == '/api/pin/unlock');
    expect(unlock.headers['Authorization'], 'Bearer shp_old', reason: 'so the unfinished uploads move over');
    expect(platform.secrets['pin_token'], contractResponse('api/pin_unlock_app.json')['token']);
    expect(find.text('Enter a new PIN'), findsNothing);
  });
}
