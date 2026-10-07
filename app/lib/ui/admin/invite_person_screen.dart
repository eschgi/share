import 'package:flutter/material.dart';

import '../../app.dart';
import '../../data/api.dart';
import '../../data/folders.dart';
import '../../data/models.dart';
import '../../data/platform.dart' show KeysException, KeysStatus;
import '../../l10n/app_localizations.dart';
import '../folders.dart';
import '../icons.dart';
import '../theme.dart';
import '../widgets.dart';

/// Screen 20: an invite as a QR code, or as a link, with the folders a new member gets (47).
/// With [forPerson] it adds a phone for someone who has an account.
class InvitePersonScreen extends StatefulWidget {
  const InvitePersonScreen({super.key, this.forPerson});
  final Person? forPerson;

  @override
  State<InvitePersonScreen> createState() => _InvitePersonScreenState();
}

class _InvitePersonScreenState extends State<InvitePersonScreen> {
  final _name = TextEditingController();
  Role _role = Role.member;
  List<String>? _picked; // the folders a new member gets, once changed
  NewInvite? _invite;
  String? _error;
  bool _busy = false;
  /// Why the invite would go without the keys of the encrypted folders it gives: said first, and
  /// made only when asked again, since the new person would then wait for an OK with a code.
  String? _noKeys;

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

  /// The folders a new member gets; until changed, the one the library shows.
  List<String> get _given {
    final folders = Services.read(context).folders;
    return _picked ?? inviteDefault(folders.list ?? const [], folders.shown?.id);
  }

  void _pick(String id) {
    final given = _given;
    setState(() {
      _noKeys = null;
      _picked = given.contains(id) ? [for (final f in given) if (f != id) f] : [...given, id];
    });
  }

  Future<void> _create({bool anyway = false}) async {
    final t = AppLocalizations.of(context);
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final services = Services.read(context);
      final admin = services.admin;
      final person = widget.forPerson;
      // The keys go along, locked with a secret that only the link carries, after a dot: the
      // person's own for another phone of one's own, the encrypted folders' for someone new.
      NewInvite invite;
      if (person == null) {
        final encrypted = [for (final f in services.folders.list ?? const <FolderInfo>[]) if (f.keyVersion != null) f.id];
        final gets = _role == Role.admin ? encrypted : [for (final id in _given) if (encrypted.contains(id)) id];
        // The root goes along whenever this phone trusts one, so the new person's phone or browser
        // checks the folders' keys with it.
        if (gets.isNotEmpty && services.keys.state.status == KeysStatus.loading) await services.keys.sync();
        ({String secret, List<Json> keys, String? root})? keys;
        if (services.keys.state.ready) {
          try {
            keys = await services.platform.inviteKeys(gets);
          } on KeysException {
            keys = null; // the new person waits for the family's phones instead
          }
        }
        // Without the root, the new person's phone or browser can't check the keys, and takes none.
        if (gets.isNotEmpty && !anyway && (keys == null || keys.root == null || keys.keys.isEmpty)) {
          final waiting = services.keys.state.status == KeysStatus.waiting;
          setState(() => _noKeys = waiting ? t.inviteNoKeysWaiting(_who) : t.inviteNoKeysFailed(_who));
          return;
        }
        _noKeys = null;
        invite = await admin.invite(_who, _role, folders: _given, keys: keys?.keys ?? const [], root: keys?.root);
        if (keys != null && (keys.keys.isNotEmpty || keys.root != null)) invite = invite.withLink('${invite.link}.${keys.secret}');
      } else {
        ({String secret, String locked, String? root})? own;
        if (person.isMe) {
          try {
            own = await services.platform.personKeyForInvite();
          } on KeysException {
            own = null;
          }
        }
        invite = await admin.invitePhone(person.id, personKey: own?.locked, root: own?.root);
        if (own != null) invite = invite.withLink('${invite.link}.${own.secret}');
      }
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
        _picked = null;
        _noKeys = null;
      });

  @override
  Widget build(BuildContext context) {
    final t = AppLocalizations.of(context);
    final c = context.colors;
    final invite = _invite;
    final person = widget.forPerson;
    final folders = Services.of(context).folders.list ?? const <FolderInfo>[];
    final given = _given;
    final lines = FolderLines(context);
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
                _RoleToggle(
                    role: _role,
                    enabled: invite == null,
                    onChanged: (r) => setState(() {
                          _noKeys = null;
                          _role = r;
                        })),
                Help(t.inviteRoleHelp),
                // With one folder there is nothing to choose: a new member gets it.
                if (folders.length > 1 && _role == Role.member) ...[
                  FieldLabel(t.foldersTitle),
                  SettingsGroup(children: [
                    for (final f in folders)
                      SettingsRow(
                        leading: FolderCover(folder: f),
                        title: f.name,
                        subtitle: lines.about(f),
                        trailing: Checkbox(value: given.contains(f.id), onChanged: invite == null ? (_) => _pick(f.id) : null),
                        onTap: invite == null ? () => _pick(f.id) : null,
                      ),
                  ]),
                ],
                if (folders.length > 1) Help(t.folderAdminsSeeAll),
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
              if (_noKeys != null && invite == null) Help(_noKeys!, error: true),
            ]),
          ),
          Padding(
            padding: const EdgeInsets.fromLTRB(24, 8, 24, 16),
            child: invite == null
                ? (person == null && _noKeys != null
                    ? OutlinedButton(
                        style: OutlinedButton.styleFrom(minimumSize: const Size.fromHeight(56)),
                        onPressed: _busy ? null : () => _create(anyway: true),
                        child: Text(t.inviteCreateAnyway),
                      )
                    : person == null
                    ? BusyButton(
                        label: t.inviteShowCode,
                        icon: AppIcons.qrCode,
                        busy: _busy,
                        // A member gets at least one folder, so there is something to see.
                        onPressed: _who.isEmpty || (_role == Role.member && given.isEmpty) ? null : _create,
                      )
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
