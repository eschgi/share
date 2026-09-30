import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 18: the PINs that work now, and a new one (19).
class PinsScreen extends StatefulWidget {
  const PinsScreen({super.key});

  @override
  State<PinsScreen> createState() => _PinsScreenState();
}

class _PinsScreenState extends State<PinsScreen> {
  List<PinInfo>? _pins;
  bool _failed = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final pins = await Services.read(context).admin.pins();
      if (mounted) {
        setState(() {
          _pins = pins;
          _failed = false;
        });
      }
    } on Exception {
      if (mounted) setState(() => _failed = true);
    }
  }

  void _say(String text) => ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(text)));

  Future<void> _share(PinInfo p) => Services.read(context).platform.shareText(AppLocalizations.of(context).pinShareText(p.link));

  Future<void> _new() async {
    final created = await showModalBottomSheet<PinInfo>(context: context, isScrollControlled: true, builder: (_) => const NewPinSheet());
    if (created == null || !mounted) return;
    await _load();
    await _share(created);
  }

  Future<bool> _confirm(String title, String body, String action) async {
    final t = AppLocalizations.of(context);
    return await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            title: Text(title),
            content: Text(body, style: TextStyle(color: context.colors.text2, height: 1.5)),
            actions: [
              TextButton(onPressed: () => Navigator.pop(context, false), child: Text(t.commonCancel)),
              TextButton(
                style: TextButton.styleFrom(foregroundColor: context.colors.danger),
                onPressed: () => Navigator.pop(context, true),
                child: Text(action),
              ),
            ],
          ),
        ) ??
        false;
  }

  Future<void> _newCode(PinInfo p) async {
    final t = AppLocalizations.of(context);
    if (!await _confirm(t.pinNewCodeTitle(p.code), t.pinNewCodeBody, t.pinNewCode) || !mounted) return;
    try {
      await Services.read(context).admin.newCode(p.id);
    } on Exception {
      _say(t.commonFailed);
    }
    await _load();
  }

  Future<void> _end(PinInfo p) async {
    final t = AppLocalizations.of(context);
    if (!await _confirm(t.pinEndTitle(p.code), t.pinEndBody, t.pinEndNow) || !mounted) return;
    try {
      await Services.read(context).admin.endPin(p.id);
    } on Exception {
      _say(t.commonFailed);
    }
    await _load();
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final pins = _pins;
    final permanent = [...?pins?.where((p) => p.kind == PinKind.permanent)];
    final day = [...?pins?.where((p) => p.kind == PinKind.day)];
    return Scaffold(
      appBar: AppBar(
        leading: const ShareBackButton(),
        title: Text(t.settingsPins, style: const TextStyle(fontFamily: 'Roboto', fontSize: 20, fontWeight: FontWeight.w600)),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _new,
        icon: const Icon(AppIcons.plus, size: 26),
        label: Text(t.pinNew, style: const TextStyle(fontFamily: 'Roboto', fontSize: 17, fontWeight: FontWeight.w600)),
      ),
      body: RefreshIndicator(
        onRefresh: _load,
        child: ListView(padding: const EdgeInsets.fromLTRB(16, 4, 16, 110), children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 4),
            child: Markup(t.pinsLead, style: TextStyle(fontSize: 16.5, height: 1.55, color: c.text2)),
          ),
          if (pins == null && !_failed) const Padding(padding: EdgeInsets.only(top: 80), child: Center(child: CircularProgressIndicator())),
          if (_failed) Padding(padding: const EdgeInsets.only(top: 24), child: Help(t.commonOffline)),
          if (pins != null && pins.isEmpty) Padding(padding: const EdgeInsets.only(top: 24), child: Help(t.pinsNone)),
          if (permanent.isNotEmpty) SectionLabel(t.pinsPermanent),
          for (final p in permanent)
            _PinCard(pin: p, onShare: () => _share(p), onNewCode: () => _newCode(p), onQr: () => showPinQr(context, p), onEnd: () => _end(p)),
          if (day.isNotEmpty) SectionLabel(t.pinsDay),
          for (final p in day)
            _PinCard(pin: p, onShare: () => _share(p), onNewCode: () => _newCode(p), onQr: () => showPinQr(context, p), onEnd: () => _end(p)),
        ]),
      ),
    );
  }
}

class _PinCard extends StatelessWidget {
  const _PinCard({required this.pin, required this.onShare, required this.onNewCode, required this.onQr, required this.onEnd});
  final PinInfo pin;
  final VoidCallback onShare, onNewCode, onQr, onEnd;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = DateTime.now();
    final permanent = pin.kind == PinKind.permanent;
    final action = TextButton.styleFrom(
      foregroundColor: c.accentText,
      minimumSize: const Size(48, 44),
      padding: const EdgeInsets.symmetric(horizontal: 10),
      textStyle: const TextStyle(fontFamily: 'Roboto', fontSize: 16, fontWeight: FontWeight.w600),
    );
    return Container(
      margin: const EdgeInsets.only(bottom: 12),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(22), border: Border.all(color: c.lineSoft)),
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 10),
          child: Row(children: [
            Expanded(
              child: Align(
                alignment: Alignment.centerLeft,
                child: FittedBox(fit: BoxFit.scaleDown, child: CodeBoxes(pin.code)),
              ),
            ),
            if (permanent) ...[const SizedBox(width: 12), Icon(AppIcons.infinity, size: 26, color: c.text3)],
          ]),
        ),
        Padding(
          padding: const EdgeInsets.fromLTRB(16, 0, 16, 12),
          child: permanent
              ? Text('${t.pinSince(formatShortDate(pin.createdAt, now, locale))} · ${t.pinUsedOn(pin.phones)}',
                  style: TextStyle(fontSize: 14, color: c.text3))
              : Wrap(spacing: 10, runSpacing: 8, crossAxisAlignment: WrapCrossAlignment.center, children: [
                  if (pin.expiresAt != null)
                    Container(
                      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 5),
                      decoration: BoxDecoration(color: c.warnSoft, borderRadius: BorderRadius.circular(20)),
                      child: Row(mainAxisSize: MainAxisSize.min, children: [
                        Icon(AppIcons.clock, size: 15, color: c.warn),
                        const SizedBox(width: 6),
                        Text(t.pinEnds(formatWhen(pin.expiresAt!, now, locale)),
                            style: TextStyle(fontSize: 13.5, fontWeight: FontWeight.w600, color: c.warn)),
                      ]),
                    ),
                  Text(pin.files == 0 ? t.pinFiles(0) : '${t.pinFiles(pin.files)} ${t.pinFromPhones(pin.phones)}',
                      style: TextStyle(fontSize: 14, color: c.text3)),
                ]),
        ),
        Divider(indent: 12, endIndent: 12, color: c.lineSoft),
        Padding(
          padding: const EdgeInsets.fromLTRB(6, 2, 6, 6),
          child: Row(children: [
            TextButton.icon(style: action, onPressed: onShare, icon: const Icon(AppIcons.share, size: 19), label: Text(t.pinShare)),
            if (permanent)
              TextButton.icon(style: action, onPressed: onNewCode, icon: const Icon(AppIcons.refresh, size: 19), label: Text(t.pinNewCode))
            else
              TextButton.icon(style: action, onPressed: onQr, icon: const Icon(AppIcons.qrCode, size: 19), label: Text(t.pinQr)),
            const Spacer(),
            if (permanent)
              PopupMenuButton<VoidCallback>(
                icon: Icon(AppIcons.more, size: 20, color: c.text2),
                onSelected: (f) => f(),
                itemBuilder: (_) => [
                  PopupMenuItem(value: onQr, child: Text(t.pinQr)),
                  PopupMenuItem(value: onEnd, child: Text(t.pinEndNow, style: TextStyle(color: c.danger))),
                ],
              )
            else
              TextButton(style: action.copyWith(foregroundColor: WidgetStatePropertyAll(c.danger)), onPressed: onEnd, child: Text(t.pinEndNow)),
          ]),
        ),
      ]),
    );
  }
}

/// A PIN as a QR code, for someone standing next to you.
Future<void> showPinQr(BuildContext context, PinInfo pin) {
  final t = AppLocalizations.of(context);
  return showDialog<void>(
    context: context,
    builder: (context) => Dialog(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(24, 26, 24, 16),
        child: Column(mainAxisSize: MainAxisSize.min, children: [
          QrCodeView(pin.link, size: 200, label: pin.link),
          const SizedBox(height: 18),
          CodeBoxes(pin.code, size: 36),
          const SizedBox(height: 14),
          Text(t.pinScanToSend, textAlign: TextAlign.center, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
          const SizedBox(height: 4),
          Text(pin.link, textAlign: TextAlign.center, style: TextStyle(fontSize: 13, color: context.colors.text3)),
          const SizedBox(height: 8),
          Align(
            alignment: Alignment.centerRight,
            child: TextButton(onPressed: () => Navigator.pop(context), child: Text(t.commonClose)),
          ),
        ]),
      ),
    ),
  );
}

/// Screen 19: how long the PIN works, and its code (made up, or typed).
class NewPinSheet extends StatefulWidget {
  const NewPinSheet({super.key});

  @override
  State<NewPinSheet> createState() => _NewPinSheetState();
}

class _NewPinSheetState extends State<NewPinSheet> {
  PinKind _kind = PinKind.day;
  final _code = TextEditingController();
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _suggest();
  }

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  Future<void> _suggest() async {
    try {
      final code = await Services.read(context).admin.suggestPin();
      if (mounted) {
        setState(() {
          _code.text = code;
          _error = null;
        });
      }
    } on Exception {
      // Typing one works too.
    }
  }

  Future<void> _create() async {
    final t = AppLocalizations.of(context);
    if (_code.text.length != 5) return setState(() => _error = t.pinBadCode);
    setState(() {
      _busy = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    try {
      final pin = await Services.read(context).admin.createPin(_kind, code: _code.text);
      navigator.pop(pin);
    } on ApiException catch (e) {
      setState(() => _error = switch (e.code) { 'pin_taken' => t.pinTaken, 'pin_format' => t.pinBadCode, _ => t.commonFailed });
    } on NetworkException {
      setState(() => _error = t.commonOffline);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final now = DateTime.now();
    return Padding(
      padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(context).bottom),
      child: SafeArea(
        child: SingleChildScrollView(
          padding: const EdgeInsets.fromLTRB(22, 8, 22, 16),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
            Text(t.pinNewTitle, style: Theme.of(context).textTheme.headlineMedium),
            FieldLabel(t.pinHowLong),
            Row(children: [
              Expanded(
                child: _KindCard(
                  icon: AppIcons.infinity,
                  title: t.pinsPermanent,
                  detail: t.pinPermanentDetail,
                  selected: _kind == PinKind.permanent,
                  onTap: () => setState(() => _kind = PinKind.permanent),
                ),
              ),
              const SizedBox(width: 12),
              Expanded(
                child: _KindCard(
                  icon: AppIcons.clock,
                  title: t.pin24h,
                  detail: t.pinUntil(formatWhen(now.add(const Duration(days: 1)), now, locale)),
                  selected: _kind == PinKind.day,
                  onTap: () => setState(() => _kind = PinKind.day),
                ),
              ),
            ]),
            FieldLabel(t.pinCodeLabel),
            Row(children: [
              Expanded(child: PinCodeField(controller: _code, onChanged: (_) => setState(() => _error = null))),
              const SizedBox(width: 10),
              IconButton.filled(
                tooltip: t.pinAnother,
                style: IconButton.styleFrom(
                  backgroundColor: c.accentSoft,
                  foregroundColor: c.accentText,
                  fixedSize: const Size(52, 58),
                  shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(14)),
                ),
                onPressed: _suggest,
                icon: const Icon(AppIcons.refresh, size: 24),
              ),
            ]),
            Help(_error ?? t.pinMadeUp, error: _error != null),
            const SizedBox(height: 22),
            BusyButton(label: t.pinCreateShare, icon: AppIcons.share, busy: _busy, onPressed: _create),
          ]),
        ),
      ),
    );
  }
}

class _KindCard extends StatelessWidget {
  const _KindCard({required this.icon, required this.title, required this.detail, required this.selected, required this.onTap});
  final IconData icon;
  final String title, detail;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Semantics(
      selected: selected,
      button: true,
      child: InkWell(
        onTap: onTap,
        borderRadius: BorderRadius.circular(18),
        child: Container(
          padding: const EdgeInsets.fromLTRB(14, 14, 12, 14),
          decoration: BoxDecoration(
            color: selected ? c.accentSoft : c.s2,
            borderRadius: BorderRadius.circular(18),
            border: Border.all(color: selected ? c.accentText : c.line, width: selected ? 1.5 : 1),
          ),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Row(children: [
              Icon(icon, size: 24, color: selected ? c.accentText : c.text2),
              const Spacer(),
              Container(
                width: 22,
                height: 22,
                decoration: BoxDecoration(shape: BoxShape.circle, border: Border.all(color: selected ? c.accentText : c.text3, width: 2)),
                alignment: Alignment.center,
                child: selected
                    ? Container(width: 10, height: 10, decoration: BoxDecoration(shape: BoxShape.circle, color: c.accentText))
                    : null,
              ),
            ]),
            const SizedBox(height: 14),
            Text(title, style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
            const SizedBox(height: 2),
            Text(detail, style: TextStyle(fontSize: 13, color: c.text3)),
          ]),
        ),
      ),
    );
  }
}

/// Five boxes to type a PIN into: letters and digits without look-alikes, in capitals.
class PinCodeField extends StatefulWidget {
  const PinCodeField({super.key, required this.controller, this.onChanged, this.autofocus = false});
  final TextEditingController controller;
  final ValueChanged<String>? onChanged;
  final bool autofocus;

  @override
  State<PinCodeField> createState() => _PinCodeFieldState();
}

class _PinCodeFieldState extends State<PinCodeField> {
  final _focus = FocusNode();

  @override
  void dispose() {
    _focus.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => GestureDetector(
        onTap: () => _focus.requestFocus(),
        child: Stack(alignment: Alignment.centerLeft, children: [
          // The real field, out of sight: the boxes show what it holds.
          Opacity(
            opacity: 0,
            child: TextField(
              controller: widget.controller,
              focusNode: _focus,
              autofocus: widget.autofocus,
              autocorrect: false,
              enableSuggestions: false,
              textCapitalization: TextCapitalization.characters,
              maxLength: 5,
              inputFormatters: [
                FilteringTextInputFormatter.allow(RegExp('[2-9A-HJ-NP-Za-hj-np-z]')),
                TextInputFormatter.withFunction((_, v) => v.copyWith(text: v.text.toUpperCase())),
              ],
              onChanged: widget.onChanged,
              decoration: const InputDecoration(counterText: ''),
            ),
          ),
          IgnorePointer(
            child: ListenableBuilder(
              listenable: Listenable.merge([widget.controller, _focus]),
              builder: (context, _) => FittedBox(
                fit: BoxFit.scaleDown,
                alignment: Alignment.centerLeft,
                child: CodeBoxes(
                  widget.controller.text,
                  size: 52,
                  active: _focus.hasFocus ? widget.controller.text.length.clamp(0, 4) : null,
                ),
              ),
            ),
          ),
        ]),
      );
}
