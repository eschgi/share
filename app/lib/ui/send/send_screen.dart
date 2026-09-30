import 'package:flutter/material.dart';

import '../../data/models.dart';
import '../../data/pin.dart';
import '../../data/platform.dart';
import '../../l10n/app_localizations.dart';
import '../format.dart';
import '../icons.dart';
import '../sign_in.dart';
import '../theme.dart';
import '../widgets.dart';
import 'pin_entry_screen.dart';
import 'send_panel.dart';

/// Screen 15 for someone signed in: no PIN needed.
class SendScreen extends StatelessWidget {
  const SendScreen({super.key, required this.user, required this.navigation, required this.onAvatar});
  final User user;
  final Widget navigation;
  final VoidCallback onAvatar;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 22,
        title: Text(t.sendTitle),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 14),
            child: InkResponse(onTap: onAvatar, radius: 24, child: Avatar(name: user.name, id: user.id, size: 36)),
          ),
        ],
      ),
      bottomNavigationBar: navigation,
      body: ListView(padding: const EdgeInsets.fromLTRB(16, 0, 16, 24), children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(6, 0, 6, 18),
          child: Text(t.sendLeadSignedIn, style: TextStyle(fontSize: 17, height: 1.5, color: context.colors.text2)),
        ),
        const SendPanel(auth: SendAuth.device),
      ]),
    );
  }
}

/// Screen 15 without an account: sending with a PIN, as on the website.
class PinSendScreen extends StatelessWidget {
  const PinSendScreen({super.key, required this.session});
  final PinSession session;

  void _newPin(BuildContext context) =>
      Navigator.push(context, MaterialPageRoute<void>(builder: (_) => PinEntryScreen(server: session.server)));

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final locale = Localizations.localeOf(context).languageCode;
    final expires = session.expiresAt;
    return Scaffold(
      appBar: AppBar(
        titleSpacing: 22,
        title: Text(t.sendTitle),
        actions: [
          PopupMenuButton<String>(
            icon: const Icon(AppIcons.more),
            onSelected: (choice) async {
              if (choice == 'pin') return _newPin(context);
              await Navigator.push(context, MaterialPageRoute<void>(builder: (_) => const SignInScreen()));
            },
            itemBuilder: (_) => [
              PopupMenuItem(value: 'pin', child: Text(t.pinOther)),
              PopupMenuItem(value: 'sign-in', child: Text(t.pinSignIn)),
            ],
          ),
        ],
      ),
      body: ListView(padding: const EdgeInsets.fromLTRB(16, 0, 16, 24), children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(6, 0, 6, 18),
          child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
            Text(t.pinSendTo(session.server.host), style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
            if (session.kind == PinKind.day && expires != null) ...[
              const SizedBox(height: 4),
              Text(t.pinValidUntil(formatWhen(expires, DateTime.now(), locale)), style: TextStyle(fontSize: 14.5, color: c.warn)),
            ],
            const SizedBox(height: 8),
            Text(t.pinSendLead, style: TextStyle(fontSize: 16, height: 1.5, color: c.text2)),
          ]),
        ),
        if (session.ended) ...[
          Container(
            padding: const EdgeInsets.all(16),
            decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              Text(t.sendPinEnded, style: TextStyle(fontSize: 15.5, height: 1.5, color: c.text2)),
              const SizedBox(height: 12),
              FilledButton(onPressed: () => _newPin(context), child: Text(t.sendNewPin)),
            ]),
          ),
          const SizedBox(height: 16),
        ],
        SendPanel(auth: SendAuth.pin, onNewPin: () => _newPin(context)),
      ]),
    );
  }
}
