import 'dart:async';

import 'package:flutter/foundation.dart';

import '../../data/api.dart';
import '../../data/library.dart';
import '../../data/models.dart';
import '../../data/platform.dart';

/// One upload day on the screen: its loaded files, and the server's totals for the day.
class DaySection {
  const DaySection({required this.day, required this.files, required this.count, required this.bytes});
  final String day;
  final List<FileInfo> files;
  final int count;
  final int bytes;
}

enum DaySelection { none, some, all }

/// Some day has fewer files than before, or none: files were moved away or hidden, which adding
/// new files on top can't show.
bool shrank(LibraryOverview before, LibraryOverview after) {
  final now = {for (final d in after.days) d.day: d.count};
  return before.days.any((d) => (now[d.day] ?? 0) < d.count);
}

/// The library screen's state: files loaded page by page, newest first, and the selection.
class LibraryController extends ChangeNotifier {
  LibraryController({required this.repo, required this.platform});

  final LibraryRepository repo;
  final Platform platform;

  LibraryFilter _filter = const LibraryFilter();
  LibraryOverview? overview;
  final files = <FileInfo>[];
  final _shown = <String>{}; // the ids in files
  final _known = <String, FileInfo>{}; // everything seen, also files fetched only for selecting
  String? _cursor;
  bool _end = false;
  bool loading = false;
  Object? error;
  Set<String> saved = {};
  final selected = <String>{};
  int _generation = 0;

  LibraryFilter get filter => _filter;
  bool get selecting => selected.isNotEmpty;
  bool get complete => _end;

  Future<void> setFilter(LibraryFilter f) {
    if (f == _filter) return Future.value();
    _filter = f;
    selected.clear();
    return reload();
  }

  Future<void> reload() async {
    final gen = ++_generation;
    files.clear();
    _shown.clear();
    _cursor = null;
    _end = false;
    error = null;
    loading = true;
    notifyListeners();
    try {
      final o = await repo.overview(_filter);
      if (gen != _generation) return;
      overview = o;
      loading = false;
      await more();
    } catch (e) {
      if (gen != _generation) return;
      error = e;
      loading = false;
      notifyListeners();
    }
  }

  /// The next page, when the list is scrolled near its end.
  Future<void> more() async {
    if (loading || _end || overview == null) return;
    final gen = _generation;
    loading = true;
    notifyListeners();
    try {
      final page = await repo.page(_filter, cursor: _cursor);
      if (gen != _generation) return;
      _add(page.files);
      _cursor = page.nextCursor;
      _end = page.nextCursor == null;
      saved = {...saved, ...await platform.savedIds(page.files.map((f) => f.id))};
    } catch (e) {
      if (gen == _generation) error = e;
    } finally {
      if (gen == _generation) {
        loading = false;
        notifyListeners();
      }
    }
  }

  void _add(Iterable<FileInfo> page) {
    for (final f in page) {
      if (!_shown.add(f.id)) continue;
      files.add(f);
      _known[f.id] = f;
    }
  }

  /// Something changed on the server (the version grew): puts new files on top without
  /// losing the place in the list; if files went away, such as into another folder, the list
  /// starts over. Says whether the library changed.
  Future<bool> refreshIfChanged() async {
    final current = overview;
    if (current == null || loading) return false;
    final gen = _generation;
    try {
      final o = await repo.overview(_filter);
      if (gen != _generation || o.version == current.version) return false;
      if (shrank(current, o)) {
        unawaited(reload());
        return true;
      }
      final first = await repo.page(_filter, limit: 100);
      if (gen != _generation) return true;
      final newer = first.files.where((f) => !_shown.contains(f.id)).toList();
      overview = o;
      files.insertAll(0, newer);
      for (final f in newer) {
        _shown.add(f.id);
        _known[f.id] = f;
      }
      notifyListeners();
      return true;
    } on ApiException catch (e) {
      if (e.status != 404 || gen != _generation) return false;
      unawaited(reload()); // the folder shown is gone, or no longer the person's: say so
      return true;
    } catch (_) {
      return false; // try again at the next check
    }
  }

  /// Files gone from the library, such as one deleted in the viewer: off the list and the
  /// selection at once; the days' totals follow from the server.
  void remove(Iterable<String> ids) {
    final gone = ids.toSet();
    files.removeWhere((f) => gone.contains(f.id));
    _shown.removeAll(gone);
    selected.removeAll(gone);
    notifyListeners();
    final gen = _generation;
    repo.overview(_filter).then((o) {
      if (gen != _generation) return;
      overview = o;
      notifyListeners();
    }, onError: (Object _) {});
  }

  /// Marks files as saved on this phone after a download.
  Future<void> refreshSaved() async {
    saved = await platform.savedIds(files.map((f) => f.id));
    notifyListeners();
  }

  List<DaySection> get sections {
    final byDay = <String, List<FileInfo>>{};
    for (final f in files) {
      byDay.putIfAbsent(f.day, () => []).add(f);
    }
    final totals = {for (final d in overview?.days ?? const <DaySummary>[]) d.day: d};
    return [
      for (final e in byDay.entries)
        DaySection(
          day: e.key,
          files: e.value,
          count: totals[e.key]?.count ?? e.value.length,
          bytes: totals[e.key]?.bytes ?? e.value.fold(0, (s, f) => s + f.size),
        ),
    ];
  }

  // Selection.

  bool isSelected(String id) => selected.contains(id);

  void toggle(String id) {
    selected.contains(id) ? selected.remove(id) : selected.add(id);
    notifyListeners();
  }

  /// Sets a run of files at once, for dragging across tiles.
  void setSelection(Set<String> ids) {
    selected
      ..clear()
      ..addAll(ids);
    notifyListeners();
  }

  void clearSelection() {
    selected.clear();
    notifyListeners();
  }

  DaySelection daySelection(DaySection s) {
    final n = selectedOfDay(s.day);
    if (n == 0) return DaySelection.none;
    return n >= s.count ? DaySelection.all : DaySelection.some;
  }

  int selectedOfDay(String day) => selected.where((id) => _known[id]?.day == day).length;

  /// The day's circle: takes the whole day, including files not loaded yet, or lets it go.
  Future<void> toggleDay(DaySection s) async {
    if (daySelection(s) == DaySelection.all) {
      selected.removeWhere((id) => _known[id]?.day == s.day);
      notifyListeners();
      return;
    }
    if (s.files.length < s.count) {
      for (final f in await repo.day(_filter, s.day)) {
        _known[f.id] = f;
      }
    }
    selected.addAll(_known.values.where((f) => f.day == s.day).map((f) => f.id));
    notifyListeners();
  }

  /// Loads every file the filter shows, page by page.
  Future<void> loadAll() async {
    while (!_end) {
      final before = files.length;
      await more();
      if (files.length == before && !_end) break; // an error; keep what we have
    }
  }

  /// Everything the filter shows, loaded first if need be.
  Future<void> selectAll() async {
    await loadAll();
    selected.addAll(files.map((f) => f.id));
    notifyListeners();
  }

  List<FileInfo> get selectedFiles => [for (final id in selected) ?_known[id]];

  int get selectedBytes => selectedFiles.fold(0, (s, f) => s + f.size);
}
