import 'package:flutter/material.dart';

import '../app.dart';
import '../data/models.dart';
import '../data/platform.dart';
import '../l10n/app_localizations.dart';
import 'format.dart';
import 'icons.dart';
import 'theme.dart';

Future<void> showDownloadSheet(BuildContext context, {required String batch, required List<FileInfo> files}) =>
    showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => DownloadSheet(batch: batch, files: files),
    );

/// Screen 13: saving files to the phone. Photos and videos go into the "Share" album,
/// documents into Downloads/Share; the downloads keep going when the sheet or the app is
/// closed.
class DownloadSheet extends StatelessWidget {
  const DownloadSheet({super.key, required this.batch, required this.files});
  final String batch;
  final List<FileInfo> files;

  @override
  Widget build(BuildContext context) {
    final platform = Services.of(context).platform;
    final media = files.where((f) => f.kind != FileKind.document).length;
    final start = TransferState(
      batch: batch,
      running: true,
      total: files.length,
      done: 0,
      failed: 0,
      skipped: 0,
      bytesTotal: files.fold(0, (s, f) => s + f.size),
      bytesDone: 0,
      media: media,
      documents: files.length - media,
    );
    return StreamBuilder<TransferState>(
      stream: platform.transfers.where((s) => s.batch == batch),
      initialData: start,
      builder: (context, snap) => _Body(state: snap.data!, onCancel: () => platform.cancelDownloads(batch)),
    );
  }
}

class _Body extends StatelessWidget {
  const _Body({required this.state, required this.onCancel});
  final TransferState state;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final s = state;
    final finished = !s.running;
    final progress = s.bytesTotal > 0 ? s.bytesDone / s.bytesTotal : (s.total > 0 ? s.done / s.total : 0.0);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(22, 0, 22, 12),
        child: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          Text(finished ? t.savedTitle(s.done) : t.savingTitle(s.total), style: Theme.of(context).textTheme.headlineSmall),
          const SizedBox(height: 14),
          Row(children: [
            Text(t.savingOf(s.done, s.total), style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
            const Spacer(),
            Text(t.bytesOf(formatBytes(s.bytesDone, locale), formatBytes(s.bytesTotal, locale)),
                style: TextStyle(fontSize: 14, color: c.text2)),
          ]),
          const SizedBox(height: 10),
          ClipRRect(
            borderRadius: BorderRadius.circular(5),
            child: LinearProgressIndicator(value: progress.clamp(0.0, 1.0), minHeight: 8),
          ),
          const SizedBox(height: 10),
          Row(children: [
            Icon(s.local ? AppIcons.wifi : AppIcons.globe, size: 15, color: s.local ? c.ok : c.text3),
            const SizedBox(width: 6),
            Text(s.local ? t.routeLocal : t.routePublic, style: TextStyle(fontSize: 13, color: s.local ? c.ok : c.text3)),
          ]),
          const SizedBox(height: 10),
          if (s.media > 0) _Destination(icon: AppIcons.images, title: t.destPhotos(s.media), where: t.destPhotosWhere),
          if (s.media > 0 && s.documents > 0) Divider(color: c.lineSoft),
          if (s.documents > 0) _Destination(icon: AppIcons.folder, title: t.destDocs(s.documents), where: t.destDocsWhere),
          const SizedBox(height: 12),
          Container(
            padding: const EdgeInsets.all(14),
            decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(16)),
            child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Icon(s.noSpace || s.failed > 0 ? AppIcons.alert : AppIcons.smartphone,
                  size: 20, color: s.noSpace || s.failed > 0 ? c.danger : c.accentText),
              const SizedBox(width: 12),
              Expanded(
                child: Text(
                  [
                    if (s.noSpace) t.savingNoSpace else if (s.failed > 0) t.savingFailed(s.failed),
                    if (!finished) t.savingKeepsGoing,
                    if (s.skipped > 0) t.savingSkipped(s.skipped),
                  ].join(' '),
                  style: TextStyle(fontSize: 14, height: 1.5, color: c.text2),
                ),
              ),
            ]),
          ),
          const SizedBox(height: 18),
          Row(children: [
            if (!finished) ...[
              Expanded(
                child: OutlinedButton(
                  style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(52)),
                  onPressed: () {
                    onCancel();
                    Navigator.pop(context);
                  },
                  child: Text(t.commonCancel),
                ),
              ),
              const SizedBox(width: 12),
            ],
            Expanded(
              child: FilledButton(
                style: FilledButton.styleFrom(
                  minimumSize: const Size.fromHeight(52),
                  backgroundColor: c.accentSoft,
                  foregroundColor: c.accentText,
                ),
                onPressed: () => Navigator.pop(context),
                child: Text(finished ? t.commonClose : t.commonHide),
              ),
            ),
          ]),
        ]),
      ),
    );
  }
}

class _Destination extends StatelessWidget {
  const _Destination({required this.icon, required this.title, required this.where});
  final IconData icon;
  final String title, where;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 11),
      child: Row(children: [
        Container(
          width: 40,
          height: 40,
          decoration: BoxDecoration(color: c.s2, borderRadius: BorderRadius.circular(12)),
          child: Icon(icon, size: 20, color: c.text2),
        ),
        const SizedBox(width: 14),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500)),
            Text(where, style: TextStyle(fontSize: 13, color: c.text3)),
          ]),
        ),
      ]),
    );
  }
}
