import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import 'app.dart';
import 'data/platform.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  _registerFontLicenses();
  runApp(ShareApp(services: AppServices(platform: ChannelPlatform())));
}

/// The bundled fonts' licences, shown in the app's licence page.
void _registerFontLicenses() {
  LicenseRegistry.addLicense(() async* {
    for (final (package, file) in const [
      ('Noto Serif', 'OFL-NotoSerif.txt'),
      ('Roboto Mono', 'OFL-RobotoMono.txt'),
      ('Lucide', 'LICENSE-Lucide.txt'),
    ]) {
      yield LicenseEntryWithLineBreaks([package], await rootBundle.loadString('assets/fonts/$file'));
    }
  });
}
