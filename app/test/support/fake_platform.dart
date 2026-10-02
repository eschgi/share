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

  @override
  Future<String> download(List<FileInfo> files) async {
    downloads.add(files);
    return 'batch-${downloads.length}';
  }

  @override
  Future<void> cancelDownloads(String batch) async {}

  @override
  Stream<TransferState> get transfers => transferEvents.stream;

  @override
  Future<Set<String>> savedIds(Iterable<String> ids) async => ids.where(saved.contains).toSet();

  @override
  Future<void> shareFiles(List<FileInfo> files) async {
    if (openError != null) throw openError!;
    shared.add(files);
  }

  @override
  Future<void> openFile(FileInfo file) async {
    if (openError != null) throw openError!;
    opened.add('file:${file.id}');
  }

  /// What play answers: a stream from the server unless a test says otherwise.
  PlaySource? Function(FileInfo file)? playSource;
  final played = <String>[];
  bool screenOn = false;

  @override
  Future<PlaySource?> play(FileInfo file) async {
    if (openError != null) throw openError!;
    played.add(file.id);
    return playSource != null
        ? playSource!(file)
        : PlaySource(Uri.parse('https://share.example.com/api/files/${file.id}/content'), headers: const {'Authorization': 'Bearer shd_x'});
  }

  @override
  Future<void> keepScreenOn(bool on) async => screenOn = on;

  @override
  Future<String> cacheDir() async => '';

  /// What pickAndSend gives back, and what was asked.
  String? nextPick = 'up-1';
  final picks = <(PickWhat, SendAuth)>[];
  final cancelledUploads = <String>[];
  final resumed = <SendAuth>[];
  final uploadEvents = StreamController<UploadState>.broadcast();

  @override
  Future<String?> pickAndSend(PickWhat what, {SendAuth auth = SendAuth.device}) async {
    picks.add((what, auth));
    return nextPick;
  }

  @override
  Future<void> cancelUpload(String batch) async => cancelledUploads.add(batch);

  @override
  Future<void> resumeUploads(SendAuth auth) async => resumed.add(auth);

  @override
  Stream<UploadState> get uploads => uploadEvents.stream;

  /// Files shared into the app, waiting; what was done with them.
  int sharedWaiting = 0;
  final sharedSent = <SendAuth>[];
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
  Future<String?> sendShared(SendAuth auth) async {
    if (sharedWaiting == 0) return null;
    sharedWaiting = 0;
    sharedSent.add(auth);
    return 'up-shared';
  }

  @override
  Future<void> dropShared() async {
    sharedWaiting = 0;
    sharedDropped++;
  }
}
