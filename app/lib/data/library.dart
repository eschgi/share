/// The library: days, pages of files, and thumbnails.
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:clock/clock.dart';

import 'api.dart';
import 'models.dart';
import 'platform.dart';

class LibraryFilter {
  const LibraryFilter({this.folder, this.kind, this.query = ''});

  final String? folder; // null: every folder the person sees
  final FileKind? kind;
  final String query;

  LibraryFilter withFolder(String? folder) => LibraryFilter(folder: folder, kind: kind, query: query);
  LibraryFilter withKind(FileKind? kind) => LibraryFilter(folder: folder, kind: kind, query: query);
  LibraryFilter withQuery(String query) => LibraryFilter(folder: folder, kind: kind, query: query);

  Map<String, String> toQuery({String? day}) => {
        'folder': ?folder,
        'kind': ?kind?.name,
        if (query.trim().isNotEmpty) 'q': query.trim(),
        'day': ?day,
      };

  @override
  bool operator ==(Object other) => other is LibraryFilter && other.folder == folder && other.kind == kind && other.query == query;

  @override
  int get hashCode => Object.hash(folder, kind, query);
}

class LibraryRepository {
  LibraryRepository({required this.api, required this.platform});

  final Api api;
  final Platform platform;

  /// The folders the person sees, the oldest first.
  Future<List<FolderInfo>> folders() async => FolderInfo.listFromJson(await api.get('/api/folders'));

  Future<LibraryOverview> overview(LibraryFilter filter) async =>
      LibraryOverview.fromJson(await api.get('/api/library', query: filter.toQuery()));

  Future<FilePage> page(LibraryFilter filter, {String? cursor, int limit = 200}) async => FilePage.fromJson(
        await api.get('/api/files', query: {...filter.toQuery(), 'limit': '$limit', 'cursor': ?cursor}),
      );

  Future<FileIds> ids(LibraryFilter filter, {String? day}) async =>
      FileIds.fromJson(await api.get('/api/files/ids', query: filter.toQuery(day: day)));

  /// Every file of one day, for selecting a whole day that isn't loaded yet.
  Future<List<FileInfo>> day(LibraryFilter filter, String day) async {
    final out = <FileInfo>[];
    String? cursor;
    do {
      final p = FilePage.fromJson(await api.get('/api/files',
          query: {...filter.toQuery(day: day), 'limit': '500', 'cursor': ?cursor}));
      out.addAll(p.files);
      cursor = p.nextCursor;
    } while (cursor != null);
    return out;
  }

  Directory? _originals;

  /// A photo at full size for the viewer, from the cache or the server. Only the last few are
  /// kept; videos and documents open through the platform instead.
  Future<File?> original(FileInfo f) async {
    final base = await platform.cacheDir();
    if (base.isEmpty) return null;
    final dir = _originals ??= await Directory('$base/originals').create(recursive: true);
    final file = File('${dir.path}/${f.id}');
    if (await file.exists() && await file.length() == f.size) return file;
    final bytes = await api.bytes('/api/files/${f.id}/content');
    await file.writeAsBytes(bytes, flush: false);
    final all = [await for (final e in dir.list()) if (e is File) e];
    if (all.length > 30) {
      final stats = [for (final e in all) (e, await e.stat())]..sort((a, b) => b.$2.modified.compareTo(a.$2.modified));
      for (final (old, _) in stats.skip(30)) {
        await old.delete();
      }
    }
    return file;
  }

  final _memory = <String, Uint8List>{}; // in insertion order: the oldest first
  int _memoryBytes = 0;
  static const _memoryLimit = 24 << 20; // decoded tiles are small; this is the JPEG bytes
  static const _diskLimit = 150 << 20;
  Directory? _disk;

  /// The thumbnail JPEG of a file, from memory, the disk cache or the server. The cache key
  /// includes updated_at, which changes when a better thumbnail arrives.
  Future<Uint8List?> thumb(FileInfo f) async {
    if (!f.hasThumb) return null;
    final key = '${f.id}-${f.updatedAt.millisecondsSinceEpoch}';
    final hit = _memory.remove(key);
    if (hit != null) return _memory[key] = hit;
    final dir = await _cacheDir();
    final file = dir == null ? null : File('${dir.path}/$key.jpg');
    Uint8List? bytes;
    if (file != null && await file.exists()) {
      bytes = await file.readAsBytes();
      file.setLastModified(clock.now()).ignore();
    } else {
      try {
        bytes = await api.bytes('/api/files/${f.id}/thumb');
      } on ApiException {
        return null;
      }
      if (file != null) {
        await file.writeAsBytes(bytes, flush: false);
        _trimDisk(dir!).ignore();
      }
    }
    _remember(key, bytes);
    return bytes;
  }

  void _remember(String key, Uint8List bytes) {
    _memory[key] = bytes;
    _memoryBytes += bytes.length;
    while (_memoryBytes > _memoryLimit && _memory.isNotEmpty) {
      _memoryBytes -= _memory.remove(_memory.keys.first)!.length;
    }
  }

  Future<Directory?> _cacheDir() async {
    if (_disk != null) return _disk;
    final base = await platform.cacheDir();
    if (base.isEmpty) return null;
    return _disk = await Directory('$base/thumbs').create(recursive: true);
  }

  int _written = 0;

  /// Keeps the disk cache under its limit, dropping the least recently used thumbnails. Checks
  /// once every few hundred new files, not on every write.
  Future<void> _trimDisk(Directory dir) async {
    if (++_written % 200 != 1) return;
    final files = [await for (final e in dir.list()) if (e is File) e];
    final stats = [for (final f in files) (f, await f.stat())];
    var total = stats.fold<int>(0, (s, e) => s + e.$2.size);
    if (total <= _diskLimit) return;
    stats.sort((a, b) => a.$2.modified.compareTo(b.$2.modified));
    for (final (f, st) in stats) {
      if (total <= _diskLimit * 0.8) break;
      await f.delete();
      total -= st.size;
    }
  }
}
