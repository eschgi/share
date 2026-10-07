import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// The app's version is Share's, the one in VERSION at the top of the repository, with the
/// versionCode scripts/version.sh makes of it; `scripts/version.sh set` changes both.
void main() {
  test('pubspec.yaml has the version of VERSION', () {
    final version = File('../VERSION').readAsStringSync().trim();
    final parts = version.split('-').first.split('.').map(int.parse).toList();
    final code = parts[0] * 10000 + parts[1] * 100 + parts[2];
    final pubspec = RegExp(r'^version: *(\S+)$', multiLine: true).firstMatch(File('pubspec.yaml').readAsStringSync())?.group(1);
    expect(pubspec, '$version+$code');
  });
}
