import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/api.dart';
import 'package:share_app/data/library.dart';
import 'package:share_app/data/models.dart';
import 'package:share_app/data/server.dart';

import 'support/fake_platform.dart';
import 'support/fake_server.dart';

final config = ServerConfig(publicUrl: Uri.parse('https://share.example.com'), serverId: 'srv');

/// Photos at full size for the viewer: from the server, or by link from its bucket.
void main() {
  late Directory cache;
  late FakePlatform platform;
  late FakeServer server;

  setUp(() async {
    cache = await Directory.systemTemp.createTemp('share-originals');
    platform = FakePlatform()..cache = cache.path;
    server = FakeServer()..addDay('2026-10-05', 2);
  });
  tearDown(() => cache.delete(recursive: true));

  Api phone() => Api(platform: platform, publicClient: server.client, localClient: (_) => server.client)
    ..config = config
    ..token = 'shd_x';
  FileInfo photo() => FileInfo.fromJson(server.files.first);

  test('from a drive it comes from the server, with the key', () async {
    final file = await LibraryRepository(api: phone(), platform: platform).original(photo());
    expect(await file!.readAsBytes(), FakeServer.contentOf(photo().id));
    final asked = server.requests.singleWhere((r) => r.url.path.endsWith('/content'));
    expect(asked.headers['Authorization'], 'Bearer shd_x');
  });

  test('from a bucket it comes by link, without the key', () async {
    server.s3 = true;
    final id = photo().id;
    final file = await LibraryRepository(api: phone(), platform: platform).original(photo());
    expect(await file!.readAsBytes(), FakeServer.contentOf(id));
    expect([for (final r in server.requests) '${r.url.host}${r.url.path}'], [
      'share.example.com/api/info',
      'share.example.com/api/s3/files/$id/url',
      'bucket.example.com/share/files/$id',
    ]);
    expect(server.requests[1].headers['Authorization'], 'Bearer shd_x');
    expect(server.requests.last.headers.containsKey('Authorization'), isFalse);
    // The second photo needn't ask where the files are.
    server.requests.clear();
    await LibraryRepository(api: phone(), platform: platform).original(FileInfo.fromJson(server.files.last));
    expect(server.requests.where((r) => r.url.path == '/api/info'), hasLength(1), reason: 'a new Api asks once');
  });

  test('a PIN that shows its folder gets the link with its own key', () async {
    server.s3 = true;
    final pin = LibraryRepository.pin(
      api: Api(platform: platform, publicClient: server.client),
      platform: platform,
      pin: (server: Uri.parse('https://share.example.com'), token: 'shp_guest'),
    );
    final file = await pin.original(photo());
    expect(await file!.readAsBytes(), FakeServer.contentOf(photo().id));
    expect(server.requests.singleWhere((r) => r.url.path.endsWith('/url')).headers['Authorization'], 'Bearer shp_guest');
    expect(server.requests.last.headers.containsKey('Authorization'), isFalse);
  });
}
