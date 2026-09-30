import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../data/platform.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 15's body: picking photos, videos or other files, and how sending them goes. The
/// sending itself happens in Kotlin and keeps going when the app is closed.
class SendPanel extends StatefulWidget {
  const SendPanel({super.key, required this.auth, this.onNewPin});
  final SendAuth auth;

  /// With a PIN that ended: asks for a new one.
  final VoidCallback? onNewPin;

  @override
  State<SendPanel> createState() => _SendPanelState();
}

class _SendPanelState extends State<SendPanel> {
  StreamSubscription<UploadState>? _sub;
  UploadState? _state; // the newest batch of this kind of sending
  bool _picking = false;

  @override
  void initState() {
    super.initState();
    _sub = Services.read(context).platform.uploads.where((u) => u.auth == widget.auth).listen((u) {
      final shown = _state;
      // The batch that's sending wins; otherwise the newest one.
      if (shown == null || shown.batch == u.batch || !shown.running || u.running) setState(() => _state = u);
    });
  }

  @override
  void dispose() {
    _sub?.cancel();
    super.dispose();
  }

  Future<void> _pick(PickWhat what) async {
    if (_picking) return;
    setState(() => _picking = true);
    try {
      await Services.read(context).platform.pickAndSend(what, auth: widget.auth);
    } on PlatformException {
      if (mounted) ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(AppLocalizations.of(context).commonFailed)));
    } finally {
      if (mounted) setState(() => _picking = false);
    }
  }

  Future<void> _stop(UploadState s) async {
    final t = AppLocalizations.of(context);
    final ok = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(t.sendStopTitle),
            content: Text(t.sendStopBody, style: TextStyle(color: context.colors.text2, height: 1.5)),
            actions: [
              TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
              TextButton(
                style: TextButton.styleFrom(foregroundColor: context.colors.danger),
                onPressed: () => Navigator.pop(context, true),
                child: Text(t.sendStop),
              ),
            ],
          ),
        ) ??
        false;
    if (ok && mounted) await Services.read(context).platform.cancelUpload(s.batch);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final s = _state;
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      Row(children: [
        Expanded(child: _PickCard(icon: AppIcons.images, label: t.sendPhotos, onTap: () => _pick(PickWhat.media))),
        const SizedBox(width: 12),
        Expanded(child: _PickCard(icon: AppIcons.fileText, label: t.sendOtherFiles, onTap: () => _pick(PickWhat.documents))),
      ]),
      if (s != null && s.total > 0) ...[
        const SizedBox(height: 16),
        _Progress(
          state: s,
          onStop: () => _stop(s),
          onPickAgain: () => _pick(s.items.any((i) => i.state == 'lost' && i.kind == FileKind.document) ? PickWhat.documents : PickWhat.media),
          onNewPin: widget.onNewPin,
          onGoOn: () => Services.read(context).platform.resumeUploads(widget.auth),
        ),
      ],
      if (s == null || s.running) ...[
        const SizedBox(height: 16),
        NoteCard(icon: AppIcons.smartphone, text: t.sendKeepsGoing),
      ],
    ]);
  }
}

class _PickCard extends StatelessWidget {
  const _PickCard({required this.icon, required this.label, required this.onTap});
  final IconData icon;
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Material(
      color: c.s1,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(22), side: BorderSide(color: c.line)),
      child: InkWell(
        borderRadius: BorderRadius.circular(22),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(16, 18, 12, 16),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Icon(icon, size: 30, color: c.accentText),
            const SizedBox(height: 30),
            Text(label, maxLines: 2, style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
          ]),
        ),
      ),
    );
  }
}

class _Progress extends StatelessWidget {
  const _Progress({required this.state, required this.onStop, required this.onPickAgain, required this.onGoOn, this.onNewPin});
  final UploadState state;
  final VoidCallback onStop, onPickAgain, onGoOn;
  final VoidCallback? onNewPin;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final s = state;
    final progress = s.bytesTotal > 0 ? (s.bytesDone / s.bytesTotal).clamp(0.0, 1.0) : 0.0;
    final eta = s.etaSeconds;
    return Container(
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 8),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(22), border: Border.all(color: c.lineSoft)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(crossAxisAlignment: CrossAxisAlignment.baseline, textBaseline: TextBaseline.alphabetic, children: [
          Expanded(
            child: Text(s.running ? t.sendingOf(s.current, s.total) : t.sentCount(s.done),
                style: const TextStyle(fontSize: 17, fontWeight: FontWeight.w600)),
          ),
          if (s.running && eta != null) Text(t.sendMinutesLeft((eta / 60).round()), style: TextStyle(fontSize: 14.5, color: c.text2)),
        ]),
        const SizedBox(height: 12),
        ClipRRect(borderRadius: BorderRadius.circular(5), child: LinearProgressIndicator(value: progress, minHeight: 8)),
        if (s.auth == SendAuth.device) ...[
          const SizedBox(height: 10),
          Row(children: [
            Icon(s.local ? AppIcons.wifi : AppIcons.globe, size: 16, color: s.local ? c.ok : c.text3),
            const SizedBox(width: 8),
            Text(s.local ? t.routeLocal : t.routePublic, style: TextStyle(fontSize: 13.5, color: c.text3)),
          ]),
        ],
        const SizedBox(height: 6),
        for (final (i, item) in s.items.indexed) ...[
          if (i > 0) Divider(color: c.lineSoft, height: 1),
          _ItemRow(item: item, locale: locale),
        ],
        if (s.paused == 'pin_ended')
          _Notice(icon: AppIcons.lock, text: t.sendPinEnded, action: onNewPin == null ? null : (t.sendNewPin, onNewPin!))
        else if (s.paused == 'signed_out')
          _Notice(icon: AppIcons.logOut, text: t.sendSignedOut)
        else if (s.paused != null)
          _Notice(icon: AppIcons.clock, text: t.sendPaused, action: (t.sendGoOn, onGoOn)),
        if (s.lost > 0) _Notice(icon: AppIcons.alert, text: t.sendLostCount(s.lost), action: (t.sendPickAgain, onPickAgain)),
        if (s.failed > 0) _Notice(icon: AppIcons.alert, text: t.sendFailedCount(s.failed), danger: true),
        if (s.running)
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(style: TextButton.styleFrom(foregroundColor: c.text2), onPressed: onStop, child: Text(t.sendStop)),
          )
        else
          const SizedBox(height: 8),
      ]),
    );
  }
}

class _ItemRow extends StatelessWidget {
  const _ItemRow({required this.item, required this.locale});
  final UploadItemState item;
  final String locale;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final sending = item.queued && item.bytes > 0;
    final fraction = item.size > 0 ? (item.bytes / item.size).clamp(0.0, 1.0) : 0.0;
    final Widget trailing = switch (item.state) {
      'done' => Container(
          width: 30,
          height: 30,
          decoration: BoxDecoration(color: c.okStrong, shape: BoxShape.circle),
          child: const Icon(AppIcons.check, size: 18, color: Colors.white),
        ),
      'failed' => Text(t.sendItemFailed, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: c.danger)),
      'lost' => Text(t.sendItemLost, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: c.warn)),
      _ when sending => Text('${(fraction * 100).floor()}%', style: TextStyle(fontSize: 15, fontWeight: FontWeight.w600, color: c.accentText)),
      _ => Text(t.sendItemWaiting, style: TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600, color: c.text3)),
    };
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 10),
      child: Row(children: [
        _Tile(item: item),
        const SizedBox(width: 12),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(item.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w500)),
            const SizedBox(height: 5),
            if (sending)
              ClipRRect(
                borderRadius: BorderRadius.circular(3),
                child: LinearProgressIndicator(value: fraction, minHeight: 4),
              )
            else
              Text(formatBytes(item.size, locale), style: TextStyle(fontSize: 13.5, color: c.text3)),
          ]),
        ),
        const SizedBox(width: 12),
        trailing,
      ]),
    );
  }
}

/// A file on its way has no thumbnail yet: its colour, and what kind it is.
class _Tile extends StatelessWidget {
  const _Tile({required this.item});
  final UploadItemState item;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    final document = item.kind == FileKind.document;
    final tone = ShareColors.tone('${item.seq}${item.name}');
    return Container(
      width: 46,
      height: 46,
      decoration: BoxDecoration(
        color: document ? c.s2 : null,
        border: document ? Border.all(color: c.line) : null,
        gradient: document
            ? null
            : LinearGradient(
                begin: Alignment.topLeft,
                end: Alignment.bottomRight,
                colors: [Color.alphaBlend(const Color(0x17FFFFFF), tone), Color.alphaBlend(const Color(0x3D000000), tone)],
              ),
        borderRadius: BorderRadius.circular(11),
      ),
      child: switch (item.kind) {
        FileKind.document => Icon(AppIcons.fileText, size: 20, color: c.text2),
        FileKind.video => Icon(AppIcons.playCircle, size: 20, color: Colors.white.withValues(alpha: 0.7)),
        FileKind.photo => null,
      },
    );
  }
}

class _Notice extends StatelessWidget {
  const _Notice({required this.icon, required this.text, this.action, this.danger = false});
  final IconData icon;
  final String text;
  final (String, VoidCallback)? action;
  final bool danger;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      margin: const EdgeInsets.only(top: 10),
      padding: const EdgeInsets.fromLTRB(14, 12, 14, 12),
      decoration: BoxDecoration(color: danger ? c.dangerSoft : c.s2, borderRadius: BorderRadius.circular(14)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Icon(icon, size: 19, color: danger ? c.danger : c.accentText),
          const SizedBox(width: 10),
          Expanded(child: Text(text, style: TextStyle(fontSize: 14.5, height: 1.45, color: c.text2))),
        ]),
        if (action != null)
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(onPressed: action!.$2, child: Text(action!.$1)),
          ),
      ]),
    );
  }
}
