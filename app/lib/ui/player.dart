import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:video_player/video_player.dart';

import '../data/platform.dart';

/// How far a video or sound is, for the viewer's controls.
@immutable
class PlayerValue {
  const PlayerValue({
    this.ready = false,
    this.playing = false,
    this.buffering = false,
    this.ended = false,
    this.failed = false,
    this.position = Duration.zero,
    this.duration = Duration.zero,
    this.aspectRatio = 16 / 9,
  });

  final bool ready, playing, buffering, ended, failed;
  final Duration position, duration;
  final double aspectRatio;
}

/// Plays a video or sound in the viewer: Flutter's video_player on the phone, a stand-in in
/// the tests (AppServices.player).
abstract class MediaPlayer {
  ValueListenable<PlayerValue> get value;
  Future<void> initialize();
  Future<void> play();
  Future<void> pause();
  Future<void> seekTo(Duration position);
  Future<void> dispose();

  /// The picture of a video.
  Widget surface();
}

typedef MediaPlayerFactory = MediaPlayer Function(PlaySource source);

/// The phone's player, for a copy on the phone or straight from the server.
MediaPlayer videoPlayer(PlaySource source) => _VideoPlayer(source);

class _VideoPlayer implements MediaPlayer {
  _VideoPlayer(PlaySource s)
      : _c = switch (s.uri.scheme) {
          'content' => VideoPlayerController.contentUri(s.uri),
          'file' => VideoPlayerController.file(File(s.uri.toFilePath())),
          _ => VideoPlayerController.networkUrl(s.uri, httpHeaders: s.headers),
        } {
    _c.addListener(_update);
  }

  final VideoPlayerController _c;
  final _value = ValueNotifier(const PlayerValue());

  void _update() {
    final v = _c.value;
    _value.value = PlayerValue(
      ready: v.isInitialized,
      playing: v.isPlaying,
      buffering: v.isBuffering,
      ended: v.isCompleted,
      failed: v.hasError,
      position: v.position,
      duration: v.duration,
      aspectRatio: v.isInitialized && v.aspectRatio > 0 ? v.aspectRatio : 16 / 9,
    );
  }

  @override
  ValueListenable<PlayerValue> get value => _value;

  @override
  Future<void> initialize() async {
    try {
      await _c.initialize();
    } on Exception {
      _value.value = const PlayerValue(failed: true);
    }
  }

  @override
  Future<void> play() => _c.play();

  @override
  Future<void> pause() => _c.pause();

  @override
  Future<void> seekTo(Duration position) => _c.seekTo(position);

  @override
  Future<void> dispose() async {
    _c.removeListener(_update);
    await _c.dispose();
    _value.dispose();
  }

  @override
  Widget surface() => VideoPlayer(_c);
}
