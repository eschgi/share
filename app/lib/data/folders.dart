/// Folders, as the person sees them: the library shows one of them or all, sending goes into one.
/// Only with a second folder is there anything to choose, and only then do the screens talk of
/// folders.
library;

import 'package:flutter/foundation.dart';

import 'library.dart';
import 'models.dart';
import 'platform.dart';

/// There is a folder to choose: the person sees more than one.
bool hasChoices(List<FolderInfo>? list) => (list?.length ?? 0) > 1;

/// The folder the library shows: the one stored, while the person still sees it; null for all.
String? validShown(List<FolderInfo>? list, String? stored) => stored != null && (list?.any((f) => f.id == stored) ?? false) ? stored : null;

/// The folder sending goes into: the one chosen for it, else the one the library shows, else the
/// oldest. null while the person sees no folder.
String? sendTarget(List<FolderInfo> list, String? shown, String? chosen) {
  for (final id in [chosen, shown]) {
    if (id != null && list.any((f) => f.id == id)) return id;
  }
  return list.firstOrNull?.id;
}

/// What all the folders hold together.
({int files, int bytes}) allTotals(List<FolderInfo> list) =>
    (files: list.fold(0, (s, f) => s + f.files), bytes: list.fold(0, (s, f) => s + f.bytes));

/// The folders this person sees, for all the app's screens. The folder the library shows stays
/// on the phone; the one chosen for sending lasts while the app runs.
class FolderStore extends ChangeNotifier {
  FolderStore({required this.library, required this.platform});

  final LibraryRepository library;
  final Platform platform;

  /// null until they are fetched.
  List<FolderInfo>? list;
  bool failed = false;
  String? _stored;
  String? _sendChoice;
  Future<void>? _loading;

  bool get choices => hasChoices(list);

  /// The folder the library shows; null for all.
  FolderInfo? get shown => byId(validShown(list, _stored));

  /// The folder sending goes into: chosen on the Send tab, or the one the library shows.
  FolderInfo? get sendTo => list == null ? null : byId(sendTarget(list!, validShown(list, _stored), _sendChoice));

  FolderInfo? byId(String? id) => id == null ? null : list?.where((f) => f.id == id).firstOrNull;

  /// Reads the folder the library showed last time.
  Future<void> start() async => _stored = await platform.readSecret('folder');

  /// Fetches the folders again; a fetch already under way is reused.
  Future<void> load() => _loading ??= () async {
        try {
          list = await library.folders();
          failed = false;
        } catch (_) {
          failed = true;
        } finally {
          _loading = null;
          notifyListeners();
        }
      }();

  /// The library shows this folder from now on; null shows all.
  Future<void> show(String? id) async {
    _stored = id;
    notifyListeners();
    await platform.writeSecret('folder', id);
  }

  /// Sending goes into this folder from now on.
  void chooseSendTo(String id) {
    _sendChoice = id;
    notifyListeners();
  }

  /// A folder shown or chosen is gone, or the person doesn't see it any more: all folders.
  Future<void> gone(String id) async {
    if (_sendChoice == id) _sendChoice = null;
    if (_stored == id) await show(null);
    await load();
  }

  /// Signed out: the next person starts with all folders.
  Future<void> clear() async {
    list = null;
    _sendChoice = null;
    await show(null);
  }
}
