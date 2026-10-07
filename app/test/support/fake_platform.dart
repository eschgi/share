import 'dart:async';
import 'dart:typed_data';

import 'package:share_app/data/models.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/data/server.dart';

/// Android, as far as the Dart code can tell: secrets and settings in memory, a route that
/// tests set, and a record of what was asked for.
class FakePlatform implements Platform {
  final secrets = <String, String>{};
  ServerConfig? server;
  RouteStatus current = RouteStatus.public;
  int routeChecks = 0;

  /// What a check finds, e.g. the local address after "checking"; without it, [current].
  RouteStatus? afterCheck;
  final opened = <String>[];
  final downloads = <List<FileInfo>>[];
  final shared = <List<FileInfo>>[];
  Set<String> saved = {};
  /// What a scan gives: [scanned], or [scanProblem] when the scanner doesn't open; with
  /// [scanWait], only once that completes.
  String? scanned;
  ScanProblem? scanProblem;
  Future<void>? scanWait;
  int scans = 0;
  String? launchLink;

  /// What shareFiles and openFile throw, if anything.
  OpenFailed? openError;

  final routeEvents = StreamController<RouteStatus>.broadcast();
  final linkEvents = StreamController<String>.broadcast();
  final transferEvents = StreamController<TransferState>.broadcast();

  @override
  Future<String?> readSecret(String key) async => secrets[key];

  @override
  Future<void> writeSecret(String key, String? value) async =>
      value == null ? secrets.remove(key) : secrets[key] = value;

  @override
  Future<ServerConfig?> loadServer() async => server;

  ServerConfig? pinServer;

  @override
  Future<ServerConfig?> loadPinServer() async => pinServer;

  @override
  Future<void> savePinServer(ServerConfig? config) async => pinServer = config;

  @override
  Future<void> saveServer(ServerConfig? config) async => server = config;

  @override
  Future<RouteStatus> route({bool check = false}) async {
    if (check) {
      routeChecks++;
      if (afterCheck != null) current = afterCheck!;
    }
    return current;
  }

  @override
  Stream<RouteStatus> get routes => routeEvents.stream;

  @override
  Future<String> deviceName() async => 'Pixel 8';

  @override
  Future<String?> initialLink() async => launchLink;

  @override
  Stream<String> get links => linkEvents.stream;

  @override
  Future<String?> scanCode() async {
    scans++;
    if (scanWait != null) await scanWait;
    if (scanProblem != null) throw ScanUnavailable(scanProblem!);
    return scanned;
  }

  @override
  Future<void> openUrl(String url) async => opened.add(url);

  final sharedTexts = <String>[];

  @override
  Future<void> shareText(String text) async => sharedTexts.add(text);

  final copiedSecrets = <String>[];

  @override
  Future<void> copySecret(String text) async => copiedSecrets.add(text);

  /// Whose key each download, share, open and play went with.
  final auths = <SendAuth>[];

  @override
  Future<String> download(List<FileInfo> files, {SendAuth auth = SendAuth.device}) async {
    downloads.add(files);
    auths.add(auth);
    return 'batch-${downloads.length}';
  }

  @override
  Future<void> cancelDownloads(String batch) async {}

  @override
  Stream<TransferState> get transfers => transferEvents.stream;

  @override
  Future<Set<String>> savedIds(Iterable<String> ids) async => ids.where(saved.contains).toSet();

  @override
  Future<void> shareFiles(List<FileInfo> files, {SendAuth auth = SendAuth.device}) async {
    if (openError != null) throw openError!;
    shared.add(files);
    auths.add(auth);
  }

  @override
  Future<void> openFile(FileInfo file, {SendAuth auth = SendAuth.device}) async {
    if (openError != null) throw openError!;
    opened.add('file:${file.id}');
    auths.add(auth);
  }

  /// What play answers: a stream from the server unless a test says otherwise.
  PlaySource? Function(FileInfo file)? playSource;
  final played = <String>[];
  bool screenOn = false;

  @override
  Future<PlaySource?> play(FileInfo file, {SendAuth auth = SendAuth.device}) async {
    if (openError != null) throw openError!;
    played.add(file.id);
    auths.add(auth);
    return playSource != null
        ? playSource!(file)
        : PlaySource(Uri.parse('https://share.example.com/api/files/${file.id}/content'), headers: const {'Authorization': 'Bearer shd_x'});
  }

  @override
  Future<void> keepScreenOn(bool on) async => screenOn = on;

  @override
  Future<({String name, int code})> appVersion() async => (name: '0.3.0', code: 3);

  /// Where the app may keep files; none unless a test gives one.
  String cache = '';

  @override
  Future<String> cacheDir() async => cache;

  /// What pickAndSend gives back, and what was asked: what, how, and into which folder.
  String? nextPick = 'up-1';
  final picks = <(PickWhat, SendAuth)>[];
  final pickFolders = <String?>[];
  final cancelledUploads = <String>[];
  final resumed = <SendAuth>[];
  final resumedFolders = <String?>[];
  final uploadEvents = StreamController<UploadState>.broadcast();

  @override
  Future<String?> pickAndSend(PickWhat what, {SendAuth auth = SendAuth.device, String? folder}) async {
    picks.add((what, auth));
    pickFolders.add(folder);
    return nextPick;
  }

  @override
  Future<void> cancelUpload(String batch) async => cancelledUploads.add(batch);

  @override
  Future<void> resumeUploads(SendAuth auth, {String? folder}) async {
    resumed.add(auth);
    resumedFolders.add(folder);
  }

  @override
  Stream<UploadState> get uploads => uploadEvents.stream;

  /// Files shared into the app, waiting; what was done with them, and into which folder.
  int sharedWaiting = 0;
  final sharedSent = <SendAuth>[];
  final sharedFolders = <String?>[];
  int sharedDropped = 0;
  final sharedEvents = StreamController<SharedFiles>.broadcast();

  /// Another app shares files with the app.
  void share(int count, {int skipped = 0}) {
    sharedWaiting += count;
    sharedEvents.add(SharedFiles(count: sharedWaiting, skipped: skipped));
  }

  @override
  Future<int> sharedCount() async => sharedWaiting;

  @override
  Stream<SharedFiles> get sharedChanges => sharedEvents.stream;

  @override
  Future<String?> sendShared(SendAuth auth, {String? folder}) async {
    if (sharedWaiting == 0) return null;
    sharedWaiting = 0;
    sharedSent.add(auth);
    sharedFolders.add(folder);
    return 'up-shared';
  }

  @override
  Future<void> dropShared() async {
    sharedWaiting = 0;
    sharedDropped++;
  }

  // End-to-end encryption: the keys in Kotlin, as far as the screens can tell. Encrypted files
  // "decrypt" to their stored bytes, except in [sealedFolders], whose keys aren't here.
  KeysState keysState = const KeysState(status: KeysStatus.ready);
  final keysEvents = StreamController<KeysState>.broadcast();
  Set<String> sealedFolders = {};

  /// The folders that have a key: encrypted now, or before.
  Set<String> encryptedFolders = {};

  /// The passwords syncKeys got (null for none), how often it checked in quietly, and what else
  /// was asked.
  final keySyncs = <String?>[];
  int keyCheckIns = 0;
  final keyCalls = <String>[];
  String recoveryCode = '7SEN-4M38-3QV2-Z38M-JEGM-QXC8-FHPD-HJ4M';
  final linkSecret = 'S' * 43;

  void setKeys(KeysState s) {
    keysState = s;
    keysEvents.add(s);
  }

  /// As Kotlin, every sync says where the keys stand.
  @override
  Future<KeysState> syncKeys({String? password, bool quiet = false}) async {
    if (quiet) {
      keyCheckIns++;
    } else {
      keySyncs.add(password);
    }
    keysEvents.add(keysState);
    return keysState;
  }

  @override
  Stream<KeysState> get keyChanges => keysEvents.stream;

  @override
  Future<Uint8List> openThumb(FileInfo file, Uint8List sealed, {SendAuth auth = SendAuth.device}) async {
    if (sealedFolders.contains(file.folder)) throw const KeysException('sealed');
    return sealed;
  }

  @override
  Future<Uint8List> decryptFile(FileInfo file, Uint8List data, {SendAuth auth = SendAuth.device}) async {
    if (sealedFolders.contains(file.folder)) throw const KeysException('sealed');
    return data;
  }

  @override
  Future<Json> encryptFolder(FolderInfo folder) async {
    keyCalls.add('encrypt ${folder.id}');
    return {'id': folder.id, 'encrypted': true, 'key_version': folder.keyVersion ?? 1};
  }

  @override
  Future<String> makeRecovery() async {
    keyCalls.add('recovery');
    setKeys(KeysState(status: keysState.status, hasRecovery: true, encryptedFolders: keysState.encryptedFolders, open: keysState.open));
    return recoveryCode;
  }

  @override
  Future<int> useRecoveryCode(String code) async {
    keyCalls.add('use $code');
    if (code.replaceAll('-', '').toUpperCase() != recoveryCode.replaceAll('-', '')) throw const KeysException('sealed');
    return 2;
  }

  @override
  Future<KeysState> startOver() async {
    keyCalls.add('start over');
    return keysState;
  }

  /// As Kotlin: an ask that is settled leaves the state; one shown or hidden stays.
  KeysState _ask(String call, KeyAsk ask, {bool settled = false}) {
    keyCalls.add('$call ${ask.kind} ${ask.id}');
    final s = keysState;
    setKeys(KeysState(
      status: s.status,
      hasRecovery: s.hasRecovery,
      encryptedFolders: s.encryptedFolders,
      open: s.open,
      asks: [for (final a in s.asks) if (!settled || a.kind != ask.kind || a.id != ask.id) a],
      codes: s.codes,
      waitsForFolders: s.waitsForFolders,
      pace: s.pace,
    ));
    return keysState;
  }

  @override
  Future<KeysState> allowAsk(KeyAsk ask) async => _ask('allow', ask, settled: true);

  @override
  Future<KeysState> denyAsk(KeyAsk ask) async => _ask('deny', ask, settled: true);

  @override
  Future<KeysState> showAsk(KeyAsk ask) async => _ask('show', ask);

  @override
  Future<KeysState> hideAsk(KeyAsk ask) async => _ask('hide', ask);

  @override
  Future<String?> passwordLock(String password) async => keysState.ready ? 'lock-$password' : null;

  /// The root this phone trusts, locked with a link's secret; null while it trusts none.
  String? lockedRoot = 'locked-root';

  /// The root's fingerprint in a PIN link (22 characters).
  final rootFingerprint = 'R' * 22;

  @override
  Future<({String secret, List<Json> keys, String? root})> inviteKeys(List<String>? folders) async {
    keyCalls.add('invite ${folders?.join(',')}');
    final open = [for (final f in folders ?? encryptedFolders.toList()) if (encryptedFolders.contains(f) && !sealedFolders.contains(f)) f];
    return (secret: linkSecret, keys: [for (final f in open) {'folder': f, 'version': 1, 'locked': 'locked-$f'}], root: lockedRoot);
  }

  @override
  Future<({String secret, String locked, String? root})?> personKeyForInvite() async =>
      keysState.ready ? (secret: linkSecret, locked: 'locked-person', root: lockedRoot) : null;

  @override
  Future<({String secret, Json body})?> pinSecret(String folder) async {
    if (!encryptedFolders.contains(folder)) return null;
    keyCalls.add('pin $folder');
    return (secret: linkSecret, body: {'locked': 'locked-secret', 'version': 1, 'root': 'locked-root', 'keys': const []});
  }

  @override
  Future<String?> pinLinkSecret(String folder, String locked, int version) async => sealedFolders.contains(folder) ? null : linkSecret;

  @override
  Future<String?> pinLinkRoot(String folder) async => encryptedFolders.contains(folder) ? rootFingerprint : null;

  @override
  Future<Json> switchOff(FolderInfo folder) async {
    keyCalls.add('switch off ${folder.id}');
    return {'id': folder.id, 'encrypted': false, 'key_version': folder.keyVersion};
  }

  /// Making folders and switching them needs the recovery key here (Kotlin's needs_root).
  bool lacksRoot = false;

  @override
  Future<Json> newFolderBody(String name) async {
    if (lacksRoot) throw const KeysException('needs_root');
    keyCalls.add('new folder $name');
    return {'name': name, 'plain_signature': 'signed-$name'};
  }

  @override
  Future<Json> renameBody(FolderInfo folder, String name) async {
    keyCalls.add('rename ${folder.id} $name');
    return {'name': name, if (!folder.encrypted) 'plain_signature': 'signed-$name'};
  }

  @override
  Future<List<Json>> moveKeys(List<FileInfo> files, String target) async {
    keyCalls.add('move ${files.map((f) => f.id).join(',')} $target');
    return [for (final f in files) {'id': f.id, 'version': 1, 'key': 'moved-${f.id}'}];
  }

  final invitesOpened = <(String?, List<Json>, String?)>[];

  @override
  Future<KeysState> keysFromInvite(String? secret, List<Json> keys, String? root) async {
    invitesOpened.add((secret, keys, root));
    return keysState;
  }

  /// What each PIN's link carried, as pinLink kept it: (secret, root); (null, null) forgets.
  final pinLinks = <(String?, String?)>[];

  @override
  Future<int> pinLink({String? secret, String? root}) async {
    pinLinks.add((secret, root));
    return secret == null ? 0 : 1;
  }

  @override
  Future<void> forgetPinKeys() async => pinLinks.add((null, null));
}
