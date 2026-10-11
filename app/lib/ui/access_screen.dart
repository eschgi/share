import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../app.dart';
import '../data/access.dart';
import '../data/api.dart';
import '../l10n/app_localizations.dart';
import 'first_start.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

/// When Cloudflare Access answered instead of Share: asks for the token, and says whether one
/// was saved, so that the caller tries again. Without a token it first says what's in the way
/// (113); a token Cloudflare turned away goes straight to the fields (112).
Future<bool> passCloudflareAccess(BuildContext context, Uri server, CloudflareAccessException e) async {
  final saved = await Navigator.push<bool>(
    context,
    MaterialPageRoute(builder: (_) => e.tokenSent ? AccessTokenScreen(server: server, refused: true) : AccessFoundScreen(server: server)),
  );
  return saved == true;
}

/// Screen 112: the service token, from Cloudflare's dashboard. Continue asks Cloudflare whether
/// it works before it is kept; it pops true then.
class AccessTokenScreen extends StatefulWidget {
  const AccessTokenScreen({super.key, required this.server, this.refused = false});
  final Uri server;

  /// Cloudflare turned the stored token away: said under the fields from the start.
  final bool refused;

  @override
  State<AccessTokenScreen> createState() => _AccessTokenScreenState();
}

class _AccessTokenScreenState extends State<AccessTokenScreen> {
  final _id = TextEditingController();
  final _secret = TextEditingController();
  bool _hidden = true;
  bool _busy = false;
  String? _idError;
  String? _error;

  @override
  void initState() {
    super.initState();
    final known = Services.read(context).access.forServer(widget.server);
    if (known != null) {
      _id.text = known.clientId;
      _secret.text = known.clientSecret;
    }
    if (widget.refused) WidgetsBinding.instance.addPostFrameCallback((_) => setState(() => _error = AppLocalizations.of(context).accessRefusedHere));
  }

  @override
  void dispose() {
    _id.dispose();
    _secret.dispose();
    super.dispose();
  }

  Future<void> _paste() async {
    final t = AppLocalizations.of(context);
    final messenger = ScaffoldMessenger.of(context);
    final text = (await Clipboard.getData(Clipboard.kTextPlain))?.text ?? '';
    final found = accessFromClipboard(text);
    if (found.id == null && found.secret == null) {
      messenger.showSnackBar(SnackBar(content: Text(t.accessPasteNothing)));
      return;
    }
    setState(() {
      if (found.id != null) _id.text = found.id!;
      if (found.secret != null) _secret.text = found.secret!;
      _idError = null;
      _error = null;
    });
  }

  Future<void> _continue() async {
    final t = AppLocalizations.of(context);
    final id = _id.text.trim(), secret = _secret.text.trim();
    setState(() {
      _idError = CloudflareAccess.validId(id) ? null : t.accessBadId;
      _error = CloudflareAccess.validSecret(secret) ? null : t.accessBadSecret;
    });
    if (_idError != null || _error != null) return;
    final repo = Services.read(context).access;
    final navigator = Navigator.of(context);
    final token = CloudflareAccess(host: widget.server.host.toLowerCase(), clientId: id, clientSecret: secret);
    setState(() => _busy = true);
    try {
      await repo.check(widget.server, token);
    } on CloudflareAccessException {
      if (mounted) setState(() => _error = t.accessRefusedHere);
      return;
    } on ApiException catch (e) {
      // Share's own answer came through, so Cloudflare let the token pass.
      if (!e.fromShare) {
        if (mounted) setState(() => _error = t.commonFailed);
        return;
      }
    } on NetworkException {
      if (mounted) setState(() => _error = t.commonOffline);
      return;
    } finally {
      if (mounted) setState(() => _busy = false);
    }
    await repo.save(token);
    navigator.pop(true);
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        leading: const ShareBackButton(),
        title: Text(t.accessTitle, style: const TextStyle(fontFamily: 'Roboto', fontSize: 20, fontWeight: FontWeight.w600)),
      ),
      body: SafeArea(
        child: Column(children: [
          Expanded(
            child: ListView(padding: const EdgeInsets.fromLTRB(24, 0, 24, 16), children: [
              Text(t.accessLead(widget.server.host), style: Theme.of(context).textTheme.bodyLarge!.copyWith(color: context.colors.text2)),
              const SizedBox(height: 18),
              FilledButton.tonalIcon(
                style: FilledButton.styleFrom(minimumSize: const Size.fromHeight(48)),
                onPressed: _busy ? null : _paste,
                icon: const Icon(AppIcons.clipboard, size: 20),
                label: Text(t.accessPaste),
              ),
              FieldLabel(t.accessClientId),
              TextField(
                controller: _id,
                autocorrect: false,
                enableSuggestions: false,
                textInputAction: TextInputAction.next,
                style: const TextStyle(fontFamily: mono, fontSize: 15),
                decoration: InputDecoration(hintText: '…….access', errorText: _idError, errorMaxLines: 2),
                onChanged: (_) => setState(() => _idError = null),
              ),
              FieldLabel(t.accessClientSecret),
              TextField(
                controller: _secret,
                obscureText: _hidden,
                autocorrect: false,
                enableSuggestions: false,
                style: const TextStyle(fontFamily: mono, fontSize: 15),
                onSubmitted: (_) => _continue(),
                onChanged: (_) => setState(() => _error = null),
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
              Help(t.accessHelp),
            ]),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
            child: Column(mainAxisSize: MainAxisSize.min, children: [
              BusyButton(label: t.accessContinue, busy: _busy, onPressed: _continue),
              Small(t.accessAtHome),
            ]),
          ),
        ]),
      ),
    );
  }
}

/// Screen 113: a sign-in, an invite or a PIN met Cloudflare's login instead of Share. An
/// admin's invite brings the token; or it is typed in (112). Pops true once a token is kept.
class AccessFoundScreen extends StatelessWidget {
  const AccessFoundScreen({super.key, required this.server});
  final Uri server;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton(close: true)),
      body: SafeArea(
        child: Column(children: [
          Expanded(
            child: ListView(padding: const EdgeInsets.fromLTRB(24, 8, 24, 16), children: [
              Align(
                alignment: Alignment.centerLeft,
                child: Container(
                  width: 76,
                  height: 76,
                  decoration: BoxDecoration(color: c.accentSoft, shape: BoxShape.circle),
                  child: Icon(AppIcons.shield, size: 34, color: c.accentText),
                ),
              ),
              const SizedBox(height: 26),
              HeroTitle(t.accessFoundTitle),
              Lead(t.accessFoundLead(server.host)),
              const SizedBox(height: 14),
              ChoiceCard(
                icon: AppIcons.scan,
                title: t.accessFoundScan,
                detail: t.accessFoundScanDetail,
                onTap: () async {
                  final navigator = Navigator.of(context);
                  navigator.pop(false);
                  // The invite brings the token, and InviteScreen keeps it.
                  unawaited(scanInvite(navigator.context));
                },
              ),
              const SizedBox(height: 14),
              ChoiceCard(
                icon: AppIcons.key,
                title: t.accessFoundEnter,
                detail: t.accessFoundEnterDetail,
                onTap: () async {
                  final navigator = Navigator.of(context);
                  final saved = await navigator.push<bool>(MaterialPageRoute(builder: (_) => AccessTokenScreen(server: server)));
                  if (saved == true) navigator.pop(true);
                },
              ),
            ]),
          ),
          Padding(padding: const EdgeInsets.fromLTRB(24, 0, 24, 16), child: Small(t.accessFoundWaits)),
        ]),
      ),
    );
  }
}

/// Screen 116: Cloudflare Access keeps the app out of this phone's server, because the token
/// expired or was revoked, or because the server now asks for one. Shown at the top of the
/// library, which keeps what it has; nothing pops up.
class AccessBanner extends StatelessWidget {
  const AccessBanner({super.key});

  @override
  Widget build(BuildContext context) {
    final services = Services.of(context);
    final api = services.api;
    return StreamBuilder<AccessProblem?>(
      stream: api.accessProblems,
      initialData: api.accessProblem,
      builder: (context, snap) {
        final problem = snap.data;
        final server = api.config?.publicUrl;
        if (problem == null || server == null) return const SizedBox.shrink();
        final t = AppLocalizations.of(context);
        final c = context.colors;
        final refused = problem == AccessProblem.refused;
        return Padding(
          padding: const EdgeInsets.fromLTRB(16, 10, 16, 0),
          child: Container(
            padding: const EdgeInsets.fromLTRB(14, 12, 14, 14),
            decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(16), border: Border.all(color: c.lineSoft)),
            child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Icon(AppIcons.alert, size: 20, color: c.danger),
              const SizedBox(width: 12),
              Expanded(
                child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(refused ? t.accessRefused : t.accessFoundTitle, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w600)),
                  const SizedBox(height: 3),
                  Text(refused ? t.accessRefusedDetail : t.accessMissingDetail, style: TextStyle(fontSize: 14, height: 1.45, color: c.text2)),
                  const SizedBox(height: 10),
                  FilledButton.tonal(
                    onPressed: () async {
                      final saved = await Navigator.push<bool>(context, MaterialPageRoute(builder: (_) => AccessTokenScreen(server: server, refused: refused)));
                      // The next answer over the public address says whether it works now.
                      if (saved == true) unawaited(services.session.refreshServer());
                    },
                    child: Text(refused ? t.accessUpdate : t.accessFoundEnter),
                  ),
                ]),
              ),
            ]),
          ),
        );
      },
    );
  }
}
