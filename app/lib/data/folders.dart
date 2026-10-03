/// Folders, as the person sees them: the library shows one of them or all, sending goes into one.
/// Only with a second folder is there anything to choose, and only then do the screens talk of
/// folders.
library;

import 'dart:convert';

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

/// Someone who sees a folder; [pending] for an invite nobody used yet.
typedef Seer = ({String id, String name, bool pending});

/// Who sees a folder: the admins, the members given it, and open invites for new people that
/// give it (an admin's invite sees every folder).
List<Seer> whoSees(People people, String folder) => [
      for (final u in people.users)
        if (u.isAdmin) (id: u.id, name: u.name, pending: false),
      for (final u in people.users)
        if (!u.isAdmin && u.folders.contains(folder)) (id: u.id, name: u.name, pending: false),
      for (final i in people.invites)
        if (i.userId == null && (i.role == Role.admin || i.folders.contains(folder))) (id: i.id, name: i.name, pending: true),
    ];

/// The folders an invite for a new member gives at first: the one the library shows, else the
/// oldest.
List<String> inviteDefault(List<FolderInfo> list, String? shown) => [?(validShown(list, shown) ?? list.firstOrNull?.id)];

/// What all the folders hold together.
({int files, int bytes}) allTotals(List<FolderInfo> list) =>
    (files: list.fold(0, (s, f) => s + f.files), bytes: list.fold(0, (s, f) => s + f.bytes));

/// The folders this person sees, for all the app's screens. They stay on the phone, so sending
/// knows where to go before the server answers, or without it; so does the folder the library
/// shows. The one chosen for sending lasts while the app runs.
class FolderStore extends ChangeNotifier {
  FolderStore({required this.library, required this.platform});

  final LibraryRepository library;
  final Platform platform;

  /// null until they are fetched.
  List<FolderInfo>? list;
  bool failed = false;
  String? _stored;
  String? _sendChoice;
  String? _kept; // the folders as the phone keeps them
  Future<void>? _loading;

  bool get choices => hasChoices(list);

  /// Files shared into the app wait for the person to say where they go: there's a folder to
  /// choose, or none known.
  bool get asksForFolder => choices || sendTo == null;

  /// The folder the library shows; null for all.
  FolderInfo? get shown => byId(validShown(list, _stored));

  /// The folder sending goes into: chosen on the Send tab, or the one the library shows.
  FolderInfo? get sendTo => list == null ? null : byId(sendTarget(list!, validShown(list, _stored), _sendChoice));

  FolderInfo? byId(String? id) => id == null ? null : list?.where((f) => f.id == id).firstOrNull;

  /// Reads the folders and the one the library showed, as they were last time.
  Future<void> start() async {
    _stored = await platform.readSecret('folder');
    _kept = await platform.readSecret('folders');
    try {
      if (_kept != null) list = FolderInfo.listFromJson(jsonDecode(_kept!) as Json);
    } catch (_) {
      // fetched again soon anyway
    }
  }

  /// Fetches the folders again; a fetch already under way is reused.
  Future<void> load() => _loading ??= () async {
        try {
          final json = await library.folders();
          list = FolderInfo.listFromJson(json);
          failed = false;
          final kept = jsonEncode(json);
          if (kept != _kept) await platform.writeSecret('folders', _kept = kept);
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
    _kept = null;
    await platform.writeSecret('folders', null);
    await show(null);
  }
}
