import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_test/flutter_test.dart';

/// Registers the real fonts: Roboto from the Flutter SDK (the phone's own), and the app's
/// bundled Noto Serif, Roboto Mono and Lucide icons. Without them, flutter test draws every
/// glyph as a box, and the screenshots are useless for comparing with the mockups.
Future<void> loadFonts() async {
  TestWidgetsFlutterBinding.ensureInitialized();
  Future<void> load(String family, List<String> paths) async {
    final loader = FontLoader(family);
    var any = false;
    for (final p in paths) {
      final f = File(p);
      if (!f.existsSync()) continue;
      any = true;
      loader.addFont(Future.value(ByteData.sublistView(f.readAsBytesSync())));
    }
    if (any) await loader.load();
  }

  final dir = _materialFonts();
  if (dir != null) {
    await load('Roboto', ['$dir/Roboto-Regular.ttf', '$dir/Roboto-Medium.ttf', '$dir/Roboto-Bold.ttf']);
    await load('MaterialIcons', ['$dir/MaterialIcons-Regular.otf']);
  }
  await load('NotoSerif', ['assets/fonts/NotoSerif-Bold.ttf']);
  await load('RobotoMono', ['assets/fonts/RobotoMono-SemiBold.ttf']);
  await load('Lucide', ['assets/fonts/Lucide.ttf']);
}

/// The SDK's font folder: from FLUTTER_ROOT, or else from where the test runner itself lives
/// (`<sdk>/bin/cache/artifacts/engine/…/flutter_tester`).
String? _materialFonts() {
  final root = Platform.environment['FLUTTER_ROOT'];
  if (root != null && root.isNotEmpty) return '$root/bin/cache/artifacts/material_fonts';
  var dir = File(Platform.resolvedExecutable).parent;
  for (var i = 0; i < 6; i++) {
    final candidate = Directory('${dir.path}/material_fonts');
    if (candidate.existsSync()) return candidate.path;
    dir = dir.parent;
  }
  return null;
}
