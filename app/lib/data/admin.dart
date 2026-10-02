/// What admins do (screens 17–21): PINs, people and invites, deleting files and the trash.
library;

import 'api.dart';
import 'models.dart';

class AdminRepository {
  AdminRepository({required this.api});

  final Api api;

  /// How long Recently deleted keeps files; known once storage or the trash was asked for.
  int trashDays = 30;

  /// The server takes at most this many ids at once; bigger selections go in parts.
  static const idsPerRequest = 1000;

  Future<List<PinInfo>> pins() async =>
      [for (final p in (await api.get('/api/pins'))['pins'] as List? ?? const []) PinInfo.fromJson((p as Map).cast())];

  Future<String> suggestPin() async => (await api.get('/api/pins/suggest'))['code'] as String? ?? '';

  /// Throws ApiException pin_taken or pin_format for a [code] that can't be had.
  Future<PinInfo> createPin(PinKind kind, {String? code}) async =>
      PinInfo.fromJson(await api.post('/api/pins', {'kind': kind.wire, if (code != null && code.isNotEmpty) 'code': code}));

  Future<PinInfo> newCode(String id) async => PinInfo.fromJson(await api.post('/api/pins/$id/new-code'));

  Future<void> endPin(String id) => api.post('/api/pins/$id/end');

  Future<People> people() async => People.fromJson(await api.get('/api/users'));

  /// Throws ApiException last_admin when nobody would be left to manage the server.
  Future<void> setRole(String userId, Role role) => api.patch('/api/users/$userId', {'role': role.name});

  Future<void> removePerson(String userId) => api.delete('/api/users/$userId');

  Future<void> signOutPhone(String phoneId) => api.delete('/api/devices/$phoneId');

  /// Makes up a new password for someone else, shown once; [username] is needed if they have
  /// none. Throws ApiException username_taken or bad_request for a username that won't do.
  Future<({String username, String password})> newPassword(String userId, {String? username}) async {
    final r = await api.post('/api/users/$userId/password', {'username': ?username});
    return (username: r['username'] as String? ?? '', password: r['password'] as String? ?? '');
  }

  Future<NewInvite> invite(String name, Role role) async =>
      NewInvite.fromJson(await api.post('/api/invites', {'name': name.trim(), 'role': role.name}));

  /// An invite that adds a phone for someone who has an account.
  Future<NewInvite> invitePhone(String userId) async => NewInvite.fromJson(await api.post('/api/users/$userId/invites'));

  Future<void> withdrawInvite(String id) => api.delete('/api/invites/$id');

  /// Moves files to Recently deleted; returns how many were in the library.
  Future<int> deleteFiles(List<String> ids) => _inParts('/api/files/delete', ids);

  Future<Trash> trash() async {
    final trash = Trash.fromJson(await api.get('/api/trash'));
    trashDays = trash.days;
    return trash;
  }

  Future<int> restore(List<String> ids) => _inParts('/api/trash/restore', ids);

  Future<int> purge(List<String> ids) => _inParts('/api/trash/purge', ids);

  Future<StorageInfo> storage() async {
    final info = StorageInfo.fromJson(await api.get('/api/admin/storage'));
    trashDays = info.trashDays;
    return info;
  }

  Future<int> _inParts(String path, List<String> ids) async {
    var changed = 0;
    for (var i = 0; i < ids.length; i += idsPerRequest) {
      final part = ids.sublist(i, (i + idsPerRequest).clamp(0, ids.length));
      final res = await api.post(path, {'ids': part});
      changed += (res['changed'] as num?)?.toInt() ?? 0;
    }
    return changed;
  }
}
