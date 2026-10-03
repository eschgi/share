import 'dart:io';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app.dart';
import '../data/models.dart';
import '../l10n/app_localizations.dart';
import 'admin/delete.dart';
import 'download_sheet.dart';
import 'fetch.dart';
import 'format.dart';
import 'icons.dart';
import 'library/tiles.dart';
import 'media_page.dart';
import 'theme.dart';
import 'widgets.dart';

/// Screen 14: one file at a time, at original size; swipe to the next. Admins can delete the file
/// shown.
class ViewerScreen extends StatefulWidget {
  const ViewerScreen({super.key, required this.files, required this.initial, this.onDeleted, this.onRestored, this.folderOf});
  final List<FileInfo> files;
  final int initial;

  /// The name of a file's folder, for the details; left out where there is only one folder.
  final String? Function(FileInfo file)? folderOf;

  /// Admins: a file was deleted here, the library drops it. Without it, there is no Delete.
  final void Function(String id)? onDeleted;

  /// Undo brought a deleted file back.
  final VoidCallback? onRestored;

  @override
  State<ViewerScreen> createState() => _ViewerScreenState();
}

class _ViewerScreenState extends State<ViewerScreen> {
  late final PageController _pages = PageController(initialPage: widget.initial);
  late int _index = widget.initial;
  // The viewer's own list: a deleted file leaves it. Pages are keyed by file, so the page of the
  // next one isn't the deleted one's.
  late final List<FileInfo> _files = List.of(widget.files);

  FileInfo get _file => _files[_index];

  @override
  void dispose() {
    _pages.dispose();
    super.dispose();
  }

  Future<void> _download() async {
    final platform = Services.read(context).platform;
    final String batch;
    try {
      batch = await platform.download([_file]);
    } on PlatformException {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(AppLocalizations.of(context).commonFailed)));
      return;
    }
    if (mounted) await showDownloadSheet(context, batch: batch, files: [_file]);
  }

  Future<void> _delete() async {
    final t = AppLocalizations.of(context);
    final admin = Services.read(context).admin;
    final f = _file;
    if (!await confirmDelete(context, 1, admin.trashDays) || !mounted) return;
    // The app's messenger: the Undo stays when the viewer closes.
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    try {
      await admin.deleteFiles([f.id]);
    } on Exception {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
      return;
    }
    widget.onDeleted?.call(f.id);
    final restored = widget.onRestored;
    // It goes by itself: staying, it would cover the viewer's buttons.
    messenger.showSnackBar(SnackBar(
      content: Text(t.deletedSnack(1)),
      persist: false,
      duration: const Duration(seconds: 6),
      action: SnackBarAction(
        label: t.commonUndo,
        onPressed: () async {
          await admin.restore([f.id]);
          restored?.call();
        },
      ),
    ));
    if (!mounted) return;
    if (_files.length == 1) {
      navigator.pop();
      return;
    }
    setState(() {
      _files.removeAt(_index);
      _index = _index.clamp(0, _files.length - 1);
    });
    // The last file went: show the one before it.
    if (_pages.hasClients && (_pages.page?.round() ?? _index) != _index) _pages.jumpToPage(_index);
  }

  void _details() {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    final f = _file;
    final c = context.colors;
    final folder = widget.folderOf?.call(f);
    showModalBottomSheet<void>(
      context: context,
      builder: (context) => SafeArea(
        child: Padding(
          padding: const EdgeInsets.fromLTRB(22, 0, 22, 16),
          child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(f.name, style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 12),
            for (final line in [
              '${formatDay(f.day, clock.now(), locale, today: t.dayToday, yesterday: t.dayYesterday)}, ${formatTime(f.uploadedAt, locale)}',
              f.from == null ? t.viewerFromPin : t.viewerFrom(f.from!),
              if (folder != null) t.viewerInFolder(folder),
              [
                formatBytes(f.size, locale),
                if (f.width != null && f.height != null) '${f.width} × ${f.height}',
                if (f.durationMs != null) formatDuration(f.durationMs!),
                f.mime,
              ].join(' · '),
            ])
              Padding(
                padding: const EdgeInsets.only(bottom: 6),
                child: Text(line, style: TextStyle(fontSize: 15, color: c.text2)),
              ),
          ]),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    final f = _file;
    final c = context.colors;
    final meta = [
      f.name,
      formatBytes(f.size, locale),
      if (f.width != null && f.height != null) '${f.width} × ${f.height}',
      if (f.durationMs != null) formatDuration(f.durationMs!),
    ].join(' · ');
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        backgroundColor: Colors.black,
        leading: const ShareBackButton(),
        titleSpacing: 0,
        title: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(formatDay(f.day, clock.now(), locale, today: t.dayToday, yesterday: t.dayYesterday),
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600, fontFamily: null)),
          Text(formatTime(f.uploadedAt, locale), style: TextStyle(fontSize: 12.5, color: c.text3)),
        ]),
        actions: [
          PopupMenuButton<String>(
            icon: const Icon(AppIcons.more),
            onSelected: (choice) => choice == 'open' ? withFetch(context, 1, () => Services.read(context).platform.openFile(f)) : _details(),
            itemBuilder: (_) => [
              PopupMenuItem(value: 'details', child: Text(t.viewerDetails)),
              PopupMenuItem(value: 'open', child: Text(t.viewerOpenElsewhere)),
            ],
          ),
        ],
      ),
      body: Column(children: [
        Expanded(
          child: PageView.builder(
            controller: _pages,
            itemCount: _files.length,
            onPageChanged: (i) => setState(() => _index = i),
            itemBuilder: (context, i) => _Page(key: ValueKey(_files[i].id), file: _files[i], active: i == _index),
            findChildIndexCallback: (key) {
              final i = _files.indexWhere((f) => ValueKey(f.id) == key);
              return i < 0 ? null : i;
            },
          ),
        ),
        _Filmstrip(files: _files, index: _index, onTap: (i) => _pages.jumpToPage(i)),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 4),
          child: Text(meta, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 13, color: c.text2)),
        ),
        Divider(color: c.lineSoft),
        SafeArea(
          top: false,
          child: Row(children: [
            _Action(icon: AppIcons.download, label: t.viewerDownload, onTap: _download),
            _Action(icon: AppIcons.share, label: t.viewerShare, onTap: () => withFetch(context, 1, () => Services.read(context).platform.shareFiles([f]))),
            _Action(icon: AppIcons.info, label: t.viewerDetails, onTap: _details),
            if (widget.onDeleted != null) _Action(icon: AppIcons.trash, label: t.deleteSelected, onTap: _delete),
          ]),
        ),
      ]),
    );
  }
}

class _Page extends StatefulWidget {
  const _Page({super.key, required this.file, required this.active});
  final FileInfo file;

  /// The page in view.
  final bool active;

  @override
  State<_Page> createState() => _PageState();
}

class _PageState extends State<_Page> {
  File? _original;

  @override
  void initState() {
    super.initState();
    if (widget.file.kind == FileKind.photo) {
      Services.read(context).library.original(widget.file).then((f) {
        if (mounted && f != null) setState(() => _original = f);
      }, onError: (Object _) {});
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final f = widget.file;
    switch (f.kind) {
      case FileKind.photo:
        final width = (MediaQuery.sizeOf(context).width * MediaQuery.devicePixelRatioOf(context) * 2).round();
        return InteractiveViewer(
          maxScale: 6,
          child: Center(
            child: _original != null
                ? Image.file(_original!, fit: BoxFit.contain, cacheWidth: width, gaplessPlayback: true)
                : ThumbImage(file: f, fit: BoxFit.contain),
          ),
        );
      case FileKind.video:
        return MediaPage(file: f, active: widget.active);
      case FileKind.document when f.isAudio:
        return MediaPage(file: f, active: widget.active);
      case FileKind.document:
        return Center(
          child: Padding(
            padding: const EdgeInsets.all(32),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              SizedBox(width: 160, height: 160, child: ClipRRect(borderRadius: BorderRadius.circular(14), child: ThumbImage(file: f))),
              const SizedBox(height: 20),
              Text(t.viewerNoPreview, textAlign: TextAlign.center, style: TextStyle(color: c.text2, fontSize: 15, height: 1.5)),
              TextButton(onPressed: () => withFetch(context, 1, () => Services.read(context).platform.openFile(f)), child: Text(f.name)),
            ]),
          ),
        );
    }
  }
}

class _Filmstrip extends StatelessWidget {
  const _Filmstrip({required this.files, required this.index, required this.onTap});
  final List<FileInfo> files;
  final int index;
  final ValueChanged<int> onTap;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    final from = (index - 2).clamp(0, files.length - 1);
    final to = (index + 2).clamp(0, files.length - 1);
    return Padding(
      padding: const EdgeInsets.only(top: 10),
      child: Row(mainAxisAlignment: MainAxisAlignment.center, children: [
        for (var i = from; i <= to; i++)
          GestureDetector(
            onTap: () => onTap(i),
            child: Container(
              width: i == index ? 52 : 44,
              height: i == index ? 52 : 44,
              margin: const EdgeInsets.symmetric(horizontal: 3),
              decoration: BoxDecoration(
                borderRadius: BorderRadius.circular(8),
                border: i == index ? Border.all(color: c.text, width: 2) : null,
              ),
              clipBehavior: Clip.antiAlias,
              child: ThumbImage(file: files[i]),
            ),
          ),
      ]),
    );
  }
}

class _Action extends StatelessWidget {
  const _Action({required this.icon, required this.label, required this.onTap});
  final IconData icon;
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Expanded(
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.symmetric(vertical: 12),
            child: Column(children: [
              Icon(icon, size: 22),
              const SizedBox(height: 6),
              Text(label, style: const TextStyle(fontSize: 12.5)),
            ]),
          ),
        ),
      );
}
