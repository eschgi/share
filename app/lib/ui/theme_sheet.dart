import 'package:flutter/material.dart';

import '../app.dart';
import '../l10n/app_localizations.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

String themeName(AppLocalizations t, AppTheme theme) => switch (theme) {
      AppTheme.ember => t.themeEmber,
      AppTheme.midnight => t.themeMidnight,
      AppTheme.moss => t.themeMoss,
      AppTheme.plum => t.themePlum,
      AppTheme.black => t.themeBlack,
      AppTheme.linen => t.themeLinen,
      AppTheme.frost => t.themeFrost,
    };

/// The settings row's line: the theme, or Automatic with the one it shows now.
String themeSummary(BuildContext context, String? setting) {
  final t = AppLocalizations.of(context);
  if (setting == ThemeChoice.automatic) return t.themeAutomaticNow(context.colors.isDark ? t.themeEmber : t.themeLinen);
  return themeName(t, AppTheme.parse(setting) ?? AppTheme.ember);
}

/// Picking a theme. Each is shown as a small picture of the app in its colours, and a tap
/// switches at once, so the sheet itself shows the choice.
class ThemeSheet extends StatelessWidget {
  const ThemeSheet({super.key});

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final services = Services.of(context);
    return ValueListenableBuilder<String?>(
      valueListenable: services.theme,
      builder: (context, chosen, _) {
        final current = chosen ?? AppTheme.ember.name;
        Widget tile(AppTheme theme) => _ThemeTile(
              name: themeName(t, theme),
              selected: current == theme.name,
              preview: ThemePreview(colors: theme.colors),
              onTap: () => services.setTheme(theme == AppTheme.ember ? null : theme.name),
            );
        return SafeArea(
          child: SingleChildScrollView(
            padding: const EdgeInsets.fromLTRB(20, 0, 20, 16),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              Text(t.settingsTheme, style: Theme.of(context).textTheme.headlineSmall),
              const SizedBox(height: 16),
              _AutomaticTile(selected: current == ThemeChoice.automatic, onTap: () => services.setTheme(ThemeChoice.automatic)),
              SectionLabel(t.themeDark),
              _Grid(children: [for (final th in AppTheme.values) if (th.colors.isDark) tile(th)]),
              SectionLabel(t.themeLight),
              _Grid(children: [for (final th in AppTheme.values) if (!th.colors.isDark) tile(th)]),
            ]),
          ),
        );
      },
    );
  }
}

class _Grid extends StatelessWidget {
  const _Grid({required this.children});
  final List<Widget> children;

  @override
  Widget build(BuildContext context) => LayoutBuilder(builder: (context, box) {
        const gap = 12.0;
        final width = (box.maxWidth - 2 * gap) / 3;
        return Wrap(spacing: gap, runSpacing: gap, children: [for (final c in children) SizedBox(width: width, child: c)]);
      });
}

class _ThemeTile extends StatelessWidget {
  const _ThemeTile({required this.name, required this.selected, required this.preview, required this.onTap});
  final String name;
  final bool selected;
  final Widget preview;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Semantics(
      button: true,
      selected: selected,
      label: name,
      excludeSemantics: true,
      child: GestureDetector(
        onTap: onTap,
        child: Column(children: [
          _Framed(selected: selected, child: AspectRatio(aspectRatio: 0.9, child: preview)),
          const SizedBox(height: 8),
          Text(name,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(fontSize: 14, fontWeight: selected ? FontWeight.w700 : FontWeight.w500, color: selected ? c.text : c.text2)),
        ]),
      ),
    );
  }
}

/// Linen and Ember side by side, as the phone switches between them.
class _AutomaticTile extends StatelessWidget {
  const _AutomaticTile({required this.selected, required this.onTap});
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return Semantics(
      button: true,
      selected: selected,
      label: '${t.themeAutomatic}. ${t.themeAutomaticDetail}',
      excludeSemantics: true,
      child: GestureDetector(
        onTap: onTap,
        child: Row(children: [
          SizedBox(
            width: 92,
            child: _Framed(
              selected: selected,
              child: AspectRatio(
                aspectRatio: 0.9,
                child: Stack(fit: StackFit.expand, children: [
                  ThemePreview(colors: AppTheme.linen.colors),
                  ClipRect(clipper: const _RightHalf(), child: ThemePreview(colors: AppTheme.ember.colors)),
                ]),
              ),
            ),
          ),
          const SizedBox(width: 16),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(t.themeAutomatic, style: TextStyle(fontSize: 16, fontWeight: selected ? FontWeight.w700 : FontWeight.w600)),
              const SizedBox(height: 4),
              Text(t.themeAutomaticDetail, style: TextStyle(fontSize: 14, color: c.text3)),
            ]),
          ),
        ]),
      ),
    );
  }
}

class _RightHalf extends CustomClipper<Rect> {
  const _RightHalf();

  @override
  Rect getClip(Size size) => Rect.fromLTRB(size.width / 2, 0, size.width, size.height);

  @override
  bool shouldReclip(_RightHalf oldClipper) => false;
}

/// The frame around a preview, with the current theme's accent when it's the chosen one.
class _Framed extends StatelessWidget {
  const _Framed({required this.selected, required this.child});
  final bool selected;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Stack(children: [
      Container(
        padding: const EdgeInsets.all(3),
        decoration: BoxDecoration(
          borderRadius: BorderRadius.circular(17),
          border: Border.all(color: selected ? c.accentText : c.line, width: selected ? 2.5 : 1),
        ),
        child: ClipRRect(borderRadius: BorderRadius.circular(13), child: child),
      ),
      if (selected)
        Positioned(
          right: 7,
          top: 7,
          child: Container(
            width: 22,
            height: 22,
            decoration: BoxDecoration(color: c.accent, shape: BoxShape.circle, border: Border.all(color: c.onAccent, width: 1.5)),
            child: Icon(AppIcons.check, size: 13, color: c.onAccent),
          ),
        ),
    ]);
  }
}

/// A small picture of the library in [colors]: title, a card, tiles, a button and the
/// navigation bar.
class ThemePreview extends StatelessWidget {
  const ThemePreview({super.key, required this.colors});
  final ShareColors colors;

  @override
  Widget build(BuildContext context) {
    final c = colors;
    Widget bar(double widthFactor, Color color, {double height = 5}) => FractionallySizedBox(
          alignment: Alignment.centerLeft,
          widthFactor: widthFactor,
          child: Container(height: height, decoration: BoxDecoration(color: color, borderRadius: BorderRadius.circular(3))),
        );
    return ColoredBox(
      color: c.bg,
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Expanded(
          child: Padding(
            padding: const EdgeInsets.fromLTRB(8, 9, 8, 6),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              bar(0.55, c.text, height: 6),
              const SizedBox(height: 7),
              Container(
                padding: const EdgeInsets.all(5),
                decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(6), border: Border.all(color: c.lineSoft)),
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  bar(0.8, c.text2, height: 3.5),
                  const SizedBox(height: 3),
                  bar(0.5, c.text3, height: 3.5),
                ]),
              ),
              const SizedBox(height: 5),
              // The library's tiles take what's left.
              Expanded(
                child: Row(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  for (final (i, tone) in [ShareColors.tones[0], ShareColors.tones[2], ShareColors.tones[4]].indexed) ...[
                    if (i > 0) const SizedBox(width: 3),
                    Expanded(child: DecoratedBox(decoration: BoxDecoration(color: tone, borderRadius: BorderRadius.circular(4)))),
                  ],
                ]),
              ),
              const SizedBox(height: 5),
              Container(
                height: 11,
                alignment: Alignment.center,
                decoration: BoxDecoration(color: c.accent, borderRadius: BorderRadius.circular(5)),
                child: FractionallySizedBox(
                  widthFactor: 0.45,
                  child: Container(height: 3, decoration: BoxDecoration(color: c.onAccent, borderRadius: BorderRadius.circular(2))),
                ),
              ),
            ]),
          ),
        ),
        Container(
          height: 13,
          color: c.bar,
          child: Row(mainAxisAlignment: MainAxisAlignment.spaceEvenly, children: [
            for (final (i, color) in [c.accentText, c.text3, c.text3].indexed)
              Container(
                width: i == 0 ? 14 : 6,
                height: 6,
                decoration: BoxDecoration(color: i == 0 ? c.accentSoft : color, borderRadius: BorderRadius.circular(3)),
                alignment: Alignment.center,
                child: i == 0 ? Container(width: 4, height: 4, decoration: BoxDecoration(color: color, shape: BoxShape.circle)) : null,
              ),
          ]),
        ),
      ]),
    );
  }
}
