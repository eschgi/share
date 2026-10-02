import 'dart:typed_data';

import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import 'library_controller.dart';

/// A file's thumbnail, or its placeholder: a muted tone for photos and videos, the
/// extension for documents.
class ThumbImage extends StatefulWidget {
  const ThumbImage({super.key, required this.file, this.fit = BoxFit.cover});
  final FileInfo file;
  final BoxFit fit;

  @override
  State<ThumbImage> createState() => _ThumbImageState();
}

class _ThumbImageState extends State<ThumbImage> {
  Uint8List? _bytes;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void didUpdateWidget(ThumbImage old) {
    super.didUpdateWidget(old);
    if (old.file.id != widget.file.id || old.file.updatedAt != widget.file.updatedAt) {
      _bytes = null;
      _load();
    }
  }

  Future<void> _load() async {
    if (!widget.file.hasThumb) return;
    final bytes = await Services.read(context).library.thumb(widget.file);
    if (mounted && bytes != null) setState(() => _bytes = bytes);
  }

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    final f = widget.file;
    if (_bytes != null) {
      return LayoutBuilder(builder: (context, box) {
        final px = (box.maxWidth * MediaQuery.devicePixelRatioOf(context)).round();
        return Image.memory(_bytes!, fit: widget.fit, cacheWidth: px > 0 ? px : null, gaplessPlayback: true);
      });
    }
    if (f.kind == FileKind.document) {
      // The film strip's small tiles have room for the icon only.
      return LayoutBuilder(builder: (context, box) {
        final small = box.maxHeight < 64;
        return DecoratedBox(
          decoration: BoxDecoration(color: c.s1, border: Border.all(color: c.lineSoft)),
          child: Stack(children: [
            Center(
              child: Column(mainAxisSize: MainAxisSize.min, children: [
                Icon(AppIcons.fileText, size: small ? 20 : 26, color: c.text2),
                if (!small) ...[
                  const SizedBox(height: 6),
                  Padding(
                    padding: const EdgeInsets.symmetric(horizontal: 6),
                    child: Text(f.name, maxLines: 1, overflow: TextOverflow.ellipsis, style: TextStyle(fontSize: 10.5, color: c.text2)),
                  ),
                ],
              ]),
            ),
            if (f.ext.isNotEmpty && !small)
              Positioned(
                top: 6,
                right: 6,
                child: Container(
                  padding: const EdgeInsets.symmetric(horizontal: 4, vertical: 2),
                  decoration: BoxDecoration(color: c.s3, borderRadius: BorderRadius.circular(4)),
                  child: Text(f.ext, style: TextStyle(fontSize: 9, fontWeight: FontWeight.w700, letterSpacing: 0.5, color: c.text2)),
                ),
              ),
          ]),
        );
      });
    }
    return DecoratedBox(
      decoration: BoxDecoration(
        gradient: LinearGradient(
          begin: Alignment.topLeft,
          end: Alignment.bottomRight,
          colors: [Color.alphaBlend(const Color(0x17FFFFFF), ShareColors.tone(f.id)), Color.alphaBlend(const Color(0x3D000000), ShareColors.tone(f.id))],
        ),
      ),
      child: Center(
        child: Icon(f.kind == FileKind.video ? AppIcons.playCircle : AppIcons.image, size: 26, color: Colors.white.withValues(alpha: 0.6)),
      ),
    );
  }
}

/// One file in the library grid.
class LibraryTile extends StatelessWidget {
  const LibraryTile({
    super.key,
    required this.file,
    required this.selected,
    required this.selecting,
    required this.saved,
  });

  final FileInfo file;
  final bool selected, selecting, saved;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return AnimatedContainer(
      duration: const Duration(milliseconds: 120),
      padding: EdgeInsets.all(selected ? 6 : 0),
      child: ClipRRect(
        borderRadius: BorderRadius.circular(selected ? 12 : 4),
        child: Stack(fit: StackFit.expand, children: [
          ThumbImage(file: file),
          if (file.kind == FileKind.video && file.durationMs != null)
            Positioned(
              right: 5,
              bottom: 5,
              child: _Badge(child: Row(mainAxisSize: MainAxisSize.min, children: [
                const Icon(AppIcons.play, size: 10, color: Colors.white),
                const SizedBox(width: 3),
                Text(formatDuration(file.durationMs!), style: const TextStyle(fontSize: 11, fontWeight: FontWeight.w600, color: Colors.white)),
              ])),
            ),
          if (saved)
            const Positioned(left: 5, bottom: 5, child: _Badge(child: Icon(AppIcons.smartphone, size: 11, color: Colors.white))),
          if (selecting)
            Positioned(
              left: 6,
              top: 6,
              child: Container(
                width: 22,
                height: 22,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: selected ? c.accent : Colors.black.withValues(alpha: 0.18),
                  border: Border.all(color: selected ? c.accent : Colors.white.withValues(alpha: 0.85), width: 2),
                ),
                child: selected ? const Icon(AppIcons.check, size: 13, color: Colors.white) : null,
              ),
            ),
        ]),
      ),
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

/// The heading of an upload day, with its circle for taking the whole day.
class DayHeader extends StatelessWidget {
  const DayHeader({super.key, required this.title, required this.meta, required this.selection, required this.onCircle, required this.circleLabel});
  final String title, meta, circleLabel;
  final DaySelection selection;
  final VoidCallback onCircle;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Padding(
      padding: const EdgeInsets.fromLTRB(4, 18, 0, 10),
      child: Row(children: [
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
            const SizedBox(height: 4),
            Text(meta, style: TextStyle(fontSize: 12.5, color: c.text3)),
          ]),
        ),
        Semantics(
          button: true,
          label: circleLabel,
          child: InkResponse(
            onTap: onCircle,
            radius: 24,
            child: Padding(
              padding: const EdgeInsets.all(10),
              child: Container(
                width: 24,
                height: 24,
                decoration: BoxDecoration(
                  shape: BoxShape.circle,
                  color: selection == DaySelection.all ? c.accent : null,
                  border: Border.all(
                    width: 2,
                    color: switch (selection) {
                      DaySelection.all => c.accent,
                      DaySelection.some => c.accentText,
                      DaySelection.none => c.text3,
                    },
                  ),
                ),
                child: switch (selection) {
                  DaySelection.all => const Icon(AppIcons.check, size: 14, color: Colors.white),
                  DaySelection.some => Icon(AppIcons.minus, size: 14, color: c.accentText),
                  DaySelection.none => null,
                },
              ),
            ),
          ),
        ),
      ]),
    );
  }
}
