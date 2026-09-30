import 'package:clock/clock.dart';
import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../library/tiles.dart';
import '../theme.dart';
import '../widgets.dart';

/// Recently deleted: what admins deleted in the last days, to bring back or remove for good.
class TrashScreen extends StatefulWidget {
  const TrashScreen({super.key});

  @override
  State<TrashScreen> createState() => _TrashScreenState();
}

class _TrashScreenState extends State<TrashScreen> {
  Trash? _trash;
  bool _failed = false, _busy = false;
  final _selected = <String>{};

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final trash = await Services.read(context).admin.trash();
      if (!mounted) return;
      setState(() {
        _trash = trash;
        _failed = false;
        _selected.retainAll(trash.files.map((f) => f.file.id));
      });
    } on Exception {
      if (mounted) setState(() => _failed = true);
    }
  }

  Future<void> _restore() async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _busy = true);
    try {
      final n = await Services.read(context).admin.restore(_selected.toList());
      messenger.showSnackBar(SnackBar(content: Text(t.trashRestored(n))));
      _selected.clear();
    } on Exception {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    await _load();
  }

  Future<void> _purge() async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    final ok = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(t.trashPurgeTitle(_selected.length)),
            content: Text(t.trashPurgeBody, style: TextStyle(color: context.colors.text2, height: 1.5)),
            actions: [
              TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
              TextButton(
                style: TextButton.styleFrom(foregroundColor: context.colors.danger),
                onPressed: () => Navigator.pop(context, true),
                child: Text(t.trashPurge),
              ),
            ],
          ),
        ) ??
        false;
    if (!ok || !mounted) return;
    setState(() => _busy = true);
    try {
      await Services.read(context).admin.purge(_selected.toList());
      _selected.clear();
    } on Exception {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final trash = _trash;
    final files = trash?.files ?? const <TrashedFile>[];
    final selecting = _selected.isNotEmpty;
    return Scaffold(
      appBar: AppBar(
        leading: selecting
            ? IconButton(icon: const Icon(AppIcons.x), onPressed: () => setState(_selected.clear))
            : const ShareBackButton(),
        title: Text(selecting ? t.selectCount(_selected.length) : t.trashTitle,
            style: const TextStyle(fontFamily: 'Roboto', fontSize: 20, fontWeight: FontWeight.w600)),
        actions: [
          if (selecting && _selected.length < files.length)
            TextButton(
              onPressed: () => setState(() => _selected.addAll(files.map((f) => f.file.id))),
              child: Text(t.selectAll),
            ),
        ],
      ),
      bottomNavigationBar: selecting
          ? SafeArea(
              child: Padding(
                padding: const EdgeInsets.fromLTRB(16, 8, 16, 12),
                child: Row(children: [
                  Expanded(
                    child: OutlinedButton(
                      style: OutlinedButton.styleFrom(foregroundColor: c.danger, minimumSize: const Size.fromHeight(54)),
                      onPressed: _busy ? null : _purge,
                      child: Text(t.trashPurge),
                    ),
                  ),
                  const SizedBox(width: 12),
                  Expanded(
                    child: FilledButton.icon(
                      style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(54)),
                      onPressed: _busy ? null : _restore,
                      icon: const Icon(AppIcons.rotate, size: 20),
                      label: Text(t.trashRestore(_selected.length)),
                    ),
                  ),
                ]),
              ),
            )
          : null,
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(padding: const EdgeInsets.fromLTRB(16, 0, 16, 24), children: [
          if (trash != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(4, 0, 4, 12),
              child: Text(t.trashLead(trash.days), style: TextStyle(fontSize: 15.5, height: 1.5, color: c.text2)),
            ),
          if (trash == null && !_failed) const Padding(padding: EdgeInsets.only(top: 80), child: Center(child: CircularProgressIndicator())),
          if (_failed) Help(t.commonOffline),
          if (trash != null && files.isEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 60),
              child: Column(children: [
                Icon(AppIcons.trash, size: 40, color: c.text3),
                const SizedBox(height: 12),
                Text(t.trashEmpty, style: TextStyle(fontSize: 16, color: c.text3)),
              ]),
            ),
          if (files.isNotEmpty)
            SettingsGroup(children: [
              for (final f in files)
                _TrashRow(
                  item: f,
                  selected: _selected.contains(f.file.id),
                  onTap: () => setState(() => _selected.contains(f.file.id) ? _selected.remove(f.file.id) : _selected.add(f.file.id)),
                ),
            ]),
        ]),
      ),
    );
  }
}

class _TrashRow extends StatelessWidget {
  const _TrashRow({required this.item, required this.selected, required this.onTap});
  final TrashedFile item;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final f = item.file;
    final left = item.purgeAt.difference(clock.now()).inDays.clamp(0, 999);
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
        child: Row(children: [
          SizedBox(width: 52, height: 52, child: ClipRRect(borderRadius: BorderRadius.circular(10), child: ThumbImage(file: f))),
          const SizedBox(width: 14),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(f.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w500)),
              const SizedBox(height: 2),
              Text(
                [formatBytes(f.size, locale), t.trashDaysLeft(left), if (item.deletedBy != null) t.trashDeletedBy(item.deletedBy!)].join(' · '),
                maxLines: 2,
                style: TextStyle(fontSize: 13, color: c.text3),
              ),
            ]),
          ),
          const SizedBox(width: 10),
          Container(
            width: 24,
            height: 24,
            decoration: BoxDecoration(
              shape: BoxShape.circle,
              color: selected ? c.accent : null,
              border: Border.all(color: selected ? c.accent : c.text3, width: 2),
            ),
            child: selected ? const Icon(AppIcons.check, size: 14, color: Colors.white) : null,
          ),
        ]),
      ),
    );
  }
}
