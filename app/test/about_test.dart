import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/icons.dart';

import 'admin_test.dart' show openSettings, tapInList;
import 'app_test.dart' show signedInPhone, startApp;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fake_server.dart';
import 'support/fonts.dart';

void main() {
  setUpAll(loadFonts);

  testWidgets('About: the versions, the license, the source code, and the licenses of the rest', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, FakeServer());
    await openSettings(tester);
    await tapInList(tester, find.text('About Share'));
    expect(find.text('0.3.0 (3)'), findsOneWidget);
    expect(find.text(contractResponse('api/about.json')['version'] as String), findsOneWidget);
    await tester.tap(find.text('Source code'));
    await tester.tap(find.text('License'));
    expect(platform.opened, ['https://github.com/eschgi/share', 'https://github.com/eschgi/share/blob/main/LICENSE']);
    await tester.tap(find.text('Open source licenses'));
    await tester.pumpAndSettle();
    expect(find.byType(LicensePage), findsOneWidget);
  });

  testWidgets('from PIN sending, without the server\'s version', (tester) async {
    final platform = FakePlatform()
      ..secrets['pin_token'] = 'shp_x'
      ..secrets['pin_session'] = jsonEncode({'server': 'https://share.example.com', 'pin_kind': 'day', 'expires_at': null});
    await startApp(tester, platform, FakeServer());
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('About Share'));
    await tester.pumpAndSettle();
    expect(find.text('0.3.0 (3)'), findsOneWidget);
    expect(find.text('The server'), findsNothing);
  });
}
