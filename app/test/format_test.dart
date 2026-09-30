import 'package:flutter_test/flutter_test.dart';
import 'package:intl/date_symbol_data_local.dart';
import 'package:share_app/ui/format.dart';

void main() {
  setUpAll(() async {
    for (final l in ['en', 'de', 'it']) {
      await initializeDateFormatting(l);
    }
  });

  test('sizes as the mockups show them', () {
    expect(formatBytes(1400000000, 'en'), '1.4 GB');
    expect(formatBytes(1400000000, 'de'), '1,4 GB');
    expect(formatBytes(74000000, 'en'), '74 MB');
    expect(formatBytes(480000, 'it'), '480 KB');
    expect(formatBytes(12, 'en'), '12 B');
  });

  test('video durations', () {
    expect(formatDuration(18000), '0:18');
    expect(formatDuration(75000), '1:15');
    expect(formatDuration(3723000), '1:02:03');
  });

  test('day headers in three languages', () {
    final now = DateTime(2026, 9, 30, 10);
    String day(String d, String l) => formatDay(d, now, l, today: 'Today', yesterday: 'Yesterday');
    expect(day('2026-09-30', 'en'), 'Today');
    expect(day('2026-09-29', 'en'), 'Yesterday');
    expect(day('2026-09-27', 'en'), 'Sunday, 27 Sep');
    expect(day('2026-09-27', 'de'), 'Sonntag, 27. September');
    expect(day('2026-09-27', 'it'), 'Domenica 27 settembre');
    expect(day('2025-12-24', 'en'), 'Wednesday, 24 Dec 2025');
  });

  test('until when', () {
    final now = DateTime(2026, 9, 30, 19);
    expect(formatWhen(DateTime(2026, 9, 30, 21), now, 'en'), 'today, 21:00');
    expect(formatWhen(DateTime(2026, 10, 1, 21), now, 'de'), 'morgen, 21:00');
    expect(formatWhen(DateTime(2026, 10, 1, 21), now, 'it'), 'domani alle 21:00');
  });
}
