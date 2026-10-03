import 'package:flutter/material.dart';

import '../../data/folders.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../folders.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 49: the folder [count] selected files go into; [here] is where they all are already,
/// if they are in one folder. The folder, or null.
Future<FolderInfo?> showMoveSheet(BuildContext context, {required int count, required List<FolderInfo> list, String? here}) =>
    showModalBottomSheet<FolderInfo>(
      context: context,
      isScrollControlled: true,
      builder: (_) => _MoveSheet(count: count, list: list, here: here),
    );

class _MoveSheet extends StatefulWidget {
  const _MoveSheet({required this.count, required this.list, this.here});
  final int count;
  final List<FolderInfo> list;
  final String? here;

  @override
  State<_MoveSheet> createState() => _MoveSheetState();
}

class _MoveSheetState extends State<_MoveSheet> {
  late String? _to = moveDefault(widget.list, widget.here);

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final lines = FolderLines(context);
    final to = widget.list.where((f) => f.id == _to).firstOrNull;
    return SafeArea(
      child: SingleChildScrollView(
        padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
        child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 4, 8, 6),
            child: Text(t.moveTitle(widget.count), style: Theme.of(context).textTheme.headlineSmall),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(8, 0, 8, 14),
            child: Text(t.moveLead, style: TextStyle(fontSize: 15, height: 1.5, color: c.text2)),
          ),
          SettingsGroup(children: [
            for (final f in widget.list)
              if (f.id == widget.here)
                Opacity(
                  opacity: 0.55,
                  child: SettingsRow(leading: FolderCover(folder: f), title: f.name, subtitle: '${t.moveHereNow} · ${lines.seen(f)}', trailing: const SizedBox.shrink()),
                )
              else
                Semantics(
                  selected: f.id == _to,
                  child: SettingsRow(
                    leading: FolderCover(folder: f),
                    title: f.name,
                    subtitle: lines.seen(f),
                    trailing: f.id == _to ? Icon(AppIcons.check, size: 22, color: c.accentText) : const SizedBox.shrink(),
                    onTap: () => setState(() => _to = f.id),
                  ),
                ),
          ]),
          const SizedBox(height: 16),
          Row(mainAxisAlignment: MainAxisAlignment.end, children: [
            TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonCancel)),
            const SizedBox(width: 8),
            FilledButton.icon(
              style: FilledButton.styleFrom(minimumSize: const Size(0, 50)),
              onPressed: to == null ? null : () => Navigator.pop(context, to),
              icon: const Icon(AppIcons.folder, size: 20),
              label: Text(t.moveTitle(widget.count)),
            ),
          ]),
        ]),
      ),
    );
  }
}
