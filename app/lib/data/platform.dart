/// What the app needs from Android. Everything that moves bytes or has to work without
/// Flutter lives in Kotlin: secrets, the route to the server, downloads. Tests replace
/// [Platform] with a fake.
library;

import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart';

import 'models.dart';
import 'server.dart';

/// Which address the app uses now, and why.
enum ServerRoute { local, public }

enum RouteReason { none, noLocal, unreachable, wrongCertificate, otherServer }

class RouteStatus {
  const RouteStatus(this.route, {this.reason = RouteReason.none, this.millis, this.checking = false});

  factory RouteStatus.fromMap(Map<Object?, Object?> m) => RouteStatus(
        m['route'] == 'local' ? ServerRoute.local : ServerRoute.public,
        reason: RouteReason.values.asNameMap()[m['reason']] ?? RouteReason.none,
        millis: (m['millis'] as num?)?.toInt(),
        checking: m['checking'] == true,
      );

  static const public = RouteStatus(ServerRoute.public, reason: RouteReason.noLocal);

  final ServerRoute route;
  final RouteReason reason;
  final int? millis; // how long the local address took to answer
  final bool checking;

  bool get isLocal => route == ServerRoute.local;
}

/// A batch of downloads, as the Kotlin engine reports it.
class TransferState {
  const TransferState({
    required this.batch,
    required this.running,
    required this.total,
    required this.done,
    required this.failed,
    required this.skipped,
    required this.bytesTotal,
    required this.bytesDone,
    required this.media,
    required this.documents,
    this.local = false,
    this.noSpace = false,
  });

  factory TransferState.fromMap(Map<Object?, Object?> m) {
    int n(String k) => (m[k] as num?)?.toInt() ?? 0;
    return TransferState(
      batch: m['batch'] as String? ?? '',
      running: m['running'] == true,
      total: n('total'),
      done: n('done'),
      failed: n('failed'),
      skipped: n('skipped'),
      bytesTotal: n('bytes_total'),
      bytesDone: n('bytes_done'),
      media: n('media'),
      documents: n('documents'),
      local: m['local'] == true,
      noSpace: m['no_space'] == true,
    );
  }

  final String batch;
  final bool running;
  final int total, done, failed, skipped;
  final int bytesTotal, bytesDone;
  final int media, documents; // where they go: the Share album, or Downloads/Share
  final bool local; // over the local address
  final bool noSpace;
}

enum PickWhat { media, documents }

/// How files are sent: with the phone's key (signed in), or with a PIN.
enum SendAuth { device, pin }

/// One file of an upload batch.
class UploadItemState {
  const UploadItemState({required this.seq, required this.name, required this.size, required this.kind, required this.state, this.bytes = 0});

  factory UploadItemState.fromMap(Map<Object?, Object?> m) => UploadItemState(
        seq: (m['seq'] as num?)?.toInt() ?? 0,
        name: m['name'] as String? ?? '',
        size: (m['size'] as num?)?.toInt() ?? 0,
        kind: FileKind.parse(m['kind']),
        state: m['state'] as String? ?? 'queued',
        bytes: (m['bytes'] as num?)?.toInt() ?? 0,
      );

  final int seq;
  final String name;
  final int size;
  final FileKind kind;
  final String state; // queued, done, failed, lost, cancelled
  final int bytes;

  bool get done => state == 'done';
  bool get queued => state == 'queued';
}

/// A batch of uploads, as the Kotlin engine reports it; [items] are a few of its files: the
/// last one sent, the one on its way and the next ones.
class UploadState {
  const UploadState({
    required this.batch,
    required this.auth,
    required this.running,
    this.paused,
    required this.total,
    required this.done,
    this.failed = 0,
    this.lost = 0,
    required this.bytesTotal,
    required this.bytesDone,
    this.etaSeconds,
    this.local = false,
    this.items = const [],
  });

  factory UploadState.fromMap(Map<Object?, Object?> m) {
    int n(String k) => (m[k] as num?)?.toInt() ?? 0;
    return UploadState(
      batch: m['batch'] as String? ?? '',
      auth: m['auth'] == 'pin' ? SendAuth.pin : SendAuth.device,
      running: m['running'] == true,
      paused: m['paused'] as String?,
      total: n('total'),
      done: n('done'),
      failed: n('failed'),
      lost: n('lost'),
      bytesTotal: n('bytes_total'),
      bytesDone: n('bytes_done'),
      etaSeconds: (m['eta_seconds'] as num?)?.toInt(),
      local: m['local'] == true,
      items: [for (final i in (m['items'] as List? ?? const [])) if (i is Map) UploadItemState.fromMap(i)],
    );
  }

  final String batch;
  final SendAuth auth;
  final bool running;
  final String? paused; // pin_ended, signed_out or user
  final int total, done, failed, lost;
  final int bytesTotal, bytesDone;
  final int? etaSeconds;
  final bool local;
  final List<UploadItemState> items;

  /// The file on its way, counted from 1, as in "Sending 12 of 40".
  int get current => (done + failed + lost + 1).clamp(1, total == 0 ? 1 : total);
}

class ScanUnavailable implements Exception {
  const ScanUnavailable();
}

/// Sharing or opening a file didn't work: it couldn't be fetched, or no app takes it.
class OpenFailed implements Exception {
  const OpenFailed({this.noApp = false});
  final bool noApp;
}

/// The batch whose progress [Platform.transfers] reports while files are fetched for
/// [Platform.shareFiles] and [Platform.openFile]; [Platform.cancelDownloads] stops it.
const fetchBatch = 'fetch';

abstract class Platform {
  Future<String?> readSecret(String key);
  Future<void> writeSecret(String key, String? value);

  Future<ServerConfig?> loadServer();
  Future<void> saveServer(ServerConfig? config);

  /// The server this phone sends to with a PIN, apart from the one it may be signed in to.
  Future<ServerConfig?> loadPinServer();
  Future<void> savePinServer(ServerConfig? config);

  /// The current route; with [check], the local address is probed again first.
  Future<RouteStatus> route({bool check = false});
  Stream<RouteStatus> get routes;

  Future<String> deviceName();

  /// The link the app was opened with, once; later links arrive on [links].
  Future<String?> initialLink();
  Stream<String> get links;

  /// Scans a QR code; null when the person backed out. Throws [ScanUnavailable] without
  /// Google Play services.
  Future<String?> scanCode();

  Future<void> openUrl(String url);

  /// Hands text to another app (a messenger, mail), e.g. an invite link.
  Future<void> shareText(String text);

  /// Starts saving files to the phone and returns the batch id.
  Future<String> download(List<FileInfo> files);
  Future<void> cancelDownloads(String batch);
  Stream<TransferState> get transfers;

  /// Which of these files are already on the phone (saved before and still there).
  Future<Set<String>> savedIds(Iterable<String> ids);

  /// Shares files with another app, or opens one (a video in the system player), after
  /// fetching them into the cache. Both throw [OpenFailed].
  Future<void> shareFiles(List<FileInfo> files);
  Future<void> openFile(FileInfo file);

  Future<String> cacheDir();

  /// Opens the picker and sends what was picked in the background; the batch, or null if
  /// nothing was picked. Files that had to be picked again go on in their old batch.
  Future<String?> pickAndSend(PickWhat what, {SendAuth auth = SendAuth.device});
  Future<void> cancelUpload(String batch);

  /// What waited for a new PIN or a sign-in goes on.
  Future<void> resumeUploads(SendAuth auth);
  Stream<UploadState> get uploads;
}

/// The real platform: MethodChannel com.eschgi.share/platform, and one event channel for
/// routes, links and transfers.
class ChannelPlatform implements Platform {
  ChannelPlatform() {
    _events.receiveBroadcastStream().listen((e) {
      if (e is! Map) return;
      switch (e['type']) {
        case 'route':
          _routes.add(RouteStatus.fromMap(e));
        case 'link':
          if (e['url'] is String) _links.add(e['url'] as String);
        case 'transfer':
          _transfers.add(TransferState.fromMap(e));
        case 'upload':
          final u = UploadState.fromMap(e);
          _lastUploads[u.batch] = u;
          _uploads.add(u);
      }
    }, onError: (Object _) {});
  }

  static const _channel = MethodChannel('com.eschgi.share/platform');
  static const _events = EventChannel('com.eschgi.share/events');

  final _routes = StreamController<RouteStatus>.broadcast();
  final _links = StreamController<String>.broadcast();
  final _transfers = StreamController<TransferState>.broadcast();
  final _uploads = StreamController<UploadState>.broadcast();
  final _lastUploads = <String, UploadState>{};

  Future<T?> _invoke<T>(String method, [Object? args]) async {
    try {
      return await _channel.invokeMethod<T>(method, args);
    } on MissingPluginException {
      return null;
    }
  }

  /// For calls whose failure the app gets over: a secret that can't be read (the KeyStore
  /// lost its key) is a missing one, and the phone signs in again.
  Future<T?> _soft<T>(String method, [Object? args]) async {
    try {
      return await _invoke<T>(method, args);
    } on PlatformException catch (e) {
      debugPrint('platform $method failed: ${e.message}');
      return null;
    }
  }

  @override
  Future<String?> readSecret(String key) => _soft<String>('secret.read', {'key': key});

  @override
  Future<void> writeSecret(String key, String? value) => _soft('secret.write', {'key': key, 'value': value});

  @override
  Future<ServerConfig?> loadServer() async {
    final raw = await _soft<String>('server.load');
    if (raw == null || raw.isEmpty) return null;
    return ServerConfig.fromJson(jsonDecode(raw) as Json);
  }

  @override
  Future<void> saveServer(ServerConfig? config) =>
      _soft('server.save', {'json': config == null ? null : jsonEncode(config.toJson())});

  @override
  Future<ServerConfig?> loadPinServer() async {
    final raw = await _soft<String>('server.load', {'slot': 'pin'});
    if (raw == null || raw.isEmpty) return null;
    return ServerConfig.fromJson(jsonDecode(raw) as Json);
  }

  @override
  Future<void> savePinServer(ServerConfig? config) =>
      _soft('server.save', {'json': config == null ? null : jsonEncode(config.toJson()), 'slot': 'pin'});

  @override
  Future<RouteStatus> route({bool check = false}) async {
    final m = await _soft<Map<Object?, Object?>>('route.get', {'check': check});
    return m == null ? RouteStatus.public : RouteStatus.fromMap(m);
  }

  @override
  Stream<RouteStatus> get routes => _routes.stream;

  @override
  Future<String> deviceName() async => await _soft<String>('device.name') ?? 'Phone';

  @override
  Future<String?> initialLink() => _soft<String>('link.initial');

  @override
  Stream<String> get links => _links.stream;

  @override
  Future<String?> scanCode() async {
    try {
      return await _channel.invokeMethod<String>('scan');
    } on PlatformException catch (e) {
      if (e.code == 'unavailable') throw const ScanUnavailable();
      return null;
    } on MissingPluginException {
      throw const ScanUnavailable();
    }
  }

  @override
  Future<void> openUrl(String url) => _soft('url.open', {'url': url});

  @override
  Future<void> shareText(String text) => _soft('text.share', {'text': text});

  @override
  Future<String> download(List<FileInfo> files) async =>
      await _invoke<String>('transfer.download', {'files': jsonEncode([for (final f in files) f.toJson()])}) ?? '';

  @override
  Future<void> cancelDownloads(String batch) => _soft('transfer.cancel', {'batch': batch});

  @override
  Stream<TransferState> get transfers => _transfers.stream;

  @override
  Future<Set<String>> savedIds(Iterable<String> ids) async {
    final list = await _soft<List<Object?>>('transfer.saved', {'ids': ids.toList()});
    return {for (final id in list ?? const []) if (id is String) id};
  }

  @override
  Future<void> shareFiles(List<FileInfo> files) =>
      _opening(() => _invoke('file.share', {'files': jsonEncode([for (final f in files) f.toJson()])}));

  @override
  Future<void> openFile(FileInfo file) => _opening(() => _invoke('file.open', {'file': jsonEncode(file.toJson())}));

  Future<void> _opening(Future<void> Function() call) async {
    try {
      await call();
    } on PlatformException catch (e) {
      throw OpenFailed(noApp: e.code == 'no_app');
    }
  }

  @override
  Future<String> cacheDir() async => await _soft<String>('cache.dir') ?? '';

  @override
  Future<String?> pickAndSend(PickWhat what, {SendAuth auth = SendAuth.device}) =>
      _invoke<String>('upload.pick', {'what': what.name, 'auth': auth.name});

  @override
  Future<void> cancelUpload(String batch) => _soft('upload.cancel', {'batch': batch});

  @override
  Future<void> resumeUploads(SendAuth auth) => _soft('upload.resume', {'auth': auth.name});

  /// Starts with how each batch stood last: the send screen may open long after the change.
  @override
  Stream<UploadState> get uploads => Stream.multi((listener) {
        for (final u in _lastUploads.values) {
          listener.add(u);
        }
        final sub = _uploads.stream.listen(listener.add, onError: listener.addError);
        listener.onCancel = sub.cancel;
      });
}
