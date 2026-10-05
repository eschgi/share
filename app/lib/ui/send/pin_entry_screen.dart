import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../l10n/app_localizations.dart';
import '../admin/pins_screen.dart' show PinCodeField;
import '../widgets.dart';

/// Screen 1 of the website, in the app: the PIN that lets this phone send to [server]. With
/// [code] (from a scanned PIN link) it unlocks right away; the link's [secret] opens a folder
/// that is encrypted, for a PIN that shows it.
class PinEntryScreen extends StatefulWidget {
  const PinEntryScreen({super.key, required this.server, this.code, this.secret});
  final Uri server;
  final String? code;
  final String? secret;

  @override
  State<PinEntryScreen> createState() => _PinEntryScreenState();
}

class _PinEntryScreenState extends State<PinEntryScreen> {
  late final _code = TextEditingController(text: widget.code ?? '');
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    if (widget.code != null) WidgetsBinding.instance.addPostFrameCallback((_) => _unlock());
  }

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  Future<void> _unlock() async {
    final t = AppLocalizations.of(context);
    if (_code.text.length != 5) return setState(() => _error = t.pinBadCode);
    setState(() {
      _busy = true;
      _error = null;
    });
    final navigator = Navigator.of(context);
    try {
      await Services.read(context).pin.unlock(widget.server, _code.text, secret: _code.text == widget.code ? widget.secret : null);
      navigator.popUntil((r) => r.isFirst);
    } on ApiException catch (e) {
      setState(() => _error = switch (e.code) {
            'pin_wrong' => t.pinWrong(e.attemptsLeft ?? 0),
            'pin_ended' => t.pinEndedCode,
            'pin_locked' => t.pinLocked(t.waitTime(((e.retryAfter?.inSeconds ?? 60) / 60).ceil())),
            'pin_format' => t.pinBadCode,
            _ => t.commonFailed,
          });
    } on NetworkException {
      setState(() => _error = t.commonOffline);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton()),
      body: SafeArea(
        child: ListView(padding: const EdgeInsets.fromLTRB(24, 4, 24, 24), children: [
          HeroTitle(t.pinEnterTitle),
          Lead(t.pinEnterLead),
          const SizedBox(height: 26),
          PinCodeField(controller: _code, autofocus: widget.code == null, onChanged: (_) => setState(() => _error = null)),
          Help(_error ?? t.pinEnterHelp, error: _error != null),
          const SizedBox(height: 26),
          BusyButton(label: t.pinUnlock, busy: _busy, onPressed: _unlock),
        ]),
      ),
    );
  }
}
