import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';
import 'package:share_app/data/zip.dart';
import 'package:video_player/video_player.dart';

import 'support/contract.dart';

/// The Dart half of contract/app/platform.json; PlatformContractTest.kt is the Kotlin half.
void main() {
  final fixture = contract('app/platform.json');

  test('the stored server config round-trips', () {
    final json = (fixture['server_config'] as Map).cast<String, dynamic>();
    final config = ServerConfig.fromJson(json);
    expect(config.hasLocal, isTrue);
    expect(config.localUrl.toString(), 'https://192.168.8.1:8443');
    expect(jsonEncode(config.toJson()), jsonEncode(json));
  });

  test('files for a download carry what Kotlin reads', () {
    for (final raw in fixture['files'] as List) {
      final json = (raw as Map).cast<String, dynamic>();
      final file = FileInfo.fromJson(json);
      expect(file.toJson(), json);
    }
    // The video is encrypted: its key and header go along, for Kotlin to decrypt it.
    final video = FileInfo.fromJson(((fixture['files'] as List)[1] as Map).cast());
    expect(video.enc?.version, 1);
    expect(video.enc?.header, 'U0hFMQABAAA9p6lLxQCBAA');
  });

  test('the keys event', () {
    final k = KeysState.fromMap(fixture['keys_event'] as Map);
    expect(k.ready, isTrue);
    expect(k.hasRecovery, isTrue);
    expect(k.encryptedFolders, 1);
    expect(k.hasFolderKey('f4mily5x2k7mbqz4bwdbyj6qsq', 1), isTrue);
    expect(k.hasFolderKey('f4mily5x2k7mbqz4bwdbyj6qsq', 2), isFalse);
    expect([for (final a in k.asks) (a.kind, a.name, a.isBrowser, a.code, a.keyChanged)], [
      ('device', 'Chrome · Windows', true, '735041', false),
      ('person', 'Maria', false, null, true),
    ]);
    expect(k.asks.first.since, DateTime.utc(2026, 10, 6, 17, 11, 11, 191));
    expect(k.asks.last.folders, ['f4mily5x2k7mbqz4bwdbyj6qsq']);
    expect([for (final c in k.codes) (c.kind, c.from, c.code)], [('person', 'Chrome · Windows', '813552')]);
    expect(k.waitsForFolders, isFalse);
    expect(k.pace, const Duration(seconds: 5));
    expect(KeysState.fromMap(const {'status': 'something new'}).status, KeysStatus.off);
  });

  test('route and transfer events', () {
    final route = RouteStatus.fromMap(fixture['route_event'] as Map);
    expect(route.isLocal, isTrue);
    expect(route.millis, 12);
    expect(route.reason, RouteReason.none);
    expect([for (final r in RouteReason.values) r.name], fixture['route_reasons']);

    final t = TransferState.fromMap(fixture['transfer_event'] as Map);
    expect([t.total, t.done, t.failed, t.skipped], [3, 1, 1, 1]);
    expect([t.bytesTotal, t.bytesDone], [3700, 1600]);
    expect([t.media, t.documents], [2, 1]);
    expect(t.running && t.local && !t.noSpace, isTrue);
  });

  test('upload events', () {
    final u = UploadState.fromMap(fixture['upload_event'] as Map);
    expect(u.auth, SendAuth.device);
    expect(u.folder, 'f4mily5x2k7mbqz4bwdbyj6qsq');
    expect([u.total, u.done, u.failed, u.lost], [5, 1, 1, 1]);
    expect([u.bytesTotal, u.bytesDone, u.etaSeconds], [61280000, 36380000, 120]);
    expect(u.current, 4, reason: 'three are behind it');
    expect(u.running && u.local && u.paused == null, isTrue);
    expect(u.items.map((i) => i.state), ['done', 'queued', 'queued', 'lost']);
    expect(u.items[1].kind, FileKind.video);
    expect(u.items[1].bytes, 33280000);
    expect(fixture['upload_paused'], ['pin_ended', 'signed_out', 'folder_gone', 'user']); // the send panel tells each
  });

  test('where the player plays from', () {
    final copy = PlaySource.fromMap(fixture['play_copy'] as Map);
    expect(copy.isCopy, isTrue);
    expect(copy.headers, isEmpty);
    final stream = PlaySource.fromMap(fixture['play_stream'] as Map);
    expect(stream.isCopy, isFalse);
    expect(stream.uri.path, '/api/files/bbbbbbbbbbbbbbbbbbbbbbbbbb/content');
    expect(stream.headers['Authorization'], startsWith('Bearer shd_'));
  });

  test('a link to the bucket reaches the player as it was signed', () {
    final link = fixture['play_s3'] as Map;
    final s3 = PlaySource.fromMap(link);
    expect(s3.isCopy, isFalse);
    expect(s3.uri.toString(), link['uri']);
    expect(s3.headers.keys, ['User-Agent'], reason: 'no key goes to the bucket');
    expect(VideoPlayerController.networkUrl(s3.uri, httpHeaders: s3.headers).dataSource, link['uri']);
    // Other services' links, and a bucket's at home, keep their escapes too.
    for (final uri in [
      'https://acct.r2.cloudflarestorage.com/share-files/share/files/b?X-Amz-Algorithm=AWS4-HMAC-SHA256&X-Amz-Credential=key%2F20261005%2Fauto%2Fs3%2Faws4_request&X-Amz-Date=20261005T101500Z&X-Amz-Expires=43200&X-Amz-SignedHeaders=host&response-content-disposition=attachment%3B%20filename%3D%22a%20b.mp4%22&X-Amz-Signature=ab',
      'https://s3.eu-central-003.backblazeb2.com/share-files/share/files/b?X-Amz-Credential=003abc%2F20261005%2Feu-central-003%2Fs3%2Faws4_request&X-Amz-Signature=cd',
      'http://192.168.8.52:9000/share/files/b?X-Amz-Credential=minio%2F20261005%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Signature=ef',
      'http://[fd12::52]:9000/share/files/b?X-Amz-Credential=minio%2F20261005%2Fus-east-1%2Fs3%2Faws4_request&X-Amz-Signature=ef',
    ]) {
      expect(PlaySource.fromMap({'uri': uri, 'headers': const {}}).uri.toString(), uri);
    }
  });

  test('ZIPs: the files to pack, a plan, and how packing goes', () {
    final take = ZipArrival.fromMap(fixture['zip_take'] as Map) as ZipToPack;
    expect(take.skipped, 1);
    expect([for (final f in take.files) (f.name, f.size, f.kind, f.onPhone)], [
      ('IMG_20261004_161502.jpg', 20000000, FileKind.photo, true),
      ('VID_20261004_161944.mp4', 36000000, FileKind.video, false),
    ]);
    expect(take.files.first.taken, DateTime.fromMillisecondsSinceEpoch(1791123302000));
    expect(ZipArrival.fromMap({'kind': 'open'}), isA<ZipToOpen>());
    expect(ZipArrival.fromMap(null), isNull);

    final plan = ZipPlanInfo.fromMap(fixture['zip_plan'] as Map);
    expect(plan.tooMany, isFalse);
    expect([for (final p in plan.parts) '${p.number}: ${p.files} files, pieces ${p.pieces.map((x) => x.number).join(',')}'], ['1: 2 files, pieces 1', '2: 0 files, pieces 2', '3: 0 files, pieces 3']);
    expect([for (final c in plan.cut) (c.name, c.size)], [('VID_20261004_161944.mp4', 500000)]);
    expect(plan.cut.single.parts, [1, 2, 3]);

    final packing = ZipPackState.fromMap(fixture['zip_packing'] as Map);
    expect([packing.stage, packing.filesDone, packing.files, packing.part, packing.parts, packing.fetching], [ZipPackStage.packing, 8, 14, 1, 1, null]);
    expect(packing.progress, closeTo(0.641, 0.001));
    final ready = ZipPackState.fromMap(fixture['zip_ready'] as Map);
    expect(ready.stage, ZipPackStage.ready);
    expect(ready.plan!.parts.single.name, 'Photos 4 Oct 2026.zip');
    final noRoom = ZipPackState.fromMap(fixture['zip_no_room'] as Map);
    expect([noRoom.stage, noRoom.needed, noRoom.free], [ZipPackStage.noRoom, 7012000000, 3100000000]);
    final failed = ZipPackState.fromMap(fixture['zip_failed'] as Map);
    expect([failed.stage, failed.reason, failed.failedFile], [ZipPackStage.failed, 'changed', 'IMG_20261004_161502.jpg']);
    final sent = ZipSent.fromMap(fixture['zip_sent'] as Map);
    expect((sent.part, sent.app), (0, 'WhatsApp'));
  });

  test('ZIPs opened: what they hold, and how saving goes', () {
    final c = ZipContents.fromMap(fixture['zip_contents'] as Map);
    expect(c.name, 'Dolomites 15–18 Oct 2026');
    expect([c.set!.parts, c.set!.here, c.set!.saved, c.set!.to], [4, [3], [1, 2], 'phone']);
    expect(c.fromWhatsapp, isTrue);
    expect([for (final f in c.files) (f.index, f.kind, f.piece?.number)], [(0, FileKind.photo, null), (1, FileKind.video, 1)]);
    final join = c.joinOf(c.files[1])!;
    expect([join.whole, join.missingParts], [false, [4]]);
    expect(c.toSave, 1);
    expect(c.waiting.single.file, 'VID_20261017_141502.mp4');

    final saving = ZipSaveState.fromMap(fixture['zip_saving'] as Map);
    expect([saving.stage, saving.done, saving.total], [ZipSaveStage.saving, 7, 14]);
    final saved = ZipSaveState.fromMap(fixture['zip_saved'] as Map);
    expect([saved.stage, saved.saved, saved.to], [ZipSaveStage.done, 11, 'phone']);
    expect(saved.joined.single.parts, [3, 4]);
    expect(saved.broken.single, (file: 'IMG_20261017_103001.jpg', part: 3));
  });
}
