import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../data/platform.dart';
import '../../data/session.dart';
import '../../data/zip.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../send/send_screen.dart';
import '../theme.dart';
import '../widgets.dart';
import 'zip_widgets.dart';

/// Screens 105 to 110 (docs/zip-plan.md): ZIPs opened with Share, from a chat or from Files. It shows
/// what's inside before anything is saved; Save puts photos and videos into the album Share and
/// documents into Downloads › Share, as downloads do, and signed in they can go into a folder instead.
/// Once a part of a set is saved, its other parts are saved the same way as soon as they're opened,
/// and a cut file is put back together when its last piece is in. Closing goes back to the app the
/// ZIP came from ([fromOutside]).
class ZipOpenScreen extends StatefulWidget {
  const ZipOpenScreen({super.key, this.fromOutside = true});
  final bool fromOutside;

  @override
  State<ZipOpenScreen> createState() => _ZipOpenScreenState();
}

enum _Stage { loading, contents, saving, saved, joined, failed }

class _ZipOpenScreenState extends State<ZipOpenScreen> {
  ZipContents? _c;
  _Stage _stage = _Stage.loading;
  ZipSaveState? _save;
  final _thumbs = <int, ZipThumb?>{};

  /// Saved while this screen is open; [_auto] when by itself, as the set's parts before.
  bool _savedNow = false;
  bool _auto = false;
  StreamSubscription<ZipSaveState>? _saves;

  Platform get _platform => Services.read(context).platform;

  @override
  void initState() {
    super.initState();
    _saves = _platform.zipSaving.listen(_saving);
    WidgetsBinding.instance.addPostFrameCallback((_) => _load());
  }

  @override
  void dispose() {
    _saves?.cancel();
    super.dispose();
  }

  bool get _signedIn => Services.read(context).session.current is SignedInState;

  Future<void> _load() async {
    final c = await _platform.zipContents();
    if (!mounted) return;
    setState(() {
      _c = c;
      _stage = _Stage.contents;
    });
    final set = c?.set;
    // A set whose parts were saved before: this one follows, the same way. Into a folder only while
    // sending still goes there, as files in the outbox go where sending goes.
    final sameFolder = _signedIn && set?.folder != null && Services.read(context).folders.sendTo?.id == set?.folder;
    if (c != null && set != null && set.saved.isNotEmpty && c.toSave > 0 && (set.to == 'phone' || set.to == 'folder' && sameFolder)) {
      _auto = true;
      await _saveTo(set.to!, set.to == 'folder' ? set.folder : null);
    }
  }

  Future<void> _thumb(int index) async {
    if (_thumbs.containsKey(index)) return;
    _thumbs[index] = null;
    final thumb = await _platform.zipOpenThumb(index);
    if (mounted && thumb != null) setState(() => _thumbs[index] = thumb);
  }

  Future<void> _saveTo(String to, String? folder) async {
    setState(() {
      _stage = _Stage.saving;
      _save = null;
    });
    try {
      await _platform.zipSave(to: to, folder: folder);
    } on PlatformException {
      if (mounted) setState(() => _stage = _Stage.failed);
    }
  }

  Future<void> _sendInto() async {
    final folders = Services.read(context).folders;
    if (folders.list == null) await folders.load();
    if (!mounted) return;
    var id = folders.sendTo?.id;
    if (folders.asksForFolder || id == null) id = await chooseSendFolder(context);
    if (id != null && mounted) await _saveTo('folder', id);
  }

  void _saving(ZipSaveState s) {
    if (!mounted) return;
    switch (s.stage) {
      case ZipSaveStage.saving:
        setState(() => _save = s);
      case ZipSaveStage.done:
        unawaited(_done(s));
      case ZipSaveStage.failed || ZipSaveStage.noRoom:
        setState(() {
          _save = s;
          _stage = _Stage.failed;
        });
    }
  }

  /// Saved: what's there now, and where to go from here.
  Future<void> _done(ZipSaveState s) async {
    final c = await _platform.zipContents() ?? _c;
    if (!mounted) return;
    setState(() {
      _c = c;
      _save = s;
      _savedNow = true;
      _stage = (c?.waiting.isNotEmpty ?? false) || s.broken.isNotEmpty
          ? _Stage.contents
          : s.joined.isNotEmpty
              ? _Stage.joined
              : _Stage.saved;
    });
  }

  Future<void> _close() async {
    final platform = _platform;
    final navigator = Navigator.of(context);
    await platform.zipCloseOpened();
    if (!mounted) return;
    navigator.pop();
    if (widget.fromOutside) await SystemNavigator.pop();
  }

  Future<void> _openGallery() async {
    final t = AppLocalizations.of(context);
    try {
      await _platform.openGallery();
    } on OpenFailed {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.openNoApp)));
    }
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _close();
      },
      child: Scaffold(
        appBar: AppBar(leading: IconButton(tooltip: MaterialLocalizations.of(context).closeButtonTooltip, icon: const Icon(AppIcons.x, size: 24), onPressed: _close)),
        body: SafeArea(
          top: false,
          child: switch (_stage) {
            _Stage.loading => const Center(child: CircularProgressIndicator()),
            _Stage.contents || _Stage.saving => _contents(context),
            _Stage.saved => _saved(context),
            _Stage.joined => _joined(context),
            _Stage.failed => _failed(context),
          },
        ),
      ),
    );
  }

  /// "3 and 4", "2, 3 and 4".
  String _and(AppLocalizations t, List<int> parts) {
    if (parts.isEmpty) return '';
    if (parts.length == 1) return '${parts.single}';
    return t.zipAnd(parts.sublist(0, parts.length - 1).join(', '), '${parts.last}');
  }

  Widget _contents(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final z = _c;
    if (z == null || (z.files.isEmpty && z.broken > 0)) {
      return _page([
        NoteCard(icon: AppIcons.alert, text: t.zipNotOpened(z?.broken ?? 1)),
      ], [
        FilledButton(onPressed: _close, child: Text(t.zipDone)),
      ]);
    }
    final set = z.set;
    final here = set?.here ?? const <int>[];
    final saving = _stage == _Stage.saving;

    // A cut file whose pieces are all in shows once, as itself; otherwise each piece here shows as one.
    final shown = <ZipEntryInfo>[];
    final whole = <String>{};
    for (final f in z.files) {
      final j = z.joinOf(f);
      if (j != null && j.whole) {
        if (whole.add(j.file)) shown.add(f);
      } else {
        shown.add(f);
      }
    }

    final String meta;
    if (set != null && set.parts > 1 && here.length > 1) {
      meta = t.zipPartsMeta(z.files.where((f) => f.piece == null).length + z.joins.length, here.length, formatBytes(z.bytes, locale));
    } else if (set != null && set.parts > 1 && here.length == 1) {
      meta = _savedNow && _auto ? t.zipSavedLikeBefore(here.single, set.parts) : t.zipPartMeta(here.single, set.parts, formatBytes(z.bytes, locale));
    } else {
      meta = zipMeta(t, locale, z.files.where((f) => f.piece == null).map((f) => f.kind).followedBy(z.joins.map((j) => j.kind)), z.bytes);
    }

    final kinds = z.files.map((f) => f.kind).toSet();
    final where = !kinds.contains(FileKind.document) ? t.zipIntoGallery : (kinds.length == 1 ? t.zipIntoDownloads : t.zipIntoBoth);
    final waiting = z.waiting;
    final broken = _save?.broken ?? const [];

    final notes = <Widget>[
      if (z.newer) NoteCard(icon: AppIcons.info, text: t.zipNewer),
      if (z.broken > 0) NoteCard(icon: AppIcons.alert, text: t.zipNotOpened(z.broken)),
      if (broken.isNotEmpty) NoteCard(icon: AppIcons.alert, text: t.zipBroken(broken.length)),
      if (waiting.length == 1)
        _waitingNote(t, waiting.single)
      else if (waiting.length > 1)
        NoteCard(icon: AppIcons.clock, text: t.zipWaitsMany(_and(t, {for (final j in waiting) ...j.missingParts}.toList()..sort()))),
    ];

    final List<Widget> buttons;
    if (saving) {
      final s = _save;
      buttons = [
        Container(
          padding: const EdgeInsets.all(16),
          decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            Text(t.zipSaving(((s?.done ?? 0) + 1).clamp(1, (s?.total ?? 1).clamp(1, 1 << 30)), (s?.total ?? 1).clamp(1, 1 << 30)), style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
            const SizedBox(height: 12),
            ClipRRect(borderRadius: BorderRadius.circular(5), child: LinearProgressIndicator(value: s?.progress ?? 0, minHeight: 8)),
          ]),
        ),
      ];
    } else if (_savedNow || z.toSave == 0) {
      buttons = [
        if (!_savedNow) ...[NoteCard(icon: AppIcons.checkCircle, text: t.zipAlreadySaved), const SizedBox(height: 14)],
        FilledButton(onPressed: _close, child: Text(t.zipDone)),
        const SizedBox(height: 12),
        OutlinedButton.icon(onPressed: _openGallery, icon: const Icon(AppIcons.images, size: 20), label: Text(t.zipOpenGallery)),
      ];
    } else {
      final folder = _signedIn ? Services.of(context).folders.sendTo : null;
      buttons = [
        FilledButton.icon(onPressed: () => _saveTo('phone', null), icon: const Icon(AppIcons.download, size: 22), label: Text(t.zipSaveCount(z.toSave))),
        if (_signedIn) ...[
          const SizedBox(height: 12),
          OutlinedButton.icon(
            onPressed: _sendInto,
            icon: const Icon(AppIcons.upload, size: 20),
            label: Text(folder == null ? t.sendInto : t.zipSendInto(folder.name), overflow: TextOverflow.ellipsis),
          ),
        ],
      ];
    }

    return Padding(
      padding: const EdgeInsets.fromLTRB(24, 0, 24, 24),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text(z.name ?? t.zipSeveral(z.zips), style: const TextStyle(fontFamily: serif, fontSize: 25, fontWeight: FontWeight.w700, height: 1.22)),
        const SizedBox(height: 6),
        Text(meta, style: TextStyle(fontSize: 15, color: c.text2)),
        if (set != null && set.parts > 1) ...[
          const SizedBox(height: 16),
          PartChips(states: [
            for (var n = 1; n <= set.parts; n++)
              if (_savedNow && here.contains(n))
                ChipState.now
              else if (set.saved.contains(n))
                ChipState.saved
              else if (here.contains(n))
                (here.length > 1 ? ChipState.here : ChipState.current)
              else
                ChipState.notYet,
          ]),
        ],
        const SizedBox(height: 14),
        Expanded(
          child: GridView.builder(
            padding: EdgeInsets.zero,
            gridDelegate: const SliverGridDelegateWithFixedCrossAxisCount(crossAxisCount: 4, mainAxisSpacing: 3, crossAxisSpacing: 3),
            itemCount: shown.length,
            itemBuilder: (context, i) {
              final f = shown[i];
              final j = z.joinOf(f);
              if (f.readable && f.kind != FileKind.document) unawaited(_thumb(f.index));
              final thumb = _thumbs[f.index];
              return ZipTile(
                name: j?.file ?? f.name,
                kind: j?.kind ?? f.kind,
                picture: thumb?.jpeg,
                durationMs: thumb?.durationMs,
                piece: j != null && !j.whole ? t.zipPiece(f.piece!.number, f.piece!.pieces) : null,
                saved: f.saved,
              );
            },
          ),
        ),
        if (!_savedNow && !saving && z.toSave > 0) ...[
          const SizedBox(height: 12),
          Row(children: [
            Icon(AppIcons.images, size: 18, color: c.text3),
            const SizedBox(width: 10),
            Expanded(child: Text(where, style: TextStyle(fontSize: 14, color: c.text2))),
          ]),
        ],
        for (final n in notes) ...[const SizedBox(height: 12), n],
        const SizedBox(height: 18),
        ...buttons,
      ]),
    );
  }

  Widget _waitingNote(AppLocalizations t, ZipJoinInfo j) {
    final c = context.colors;
    final part = j.missingParts.isEmpty ? 0 : j.missingParts.first;
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(14), border: Border.all(color: c.lineSoft)),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(AppIcons.clock, size: 20, color: c.accentText),
        const SizedBox(width: 12),
        Expanded(
          child: Text.rich(
            TextSpan(children: [
              TextSpan(text: j.kind == FileKind.video ? t.zipWaitsVideo(part) : t.zipWaitsFile(part), style: TextStyle(fontWeight: FontWeight.w600, color: c.text)),
              TextSpan(text: ' ${t.zipWaitsDetail(part, j.file)}'),
            ]),
            style: TextStyle(fontSize: 13.5, height: 1.5, color: c.text2),
          ),
        ),
      ]),
    );
  }

  /// A column that fills the screen, with the buttons at the bottom.
  Widget _page(List<Widget> top, List<Widget> bottom) => LayoutBuilder(
        builder: (context, box) => SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(24, 6, 24, 24),
          child: ConstrainedBox(
            constraints: BoxConstraints(minHeight: box.maxHeight - 30),
            child: IntrinsicHeight(child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [...top, const Spacer(), ...bottom])),
          ),
        ),
      );

  Widget _saved(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final z = _c;
    final s = _save;
    final kinds = z?.files.map((f) => f.kind).toSet() ?? const <FileKind>{};
    final where = !kinds.contains(FileKind.document) ? t.zipSavedInGallery : (kinds.length == 1 ? t.zipSavedInDownloads : t.zipSavedInBoth);
    return _page([
      const SizedBox(height: 40),
      Center(
        child: Container(
          width: 112,
          height: 112,
          decoration: BoxDecoration(color: c.okSoft, shape: BoxShape.circle),
          child: Icon(AppIcons.check, size: 50, color: c.ok),
        ),
      ),
      const SizedBox(height: 28),
      Text(t.zipSavedTitle(s?.saved ?? 0), textAlign: TextAlign.center, style: Theme.of(context).textTheme.headlineMedium),
      Lead(where, center: true),
      if (z != null && z.fromWhatsapp) ...[const SizedBox(height: 22), NoteCard(icon: AppIcons.info, text: t.zipStillInWhatsapp(formatBytes(z.bytes, locale)))],
    ], [
      FilledButton.icon(onPressed: _openGallery, icon: const Icon(AppIcons.images, size: 22), label: Text(t.zipOpenGallery)),
      const SizedBox(height: 12),
      OutlinedButton(onPressed: _close, child: Text(t.zipDone)),
    ]);
  }

  Widget _joined(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final j = _save!.joined.first;
    final set = _c?.set;
    final all = set != null && {...set.saved, ...set.here}.length >= set.parts;
    return _page([
      const SizedBox(height: 40),
      Center(
        child: SizedBox(
          width: 236,
          height: 150,
          child: Stack(clipBehavior: Clip.none, children: [
            Positioned.fill(
              top: 12,
              right: 13,
              child: Container(
                decoration: BoxDecoration(
                  color: ShareColors.tone(j.file),
                  borderRadius: BorderRadius.circular(18),
                  boxShadow: [BoxShadow(color: c.shadow, blurRadius: 36, offset: const Offset(0, 16))],
                ),
                child: Stack(children: [
                  Center(child: Icon(j.kind == FileKind.video ? AppIcons.playCircle : AppIcons.file, size: 48, color: Colors.white.withValues(alpha: 0.78))),
                  Positioned(
                    right: 10,
                    bottom: 10,
                    child: Container(
                      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 3),
                      decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.55), borderRadius: BorderRadius.circular(7)),
                      child: Text(formatBytes(j.total, locale), style: const TextStyle(fontSize: 12.5, fontWeight: FontWeight.w700, color: Colors.white)),
                    ),
                  ),
                ]),
              ),
            ),
            Positioned(
              right: 0,
              top: 0,
              child: Container(
                width: 40,
                height: 40,
                decoration: BoxDecoration(color: c.ok, shape: BoxShape.circle, border: Border.all(color: c.bg, width: 4)),
                child: Icon(AppIcons.check, size: 21, color: c.bg),
              ),
            ),
          ]),
        ),
      ),
      const SizedBox(height: 30),
      Text(j.kind == FileKind.video ? t.zipJoinedVideo : t.zipJoinedFile, textAlign: TextAlign.center, style: Theme.of(context).textTheme.headlineMedium),
      Lead(t.zipJoinedDetail(j.file, formatBytes(j.total, locale), _and(t, j.parts)), center: true),
      if (all) ...[
        const SizedBox(height: 18),
        Row(mainAxisAlignment: MainAxisAlignment.center, children: [
          Icon(AppIcons.check, size: 18, color: c.ok),
          const SizedBox(width: 8),
          Text(t.zipAllPartsIn(set.parts), style: TextStyle(fontSize: 15, color: c.text2)),
        ]),
      ],
    ], [
      FilledButton.icon(onPressed: _openGallery, icon: const Icon(AppIcons.images, size: 22), label: Text(t.zipOpenGallery)),
      const SizedBox(height: 12),
      OutlinedButton(onPressed: _close, child: Text(t.zipDone)),
    ]);
  }

  Widget _failed(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    final s = _save;
    final noRoom = s?.stage == ZipSaveStage.noRoom;
    return _page([
      const SizedBox(height: 24),
      NoteCard(icon: AppIcons.alert, text: noRoom ? t.zipNoRoom(formatBytes(s!.needed, locale), formatBytes(s.free, locale)) : t.commonFailed),
    ], [
      if (noRoom) ...[OutlinedButton(onPressed: _platform.freeUpSpace, child: Text(t.zipFreeUp)), const SizedBox(height: 12)],
      FilledButton(onPressed: () => setState(() => _stage = _Stage.contents), child: Text(t.commonRetry)),
    ]);
  }
}
