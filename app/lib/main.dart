import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'app.dart';
import 'data/platform.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  _registerLicenses();
  runApp(ShareApp(services: AppServices(platform: ChannelPlatform())));
}

/// For the licence page (About): the bundled fonts', and those of the Android libraries the
/// Dart packages don't bring along, all Apache 2.0, as Share itself is.
void _registerLicenses() {
  LicenseRegistry.addLicense(() async* {
    for (final (package, file) in const [
      ('Noto Serif', 'OFL-NotoSerif.txt'),
      ('Roboto Mono', 'OFL-RobotoMono.txt'),
      ('Lucide', 'LICENSE-Lucide.txt'),
    ]) {
      yield LicenseEntryWithLineBreaks([package], await rootBundle.loadString('assets/fonts/$file'));
    }
    final apache = await rootBundle.loadString('assets/licenses/Apache-2.0.txt');
    yield LicenseEntryWithLineBreaks(['Share'], 'Copyright 2026 Stefan Eschgfäller\n\n$apache');
    yield LicenseEntryWithLineBreaks(['AndroidX', 'AndroidX Media3', 'Kotlin'], apache);
  });
}
