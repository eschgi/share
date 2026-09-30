import 'package:flutter/material.dart';

import '../app.dart';
import '../data/api.dart';
import '../data/models.dart';
import '../data/server.dart';
import '../l10n/app_localizations.dart';
import 'format.dart';
import 'icons.dart';
import 'theme.dart';
import 'widgets.dart';

/// Screen 10: after scanning an invite or tapping its link. One tap, no password; the phone
/// gets its own long-lived key.
class InviteScreen extends StatefulWidget {
  const InviteScreen({super.key, required this.link});
  final InviteLink link;

  @override
  State<InviteScreen> createState() => _InviteScreenState();
}

class _InviteScreenState extends State<InviteScreen> {
  InvitePeek? _peek;
  String? _problem; // an error message, instead of the invite
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    try {
      final peek = await Services.read(context).session.peekInvite(widget.link);
      if (mounted) setState(() => _peek = peek);
    } on Exception catch (e) {
      if (mounted) setState(() => _problem = _message(e));
    }
  }

  String _message(Exception e) {
    final t = AppLocalizations.of(context);
    if (e is ApiException) {
      return switch (e.code) {
        'invite_used' => t.joinUsed,
        'invite_expired' => t.joinExpired,
        'invite_revoked' => t.joinRevoked,
        'invite_unknown' => t.joinUnknown,
        'invite_locked' => t.joinLocked,
        _ => t.commonFailed,
      };
    }
    return t.commonOffline;
  }

  Future<void> _join() async {
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    try {
      await Services.read(context).session.acceptInvite(widget.link);
      navigator.popUntil((r) => r.isFirst);
    } on Exception catch (e) {
      if (mounted) setState(() => _problem = _message(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final peek = _peek;
    Widget body;
    if (_problem != null) {
      body = Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        const SizedBox(height: 24),
        Container(
          width: 76,
          height: 76,
          decoration: BoxDecoration(color: c.accentSoft, shape: BoxShape.circle),
          child: Icon(AppIcons.alert, size: 34, color: c.accentText),
        ),
        const SizedBox(height: 26),
        HeroTitle(t.joinProblemTitle),
        Lead(_problem!),
      ]);
    } else if (peek == null) {
      body = const Center(child: Padding(padding: EdgeInsets.only(top: 120), child: CircularProgressIndicator()));
    } else {
      final locale = Localizations.localeOf(context).languageCode;
      final title = peek.inviter == null ? t.joinInvited : t.joinInvitedBy(peek.inviter!);
      body = Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        const SizedBox(height: 28),
        Center(child: _BigAvatar(name: peek.name)),
        const SizedBox(height: 26),
        HeroTitle(title, center: true),
        Lead(t.joinLead(peek.name), center: true),
        const SizedBox(height: 28),
        Container(
          decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.lineSoft)),
          child: Column(children: [
            _Fact(icon: AppIcons.user, label: t.joinName, value: peek.name),
            Divider(color: c.lineSoft),
            _Fact(icon: AppIcons.shield, label: t.joinRole, value: peek.role == Role.admin ? t.roleAdminLong : t.roleMemberLong),
            Divider(color: c.lineSoft),
            _Fact(icon: AppIcons.clock, label: t.joinValid, value: t.joinValidUntil(formatWhen(peek.expiresAt, DateTime.now(), locale))),
          ]),
        ),
      ]);
    }
    return Scaffold(
      appBar: AppBar(leading: const ShareBackButton(close: true)),
      body: SafeArea(
        child: Column(children: [
          Expanded(child: SingleChildScrollView(padding: const EdgeInsets.symmetric(horizontal: 24), child: body)),
          if (peek != null && _problem == null)
            Padding(
              padding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
              child: Column(children: [
                BusyButton(label: t.joinButton(peek.name), busy: _busy, onPressed: _join),
                Small(peek.inviter == null ? t.joinNotYou(peek.name) : t.joinNotYouInviter(peek.name, peek.inviter!)),
              ]),
            ),
        ]),
      ),
    );
  }
}

class _BigAvatar extends StatelessWidget {
  const _BigAvatar({required this.name});
  final String name;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return SizedBox(
      width: 108,
      height: 108,
      child: Stack(children: [
        Container(
          width: 104,
          height: 104,
          alignment: Alignment.center,
          decoration: const BoxDecoration(color: Color(0xFFD9BFA6), shape: BoxShape.circle),
          child: Text(name.isEmpty ? '?' : name.characters.first.toUpperCase(),
              style: const TextStyle(fontFamily: serif, fontSize: 44, fontWeight: FontWeight.w700, color: Color(0xFF3A2A1C))),
        ),
        Positioned(
          right: 0,
          bottom: 0,
          child: Container(
            width: 36,
            height: 36,
            decoration: BoxDecoration(color: c.okStrong, shape: BoxShape.circle, border: Border.all(color: c.bg, width: 3)),
            child: const Icon(AppIcons.check, size: 18, color: Colors.white),
          ),
        ),
      ]),
    );
  }
}

class _Fact extends StatelessWidget {
  const _Fact({required this.icon, required this.label, required this.value});
  final IconData icon;
  final String label, value;

  @override
  Widget build(BuildContext context) {
    final c = context.colors;
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 14),
      child: Row(children: [
        Icon(icon, size: 20, color: c.text3),
        const SizedBox(width: 12),
        SizedBox(width: 62, child: Text(label, style: TextStyle(fontSize: 14, color: c.text3))),
        Expanded(child: Text(value, style: const TextStyle(fontSize: 15, fontWeight: FontWeight.w500))),
      ]),
    );
  }
}
