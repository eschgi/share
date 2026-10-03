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
