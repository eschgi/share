import 'package:intl/intl.dart';

/// 1.4 GB, 520 MB, 480 KB: decimal units as phones show sizes, one decimal below 10.
String formatBytes(int bytes, String locale) {
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var v = bytes.toDouble();
  var u = 0;
  while (v >= 1000 && u < units.length - 1) {
    v /= 1000;
    u++;
  }
  final digits = u == 0 || v >= 10 ? 0 : 1;
  final f = NumberFormat.decimalPatternDigits(locale: locale, decimalDigits: digits);
  return '${f.format(v)} ${units[u]}';
}

/// 0:18, 1:15, 1:02:03 for the video badge.
String formatDuration(int ms) {
  final s = (ms / 1000).round();
  final h = s ~/ 3600, m = (s % 3600) ~/ 60, sec = s % 60;
  String two(int n) => n.toString().padLeft(2, '0');
  return h > 0 ? '$h:${two(m)}:${two(sec)}' : '$m:${two(sec)}';
}

/// The header of an upload day: today, yesterday, or the date the way each language writes
/// it (Sunday, 27 Sep; Sonntag, 27. September; Domenica 27 settembre).
String formatDay(String day, DateTime now, String locale, {required String today, required String yesterday}) {
  final d = DateTime.tryParse(day);
  if (d == null) return day;
  final todayDate = DateTime(now.year, now.month, now.day);
  final diff = todayDate.difference(DateTime(d.year, d.month, d.day)).inDays;
  if (diff == 0) return today;
  if (diff == 1) return yesterday;
  final pattern = switch (locale) {
    'de' => 'EEEE, d. MMMM',
    'it' => 'EEEE d MMMM',
    _ => 'EEEE, d MMM',
  };
  final withYear = d.year != now.year ? ' y' : '';
  final text = DateFormat('$pattern$withYear', locale).format(d);
  return text.substring(0, 1).toUpperCase() + text.substring(1);
}

/// The day a printed page names, in full: Saturday, 10 October 2026; Samstag, 10. Oktober 2026;
/// Sabato 10 ottobre 2026.
String formatLongDay(DateTime day, String locale) {
  final pattern = switch (locale) {
    'de' => 'EEEE, d. MMMM y',
    'it' => 'EEEE d MMMM y',
    _ => 'EEEE, d MMMM y',
  };
  final text = DateFormat(pattern, locale).format(day);
  return text.substring(0, 1).toUpperCase() + text.substring(1);
}

/// When something ends: "today, 21:00", "tomorrow, 21:00" or a date with the time, in the
/// same style as the website's.
String formatWhen(DateTime when, DateTime now, String locale) {
  final time = DateFormat.Hm(locale).format(when);
  final day = DateTime(when.year, when.month, when.day);
  final today = DateTime(now.year, now.month, now.day);
  final diff = day.difference(today).inDays;
  final words = switch (locale) {
    'de' => const ('heute,', 'morgen,', ', '),
    'it' => const ('oggi alle', 'domani alle', ' alle '),
    _ => const ('today,', 'tomorrow,', ', '),
  };
  if (diff == 0) return '${words.$1} $time';
  if (diff == 1) return '${words.$2} $time';
  return '${DateFormat.MMMEd(locale).format(when)}${words.$3}$time';
}

/// "12:32" for the viewer's title.
String formatTime(DateTime t, String locale) => DateFormat.Hm(locale).format(t);

/// "12 Sep", "12. Sep.", "12 set": a date in the current year, for "since" lines.
String formatShortDate(DateTime d, DateTime now, String locale) {
  final pattern = switch (locale) {
    'de' => d.year == now.year ? 'd. MMM' : 'd. MMM y',
    _ => d.year == now.year ? 'd MMM' : 'd MMM y',
  };
  return DateFormat(pattern, locale).format(d);
}

/// How long ago a phone was used: 0 today, 1 yesterday, and so on.
int daysAgo(DateTime when, DateTime now) =>
    DateTime(now.year, now.month, now.day).difference(DateTime(when.year, when.month, when.day)).inDays;
