import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/data/models.dart';

import 'support/contract.dart';

void main() {
  test('a signed-in phone, from the invite fixture', () {
    final s = SignedIn.fromJson(contractResponse('api/invite_accept.json'));
    expect(s.token, startsWith('shd_'));
    expect(s.user.name, 'Maria');
    expect(s.user.role, Role.member);
    expect(s.server.localUrl, 'https://192.168.8.1:8443');
    expect(s.server.localCertSha256.single, hasLength(64));
  });

  test('a page of files', () {
    final page = FilePage.fromJson(contractResponse('api/files.json'));
    final f = page.files.single;
    expect(f.kind, FileKind.photo);
    expect(f.day, '2026-09-27');
    expect(f.width, 4032);
    expect(f.durationMs, isNull);
    expect(f.hasThumb, isTrue);
    expect(f.from, 'Maria');
    expect(f.ext, 'JPG');
    expect(page.nextCursor, isNotNull);
  });

  test('the library overview and ids', () {
    final lib = LibraryOverview.fromJson(contractResponse('api/library.json'));
    expect(lib.days.single.count, 6);
    expect(FileIds.fromJson(contractResponse('api/file_ids.json')).ids, hasLength(1));
  });

  test('an invite peek', () {
    final p = InvitePeek.fromJson(contractResponse('api/invite_peek.json'));
    expect(p.inviter, 'Stefan');
    expect(p.addsPhone, isFalse);
  });

  test('odd fields give defaults instead of errors', () {
    final f = FileInfo.fromJson({'id': 'x', 'size': 'big', 'kind': 'hologram'});
    expect(f.size, 0);
    expect(f.kind, FileKind.document);
  });
}
