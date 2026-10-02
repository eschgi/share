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

  test('the admin screens\' shapes', () {
    final pins = [for (final p in contractResponse('api/pins.json')['pins'] as List) PinInfo.fromJson((p as Map).cast())];
    expect(pins.map((p) => p.kind), [PinKind.permanent, PinKind.day]);
    expect(pins.first.expiresAt, isNull);
    expect(pins.last.expiresAt, isNotNull);
    expect(pins.last.link, 'https://share.example.com/#4HX9T');
    expect([pins.last.files, pins.last.phones], [45, 7]);

    final people = People.fromJson(contractResponse('api/people.json'));
    expect(people.users.first.isMe, isTrue);
    expect(people.users.first.phones.single.isThis, isTrue);
    expect(people.users.last.phones, hasLength(2));
    expect([people.users.last.phones.first.isBrowser, people.users.last.phones.last.isBrowser], [false, true]);
    expect(people.users.last.phones.last.homeOnly, isTrue);
    expect(people.invites.single.userId, isNull);

    final invite = NewInvite.fromJson(contractResponse('api/invite_create.json'));
    expect(invite.link, endsWith('#shi_0123456789abcdefghijklmnopqrstuvwxyzABCDEFG'));
    expect(invite.invite.role, Role.member);

    final trash = Trash.fromJson(contractResponse('api/trash.json'));
    expect(trash.days, 30);
    expect(trash.files.single.file.name, 'IMG_2041.jpg');
    expect(trash.files.single.deletedBy, 'Stefan');
    expect(trash.files.single.purgeAt.difference(trash.files.single.deletedAt).inDays, 30);

    final storage = StorageInfo.fromJson(contractResponse('api/storage.json'));
    expect(storage.storageDir, '/mnt/usb/share');
    expect(storage.freeBytes, lessThan(storage.totalBytes));
    expect(storage.trashFiles, 12);
    expect(storage.warnings.single.code, 'ignores_case');
    expect(storage.warnings.single.problem, isFalse);
  });
}
