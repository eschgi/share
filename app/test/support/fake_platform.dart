import 'dart:async';

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
  String? scanned;
  bool scanWorks = true;
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
    if (!scanWorks) throw const ScanUnavailable();
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
}
