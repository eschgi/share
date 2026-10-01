import 'package:flutter/material.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/server.dart';
import '../data/session.dart';
import '../l10n/app_localizations.dart';
import 'first_start.dart';
import 'icons.dart';
import 'widgets.dart';

/// Screen 8: username, password and the server. Most people come in through an invite.
class SignInScreen extends StatefulWidget {
  const SignInScreen({super.key});

  @override
  State<SignInScreen> createState() => _SignInScreenState();
}

class _SignInScreenState extends State<SignInScreen> {
  final _server = TextEditingController();
  final _username = TextEditingController();
  final _password = TextEditingController();
  bool _hidden = true;
  bool _busy = false;
  String? _serverError;
  String? _error;

  @override
  void initState() {
    super.initState();
    // After a sign-out the address is still known.
    Services.read(context).platform.loadServer().then((c) {
      if (c != null && mounted && _server.text.isEmpty) {
        // Just the name for an ordinary https address; with http or a port, all of it.
        _server.text = c.publicUrl.scheme == 'https' && !c.publicUrl.hasPort ? c.publicUrl.host : c.publicUrl.toString();
      }
    });
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton()),
      body: SafeArea(
        child: ListView(padding: const EdgeInsets.fromLTRB(24, 4, 24, 24), children: [
          HeroTitle(t.signInTitle),
          Lead(t.signInLead),
          FieldLabel(t.signInServer),
          TextField(
            controller: _server,
            keyboardType: TextInputType.url,
            autocorrect: false,
            textInputAction: TextInputAction.next,
            decoration: InputDecoration(hintText: t.signInServerHint, errorText: _serverError),
          ),
          FieldLabel(t.signInUsername),
          TextField(
            controller: _username,
            autocorrect: false,
            textInputAction: TextInputAction.next,
            autofillHints: const [AutofillHints.username],
          ),
          FieldLabel(t.signInPassword),
          TextField(
            controller: _password,
            obscureText: _hidden,
            autofillHints: const [AutofillHints.password],
            onSubmitted: (_) => _signIn(),
            decoration: InputDecoration(
              errorText: _error,
              errorMaxLines: 3,
              suffixIcon: IconButton(
                tooltip: _hidden ? t.signInShowPassword : t.signInHidePassword,
                icon: Icon(_hidden ? AppIcons.eye : AppIcons.eyeOff, size: 22),
                onPressed: () => setState(() => _hidden = !_hidden),
              ),
            ),
          ),
          const SizedBox(height: 24),
          BusyButton(label: t.signInButton, busy: _busy, onPressed: _signIn),
          OrDivider(t.signInOr),
          OutlinedButton.icon(
            onPressed: () => scanInvite(context),
            icon: const Icon(AppIcons.scan, size: 22),
            label: Text(t.signInScan),
          ),
          const SizedBox(height: 24),
          Small(t.signInNoPassword),
        ]),
      ),
    );
  }

  Future<void> _signIn() async {
    final t = AppLocalizations.of(context);
    final server = normalizePublicAddress(_server.text);
    setState(() {
      _serverError = server == null ? t.serverBadAddress : null;
      _error = null;
    });
    if (server == null || _username.text.trim().isEmpty || _password.text.isEmpty) return;
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    try {
      await Services.read(context).session.signIn(server, _username.text, _password.text);
      navigator.popUntil((r) => r.isFirst);
    } on NotShareServer {
      setState(() => _serverError = t.serverNotShare);
    } on NetworkException {
      setState(() => _serverError = t.commonOffline);
    } on ApiException catch (e) {
      setState(() => _error = switch (e.code) {
            'login_wrong' => t.signInWrong(e.attemptsLeft ?? 0),
            'login_locked' => t.signInLocked(t.waitTime(((e.retryAfter?.inSeconds ?? 60) / 60).ceil())),
            _ => t.commonFailed,
          });
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }
}
