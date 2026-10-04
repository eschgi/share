import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

/// The app's colours, by role. Ember is the mockups' own (docs/share-mockup.html); the other
/// themes give the same roles other values, so every screen works in each of them.
@immutable
class ShareColors extends ThemeExtension<ShareColors> {
  const ShareColors({
    required this.brightness,
    required this.bg,
    required this.bar,
    required this.s1,
    required this.s2,
    required this.s3,
    required this.line,
    required this.lineSoft,
    required this.text,
    required this.text2,
    required this.text3,
    required this.accent,
    required this.onAccent,
    required this.accentText,
    required this.accentSoft,
    required this.ok,
    required this.okStrong,
    required this.okSoft,
    required this.danger,
    required this.dangerStrong,
    required this.dangerSoft,
    required this.warn,
    required this.warnSoft,
    required this.shadow,
  });

  final Brightness brightness;
  final Color bg; // the page
  final Color bar; // the navigation bar
  final Color s1, s2, s3; // cards, and what sits on them
  final Color line, lineSoft; // borders and dividers
  final Color text, text2, text3; // text, quieter text, quietest text
  final Color accent; // filled buttons
  final Color onAccent; // text on them
  final Color accentText; // links, selected things
  final Color accentSoft; // behind selected things
  final Color ok, okStrong, okSoft;
  final Color danger, dangerStrong, dangerSoft;
  final Color warn, warnSoft;
  final Color shadow; // under the pictures of the first screen

  bool get isDark => brightness == Brightness.dark;

  /// Muted placeholder tones for tiles without a thumbnail, as on the website.
  static const tones = [
    Color(0xFF7B6352), Color(0xFF586B66), Color(0xFF5A6782), Color(0xFF81594C), Color(0xFF65704F), //
    Color(0xFF7D5E66), Color(0xFF80704F), Color(0xFF4D5E50), Color(0xFF6E6152), Color(0xFF4F5A6B),
  ];

  /// Avatar colours: background and letter.
  static const avatars = [
    (Color(0xFFE3A07F), Color(0xFF3A1D10)),
    (Color(0xFFD9BFA6), Color(0xFF3A2A1C)),
    (Color(0xFFB7C8BE), Color(0xFF1F2E26)),
  ];

  static Color tone(String id) => tones[_hash(id) % tones.length];
  static (Color, Color) avatar(String id) => avatars[_hash(id) % avatars.length];

  static int _hash(String s) {
    var h = 0;
    for (final c in s.codeUnits) {
      h = (h * 31 + c) & 0x7fffffff;
    }
    return h;
  }

  @override
  ShareColors copyWith() => this;

  /// Switching themes fades from one to the other.
  @override
  ShareColors lerp(ShareColors? other, double t) {
    if (other == null) return this;
    Color l(Color a, Color b) => Color.lerp(a, b, t)!;
    return ShareColors(
      brightness: t < 0.5 ? brightness : other.brightness,
      bg: l(bg, other.bg),
      bar: l(bar, other.bar),
      s1: l(s1, other.s1),
      s2: l(s2, other.s2),
      s3: l(s3, other.s3),
      line: l(line, other.line),
      lineSoft: l(lineSoft, other.lineSoft),
      text: l(text, other.text),
      text2: l(text2, other.text2),
      text3: l(text3, other.text3),
      accent: l(accent, other.accent),
      onAccent: l(onAccent, other.onAccent),
      accentText: l(accentText, other.accentText),
      accentSoft: l(accentSoft, other.accentSoft),
      ok: l(ok, other.ok),
      okStrong: l(okStrong, other.okStrong),
      okSoft: l(okSoft, other.okSoft),
      danger: l(danger, other.danger),
      dangerStrong: l(dangerStrong, other.dangerStrong),
      dangerSoft: l(dangerSoft, other.dangerSoft),
      warn: l(warn, other.warn),
      warnSoft: l(warnSoft, other.warnSoft),
      shadow: l(shadow, other.shadow),
    );
  }
}

// Ok, danger and warning read the same on every dark theme; the light ones need darker ink.
const _okDark = Color(0xFF72CB9A), _okStrong = Color(0xFF2E8553), _okSoftDark = Color(0x2472CB9A);
const _dangerDark = Color(0xFFF2826F), _dangerStrong = Color(0xFFC8463A), _dangerSoftDark = Color(0x24F2826F);
const _warnDark = Color(0xFFE9BB68), _warnSoftDark = Color(0x21E9BB68);
const _okLight = Color(0xFF257346), _okSoftLight = Color(0x1F2E8553);
const _dangerLight = Color(0xFFB23A2B), _dangerSoftLight = Color(0x1CC8463A);
const _warnLight = Color(0xFF8A5C00), _warnSoftLight = Color(0x2EE9BB68);

/// The themes to pick from in the settings.
enum AppTheme {
  /// Warm dark brown with terracotta: the mockups.
  ember(ShareColors(
    brightness: Brightness.dark,
    bg: Color(0xFF16120F), bar: Color(0xFF1C1713), s1: Color(0xFF211B17), s2: Color(0xFF2A231E), s3: Color(0xFF352D27), //
    line: Color(0xFF3E352E), lineSoft: Color(0xFF2C2520),
    text: Color(0xFFF4ECE3), text2: Color(0xFFBFB2A4), text3: Color(0xFF8E8174),
    accent: Color(0xFFC95E34), onAccent: Color(0xFFFFF7F1), accentText: Color(0xFFEE8D61), accentSoft: Color(0x26E87D50),
    ok: _okDark, okStrong: _okStrong, okSoft: _okSoftDark,
    danger: _dangerDark, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftDark,
    warn: _warnDark, warnSoft: _warnSoftDark,
    shadow: Color(0x80000000),
  )),

  /// Night blue with a clear blue.
  midnight(ShareColors(
    brightness: Brightness.dark,
    bg: Color(0xFF0F141B), bar: Color(0xFF131A23), s1: Color(0xFF161E28), s2: Color(0xFF1D2633), s3: Color(0xFF263140), //
    line: Color(0xFF303D4E), lineSoft: Color(0xFF1E2834),
    text: Color(0xFFE9EFF6), text2: Color(0xFFA9B5C4), text3: Color(0xFF7A8797),
    accent: Color(0xFF3B7DD8), onAccent: Color(0xFFF5F9FF), accentText: Color(0xFF86B6F7), accentSoft: Color(0x2686B6F7),
    ok: _okDark, okStrong: _okStrong, okSoft: _okSoftDark,
    danger: _dangerDark, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftDark,
    warn: _warnDark, warnSoft: _warnSoftDark,
    shadow: Color(0x99000000),
  )),

  /// Forest green with olive.
  moss(ShareColors(
    brightness: Brightness.dark,
    bg: Color(0xFF111512), bar: Color(0xFF151A16), s1: Color(0xFF19201B), s2: Color(0xFF202923), s3: Color(0xFF29342C), //
    line: Color(0xFF334137), lineSoft: Color(0xFF212B24),
    text: Color(0xFFEAF0EA), text2: Color(0xFFAEBDB0), text3: Color(0xFF7F8E82),
    accent: Color(0xFF6B8A38), onAccent: Color(0xFFF7FBEF), accentText: Color(0xFFB3CF7A), accentSoft: Color(0x26B3CF7A),
    ok: Color(0xFF6FD0B5), okStrong: Color(0xFF237A66), okSoft: Color(0x246FD0B5),
    danger: _dangerDark, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftDark,
    warn: _warnDark, warnSoft: _warnSoftDark,
    shadow: Color(0x80000000),
  )),

  /// Aubergine with violet.
  plum(ShareColors(
    brightness: Brightness.dark,
    bg: Color(0xFF151118), bar: Color(0xFF1A151E), s1: Color(0xFF1F1924), s2: Color(0xFF28202E), s3: Color(0xFF322939), //
    line: Color(0xFF3D3346), lineSoft: Color(0xFF261F2C),
    text: Color(0xFFF2ECF5), text2: Color(0xFFBEB2C6), text3: Color(0xFF8E8298),
    accent: Color(0xFF9B5CC9), onAccent: Color(0xFFFBF5FF), accentText: Color(0xFFC9A0F0), accentSoft: Color(0x26C9A0F0),
    ok: _okDark, okStrong: _okStrong, okSoft: _okSoftDark,
    danger: _dangerDark, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftDark,
    warn: _warnDark, warnSoft: _warnSoftDark,
    shadow: Color(0x80000000),
  )),

  /// True black with terracotta: dark, and easy on OLED batteries.
  black(ShareColors(
    brightness: Brightness.dark,
    bg: Color(0xFF000000), bar: Color(0xFF0B0A09), s1: Color(0xFF121010), s2: Color(0xFF1B1817), s3: Color(0xFF262221), //
    line: Color(0xFF302B28), lineSoft: Color(0xFF1B1817),
    text: Color(0xFFF4ECE3), text2: Color(0xFFBFB2A4), text3: Color(0xFF8E8174),
    accent: Color(0xFFC95E34), onAccent: Color(0xFFFFF7F1), accentText: Color(0xFFEE8D61), accentSoft: Color(0x26E87D50),
    ok: _okDark, okStrong: _okStrong, okSoft: _okSoftDark,
    danger: _dangerDark, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftDark,
    warn: _warnDark, warnSoft: _warnSoftDark,
    shadow: Color(0x00000000),
  )),

  /// Light: cream and terracotta, Ember by day.
  linen(ShareColors(
    brightness: Brightness.light,
    bg: Color(0xFFF6F0E9), bar: Color(0xFFEEE6DC), s1: Color(0xFFFFFCF8), s2: Color(0xFFF1E9E0), s3: Color(0xFFE6DCD1), //
    line: Color(0xFFD9CBBD), lineSoft: Color(0xFFE9DFD4),
    text: Color(0xFF2A1F18), text2: Color(0xFF5F5147), text3: Color(0xFF85766A),
    accent: Color(0xFFC45A30), onAccent: Color(0xFFFFFFFF), accentText: Color(0xFFA4461F), accentSoft: Color(0x1FC45A30),
    ok: _okLight, okStrong: _okStrong, okSoft: _okSoftLight,
    danger: _dangerLight, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftLight,
    warn: _warnLight, warnSoft: _warnSoftLight,
    shadow: Color(0x2E3A2412),
  )),

  /// Light: cool white and blue.
  frost(ShareColors(
    brightness: Brightness.light,
    bg: Color(0xFFF3F6FA), bar: Color(0xFFE8EDF4), s1: Color(0xFFFFFFFF), s2: Color(0xFFEDF1F6), s3: Color(0xFFE0E7EF), //
    line: Color(0xFFCFD8E3), lineSoft: Color(0xFFE3E9F0),
    text: Color(0xFF16202B), text2: Color(0xFF4A5767), text3: Color(0xFF738091),
    accent: Color(0xFF2F6BCB), onAccent: Color(0xFFFFFFFF), accentText: Color(0xFF2558A8), accentSoft: Color(0x1F2F6BCB),
    ok: _okLight, okStrong: _okStrong, okSoft: _okSoftLight,
    danger: _dangerLight, dangerStrong: _dangerStrong, dangerSoft: _dangerSoftLight,
    warn: _warnLight, warnSoft: _warnSoftLight,
    shadow: Color(0x2E16202B),
  ));

  const AppTheme(this.colors);
  final ShareColors colors;

  static AppTheme? parse(String? name) => values.asNameMap()[name];
}

/// The theme setting: one of [AppTheme] by name, or [automatic]. Unset is Ember.
abstract final class ThemeChoice {
  /// Linen by day, Ember at night, as the phone switches.
  static const automatic = 'auto';

  static (ShareColors light, ShareColors dark, ThemeMode mode) resolve(String? setting) {
    if (setting == automatic) return (AppTheme.linen.colors, AppTheme.ember.colors, ThemeMode.system);
    final c = (AppTheme.parse(setting) ?? AppTheme.ember).colors;
    return (c, c, c.isDark ? ThemeMode.dark : ThemeMode.light);
  }

  /// The viewer's colours: dark in every theme. A light one lends it its dark sibling.
  static ShareColors viewer(String? setting) {
    final (_, dark, _) = resolve(setting);
    if (dark.isDark) return dark;
    return (setting == AppTheme.frost.name ? AppTheme.midnight : AppTheme.ember).colors;
  }
}

extension ShareTheme on BuildContext {
  ShareColors get colors => Theme.of(this).extension<ShareColors>()!;
}

/// Noto Serif Bold, the mockups' heading face.
const serif = 'NotoSerif';

/// Roboto Mono, for PINs and paths.
const mono = 'RobotoMono';

ThemeData shareTheme([ShareColors? colors]) {
  final c = colors ?? AppTheme.ember.colors;
  final scheme = ColorScheme(
    brightness: c.brightness,
    primary: c.accent,
    onPrimary: c.onAccent,
    primaryContainer: c.accentSoft,
    onPrimaryContainer: c.accentText,
    secondary: c.accentText,
    onSecondary: c.bg,
    secondaryContainer: c.accentSoft,
    onSecondaryContainer: c.accentText,
    error: c.danger,
    onError: c.bg,
    errorContainer: c.dangerSoft,
    onErrorContainer: c.danger,
    surface: c.bg,
    onSurface: c.text,
    onSurfaceVariant: c.text2,
    surfaceContainerLowest: c.bg,
    surfaceContainerLow: c.s1,
    surfaceContainer: c.s1,
    surfaceContainerHigh: c.s2,
    surfaceContainerHighest: c.s3,
    outline: c.line,
    outlineVariant: c.lineSoft,
    inverseSurface: c.text,
    onInverseSurface: c.bg,
    scrim: Colors.black,
    shadow: Colors.black,
  );
  // Roboto by name: on Android that is the system font; tests load it from the SDK.
  final base = ThemeData(brightness: c.brightness, colorScheme: scheme, useMaterial3: true, fontFamily: 'Roboto');
  final text = base.textTheme.apply(bodyColor: c.text, displayColor: c.text);
  const buttonShape = RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(18)));
  const buttonText = TextStyle(fontFamily: 'Roboto', fontSize: 18, fontWeight: FontWeight.w700, letterSpacing: 0.1);
  return base.copyWith(
    scaffoldBackgroundColor: c.bg,
    canvasColor: c.bg,
    extensions: [c],
    // Merged into the base styles, so they keep its font family.
    textTheme: text.copyWith(
      headlineLarge: text.headlineLarge!.copyWith(fontFamily: serif, fontWeight: FontWeight.w700, fontSize: 32, height: 1.13, letterSpacing: -0.4),
      headlineMedium: text.headlineMedium!.copyWith(fontFamily: serif, fontWeight: FontWeight.w700, fontSize: 27, letterSpacing: -0.2),
      headlineSmall: text.headlineSmall!.copyWith(fontFamily: serif, fontWeight: FontWeight.w700, fontSize: 23),
      bodyLarge: text.bodyLarge!.copyWith(fontSize: 17, height: 1.55, letterSpacing: 0),
      bodyMedium: text.bodyMedium!.copyWith(fontSize: 15, height: 1.45, letterSpacing: 0),
      bodySmall: text.bodySmall!.copyWith(fontSize: 13, height: 1.45, color: c.text3, letterSpacing: 0),
      titleMedium: text.titleMedium!.copyWith(fontSize: 16, fontWeight: FontWeight.w600, letterSpacing: 0),
      labelLarge: text.labelLarge!.copyWith(fontSize: 15, fontWeight: FontWeight.w500, letterSpacing: 0),
    ),
    appBarTheme: AppBarTheme(
      backgroundColor: c.bg,
      surfaceTintColor: Colors.transparent,
      foregroundColor: c.text,
      elevation: 0,
      // Status bar icons that stand out from the page.
      systemOverlayStyle: (c.isDark ? SystemUiOverlayStyle.light : SystemUiOverlayStyle.dark).copyWith(
        statusBarColor: Colors.transparent,
        systemNavigationBarColor: c.bar,
        systemNavigationBarIconBrightness: c.isDark ? Brightness.light : Brightness.dark,
      ),
      titleTextStyle: TextStyle(fontFamily: serif, fontWeight: FontWeight.w700, fontSize: 27, color: c.text),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: c.s1,
      contentPadding: const EdgeInsets.symmetric(horizontal: 16, vertical: 18),
      hintStyle: TextStyle(color: c.text3, fontSize: 17),
      border: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide(color: c.line, width: 1.5)),
      enabledBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide(color: c.line, width: 1.5)),
      focusedBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide(color: c.accentText, width: 1.5)),
      errorBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide(color: c.danger, width: 1.5)),
      focusedErrorBorder: OutlineInputBorder(borderRadius: BorderRadius.circular(14), borderSide: BorderSide(color: c.danger, width: 1.5)),
      errorStyle: TextStyle(color: c.danger, fontSize: 14),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: c.accent,
        foregroundColor: c.onAccent,
        disabledBackgroundColor: c.s2,
        disabledForegroundColor: c.text3,
        minimumSize: const Size.fromHeight(60),
        shape: buttonShape,
        textStyle: buttonText,
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: c.text,
        side: BorderSide(color: c.line, width: 1.5),
        minimumSize: const Size.fromHeight(60),
        shape: buttonShape,
        textStyle: buttonText,
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(
        foregroundColor: c.accentText,
        minimumSize: const Size(64, 52),
        textStyle: const TextStyle(fontFamily: 'Roboto', fontSize: 16.5, fontWeight: FontWeight.w500),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: c.bar,
      surfaceTintColor: Colors.transparent,
      indicatorColor: c.accentSoft,
      height: 80,
      iconTheme: WidgetStateProperty.resolveWith(
        (s) => IconThemeData(size: 22, color: s.contains(WidgetState.selected) ? c.accentText : c.text3),
      ),
      labelTextStyle: WidgetStateProperty.resolveWith(
        (s) => TextStyle(fontSize: 12.5, fontWeight: FontWeight.w500, color: s.contains(WidgetState.selected) ? c.text : c.text3),
      ),
    ),
    bottomSheetTheme: BottomSheetThemeData(
      backgroundColor: c.s1,
      surfaceTintColor: Colors.transparent,
      showDragHandle: true,
      dragHandleColor: c.line,
      shape: const RoundedRectangleBorder(borderRadius: BorderRadius.vertical(top: Radius.circular(28))),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: c.s1,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(24)),
    ),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: c.s3,
      contentTextStyle: TextStyle(color: c.text, fontSize: 15),
      behavior: SnackBarBehavior.floating,
    ),
    dividerTheme: DividerThemeData(color: c.lineSoft, thickness: 1, space: 1),
    floatingActionButtonTheme: FloatingActionButtonThemeData(
      backgroundColor: c.accent,
      foregroundColor: c.onAccent,
      elevation: 6,
      highlightElevation: 8,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(22)),
      extendedPadding: const EdgeInsets.symmetric(horizontal: 26),
      extendedSizeConstraints: const BoxConstraints(minHeight: 60),
      extendedTextStyle: const TextStyle(fontFamily: 'Roboto', fontSize: 17, fontWeight: FontWeight.w600),
    ),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: c.accent, linearTrackColor: c.s3),
    // Who sees a folder (screen 41): on in the accent, off quietly; an admin's stays on, faded.
    switchTheme: SwitchThemeData(
      thumbColor: WidgetStateProperty.resolveWith((s) {
        final color = s.contains(WidgetState.selected) ? Colors.white : c.text3;
        return s.contains(WidgetState.disabled) ? color.withValues(alpha: 0.7) : color;
      }),
      trackColor: WidgetStateProperty.resolveWith((s) {
        final color = s.contains(WidgetState.selected) ? c.accent : c.s3;
        return s.contains(WidgetState.disabled) ? color.withValues(alpha: 0.45) : color;
      }),
      trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
    ),
    // The folders an invite gives (screen 47).
    checkboxTheme: CheckboxThemeData(
      fillColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.selected) ? c.accent : Colors.transparent),
      checkColor: const WidgetStatePropertyAll(Colors.white),
      side: WidgetStateBorderSide.resolveWith((s) => BorderSide(color: s.contains(WidgetState.selected) ? c.accent : c.text3, width: 2)),
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(7)),
    ),
  );
}
