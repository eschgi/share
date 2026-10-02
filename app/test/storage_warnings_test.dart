import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/l10n/app_localizations.dart';
import 'package:share_app/ui/settings_screen.dart';

import 'support/contract.dart';

/// contract/storage_warnings.json: the app says each code in its own words, in every language.
void main() {
  final codes = (contract('storage_warnings.json')['codes'] as Map).keys.cast<String>();

  test('every code has its words', () {
    for (final lang in ['en', 'de', 'it']) {
      final t = lookupAppLocalizations(Locale(lang));
      final general = storageWarningText(t, 'raid_degraded');
      final texts = {for (final code in codes) code: storageWarningText(t, code)};
      for (final e in texts.entries) {
        expect(e.value, isNot(general), reason: '$lang ${e.key}');
      }
      expect(texts.values.toSet(), hasLength(codes.length), reason: '$lang: each its own');
    }
  });
}
