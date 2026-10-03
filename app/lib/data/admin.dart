/// What admins do (screens 17–21, 40–41): PINs, people and invites, folders, deleting files and
/// the trash.
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

  /// A PIN that sends into [folder], and with [showsFolder] also shows it. Throws ApiException
  /// pin_taken or pin_format for a [code] that can't be had, folder_gone for a folder that is no
  /// more.
  Future<PinInfo> createPin(PinKind kind, {String? code, String? folder, bool showsFolder = false}) async => PinInfo.fromJson(await api.post('/api/pins', {
        'kind': kind.wire,
        if (code != null && code.isNotEmpty) 'code': code,
        'folder': ?folder,
        if (showsFolder) 'shows_folder': true,
      }));

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

  /// An invite for someone new; a member gets [folders], an admin sees every folder.
  Future<NewInvite> invite(String name, Role role, {List<String> folders = const []}) async => NewInvite.fromJson(
      await api.post('/api/invites', {'name': name.trim(), 'role': role.name, if (role == Role.member) 'folders': folders}));

  /// An invite that adds a phone for someone who has an account.
  Future<NewInvite> invitePhone(String userId) async => NewInvite.fromJson(await api.post('/api/users/$userId/invites'));

  Future<void> withdrawInvite(String id) => api.delete('/api/invites/$id');

  /// A new folder, which only admins see at first. Throws ApiException folder_name_taken, or
  /// bad_request for a name that won't do.
  Future<FolderInfo> createFolder(String name) async => FolderInfo.fromJson(await api.post('/api/folders', {'name': name.trim()}));

  /// Also throws ApiException folder_busy while the files of the last rename are still moving.
  Future<FolderInfo> renameFolder(String id, String name) async => FolderInfo.fromJson(await api.patch('/api/folders/$id', {'name': name.trim()}));

  /// Its files go to Recently deleted and its PINs end; returns how many files went. Throws
  /// ApiException last_folder for the last one.
  Future<int> deleteFolder(String id) async => ((await api.delete('/api/folders/$id'))['changed'] as num?)?.toInt() ?? 0;

  /// Gives someone a folder, or takes it away; admins see every folder anyway.
  Future<void> setFolderPerson(String folder, String userId, {required bool sees}) =>
      sees ? api.put('/api/folders/$folder/people/$userId', const {}) : api.delete('/api/folders/$folder/people/$userId');

  /// The same for an open invite for a new member.
  Future<void> setFolderInvite(String folder, String inviteId, {required bool gets}) =>
      gets ? api.put('/api/folders/$folder/invites/$inviteId', const {}) : api.delete('/api/folders/$folder/invites/$inviteId');

  /// Moves files of the library into [folder]; who sees them changes with it. Returns how many
  /// moved: files already in it are left alone.
  Future<int> moveFiles(List<String> ids, String folder) => _inParts('/api/files/move', ids, {'folder': folder});

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

  Future<int> _inParts(String path, List<String> ids, [Json more = const {}]) async {
    var changed = 0;
    for (var i = 0; i < ids.length; i += idsPerRequest) {
      final part = ids.sublist(i, (i + idsPerRequest).clamp(0, ids.length));
      final res = await api.post(path, {'ids': part, ...more});
      changed += (res['changed'] as num?)?.toInt() ?? 0;
    }
    return changed;
  }
}
