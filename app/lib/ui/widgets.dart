import 'dart:math' as math;

import 'package:flutter/material.dart';

import 'icons.dart';
import 'theme.dart';

/// The logo and the server's name, as at the top of the website.
class Brand extends StatelessWidget {
  const Brand({super.key, required this.name});
  final String name;

  @override
  Widget build(BuildContext context) => Row(children: [
        Icon(AppIcons.images, size: 24, color: context.colors.text),
        const SizedBox(width: 10),
        Text(name, style: const TextStyle(fontFamily: serif, fontSize: 20, fontWeight: FontWeight.w700)),
      ]);
}

/// Three cards, a photo on each side and a document in front: the picture of the first screens.
class PhotoStack extends StatelessWidget {
  const PhotoStack({super.key, this.left = 3, this.right = 2});
  final int left, right; // tones

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    Widget photo(int tone, double angle, double w, double h) => Transform.rotate(
          angle: angle * math.pi / 180,
          child: Container(
            width: w,
            height: h,
            decoration: BoxDecoration(
              color: ShareColors.tones[tone],
              borderRadius: BorderRadius.circular(14),
              border: Border.all(color: const Color(0xFFD9CEC1), width: 5),
              boxShadow: const [BoxShadow(color: Color(0x80000000), blurRadius: 36, offset: Offset(0, 16))],
            ),
            child: Icon(AppIcons.image, size: 38, color: Colors.white.withValues(alpha: 0.6)),
          ),
        );
    return SizedBox(
      height: 196,
      child: LayoutBuilder(builder: (context, box) {
        final w = box.maxWidth;
        return Stack(clipBehavior: Clip.none, children: [
          Positioned(left: 2, top: 36, child: photo(left, -8, w * 0.52, 136)),
          Positioned(right: 2, top: 8, child: photo(right, 6, w * 0.49, 132)),
          Positioned(
            left: w / 2 - 88,
            top: 44,
            child: Transform.rotate(
              angle: -1.5 * math.pi / 180,
              child: Container(
                width: 176,
                height: 140,
                decoration: BoxDecoration(
                  color: const Color(0xFFEEE6DC),
                  borderRadius: BorderRadius.circular(14),
                  boxShadow: const [BoxShadow(color: Color(0x80000000), blurRadius: 36, offset: Offset(0, 16))],
                ),
                child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
                  Icon(AppIcons.fileText, size: 38, color: c.accent),
                  const SizedBox(height: 8),
                  const Text('PDF', style: TextStyle(color: Color(0xFF8A7B6C), fontWeight: FontWeight.w700, letterSpacing: 1.2)),
                ]),
              ),
            ),
          ),
        ]);
      }),
    );
  }
}

class HeroTitle extends StatelessWidget {
  const HeroTitle(this.text, {super.key, this.center = false});
  final String text;
  final bool center;

  @override
  Widget build(BuildContext context) =>
      Text(text, textAlign: center ? TextAlign.center : null, style: Theme.of(context).textTheme.headlineLarge);
}

class Lead extends StatelessWidget {
  const Lead(this.text, {super.key, this.center = false});
  final String text;
  final bool center;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 14),
        child: Text(text,
            textAlign: center ? TextAlign.center : null,
            style: Theme.of(context).textTheme.bodyLarge!.copyWith(color: context.colors.text2)),
      );
}

/// Small grey text below the main button.
class Small extends StatelessWidget {
  const Small(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.fromLTRB(8, 14, 8, 0),
        child: Text(text, textAlign: TextAlign.center, style: TextStyle(fontSize: 14, height: 1.5, color: context.colors.text3)),
      );
}

/// One of the two doors on the first screen.
class ChoiceCard extends StatelessWidget {
  const ChoiceCard({super.key, required this.icon, required this.title, required this.detail, required this.onTap});
  final IconData icon;
  final String title, detail;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Material(
      color: c.s1,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(20), side: BorderSide(color: c.lineSoft)),
      child: InkWell(
        borderRadius: BorderRadius.circular(20),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 18),
          child: Row(children: [
            Container(
              width: 52,
              height: 52,
              decoration: BoxDecoration(color: c.accentSoft, borderRadius: BorderRadius.circular(16)),
              child: Icon(icon, size: 26, color: c.accentText),
            ),
            const SizedBox(width: 16),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Text(title, style: const TextStyle(fontSize: 18, fontWeight: FontWeight.w600)),
                const SizedBox(height: 3),
                Text(detail, style: TextStyle(fontSize: 14, height: 1.45, color: c.text2)),
              ]),
            ),
            const SizedBox(width: 8),
            Icon(AppIcons.chevronRight, size: 20, color: c.text3),
          ]),
        ),
      ),
    );
  }
}

class FieldLabel extends StatelessWidget {
  const FieldLabel(this.text, {super.key, this.optional, this.first = false});
  final String text;
  final String? optional;
  final bool first;

  @override
  Widget build(BuildContext context) => Padding(
        padding: EdgeInsets.only(top: first ? 0 : 24, bottom: 8),
        child: Row(children: [
          Text(text, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500)),
          if (optional != null) ...[
            const SizedBox(width: 8),
            Text(optional!, style: TextStyle(fontSize: 13, color: context.colors.text3)),
          ],
        ]),
      );
}

/// Grey help text under a field.
class Help extends StatelessWidget {
  const Help(this.text, {super.key, this.error = false});
  final String text;
  final bool error;

  @override
  Widget build(BuildContext context) => Padding(
        padding: const EdgeInsets.only(top: 10),
        child: Text(text, style: TextStyle(fontSize: 14, height: 1.5, color: error ? context.colors.danger : context.colors.text3)),
      );
}

/// A card with an icon and a short explanation.
class NoteCard extends StatelessWidget {
  const NoteCard({super.key, required this.icon, required this.text, this.color});
  final IconData icon;
  final String text;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(color: color ?? c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
      child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Icon(icon, size: 22, color: c.accentText),
        const SizedBox(width: 14),
        Expanded(child: Text(text, style: TextStyle(fontSize: 15, height: 1.5, color: c.text2))),
      ]),
    );
  }
}

/// A person's initial in a circle.
class Avatar extends StatelessWidget {
  const Avatar({super.key, required this.name, required this.id, this.size = 40, this.pending = false});
  final String name, id;
  final double size;
  final bool pending;

  @override
  Widget build(BuildContext context) {
    final (bg, fg) = ShareColors.avatar(id);
    final letter = name.isEmpty ? '?' : name.characters.first.toUpperCase();
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: pending
          ? BoxDecoration(shape: BoxShape.circle, border: Border.all(color: context.colors.text3, width: 1.5))
          : BoxDecoration(shape: BoxShape.circle, color: bg),
      child: Text(letter,
          style: TextStyle(fontSize: size * 0.4, fontWeight: FontWeight.w600, color: pending ? context.colors.text2 : fg)),
    );
  }
}

/// Settings rows, grouped on a card.
class SettingsGroup extends StatelessWidget {
  const SettingsGroup({super.key, required this.children});
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(20), border: Border.all(color: c.lineSoft)),
      clipBehavior: Clip.antiAlias,
      child: Column(children: [
        for (var i = 0; i < children.length; i++) ...[
          if (i > 0) Divider(indent: 16, endIndent: 16, color: c.lineSoft),
          children[i],
        ],
      ]),
    );
  }
}

class SettingsRow extends StatelessWidget {
  const SettingsRow({super.key, required this.leading, required this.title, this.subtitle, this.trailing, this.onTap, this.danger = false});
  final Widget leading;
  final String title;
  final String? subtitle;
  final Widget? trailing;
  final VoidCallback? onTap;
  final bool danger;

  /// The square icon of a row, accent-coloured for the most used ones.
  static Widget icon(BuildContext context, IconData icon, {bool accent = false}) {
    final c = context.colors;
    return Container(
      width: 40,
      height: 40,
      decoration: BoxDecoration(color: accent ? c.accentSoft : c.s2, borderRadius: BorderRadius.circular(12)),
      child: Icon(icon, size: 20, color: accent ? c.accentText : c.text2),
    );
  }

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return InkWell(
      onTap: onTap,
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
        child: Row(children: [
          leading,
          const SizedBox(width: 14),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(title, style: TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600, color: danger ? c.danger : c.text)),
              if (subtitle != null) ...[
                const SizedBox(height: 2),
                Text(subtitle!, style: TextStyle(fontSize: 13, color: c.text3)),
              ],
            ]),
          ),
          if (trailing != null) trailing! else if (onTap != null) Icon(AppIcons.chevronRight, size: 18, color: c.text3),
        ]),
      ),
    );
  }
}

class RoleBadge extends StatelessWidget {
  const RoleBadge({super.key, required this.label, this.admin = false});
  final String label;
  final bool admin;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
      decoration: BoxDecoration(color: admin ? c.accentSoft : c.s3, borderRadius: BorderRadius.circular(8)),
      child: Row(mainAxisSize: MainAxisSize.min, children: [
        if (admin) ...[Icon(AppIcons.crown, size: 13, color: c.accentText), const SizedBox(width: 4)],
        Text(label, style: TextStyle(fontSize: 12, fontWeight: FontWeight.w600, color: admin ? c.accentText : c.text2)),
      ]),
    );
  }
}

/// A card saying how something stands: fine (green check), or not (grey).
class StatusCard extends StatelessWidget {
  const StatusCard({super.key, required this.ok, required this.title, required this.detail, this.busy = false});
  final bool ok, busy;
  final String title, detail;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Container(
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
      child: Row(children: [
        Container(
          width: 40,
          height: 40,
          decoration: BoxDecoration(color: ok ? c.okSoft : c.s2, borderRadius: BorderRadius.circular(12)),
          child: busy
              ? Padding(padding: const EdgeInsets.all(11), child: CircularProgressIndicator(strokeWidth: 2, color: c.text2))
              : Icon(ok ? AppIcons.check : AppIcons.cloud, size: 20, color: ok ? c.ok : c.text2),
        ),
        const SizedBox(width: 14),
        Expanded(
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(title, style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w600)),
            const SizedBox(height: 2),
            Text(detail, style: TextStyle(fontSize: 13, height: 1.4, color: c.text3)),
          ]),
        ),
      ]),
    );
  }
}

/// "── or ──"
class OrDivider extends StatelessWidget {
  const OrDivider(this.text, {super.key});
  final String text;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 22),
      child: Row(children: [
        Expanded(child: Divider(color: c.line)),
        Padding(padding: const EdgeInsets.symmetric(horizontal: 14), child: Text(text, style: TextStyle(color: c.text3, fontSize: 14))),
        Expanded(child: Divider(color: c.line)),
      ]),
    );
  }
}

/// A primary button that shows a spinner while [busy].
class BusyButton extends StatelessWidget {
  const BusyButton({super.key, required this.label, required this.onPressed, this.busy = false, this.icon});
  final String label;
  final VoidCallback? onPressed;
  final bool busy;
  final IconData? icon;

  @override
  Widget build(BuildContext context) => FilledButton(
        onPressed: busy ? null : onPressed,
        child: busy
            ? SizedBox(width: 22, height: 22, child: CircularProgressIndicator(strokeWidth: 2.5, color: context.colors.text2))
            : Row(mainAxisSize: MainAxisSize.min, children: [
                if (icon != null) ...[Icon(icon, size: 22), const SizedBox(width: 10)],
                Flexible(child: Text(label, overflow: TextOverflow.ellipsis)),
              ]),
      );
}

/// The back arrow of the mockups (Lucide), instead of Material's.
class ShareBackButton extends StatelessWidget {
  const ShareBackButton({super.key, this.close = false});
  final bool close;

  @override
  Widget build(BuildContext context) => IconButton(
        tooltip: MaterialLocalizations.of(context).backButtonTooltip,
        icon: Icon(close ? AppIcons.x : AppIcons.back, size: 24),
        onPressed: () => Navigator.maybePop(context),
      );
}
