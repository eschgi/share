import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../data/platform.dart';
import '../../data/zip.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';
import 'zip_widgets.dart';

/// Screens 98 to 100 and 104 (docs/zip-plan.md): a ZIP's name and where it goes, packing, and handing
/// what was packed to another app. All of it happens on the phone, without the server, except for
/// library files that aren't on the phone yet. Opened from another app's share sheet ([fromOutside]),
/// closing goes back there.
class ZipScreen extends StatefulWidget {
  const ZipScreen({super.key, required this.files, this.folder, this.thumbs, this.skipped = 0, this.encrypted = false, this.fromOutside = false});
  final List<ZipFileInfo> files;

  /// The library folder the files come from, for the name.
  final String? folder;

  /// Pictures of the files; without it, the phone's own (zip.thumb).
  final Future<Uint8List?> Function(int index)? thumbs;

  /// Files another app lent that couldn't be read.
  final int skipped;

  /// Files from an encrypted folder: the ZIP isn't encrypted, and the screen says so.
  final bool encrypted;
  final bool fromOutside;

  @override
  State<ZipScreen> createState() => _ZipScreenState();
}

enum _Stage { setup, packing, ready, failed }

class _ZipScreenState extends State<ZipScreen> {
  final _name = TextEditingController();
  String _proposal = '';
  ZipWhere _where = ZipWhere.whatsapp;
  int _otherMb = 500;
  ZipPlanInfo? _plan;
  int _planned = 0;
  _Stage _stage = _Stage.setup;
  ZipPackState? _pack;
  String _packedName = '';
  final _sent = <int, String?>{};
  List<Uint8List?> _pictures = const [];
  StreamSubscription<ZipPackState>? _packs;
  StreamSubscription<ZipSent>? _sends;

  Platform get _platform => Services.read(context).platform;

  @override
  void initState() {
    super.initState();
    _packs = _platform.zipPacking.listen(_packed);
    _sends = _platform.zipSent.listen((s) {
      if (mounted) setState(() => _sent[s.part] = s.app);
    });
    WidgetsBinding.instance.addPostFrameCallback((_) => _start());
  }

  @override
  void dispose() {
    _packs?.cancel();
    _sends?.cancel();
    _name.dispose();
    super.dispose();
  }

  Future<void> _start() async {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    _proposal = zipName(
      kinds: widget.files.map((f) => f.kind),
      days: widget.files.map((f) => f.taken),
      locale: locale,
      words: (photos: t.zipNamePhotos, videos: t.zipNameVideos, documents: t.zipNameDocuments, files: t.zipNameFiles),
      folder: widget.folder,
    );
    _name.text = _proposal;
    final saved = ZipWhere.parse(await _platform.readSecret('zip_where'));
    if (!mounted) return;
    setState(() {
      _where = saved.where;
      _otherMb = saved.otherMb;
    });
    unawaited(_replan());
    unawaited(_loadPictures());
  }

  /// The pictures of the first three photos or videos, for the prints.
  Future<void> _loadPictures() async {
    final thumbs = widget.thumbs ?? _platform.zipThumb;
    final media = [for (final (i, f) in widget.files.indexed) if (f.kind != FileKind.document) i].take(3).toList();
    final pictures = <Uint8List?>[];
    for (final i in media) {
      pictures.add(await thumbs(i).catchError((Object _) => null));
    }
    if (mounted) setState(() => _pictures = pictures);
  }

  String get _finalName => cleanZipName(_name.text).isEmpty ? _proposal : cleanZipName(_name.text);

  Future<void> _replan() async {
    final seq = ++_planned;
    final plan = await _platform.zipPlan(name: _finalName, about: AppLocalizations.of(context).zipAbout, limit: _where.bytes(_otherMb));
    if (mounted && seq == _planned) setState(() => _plan = plan);
  }

  Future<void> _chooseWhere() async {
    final t = AppLocalizations.of(context);
    final chosen = await showModalBottomSheet<({ZipWhere where, int otherMb})>(
      context: context,
      isScrollControlled: true,
      builder: (_) => ZipWhereSheet(
        where: _where,
        otherMb: _otherMb,
        plan: (limit) => _platform.zipPlan(name: _finalName, about: t.zipAbout, limit: limit),
      ),
    );
    if (chosen == null || !mounted) return;
    setState(() {
      _where = chosen.where;
      _otherMb = chosen.otherMb;
      _plan = null;
    });
    await _platform.writeSecret('zip_where', _where.save(_otherMb));
    await _replan();
  }

  Future<void> _packNow() async {
    final t = AppLocalizations.of(context);
    setState(() {
      _packedName = _finalName;
      _stage = _Stage.packing;
      _pack = null;
      _sent.clear();
    });
    await _platform.writeSecret('zip_where', _where.save(_otherMb));
    await _platform.zipPack(name: _packedName, about: t.zipAbout, partName: t.zipPartName('{name}', '{part}', '{parts}'), limit: _where.bytes(_otherMb));
  }

  void _packed(ZipPackState s) {
    if (!mounted || _stage != _Stage.packing) return;
    setState(() {
      _pack = s;
      _stage = switch (s.stage) {
        ZipPackStage.packing => _Stage.packing,
        ZipPackStage.ready => _Stage.ready,
        ZipPackStage.stopped => _Stage.setup,
        ZipPackStage.failed || ZipPackStage.noRoom => _Stage.failed,
      };
    });
  }

  Future<void> _send({int? part}) async {
    final t = AppLocalizations.of(context);
    try {
      await _platform.zipSend(part: part);
    } on OpenFailed {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(t.openNoApp)));
    }
  }

  Future<void> _saveToDownloads() async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final n = await _platform.zipSaveToDownloads();
      messenger.showSnackBar(SnackBar(content: Text(t.zipSavedDownloads(n))));
    } on PlatformException {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    }
  }

  Future<void> _close() async {
    final platform = _platform;
    final navigator = Navigator.of(context);
    if (_stage == _Stage.packing) await platform.zipStop();
    await platform.zipClose();
    if (!mounted) return;
    navigator.pop();
    if (widget.fromOutside) await SystemNavigator.pop();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) {
        if (!didPop) _close();
      },
      child: Scaffold(
        appBar: AppBar(
          leading: IconButton(tooltip: MaterialLocalizations.of(context).closeButtonTooltip, icon: const Icon(AppIcons.x, size: 24), onPressed: _close),
          title: _stage == _Stage.setup ? Text(t.zipCardTitle) : null,
        ),
        body: SafeArea(
          top: false,
          child: switch (_stage) {
            _Stage.setup => _setup(context),
            _Stage.packing => _packing(context),
            _Stage.ready => (_pack?.plan?.parts.length ?? 1) > 1 ? _parts(context) : _readyOne(context),
            _Stage.failed => _failed(context),
          },
        ),
      ),
    );
  }

  /// A column that fills the screen, with the buttons at the bottom, and scrolls when it can't.
  Widget _page(List<Widget> top, List<Widget> bottom) => LayoutBuilder(
        builder: (context, box) => SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(24, 6, 24, 24),
          child: ConstrainedBox(
            constraints: BoxConstraints(minHeight: box.maxHeight - 30),
            child: IntrinsicHeight(
              child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [...top, const Spacer(), ...bottom]),
            ),
          ),
        ),
      );

  Widget _setup(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final plan = _plan;
    final total = widget.files.fold(0, (s, f) => s + f.size);
    final fromServer = widget.files.where((f) => !f.onPhone).length;
    final parts = plan?.parts.length ?? 1;
    return _page([
      ZipStack(pictures: _pictures, height: 160),
      const SizedBox(height: 12),
      Text(zipMeta(t, locale, widget.files.map((f) => f.kind), total), textAlign: TextAlign.center, style: TextStyle(fontSize: 15, color: c.text2)),
      if (widget.skipped > 0) ...[const SizedBox(height: 14), NoteCard(icon: AppIcons.alert, text: t.zipSkipped(widget.skipped))],
      FieldLabel(t.zipNameLabel),
      TextField(
        controller: _name,
        maxLength: 80,
        textCapitalization: TextCapitalization.sentences,
        decoration: const InputDecoration(suffixText: '.zip', counterText: ''),
        onSubmitted: (_) => _replan(),
      ),
      Help(t.zipNameHelp),
      FieldLabel(t.zipWhereLabel),
      _WhereRow(where: _where, line: _whereLine(t, locale), onChange: _chooseWhere),
      if (fromServer > 0) ...[const SizedBox(height: 14), NoteCard(icon: AppIcons.server, text: t.zipShareFromServer(fromServer))],
      if (widget.encrypted) ...[const SizedBox(height: 14), NoteCard(icon: AppIcons.lockOpen, text: t.zipShareEncrypted)],
    ], [
      const SizedBox(height: 18),
      NoteCard(icon: AppIcons.smartphone, text: t.zipNote),
      const SizedBox(height: 14),
      FilledButton.icon(
        onPressed: plan == null || plan.tooMany || widget.files.isEmpty ? null : _packNow,
        icon: const Icon(AppIcons.archive, size: 22),
        label: Text(parts > 1 ? t.zipPackMany(parts) : t.zipPackOne),
      ),
      Small(widget.files.isEmpty ? t.zipNothing : t.zipAsTheyAre),
    ]);
  }

  String _whereLine(AppLocalizations t, String locale) {
    final plan = _plan;
    if (_where.bytes(_otherMb) == null) return t.zipWhereAnySize;
    final size = zipLimitLabel(_where, _otherMb, locale);
    if (plan == null) return t.zipWhereUpTo(size);
    if (plan.tooMany) return t.zipWhereTooMany;
    if (plan.parts.length <= 1) return t.zipWhereOne(size);
    return t.zipWhereMany(size, plan.parts.length) + (plan.cut.isEmpty ? '' : t.zipWhereCut(plan.cut.length));
  }

  Widget _title(String text) => Text(text, textAlign: TextAlign.center, style: const TextStyle(fontFamily: serif, fontSize: 25, fontWeight: FontWeight.w700, height: 1.22));

  Widget _packing(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final s = _pack;
    final total = widget.files.fold(0, (sum, f) => sum + f.size);
    final progress = s?.progress;
    return _page([
      const SizedBox(height: 14),
      ZipStack(pictures: _pictures),
      const SizedBox(height: 26),
      _title('$_packedName.zip'),
      const SizedBox(height: 6),
      Text('${t.zipCountFiles(widget.files.length)} · ${formatBytes(total, locale)}', textAlign: TextAlign.center, style: TextStyle(fontSize: 15, color: c.text2)),
    ], [
      const SizedBox(height: 24),
      Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Row(children: [
            Expanded(child: Text(t.zipPacking(((s?.filesDone ?? 0) + 1).clamp(1, widget.files.length), widget.files.length), style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600))),
            if (progress != null) Text('${(progress * 100).floor()}%', style: TextStyle(fontSize: 16, fontWeight: FontWeight.w600, color: c.accentText)),
          ]),
          const SizedBox(height: 12),
          ClipRRect(borderRadius: BorderRadius.circular(5), child: LinearProgressIndicator(value: progress ?? 0, minHeight: 8)),
          if (s != null && s.parts > 1) ...[const SizedBox(height: 10), Text(t.zipPartOfParts(s.part.clamp(1, s.parts), s.parts), style: TextStyle(fontSize: 14, color: c.text3))],
          if (s?.fetching != null) ...[const SizedBox(height: 10), Text(t.zipFetching(s!.fetching!), style: TextStyle(fontSize: 14, color: c.text3))],
          const SizedBox(height: 12),
          Text(t.zipAsTheyAre, style: TextStyle(fontSize: 14.5, height: 1.5, color: c.text2)),
        ]),
      ),
      const SizedBox(height: 6),
      TextButton(onPressed: _platform.zipStop, child: Text(t.zipStop)),
    ]);
  }

  String _fits(AppLocalizations t, String locale, int bytes) => switch (_where) {
        ZipWhere.whatsapp => bytes <= ZipWhere.email.limit! ? t.zipFitsWhatsappAndEmail : t.zipFitsWhatsapp,
        ZipWhere.signal => t.zipFitsSignal,
        ZipWhere.email => t.zipFitsEmail,
        ZipWhere.any => t.zipFitsAny(formatBytes(bytes, locale)),
        ZipWhere.other => t.zipFitsOther(formatBytes(_otherMb * 1000000, locale)),
      };

  Widget _readyOne(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final bytes = _pack?.plan?.bytes ?? 0;
    return _page([
      const SizedBox(height: 14),
      ZipStack(pictures: _pictures, zip: true),
      const SizedBox(height: 26),
      _title('$_packedName.zip'),
      const SizedBox(height: 6),
      Text('${t.zipCountFiles(widget.files.length)} · ${formatBytes(bytes, locale)}', textAlign: TextAlign.center, style: TextStyle(fontSize: 15, color: c.text2)),
    ], [
      const SizedBox(height: 24),
      NoteCard(icon: AppIcons.info, text: _fits(t, locale, bytes)),
      const SizedBox(height: 14),
      FilledButton.icon(onPressed: _send, icon: const Icon(AppIcons.send, size: 22), label: Text(t.zipSendOne)),
      const SizedBox(height: 4),
      TextButton(onPressed: _saveToDownloads, child: Text(t.zipSaveDownloads)),
    ]);
  }

  Widget _parts(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final plan = _pack!.plan!;
    final n = plan.parts.length;
    String line(ZipPartInfo p) {
      final size = formatBytes(p.bytes, locale);
      if (p.pieces.isEmpty) return t.zipPartFiles(p.files, size);
      return p.pieces.length == 1 ? t.zipPartFilesPiece(p.files, size) : t.zipPartFilesPieces(p.files, size);
    }

    final cut = plan.cut;
    final video = cut.length == 1 && widget.files.where((f) => f.name == cut.single.name).firstOrNull?.kind == FileKind.video;
    return ListView(padding: const EdgeInsets.fromLTRB(24, 6, 24, 24), children: [
      Text(_packedName, style: const TextStyle(fontFamily: serif, fontSize: 25, fontWeight: FontWeight.w700, height: 1.22)),
      const SizedBox(height: 6),
      Text(t.zipPartsMeta(widget.files.length, n, formatBytes(plan.bytes, locale)), style: TextStyle(fontSize: 15, color: c.text2)),
      const SizedBox(height: 18),
      SettingsGroup(children: [
        for (final p in plan.parts)
          SettingsRow(
            leading: SettingsRow.icon(context, AppIcons.archive, accent: true),
            title: t.zipPartOfParts(p.number, n),
            subtitle: line(p),
            trailing: _sent.containsKey(p.number) || _sent.containsKey(0)
                ? Text((_sent[p.number] ?? _sent[0]) == null ? t.zipPartSent : t.zipPartSentTo((_sent[p.number] ?? _sent[0])!), style: TextStyle(fontSize: 13, color: c.ok))
                : IconButton(tooltip: t.zipSendPart(p.number), icon: Icon(AppIcons.send, size: 22, color: c.text2), onPressed: () => _send(part: p.number)),
          ),
      ]),
      if (cut.isNotEmpty) ...[
        const SizedBox(height: 18),
        _TitledNote(
          icon: AppIcons.scissors,
          title: cut.length == 1 ? (video ? t.zipCutVideo(cut.single.parts.length) : t.zipCutFile(cut.single.parts.length)) : t.zipCutMany(cut.length),
          text: cut.length == 1 ? t.zipCutDetailOne(cut.single.name, formatBytes(cut.single.size, locale)) : t.zipCutDetailMany,
        ),
      ],
      const SizedBox(height: 24),
      FilledButton.icon(onPressed: _send, icon: const Icon(AppIcons.send, size: 22), label: Text(t.zipSendMany(n))),
      const SizedBox(height: 4),
      TextButton(onPressed: _saveToDownloads, child: Text(t.zipSaveDownloads)),
    ]);
  }

  Widget _failed(BuildContext context) {
    final t = AppLocalizations.of(context);
    final locale = Localizations.localeOf(context).languageCode;
    final s = _pack;
    final noRoom = s?.stage == ZipPackStage.noRoom;
    final text = noRoom
        ? t.zipNoRoom(formatBytes(s!.needed, locale), formatBytes(s.free, locale))
        : s?.reason == 'changed' && s?.failedFile != null
            ? t.zipChanged(s!.failedFile!)
            : t.zipPackFailed;
    return _page([
      const SizedBox(height: 14),
      ZipStack(pictures: _pictures),
      const SizedBox(height: 26),
      NoteCard(icon: AppIcons.alert, text: text),
    ], [
      if (noRoom) ...[OutlinedButton(onPressed: _platform.freeUpSpace, child: Text(t.zipFreeUp)), const SizedBox(height: 12)],
      FilledButton(onPressed: () => setState(() => _stage = _Stage.setup), child: Text(t.commonRetry)),
    ]);
  }
}

/// How big each ZIP may be, as the screens say it: an app's own limit, whatever margin Share keeps
/// under it, or the size typed; a round size without its ".0": 2 GB, 500 MB.
String zipLimitLabel(ZipWhere where, int otherMb, String locale) {
  String round(int bytes) => formatBytes(bytes, locale).replaceFirst(RegExp(r'[.,]0 '), ' ');
  return switch (where) {
    ZipWhere.whatsapp => round(2000000000),
    ZipWhere.signal => round(100000000),
    ZipWhere.email => round(ZipWhere.email.limit!),
    ZipWhere.any => '',
    ZipWhere.other => round(otherMb * 1000000),
  };
}

IconData _whereIcon(ZipWhere where) => switch (where) {
      ZipWhere.whatsapp || ZipWhere.signal => AppIcons.message,
      ZipWhere.email => AppIcons.mail,
      ZipWhere.any => AppIcons.monitor,
      ZipWhere.other => AppIcons.archive,
    };

String _whereName(AppLocalizations t, ZipWhere where) => switch (where) {
      ZipWhere.whatsapp => t.zipWhereWhatsapp,
      ZipWhere.signal => t.zipWhereSignal,
      ZipWhere.email => t.zipWhereEmail,
      ZipWhere.any => t.zipWhereAny,
      ZipWhere.other => t.zipWhereOther,
    };

/// Where it goes, with Change (screen 98).
class _WhereRow extends StatelessWidget {
  const _WhereRow({required this.where, required this.line, required this.onChange});
  final ZipWhere where;
  final String line;
  final VoidCallback onChange;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return Material(
      color: c.s1,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16), side: BorderSide(color: c.line, width: 1.5)),
      child: InkWell(
        borderRadius: BorderRadius.circular(16),
        onTap: onChange,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(10, 10, 6, 10),
          child: Row(children: [
            Container(
              width: 44,
              height: 44,
              decoration: BoxDecoration(color: c.accentSoft, borderRadius: BorderRadius.circular(12)),
              child: Icon(_whereIcon(where), size: 20, color: c.accentText),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(_whereName(t, where), style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600)),
                const SizedBox(height: 2),
                Text(line, style: TextStyle(fontSize: 13, color: c.text3)),
              ]),
            ),
            TextButton(onPressed: onChange, child: Text(t.zipChange)),
          ]),
        ),
      ),
    );
  }
}

/// A note with a bold first sentence, as the cut file's (screen 104).
class _TitledNote extends StatelessWidget {
  const _TitledNote({required this.icon, required this.title, required this.text});
  final IconData icon;
  final String title, text;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(icon, size: 22, color: c.accentText),
        const SizedBox(width: 14),
        Expanded(
          child: Text.rich(
            TextSpan(children: [
              TextSpan(text: title, style: TextStyle(fontWeight: FontWeight.w600, color: c.text)),
              TextSpan(text: ' $text'),
            ]),
            style: TextStyle(fontSize: 15, height: 1.5, color: c.text2),
          ),
        ),
      ]),
    );
  }
}

/// Screen 103: where a ZIP goes, which sets how big each may be. Each choice says what it makes of
/// these files, so nobody needs to know sizes; one that makes more than 100 ZIPs can't be picked.
/// Somewhere else takes any size in MB.
class ZipWhereSheet extends StatefulWidget {
  const ZipWhereSheet({super.key, required this.where, required this.otherMb, required this.plan});
  final ZipWhere where;
  final int otherMb;
  final Future<ZipPlanInfo> Function(int? limit) plan;

  @override
  State<ZipWhereSheet> createState() => _ZipWhereSheetState();
}

class _ZipWhereSheetState extends State<ZipWhereSheet> {
  final _counts = <ZipWhere, ZipPlanInfo>{};
  late final _mb = TextEditingController(text: '${widget.otherMb}');
  Timer? _typing;

  int get _otherMb => int.tryParse(_mb.text.trim()) ?? 0;
  bool get _otherOk => _otherMb >= ZipWhere.minMb && _otherMb <= ZipWhere.maxMb;

  @override
  void initState() {
    super.initState();
    for (final w in ZipWhere.values) {
      _count(w);
    }
  }

  @override
  void dispose() {
    _typing?.cancel();
    _mb.dispose();
    super.dispose();
  }

  Future<void> _count(ZipWhere w) async {
    if (w == ZipWhere.other && !_otherOk) {
      setState(() => _counts.remove(w));
      return;
    }
    final mb = _otherMb;
    final plan = await widget.plan(w.bytes(mb));
    if (mounted && (w != ZipWhere.other || mb == _otherMb)) setState(() => _counts[w] = plan);
  }

  void _pick(ZipWhere w) {
    final plan = _counts[w];
    if (plan != null && plan.tooMany) return;
    if (w == ZipWhere.other && !_otherOk) return;
    Navigator.pop(context, (where: w, otherMb: w == ZipWhere.other ? _otherMb : widget.otherMb));
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    Widget row(ZipWhere w) {
      final plan = _counts[w];
      final tooMany = plan?.tooMany ?? false;
      final on = w == widget.where;
      final count = plan == null ? '' : (tooMany ? t.zipWhereTooManyShort : t.zipWhereCount(plan.parts.length));
      final Widget detail = switch (w) {
        ZipWhere.any => Text(t.zipWhereAnySize, style: TextStyle(fontSize: 13, color: c.text3)),
        ZipWhere.other => Row(children: [
            Text(t.zipWhereOtherBefore, style: TextStyle(fontSize: 13, color: c.text3)),
            const SizedBox(width: 6),
            SizedBox(
              width: 72,
              height: 34,
              child: TextField(
                controller: _mb,
                keyboardType: TextInputType.number,
                inputFormatters: [FilteringTextInputFormatter.digitsOnly, LengthLimitingTextInputFormatter(4)],
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600),
                decoration: const InputDecoration(isDense: true, contentPadding: EdgeInsets.symmetric(horizontal: 6, vertical: 8)),
                onChanged: (_) {
                  _typing?.cancel();
                  _typing = Timer(const Duration(milliseconds: 300), () => _count(ZipWhere.other));
                  setState(() {});
                },
                onSubmitted: (_) => _pick(ZipWhere.other),
              ),
            ),
            const SizedBox(width: 6),
            Text(t.zipWhereOtherAfter, style: TextStyle(fontSize: 13, color: c.text3)),
          ]),
        _ => Text(t.zipWhereUpTo(zipLimitLabel(w, widget.otherMb, locale)), style: TextStyle(fontSize: 13, color: c.text3)),
      };
      return Opacity(
        opacity: tooMany ? 0.45 : 1,
        child: Material(
          color: on ? c.accentSoft : Colors.transparent,
          child: InkWell(
            onTap: tooMany ? null : () => _pick(w),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
              child: Row(children: [
                Container(
                  width: 20,
                  height: 20,
                  decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: on ? c.accentText : c.text3, width: 2)),
                  child: on ? Center(child: Container(width: 9, height: 9, decoration: BoxDecoration(shape: BoxShape.circle, color: c.accentText))) : null,
                ),
                const SizedBox(width: 14),
                Expanded(
                  child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                    Text(_whereName(t, w), style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600, color: on ? c.accentText : c.text)),
                    const SizedBox(height: 3),
                    detail,
                  ]),
                ),
                const SizedBox(width: 8),
                Text(count, style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: on ? c.accentText : c.text2)),
              ]),
            ),
          ),
        ),
      );
    }

    return Padding(
      padding: EdgeInsets.fromLTRB(22, 0, 22, 24 + MediaQuery.viewInsetsOf(context).bottom),
      child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text(t.zipWhereTitle, style: const TextStyle(fontFamily: serif, fontSize: 23, fontWeight: FontWeight.w700)),
        const SizedBox(height: 14),
        Container(
          clipBehavior: Clip.antiAlias,
          decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(18)),
          child: Column(children: [
            for (final (i, w) in ZipWhere.values.indexed) ...[
              if (i > 0) Divider(height: 1, color: c.lineSoft),
              row(w),
            ],
          ]),
        ),
        Help(t.zipWhereRemembers),
      ]),
    );
  }
}
