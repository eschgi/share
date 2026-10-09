import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter/rendering.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/folders.dart';
import '../../data/models.dart';
import '../../data/platform.dart' show KeysException;
import '../../l10n/app_localizations.dart';
import '../admin/delete.dart';
import '../download_sheet.dart';
import '../encryption.dart';
import '../folders.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../viewer.dart';
import '../widgets.dart';
import '../zip/zip_entry.dart';
import 'library_controller.dart';
import 'move_sheet.dart';
import 'tiles.dart';

/// Screens 11 and 12: everything that was sent, newest day first, and selecting many.
class LibraryScreen extends StatefulWidget {
  const LibraryScreen({super.key, required this.user, required this.navigation, required this.onAvatar});
  final User user;
  final Widget navigation;
  final VoidCallback onAvatar;

  @override
  State<LibraryScreen> createState() => _LibraryScreenState();
}

/// Marks a tile for hit tests while dragging across the grid.
class _TileRef {
  const _TileRef(this.index);
  final int index;
}

class _LibraryScreenState extends State<LibraryScreen> with WidgetsBindingObserver {
  late final LibraryController _c;
  late final FolderStore _folders = Services.read(context).folders;
  bool _started = false;
  final _scroll = ScrollController();
  final _scrollBox = GlobalKey();
  Timer? _poll;
  bool _searching = false;
  Timer? _searchDelay;

  // Dragging across tiles to select a run of them.
  int? _anchor;
  bool _dragAdds = true;
  Set<String> _before = {};
  Offset? _pointer;
  Timer? _autoScroll;

  @override
  void initState() {
    super.initState();
    final s = Services.read(context);
    _c = LibraryController(repo: s.library, platform: s.platform)..addListener(_changed);
    _folders.addListener(_foldersChanged);
    unawaited(_first());
    _scroll.addListener(() {
      if (_scroll.position.extentAfter < 1200) _c.more();
    });
    _poll = Timer.periodic(const Duration(seconds: 30), (_) => _refresh());
    WidgetsBinding.instance.addObserver(this);
  }

  /// The first page waits for the folders, to show the folder chosen last time.
  Future<void> _first() async {
    if (_folders.list == null && !_folders.failed) await _folders.load();
    if (!mounted) return;
    _started = true;
    final f = _c.filter.withFolder(_folders.shown?.id);
    await (f == _c.filter ? _c.reload() : _c.setFilter(f));
  }

  void _changed() {
    // A folder the person doesn't see any more answers 404: then all folders.
    final e = _c.error;
    final folder = _c.filter.folder;
    if (e is ApiException && e.status == 404 && folder != null) unawaited(_folders.gone(folder));
    setState(() {});
  }

  void _foldersChanged() {
    if (_started && _folders.shown?.id != _c.filter.folder) _c.setFilter(_c.filter.withFolder(_folders.shown?.id));
    setState(() {});
  }

  /// New files, and with them the folders' counts.
  Future<void> _refresh() async {
    if (await _c.refreshIfChanged()) unawaited(_folders.load());
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      _refresh();
      _c.refreshSaved();
    }
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _folders.removeListener(_foldersChanged);
    _poll?.cancel();
    _searchDelay?.cancel();
    _autoScroll?.cancel();
    _scroll.dispose();
    _c.dispose();
    super.dispose();
  }

  void _setKind(FileKind? kind) => _c.setFilter(_c.filter.withKind(kind));

  void _search(String q) {
    _searchDelay?.cancel();
    _searchDelay = Timer(const Duration(milliseconds: 350), () => _c.setFilter(_c.filter.withQuery(q)));
  }

  // Drag selection.

  void _startDrag(int index) {
    final id = _c.files[index].id;
    _anchor = index;
    _before = {..._c.selected};
    _dragAdds = !_c.isSelected(id);
    _applyDrag(index);
    _autoScroll = Timer.periodic(const Duration(milliseconds: 16), (_) => _scrollNearEdge());
  }

  void _dragTo(Offset global) {
    _pointer = global;
    final index = _tileAt(global);
    if (index != null) _applyDrag(index);
  }

  void _endDrag() {
    _anchor = null;
    _pointer = null;
    _autoScroll?.cancel();
    _autoScroll = null;
  }

  void _applyDrag(int to) {
    final from = _anchor!;
    final (lo, hi) = from <= to ? (from, to) : (to, from);
    final run = {for (var i = lo; i <= hi; i++) _c.files[i].id};
    _c.setSelection(_dragAdds ? {..._before, ...run} : _before.difference(run));
  }

  int? _tileAt(Offset global) {
    final result = HitTestResult();
    WidgetsBinding.instance.hitTestInView(result, global, View.of(context).viewId);
    for (final e in result.path) {
      final t = e.target;
      if (t is RenderMetaData && t.metaData is _TileRef) return (t.metaData as _TileRef).index;
    }
    return null;
  }

  /// While dragging near the top or bottom, the list scrolls, faster closer to the edge.
  void _scrollNearEdge() {
    final p = _pointer;
    final box = _scrollBox.currentContext?.findRenderObject() as RenderBox?;
    if (p == null || box == null || !_scroll.hasClients) return;
    final local = box.globalToLocal(p);
    const zone = 80.0;
    double delta = 0;
    if (local.dy < zone) delta = -(zone - local.dy) / 3;
    if (local.dy > box.size.height - zone) delta = (local.dy - (box.size.height - zone)) / 3;
    if (delta == 0) return;
    final pos = _scroll.position;
    final target = (pos.pixels + delta).clamp(pos.minScrollExtent, pos.maxScrollExtent);
    if (target != pos.pixels) {
      _scroll.jumpTo(target);
      _dragTo(p);
    }
  }

  Future<void> _download() async {
    final files = _c.selectedFiles;
    if (files.isEmpty) return;
    final platform = Services.read(context).platform;
    final String batch;
    try {
      batch = await platform.download(files);
    } on PlatformException {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(AppLocalizations.of(context).commonFailed)));
      return;
    }
    _c.clearSelection();
    if (!mounted) return;
    await showDownloadSheet(context, batch: batch, files: files);
    await _c.refreshSaved();
  }

  /// Admins: into Recently deleted, with a moment to take it back.
  Future<void> _delete() async {
    final t = AppLocalizations.of(context);
    final admin = Services.read(context).admin;
    final ids = _c.selected.toList();
    if (ids.isEmpty || !await confirmDelete(context, ids.length, admin.trashDays) || !mounted) return;
    final messenger = ScaffoldMessenger.of(context);
    try {
      final n = await admin.deleteFiles(ids);
      _c.clearSelection();
      await _c.reload();
      messenger.showSnackBar(SnackBar(
        content: Text(t.deletedSnack(n)),
        action: SnackBarAction(
          label: t.commonUndo,
          onPressed: () async {
            await admin.restore(ids);
            await _c.reload();
          },
        ),
      ));
    } on Exception {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    }
  }

  /// Admins, from the second folder on (screen 49): the files go into another folder, and out of
  /// the one shown. Files that were all in one folder can go back there.
  Future<void> _move() async {
    final t = AppLocalizations.of(context);
    final services = Services.read(context);
    final admin = services.admin;
    final files = _c.selectedFiles;
    final ids = [for (final f in files) f.id];
    final from = {for (final f in files) f.folder}.singleOrNull; // where they all are
    final to = await showMoveSheet(context, count: ids.length, list: _folders.list ?? const [], here: from);
    if (to == null || !mounted) return;
    final messenger = ScaffoldMessenger.of(context);
    String problem(Object e) => switch (e) {
          ApiException(code: 'not_encrypted') => t.encryptionMoveIntoPlain,
          KeysException(sealed: true) when to.keyVersion == null => t.encryptionMoveIntoPlain,
          KeysException(sealed: true) => t.keysCantOpen,
          _ => t.commonFailed,
        };
    try {
      // Encrypted files go with their keys, sealed for the folder's newest key.
      final encrypted = [for (final f in files) if (f.enc != null) f];
      final keys = encrypted.isEmpty ? const <Json>[] : await services.platform.moveKeys(encrypted, to.id);
      final n = await admin.moveFiles(ids, to.id, keys: keys);
      _c.clearSelection();
      if (_c.filter.folder != null) {
        _c.remove(ids);
      } else {
        unawaited(_c.reload());
      }
      unawaited(_folders.load());
      messenger.showSnackBar(SnackBar(
        content: Text(t.moveDone(n, to.name)),
        action: from == null
            ? null
            : SnackBarAction(
                label: t.commonUndo,
                onPressed: () async {
                  try {
                    // Back with the keys sealed for where they came from, from those they have now.
                    final there = [
                      for (final f in encrypted)
                        if (keys.where((k) => k['id'] == f.id).firstOrNull case final k?)
                          FileInfo.fromJson({...f.toJson(), 'folder': to.id, 'enc': {...f.enc!.toJson(), 'version': k['version'], 'key': k['key']}}),
                    ];
                    final back = there.isEmpty ? const <Json>[] : await services.platform.moveKeys(there, from);
                    await admin.moveFiles(ids, from, keys: back);
                  } on Exception catch (e) {
                    messenger.showSnackBar(SnackBar(content: Text(problem(e))));
                  }
                  await Future.wait([_c.reload(), _folders.load()]);
                },
              ),
      ));
    } on Exception catch (e) {
      messenger.showSnackBar(SnackBar(content: Text(problem(e))));
    }
  }

  void _open(int index) {
    Navigator.push(
      context,
      MaterialPageRoute<void>(
        builder: (_) => ViewerScreen(
          files: List.of(_c.files),
          initial: index,
          onDeleted: widget.user.isAdmin ? (id) => _c.remove([id]) : null,
          onRestored: _c.reload,
          folderOf: _folders.choices ? (f) => _folders.byId(f.folder)?.name : null,
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = clock.now();

    final slivers = <Widget>[
      if (!_c.selecting) SliverToBoxAdapter(child: _Filters(kind: _c.filter.kind, onKind: _setKind)),
      if (!_c.selecting) SliverToBoxAdapter(child: KeysBanner(admin: widget.user.isAdmin)),
      if (_c.selecting)
        SliverToBoxAdapter(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
            child: _TipBar(text: t.selectTip),
          ),
        ),
    ];
    var offset = 0;
    for (final s in _c.sections) {
      final start = offset;
      offset += s.files.length;
      final sel = _c.selectedOfDay(s.day);
      slivers
        ..add(SliverPadding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          sliver: SliverToBoxAdapter(
            child: DayHeader(
              title: formatDay(s.day, now, locale, today: t.dayToday, yesterday: t.dayYesterday),
              meta: sel > 0 && sel < s.count ? t.daySelected(sel, s.count) : t.dayMeta(s.count, formatBytes(s.bytes, locale)),
              selection: _c.daySelection(s),
              circleLabel: t.selectDay,
              onCircle: () => _c.toggleDay(s),
            ),
          ),
        ))
        ..add(SliverPadding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          sliver: SliverGrid(
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: 4, mainAxisSpacing: 3, crossAxisSpacing: 3),
            delegate: SliverChildBuilderDelegate(
              (context, i) {
                final f = s.files[i];
                final index = start + i;
                return MetaData(
                  metaData: _TileRef(index),
                  behavior: HitTestBehavior.opaque,
                  child: GestureDetector(
                    onTap: () => _c.selecting ? _c.toggle(f.id) : _open(index),
                    onLongPressStart: (_) => _startDrag(index),
                    onLongPressMoveUpdate: (d) => _dragTo(d.globalPosition),
                    onLongPressEnd: (_) => _endDrag(),
                    child: LibraryTile(
                      file: f,
                      selected: _c.isSelected(f.id),
                      selecting: _c.selecting,
                      saved: _c.saved.contains(f.id),
                    ),
                  ),
                );
              },
              childCount: s.files.length,
            ),
          ),
        ));
    }
    if (_c.files.isEmpty && !_c.loading) {
      final empty = _c.error != null
          ? t.commonOffline
          : (_c.filter.kind != null || _c.filter.query.isNotEmpty ? t.libraryNothingFound : t.libraryEmpty);
      slivers.add(SliverFillRemaining(
        hasScrollBody: false,
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
            Icon(_c.error != null ? AppIcons.cloud : AppIcons.images, size: 40, color: c.text3),
            const SizedBox(height: 16),
            Text(empty, textAlign: TextAlign.center, style: TextStyle(color: c.text2, fontSize: 15, height: 1.5)),
            if (_c.error != null) TextButton(onPressed: _c.reload, child: Text(t.commonRetry)),
          ]),
        ),
      ));
    } else if (_c.loading || !_c.complete) {
      slivers.add(const SliverToBoxAdapter(
        child: Padding(padding: EdgeInsets.all(24), child: Center(child: CircularProgressIndicator(strokeWidth: 2.5))),
      ));
    }
    slivers.add(const SliverToBoxAdapter(child: SizedBox(height: 24)));

    return PopScope(
      canPop: !_c.selecting,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _c.clearSelection();
      },
      child: Scaffold(
        appBar: _c.selecting ? _selectionBar(t) : _libraryBar(t, c),
        body: RefreshIndicator(
          onRefresh: _c.reload,
          child: CustomScrollView(key: _scrollBox, controller: _scroll, slivers: slivers),
        ),
        bottomNavigationBar: _c.selecting ? _actionBar(t, locale) : widget.navigation,
      ),
    );
  }

  PreferredSizeWidget _libraryBar(AppLocalizations t, ShareColors c) => AppBar(
        titleSpacing: 22,
        title: _searching
            ? TextField(
                autofocus: true,
                onChanged: _search,
                style: const TextStyle(fontSize: 17),
                decoration: InputDecoration(
                  hintText: t.librarySearch,
                  filled: false,
                  border: InputBorder.none,
                  enabledBorder: InputBorder.none,
                  focusedBorder: InputBorder.none,
                ),
              )
            : _folders.choices
                ? FolderTitle(shown: _folders.shown, onTap: () => showFolderSheet(context))
                : Text(t.libraryTitle),
        actions: [
          IconButton(
            tooltip: t.librarySearch,
            icon: Icon(_searching ? AppIcons.x : AppIcons.search, size: 22),
            onPressed: () {
              setState(() => _searching = !_searching);
              if (!_searching) _search('');
            },
          ),
          Padding(
            padding: const EdgeInsets.only(right: 14, left: 4),
            child: GestureDetector(
              onTap: widget.onAvatar,
              child: Avatar(name: widget.user.name, id: widget.user.id, size: 34),
            ),
          ),
        ],
      );

  PreferredSizeWidget _selectionBar(AppLocalizations t) => AppBar(
        backgroundColor: context.colors.bar,
        leading: IconButton(icon: const Icon(AppIcons.x), onPressed: _c.clearSelection),
        titleTextStyle: TextStyle(fontFamily: 'Roboto', fontSize: 19, fontWeight: FontWeight.w600, color: context.colors.text),
        title: Text(t.selectCount(_c.selected.length)),
        actions: [TextButton(onPressed: _c.selectAll, child: Text(t.selectAll))],
      );

  Widget _actionBar(AppLocalizations t, String locale) {
    final c = context.colors;
    return Container(
      color: c.bar,
      padding: EdgeInsets.fromLTRB(16, 12, 16, 12 + MediaQuery.paddingOf(context).bottom),
      child: Row(children: [
        Material(
          color: c.s2,
          borderRadius: BorderRadius.circular(16),
          child: InkWell(
            borderRadius: BorderRadius.circular(16),
            onTap: () {
              shareHow(context, _c.selectedFiles);
            },
            child: SizedBox(width: 56, height: 56, child: Icon(AppIcons.share, size: 22, semanticLabel: t.shareSelected)),
          ),
        ),
        if (widget.user.isAdmin && _folders.choices) ...[
          const SizedBox(width: 10),
          Material(
            color: c.s2,
            borderRadius: BorderRadius.circular(16),
            child: InkWell(
              borderRadius: BorderRadius.circular(16),
              onTap: _move,
              child: SizedBox(width: 56, height: 56, child: Icon(AppIcons.folder, size: 22, semanticLabel: t.moveSelected)),
            ),
          ),
        ],
        if (widget.user.isAdmin) ...[
          const SizedBox(width: 10),
          Material(
            color: c.dangerSoft,
            borderRadius: BorderRadius.circular(16),
            child: InkWell(
              borderRadius: BorderRadius.circular(16),
              onTap: _delete,
              child: SizedBox(width: 56, height: 56, child: Icon(AppIcons.trash, size: 22, color: c.danger, semanticLabel: t.deleteSelected)),
            ),
          ),
        ],
        const SizedBox(width: 10),
        Expanded(
          child: FilledButton(
            style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(56), shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16))),
            onPressed: _download,
            child: FittedBox(
              fit: BoxFit.scaleDown,
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                const Icon(AppIcons.download, size: 20),
                const SizedBox(width: 10),
                Text(t.downloadCount(_c.selected.length), style: const TextStyle(fontSize: 16.5)),
                Text(' · ${formatBytes(_c.selectedBytes, locale)}',
                    style: TextStyle(fontSize: 14, fontWeight: FontWeight.w500, color: c.onAccent.withValues(alpha: 0.8))),
              ]),
            ),
          ),
        ),
      ]),
    );
  }
}

class _Filters extends StatelessWidget {
  const _Filters({required this.kind, required this.onKind});
  final FileKind? kind;
  final ValueChanged<FileKind?> onKind;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    Widget chip(String label, FileKind? k) {
      final on = kind == k;
      return Padding(
        padding: const EdgeInsets.only(right: 8),
        child: Material(
          color: on ? c.accentSoft : Colors.transparent,
          shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(10), side: BorderSide(color: on ? Colors.transparent : c.line)),
          child: InkWell(
            borderRadius: BorderRadius.circular(10),
            onTap: () => onKind(k),
            child: Container(
              height: 34,
              padding: const EdgeInsets.symmetric(horizontal: 14),
              alignment: Alignment.center,
              child: Text(label, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w500, color: on ? c.accentText : c.text2)),
            ),
          ),
        ),
      );
    }

    return SingleChildScrollView(
      scrollDirection: Axis.horizontal,
      padding: const EdgeInsets.fromLTRB(16, 2, 16, 4),
      child: Row(children: [
        chip(t.filterAll, null),
        chip(t.filterPhotos, FileKind.photo),
        chip(t.filterVideos, FileKind.video),
        chip(t.filterDocuments, FileKind.document),
      ]),
    );
  }
}

class _TipBar extends StatelessWidget {
  const _TipBar({required this.text});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 11),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(14), border: Border.all(color: c.lineSoft)),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(AppIcons.info, size: 18, color: c.accentText),
        const SizedBox(width: 10),
        Expanded(child: Text(text, style: TextStyle(fontSize: 13, height: 1.45, color: c.text2))),
      ]),
    );
  }
}
