import 'package:flutter/material.dart';

/// The colours of the mockups (docs/share-mockup.html), dark only.
@immutable
class ShareColors extends ThemeExtension<ShareColors> {
  const ShareColors();

  final Color bg = const Color(0xFF16120F);
  final Color bar = const Color(0xFF1C1713);
  final Color s1 = const Color(0xFF211B17);
  final Color s2 = const Color(0xFF2A231E);
  final Color s3 = const Color(0xFF352D27);
  final Color line = const Color(0xFF3E352E);
  final Color lineSoft = const Color(0xFF2C2520);
  final Color text = const Color(0xFFF4ECE3);
  final Color text2 = const Color(0xFFBFB2A4);
  final Color text3 = const Color(0xFF8E8174);
  final Color accent = const Color(0xFFC95E34);
  final Color onAccent = const Color(0xFFFFF7F1);
  final Color accentText = const Color(0xFFEE8D61);
  final Color accentSoft = const Color(0x26E87D50);
  final Color ok = const Color(0xFF72CB9A);
  final Color okStrong = const Color(0xFF2E8553);
  final Color okSoft = const Color(0x2472CB9A);
  final Color danger = const Color(0xFFF2826F);
  final Color dangerStrong = const Color(0xFFC8463A);
  final Color dangerSoft = const Color(0x24F2826F);
  final Color warn = const Color(0xFFE9BB68);
  final Color warnSoft = const Color(0x21E9BB68);

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

  @override
  ShareColors lerp(ShareColors? other, double t) => this;
}

extension ShareTheme on BuildContext {
  ShareColors get colors => Theme.of(this).extension<ShareColors>()!;
}

/// Noto Serif Bold, the mockups' heading face.
const serif = 'NotoSerif';

/// Roboto Mono, for PINs and paths.
const mono = 'RobotoMono';

ThemeData shareTheme() {
  const c = ShareColors();
  final scheme = ColorScheme(
    brightness: Brightness.dark,
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
  final base = ThemeData(brightness: Brightness.dark, colorScheme: scheme, useMaterial3: true, fontFamily: 'Roboto');
  final text = base.textTheme.apply(bodyColor: c.text, displayColor: c.text);
  const buttonShape = RoundedRectangleBorder(borderRadius: BorderRadius.all(Radius.circular(18)));
  const buttonText = TextStyle(fontFamily: 'Roboto', fontSize: 18, fontWeight: FontWeight.w700, letterSpacing: 0.1);
  return base.copyWith(
    scaffoldBackgroundColor: c.bg,
    canvasColor: c.bg,
    extensions: const [ShareColors()],
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
  );
}
