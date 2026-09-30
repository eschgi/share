import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/models.dart';
import '../../l10n/app_localizations.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 20: an invite as a QR code, or as a link. With [forPerson] it adds a phone for
/// someone who has an account.
class InvitePersonScreen extends StatefulWidget {
  const InvitePersonScreen({super.key, this.forPerson});
  final Person? forPerson;

  @override
  State<InvitePersonScreen> createState() => _InvitePersonScreenState();
}

class _InvitePersonScreenState extends State<InvitePersonScreen> {
  final _name = TextEditingController();
  Role _role = Role.member;
  NewInvite? _invite;
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _name.addListener(() => setState(() {}));
    if (widget.forPerson != null) _create();
  }

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
  }

  String get _who => widget.forPerson?.name ?? _name.text.trim();

  Future<void> _create() async {
    final t = AppLocalizations.of(context);
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final admin = Services.read(context).admin;
      final invite = widget.forPerson == null ? await admin.invite(_who, _role) : await admin.invitePhone(widget.forPerson!.id);
      if (mounted) setState(() => _invite = invite);
    } on ApiException {
      setState(() => _error = t.commonFailed);
    } on NetworkException {
      setState(() => _error = t.commonOffline);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _again() => setState(() {
        _invite = null;
        _name.clear();
        _role = Role.member;
      });

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final invite = _invite;
    final person = widget.forPerson;
    return Scaffold(
      appBar: AppBar(
        leading: const ShareBackButton(),
        title: Text(person == null ? t.inviteTitle : t.invitePhoneTitle(person.name),
            style: const TextStyle(fontFamily: 'Roboto', fontSize: 20, fontWeight: FontWeight.w600)),
      ),
      body: SafeArea(
        child: Column(children: [
          Expanded(
            child: ListView(padding: const EdgeInsets.fromLTRB(24, 8, 24, 16), children: [
              if (person == null) ...[
                FieldLabel(t.joinName, first: true),
                TextField(
                  controller: _name,
                  readOnly: invite != null,
                  textCapitalization: TextCapitalization.words,
                  textInputAction: TextInputAction.done,
                ),
                FieldLabel(t.joinRole),
                _RoleToggle(role: _role, enabled: invite == null, onChanged: (r) => setState(() => _role = r)),
                Help(t.inviteRoleHelp),
                const SizedBox(height: 22),
              ],
              if (invite != null)
                Container(
                  padding: const EdgeInsets.fromLTRB(20, 24, 20, 20),
                  decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(22), border: Border.all(color: c.lineSoft)),
                  child: Column(children: [
                    QrCodeView(invite.link, size: 196, label: t.inviteScanThis(_who)),
                    const SizedBox(height: 18),
                    Text(person == null ? t.inviteScanThis(_who) : t.invitePhoneLead(person.name),
                        textAlign: TextAlign.center, style: const TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600, height: 1.35)),
                    const SizedBox(height: 8),
                    Row(mainAxisAlignment: MainAxisAlignment.center, children: [
                      Icon(AppIcons.clock, size: 17, color: c.text3),
                      const SizedBox(width: 6),
                      Flexible(child: Text(t.inviteWorksOnce, style: TextStyle(fontSize: 14, color: c.text3))),
                    ]),
                  ]),
                )
              else if (person != null && _busy)
                const Padding(padding: EdgeInsets.only(top: 80), child: Center(child: CircularProgressIndicator())),
              if (_error != null) Help(_error!, error: true),
            ]),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
            child: invite == null
                ? (person == null
                    ? BusyButton(label: t.inviteShowCode, icon: AppIcons.qrCode, busy: _busy, onPressed: _who.isEmpty ? null : _create)
                    : const SizedBox())
                : Column(mainAxisSize: MainAxisSize.min, children: [
                    OutlinedButton.icon(
                      style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(56)),
                      onPressed: () => Services.read(context).platform.shareText(t.inviteShareText(_who, invite.link)),
                      icon: const Icon(AppIcons.share, size: 22),
                      label: Text(t.inviteSendLink),
                    ),
                    if (person == null) TextButton(onPressed: _again, child: Text(t.inviteAnother)),
                  ]),
          ),
        ]),
      ),
    );
  }
}

class _RoleToggle extends StatelessWidget {
  const _RoleToggle({required this.role, required this.onChanged, this.enabled = true});
  final Role role;
  final ValueChanged<Role> onChanged;
  final bool enabled;

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    Widget option(Role r, String label) {
      final selected = role == r;
      return Expanded(
        child: Semantics(
          selected: selected,
          button: true,
          child: InkWell(
            borderRadius: BorderRadius.circular(14),
            onTap: enabled ? () => onChanged(r) : null,
            child: Container(
              height: 50,
              alignment: Alignment.center,
              decoration: BoxDecoration(color: selected ? c.accentSoft : null, borderRadius: BorderRadius.circular(14)),
              child: Row(mainAxisSize: MainAxisSize.min, children: [
                if (selected) ...[Icon(AppIcons.check, size: 20, color: c.accentText), const SizedBox(width: 8)],
                Text(label,
                    style: TextStyle(fontSize: 16.5, fontWeight: FontWeight.w600, color: selected ? c.accentText : c.text2)),
              ]),
            ),
          ),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.all(5),
      decoration: BoxDecoration(color: c.s1, borderRadius: BorderRadius.circular(18), border: Border.all(color: c.line)),
      child: Row(children: [option(Role.member, t.roleMember), option(Role.admin, t.roleAdmin)]),
    );
  }
}
