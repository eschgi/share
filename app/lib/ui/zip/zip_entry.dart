import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../data/platform.dart';
import '../../l10n/app_localizations.dart';
import '../fetch.dart';
import '../icons.dart';
import '../library/scope.dart';
import '../theme.dart';
import '../widgets.dart';
import 'zip_screen.dart';

/// Picks photos and videos with Android's photo picker, and opens the ZIP screen with them (screen
/// 102 and the first screen): for files on the phone, without the server.
Future<void> zipFromPicker(BuildContext context) async {
  final navigator = Navigator.of(context);
  final messenger = ScaffoldMessenger.of(context);
  final t = AppLocalizations.of(context);
  try {
    final files = await Services.read(context).platform.pickForZip();
    if (files == null || files.isEmpty) return;
    await navigator.push(MaterialPageRoute<void>(builder: (_) => ZipScreen(files: files)));
  } on PlatformException {
    messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
  }
}

/// Library files as a ZIP (screen 101): the ones on the phone are packed from there, the others
/// fetched first, decrypted for an encrypted folder.
Future<void> zipFromLibrary(BuildContext context, List<FileInfo> files, {SendAuth auth = SendAuth.device}) async {
  final services = Services.read(context);
  final library = LibraryScope.read(context);
  final navigator = Navigator.of(context);
  final ids = files.map((f) => f.folder).toSet();
  final folder = ids.length == 1 ? services.folders.list?.where((f) => f.id == ids.single).firstOrNull?.name : null;
  final list = await services.platform.zipLibrary(files, auth: auth);
  if (list.isEmpty) return;
  await navigator.push(MaterialPageRoute<void>(
    builder: (_) => ZipScreen(
      files: list,
      folder: folder,
      encrypted: files.any((f) => f.enc != null),
      thumbs: (i) async => i < files.length && files[i].enc == null && files[i].hasThumb ? library.thumb(files[i]) : null,
    ),
  ));
}

/// Share in the selection and in the viewer (screen 101): as a ZIP in full quality, or one by one as
/// before, when WhatsApp makes photos and videos smaller.
Future<void> shareHow(BuildContext context, List<FileInfo> files, {SendAuth auth = SendAuth.device}) async {
  final platform = Services.read(context).platform;
  final onPhone = await platform.savedIds(files.map((f) => f.id));
  if (!context.mounted) return;
  final how = await showModalBottomSheet<bool>(
    context: context,
    isScrollControlled: true,
    builder: (context) => _ShareHowSheet(count: files.length, fromServer: files.where((f) => !onPhone.contains(f.id)).length),
  );
  if (how == null || !context.mounted) return;
  if (how) {
    await zipFromLibrary(context, files, auth: auth);
  } else {
    await withFetch(context, files.length, () => platform.shareFiles(files, auth: auth));
  }
}

class _ShareHowSheet extends StatelessWidget {
  const _ShareHowSheet({required this.count, required this.fromServer});
  final int count, fromServer;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Padding(
      padding: const EdgeInsets.fromLTRB(22, 0, 22, 28),
      child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text(t.zipShareTitle(count), style: const TextStyle(fontFamily: serif, fontSize: 23, fontWeight: FontWeight.w700)),
        const SizedBox(height: 16),
        ChoiceCard(icon: AppIcons.archive, title: t.zipShareAsZip, detail: t.zipShareAsZipDetail, onTap: () => Navigator.pop(context, true)),
        const SizedBox(height: 10),
        ChoiceCard(icon: AppIcons.images, title: t.zipShareOneByOne, detail: t.zipShareOneByOneDetail, onTap: () => Navigator.pop(context, false)),
        if (fromServer > 0) ...[
          const SizedBox(height: 14),
          NoteCard(icon: AppIcons.server, text: t.zipShareFromServer(fromServer)),
        ],
      ]),
    );
  }
}

/// The card on the Send tab (screen 102): photos and videos in full quality, for WhatsApp and the
/// like, not through the server. The gallery's share sheet has the same.
class ZipCard extends StatelessWidget {
  const ZipCard({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          SettingsRow.icon(context, AppIcons.archive, accent: true),
          const SizedBox(width: 14),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(t.zipCardTitle, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
              const SizedBox(height: 3),
              Text(t.zipCardDetail, style: TextStyle(fontSize: 13.5, height: 1.45, color: c.text2)),
            ]),
          ),
        ]),
        const SizedBox(height: 12),
        Text(t.zipCardGallery, style: TextStyle(fontSize: 14, height: 1.5, color: c.text2)),
        const SizedBox(height: 14),
        // Quieter than the Send tab's picks: the mockup's tonal button.
        FilledButton.icon(
          style: FilledButton.styleFrom(backgroundColor: c.accentSoft, foregroundColor: c.accentText, minimumSize: const Size.fromHeight(48)),
          onPressed: () => zipFromPicker(context),
          icon: const Icon(AppIcons.images, size: 20),
          label: Text(t.zipCardPick),
        ),
      ]),
    );
  }
}
