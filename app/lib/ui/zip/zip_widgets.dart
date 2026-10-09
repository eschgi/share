import 'dart:math' as math;
import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';

/// "12 photos, 2 videos · 312 MB": what the files are, and how big.
String zipMeta(AppLocalizations t, String locale, Iterable<FileKind> kinds, int bytes) {
  var photos = 0, videos = 0, documents = 0;
  for (final k in kinds) {
    switch (k) {
      case FileKind.photo:
        photos++;
      case FileKind.video:
        videos++;
      case FileKind.document:
        documents++;
    }
  }
  final parts = [
    if (photos > 0) t.zipCountPhotos(photos),
    if (videos > 0) t.zipCountVideos(videos),
    if (documents > 0) t.zipCountDocuments(documents),
  ];
  return '${parts.join(', ')} · ${formatBytes(bytes, locale)}';
}

/// Three photo prints, as the first screens have them, with the files' own pictures where there are
/// any, and the ZIP badge once they're packed (screens 98 to 100).
class ZipStack extends StatelessWidget {
  const ZipStack({super.key, this.pictures = const [], this.zip = false, this.height = 196});
  final List<Uint8List?> pictures;
  final bool zip;
  final double height;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    Widget print(int i, double angle, double w, double h, {Widget? badge}) {
      final picture = i < pictures.length ? pictures[i] : null;
      return Transform.rotate(
        angle: angle * math.pi / 180,
        child: Stack(clipBehavior: Clip.none, children: [
          Container(
            width: w,
            height: h,
            clipBehavior: Clip.antiAlias,
            decoration: BoxDecoration(
              color: ShareColors.tones[(i * 3 + 1) % ShareColors.tones.length],
              borderRadius: BorderRadius.circular(14),
              border: Border.all(color: const Color(0xFFD9CEC1), width: 5),
              boxShadow: [BoxShadow(color: c.shadow, blurRadius: 36, offset: const Offset(0, 16))],
            ),
            child: picture != null
                ? Image.memory(picture, fit: BoxFit.cover, gaplessPlayback: true)
                : Icon(AppIcons.image, size: 38, color: Colors.white.withValues(alpha: 0.6)),
          ),
          if (badge != null) Positioned(right: -18, bottom: -14, child: badge),
        ]),
      );
    }

    final badge = Container(
      height: 36,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      decoration: BoxDecoration(color: c.accent, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.bg, width: 4)),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        Icon(AppIcons.archive, size: 17, color: c.onAccent),
        const SizedBox(width: 6),
        Text('ZIP', style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w700, letterSpacing: 0.6, color: c.onAccent)),
      ]),
    );
    final scale = height / 196;
    return SizedBox(
      height: height,
      child: LayoutBuilder(builder: (context, box) {
        final w = box.maxWidth;
        return Stack(clipBehavior: Clip.none, children: [
          Positioned(left: 2, top: 36 * scale, child: print(1, -8, w * 0.46, 136 * scale)),
          Positioned(right: 2, top: 8 * scale, child: print(2, 6, w * 0.44, 132 * scale)),
          Positioned(left: w / 2 - 88 * scale, top: 44 * scale, child: print(0, -1.5, 176 * scale, 140 * scale, badge: zip ? badge : null)),
        ]);
      }),
    );
  }
}

/// One part's chip over the files (screens 108, 109): saved, here, this one, or not yet.
enum ChipState { saved, now, here, current, notYet }

class PartChips extends StatelessWidget {
  const PartChips({super.key, required this.states});
  final List<ChipState> states; // part 1 first

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    Widget chip(int part, ChipState s) {
      final ok = s == ChipState.saved || s == ChipState.now || s == ChipState.here;
      final (label, color, border, fill) = switch (s) {
        ChipState.saved => (t.zipChipSaved, c.ok, c.lineSoft, c.s1),
        ChipState.now => (t.zipChipNow, c.ok, c.ok, c.okSoft),
        ChipState.here => (t.zipChipHere, c.ok, c.lineSoft, c.s1),
        ChipState.current => (t.zipChipThis, c.accentText, c.accentText, c.accentSoft),
        ChipState.notYet => (t.zipChipNotYet, c.text3, c.line, Colors.transparent),
      };
      return Container(
        padding: const EdgeInsets.fromLTRB(9, 8, 9, 8),
        decoration: BoxDecoration(color: fill, borderRadius: BorderRadius.circular(14), border: Border.all(color: border, width: 1.5)),
        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
          Text(t.zipChipPart(part), maxLines: 1, overflow: TextOverflow.fade, softWrap: false, style: const TextStyle(fontSize: 14.5, fontWeight: FontWeight.w600)),
          const SizedBox(height: 2),
          // Long words, as Noch nicht or Non ancora, get a little smaller rather than cut off.
          FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Row(mainAxisSize: MainAxisSize.min, children: [
              if (ok) ...[Icon(AppIcons.check, size: 13, color: color), const SizedBox(width: 4)],
              Text(label, maxLines: 1, style: TextStyle(fontSize: 12.5, color: color)),
            ]),
          ),
        ]),
      );
    }

    // Four in a row, as many rows as it takes.
    final rows = <Widget>[];
    for (var i = 0; i < states.length; i += 4) {
      rows.add(Padding(
        padding: EdgeInsets.only(top: i == 0 ? 0 : 6),
        child: Row(children: [
          for (var k = i; k < i + 4; k++) ...[
            if (k > i) const SizedBox(width: 6),
            Expanded(child: k < states.length ? chip(k + 1, states[k]) : const SizedBox()),
          ],
        ]),
      ));
    }
    return Column(children: rows);
  }
}

/// A file in a ZIP, as a tile of the grid: its picture once it's there, a video's length, and for a
/// piece of a cut file a dashed edge and which piece it is.
class ZipTile extends StatelessWidget {
  const ZipTile({super.key, required this.name, required this.kind, this.picture, this.durationMs, this.piece, this.saved = false});
  final String name;
  final FileKind kind;
  final Uint8List? picture;
  final int? durationMs;

  /// "Piece 1 of 2", for a piece whose file isn't whole here.
  final String? piece;
  final bool saved;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    final tone = ShareColors.tone(name);
    final Widget base;
    if (picture != null) {
      base = LayoutBuilder(builder: (context, box) {
        final px = (box.maxWidth * MediaQuery.devicePixelRatioOf(context)).round();
        return Image.memory(picture!, fit: BoxFit.cover, cacheWidth: px > 0 ? px : null, gaplessPlayback: true);
      });
    } else if (kind == FileKind.document) {
      base = DecoratedBox(
        decoration: BoxDecoration(color: c.s1, border: Border.all(color: c.lineSoft)),
        child: Center(
          child: Column(mainAxisSize: MainAxisSize.min, children: [
            Icon(AppIcons.fileText, size: 26, color: c.text2),
            const SizedBox(height: 6),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6),
              child: Text(name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 10.5, color: c.text2)),
            ),
          ]),
        ),
      );
    } else {
      base = DecoratedBox(
        decoration: BoxDecoration(
          gradient: LinearGradient(
            begin: Alignment.topLeft,
            end: Alignment.bottomRight,
            colors: [Color.alphaBlend(const Color(0x17FFFFFF), tone), Color.alphaBlend(const Color(0x3D000000), tone)],
          ),
        ),
        child: Center(child: Icon(kind == FileKind.video ? AppIcons.playCircle : AppIcons.image, size: 26, color: Colors.white.withValues(alpha: 0.6))),
      );
    }
    return ClipRRect(
      borderRadius: BorderRadius.circular(4),
      child: Stack(fit: StackFit.expand, children: [
        base,
        if (piece != null)
          Positioned.fill(
            child: Padding(padding: const EdgeInsets.all(6), child: CustomPaint(painter: _Dashed(Colors.white.withValues(alpha: 0.62)))),
          ),
        if (piece != null)
          Positioned(left: 4, right: 4, bottom: 5, child: _Badge(child: Text(piece!, textAlign: TextAlign.center, maxLines: 1, overflow: TextOverflow.fade, softWrap: false, style: const TextStyle(fontSize: 10, fontWeight: FontWeight.w700, color: Colors.white))))
        else if (kind == FileKind.video && durationMs != null)
          Positioned(
            right: 5,
            bottom: 5,
            child: _Badge(child: Row(mainAxisSize: MainAxisSize.min, children: [
              const Icon(AppIcons.play, size: 10, color: Colors.white),
              const SizedBox(width: 3),
              Text(formatDuration(durationMs!), style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: Colors.white)),
            ])),
          ),
        if (saved) const Positioned(left: 5, top: 5, child: _Badge(child: Icon(AppIcons.smartphone, size: 11, color: Colors.white))),
      ]),
    );
  }
}

class _Badge extends StatelessWidget {
  const _Badge({required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 5, vertical: 3),
        decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.55), borderRadius: BorderRadius.circular(6)),
        child: child,
      );
}

/// A dashed frame, where a piece's tile shows that more of the file is elsewhere.
class _Dashed extends CustomPainter {
  _Dashed(this.color);
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 1.5
      ..style = PaintingStyle.stroke;
    void line(Offset a, Offset b) {
      final d = b - a;
      final n = (d.distance / 7).floor();
      for (var i = 0; i < n; i += 2) {
        canvas.drawLine(a + d * (i / n), a + d * ((i + 1) / n), paint);
      }
    }

    final r = Offset.zero & size;
    line(r.topLeft, r.topRight);
    line(r.topRight, r.bottomRight);
    line(r.bottomRight, r.bottomLeft);
    line(r.bottomLeft, r.topLeft);
  }

  @override
  bool shouldRepaint(_Dashed old) => old.color != color;
}
