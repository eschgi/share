import 'dart:typed_data';

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/format.dart';

import 'admin_test.dart' show adminServer, openSettings;
import 'app_test.dart' show signedInPhone, startApp;
import 'support/contract.dart';
import 'support/fake_platform.dart';
import 'support/fonts.dart';

/// The width and height a PNG says it has.
(int, int) pngSize(Uint8List png) {
  final b = ByteData.sublistView(png);
  return (b.getUint32(16), b.getUint32(20));
}

/// Upload PINs, then Print in the permanent PIN's menu (screen 69).
Future<void> openPrint(WidgetTester tester) async {
  await openSettings(tester);
  await tester.tap(find.text('Upload PINs'));
  await tester.pumpAndSettle();
  await tester.tap(find.byType(PopupMenuButton<VoidCallback>));
  await tester.pumpAndSettle();
  await tester.tap(find.text('Print'));
  await tester.pumpAndSettle();
}

/// Print or save as PDF; drawing the page at 300 dpi takes real time.
Future<void> printIt(WidgetTester tester, FakePlatform platform) async {
  await tester.ensureVisible(find.text('Print or save as PDF'));
  await tester.pumpAndSettle();
  await tester.runAsync(() async {
    await tester.tap(find.text('Print or save as PDF'));
    for (var i = 0; i < 200 && platform.printed.isEmpty; i++) {
      await Future<void>.delayed(const Duration(milliseconds: 50));
    }
  });
  await tester.pumpAndSettle();
}

void main() {
  setUpAll(loadFonts);

  testWidgets("a permanent PIN prints as a poster, in the server's language, at 300 dpi", (tester) async {
    await withClock(Clock.fixed(DateTime(2026, 10, 10, 9)), () async {
      final server = adminServer();
      server.routes['GET /api/info'] = (_) => server.json({...contractResponse('api/info.json'), 'default_language': 'de'});
      final platform = signedInPhone();
      await startApp(tester, platform, server);
      await openPrint(tester);
      expect(find.text('Print K7M2Q'), findsOneWidget);
      expect(find.widgetWithText(TextField, 'Family'), findsOneWidget, reason: "the folder's name, to begin with");
      expect(find.widgetWithText(TextField, 'Teilt eure Fotos von heute mit uns'), findsOneWidget, reason: 'the line guests read');
      expect(find.text('Printed in the language guests see first: Deutsch.'), findsOneWidget);
      expect(find.text('Samstag, 10. Oktober 2026'), findsWidgets, reason: 'the day, as the poster says it');
      expect(find.text('Mit der Handykamera scannen'), findsOneWidget, reason: 'the preview');
      expect(find.text('Kein QR-Leser? Öffne share.example.com und tippe:'), findsOneWidget);

      await tester.enterText(find.widgetWithText(TextField, 'Family'), 'Anna & Marco');
      await tester.pump();
      await printIt(tester, platform);
      final page = platform.printed.single;
      expect(page.name, 'Anna & Marco');
      expect(pngSize(page.png), (2480, 3507), reason: 'A4 at 300 dpi');
    });
  });

  testWidgets('table cards have no date, and the code to type can go', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, adminServer());
    await openPrint(tester);
    expect(find.textContaining('the language guests see first'), findsNothing, reason: "the app's language is the server's");
    expect(find.text('Date'), findsOneWidget);
    await tester.tap(find.text('Table cards'));
    await tester.pumpAndSettle();
    expect(find.text('Date'), findsNothing);
    expect(find.text('No QR reader? Open share.example.com and type:'), findsNothing, reason: 'cards have only the code');
    expect(find.text("Scan with your phone's camera"), findsNWidgets(4));

    // The code's letters: on the PIN's card behind, and on each of the four cards.
    expect(find.text('7'), findsNWidgets(5));
    await tester.ensureVisible(find.byType(Switch));
    await tester.tap(find.byType(Switch));
    await tester.pumpAndSettle();
    expect(find.text('7'), findsOneWidget, reason: 'only on the PIN\'s card now');
    await printIt(tester, platform);
    expect(platform.printed.single.name, 'Family');
  });

  test('a poster names the day in full, as each language writes it', () {
    final day = DateTime(2026, 10, 10);
    expect(formatLongDay(day, 'en'), 'Saturday, 10 October 2026');
  });
}
