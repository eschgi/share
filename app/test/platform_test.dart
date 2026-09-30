import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';

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
    expect([u.total, u.done, u.failed, u.lost], [5, 1, 1, 1]);
    expect([u.bytesTotal, u.bytesDone, u.etaSeconds], [61280000, 36380000, 120]);
    expect(u.current, 4, reason: 'three are behind it');
    expect(u.running && u.local && u.paused == null, isTrue);
    expect(u.items.map((i) => i.state), ['done', 'queued', 'queued', 'lost']);
    expect(u.items[1].kind, FileKind.video);
    expect(u.items[1].bytes, 33280000);
    expect(fixture['upload_paused'], ['pin_ended', 'signed_out', 'user']);
  });
}
