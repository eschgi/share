/// Sending photos as ZIPs, and opening them (docs/zip-plan.md, screens 97 to 110). The bytes move
/// in Kotlin (android/app/src/main/kotlin/com/eschgi/share/zip); this is what the screens see of it,
/// as contract/app/platform.json's zip_* fixtures have it.
library;

import 'dart:typed_data';

import 'package:intl/intl.dart';

import 'models.dart';

/// Where a ZIP goes, which sets how big each may be (screen 103). The limits keep a margin below what
/// the apps take: WhatsApp's 2 GB however it counts them, Signal's 100 MB, and for email 14 MB, which
/// grows by a third in the mail and still stays under 20 MB. [other] takes a size in MB.
enum ZipWhere {
  whatsapp(1950000000),
  signal(95000000),
  email(14000000),
  any(null),
  other(null);

  const ZipWhere(this.limit);
  final int? limit;

  /// What's remembered of a choice (writeSecret 'zip_where'): its name, and for [other] the MB, as other:500.
  static ({ZipWhere where, int otherMb}) parse(String? saved) {
    if (saved != null && saved.startsWith('other:')) {
      final mb = int.tryParse(saved.substring(6)) ?? 0;
      if (mb >= minMb && mb <= maxMb) return (where: ZipWhere.other, otherMb: mb);
    }
    return (where: ZipWhere.values.asNameMap()[saved] ?? ZipWhere.whatsapp, otherMb: 500);
  }

  String save(int otherMb) => this == ZipWhere.other ? 'other:$otherMb' : name;

  /// The bytes each ZIP may have, or null for one ZIP of any size.
  int? bytes(int otherMb) => this == ZipWhere.other ? otherMb * 1000000 : limit;

  static const minMb = 1, maxMb = 4000;
}

/// More than this many ZIPs can't be picked: nobody sends them one by one.
const zipMaxParts = 100;

/// A file waiting to be packed: [onPhone] unless it has to come from the server first.
class ZipFileInfo {
  const ZipFileInfo({required this.name, required this.size, required this.type, required this.kind, required this.taken, this.onPhone = true});

  factory ZipFileInfo.fromMap(Map<Object?, Object?> m) => ZipFileInfo(
        name: m['name'] as String? ?? '',
        size: (m['size'] as num?)?.toInt() ?? 0,
        type: m['type'] as String? ?? 'application/octet-stream',
        kind: FileKind.parse(m['kind']),
        taken: DateTime.fromMillisecondsSinceEpoch((m['taken'] as num?)?.toInt() ?? 0),
        onPhone: m['on_phone'] != false,
      );

  final String name;
  final int size;
  final String type;
  final FileKind kind;
  final DateTime taken;
  final bool onPhone;
}

/// What arrived from another app: files to pack (Send as ZIP), or ZIPs to open (Open with).
sealed class ZipArrival {
  const ZipArrival();

  static ZipArrival? fromMap(Map<Object?, Object?>? m) => switch (m?['kind']) {
        'pack' => ZipToPack([for (final f in (m!['files'] as List? ?? const [])) if (f is Map) ZipFileInfo.fromMap(f)], skipped: (m['skipped'] as num?)?.toInt() ?? 0),
        'open' => const ZipToOpen(),
        _ => null,
      };
}

class ZipToPack extends ZipArrival {
  const ZipToPack(this.files, {this.skipped = 0});
  final List<ZipFileInfo> files;
  final int skipped; // lent files that couldn't be read
}

class ZipToOpen extends ZipArrival {
  const ZipToOpen();
}

/// A piece of a cut file in a part: piece [number] of [pieces] of [file].
class ZipPieceInfo {
  const ZipPieceInfo({required this.file, required this.number, required this.pieces});

  factory ZipPieceInfo.fromMap(Map<Object?, Object?> m) =>
      ZipPieceInfo(file: m['file'] as String? ?? '', number: (m['number'] as num?)?.toInt() ?? 1, pieces: (m['pieces'] as num?)?.toInt() ?? 1);

  final String file;
  final int number, pieces;
}

/// One ZIP of a plan: its [name] once packed, its size, its whole files, and the pieces it holds.
class ZipPartInfo {
  const ZipPartInfo({required this.number, this.name, required this.bytes, required this.files, this.pieces = const []});

  factory ZipPartInfo.fromMap(Map<Object?, Object?> m) => ZipPartInfo(
        number: (m['number'] as num?)?.toInt() ?? 1,
        name: m['name'] as String?,
        bytes: (m['bytes'] as num?)?.toInt() ?? 0,
        files: (m['files'] as num?)?.toInt() ?? 0,
        pieces: [for (final p in (m['pieces'] as List? ?? const [])) if (p is Map) ZipPieceInfo.fromMap(p)],
      );

  final int number;
  final String? name;
  final int bytes, files;
  final List<ZipPieceInfo> pieces;
}

/// A file too big for one ZIP, and the parts its pieces go into.
class ZipCutInfo {
  const ZipCutInfo({required this.name, required this.size, required this.parts});

  factory ZipCutInfo.fromMap(Map<Object?, Object?> m) => ZipCutInfo(
        name: m['name'] as String? ?? '',
        size: (m['size'] as num?)?.toInt() ?? 0,
        parts: [for (final p in (m['parts'] as List? ?? const [])) if (p is num) p.toInt()],
      );

  final String name;
  final int size;
  final List<int> parts;
}

/// What a choice makes of the files waiting (zip.plan), or what packing made.
class ZipPlanInfo {
  const ZipPlanInfo({this.tooMany = false, this.parts = const [], this.cut = const []});

  factory ZipPlanInfo.fromMap(Map<Object?, Object?> m) => ZipPlanInfo(
        tooMany: m['too_many'] == true,
        parts: [for (final p in (m['parts'] as List? ?? const [])) if (p is Map) ZipPartInfo.fromMap(p)],
        cut: [for (final c in (m['cut'] as List? ?? const [])) if (c is Map) ZipCutInfo.fromMap(c)],
      );

  final bool tooMany;
  final List<ZipPartInfo> parts;
  final List<ZipCutInfo> cut;

  int get bytes => parts.fold(0, (s, p) => s + p.bytes);
}

enum ZipPackStage { packing, ready, stopped, failed, noRoom }

/// How packing goes (events of type zip): which file and part, and at the end the parts.
class ZipPackState {
  const ZipPackState({
    required this.stage,
    this.filesDone = 0,
    this.files = 0,
    this.bytesDone = 0,
    this.bytesTotal = 0,
    this.part = 1,
    this.parts = 1,
    this.fetching,
    this.plan,
    this.reason,
    this.failedFile,
    this.needed = 0,
    this.free = 0,
  });

  factory ZipPackState.fromMap(Map<Object?, Object?> m) {
    // parts is a count while packing, and the list of parts once ready.
    int n(String k) => m[k] is num ? (m[k] as num).toInt() : 0;
    final stage = switch (m['state']) {
      'ready' => ZipPackStage.ready,
      'stopped' => ZipPackStage.stopped,
      'no_room' => ZipPackStage.noRoom,
      'failed' => m['reason'] == 'no_room' ? ZipPackStage.noRoom : ZipPackStage.failed,
      _ => ZipPackStage.packing,
    };
    return ZipPackState(
      stage: stage,
      filesDone: n('files_done'),
      files: n('files'),
      bytesDone: n('bytes_done'),
      bytesTotal: n('bytes_total'),
      part: n('part'),
      parts: n('parts'),
      fetching: m['fetching'] as String?,
      plan: stage == ZipPackStage.ready ? ZipPlanInfo.fromMap(m) : null,
      reason: m['reason'] as String?,
      failedFile: m['file'] as String?,
      needed: n('needed'),
      free: n('free'),
    );
  }

  final ZipPackStage stage;
  final int filesDone, files, bytesDone, bytesTotal, part, parts;

  /// The library file being fetched from the server right now.
  final String? fetching;
  final ZipPlanInfo? plan;

  /// Why it failed: changed (a file changed while packing: [failedFile]) or failed.
  final String? reason;
  final String? failedFile;

  /// Without room: what packing needs, and what's free.
  final int needed, free;

  double? get progress => bytesTotal == 0 ? null : (bytesDone / bytesTotal).clamp(0.0, 1.0);
}

/// An app took part [part] of what was packed, or all of them (0).
class ZipSent {
  const ZipSent({required this.part, this.app});

  factory ZipSent.fromMap(Map<Object?, Object?> m) => ZipSent(part: (m['part'] as num?)?.toInt() ?? 0, app: m['app'] as String?);

  final int part;
  final String? app;
}

/// A ZIP's set as this phone knows it: how many parts, which are open now, which were saved, and
/// where its files went ([to] phone or folder), so the others follow.
class ZipSetInfo {
  const ZipSetInfo({required this.parts, this.here = const [], this.saved = const [], this.to, this.folder});

  factory ZipSetInfo.fromMap(Map<Object?, Object?> m) => ZipSetInfo(
        parts: (m['parts'] as num?)?.toInt() ?? 1,
        here: [for (final p in (m['here'] as List? ?? const [])) if (p is num) p.toInt()],
        saved: [for (final p in (m['saved'] as List? ?? const [])) if (p is num) p.toInt()],
        to: m['to'] as String?,
        folder: m['folder'] as String?,
      );

  final int parts;
  final List<int> here, saved;
  final String? to, folder;
}

/// A file of the ZIPs open: [piece] for a piece of a cut file.
class ZipEntryInfo {
  const ZipEntryInfo({required this.index, required this.name, required this.kind, required this.size, this.readable = true, this.saved = false, this.piece});

  factory ZipEntryInfo.fromMap(Map<Object?, Object?> m) => ZipEntryInfo(
        index: (m['index'] as num?)?.toInt() ?? 0,
        name: m['name'] as String? ?? '',
        kind: FileKind.parse(m['kind']),
        size: (m['size'] as num?)?.toInt() ?? 0,
        readable: m['readable'] != false,
        saved: m['saved'] == true,
        piece: m['piece'] is Map ? ZipPieceInfo.fromMap(m['piece'] as Map) : null,
      );

  final int index;
  final String name;
  final FileKind kind;
  final int size;
  final bool readable, saved;
  final ZipPieceInfo? piece;
}

/// A cut file: the pieces this phone has, from before or in the ZIPs open now, and the parts that hold them.
class ZipJoinInfo {
  const ZipJoinInfo({required this.file, required this.kind, required this.total, required this.pieces, this.have = const [], this.parts = const []});

  factory ZipJoinInfo.fromMap(Map<Object?, Object?> m) => ZipJoinInfo(
        file: m['file'] as String? ?? '',
        kind: FileKind.parse(m['kind']),
        total: (m['total'] as num?)?.toInt() ?? 0,
        pieces: (m['pieces'] as num?)?.toInt() ?? 1,
        have: [for (final p in (m['have'] as List? ?? const [])) if (p is num) p.toInt()],
        parts: [for (final p in (m['parts'] as List? ?? const [])) if (p is num) p.toInt()],
      );

  final String file;
  final FileKind kind;
  final int total, pieces;
  final List<int> have, parts;

  bool get whole => have.length >= pieces;

  /// The parts holding the pieces this phone still lacks.
  List<int> get missingParts => [for (var n = 1; n <= pieces; n++) if (!have.contains(n) && n - 1 < parts.length) parts[n - 1]];
}

/// What the ZIPs open hold (zip.contents).
class ZipContents {
  const ZipContents({this.name, this.zips = 1, this.broken = 0, this.newer = false, this.set, this.fromWhatsapp = false, this.bytes = 0, this.files = const [], this.joins = const []});

  factory ZipContents.fromMap(Map<Object?, Object?> m) => ZipContents(
        name: m['name'] as String?,
        zips: (m['zips'] as num?)?.toInt() ?? 0,
        broken: (m['broken'] as num?)?.toInt() ?? 0,
        newer: m['newer'] == true,
        set: m['set'] is Map ? ZipSetInfo.fromMap(m['set'] as Map) : null,
        fromWhatsapp: m['from_whatsapp'] == true,
        bytes: (m['bytes'] as num?)?.toInt() ?? 0,
        files: [for (final f in (m['files'] as List? ?? const [])) if (f is Map) ZipEntryInfo.fromMap(f)],
        joins: [for (final j in (m['joins'] as List? ?? const [])) if (j is Map) ZipJoinInfo.fromMap(j)],
      );

  /// The set's name, or the ZIP's; null when ZIPs of several sets are open.
  final String? name;
  final int zips;

  /// ZIPs that couldn't be opened.
  final int broken;

  /// Made by a newer Share: it opens as an ordinary ZIP.
  final bool newer;
  final ZipSetInfo? set;
  final bool fromWhatsapp;
  final int bytes;
  final List<ZipEntryInfo> files;
  final List<ZipJoinInfo> joins;

  ZipJoinInfo? joinOf(ZipEntryInfo f) => f.piece == null ? null : joins.where((j) => j.file == f.piece!.file).firstOrNull;

  /// Whole files to save now: whole ones that aren't saved yet, and cut ones whose pieces are all in.
  int get toSave => files.where((f) => f.readable && !f.saved && f.piece == null).length + joins.where((j) => j.whole && !files.any((f) => f.piece?.file == j.file && f.saved)).length;

  /// Cut files that still wait for a part.
  List<ZipJoinInfo> get waiting => [for (final j in joins) if (!j.whole) j];
}

enum ZipSaveStage { saving, done, failed, noRoom }

/// How saving goes (events of type zip_save), and at the end what came of it.
class ZipSaveState {
  const ZipSaveState({required this.stage, this.done = 0, this.total = 0, this.bytesDone = 0, this.bytesTotal = 0, this.saved = 0, this.joined = const [], this.broken = const [], this.to, this.needed = 0, this.free = 0});

  factory ZipSaveState.fromMap(Map<Object?, Object?> m) {
    int n(String k) => (m[k] as num?)?.toInt() ?? 0;
    return ZipSaveState(
      stage: switch (m['state']) {
        'done' => ZipSaveStage.done,
        'no_room' => ZipSaveStage.noRoom,
        'failed' => m['reason'] == 'no_room' ? ZipSaveStage.noRoom : ZipSaveStage.failed,
        _ => ZipSaveStage.saving,
      },
      done: n('done'),
      total: n('total'),
      bytesDone: n('bytes_done'),
      bytesTotal: n('bytes_total'),
      saved: n('saved'),
      joined: [
        for (final j in (m['joined'] as List? ?? const []))
          if (j is Map) (file: j['file'] as String? ?? '', kind: FileKind.parse(j['kind']), total: (j['total'] as num?)?.toInt() ?? 0, parts: [for (final p in (j['parts'] as List? ?? const [])) if (p is num) p.toInt()]),
      ],
      broken: [for (final b in (m['broken'] as List? ?? const [])) if (b is Map) (file: b['file'] as String? ?? '', part: (b['part'] as num?)?.toInt())],
      to: m['to'] as String?,
      needed: n('needed'),
      free: n('free'),
    );
  }

  final ZipSaveStage stage;
  final int done, total, bytesDone, bytesTotal, saved;

  /// Cut files put back together, and the parts their pieces came in.
  final List<({String file, FileKind kind, int total, List<int> parts})> joined;
  final List<({String file, int? part})> broken;
  final String? to;
  final int needed, free;

  double? get progress => bytesTotal == 0 ? null : (bytesDone / bytesTotal).clamp(0.0, 1.0);
}

/// A picture of a file in a ZIP, and a video's length.
class ZipThumb {
  const ZipThumb(this.jpeg, {this.durationMs});

  factory ZipThumb.fromMap(Map<Object?, Object?> m) => ZipThumb(m['jpeg'] as Uint8List, durationMs: (m['duration_ms'] as num?)?.toInt());

  final Uint8List jpeg;
  final int? durationMs;
}

/// The words a ZIP's name starts with, in the phone's language.
typedef ZipNameWords = ({String photos, String videos, String documents, String files});

/// The name Share proposes for a ZIP (screen 98): the [folder] the files come from, or a word for
/// what they are, and the days they were taken, written as [locale] writes dates: 4 Oct 2026,
/// 15–18 Oct 2026, 28 Sep – 3 Oct 2026, 28 Dec 2025 – 3 Jan 2026, or over more than two months only
/// the months, Jul – Sep 2026.
String zipName({required Iterable<FileKind> kinds, required Iterable<DateTime> days, required String locale, required ZipNameWords words, String? folder}) {
  final k = kinds.toSet();
  final word = folder ??
      (k.isEmpty || k.every((x) => x == FileKind.photo || x == FileKind.video)
          ? (k.length == 1 && k.single == FileKind.video ? words.videos : words.photos)
          : k.every((x) => x == FileKind.document)
              ? words.documents
              : words.files);
  final list = days.map((d) => DateTime(d.year, d.month, d.day)).toList()..sort();
  if (list.isEmpty) return word;
  return '$word ${zipDays(list.first, list.last, locale)}';
}

/// From [a] to [b], as a ZIP's name says it; the same day once.
String zipDays(DateTime a, DateTime b, String locale) {
  final de = locale == 'de';
  String f(String pattern, DateTime d) => DateFormat(pattern, locale).format(d);
  final full = de ? 'd. MMM y' : 'd MMM y';
  if (a.year == b.year && a.month == b.month && a.day == b.day) return f(full, a);
  if (b.difference(a).inDays > 62) return a.year == b.year ? '${f('MMM', a)} – ${f('MMM y', b)}' : '${f('MMM y', a)} – ${f('MMM y', b)}';
  if (a.year == b.year && a.month == b.month) return '${f(de ? 'd.' : 'd', a)}–${f(full, b)}';
  if (a.year == b.year) return '${f(de ? 'd. MMM' : 'd MMM', a)} – ${f(full, b)}';
  return '${f(full, a)} – ${f(full, b)}';
}

/// A name as typed, made fit for a file name: no slashes and nothing else file systems refuse, no
/// dots or spaces at the ends, at most 80 characters; empty when nothing is left.
String cleanZipName(String typed) {
  final s = typed.replaceAll(RegExp(r'[/\\:*?"<>|\x00-\x1f\x7f]'), '_').trim().replaceAll(RegExp(r'^[. ]+|[. ]+$'), '');
  return s.length <= 80 ? s : s.substring(0, 80).trimRight();
}
