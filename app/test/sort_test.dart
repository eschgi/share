import 'package:clock/clock.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:share_app/ui/icons.dart';
import 'package:share_app/ui/library/tiles.dart';

import 'app_test.dart' show daysAgo, signedInPhone, startApp, today;
import 'support/fake_server.dart';
import 'support/fonts.dart';

/// A library with two photos sent today: one taken in June 2025 by Maria, one a guest sent with
/// a PIN, without a date; and yesterday's from Peter.
FakeServer sortServer() {
  final server = FakeServer()
    ..addDay(daysAgo(1), 1)
    ..addDay(today(), 2, startId: 10);
  Map<String, dynamic> set(int i, Map<String, dynamic> extra) => server.files[i] = {...server.files[i], ...extra};
  set(0, {'from': 'Peter', 'from_id': 'peter', 'taken_at': '${daysAgo(1)}T08:00:00Z', 'taken_day': daysAgo(1)});
  set(1, {'name': 'IMG_4471.jpg', 'from': 'Maria', 'from_id': 'maria', 'taken_at': '2025-06-14T16:32:00Z', 'taken_day': '2025-06-14'});
  set(2, {'name': 'guest.jpg'});
  return server;
}

Future<void> pickSort(WidgetTester tester, String choice) async {
  await tester.tap(find.byTooltip('Sort the library'));
  await tester.pumpAndSettle();
  await tester.tap(find.text(choice).last);
  await tester.pumpAndSettle();
  await tester.tapAt(const Offset(200, 80)); // the sheet closes
  await tester.pumpAndSettle();
}

List<String> headers(WidgetTester tester) => [for (final h in tester.widgetList<DayHeader>(find.byType(DayHeader))) h.title];

void main() {
  setUpAll(loadFonts);

  testWidgets('by date taken, a photo of June 2025 sent today sits in June 2025, and the phone remembers it', (tester) async {
    final platform = signedInPhone();
    await startApp(tester, platform, sortServer());
    expect(headers(tester), ['Today', 'Yesterday']);
    expect(find.text('By date taken, newest first'), findsNothing);

    await pickSort(tester, 'Date taken');
    expect(headers(tester), ['Today', 'Yesterday', clock.now().year == 2025 ? 'Saturday, 14 Jun' : 'Saturday, 14 Jun 2025']);
    expect(find.text('By date taken, newest first'), findsOneWidget);
    expect(find.textContaining(RegExp(r'^1 file · .* · sent today$')), findsOneWidget);
    expect(platform.secrets['library_sort'], 'taken newest');

    await pickSort(tester, 'Oldest first');
    expect(headers(tester).first, contains('14 Jun'));
    expect(find.text('By date taken, oldest first'), findsOneWidget);
    expect(platform.secrets['library_sort'], 'taken oldest');
  });

  testWidgets('by who sent it: a group for each person, and one for the guests', (tester) async {
    await startApp(tester, signedInPhone(), sortServer());
    await pickSort(tester, 'Who sent it');
    expect(headers(tester), ['Maria', 'Guests with a PIN', 'Peter']);
    expect(find.text('By who sent it'), findsOneWidget);
    expect(find.textContaining('last today'), findsNWidgets(2));
    expect(find.textContaining('last yesterday'), findsOneWidget);
  });

  testWidgets('a choice made before holds when the app starts again', (tester) async {
    final server = sortServer();
    await startApp(tester, signedInPhone()..secrets['library_sort'] = 'taken oldest', server);
    expect(server.requests.any((r) => r.url.path == '/api/files' && r.url.queryParameters['sort'] == 'taken' && r.url.queryParameters['order'] == 'oldest'), isTrue);
    expect(headers(tester).first, contains('14 Jun'));
  });

  testWidgets('the details give both dates', (tester) async {
    await startApp(tester, signedInPhone(), sortServer());
    await tester.tap(find.byType(LibraryTile).first); // Maria's, sent today, taken in June 2025
    await tester.pumpAndSettle();
    await tester.tap(find.byIcon(AppIcons.more));
    await tester.pumpAndSettle();
    await tester.tap(find.text('Details').last);
    await tester.pumpAndSettle();
    expect(find.textContaining(RegExp(r'^Taken Saturday, 14 Jun( 2025)?, \d\d:32$')), findsOneWidget);
    expect(find.textContaining(RegExp(r'^Received Today, ')), findsOneWidget);
  });
}
