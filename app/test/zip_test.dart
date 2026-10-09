import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:intl/intl.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/zip.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/zip/zip_screen.dart';

import 'app_test.dart' show signedInPhone, startApp, today;
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

final sunday = DateTime(2026, 10, 4, 16, 15);

/// Anna's Sunday (screens 97 to 100): 12 photos and 2 videos, 312 MB.
List<ZipFileInfo> sundayFiles() => [
      for (var i = 0; i < 12; i++) ZipFileInfo(name: 'IMG_20261004_${1000 + i}.jpg', size: 20000000, type: 'image/jpeg', kind: FileKind.photo, taken: sunday.add(Duration(minutes: i))),
      for (var i = 0; i < 2; i++) ZipFileInfo(name: 'VID_20261004_${2000 + i}.mp4', size: 36000000, type: 'video/mp4', kind: FileKind.video, taken: sunday.add(Duration(minutes: 20 + i))),
    ];

const words = (photos: 'Photos', videos: 'Videos', documents: 'Documents', files: 'Files');

ZipContents sundayZip({bool saved = false}) => ZipContents(
      name: 'Photos 4 Oct 2026',
      zips: 1,
      fromWhatsapp: true,
      bytes: 312000000,
      files: [for (final (i, f) in sundayFiles().indexed) ZipEntryInfo(index: i, name: f.name, kind: f.kind, size: f.size, saved: saved)],
    );

/// Part 3 of Stefan's Dolomites (screen 109): 11 photos and the video's first piece; parts 1 and 2
/// were saved on this phone before.
ZipContents dolomitesPart3({bool saved = false}) => ZipContents(
      name: 'Dolomites 15–18 Oct 2026',
      zips: 1,
      bytes: 1900000000,
      set: ZipSetInfo(parts: 4, here: const [3], saved: saved ? const [1, 2, 3] : const [1, 2], to: 'phone'),
      files: [
        for (var i = 0; i < 11; i++) ZipEntryInfo(index: i, name: 'IMG_$i.jpg', kind: FileKind.photo, size: 70000000, saved: saved),
        const ZipEntryInfo(index: 11, name: 'VID_20261017_141502.mp4.001', kind: FileKind.video, size: 1105000000, piece: ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 1, pieces: 2)),
      ],
      joins: const [ZipJoinInfo(file: 'VID_20261017_141502.mp4', kind: FileKind.video, total: 2412345678, pieces: 2, have: [1], parts: [3, 4])],
    );

void main() {
  setUpAll(() async {
    await loadFonts();
    await initializeDateFormatting();
  });

  group('the proposed name', () {
    String name(List<DateTime> days, String locale, {List<FileKind> kinds = const [FileKind.photo], String? folder}) =>
        zipName(kinds: kinds, days: days, locale: locale, words: words, folder: folder);

    test('one day, days of a month, across months and years, in three languages', () {
      final d = DateTime(2026, 10, 4);
      expect(name([d], 'en'), 'Photos 4 Oct 2026');
      expect(name([d, DateTime(2026, 10, 4, 23)], 'en'), 'Photos 4 Oct 2026');
      expect(name([DateTime(2026, 10, 18), DateTime(2026, 10, 15)], 'en'), 'Photos 15–18 Oct 2026');
      expect(name([DateTime(2026, 9, 28), DateTime(2026, 10, 3)], 'en'), 'Photos 28 Sep – 3 Oct 2026');
      expect(name([DateTime(2025, 12, 28), DateTime(2026, 1, 3)], 'en'), 'Photos 28 Dec 2025 – 3 Jan 2026');
      expect(name([DateTime(2026, 7, 2), DateTime(2026, 9, 20)], 'en'), 'Photos Jul – Sep 2026');
      expect(name([DateTime(2025, 11, 2), DateTime(2026, 2, 20)], 'en'), 'Photos Nov 2025 – Feb 2026');
      expect(zipName(kinds: const [FileKind.photo], days: [d], locale: 'de', words: (photos: 'Fotos', videos: 'Videos', documents: 'Dokumente', files: 'Dateien')), 'Fotos 4. Okt. 2026');
      expect(zipDays(DateTime(2026, 10, 15), DateTime(2026, 10, 18), 'de'), '15.–18. Okt. 2026');
      expect(zipDays(DateTime(2026, 10, 4), DateTime(2026, 10, 4), 'it'), '4 ott 2026');
      expect(zipDays(DateTime(2026, 10, 15), DateTime(2026, 10, 18), 'it'), '15–18 ott 2026');
    });

    test('the word says what the files are, or the folder they come from', () {
      final d = [DateTime(2026, 10, 4)];
      expect(name(d, 'en', kinds: const [FileKind.photo, FileKind.video]), 'Photos 4 Oct 2026');
      expect(name(d, 'en', kinds: const [FileKind.video]), 'Videos 4 Oct 2026');
      expect(name(d, 'en', kinds: const [FileKind.document]), 'Documents 4 Oct 2026');
      expect(name(d, 'en', kinds: const [FileKind.photo, FileKind.document]), 'Files 4 Oct 2026');
      expect(name(d, 'en', folder: 'Dolomites'), 'Dolomites 4 Oct 2026');
      expect(name(const [], 'en'), 'Photos');
    });

    test('a name typed is made fit for a file name', () {
      expect(cleanZipName('  Sunday: a/b\\c*?"<>| '), 'Sunday_ a_b_c______');
      expect(cleanZipName('...hidden...'), 'hidden');
      expect(cleanZipName('   '), '');
      expect(cleanZipName('x' * 100).length, 80);
    });

    test('where it goes is remembered, a size of one\'s own too', () {
      expect(ZipWhere.parse(null).where, ZipWhere.whatsapp);
      expect(ZipWhere.parse('email').where, ZipWhere.email);
      expect(ZipWhere.parse('other:500'), (where: ZipWhere.other, otherMb: 500));
      expect(ZipWhere.parse('other:0').where, ZipWhere.whatsapp);
      expect(ZipWhere.parse('nonsense').where, ZipWhere.whatsapp);
      expect(ZipWhere.other.save(250), 'other:250');
      expect(ZipWhere.other.bytes(250), 250000000);
      expect(ZipWhere.whatsapp.bytes(250), 1950000000);
      expect(ZipWhere.any.bytes(250), isNull);
    });
  });

  testWidgets('Send as ZIP from the gallery, signed out: the name, where it goes, packing and sending', (tester) async {
    final platform = FakePlatform()
      ..zipArrival = ZipToPack(sundayFiles())
      ..zipSizes = [for (final f in sundayFiles()) f.size];
    await startApp(tester, platform, FakeServer());

    expect(find.text('Send as ZIP'), findsOneWidget);
    expect(find.widgetWithText(TextField, 'Photos 4 Oct 2026'), findsOneWidget);
    expect(find.text('12 photos, 2 videos · 312 MB'), findsOneWidget);
    expect(find.text('WhatsApp, Telegram'), findsOneWidget);
    expect(find.text('Up to 2 GB each: all in one ZIP'), findsOneWidget);

    await tester.tap(find.text('Pack ZIP'));
    await tester.pumpAndSettle();
    final pack = platform.zipPacks.single;
    expect(pack.name, 'Photos 4 Oct 2026');
    expect(pack.partName, '{name} ({part} of {parts})');
    expect(pack.limit, 1950000000);
    expect(pack.about, startsWith('Made with Share.'));
    expect(platform.secrets['zip_where'], 'whatsapp');

    platform.zipPackingEvents.add(const ZipPackState(stage: ZipPackStage.packing, filesDone: 8, files: 14, bytesDone: 200000000, bytesTotal: 312000000, part: 1, parts: 1));
    await tester.pumpAndSettle();
    expect(find.text('Photos 4 Oct 2026.zip'), findsOneWidget);
    expect(find.text('Packing 9 of 14'), findsOneWidget);
    expect(find.text('64%'), findsOneWidget);

    platform.zipPackingEvents.add(const ZipPackState(stage: ZipPackStage.ready, plan: ZipPlanInfo(parts: [ZipPartInfo(number: 1, name: 'Photos 4 Oct 2026.zip', bytes: 312004000, files: 14)])));
    await tester.pumpAndSettle();
    expect(find.text('Fits WhatsApp and Telegram, which take up to 2 GB. Too big for most email.'), findsOneWidget);
    await tester.tap(find.text('Send ZIP'));
    await tester.pumpAndSettle();
    expect(platform.zipSends, [null]);

    await tester.tap(find.text('Save to Downloads'));
    await tester.pumpAndSettle();
    expect(find.text('Saved in Downloads › Share.'), findsOneWidget);

    await tester.tap(find.byIcon(AppIcons.x));
    await tester.pumpAndSettle();
    expect(platform.zipCloses, 1);
    expect(find.text('What would you like to do?'), findsOneWidget);
  });

  testWidgets('where it goes: what each choice makes, too many, and a size of one\'s own', (tester) async {
    final files = [
      for (var i = 0; i < 49; i++) ZipFileInfo(name: 'IMG_$i.jpg', size: 90000000, type: 'image/jpeg', kind: FileKind.photo, taken: sunday),
      ZipFileInfo(name: 'VID_1.mp4', size: 2590000000, type: 'video/mp4', kind: FileKind.video, taken: sunday),
    ];
    final platform = FakePlatform()
      ..zipArrival = ZipToPack(files)
      ..zipSizes = [for (final f in files) f.size]; // 7.0 GB
    await startApp(tester, platform, FakeServer());
    expect(find.text('Up to 2 GB each: 4 ZIPs'), findsOneWidget);
    expect(find.text('Pack 4 ZIPs'), findsOneWidget);

    await tester.tap(find.text('Change'));
    await tester.pumpAndSettle();
    expect(find.text('Where does it go?'), findsOneWidget);
    expect(find.text('4 ZIPs'), findsOneWidget);
    expect(find.text('74 ZIPs'), findsOneWidget); // Signal
    expect(find.text('Too many'), findsOneWidget); // email
    expect(find.text('1 ZIP'), findsOneWidget); // any size
    expect(find.text('14 ZIPs'), findsOneWidget); // 500 MB

    // Email would make more than 100: it can't be picked.
    await tester.tap(find.text('Email'));
    await tester.pumpAndSettle();
    expect(find.text('Where does it go?'), findsOneWidget);

    await tester.enterText(find.byType(TextField).last, '1000');
    await tester.pump(const Duration(milliseconds: 400));
    await tester.pumpAndSettle();
    expect(find.text('7 ZIPs'), findsOneWidget);
    await tester.tap(find.text('Somewhere else'));
    await tester.pumpAndSettle();
    expect(find.text('Up to 1 GB each: 7 ZIPs'), findsOneWidget);
    expect(platform.secrets['zip_where'], 'other:1000');
    expect(find.text('Pack 7 ZIPs'), findsOneWidget);

    await tester.tap(find.text('Pack 7 ZIPs'));
    await tester.pumpAndSettle();
    expect(platform.zipPacks.single.limit, 1000000000);
  });

  testWidgets('four ZIPs: each part, the video in pieces, and sending one part', (tester) async {
    final platform = FakePlatform()
      ..zipArrival = ZipToPack(sundayFiles())
      ..zipSizes = [for (final f in sundayFiles()) f.size];
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Pack ZIP'));
    await tester.pumpAndSettle();
    platform.zipPackingEvents.add(const ZipPackState(
      stage: ZipPackStage.ready,
      plan: ZipPlanInfo(parts: [
        ZipPartInfo(number: 1, bytes: 1900000000, files: 18),
        ZipPartInfo(number: 2, bytes: 1900000000, files: 22),
        ZipPartInfo(number: 3, bytes: 1900000000, files: 11, pieces: [ZipPieceInfo(file: 'VID_20261004_2000.mp4', number: 1, pieces: 2)]),
        ZipPartInfo(number: 4, bytes: 1300000000, files: 0, pieces: [ZipPieceInfo(file: 'VID_20261004_2000.mp4', number: 2, pieces: 2)]),
      ], cut: [ZipCutInfo(name: 'VID_20261004_2000.mp4', size: 2400000000, parts: [3, 4])]),
    ));
    await tester.pumpAndSettle();
    expect(find.text('Part 1 of 4'), findsOneWidget);
    expect(find.text('18 files · 1.9 GB'), findsOneWidget);
    expect(find.text('11 files and a piece of a big file · 1.9 GB'), findsOneWidget);
    expect(find.text('A piece of a big file · 1.3 GB'), findsOneWidget);
    expect(find.textContaining('1 video goes in 2 pieces.'), findsOneWidget);
    expect(find.textContaining('VID_20261004_2000.mp4 is 2.4 GB'), findsOneWidget);

    await tester.tap(find.byTooltip('Send part 2'));
    await tester.pumpAndSettle();
    expect(platform.zipSends, [2]);
    platform.zipSentEvents.add(const ZipSent(part: 2, app: 'WhatsApp'));
    await tester.pumpAndSettle();
    expect(find.text('Sent with WhatsApp'), findsOneWidget);

    await tester.scrollUntilVisible(find.text('Send 4 ZIPs'), 200);
    await tester.tap(find.text('Send 4 ZIPs'));
    await tester.pumpAndSettle();
    expect(platform.zipSends, [2, null]);
  });

  testWidgets('a ZIP from a chat: what\'s inside, saving it, and where it went', (tester) async {
    final platform = signedInPhone()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = sundayZip();
    await startApp(tester, platform, FakeServer());
    expect(find.text('Photos 4 Oct 2026'), findsOneWidget);
    expect(find.text('12 photos, 2 videos · 312 MB'), findsOneWidget);
    expect(find.text('Into the gallery, album “Share”'), findsOneWidget);
    expect(find.text('Send into Family'), findsOneWidget);

    await tester.tap(find.text('Save 14 to this phone'));
    await tester.pumpAndSettle();
    expect(platform.zipSaves.single, (to: 'phone', folder: null));
    platform.zipSavingEvents.add(const ZipSaveState(stage: ZipSaveStage.saving, done: 7, total: 14, bytesDone: 150000000, bytesTotal: 312000000));
    await tester.pumpAndSettle();
    expect(find.text('Saving 8 of 14'), findsOneWidget);

    platform.zipOpened = sundayZip(saved: true);
    platform.zipSavingEvents.add(const ZipSaveState(stage: ZipSaveStage.done, saved: 14, to: 'phone'));
    await tester.pumpAndSettle();
    expect(find.text('14 files saved'), findsOneWidget);
    expect(find.text('In your gallery, in the album “Share”.'), findsOneWidget);
    expect(find.text('WhatsApp still keeps the ZIP. Deleting it in the chat frees 312 MB.'), findsOneWidget);

    await tester.tap(find.text('Open gallery'));
    await tester.pumpAndSettle();
    expect(platform.galleryOpened, 1);
    await tester.tap(find.text('Done'));
    await tester.pumpAndSettle();
    expect(platform.zipOpenedCloses, 1);
    expect(find.text('Library'), findsWidgets);
  });

  testWidgets('sent into a folder: the files go to the outbox, as files shared from another app', (tester) async {
    final platform = signedInPhone()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = sundayZip();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Send into Family'));
    await tester.pumpAndSettle();
    expect(platform.zipSaves.single, (to: 'folder', folder: 'f4mily5x2k7mbqz4bwdbyj6qsq'));
  });

  testWidgets('a later part of a set is saved by itself, and a video waits for its other part', (tester) async {
    final platform = FakePlatform()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = dolomitesPart3();
    await startApp(tester, platform, FakeServer());
    // Parts 1 and 2 went to this phone, so part 3 follows without a tap.
    expect(platform.zipSaves.single, (to: 'phone', folder: null));

    platform.zipOpened = dolomitesPart3(saved: true);
    platform.zipSavingEvents.add(const ZipSaveState(stage: ZipSaveStage.done, saved: 11, to: 'phone'));
    await tester.pumpAndSettle();
    expect(find.text('Dolomites 15–18 Oct 2026'), findsOneWidget);
    expect(find.text('Part 3 of 4 · saved like the parts before'), findsOneWidget);
    expect(find.text('Saved'), findsNWidgets(2));
    expect(find.text('Just now'), findsOneWidget);
    expect(find.text('Not yet'), findsOneWidget);
    expect(find.text('Piece 1 of 2'), findsOneWidget);
    expect(find.textContaining('The video waits for part 4.'), findsOneWidget);
    expect(find.textContaining('Open part 4 from the chat too, and Share puts VID_20261017_141502.mp4 back together.'), findsOneWidget);
    expect(find.text('Done'), findsOneWidget);
  });

  testWidgets('the last piece puts the video back together', (tester) async {
    final platform = FakePlatform()
      ..zipArrival = const ZipToOpen()
      ..zipOpened = const ZipContents(
        name: 'Dolomites 15–18 Oct 2026',
        bytes: 1300000000,
        set: ZipSetInfo(parts: 4, here: [4], saved: [1, 2, 3], to: 'phone'),
        files: [ZipEntryInfo(index: 0, name: 'VID_20261017_141502.mp4.002', kind: FileKind.video, size: 1307345678, piece: ZipPieceInfo(file: 'VID_20261017_141502.mp4', number: 2, pieces: 2))],
        joins: [ZipJoinInfo(file: 'VID_20261017_141502.mp4', kind: FileKind.video, total: 2412345678, pieces: 2, have: [1, 2], parts: [3, 4])],
      );
    await startApp(tester, platform, FakeServer());
    expect(platform.zipSaves.single.to, 'phone');
    platform.zipSavingEvents.add(const ZipSaveState(
      stage: ZipSaveStage.done,
      saved: 1,
      to: 'phone',
      joined: [(file: 'VID_20261017_141502.mp4', kind: FileKind.video, total: 2412345678, parts: [3, 4])],
    ));
    await tester.pumpAndSettle();
    expect(find.text('Video put back together'), findsOneWidget);
    expect(find.text('VID_20261017_141502.mp4, 2.4 GB, from parts 3 and 4. Share checked that nothing is missing and saved it.'), findsOneWidget);
    expect(find.text('All 4 parts are in'), findsOneWidget);
  });

  testWidgets('from the library: Share, As a ZIP, named after the folder', (tester) async {
    final server = FakeServer()..addDay(today(), 6);
    final platform = signedInPhone();
    await startApp(tester, platform, server);
    await tester.tap(find.bySemanticsLabel('Select the day').first);
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.share));
    await tester.pumpAndSettle();
    expect(find.text('Share 6 files'), findsOneWidget);
    expect(find.text('6 of them aren\'t on this phone yet: Share gets them from the server first.'), findsOneWidget);

    await tester.tap(find.text('As a ZIP'));
    await tester.pumpAndSettle();
    expect(platform.zipLibraries.single, hasLength(6));
    expect(find.byType(ZipScreen), findsOneWidget);
    final day = DateTime.parse('${today()}T10:59:00Z').toLocal();
    expect(find.widgetWithText(TextField, 'Family ${DateFormat('d MMM y', 'en').format(day)}'), findsOneWidget);
    expect(find.byType(LibraryTile), findsNothing);
  });
}
