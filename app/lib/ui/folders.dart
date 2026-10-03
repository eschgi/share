import 'package:flutter/material.dart';

import '../app.dart';
import '../data/folders.dart';
import '../data/models.dart';
import '../l10n/app_localizations.dart';
import 'format.dart';
import 'icons.dart';
import 'library/tiles.dart';
import 'theme.dart';
import 'widgets.dart';

/// "2,340 files · 41 GB", "2,340 files · 4 people" and the like, for folders.
class FolderLines {
  FolderLines(BuildContext context)
      : _t = AppLocalizations.of(context),
        _locale = Localizations.localeOf(context).languageCode;
  final AppLocalizations _t;
  final String _locale;

  String holds(int files, int bytes) => _t.folderHolds(files, formatBytes(bytes, _locale));
  String count(int files) => _t.folderFiles(files);

  /// Who sees a folder: "4 people", or "Only admins".
  String seen(FolderInfo f) => f.adminsOnly ? _t.folderOnlyAdmins : _t.folderPeople(f.people);

  /// What a folder holds and who sees it: "2,340 files · 4 people", "37 files · only admins".
  String about(FolderInfo f) => '${count(f.files)} · ${f.adminsOnly ? _t.folderOnlyAdminsLine : _t.folderPeople(f.people)}';
}

/// A folder's picture: its newest photo or video, a document for one without pictures, and the
/// library's for all folders (folder null).
class FolderCover extends StatelessWidget {
  const FolderCover({super.key, required this.folder, this.size = 44});
  final FolderInfo? folder;
  final double size;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    final radius = BorderRadius.circular(size * 0.27);
    final cover = folder?.cover;
    if (folder == null) {
      return Container(
        width: size,
        height: size,
        decoration: BoxDecoration(color: c.accentSoft, borderRadius: radius),
        child: Icon(AppIcons.images, size: size * 0.43, color: c.accentText),
      );
    }
    if (cover == null) {
      return Container(
        width: size,
        height: size,
        decoration: BoxDecoration(color: c.s2, borderRadius: radius, border: Border.all(color: c.lineSoft)),
        child: Icon(AppIcons.file, size: size * 0.43, color: c.text2),
      );
    }
    return ClipRRect(borderRadius: radius, child: SizedBox(width: size, height: size, child: ThumbImage(file: cover)));
  }
}

/// The library's title while there is a folder to choose: the folder shown, which opens the
/// choice (screen 38).
class FolderTitle extends StatelessWidget {
  const FolderTitle({super.key, required this.shown, required this.onTap});
  final FolderInfo? shown;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Semantics(
      button: true,
      hint: t.foldersSwitch,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(8),
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          Flexible(child: Text(shown?.name ?? t.foldersAll, overflow: TextOverflow.ellipsis)),
          const SizedBox(width: 6),
          RotatedBox(quarterTurns: 1, child: Icon(AppIcons.chevronRight, size: 22, color: context.colors.text3)),
        ]),
      ),
    );
  }
}

/// Choosing the folder the library shows (screen 39): all folders, then each with its newest
/// picture, what it holds, and a tick on the one shown.
Future<void> showFolderSheet(BuildContext context) => showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => const _FolderSheet(),
    );

class _FolderSheet extends StatelessWidget {
  const _FolderSheet();

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final store = Services.of(context).folders;
    final lines = FolderLines(context);
    return ListenableBuilder(
      listenable: store,
      builder: (context, _) {
        final list = store.list ?? const <FolderInfo>[];
        final all = allTotals(list);
        Widget row(FolderInfo? f, String name, String line) {
          final on = store.shown?.id == f?.id;
          return Semantics(
            selected: on,
            child: SettingsRow(
              leading: FolderCover(folder: f),
              title: name,
              subtitle: line,
              trailing: on ? Icon(AppIcons.check, size: 22, color: c.accentText) : const SizedBox.shrink(),
              onTap: () {
                store.show(f?.id);
                Navigator.pop(context);
              },
            ),
          );
        }

        return SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(8, 4, 8, 14),
                child: Text(t.foldersTitle, style: Theme.of(context).textTheme.headlineSmall),
              ),
              SettingsGroup(children: [
                row(null, t.foldersAll, lines.holds(all.files, all.bytes)),
                for (final f in list) row(f, f.name, lines.holds(f.files, f.bytes)),
              ]),
            ]),
          ),
        );
      },
    );
  }
}

/// A folder picked for something, such as sending (screen 48) or a new PIN (43): its picture,
/// what it holds and who sees it; tapping it chooses another one. On a sheet it takes the
/// sheet's raised [color].
class FolderField extends StatelessWidget {
  const FolderField({super.key, this.label, required this.value, required this.onTap, this.color});
  final String? label;
  final FolderInfo? value;
  final VoidCallback? onTap;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final f = value;
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      if (label != null)
        Padding(
          padding: const EdgeInsets.fromLTRB(4, 0, 4, 8),
          child: Text(label!, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500)),
        ),
      Material(
        color: color ?? c.s1,
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(16), side: BorderSide(color: c.line, width: 1.5)),
        clipBehavior: Clip.antiAlias,
        child: InkWell(
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(10, 10, 14, 10),
            child: Row(children: [
              if (f != null) ...[FolderCover(folder: f), const SizedBox(width: 12)],
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(f?.name ?? t.sendChooseFolder,
                      maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
                  if (f != null) ...[
                    const SizedBox(height: 2),
                    Text(FolderLines(context).about(f), style: TextStyle(fontSize: 13.5, color: c.text2)),
                  ],
                ]),
              ),
              const SizedBox(width: 8),
              RotatedBox(quarterTurns: 1, child: Icon(AppIcons.chevronRight, size: 20, color: c.text3)),
            ]),
          ),
        ),
      ),
    ]);
  }
}

/// Choosing one folder, such as the one sending goes into: each with its picture, what it holds
/// and who sees it. A folder with a [note] can't be chosen. The folder chosen, or null.
Future<String?> showFolderChoice(
  BuildContext context, {
  required String title,
  required List<FolderInfo> list,
  String? value,
  String? Function(FolderInfo f)? note,
}) =>
    showModalBottomSheet<String>(
      context: context,
      isScrollControlled: true,
      builder: (context) {
        final c = context.colors;
        final lines = FolderLines(context);
        return SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Padding(
                padding: const EdgeInsets.fromLTRB(8, 4, 8, 14),
                child: Text(title, style: Theme.of(context).textTheme.headlineSmall),
              ),
              SettingsGroup(children: [
                for (final f in list)
                  if (note?.call(f) case final why?)
                    Opacity(
                      opacity: 0.55,
                      child: SettingsRow(leading: FolderCover(folder: f), title: f.name, subtitle: '$why · ${lines.seen(f)}', trailing: const SizedBox.shrink()),
                    )
                  else
                    Semantics(
                      selected: f.id == value,
                      child: SettingsRow(
                        leading: FolderCover(folder: f),
                        title: f.name,
                        subtitle: lines.about(f),
                        trailing: f.id == value ? Icon(AppIcons.check, size: 22, color: c.accentText) : const SizedBox.shrink(),
                        onTap: () => Navigator.pop(context, f.id),
                      ),
                    ),
              ]),
            ]),
          ),
        );
      },
    );
