import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';
import 'package:share_app/ui/settings_screen.dart';
import 'package:share_app/ui/theme.dart';

import 'app_test.dart' show signedInPhone, startApp, today;
import 'support/fake_server.dart';
import 'support/fonts.dart';

void main() {
  setUpAll(loadFonts);

  Color background(WidgetTester tester) => Theme.of(tester.element(find.byType(SettingsScreen))).scaffoldBackgroundColor;

  Future<void> pick(WidgetTester tester, String name) async {
    await tester.tap(find.text('Theme'));
    await tester.pumpAndSettle();
    await tester.ensureVisible(find.text(name));
    await tester.pumpAndSettle();
    await tester.tap(find.text(name));
    await tester.pumpAndSettle();
    await tester.tapAt(const Offset(20, 20)); // closes the sheet
    await tester.pumpAndSettle();
  }

  testWidgets('a theme takes effect at once and is kept', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    expect(background(tester), AppTheme.ember.colors.bg);
    expect(find.text('Ember'), findsOneWidget, reason: 'the row says which one');

    await pick(tester, 'Midnight');
    expect(platform.secrets['theme'], 'midnight');
    expect(background(tester), AppTheme.midnight.colors.bg);
    expect(find.text('Midnight'), findsOneWidget);

    await pick(tester, 'Ember');
    expect(platform.secrets.containsKey('theme'), isFalse, reason: 'the default is stored as nothing');
  });

  testWidgets('automatic follows the phone: Linen by day, Ember at night', (tester) async {
    addTearDown(tester.platformDispatcher.clearPlatformBrightnessTestValue);
    tester.platformDispatcher.platformBrightnessTestValue = Brightness.light;
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    await pick(tester, 'Automatic');
    expect(platform.secrets['theme'], 'auto');
    expect(background(tester), AppTheme.linen.colors.bg);
    expect(find.text('Automatic · Linen'), findsOneWidget);

    tester.platformDispatcher.platformBrightnessTestValue = Brightness.dark;
    await tester.pumpAndSettle();
    expect(background(tester), AppTheme.ember.colors.bg);
    expect(find.text('Automatic · Ember'), findsOneWidget);
  });

  testWidgets('a stored theme is there from the start', (tester) async {
    await startApp(tester, signedInPhone()..secrets['theme'] = 'frost', FakeServer());
    await tester.tap(find.text('Settings').last);
    await tester.pumpAndSettle();
    expect(background(tester), AppTheme.frost.colors.bg);
    expect(Theme.of(tester.element(find.byType(SettingsScreen))).brightness, Brightness.light);
  });

  for (final (setting, dark) in [('linen', AppTheme.ember), ('frost', AppTheme.midnight), ('plum', AppTheme.plum)]) {
    testWidgets('the viewer is dark in $setting too, its menu and sheets with it', (tester) async {
      await startApp(tester, signedInPhone()..secrets['theme'] = setting, FakeServer()..addDay(today(), 6));
      await tester.tap(find.byType(LibraryTile).first);
      await tester.pumpAndSettle();
      await tester.tap(find.byIcon(AppIcons.more));
      await tester.pumpAndSettle();
      await tester.tap(find.text('Details').last);
      await tester.pumpAndSettle();
      expect(Theme.of(tester.element(find.text('Details').first)).extension<ShareColors>(), same(dark.colors), reason: 'the viewer');
      expect(Theme.of(tester.element(find.textContaining(' · image/').last)).extension<ShareColors>(), same(dark.colors), reason: 'the details');
    });
  }
}
