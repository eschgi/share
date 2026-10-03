import 'dart:async';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/library.dart';
import '../../data/models.dart';
import '../../data/platform.dart';
import '../../l10n/app_localizations.dart';
import '../download_sheet.dart';
import '../folders.dart';
import '../format.dart';
import '../icons.dart';
import '../library/library_controller.dart';
import '../library/scope.dart';
import '../library/tiles.dart';
import '../theme.dart';
import '../viewer.dart';

/// Screen 46: what everyone sent into the folder a PIN shows, by day, to look at and save, with
/// the PIN's key. Nothing can be deleted, and nobody's name shows. It shares the bar at the
/// bottom ([navigation]) and the menu with sending.
class SeeScreen extends StatefulWidget {
  const SeeScreen({super.key, required this.access, this.folderName, required this.navigation, required this.menu});
  final PinAccess access;

  /// The folder's name as the session says it, until the folder itself is fetched.
  final String? folderName;
  final Widget navigation;
  final Widget menu;

  @override
  State<SeeScreen> createState() => _SeeScreenState();
}

class _SeeScreenState extends State<SeeScreen> {
  late final AppServices _services = Services.read(context);
  late final LibraryRepository _library = LibraryRepository.pin(api: _services.api, platform: _services.platform, pin: widget.access);
  late final LibraryController _c = LibraryController(repo: _library, platform: _services.platform)..addListener(_changed);
  final _scroll = ScrollController();
  FolderInfo? _folder;
  Timer? _poll;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    _c.reload();
    _loadFolder();
    _scroll.addListener(() {
      if (_scroll.position.extentAfter < 1200) _c.more();
    });
    _poll = Timer.periodic(const Duration(seconds: 30), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    _scroll.dispose();
    _c.dispose();
    super.dispose();
  }

  void _changed() {
    if (_c.error case ApiException(status: 401)) _services.pin.ended();
    setState(() {});
  }

  /// The folder's name, how much it holds and from how many people.
  Future<void> _loadFolder() async {
    try {
      final list = FolderInfo.listFromJson(await _library.folders());
      if (mounted) setState(() => _folder = list.firstOrNull);
    } on ApiException catch (e) {
      if (e.status == 401) _services.pin.ended();
    } on NetworkException {
      // The library says so.
    }
  }

  Future<void> _refresh() async {
    if (await _c.refreshIfChanged()) await _loadFolder();
  }

  void _open(int index) => Navigator.push(
        context,
        MaterialPageRoute<void>(builder: (_) => LibraryScope(library: _library, child: ViewerScreen(files: List.of(_c.files), initial: index))),
      );

  /// Everything in the folder onto the phone.
  Future<void> _saveAll() async {
    final t = AppLocalizations.of(context);
    setState(() => _saving = true);
    try {
      await _c.loadAll();
      final files = List.of(_c.files);
      final batch = await _services.platform.download(files, auth: SendAuth.pin);
      if (mounted) await showDownloadSheet(context, batch: batch, files: files);
    } on PlatformException {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.commonFailed)));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = clock.now();
    final f = _folder;
    // How much the folder holds, from how many people, and saving it all.
    final slivers = <Widget>[
      SliverPadding(
        padding: const EdgeInsets.fromLTRB(20, 0, 20, 4),
        sliver: SliverToBoxAdapter(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(f == null ? '' : [FolderLines(context).count(f.files), if (f.senders > 0) t.seeFrom(f.senders)].join(' '),
                style: TextStyle(fontSize: 15, color: c.text2)),
            const SizedBox(height: 12),
            FilledButton.icon(
              style: FilledButton.styleFrom(
                backgroundColor: c.accentSoft,
                foregroundColor: c.accentText,
                minimumSize: const Size(0, 42),
                padding: const EdgeInsets.symmetric(horizontal: 16),
                shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
                textStyle: const TextStyle(fontFamily: 'Roboto', fontSize: 15, fontWeight: FontWeight.w600),
              ),
              onPressed: f == null || f.files == 0 || _saving ? null : _saveAll,
              icon: const Icon(AppIcons.download, size: 19),
              label: Text(t.seeDownloadAll(formatBytes(f?.bytes ?? 0, locale))),
            ),
          ]),
        ),
      ),
    ];
    var offset = 0;
    for (final s in _c.sections) {
      final start = offset;
      offset += s.files.length;
      slivers
        ..add(SliverPadding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          sliver: SliverToBoxAdapter(
            child: DayHeader(
              title: formatDay(s.day, now, locale, today: t.dayToday, yesterday: t.dayYesterday),
              meta: t.dayMeta(s.count, formatBytes(s.bytes, locale)),
            ),
          ),
        ))
        ..add(SliverPadding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          sliver: SliverGrid(
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: 4, mainAxisSpacing: 3, crossAxisSpacing: 3),
            delegate: SliverChildBuilderDelegate(
              (context, i) => GestureDetector(
                onTap: () => _open(start + i),
                child: LibraryTile(file: s.files[i], selected: false, selecting: false, saved: _c.saved.contains(s.files[i].id)),
              ),
              childCount: s.files.length,
            ),
          ),
        ));
    }
    if (_c.files.isEmpty && !_c.loading) {
      slivers.add(SliverFillRemaining(
        hasScrollBody: false,
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
            Icon(_c.error != null ? AppIcons.cloud : AppIcons.images, size: 40, color: c.text3),
            const SizedBox(height: 16),
            Text(_c.error != null ? t.commonOffline : t.seeEmpty, textAlign: TextAlign.center, style: TextStyle(fontSize: 15.5, height: 1.5, color: c.text2)),
          ]),
        ),
      ));
    }
    if (_c.loading && _c.files.isNotEmpty) {
      slivers.add(const SliverToBoxAdapter(child: Padding(padding: EdgeInsets.all(20), child: Center(child: CircularProgressIndicator()))));
    }
    return LibraryScope(
      library: _library,
      child: Scaffold(
        appBar: AppBar(
          titleSpacing: 22,
          // A folder's name is longer than the other screens' titles.
          title: Text(f?.name ?? widget.folderName ?? '', overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 24)),
          actions: [widget.menu],
        ),
        bottomNavigationBar: widget.navigation,
        body: RefreshIndicator(
          onRefresh: () => Future.wait([_c.reload(), _loadFolder()]),
          child: CustomScrollView(controller: _scroll, physics: const AlwaysScrollableScrollPhysics(), slivers: slivers),
        ),
      ),
    );
  }
}
