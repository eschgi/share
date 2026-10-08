/// A permanent PIN printed for guests (screens 69, 71 and 72): a poster for a wall, or four cards
/// for the tables, so nobody has to send them a link.
library;

import 'dart:ui' as ui;

import 'package:clock/clock.dart';
import 'package:flutter/material.dart';
import 'package:flutter/rendering.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';
import 'pins_screen.dart';

enum PrintKind { poster, cards }

/// What goes on the page: the sheet's choices, and the PIN's link and code.
class Printed {
  const Printed({
    required this.kind,
    required this.title,
    required this.line,
    required this.link,
    required this.brand,
    required this.texts,
    required this.locale,
    this.day,
    this.code,
    this.showsFolder = false,
  });

  final PrintKind kind;
  final String title;
  final String line;
  final String link;

  /// The server's name, at the top of a poster.
  final String brand;

  /// The page's words, in the server's language [locale], the one guests see first.
  final AppLocalizations texts;
  final String locale;

  /// The day the poster names, or none.
  final DateTime? day;

  /// The code to type, or none.
  final String? code;

  /// Guests can come back for everyone's photos.
  final bool showsFolder;
}

/// Screen 69: opens the sheet for [pin], with its whole link, in the server's language; without
/// an answer from the server, in the app's.
Future<void> showPrintSheet(BuildContext context, PinInfo pin) async {
  final api = Services.read(context).api;
  final own = Localizations.localeOf(context).languageCode;
  final link = await pinLink(context, pin);
  Json? info;
  try {
    info = await api.get('/api/info');
  } on Exception {
    info = null;
  }
  if (!context.mounted) return;
  final server = info?['default_language'];
  final locale = AppLocalizations.supportedLocales.any((l) => l.languageCode == server) ? server as String : own;
  await showModalBottomSheet<void>(
    context: context,
    isScrollControlled: true,
    builder: (_) => PrintSheet(
      pin: pin,
      link: link,
      brand: info?['name'] as String? ?? 'Share',
      texts: lookupAppLocalizations(Locale(locale)),
      locale: locale,
    ),
  );
}

class PrintSheet extends StatefulWidget {
  const PrintSheet({super.key, required this.pin, required this.link, required this.brand, required this.texts, required this.locale});
  final PinInfo pin;
  final String link;
  final String brand;

  /// The page's words and language: the server's.
  final AppLocalizations texts;
  final String locale;

  @override
  State<PrintSheet> createState() => _PrintSheetState();
}

class _PrintSheetState extends State<PrintSheet> {
  PrintKind _kind = PrintKind.poster;
  late final _title = TextEditingController(text: Services.read(context).folders.byId(widget.pin.folder)?.name ?? widget.brand);
  late final _line = TextEditingController(text: widget.texts.printLine);
  bool _dated = true;
  DateTime _day = clock.now();
  bool _withCode = true;
  bool _busy = false;

  /// The page in the preview, which printing draws again at 300 dpi.
  final _page = GlobalKey();

  @override
  void dispose() {
    _title.dispose();
    _line.dispose();
    super.dispose();
  }

  Future<void> _pickDay() async {
    final day = await showDatePicker(context: context, initialDate: _day, firstDate: DateTime(2020), lastDate: DateTime(2100));
    if (day != null) setState(() => _day = day);
  }

  Future<void> _print() async {
    final t = AppLocalizations.of(context);
    final platform = Services.read(context).platform;
    final messenger = ScaffoldMessenger.of(context);
    final title = _title.text.trim();
    setState(() => _busy = true);
    try {
      final boundary = _page.currentContext!.findRenderObject()! as RenderRepaintBoundary;
      final image = await boundary.toImage(pixelRatio: PrintPage.dots / PrintPage.size.width);
      final png = await image.toByteData(format: ui.ImageByteFormat.png);
      image.dispose();
      await platform.printPage(png!.buffer.asUint8List(), title.isEmpty ? widget.pin.code : title);
    } on PlatformException {
      messenger.showSnackBar(SnackBar(content: Text(t.commonFailed)));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  String _languageName(AppLocalizations t, String code) => switch (code) {
        'de' => t.languageGerman,
        'it' => t.languageItalian,
        _ => t.languageEnglish,
      };

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final own = Localizations.localeOf(context).languageCode;
    final poster = _kind == PrintKind.poster;
    final printed = Printed(
      kind: _kind,
      title: _title.text.trim(),
      line: _line.text.trim(),
      link: widget.link,
      brand: widget.brand,
      texts: widget.texts,
      locale: widget.locale,
      day: poster && _dated ? _day : null,
      code: _withCode ? widget.pin.code : null,
      showsFolder: widget.pin.showsFolder,
    );
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(22, 8, 22, 16),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
            Text(t.printTitle(widget.pin.code), style: Theme.of(context).textTheme.headlineSmall),
            const SizedBox(height: 18),
            IntrinsicHeight(
              child: Row(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                Expanded(
                  child: KindCard(
                    icon: AppIcons.file,
                    title: t.printPoster,
                    detail: t.printPosterDetail,
                    selected: poster,
                    onTap: () => setState(() => _kind = PrintKind.poster),
                  ),
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: KindCard(
                    icon: AppIcons.copy,
                    title: t.printCards,
                    detail: t.printCardsDetail,
                    selected: !poster,
                    onTap: () => setState(() => _kind = PrintKind.cards),
                  ),
                ),
              ]),
            ),
            FieldLabel(t.printHeading),
            TextField(
              controller: _title,
              maxLength: 60,
              textCapitalization: TextCapitalization.sentences,
              decoration: InputDecoration(fillColor: c.s2, counterText: ''),
              onChanged: (_) => setState(() {}),
            ),
            FieldLabel(t.printUnder),
            TextField(
              controller: _line,
              maxLength: 100,
              textCapitalization: TextCapitalization.sentences,
              decoration: InputDecoration(fillColor: c.s2, counterText: ''),
              onChanged: (_) => setState(() {}),
            ),
            if (poster) ...[
              const SizedBox(height: 18),
              _SwitchRow(
                title: t.printDate,
                detail: _dated ? formatLongDay(_day, widget.locale) : t.printNoDate,
                value: _dated,
                onChanged: (on) => setState(() => _dated = on),
              ),
              if (_dated)
                Align(
                  alignment: Alignment.centerLeft,
                  child: TextButton.icon(
                    style: TextButton.styleFrom(foregroundColor: c.accentText, padding: const EdgeInsets.symmetric(horizontal: 4)),
                    onPressed: _pickDay,
                    icon: const Icon(AppIcons.calendar, size: 19),
                    label: Text(MaterialLocalizations.of(context).datePickerHelpText),
                  ),
                ),
            ],
            const SizedBox(height: 14),
            _SwitchRow(title: t.printCode, detail: t.printCodeDetail, value: _withCode, onChanged: (on) => setState(() => _withCode = on)),
            if (widget.locale != own) Help(t.printInLanguage(_languageName(t, widget.locale))),
            const SizedBox(height: 22),
            Center(
              child: ExcludeSemantics(
                child: Container(
                  width: 260,
                  clipBehavior: Clip.antiAlias,
                  decoration: BoxDecoration(
                    borderRadius: BorderRadius.circular(6),
                    boxShadow: const [BoxShadow(color: Color(0x55000000), blurRadius: 24, offset: Offset(0, 10))],
                  ),
                  child: FittedBox(child: RepaintBoundary(key: _page, child: PrintPage(printed))),
                ),
              ),
            ),
            const SizedBox(height: 24),
            BusyButton(label: t.printGo, icon: AppIcons.printer, busy: _busy, onPressed: _print),
          ]),
        ),
      ),
    );
  }
}

/// A choice to switch on or off, with a line under it.
class _SwitchRow extends StatelessWidget {
  const _SwitchRow({required this.title, required this.detail, required this.value, required this.onChanged});
  final String title, detail;
  final bool value;
  final ValueChanged<bool> onChanged;

  @override
  Widget build(BuildContext context) => MergeSemantics(
        child: Row(children: [
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(title, style: const TextStyle(fontSize: 15.5, fontWeight: FontWeight.w500)),
              const SizedBox(height: 2),
              Text(detail, style: TextStyle(fontSize: 13.5, height: 1.45, color: context.colors.text3)),
            ]),
          ),
          const SizedBox(width: 12),
          Switch(value: value, onChanged: onChanged),
        ]),
      );
}

/// Screens 71 and 72: the A4 page, a poster or four cards to cut out, in the server's language.
/// Drawn at [size] in paper colours, whatever the theme, and printed at 300 dpi.
class PrintPage extends StatelessWidget {
  const PrintPage(this.p, {super.key});
  final Printed p;

  /// A4's shape, at the mockup's width.
  static const size = Size(780, 1103);

  /// The page's width in dots at 300 dpi: A4 is 210 mm wide.
  static const dots = 2480.0;

  static const _ink = Color(0xFF2A1F18);
  static const _brand = Color(0xFFA4461F);
  static const _soft = Color(0xFF5F5147);
  static const _lineInk = Color(0xFF3A2C24);
  static const _stepInk = Color(0xFF4A3A30);
  static const _faint = Color(0xFF85766A);
  static const _step = Color(0xFFC45A30);
  static const _frame = Color(0xFFE6DACB);
  static const _box = Color(0xFFD9CBBB);
  static const _cut = Color(0xFFCDBDAB);

  @override
  Widget build(BuildContext context) => MediaQuery.withNoTextScaling(
        child: DefaultTextStyle(
          style: const TextStyle(fontFamily: 'Roboto', color: _ink, fontSize: 19, height: 1.2),
          textAlign: TextAlign.center,
          child: SizedBox.fromSize(
            size: size,
            child: ColoredBox(color: Colors.white, child: p.kind == PrintKind.poster ? _poster() : _cards()),
          ),
        ),
      );

  Widget _poster() {
    final t = p.texts;
    return Padding(
      padding: const EdgeInsets.fromLTRB(76, 70, 76, 56),
      child: Column(children: [
        Row(mainAxisSize: MainAxisSize.min, children: [
          const Icon(AppIcons.images, size: 28, color: _brand),
          const SizedBox(width: 10),
          Text(p.brand, style: const TextStyle(fontFamily: serif, fontSize: 26, fontWeight: FontWeight.w700, color: _brand)),
        ]),
        const SizedBox(height: 30),
        _title(78, 54),
        if (p.day != null) ...[
          const SizedBox(height: 12),
          Text(formatLongDay(p.day!, p.locale), style: const TextStyle(fontSize: 24, color: _soft)),
        ],
        if (p.line.isNotEmpty) ...[
          const SizedBox(height: 30),
          Text(p.line, maxLines: 2, style: const TextStyle(fontSize: 33, height: 1.3, color: _lineInk)),
        ],
        const SizedBox(height: 34),
        _qr(300),
        const SizedBox(height: 16),
        Text(t.printScan, style: const TextStyle(fontSize: 23, fontWeight: FontWeight.w600)),
        const SizedBox(height: 32),
        Row(mainAxisAlignment: MainAxisAlignment.center, crossAxisAlignment: CrossAxisAlignment.start, children: [
          for (final (i, step) in [t.printStep1, t.printStep2, t.printStep3].indexed) ...[
            if (i > 0) const SizedBox(width: 24),
            SizedBox(
              width: 184,
              child: Column(children: [
                Container(
                  width: 44,
                  height: 44,
                  alignment: Alignment.center,
                  decoration: const BoxDecoration(color: _step, shape: BoxShape.circle),
                  child: Text('${i + 1}', style: const TextStyle(fontSize: 21, fontWeight: FontWeight.w700, color: Colors.white)),
                ),
                const SizedBox(height: 10),
                Text(step, style: const TextStyle(fontSize: 19, height: 1.35, color: _stepInk)),
              ]),
            ),
          ],
        ]),
        const SizedBox(height: 26),
        // A PIN that shows its folder lets guests come back for everyone's photos.
        Text(p.showsFolder ? '${t.printNoApp} ${t.printSeeLater}' : t.printNoApp, style: const TextStyle(fontSize: 19, height: 1.45, color: _soft)),
        const Spacer(),
        if (p.code != null) ...[
          Text(t.printOrType(Uri.parse(p.link).authority), style: const TextStyle(fontSize: 18, color: _faint)),
          const SizedBox(height: 12),
          _code(p.code!, 50, 60, 30),
        ],
      ]),
    );
  }

  Widget _cards() => Stack(children: [
        Column(children: [
          for (var row = 0; row < 2; row++)
            Expanded(child: Row(children: [for (var col = 0; col < 2; col++) Expanded(child: _card())])),
        ]),
        const Positioned.fill(child: CustomPaint(painter: _CutLines(_cut))),
      ]);

  Widget _card() => Padding(
        padding: const EdgeInsets.symmetric(horizontal: 28, vertical: 30),
        child: Column(mainAxisAlignment: MainAxisAlignment.center, children: [
          _title(40, 30),
          if (p.line.isNotEmpty) ...[
            const SizedBox(height: 10),
            Text(p.line, maxLines: 2, style: const TextStyle(fontSize: 19, height: 1.3, color: _lineInk)),
          ],
          const SizedBox(height: 20),
          _qr(196),
          const SizedBox(height: 12),
          Text(p.texts.printScan, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
          if (p.code != null) ...[const SizedBox(height: 12), _code(p.code!, 32, 40, 20)],
        ]),
      );

  /// A long title gets smaller letters, so it still fits in two or three lines.
  Widget _title(double size, double long) => Text(
        p.title,
        maxLines: 3,
        style: TextStyle(fontFamily: serif, fontSize: p.title.length > 22 ? long : size, fontWeight: FontWeight.w700, height: 1.05),
      );

  Widget _qr(double size) => Container(
        width: size,
        height: size,
        padding: EdgeInsets.all(size * 0.02),
        decoration: BoxDecoration(color: Colors.white, border: Border.all(color: _frame, width: 2), borderRadius: BorderRadius.circular(size * 0.093)),
        child: FittedBox(child: QrCodeView(p.link, size: 200)),
      );

  Widget _code(String code, double width, double height, double font) => Row(mainAxisSize: MainAxisSize.min, children: [
        for (final (i, char) in code.split('').indexed) ...[
          if (i > 0) SizedBox(width: width * 0.16),
          Container(
            width: width,
            height: height,
            alignment: Alignment.center,
            decoration: BoxDecoration(border: Border.all(color: _box, width: 2), borderRadius: BorderRadius.circular(width * 0.24)),
            child: Text(char, style: TextStyle(fontFamily: mono, fontSize: font, fontWeight: FontWeight.w600)),
          ),
        ],
      ]);
}

/// The dashed lines four cards are cut along.
class _CutLines extends CustomPainter {
  const _CutLines(this.color);
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..strokeWidth = 2;
    const dash = 10.0, gap = 7.0;
    for (var y = 0.0; y < size.height; y += dash + gap) {
      canvas.drawLine(Offset(size.width / 2, y), Offset(size.width / 2, (y + dash).clamp(0, size.height)), paint);
    }
    for (var x = 0.0; x < size.width; x += dash + gap) {
      canvas.drawLine(Offset(x, size.height / 2), Offset((x + dash).clamp(0, size.width), size.height / 2), paint);
    }
  }

  @override
  bool shouldRepaint(_CutLines old) => old.color != color;
}
