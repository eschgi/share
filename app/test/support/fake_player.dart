import 'package:flutter/foundation.dart';
import 'package:flutter/widgets.dart';
import 'package:share_app/data/platform.dart';
import 'package:share_app/ui/player.dart';

/// A player that plays nothing: it is ready at once with an 18-second file, or fails to load.
class FakeMediaPlayer implements MediaPlayer {
  FakeMediaPlayer(this.source, {this.fails = false});
  final PlaySource source;
  final bool fails;
  bool disposed = false;
  final _value = ValueNotifier(const PlayerValue());

  PlayerValue get now => _value.value;

  void _set({bool? playing, Duration? position}) => _value.value = PlayerValue(
        ready: true,
        playing: playing ?? now.playing,
        position: position ?? now.position,
        duration: const Duration(seconds: 18),
        aspectRatio: 16 / 9,
      );

  @override
  ValueListenable<PlayerValue> get value => _value;

  @override
  Future<void> initialize() async => fails ? _value.value = const PlayerValue(failed: true) : _set();

  @override
  Future<void> play() async => _set(playing: true);

  @override
  Future<void> pause() async => _set(playing: false);

  @override
  Future<void> seekTo(Duration position) async => _set(position: position);

  @override
  Future<void> dispose() async => disposed = true;

  @override
  Widget surface() => const ColoredBox(color: Color(0xFF203040));
}
