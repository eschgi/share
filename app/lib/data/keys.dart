/// End-to-end encryption on this phone, as the screens see it (docs/e2ee-plan.md). The keys live
/// in Kotlin, where downloads and uploads use them without Flutter; this keeps what Kotlin said
/// last, and asks for the admins' setting for new folders.
library;

import 'dart:async';

import 'package:flutter/foundation.dart';

import 'api.dart';
import 'platform.dart';

class KeysRepository extends ChangeNotifier {
  KeysRepository({required this.platform, required this.api}) {
    _changes = platform.keyChanges.listen(_set);
  }

  final Platform platform;
  final Api api;
  late final StreamSubscription<KeysState> _changes;
  KeysState _state = const KeysState();

  KeysState get state => _state;

  void _set(KeysState s) {
    _state = s;
    notifyListeners();
  }

  /// Opens the keys again and does what is due; with [password] right after signing in with it.
  /// Never throws: the state says how it went.
  Future<void> sync({String? password}) async {
    try {
      _set(await platform.syncKeys(password: password));
    } on KeysException {
      // the state's event says so
    }
  }

  /// Checks in with the server: seals the person's key for a phone or browser that waits, and
  /// opens what was sealed for this phone. Every half minute while the app is in front, and when
  /// it comes back; quietly, so the screens don't flicker through loading. Never throws.
  Future<void> checkIn() async {
    try {
      _set(await platform.syncKeys(quiet: true));
    } on KeysException {
      // no answer this time: the next check-in asks again
    }
  }

  /// A phone without its keys that waits for another one, while there are encrypted folders.
  bool get waiting => _state.status == KeysStatus.waiting && _state.encryptedFolders > 0;

  /// Admins: whether new folders are encrypted from the start.
  Future<bool> newFoldersEncrypted() async => (await api.get('/api/admin/settings'))['new_folders_encrypted'] == true;

  Future<void> setNewFoldersEncrypted(bool on) => api.put('/api/admin/settings', {'new_folders_encrypted': on});

  @override
  void dispose() {
    _changes.cancel();
    super.dispose();
  }
}
