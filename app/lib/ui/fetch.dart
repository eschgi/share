import 'dart:async';

import 'package:flutter/material.dart';

import '../app.dart';
import '../data/platform.dart';
import '../l10n/app_localizations.dart';
import 'format.dart';
import 'theme.dart';

/// Runs [action] (sharing or opening files), which first fetches them onto the phone. When
/// that takes more than a moment, a dialog shows how far it is and can stop it.
Future<void> withFetch(BuildContext context, int count, Future<void> Function() action) async {
  final t = AppLocalizations.of(context);
  final platform = Services.read(context).platform;
  final navigator = Navigator.of(context);
  final messenger = ScaffoldMessenger.of(context);
  var shown = false;
  final timer = Timer(const Duration(milliseconds: 400), () {
    shown = true;
    showDialog<void>(
      context: context,
      barrierDismissible: false,
      builder: (_) => FetchDialog(count: count, onCancel: () => platform.cancelDownloads(fetchBatch)),
    );
  });
  try {
    await action();
  } on OpenFailed catch (e) {
    messenger.showSnackBar(SnackBar(content: Text(e.noApp ? t.openNoApp : t.fetchFailed)));
  } finally {
    timer.cancel();
    if (shown) navigator.pop();
  }
}

class FetchDialog extends StatelessWidget {
  const FetchDialog({super.key, required this.count, required this.onCancel});
  final int count;
  final VoidCallback onCancel;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    return AlertDialog(
      title: Text(t.fetchTitle(count)),
      content: StreamBuilder<TransferState>(
        stream: Services.of(context).platform.transfers.where((s) => s.batch == fetchBatch),
        builder: (context, snap) {
          final s = snap.data;
          final progress = s == null || s.bytesTotal == 0 ? null : (s.bytesDone / s.bytesTotal).clamp(0.0, 1.0);
          return Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
            ClipRRect(
              borderRadius: BorderRadius.circular(5),
              child: LinearProgressIndicator(value: progress, minHeight: 8),
            ),
            const SizedBox(height: 10),
            Text(
              s == null ? ' ' : t.bytesOf(formatBytes(s.bytesDone, locale), formatBytes(s.bytesTotal, locale)),
              style: TextStyle(fontSize: 14, color: c.text2),
            ),
          ]);
        },
      ),
      actions: [TextButton(onPressed: onCancel, child: Text(t.commonCancel))],
    );
  }
}
