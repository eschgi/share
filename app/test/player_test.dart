import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';

import 'app_test.dart' show signedInPhone, startApp, today;
import 'support/fake_player.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// Videos and sound play in the viewer.
void main() {
  setUpAll(loadFonts);

  testWidgets('a video plays in the viewer, keeping the screen on, and pauses when swiped away', (tester) async {
    final platform = signedInPhone();
    final players = <FakeMediaPlayer>[];
    await startApp(tester, platform, FakeServer()..addDay(today(), 6), player: (s) => FakeMediaPlayer(s)..also(players.add));
    await tester.tap(find.byType(LibraryTile).at(2)); // the day's video
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Play'));
    await tester.pumpAndSettle();

    final player = players.single;
    expect(player.source.uri.path, endsWith('/content'), reason: 'from the server, with the key');
    expect(player.source.headers['Authorization'], 'Bearer shd_x');
    expect(player.now.playing, isTrue);
    expect(platform.screenOn, isTrue);
    expect(find.text('0:18'), findsOneWidget);

    await tester.tap(find.byTooltip('Pause'));
    await tester.pumpAndSettle();
    expect(player.now.playing, isFalse);
    expect(platform.screenOn, isFalse);

    await tester.tap(find.byTooltip('Play').last);
    await tester.pumpAndSettle();
    await tester.drag(find.byType(PageView), const Offset(-600, 0));
    await tester.pumpAndSettle();
    expect(player.now.playing, isFalse, reason: 'swiped to the next file');
    expect(platform.screenOn, isFalse);
  });

  testWidgets('trying again after it stopped halfway goes on from there', (tester) async {
    final players = <FakeMediaPlayer>[];
    await startApp(tester, signedInPhone(), FakeServer()..addDay(today(), 6), player: (s) => FakeMediaPlayer(s)..also(players.add));
    await tester.tap(find.byType(LibraryTile).at(2));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Play'));
    await tester.pumpAndSettle();
    await players.single.seekTo(const Duration(seconds: 7));
    await tester.pump();
    expect(find.text('0:07'), findsOneWidget);

    players.single.fail();
    await tester.pumpAndSettle();
    expect(find.textContaining("can't be played here"), findsOneWidget);
    await tester.tap(find.text('Try again'));
    await tester.pumpAndSettle();
    expect(players, hasLength(2));
    expect(players.first.disposed, isTrue);
    expect(players.last.now.position, const Duration(seconds: 7));
    expect(players.last.now.playing, isTrue);
    expect(find.text('0:07'), findsOneWidget);
  });

  testWidgets("what can't play here opens in another app", (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer()..addDay(today(), 6), player: (s) => FakeMediaPlayer(s, fails: true));
    await tester.tap(find.byType(LibraryTile).at(2));
    await tester.pumpAndSettle();
    await tester.tap(find.byTooltip('Play'));
    await tester.pumpAndSettle();
    expect(find.textContaining("can't be played here"), findsOneWidget);
    await tester.tap(find.text('Open in another app'));
    await tester.pumpAndSettle();
    expect(platform.opened, ['file:${platform.played.single}']);
  });

  testWidgets('sound plays too, and the menu opens any file in another app', (tester) async {
    final platform = signedInPhone()..playSource = (f) => PlaySource(Uri.parse('content://com.eschgi.share.files/fetch/${f.id}/${f.name}'));
    final players = <FakeMediaPlayer>[];
    final server = FakeServer()..addDay(today(), 1);
    server.files.insert(0, {
      ...server.files.first,
      'id': 'aaaaaaaaaaaaaaaaaaaaaaaaaa',
      'name': 'Voice message.m4a',
      'mime': 'audio/mp4',
      'kind': 'document',
      'width': null,
      'height': null,
    });
    await startApp(tester, platform, server, player: (s) => FakeMediaPlayer(s)..also(players.add));
    await tester.tap(find.byType(LibraryTile).first);
    await tester.pumpAndSettle();
    expect(find.byIcon(AppIcons.music), findsOneWidget);
    await tester.tap(find.byTooltip('Play'));
    await tester.pumpAndSettle();
    expect(players.single.source.isCopy, isTrue, reason: 'a copy on the phone needs no key');
    expect(players.single.now.playing, isTrue);

    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Open in another app'));
    await tester.pumpAndSettle();
    expect(platform.opened, ['file:aaaaaaaaaaaaaaaaaaaaaaaaaa']);
  });
}

extension<T> on T {
  T also(void Function(T) f) {
    f(this);
    return this;
  }
}
