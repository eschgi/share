import 'package:flutter/material.dart';

import '../app.dart';
import '../data/models.dart';
import '../data/platform.dart';
import '../l10n/app_localizations.dart';
import 'fetch.dart';
import 'format.dart';
import 'icons.dart';
import 'library/scope.dart';
import 'library/tiles.dart';
import 'player.dart';
import 'theme.dart';

/// A video or a sound in the viewer: its picture and Play, then the player with its controls.
/// It plays from a copy on the phone or straight from the server (platform.play); what can't
/// play here opens in another app.
class MediaPage extends StatefulWidget {
  const MediaPage({super.key, required this.file, required this.active});
  final FileInfo file;

  /// The page in view: one swiped away pauses.
  final bool active;

  @override
  State<MediaPage> createState() => _MediaPageState();
}

class _MediaPageState extends State<MediaPage> {
  late final AppServices _services = Services.read(context);
  MediaPlayer? _player;
  bool _starting = false;
  bool _failed = false;
  bool _awake = false;

  Future<void> _start() async {
    if (_starting) return;
    setState(() {
      _starting = true;
      _failed = false;
    });
    PlaySource? source;
    var failed = false;
    await withFetch(context, 1, () async {
      try {
        source = await _services.platform.play(widget.file, auth: LibraryScope.read(context).auth);
      } on OpenFailed {
        failed = true;
      }
    });
    if (!mounted) return;
    final from = source;
    if (from == null) {
      setState(() {
        _starting = false;
        _failed = failed;
      });
      return;
    }
    final player = _services.player(from);
    _player = player;
    player.value.addListener(_changed);
    await player.initialize();
    if (!mounted || _player != player) return;
    setState(() => _starting = false);
    if (!player.value.value.failed && widget.active) await player.play();
  }

  /// The screen stays on while it plays; a failure shows what else to do.
  void _changed() {
    final v = _player?.value.value;
    if (v == null) return;
    if (v.playing != _awake) {
      _awake = v.playing;
      _services.platform.keepScreenOn(_awake);
    }
    if (v.failed && !_failed && mounted) setState(() => _failed = true);
  }

  Future<void> _retry() async {
    await _stop();
    await _start();
  }

  Future<void> _stop() async {
    final player = _player;
    _player = null;
    player?.value.removeListener(_changed);
    if (_awake) {
      _awake = false;
      await _services.platform.keepScreenOn(false);
    }
    await player?.dispose();
  }

  @override
  void didUpdateWidget(MediaPage old) {
    super.didUpdateWidget(old);
    if (old.active && !widget.active) _player?.pause();
  }

  @override
  void dispose() {
    _stop();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final f = widget.file;
    final picture = f.isAudio ? _SoundArt(file: f) : ThumbImage(file: f, fit: BoxFit.contain);
    if (_failed) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            Icon(AppIcons.alert, size: 32, color: c.text2),
            const SizedBox(height: 14),
            Text(t.viewerCantPlay, textAlign: TextAlign.center, style: TextStyle(color: c.text2, fontSize: 15, height: 1.5)),
            const SizedBox(height: 18),
            Wrap(alignment: WrapAlignment.center, spacing: 10, runSpacing: 8, children: [
              TextButton(onPressed: _retry, child: Text(t.commonRetry)),
              FilledButton.tonalIcon(
                onPressed: () => withFetch(context, 1, () => _services.platform.openFile(f, auth: LibraryScope.read(context).auth)),
                icon: const Icon(AppIcons.externalLink, size: 18),
                label: Text(t.viewerOpenElsewhere),
              ),
            ]),
          ]),
        ),
      );
    }
    final player = _player;
    if (player == null) {
      return Stack(fit: StackFit.expand, children: [
        picture,
        Center(
          child: _starting
              ? const CircularProgressIndicator(color: Colors.white)
              : IconButton.filled(
                  iconSize: 40,
                  style: IconButton.styleFrom(backgroundColor: Colors.black54, padding: const EdgeInsets.all(18)),
                  tooltip: t.viewerOpenVideo,
                  icon: const Icon(AppIcons.play, color: Colors.white),
                  onPressed: _start,
                ),
        ),
      ]);
    }
    return ValueListenableBuilder<PlayerValue>(
      valueListenable: player.value,
      builder: (context, v, _) => Stack(fit: StackFit.expand, children: [
        if (f.isAudio || !v.ready)
          picture
        else
          GestureDetector(
            onTap: () => v.playing ? player.pause() : player.play(),
            child: Center(child: AspectRatio(aspectRatio: v.aspectRatio, child: player.surface())),
          ),
        if (!v.ready || v.buffering) const Center(child: CircularProgressIndicator(color: Colors.white)),
        Positioned(left: 0, right: 0, bottom: 0, child: _Controls(value: v, player: player)),
      ]),
    );
  }
}

/// Play or pause, where it is, and the bar to move along it.
class _Controls extends StatelessWidget {
  const _Controls({required this.value, required this.player});
  final PlayerValue value;
  final MediaPlayer player;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final v = value;
    final total = v.duration.inMilliseconds;
    final at = v.position.inMilliseconds.clamp(0, total > 0 ? total : 0);
    const time = TextStyle(fontSize: 12.5, color: Colors.white, fontFeatures: [FontFeature.tabularFigures()]);
    return DecoratedBox(
      decoration: const BoxDecoration(gradient: LinearGradient(begin: Alignment.topCenter, end: Alignment.bottomCenter, colors: [Colors.transparent, Colors.black54])),
      child: Padding(
        padding: const EdgeInsets.fromLTRB(4, 18, 14, 4),
        child: Row(children: [
          IconButton(
            tooltip: v.playing ? t.viewerPause : t.viewerOpenVideo,
            icon: Icon(v.playing ? AppIcons.pause : AppIcons.play, color: Colors.white),
            onPressed: () async {
              if (v.playing) return player.pause();
              if (v.ended) await player.seekTo(Duration.zero);
              await player.play();
            },
          ),
          Text(formatDuration(at), style: time),
          Expanded(
            child: Slider(
              value: at.toDouble(),
              max: total > 0 ? total.toDouble() : 1,
              onChanged: total > 0 ? (ms) => player.seekTo(Duration(milliseconds: ms.round())) : null,
            ),
          ),
          Text(formatDuration(total), style: time),
        ]),
      ),
    );
  }
}

/// A sound has no picture: a note and its name.
class _SoundArt extends StatelessWidget {
  const _SoundArt({required this.file});
  final FileInfo file;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Center(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(32, 32, 32, 96),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          Container(
            width: 120,
            height: 120,
            decoration: BoxDecoration(color: c.s2, shape: BoxShape.circle),
            child: Icon(AppIcons.music, size: 48, color: c.accentText),
          ),
          const SizedBox(height: 18),
          Text(file.name, textAlign: TextAlign.center, maxLines: 2, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 15, color: c.text2)),
        ]),
      ),
    );
  }
}
